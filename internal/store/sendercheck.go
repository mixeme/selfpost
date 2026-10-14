package store

import (
	"fmt"
	"time"
)

// Why a send-log row with status "rejected" was refused (send_log.reason).
// A row written before the reason was recorded has "".
const (
	ReasonLimit  = "limit"  // a level-2 rate limit
	ReasonClient = "client" // the application's list of client addresses
	ReasonFrom   = "from"   // the From: line is not the application's
)

// Who decided a "bounced" row the log did not (send_log.reason).
const (
	// ReasonReconcile: the reconcile sweep closed the row - its message left
	// Postfix's queue with no delivery line ever read for it.
	ReasonReconcile = "reconcile"
	// ReasonReconcileContradicted: the sweep closed it and a delivery line
	// read afterwards gave the status the row now has.
	ReasonReconcileContradicted = "reconcile-contradicted"
)

// Settings keys of the sender check's statistics.
const (
	// settingSenderCheckSince is the first UTC day the statistics cover.
	settingSenderCheckSince = "sender_check_since"
	// settingSenderCheckDeferredAt is when a message was last deferred because
	// the check did not answer (RFC3339 UTC).
	settingSenderCheckDeferredAt = "sender_check_deferred_at"
	// settingSenderCheckCountedPlace is the place in mail.log of the last
	// warning counted ("<fingerprint>@<offset>"), saved with the count.
	settingSenderCheckCountedPlace = "sender_check_counted_place"
)

// senderCheckKeepDays is how long a day of the statistics is kept.
const senderCheckKeepDays = 366

const dayLayout = "2006-01-02"

// SenderCheckTotals is what the strict sender check has cost over a period.
type SenderCheckTotals struct {
	Down     time.Duration // the panel not answering while Postfix was running
	Deferred int64         // messages Postfix deferred because of it
}

// SenderCheckStats is the card of Server > Health.
type SenderCheckStats struct {
	Today, Month, Year SenderCheckTotals
	// Since is the first day counted; zero before anything was.
	Since time.Time
	// DeferredAt is when a message was last deferred; zero if never.
	DeferredAt time.Time
}

// StartSenderCheckStats records the first day the statistics cover, once.
func (s *Store) StartSenderCheckStats(now time.Time) error {
	v, err := s.GetSetting(settingSenderCheckSince)
	if err != nil || v != "" {
		return err
	}
	return s.SetSetting(settingSenderCheckSince, now.UTC().Format(dayLayout))
}

// AddSenderCheckDown adds to the day of at the time the panel did not answer
// while Postfix was running.
func (s *Store) AddSenderCheckDown(at time.Time, d time.Duration) error {
	secs := int64(d / time.Second)
	if secs <= 0 {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO sender_check_days (day, down_seconds) VALUES (?, ?)
		 ON CONFLICT (day) DO UPDATE SET down_seconds = down_seconds + excluded.down_seconds`,
		at.UTC().Format(dayLayout), secs,
	)
	if err != nil {
		return fmt.Errorf("sender check downtime: %w", err)
	}
	return nil
}

// AddSenderCheckDeferred counts, on the day of at, n messages deferred because
// the check did not answer, remembers at as the last such moment, and saves
// place - where in mail.log the counted line ends - in the same transaction:
// a count without its place, or the other way round, would be counted again
// or lost at the next start (audit 2026-10-12, stage 2b).
func (s *Store) AddSenderCheckDeferred(at time.Time, n int64, place string) error {
	if n <= 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("sender check deferred: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`INSERT INTO sender_check_days (day, deferred) VALUES (?, ?)
		 ON CONFLICT (day) DO UPDATE SET deferred = deferred + excluded.deferred`,
		at.UTC().Format(dayLayout), n,
	); err != nil {
		return fmt.Errorf("sender check deferred: %w", err)
	}
	// To the fraction of a second the log gives, and of a fixed width so that
	// the strings compare as the times do; a re-read log must not move the
	// mark back.
	stamp := at.UTC().Format("2006-01-02T15:04:05.000000Z")
	setting := func(key, value, cond string) error {
		_, err := tx.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT (key) DO UPDATE SET value = excluded.value`+cond, key, value)
		return err
	}
	if err := setting(settingSenderCheckDeferredAt, stamp, " WHERE excluded.value > settings.value"); err != nil {
		return fmt.Errorf("sender check deferred: %w", err)
	}
	if place != "" {
		if err := setting(settingSenderCheckCountedPlace, place, ""); err != nil {
			return fmt.Errorf("sender check deferred: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sender check deferred: %w", err)
	}
	return nil
}

// SenderCheckCountedPlace is the place in mail.log of the last warning
// counted; "" before any was.
func (s *Store) SenderCheckCountedPlace() (string, error) {
	return s.GetSetting(settingSenderCheckCountedPlace)
}

// SenderCheckDeferredAt is when a message was last counted as deferred; zero
// if none was.
func (s *Store) SenderCheckDeferredAt() (time.Time, error) {
	v, err := s.GetSetting(settingSenderCheckDeferredAt)
	if err != nil || v == "" {
		return time.Time{}, err
	}
	at, _ := time.Parse(time.RFC3339Nano, v)
	return at, nil
}

// SenderCheckStats sums the days up to now: today, the last 30 days and the
// last 12 months, each including today.
func (s *Store) SenderCheckStats(now time.Time) (SenderCheckStats, error) {
	now = now.UTC()
	var out SenderCheckStats
	sum := func(days int) (SenderCheckTotals, error) {
		var secs, deferred int64
		err := s.db.QueryRow(
			`SELECT COALESCE(SUM(down_seconds), 0), COALESCE(SUM(deferred), 0)
			 FROM sender_check_days WHERE day > ? AND day <= ?`,
			now.AddDate(0, 0, -days).Format(dayLayout), now.Format(dayLayout),
		).Scan(&secs, &deferred)
		if err != nil {
			return SenderCheckTotals{}, fmt.Errorf("sender check stats: %w", err)
		}
		return SenderCheckTotals{Down: time.Duration(secs) * time.Second, Deferred: deferred}, nil
	}
	var err error
	if out.Today, err = sum(1); err != nil {
		return out, err
	}
	if out.Month, err = sum(30); err != nil {
		return out, err
	}
	if out.Year, err = sum(365); err != nil {
		return out, err
	}
	since, err := s.GetSetting(settingSenderCheckSince)
	if err != nil {
		return out, err
	}
	out.Since, _ = time.Parse(dayLayout, since)
	deferredAt, err := s.GetSetting(settingSenderCheckDeferredAt)
	if err != nil {
		return out, err
	}
	out.DeferredAt, _ = time.Parse(time.RFC3339, deferredAt)
	return out, nil
}

// PruneSenderCheckDays deletes the days older than a year.
func (s *Store) PruneSenderCheckDays(now time.Time) error {
	_, err := s.db.Exec(`DELETE FROM sender_check_days WHERE day < ?`,
		now.UTC().AddDate(0, 0, -senderCheckKeepDays).Format(dayLayout))
	if err != nil {
		return fmt.Errorf("prune sender check days: %w", err)
	}
	return nil
}
