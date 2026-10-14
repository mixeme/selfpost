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
// SQLite's substr counts characters of a TEXT value, len(prefix) its bytes
// (https://sqlite.org/lang_corefunc.html#substr): the two agree for the ASCII
// prefixes the callers pass ("dns:", "limit:").
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

// LimitRefusal is the mail the applications of one sending domain were refused
// under a level-2 rate limit in a span of time.
type LimitRefusal struct {
	Domain string
	// Apps are the logins that were refused, in order.
	Apps  []string
	Count int
	First time.Time
}

// LimitRefusalsSince returns, per sending domain, the messages the journal
// milter refused under a level-2 limit at or after since.
//
// A refusal is a send-log row with status "rejected", and what such a row
// says about itself is not to be believed whole. Its domain is the one in
// MAIL FROM, which the client typed and Postfix has not yet held against the
// login when the milter refuses; so the domain here is the one the login is
// registered under, and a row whose login is no application's is not counted.
// The same status is written when an application is refused for its client
// address (applications.auth_ip_restrict), which is no limit at all; so a
// login counts only while a level-2 limit is set on it or on its domain.
// Since 2.4.0 a row says why it was refused, and one refused for its client
// address or its From: line is left out by that; a row from before has no
// reason and is still judged the old way.
func (s *Store) LimitRefusalsSince(since time.Time) ([]LimitRefusal, error) {
	// The status is a literal so that the partial index of migration 0005
	// answers the question: the query's term matches the index's WHERE
	// exactly (https://sqlite.org/partialindex.html § 3) when the statement is
	// first prepared.
	rows, err := s.db.Query(
		`SELECT d.name, a.login, COUNT(*), MIN(l.created_at)
		 FROM send_log l
		 JOIN domains d ON d.id = l.domain_id
		 JOIN applications a ON a.login = l.app_login AND a.domain_id = d.id
		 WHERE l.status = 'rejected' AND l.created_at >= ? AND l.reason IN ('', 'limit')
		   AND (EXISTS (SELECT 1 FROM rate_limits r WHERE r.scope = 'application' AND r.ref_id = a.id)
		     OR EXISTS (SELECT 1 FROM rate_limits r WHERE r.scope = 'domain' AND r.ref_id = d.id))
		 GROUP BY d.name, a.login ORDER BY d.name, a.login`,
		since.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("limit refusals: %w", err)
	}
	defer rows.Close()
	var out []LimitRefusal
	for rows.Next() {
		var (
			domain, login, first string
			count                int
		)
		if err := rows.Scan(&domain, &login, &count, &first); err != nil {
			return nil, fmt.Errorf("limit refusals scan: %w", err)
		}
		at, _ := time.Parse(time.RFC3339, first)
		if n := len(out); n > 0 && out[n-1].Domain == domain {
			out[n-1].Apps = append(out[n-1].Apps, login)
			out[n-1].Count += count
			if at.Before(out[n-1].First) {
				out[n-1].First = at
			}
			continue
		}
		out = append(out, LimitRefusal{Domain: domain, Apps: []string{login}, Count: count, First: at})
	}
	return out, rows.Err()
}
