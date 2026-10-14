package store

import (
	"testing"
	"time"
)

// Postfix's line about a refusal at MAIL FROM goes on the row of that sender
// and login refused about then, the newest without a line; a queued row, a
// row of another sender and a row from long ago are not it (audit
// 2026-10-12, stage 2b).
func TestAttachRefusalLine(t *testing.T) {
	st := openTestStore(t)
	now := time.Now().UTC()
	for _, e := range []SendLogEntry{
		{AppLogin: "l2app", From: "billing@example.com", Reason: ReasonLimit},
		{AppLogin: "l2app", From: "other@example.com", Reason: ReasonLimit},
	} {
		if err := st.InsertRejected(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.InsertQueued(SendLogEntry{QueueID: "Q1", AppLogin: "l2app", From: "billing@example.com", To: "x@example.net"}); err != nil {
		t.Fatal(err)
	}
	line := "Oct 14 12:00:00 mail postfix/smtpd[1]: NOQUEUE: milter-reject: MAIL from unknown[10.77.0.1]: 451 4.7.1 Service unavailable"
	if ok, err := st.AttachRefusalLine("l2app", "Billing@example.com", now, line); err != nil || !ok {
		t.Fatalf("attach = %v, %v", ok, err)
	}
	// The row has its line now; a second line finds no row without one.
	if ok, _ := st.AttachRefusalLine("l2app", "billing@example.com", now, line); ok {
		t.Fatal("a second line took a row that has one")
	}
	if ok, _ := st.AttachRefusalLine("l2app", "other@example.com", now.Add(-time.Hour), line); ok {
		t.Fatal("a line an hour off took a row")
	}
	if ok, _ := st.AttachRefusalLine("someone", "other@example.com", now, line); ok {
		t.Fatal("a line of another login took a row")
	}
	rows, _ := st.QuerySendLog(SendLogFilter{AllDomains: true}, 10, 0)
	got := 0
	for _, r := range rows {
		full, _ := st.GetSendLog(r.ID)
		if full.RefusalLine != "" {
			got++
			if full.From != "billing@example.com" || full.Status != StatusRejected {
				t.Errorf("the line went to %+v", full)
			}
		}
	}
	if got != 1 {
		t.Fatalf("%d rows have a line, want 1", got)
	}
}

// A refused message's lines are those of a few minutes around the refusal:
// its queue id was freed at once, and a later message with the same id lends
// it nothing.
func TestSendLogLinesOfARefusalAreItsOwn(t *testing.T) {
	st := openTestStore(t)
	if err := st.InsertRejected(SendLogEntry{QueueID: "7BCAF3C75DA0", AppLogin: "shop", From: "shop@shop.example", Reason: ReasonFrom}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := st.InsertSendLogLines([]SendLogLine{
		{QueueID: "7BCAF3C75DA0", At: now.Add(-time.Second), Line: "its own refusal"},
		{QueueID: "7BCAF3C75DA0", At: now.Add(20 * time.Minute), Line: "a later message with the id"},
	}, false); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.QuerySendLog(SendLogFilter{AllDomains: true}, 1, 0)
	lines, err := st.SendLogLines(rows[0], 5*24*time.Hour, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0].Line != "its own refusal" {
		t.Fatalf("lines = %+v, want its own alone", lines)
	}
}
