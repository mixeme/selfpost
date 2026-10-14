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

// ReasonReconcile marks a "bounced" row the reconcile sweep closed (send_log.reason):
// its message left Postfix's queue with no delivery line ever read for it.
const ReasonReconcile = "reconcile"

// Settings keys of the sender check's statistics.
const (
	// settingSenderCheckSince is the first UTC day the statistics cover.
	settingSenderCheckSince = "sender_check_since"
	// settingSenderCheckDeferredAt is when a message was last deferred because
	// the check did not answer (RFC3339 UTC).
	settingSenderCheckDeferredAt = "sender_check_deferred_at"
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
// the check did not answer, and remembers at as the last such moment.
func (s *Store) AddSenderCheckDeferred(at time.Time, n int64) error {
	if n <= 0 {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO sender_check_days (day, deferred) VALUES (?, ?)
		 ON CONFLICT (day) DO UPDATE SET deferred = deferred + excluded.deferred`,
		at.UTC().Format(dayLayout), n,
	)
	if err != nil {
		return fmt.Errorf("sender check deferred: %w", err)
	}
	// Lines are read in order, but a re-read log must not move the mark back.
	last, err := s.GetSetting(settingSenderCheckDeferredAt)
	if err != nil {
		return err
	}
	// To the fraction of a second the log gives, and of a fixed width so that
	// the strings compare as the times do.
	stamp := at.UTC().Format("2006-01-02T15:04:05.000000Z")
	if stamp > last {
		return s.SetSetting(settingSenderCheckDeferredAt, stamp)
	}
	return nil
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
