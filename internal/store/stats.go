package store

import (
	"fmt"
	"math"
	"time"
)

// StatsWindowDays is the rolling window for send statistics on the domain page.
const StatsWindowDays = 30

// SendStats holds message-volume metrics over a stats window (guide § Rate
// limiting — same counting rules as CountMessages).
type SendStats struct {
	Total       int64
	PeakPerHour int64
	AvgPerHour  float64
	WindowDays  int     // actual days queried (min(30, retention))
	WindowHours float64 // hours used for the average-rate denominator
}

// StatsWindow computes the since timestamp and hours denominator for send
// statistics. The query window is min(30, retention) days; hours_in_window is
// min(720, entity age in hours, retention hours).
func StatsWindow(retentionDays int, entityCreated time.Time) (since time.Time, hours float64, windowDays int) {
	if retentionDays <= 0 {
		retentionDays = SendLogRetentionDaysDefault
	}
	windowDays = StatsWindowDays
	if retentionDays < StatsWindowDays {
		windowDays = retentionDays
	}
	since = time.Now().UTC().AddDate(0, 0, -windowDays)

	retentionHours := float64(retentionDays) * 24
	ageHours := time.Since(entityCreated).Hours()
	if ageHours < 0 {
		ageHours = 0
	}

	hours = math.Min(720, math.Min(ageHours, retentionHours))
	if hours < 1 {
		hours = 1
	}
	return since, hours, windowDays
}

// DomainSendStats returns 30-day (or shorter when retention is lower) send
// statistics for a domain name.
func (s *Store) DomainSendStats(name string, retentionDays int, createdAt time.Time) (SendStats, error) {
	since, hours, windowDays := StatsWindow(retentionDays, createdAt)
	stats, err := s.sendStats("domain", name, since, hours)
	if err != nil {
		return SendStats{}, err
	}
	stats.WindowDays = windowDays
	stats.WindowHours = hours
	return stats, nil
}

// AppSendStats returns send statistics for an application login.
func (s *Store) AppSendStats(login string, retentionDays int, createdAt time.Time) (SendStats, error) {
	since, hours, windowDays := StatsWindow(retentionDays, createdAt)
	stats, err := s.sendStats("app_login", login, since, hours)
	if err != nil {
		return SendStats{}, err
	}
	stats.WindowDays = windowDays
	stats.WindowHours = hours
	return stats, nil
}

func (s *Store) sendStats(column, ref string, since time.Time, hours float64) (SendStats, error) {
	sinceStr := since.UTC().Format(time.RFC3339)

	var total int64
	err := s.db.QueryRow(
		`SELECT COUNT(DISTINCT queue_id) FROM send_log
		 WHERE `+column+` = ? AND status != ? AND created_at >= ?`,
		ref, StatusRejected, sinceStr,
	).Scan(&total)
	if err != nil {
		return SendStats{}, fmt.Errorf("send stats total %s %q: %w", column, ref, err)
	}

	var peak int64
	err = s.db.QueryRow(
		`SELECT COALESCE(MAX(bucket_count), 0) FROM (
		   SELECT COUNT(DISTINCT queue_id) AS bucket_count FROM send_log
		   WHERE `+column+` = ? AND status != ? AND created_at >= ?
		   GROUP BY substr(created_at, 1, 13)
		 )`,
		ref, StatusRejected, sinceStr,
	).Scan(&peak)
	if err != nil {
		return SendStats{}, fmt.Errorf("send stats peak %s %q: %w", column, ref, err)
	}

	avg := float64(total) / hours
	return SendStats{Total: total, PeakPerHour: peak, AvgPerHour: avg}, nil
}
