package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// settingNotifications is the instance switch for notification e-mails. A
// missing value means on: the switch exists to turn them off.
const settingNotifications = "notifications"

// NotificationsEnabled reports whether the instance sends notification e-mails
// at all (Server › Settings).
func (s *Store) NotificationsEnabled() (bool, error) {
	v, err := s.GetSetting(settingNotifications)
	if err != nil {
		return false, err
	}
	return v != "off", nil
}

// SetNotificationsEnabled turns notification e-mails on or off for everyone.
func (s *Store) SetNotificationsEnabled(on bool) error {
	v := "on"
	if !on {
		v = "off"
	}
	return s.SetSetting(settingNotifications, v)
}

// SetUserNotify sets a user's own switch (Account).
func (s *Store) SetUserNotify(id int64, on bool) error {
	res, err := s.db.Exec("UPDATE users SET notify = ? WHERE id = ?", on, id)
	if err != nil {
		return fmt.Errorf("set user notify: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// NotifyRecipients returns the addresses a notification goes to: every global
// user, and for a subject that is a sending domain's (domainName not empty)
// also the domain users it is assigned to - by name on their list, or by
// "all domains". Only users with an e-mail and their own switch on; an address
// two users share is returned once.
func (s *Store) NotifyRecipients(domainName string) ([]string, error) {
	query := `SELECT DISTINCT email FROM users
		 WHERE notify = 1 AND email <> '' AND role = ?`
	args := []any{RoleGlobal}
	if domainName != "" {
		query = `SELECT DISTINCT email FROM users
		 WHERE notify = 1 AND email <> '' AND (
			role = ?
			OR all_domains = 1
			OR EXISTS (SELECT 1 FROM user_domains ud JOIN domains d ON d.id = ud.domain_id
			           WHERE ud.user_id = users.id AND d.name = ?))`
		args = append(args, domainName)
	}
	rows, err := s.db.Query(query+" ORDER BY email", args...)
	if err != nil {
		return nil, fmt.Errorf("notify recipients: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, fmt.Errorf("notify recipients scan: %w", err)
		}
		out = append(out, email)
	}
	return out, rows.Err()
}

// NotifyState is what the panel remembers about one subject of notification
// (migration 0004 describes the keys and the fields).
type NotifyState struct {
	Key    string
	Phase  string
	Count  int
	Mark   string
	Since  time.Time
	SentAt time.Time // zero when nothing was sent yet
}

// GetNotifyState returns the state kept under key; ok is false when there is
// none, which is the state of a subject nothing is wrong with.
func (s *Store) GetNotifyState(key string) (NotifyState, bool, error) {
	st := NotifyState{Key: key}
	var since, sentAt string
	err := s.db.QueryRow(
		"SELECT phase, count, mark, since, sent_at FROM notify_state WHERE key = ?", key,
	).Scan(&st.Phase, &st.Count, &st.Mark, &since, &sentAt)
	if errors.Is(err, sql.ErrNoRows) {
		return NotifyState{}, false, nil
	}
	if err != nil {
		return NotifyState{}, false, fmt.Errorf("get notify state %s: %w", key, err)
	}
	st.Since, _ = time.Parse(time.RFC3339, since)
	if sentAt != "" {
		st.SentAt, _ = time.Parse(time.RFC3339, sentAt)
	}
	return st, true, nil
}

// SetNotifyState writes the state of one subject, replacing what was there.
func (s *Store) SetNotifyState(st NotifyState) error {
	sentAt := ""
	if !st.SentAt.IsZero() {
		sentAt = st.SentAt.UTC().Format(time.RFC3339)
	}
	_, err := s.db.Exec(
		`INSERT INTO notify_state (key, phase, count, mark, since, sent_at) VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET phase = excluded.phase, count = excluded.count,
			mark = excluded.mark, since = excluded.since, sent_at = excluded.sent_at`,
		st.Key, st.Phase, st.Count, st.Mark, st.Since.UTC().Format(time.RFC3339), sentAt,
	)
	if err != nil {
		return fmt.Errorf("set notify state %s: %w", st.Key, err)
	}
	return nil
}

// DeleteNotifyState forgets a subject: nothing is wrong with it any more, or
// it no longer exists.
func (s *Store) DeleteNotifyState(key string) error {
	if _, err := s.db.Exec("DELETE FROM notify_state WHERE key = ?", key); err != nil {
		return fmt.Errorf("delete notify state %s: %w", key, err)
	}
	return nil
}

// ListNotifyStateKeys returns the keys that begin with prefix, so that the
// states of subjects that are gone - a deleted domain - can be found.
func (s *Store) ListNotifyStateKeys(prefix string) ([]string, error) {
	rows, err := s.db.Query(
		"SELECT key FROM notify_state WHERE substr(key, 1, ?) = ? ORDER BY key", len(prefix), prefix)
	if err != nil {
		return nil, fmt.Errorf("list notify state: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("list notify state scan: %w", err)
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

// LimitRefusal is the mail one application was refused under a level-2 rate
// limit in a span of time: how many messages and when the first.
type LimitRefusal struct {
	Domain   string
	AppLogin string
	Count    int
	First    time.Time
}

// LimitRefusalsSince returns, per domain and application, the messages the
// journal milter refused under a level-2 limit at or after since (send-log
// rows with status "rejected").
func (s *Store) LimitRefusalsSince(since time.Time) ([]LimitRefusal, error) {
	rows, err := s.db.Query(
		`SELECT COALESCE(domain, ''), COALESCE(app_login, ''), COUNT(*), MIN(created_at) FROM send_log
		 WHERE status = ? AND created_at >= ?
		 GROUP BY domain, app_login ORDER BY domain, app_login`,
		StatusRejected, since.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("limit refusals: %w", err)
	}
	defer rows.Close()
	var out []LimitRefusal
	for rows.Next() {
		var (
			r     LimitRefusal
			first string
		)
		if err := rows.Scan(&r.Domain, &r.AppLogin, &r.Count, &first); err != nil {
			return nil, fmt.Errorf("limit refusals scan: %w", err)
		}
		r.First, _ = time.Parse(time.RFC3339, first)
		out = append(out, r)
	}
	return out, rows.Err()
}
