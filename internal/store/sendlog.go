package store

import (
	"fmt"
	"time"
)

// Send-log status values (spec 7.3). "queued" is written by the journal-milter
// when a message is accepted; the log-tailer advances it to one of the final
// states as Postfix reports delivery per recipient.
const (
	StatusQueued   = "queued"
	StatusSent     = "sent"
	StatusDeferred = "deferred"
	StatusBounced  = "bounced"
)

// SendLogEntry is a single queued send-log row. The journal-milter creates one
// per (queue-id, recipient) pair at end-of-message (spec 7.3.3); every field
// except the status/timestamps comes from the accepted message.
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
// error for the caller to log rather than deciding policy here; the milter must
// stay fail-open regardless (spec 7.3).
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

// DeleteSendLogBefore removes send-log rows created before cutoff, implementing
// the configurable retention window (spec 7.3, SEND_LOG_RETENTION_DAYS). It
// returns the number of rows pruned. created_at is stored as RFC3339 UTC, so a
// lexical comparison against the same format is chronologically correct.
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
