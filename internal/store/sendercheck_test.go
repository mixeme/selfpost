package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSenderCheckStats(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "selfpost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	day := func(n int) time.Time { return now.AddDate(0, 0, -n) }

	// Nothing counted yet: zeros, and no first day.
	got, err := st.SenderCheckStats(now)
	if err != nil {
		t.Fatal(err)
	}
	if got != (SenderCheckStats{}) {
		t.Fatalf("empty statistics = %+v", got)
	}

	// The first day is recorded once.
	if err := st.StartSenderCheckStats(day(40)); err != nil {
		t.Fatal(err)
	}
	if err := st.StartSenderCheckStats(now); err != nil {
		t.Fatal(err)
	}

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.AddSenderCheckDown(now, 3*time.Second))
	must(st.AddSenderCheckDown(now, 1500*time.Millisecond)) // whole seconds: one
	must(st.AddSenderCheckDown(now, 400*time.Millisecond))  // nothing
	must(st.AddSenderCheckDeferred(now.Add(-time.Hour), 2, ""))
	must(st.AddSenderCheckDown(day(10), 30*time.Second))
	must(st.AddSenderCheckDeferred(day(10), 1, ""))
	must(st.AddSenderCheckDown(day(29), time.Minute))
	must(st.AddSenderCheckDown(day(30), time.Hour)) // the 31st day back: the year's only
	must(st.AddSenderCheckDeferred(day(200), 5, ""))
	must(st.AddSenderCheckDown(day(365), 24*time.Hour)) // a year and a day: nobody's

	got, err = st.SenderCheckStats(now)
	if err != nil {
		t.Fatal(err)
	}
	want := SenderCheckStats{
		Today:      SenderCheckTotals{Down: 4 * time.Second, Deferred: 2},
		Month:      SenderCheckTotals{Down: 94 * time.Second, Deferred: 3},
		Year:       SenderCheckTotals{Down: 94*time.Second + time.Hour, Deferred: 8},
		Since:      time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
		DeferredAt: now.Add(-time.Hour),
	}
	if got != want {
		t.Fatalf("statistics\n got %+v\nwant %+v", got, want)
	}

	// The last deferral is the latest one seen, not the last one written: a
	// log read again after an unclean stop does not move it back.
	if got.DeferredAt != now.Add(-time.Hour) {
		t.Fatalf("an older deferral moved the mark to %s", got.DeferredAt)
	}

	// A year on, the old days are gone.
	must(st.PruneSenderCheckDays(now.AddDate(1, 0, 0)))
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM sender_check_days`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d days left after a year's prune (%v), want the newest alone", n, err)
	}
}

// A refused row keeps why it was refused and, for its From: line, what the
// line said; an accepted row has neither.
func TestRefusedRowKeepsItsReason(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "selfpost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.InsertRejected(SendLogEntry{Domain: "shop.example.org", AppLogin: "shop", From: "shop@shop.example.org",
		To: "k.berg@web.de", Subject: "s", Reason: ReasonFrom, HeaderFrom: "director@example.org"}); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertQueued(SendLogEntry{QueueID: "4XcB7k2Jm9z1", Domain: "shop.example.org", AppLogin: "shop",
		From: "shop@shop.example.org", To: "k.berg@web.de"}); err != nil {
		t.Fatal(err)
	}
	rows, err := st.QuerySendLog(SendLogFilter{AllDomains: true}, 10, 0)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %d, %v", len(rows), err)
	}
	queued, refused := rows[0], rows[1]
	if refused.Status != StatusRejected || refused.Reason != ReasonFrom || refused.HeaderFrom != "director@example.org" || refused.To != "k.berg@web.de" {
		t.Errorf("refused row = %+v", refused)
	}
	if queued.Reason != "" || queued.HeaderFrom != "" {
		t.Errorf("queued row carries a refusal: %+v", queued)
	}
	one, err := st.GetSendLog(refused.ID)
	if err != nil || one.Reason != ReasonFrom || one.HeaderFrom != "director@example.org" {
		t.Errorf("GetSendLog = %+v, %v", one, err)
	}
}

// The place in the log is saved with the count, and the moment of the last
// deferral does not move back when a log is read again.
func TestSenderCheckDeferredSavesItsPlace(t *testing.T) {
	st := openTestStore(t)
	later := time.Date(2026, 10, 14, 12, 0, 1, 0, time.UTC)
	if err := st.AddSenderCheckDeferred(later, 1, "abc:1.2@100"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSenderCheckDeferred(later.Add(-time.Hour), 1, "abc:1.2@200"); err != nil {
		t.Fatal(err)
	}
	if p, err := st.SenderCheckCountedPlace(); err != nil || p != "abc:1.2@200" {
		t.Fatalf("place = %q, %v", p, err)
	}
	if at, err := st.SenderCheckDeferredAt(); err != nil || !at.Equal(later) {
		t.Fatalf("last deferral = %s, %v; want %s", at, err, later)
	}
}
