package store

import (
	"fmt"
	"time"
	"unicode/utf8"
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

// cutSendLogLine cuts a line to SendLogLineMax bytes, and not through the
// middle of a character.
func cutSendLogLine(line string) string {
	if len(line) <= SendLogLineMax {
		return line
	}
	end := SendLogLineMax
	for end > 0 && !utf8.RuneStart(line[end]) {
		end--
	}
	return line[:end]
}

// InsertSendLogLines stores lines in one transaction and returns how many it
// wrote. The tailer calls it with what it read in one pass over the log,
// whether or not send-log rows exist for the queue ids yet: a commit per line
// would be a disk sync per line, on the one connection the journal milter
// writes through. SQLite 3.53.4 as modernc builds it keeps synchronous at FULL
// (SQLITE_DEFAULT_WAL_SYNCHRONOUS 2), and in WAL mode FULL syncs the WAL at
// every commit (https://sqlite.org/wal.html § 2.3, pragma.html#pragma_synchronous).
// The first statement stepped is an INSERT, a write (see UpdateStatus):
// tx.Prepare leaves no transaction open (prepare.c sqlite3InitOne() commits
// the read it opens to load the schema).
//
// With once set, a line is written only if the same line is not there already
// under its queue id. That is for the lines the tailer reads again after an
// unclean stop; a line carries its time and its process id, so two that are
// equal are one line read twice.
func (s *Store) InsertSendLogLines(lines []SendLogLine, once bool) (int, error) {
	if len(lines) == 0 {
		return 0, nil
	}
	query := `INSERT INTO send_log_lines (queue_id, logged_at, line) VALUES (?1, ?2, ?3)`
	if once {
		query = `INSERT INTO send_log_lines (queue_id, logged_at, line)
			 SELECT ?1, ?2, ?3
			 WHERE NOT EXISTS (SELECT 1 FROM send_log_lines WHERE queue_id = ?1 AND line = ?3)`
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("insert send_log_lines: %w", err)
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(query)
	if err != nil {
		return 0, fmt.Errorf("insert send_log_lines: %w", err)
	}
	defer stmt.Close()
	wrote := 0
	for _, l := range lines {
		res, err := stmt.Exec(l.QueueID, l.At.UTC().Format(time.RFC3339), cutSendLogLine(l.Line))
		if err != nil {
			return 0, fmt.Errorf("insert send_log_lines: %w", err)
		}
		n, _ := res.RowsAffected()
		wrote += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("insert send_log_lines: %w", err)
	}
	return wrote, nil
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
//
// A refused message - one refused for its From: line has a queue id, given
// before the headers were judged - is over in a moment, and Postfix frees the
// id at once: its lines are those of a few minutes around the refusal, so
// that the next message to get the id lends it none of its own.
func (s *Store) SendLogLines(row SendLogRow, lifetime time.Duration, n int) ([]SendLogLine, error) {
	if row.QueueID == "" || n <= 0 {
		return nil, nil
	}
	before := sendLogLineBefore
	if row.Status == StatusRejected {
		before, lifetime = refusedLinesBefore, refusedLinesAfter
	}
	rows, err := s.db.Query(
		`SELECT logged_at, line FROM (
			SELECT id, logged_at, line FROM send_log_lines
			WHERE queue_id = ? AND logged_at >= ? AND logged_at <= ?
			ORDER BY id DESC LIMIT ?)
		 ORDER BY id`,
		row.QueueID,
		row.CreatedAt.Add(-before).UTC().Format(time.RFC3339),
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
// deleted, and the lines of messages that never had a row - the bounces
// Postfix generates. cutoff leaves alone the lines of a message still being
// received, whose row the journal milter has yet to write.
//
// The send log is asked once per queue id, not once per line: the ids are read
// off the index, and a message is several lines. It is the longest write the
// panel makes: measured on the test server, 2026-10-15 (2 cores; 1.37
// million lines, 120 days at 1 900 messages a day, timed through this
// package), 1.9 s to delete 342 000 lines after a 30-day gap, 0.4 s with
// nothing to delete - inside busy_timeout's 5 s (Open), which dmarc-ingest's
// writes wait through: the longest waited 1.8 s (assumptions D-S7).
func (s *Store) PruneSendLogLines(cutoff time.Time) (int64, error) {
	res, err := s.db.Exec(
		`DELETE FROM send_log_lines
		 WHERE logged_at < ?
		   AND queue_id IN (
			SELECT q.queue_id FROM (SELECT DISTINCT queue_id FROM send_log_lines) AS q
			WHERE NOT EXISTS (SELECT 1 FROM send_log WHERE send_log.queue_id = q.queue_id))`,
		cutoff.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("prune send_log_lines: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// The lines of a refused message: from its connection - a slow client may
// take minutes to reach the end of its headers - to just after the refusal.
const (
	refusedLinesBefore = 10 * time.Minute
	refusedLinesAfter  = time.Minute
)
