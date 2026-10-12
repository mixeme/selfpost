package store

import (
	"fmt"
	"time"
)

// SendLogLine is one mail.log line Postfix wrote under a queue id, kept so a
// message's page can show it after mail.log has rotated (architecture.md § Log
// tailer). At is the line's own time, not when the tailer read it.
type SendLogLine struct {
	QueueID string
	At      time.Time
	Line    string
}

// SendLogLineMax is how many bytes of a line are kept. Postfix quotes the far
// end's reply in a delivery line, so a line is as long as someone else cared
// to make it; what a page needs of it is the beginning.
const SendLogLineMax = 4096

// sendLogLineBefore is how long before a row was written a line may be dated
// and still be the row's. The journal milter writes the row at end-of-message;
// the smtpd line that opens the queue id is written when the first recipient
// is accepted, and the message body can take its time after that.
const sendLogLineBefore = time.Hour

func cutSendLogLine(line string) string {
	if len(line) > SendLogLineMax {
		return line[:SendLogLineMax]
	}
	return line
}

// InsertSendLogLine stores one line. The tailer calls it for every line headed
// by a queue id, whether or not a send-log row exists for it yet.
func (s *Store) InsertSendLogLine(l SendLogLine) error {
	_, err := s.db.Exec(
		`INSERT INTO send_log_lines (queue_id, logged_at, line) VALUES (?, ?, ?)`,
		l.QueueID, l.At.UTC().Format(time.RFC3339), cutSendLogLine(l.Line),
	)
	if err != nil {
		return fmt.Errorf("insert send_log_lines: %w", err)
	}
	return nil
}

// InsertSendLogLineOnce stores a line unless the same one is already there,
// and reports whether it wrote. It is for the lines the tailer reads again
// after an unclean stop; a line carries its time and its process id, so two
// that are equal are one line read twice.
func (s *Store) InsertSendLogLineOnce(l SendLogLine) (bool, error) {
	line := cutSendLogLine(l.Line)
	res, err := s.db.Exec(
		`INSERT INTO send_log_lines (queue_id, logged_at, line)
		 SELECT ?, ?, ?
		 WHERE NOT EXISTS (SELECT 1 FROM send_log_lines WHERE queue_id = ? AND line = ?)`,
		l.QueueID, l.At.UTC().Format(time.RFC3339), line,
		l.QueueID, line,
	)
	if err != nil {
		return false, fmt.Errorf("insert send_log_lines once: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SendLogLines returns the stored lines of the message a send-log row is
// about, oldest first and at most n of them - the latest n when there are
// more, since what a message did last is what its page is opened for.
//
// Postfix reuses a queue id once the message has left the queue, and a row
// lives far longer than that, so the id alone does not say whose a line is.
// A line is the row's when it is dated from shortly before the row was written
// to lifetime after it; lifetime is how long Postfix may keep working on a
// message, with a margin, and the caller's to state (postfix.RetryPolicy).
func (s *Store) SendLogLines(row SendLogRow, lifetime time.Duration, n int) ([]SendLogLine, error) {
	if row.QueueID == "" || n <= 0 {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT logged_at, line FROM (
			SELECT id, logged_at, line FROM send_log_lines
			WHERE queue_id = ? AND logged_at >= ? AND logged_at <= ?
			ORDER BY id DESC LIMIT ?)
		 ORDER BY id`,
		row.QueueID,
		row.CreatedAt.Add(-sendLogLineBefore).UTC().Format(time.RFC3339),
		row.CreatedAt.Add(lifetime).UTC().Format(time.RFC3339),
		n,
	)
	if err != nil {
		return nil, fmt.Errorf("query send_log_lines: %w", err)
	}
	defer rows.Close()

	var out []SendLogLine
	for rows.Next() {
		var at string
		l := SendLogLine{QueueID: row.QueueID}
		if err := rows.Scan(&at, &l.Line); err != nil {
			return nil, fmt.Errorf("scan send_log_lines row: %w", err)
		}
		l.At, _ = time.Parse(time.RFC3339, at)
		out = append(out, l)
	}
	return out, rows.Err()
}

// PruneSendLogLines deletes the lines dated before cutoff whose queue id has
// no send-log row, and returns how many went. That one rule is the whole of
// the table's retention: it removes the lines of rows the send-log sweep has
// deleted, and the lines of messages that never had a row - inbound relay
// mail, the bounces Postfix generates. cutoff leaves alone the lines of a
// message still being received, whose row the journal milter has yet to write.
func (s *Store) PruneSendLogLines(cutoff time.Time) (int64, error) {
	res, err := s.db.Exec(
		`DELETE FROM send_log_lines
		 WHERE logged_at < ?
		   AND NOT EXISTS (SELECT 1 FROM send_log WHERE send_log.queue_id = send_log_lines.queue_id)`,
		cutoff.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("prune send_log_lines: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
