package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Send-log status values (architecture.md § Persistence). "queued" is written
// by the journal-milter when a message is accepted; the log-tailer advances it
// to one of the final states as Postfix reports delivery per recipient.
const (
	StatusQueued   = "queued"
	StatusSent     = "sent"
	StatusDeferred = "deferred"
	StatusBounced  = "bounced"
	// StatusRejected marks a message the journal-milter refused with a 4xx under
	// a level-2 rate limit (guide § Rate limiting). Such a row never gets a
	// queue-id and is excluded from the level-2 message count (it was never
	// sent).
	StatusRejected = "rejected"
)

// SendLogEntry is a single queued send-log row. The journal-milter creates one
// per (queue-id, recipient) pair at end-of-message (architecture.md §
// Persistence); every field except the status/timestamps comes from the
// accepted message.
type SendLogEntry struct {
	QueueID  string
	Domain   string
	AppLogin string
	From     string
	To       string
	Subject  string
}

// InsertQueued records an accepted message in the send log with status
// "queued". It is called from the journal-milter hot path, so it returns any
// error for the caller to log rather than deciding policy here; the milter
// must stay fail-open regardless (architecture.md § Persistence).
func (s *Store) InsertQueued(e SendLogEntry) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT INTO send_log
			(queue_id, domain, app_login, from_addr, to_addr, subject, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.QueueID, e.Domain, e.AppLogin, e.From, e.To, e.Subject, StatusQueued, now, now,
	)
	if err != nil {
		return fmt.Errorf("insert send_log: %w", err)
	}
	return nil
}

// InsertRejected records a message the journal-milter refused under a level-2
// rate limit (guide § Rate limiting), so the rejection is visible in the
// send-log UI. Only the fields known at MAIL FROM are set (domain, sender, app
// login); there is no queue-id or recipient because the message was rejected
// before it was queued.
func (s *Store) InsertRejected(e SendLogEntry) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT INTO send_log
			(queue_id, domain, app_login, from_addr, to_addr, subject, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.QueueID, e.Domain, e.AppLogin, e.From, e.To, e.Subject, StatusRejected, now, now,
	)
	if err != nil {
		return fmt.Errorf("insert rejected send_log: %w", err)
	}
	return nil
}

// UpdateStatus advances the delivery status of the send-log rows matching a
// (queue-id, recipient) pair, which the log-tailer parses out of mail.log.
// Recipient matching is case-insensitive because Postfix may normalise address
// case between the milter (envelope) and the delivery log. It returns the
// number of rows updated so the caller can tell whether the line matched a
// journal entry.
func (s *Store) UpdateStatus(queueID, recipient, status string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE send_log SET status = ?, updated_at = ?
		 WHERE queue_id = ? AND to_addr = ? COLLATE NOCASE`,
		status, now, queueID, recipient,
	)
	if err != nil {
		return 0, fmt.Errorf("update send_log status: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// QueuedDelivery is a send-log row still waiting for a delivery result,
// reduced to what the log-tailer's reconcile sweep needs to look it up in the
// Postfix queue and, failing that, to close it (architecture.md § Log tailer).
type QueuedDelivery struct {
	QueueID string
	To      string
}

// ListQueuedOlderThan returns the rows still marked "queued" that were accepted
// before cutoff — old enough that Postfix should long since have reported a
// result for them. Rows without a queue-id are skipped: the milter refused
// those before Postfix ever saw the message, so the queue has nothing to say
// about them.
//
// created_at is stored as RFC3339 UTC, so a lexical comparison against the same
// format is chronologically correct.
func (s *Store) ListQueuedOlderThan(cutoff time.Time) ([]QueuedDelivery, error) {
	rows, err := s.db.Query(
		`SELECT queue_id, to_addr FROM send_log
		 WHERE status = ? AND queue_id <> '' AND created_at < ?
		 ORDER BY id`,
		StatusQueued, cutoff.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("list queued send_log rows: %w", err)
	}
	defer rows.Close()

	var out []QueuedDelivery
	for rows.Next() {
		var d QueuedDelivery
		if err := rows.Scan(&d.QueueID, &d.To); err != nil {
			return nil, fmt.Errorf("scan queued send_log row: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SendLogRow is one row as returned to the monitoring UI (architecture.md §
// Persistence): a SendLogEntry plus the fields that only exist once a row has
// been written (id, current status, timestamps).
type SendLogRow struct {
	ID        int64
	QueueID   string
	Domain    string
	AppLogin  string
	From      string
	To        string
	Subject   string
	Status    string
	CreatedAt time.Time
	// UpdatedAt is when the status last changed — the log-tailer's report of
	// the delivery attempt. It equals CreatedAt while a row is still queued.
	UpdatedAt time.Time
}

// ErrSendLogNotFound is returned by GetSendLog for an id that no longer exists.
// Send-log rows are pruned on the retention window, so a bookmarked delivery
// disappearing is expected, not a fault.
var ErrSendLogNotFound = errors.New("send-log entry not found")

// GetSendLog returns a single send-log row by id, for the delivery detail page
// the log links each row to.
func (s *Store) GetSendLog(id int64) (SendLogRow, error) {
	var (
		row                  SendLogRow
		createdAt, updatedAt string
	)
	err := s.db.QueryRow(
		`SELECT id, queue_id, domain, app_login, from_addr, to_addr, subject, status, created_at, updated_at
		 FROM send_log WHERE id = ?`, id,
	).Scan(&row.ID, &row.QueueID, &row.Domain, &row.AppLogin, &row.From, &row.To,
		&row.Subject, &row.Status, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SendLogRow{}, ErrSendLogNotFound
	}
	if err != nil {
		return SendLogRow{}, fmt.Errorf("get send_log %d: %w", id, err)
	}
	row.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	row.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return row, nil
}

// SendLogFilter narrows QuerySendLog/CountSendLog by domain and/or
// application login. An empty field matches everything.
type SendLogFilter struct {
	Domain   string
	AppLogin string
}

// QuerySendLog returns send-log rows matching filter, newest first, for the
// monitoring screen's server-side pagination (product.md's send-log view).
func (s *Store) QuerySendLog(filter SendLogFilter, limit, offset int) ([]SendLogRow, error) {
	where, args := sendLogWhere(filter)
	args = append(args, limit, offset)
	rows, err := s.db.Query(
		`SELECT id, queue_id, domain, app_login, from_addr, to_addr, subject, status, created_at, updated_at
		 FROM send_log`+where+`
		 ORDER BY id DESC
		 LIMIT ? OFFSET ?`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query send_log: %w", err)
	}
	defer rows.Close()

	var out []SendLogRow
	for rows.Next() {
		var (
			row                  SendLogRow
			createdAt, updatedAt string
		)
		if err := rows.Scan(&row.ID, &row.QueueID, &row.Domain, &row.AppLogin,
			&row.From, &row.To, &row.Subject, &row.Status, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan send_log row: %w", err)
		}
		row.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		row.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		out = append(out, row)
	}
	return out, rows.Err()
}

// CountSendLog returns how many send-log rows match filter, so the monitoring
// screen can render page numbers/next-prev links.
func (s *Store) CountSendLog(filter SendLogFilter) (int64, error) {
	where, args := sendLogWhere(filter)
	var n int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM send_log`+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count send_log: %w", err)
	}
	return n, nil
}

func sendLogWhere(f SendLogFilter) (string, []any) {
	var clauses []string
	var args []any
	if f.Domain != "" {
		clauses = append(clauses, "domain = ?")
		args = append(args, f.Domain)
	}
	if f.AppLogin != "" {
		clauses = append(clauses, "app_login = ?")
		args = append(args, f.AppLogin)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// DeleteSendLogBefore removes send-log rows created before cutoff,
// implementing the configurable retention window (architecture.md §
// Persistence, SEND_LOG_RETENTION_DAYS). It returns the number of rows pruned.
// created_at is stored as RFC3339 UTC, so a lexical comparison against the
// same format is chronologically correct.
func (s *Store) DeleteSendLogBefore(cutoff time.Time) (int64, error) {
	res, err := s.db.Exec(
		`DELETE FROM send_log WHERE created_at < ?`,
		cutoff.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("prune send_log: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
