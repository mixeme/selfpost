package store

import (
	"strings"
	"testing"
	"time"
)

func sendLogLineTexts(lines []SendLogLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Line
	}
	return out
}

func countSendLogLines(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM send_log_lines`).Scan(&n); err != nil {
		t.Fatalf("count send_log_lines: %v", err)
	}
	return n
}

// A row's page gets the lines of its queue id, oldest first, and not the lines
// of another message.
func TestSendLogLinesOfOneMessage(t *testing.T) {
	s := openTestStore(t)
	at := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	for i, l := range []SendLogLine{
		{QueueID: "41E862C00D9E", At: at, Line: "client=app"},
		{QueueID: "5F00AA", At: at, Line: "someone else's"},
		{QueueID: "41E862C00D9E", At: at.Add(time.Second), Line: "status=sent"},
		{QueueID: "41E862C00D9E", At: at.Add(2 * time.Second), Line: "removed"},
	} {
		if err := s.InsertSendLogLine(l); err != nil {
			t.Fatalf("insert line %d: %v", i, err)
		}
	}
	row := SendLogRow{QueueID: "41E862C00D9E", CreatedAt: at}

	lines, err := s.SendLogLines(row, 6*24*time.Hour, 200)
	if err != nil {
		t.Fatalf("SendLogLines: %v", err)
	}
	if got := strings.Join(sendLogLineTexts(lines), "|"); got != "client=app|status=sent|removed" {
		t.Errorf("lines = %q", got)
	}
	if len(lines) > 0 && !lines[0].At.Equal(at) {
		t.Errorf("first line dated %v, want %v", lines[0].At, at)
	}

	// More than the page shows: the latest are kept, still oldest first.
	lines, err = s.SendLogLines(row, 6*24*time.Hour, 2)
	if err != nil {
		t.Fatalf("SendLogLines: %v", err)
	}
	if got := strings.Join(sendLogLineTexts(lines), "|"); got != "status=sent|removed" {
		t.Errorf("latest two = %q", got)
	}

	// A row the milter refused has no queue id and so no lines - not every line.
	lines, err = s.SendLogLines(SendLogRow{CreatedAt: at}, 6*24*time.Hour, 200)
	if err != nil || len(lines) != 0 {
		t.Errorf("lines of a row without a queue id = %v, %v", lines, err)
	}
}

// Postfix reuses a queue id once the message has left the queue. The lines of
// a later message under the same id are not the earlier row's, nor the other
// way round.
func TestSendLogLinesBoundedByTheRowsLifetime(t *testing.T) {
	s := openTestStore(t)
	first := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	second := first.AddDate(0, 0, 30)
	lifetime := 6 * 24 * time.Hour
	for _, l := range []SendLogLine{
		{QueueID: "ABC123", At: first.Add(-30 * time.Minute), Line: "first: client"},
		{QueueID: "ABC123", At: first.Add(5 * 24 * time.Hour), Line: "first: expired"},
		{QueueID: "ABC123", At: second.Add(-2 * time.Hour), Line: "between the two"},
		{QueueID: "ABC123", At: second, Line: "second: sent"},
	} {
		if err := s.InsertSendLogLine(l); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	lines, err := s.SendLogLines(SendLogRow{QueueID: "ABC123", CreatedAt: first}, lifetime, 200)
	if err != nil {
		t.Fatalf("SendLogLines: %v", err)
	}
	if got := strings.Join(sendLogLineTexts(lines), "|"); got != "first: client|first: expired" {
		t.Errorf("the first message's lines = %q", got)
	}
	lines, err = s.SendLogLines(SendLogRow{QueueID: "ABC123", CreatedAt: second}, lifetime, 200)
	if err != nil {
		t.Fatalf("SendLogLines: %v", err)
	}
	if got := strings.Join(sendLogLineTexts(lines), "|"); got != "second: sent" {
		t.Errorf("the second message's lines = %q", got)
	}
}

// After an unclean stop the tailer reads some lines twice; the second reading
// writes nothing. The same text under another queue id is another line.
func TestInsertSendLogLineOnce(t *testing.T) {
	s := openTestStore(t)
	l := SendLogLine{QueueID: "ABC123", At: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC), Line: "Oct 12 09:00:00 mail postfix/qmgr[7]: ABC123: removed"}

	for i, want := range []bool{true, false} {
		wrote, err := s.InsertSendLogLineOnce(l)
		if err != nil || wrote != want {
			t.Errorf("reading %d: wrote = %v, %v; want %v", i+1, wrote, err, want)
		}
	}
	other := l
	other.QueueID = "DEF456"
	if wrote, err := s.InsertSendLogLineOnce(other); err != nil || !wrote {
		t.Errorf("another queue id: wrote = %v, %v", wrote, err)
	}
	if n := countSendLogLines(t, s); n != 2 {
		t.Errorf("%d lines stored, want 2", n)
	}
}

// The far end writes the reply a delivery line quotes; what is kept of a line
// has a ceiling, and a line read twice is still recognised past it.
func TestSendLogLineIsCut(t *testing.T) {
	s := openTestStore(t)
	at := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	l := SendLogLine{QueueID: "ABC123", At: at, Line: strings.Repeat("x", SendLogLineMax+500)}
	if err := s.InsertSendLogLine(l); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if wrote, err := s.InsertSendLogLineOnce(l); err != nil || wrote {
		t.Errorf("the same long line again: wrote = %v, %v", wrote, err)
	}
	lines, err := s.SendLogLines(SendLogRow{QueueID: "ABC123", CreatedAt: at}, time.Hour, 10)
	if err != nil || len(lines) != 1 {
		t.Fatalf("lines = %d, %v", len(lines), err)
	}
	if len(lines[0].Line) != SendLogLineMax {
		t.Errorf("kept %d bytes, want %d", len(lines[0].Line), SendLogLineMax)
	}
}

// The prune is the table's retention: lines go when no send-log row carries
// their queue id - the row was pruned, or there never was one - and a message
// still being received is left alone.
func TestPruneSendLogLines(t *testing.T) {
	s := openTestStore(t)
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	old := now.Add(-3 * time.Hour)

	if err := s.InsertQueued(SendLogEntry{QueueID: "KEPT01", Domain: "example.com", To: "a@example.net"}); err != nil {
		t.Fatalf("insert row: %v", err)
	}
	for _, l := range []SendLogLine{
		{QueueID: "KEPT01", At: old, Line: "has a row"},
		{QueueID: "INBOUND1", At: old, Line: "never had a row"},
		{QueueID: "ARRIVING", At: now.Add(-time.Minute), Line: "its row is not written yet"},
	} {
		if err := s.InsertSendLogLine(l); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	n, err := s.PruneSendLogLines(now.Add(-time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("prune removed %d, %v; want 1", n, err)
	}
	var left []string
	rows, err := s.db.Query(`SELECT queue_id FROM send_log_lines ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		left = append(left, id)
	}
	rows.Close()
	if got := strings.Join(left, " "); got != "KEPT01 ARRIVING" {
		t.Errorf("left after the prune: %q", got)
	}

	// The send-log sweep deletes the row; the next prune takes its lines.
	if _, err := s.DeleteSendLogBefore(time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("delete rows: %v", err)
	}
	if n, err := s.PruneSendLogLines(now.Add(-time.Hour)); err != nil || n != 1 {
		t.Errorf("prune after the row went removed %d, %v; want 1", n, err)
	}
}
