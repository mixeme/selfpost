package store

import (
	"fmt"
	"testing"
	"time"
)

// Postfix's line about a refusal at MAIL FROM goes on the row of that client
// and sender refused about then; a queued row, a row of another client or
// sender, and a row from long ago are not it; a line already on a row goes on
// no other; and two refusals of one second take one line each, in the order
// they were written (security and technical reviews of stage 2b).
func TestAttachRefusalLine(t *testing.T) {
	st := openTestStore(t)
	now := time.Now().UTC()
	for _, e := range []SendLogEntry{
		{AppLogin: "l2app", From: "billing@example.com", Reason: ReasonLimit, ClientIP: "10.0.0.1"},
		{AppLogin: "l2app", From: "billing@example.com", Reason: ReasonLimit, ClientIP: "10.0.0.1"},
		{AppLogin: "other", From: "billing@example.com", Reason: ReasonClient, ClientIP: "10.0.0.2"},
	} {
		if err := st.InsertRejected(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.InsertQueued(SendLogEntry{QueueID: "Q1", AppLogin: "l2app", From: "billing@example.com", To: "x@example.net"}); err != nil {
		t.Fatal(err)
	}
	first := "Oct 14 12:00:00 mail postfix/smtpd[1]: NOQUEUE: milter-reject: MAIL from unknown[10.0.0.1]: 451 4.7.1 first"
	second := "Oct 14 12:00:00 mail postfix/smtpd[2]: NOQUEUE: milter-reject: MAIL from unknown[10.0.0.1]: 451 4.7.1 second"
	for _, line := range []string{first, second} {
		if ok, err := st.AttachRefusalLine("10.0.0.1", "Billing@example.com", now, line); err != nil || !ok {
			t.Fatalf("attach %q = %v, %v", line, ok, err)
		}
	}
	// Read again after a restart: no row takes it a second time.
	if ok, _ := st.AttachRefusalLine("10.0.0.1", "billing@example.com", now, first); ok {
		t.Fatal("a line already on a row took another")
	}
	if ok, _ := st.AttachRefusalLine("10.0.0.3", "billing@example.com", now, "a third"); ok {
		t.Fatal("a line of another client took a row")
	}
	if ok, _ := st.AttachRefusalLine("10.0.0.2", "billing@example.com", now.Add(-time.Hour), "an hour off"); ok {
		t.Fatal("a line an hour off took a row")
	}
	lines := map[string]string{}
	rows, _ := st.QuerySendLog(SendLogFilter{AllDomains: true}, 10, 0)
	for _, r := range rows {
		full, _ := st.GetSendLog(r.ID)
		if full.RefusalLine != "" {
			lines[fmt.Sprint(full.ID)] = full.RefusalLine
		}
	}
	if len(lines) != 2 {
		t.Fatalf("lines on rows: %v, want the first two rows", lines)
	}
	r1, _ := st.GetSendLog(rows[len(rows)-1].ID) // the oldest row
	if r1.RefusalLine != first {
		t.Fatalf("the first row has %q, want the first line", r1.RefusalLine)
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

// A From: refusal's row is written from the log only when the panel wrote
// none for that queue id about then; once, however often the line is read.
func TestRecoverRefusal(t *testing.T) {
	st := openTestStore(t)
	d := addTestDomain(t, st, "sender.example")
	if _, err := st.AddApplication(d.ID, "app1", AddressModeWildcard, nil); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// The panel wrote this one itself.
	if err := st.InsertRejected(SendLogEntry{QueueID: "WRITTEN", AppLogin: "app1", From: "a@sender.example", Reason: ReasonFrom}); err != nil {
		t.Fatal(err)
	}
	e := SendLogEntry{QueueID: "WRITTEN", AppLogin: "app1", From: "a@sender.example", To: "r@x", Domain: "sender.example", Reason: ReasonFrom, HeaderFrom: "x@y"}
	if wrote, err := st.RecoverRefusal(e, now); err != nil || wrote {
		t.Fatalf("a refusal the panel wrote was written again: %v, %v", wrote, err)
	}
	e.QueueID = "LOST"
	for i := 0; i < 2; i++ {
		wrote, err := st.RecoverRefusal(e, now)
		if err != nil || wrote != (i == 0) {
			t.Fatalf("recovery %d: wrote %v, %v", i, wrote, err)
		}
	}
	rows, _ := st.QuerySendLog(SendLogFilter{DomainIDs: []int64{d.ID}}, 10, 0)
	var found SendLogRow
	for _, r := range rows {
		if r.QueueID == "LOST" {
			found, _ = st.GetSendLog(r.ID)
		}
	}
	if !found.Recovered || found.Status != StatusRejected || found.Reason != ReasonFrom || found.HeaderFrom != "x@y" || found.DomainID != d.ID {
		t.Fatalf("the recovered row: %+v", found)
	}
}

// A recovered row whose login the tailer did not see is its envelope
// domain's, not nobody's (review of stage 2b).
func TestRecoverRefusalWithoutALogin(t *testing.T) {
	st := openTestStore(t)
	d := addTestDomain(t, st, "sender.example")
	e := SendLogEntry{QueueID: "NOLOGIN", From: "a@sender.example", To: "r@x", Domain: "sender.example", Reason: ReasonFrom}
	if wrote, err := st.RecoverRefusal(e, time.Now()); err != nil || !wrote {
		t.Fatalf("recover = %v, %v", wrote, err)
	}
	rows, _ := st.QuerySendLog(SendLogFilter{DomainIDs: []int64{d.ID}}, 10, 0)
	if len(rows) != 1 {
		t.Fatalf("the domain's log has %d rows, want the recovered one", len(rows))
	}
}

// A refused row carries the queue id it was given; a delivery line of a later
// message with that id does not touch it.
func TestUpdateStatusLeavesARefusalAlone(t *testing.T) {
	st := openTestStore(t)
	if err := st.InsertRejected(SendLogEntry{QueueID: "REUSED", To: "a@example.net", Reason: ReasonFrom}); err != nil {
		t.Fatal(err)
	}
	if n, err := st.UpdateStatus("REUSED", "a@example.net", StatusSent); err != nil || n != 0 {
		t.Fatalf("UpdateStatus = %d, %v; want 0 rows", n, err)
	}
}
