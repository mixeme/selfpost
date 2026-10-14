package store

import (
	"fmt"
	"time"
)

// The counters of delivery_log_days (migration 0007): what the reconcile sweep
// and the log tailer did, a row per UTC day, read off a copy of the database
// to decide the sweep's fate (audit 2026-10-12, stage 1). No page shows them.

// deliveryLogKeepDays is how long a day of the counters is kept.
const deliveryLogKeepDays = 366

// deliveryLogCounters are the columns addDeliveryLogDay may add to: the
// name is put into the statement, so nothing else may reach it.
var deliveryLogCounters = map[string]bool{
	"reconcile_closed": true, "reconcile_missed": true, "tailer_blind_seconds": true,
}

// addDeliveryLogDay adds n to one counter of the day of at.
func (s *Store) addDeliveryLogDay(at time.Time, column string, n int64) error {
	if !deliveryLogCounters[column] {
		return fmt.Errorf("delivery log day: no counter %q", column)
	}
	if n <= 0 {
		return nil
	}
	_, err := s.db.Exec(fmt.Sprintf(
		`INSERT INTO delivery_log_days (day, %[1]s) VALUES (?, ?)
		 ON CONFLICT (day) DO UPDATE SET %[1]s = %[1]s + excluded.%[1]s`, column),
		at.UTC().Format(dayLayout), n,
	)
	if err != nil {
		return fmt.Errorf("delivery log day, %s: %w", column, err)
	}
	return nil
}

// AddReconcileClosed counts n rows the reconcile sweep closed.
func (s *Store) AddReconcileClosed(at time.Time, n int64) error {
	return s.addDeliveryLogDay(at, "reconcile_closed", n)
}

// AddReconcileMissed counts n rows the sweep closed whose delivery line was
// in mail.log all along: the tailer did not read it.
func (s *Store) AddReconcileMissed(at time.Time, n int64) error {
	return s.addDeliveryLogDay(at, "reconcile_missed", n)
}

// AddTailerBlind counts a stretch the log tailer could not read mail.log,
// on the day it began. Under a second counts nothing.
func (s *Store) AddTailerBlind(from time.Time, d time.Duration) error {
	return s.addDeliveryLogDay(from, "tailer_blind_seconds", int64(d/time.Second))
}

// SetQueuedOverLifetime records how many rows are still queued past Postfix's
// queue lifetime. It is a level, not a sum: each sweep overwrites the day's
// value, so a past day keeps what its last sweep saw.
func (s *Store) SetQueuedOverLifetime(at time.Time, n int64) error {
	_, err := s.db.Exec(
		`INSERT INTO delivery_log_days (day, queued_over_lifetime) VALUES (?, ?)
		 ON CONFLICT (day) DO UPDATE SET queued_over_lifetime = excluded.queued_over_lifetime`,
		at.UTC().Format(dayLayout), n,
	)
	if err != nil {
		return fmt.Errorf("delivery log day, queued_over_lifetime: %w", err)
	}
	return nil
}

// CountQueuedOlderThan counts the rows still "queued" that were accepted
// before cutoff. created_at is RFC3339 UTC, so the strings compare as times.
func (s *Store) CountQueuedOlderThan(cutoff time.Time) (int64, error) {
	var n int64
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM send_log WHERE status = ? AND queue_id <> '' AND created_at < ?`,
		StatusQueued, cutoff.UTC().Format(time.RFC3339),
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count queued send_log rows: %w", err)
	}
	return n, nil
}

// DeliveryLogDay is one day of the counters, for tests and for reading a copy.
type DeliveryLogDay struct {
	Day                                               string
	Closed, Missed, Contradicted, BlindSeconds, Stuck int64
}

// DeliveryLogDays returns the counted days, oldest first.
func (s *Store) DeliveryLogDays() ([]DeliveryLogDay, error) {
	rows, err := s.db.Query(
		`SELECT day, reconcile_closed, reconcile_missed, reconcile_contradicted, tailer_blind_seconds, queued_over_lifetime
		 FROM delivery_log_days ORDER BY day`)
	if err != nil {
		return nil, fmt.Errorf("delivery log days: %w", err)
	}
	defer rows.Close()
	var out []DeliveryLogDay
	for rows.Next() {
		var d DeliveryLogDay
		if err := rows.Scan(&d.Day, &d.Closed, &d.Missed, &d.Contradicted, &d.BlindSeconds, &d.Stuck); err != nil {
			return nil, fmt.Errorf("scan delivery log day: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// PruneDeliveryLogDays deletes the days older than a year.
func (s *Store) PruneDeliveryLogDays(now time.Time) error {
	_, err := s.db.Exec(`DELETE FROM delivery_log_days WHERE day < ?`,
		now.UTC().AddDate(0, 0, -deliveryLogKeepDays).Format(dayLayout))
	if err != nil {
		return fmt.Errorf("prune delivery log days: %w", err)
	}
	return nil
}
