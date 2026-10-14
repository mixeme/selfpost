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
	// Nothing before the entity was created is its own: rows from then are a
	// predecessor's of the same name or login (review of audit stage 2).
	if c := entityCreated.UTC(); c.After(since) {
		since = c
	}
	return since, hours, windowDays
}

// DomainSendStats returns 30-day (or shorter when retention is lower) send
// statistics for a domain, named by its name: the rows that belong to the
// domain registered under it now (audit 2026-10-12, L12).
func (s *Store) DomainSendStats(name string, retentionDays int, createdAt time.Time) (SendStats, error) {
	since, hours, windowDays := StatsWindow(retentionDays, createdAt)
	stats, err := s.sendStats(domainByName, name, since, hours)
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
	stats, err := s.sendStats(appByLogin, login, since, hours)
	if err != nil {
		return SendStats{}, err
	}
	stats.WindowDays = windowDays
	stats.WindowHours = hours
	return stats, nil
}

// domainByName selects a domain's send-log rows by the id of the domain
// registered under the name it is given.
const domainByName = "domain_id = (SELECT d.id FROM domains d WHERE d.name = ?)"

// appByLogin selects an application's send-log rows: its login's rows of its
// domain. A login is unique while it exists, but a deleted application's
// login can be taken again in another domain, and its numbers - statistics,
// the level-2 count - must not start with the old one's (review of audit
// stage 2). ?1 twice; a plain ? after it takes the next number.
// SQLite 3.53.4, https://sqlite.org/lang_expr.html § 4: a plain ? is numbered
// one more than the largest number already assigned; modernc.org/sqlite
// v1.60.1, conn.go bind() and argFinder.find(): ?NNN takes argument NNN, a
// plain ? the argument of its number.
const appByLogin = "app_login = ?1 AND domain_id = (SELECT a.domain_id FROM applications a WHERE a.login = ?1)"

// sendStats counts the rows that match, a condition with one placeholder
// taking ref.
func (s *Store) sendStats(match, ref string, since time.Time, hours float64) (SendStats, error) {
	sinceStr := since.UTC().Format(time.RFC3339)

	var total int64
	err := s.db.QueryRow(
		`SELECT COUNT(DISTINCT queue_id) FROM send_log
		 WHERE `+match+` AND status != ? AND created_at >= ?`,
		ref, StatusRejected, sinceStr,
	).Scan(&total)
	if err != nil {
		return SendStats{}, fmt.Errorf("send stats total %q: %w", ref, err)
	}

	// The busiest UTC hour: substr counts characters from 1
	// (https://sqlite.org/lang_corefunc.html#substr), so 1-13 of an RFC3339
	// UTC stamp is YYYY-MM-DDTHH.
	var peak int64
	err = s.db.QueryRow(
		`SELECT COALESCE(MAX(bucket_count), 0) FROM (
		   SELECT COUNT(DISTINCT queue_id) AS bucket_count FROM send_log
		   WHERE `+match+` AND status != ? AND created_at >= ?
		   GROUP BY substr(created_at, 1, 13)
		 )`,
		ref, StatusRejected, sinceStr,
	).Scan(&peak)
	if err != nil {
		return SendStats{}, fmt.Errorf("send stats peak %q: %w", ref, err)
	}

	avg := float64(total) / hours
	return SendStats{Total: total, PeakPerHour: peak, AvgPerHour: avg}, nil
}
