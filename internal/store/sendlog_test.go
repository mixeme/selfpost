package store

import (
	"sort"
	"strings"
	"testing"
	"time"
)

// readSendLog returns every send_log row ordered by id. The monitoring UI has
// no equivalent read query, so tests read the table directly.
type sendLogRow struct {
	QueueID  string
	Domain   string
	AppLogin string
	From     string
	To       string
	Subject  string
	Status   string
}

func readSendLog(t *testing.T, s *Store) []sendLogRow {
	t.Helper()
	rows, err := s.db.Query(
		`SELECT queue_id, domain, app_login, from_addr, to_addr, subject, status
		 FROM send_log ORDER BY id`)
	if err != nil {
		t.Fatalf("query send_log: %v", err)
	}
	defer rows.Close()
	var out []sendLogRow
	for rows.Next() {
		var r sendLogRow
		if err := rows.Scan(&r.QueueID, &r.Domain, &r.AppLogin, &r.From, &r.To, &r.Subject, &r.Status); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func TestInsertQueuedAndUpdateStatus(t *testing.T) {
	st := openTestStore(t)

	// Two recipients on the same queue-id → two independent rows (architecture.md
	// § Persistence).
	for _, to := range []string{"a@example.net", "b@example.net"} {
		if err := st.InsertQueued(SendLogEntry{
			QueueID:  "ABC123",
			Domain:   "example.com",
			AppLogin: "app1",
			From:     "noreply@example.com",
			To:       to,
			Subject:  "Hello",
		}); err != nil {
			t.Fatalf("InsertQueued: %v", err)
		}
	}

	rows := readSendLog(t, st)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.Status != StatusQueued {
			t.Fatalf("new row should be queued, got %q", r.Status)
		}
	}

	// One recipient goes to sent; the other stays queued.
	n, err := st.UpdateStatus("ABC123", "a@example.net", StatusSent)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 row updated, got %d", n)
	}

	rows = readSendLog(t, st)
	if rows[0].Status != StatusSent || rows[1].Status != StatusQueued {
		t.Fatalf("unexpected statuses: %+v", rows)
	}
}

func TestUpdateStatusRecipientCaseInsensitive(t *testing.T) {
	st := openTestStore(t)
	if err := st.InsertQueued(SendLogEntry{QueueID: "Q1", To: "User@Example.NET"}); err != nil {
		t.Fatalf("InsertQueued: %v", err)
	}
	// mail.log may report a differently-cased recipient; matching must still hit.
	n, err := st.UpdateStatus("Q1", "user@example.net", StatusBounced)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if n != 1 {
		t.Fatalf("case-insensitive match failed, updated %d rows", n)
	}
}

// rowID is the id of the newest send-log row of a queue id.
func rowID(t *testing.T, st *Store, queueID string) int64 {
	t.Helper()
	var id int64
	if err := st.db.QueryRow(`SELECT MAX(id) FROM send_log WHERE queue_id = ?`, queueID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// The sweep closes only what is still queued, and says it was the sweep: a
// status the log wrote stands, whatever the sweep believed when it listed the
// row.
func TestCloseUnreportedClosesOnlyQueuedRows(t *testing.T) {
	st := openTestStore(t)
	for _, e := range []SendLogEntry{{QueueID: "Q1", To: "Lost@example.net"}, {QueueID: "Q2", To: "done@example.net"}} {
		if err := st.InsertQueued(e); err != nil {
			t.Fatalf("InsertQueued: %v", err)
		}
	}
	if _, err := st.UpdateStatus("Q2", "done@example.net", StatusSent); err != nil {
		t.Fatal(err)
	}

	if n, err := st.CloseUnreported(rowID(t, st, "Q1")); err != nil || n != 1 {
		t.Fatalf("CloseUnreported of a queued row = %d, %v; want 1", n, err)
	}
	if n, err := st.CloseUnreported(rowID(t, st, "Q2")); err != nil || n != 0 {
		t.Fatalf("CloseUnreported of a sent row = %d, %v; want 0", n, err)
	}

	var status, reason string
	for _, c := range []struct{ id, status, reason string }{
		{"Q1", StatusBounced, ReasonReconcile},
		{"Q2", StatusSent, ""},
	} {
		if err := st.db.QueryRow(`SELECT status, reason FROM send_log WHERE queue_id = ?`, c.id).Scan(&status, &reason); err != nil {
			t.Fatal(err)
		}
		if status != c.status || reason != c.reason {
			t.Errorf("%s: status %q reason %q, want %q %q", c.id, status, reason, c.status, c.reason)
		}
	}
}

// A delivery line about a row the sweep closed proves the sweep guessed what
// the log could have said: the log's status wins, the row is marked
// reconcile-contradicted, and the day counts one contradiction - once,
// however often the line is read again.
func TestUpdateStatusCountsAContradictedSweep(t *testing.T) {
	st := openTestStore(t)
	if err := st.InsertQueued(SendLogEntry{QueueID: "Q1", To: "a@example.net"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CloseUnreported(rowID(t, st, "Q1")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if n, err := st.UpdateStatus("Q1", "A@example.net", StatusSent); err != nil || n != 1 {
			t.Fatalf("UpdateStatus = %d, %v", n, err)
		}
	}
	var status, reason string
	if err := st.db.QueryRow(`SELECT status, reason FROM send_log WHERE queue_id = 'Q1'`).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != StatusSent || reason != ReasonReconcileContradicted {
		t.Fatalf("status %q reason %q, want sent and %s", status, reason, ReasonReconcileContradicted)
	}
	days, err := st.DeliveryLogDays()
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 || days[0].Contradicted != 1 {
		t.Fatalf("days = %+v, want one day with one contradiction", days)
	}
}

// Postfix reuses a short queue id once its message has left the queue; a
// line about the message of today rewrites today's row, not the months-old
// one of the same id and recipient.
func TestUpdateStatusRewritesOnlyTheNewestRow(t *testing.T) {
	st := openTestStore(t)
	for i := 0; i < 2; i++ {
		if err := st.InsertQueued(SendLogEntry{QueueID: "REUSED", To: "a@example.net"}); err != nil {
			t.Fatal(err)
		}
	}
	old := rowID(t, st, "REUSED") - 1
	if _, err := st.db.Exec(`UPDATE send_log SET status = ? WHERE id = ?`, StatusSent, old); err != nil {
		t.Fatal(err)
	}
	if n, err := st.UpdateStatus("REUSED", "a@example.net", StatusBounced); err != nil || n != 1 {
		t.Fatalf("UpdateStatus = %d, %v; want 1 row", n, err)
	}
	var status string
	if err := st.db.QueryRow(`SELECT status FROM send_log WHERE id = ?`, old).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != StatusSent {
		t.Fatalf("the old row became %q, want it left sent", status)
	}
}

// The reconcile sweep asks for the rows old enough that Postfix should have
// reported on them by now. A message accepted moments ago is simply in flight,
// and one the milter refused never reached the queue at all, so neither is the
// sweep's business.
func TestListQueuedOlderThan(t *testing.T) {
	st := openTestStore(t)

	for _, e := range []SendLogEntry{
		{QueueID: "OLD1", To: "stale@example.net"},
		{QueueID: "NEW1", To: "fresh@example.net"},
	} {
		if err := st.InsertQueued(e); err != nil {
			t.Fatalf("InsertQueued: %v", err)
		}
	}
	// A row the milter refused: no queue-id, and a status the sweep never sees.
	if err := st.InsertRejected(SendLogEntry{To: "refused@example.net"}); err != nil {
		t.Fatalf("InsertRejected: %v", err)
	}
	// A row that has already been delivered, aged the same as the stale one.
	if err := st.InsertQueued(SendLogEntry{QueueID: "DONE1", To: "done@example.net"}); err != nil {
		t.Fatalf("InsertQueued: %v", err)
	}
	if _, err := st.UpdateStatus("DONE1", "done@example.net", StatusSent); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	cutoff := time.Now().UTC().Add(-2 * time.Minute)
	backdate(t, st, "OLD1", cutoff.Add(-time.Hour))
	backdate(t, st, "DONE1", cutoff.Add(-time.Hour))

	got, err := st.ListQueuedOlderThan(cutoff)
	if err != nil {
		t.Fatalf("ListQueuedOlderThan: %v", err)
	}
	if len(got) != 1 || got[0] != (QueuedDelivery{ID: got[0].ID, QueueID: "OLD1", To: "stale@example.net"}) {
		t.Fatalf("got %+v, want only the stale queued row", got)
	}
}

// backdate rewrites a row's acceptance time, so a test can age it past a cutoff
// without waiting.
func backdate(t *testing.T, s *Store, queueID string, at time.Time) {
	t.Helper()
	if _, err := s.db.Exec(
		`UPDATE send_log SET created_at = ? WHERE queue_id = ?`,
		at.UTC().Format(time.RFC3339), queueID,
	); err != nil {
		t.Fatalf("backdate %s: %v", queueID, err)
	}
}

func TestUpdateStatusNoMatch(t *testing.T) {
	st := openTestStore(t)
	if err := st.InsertQueued(SendLogEntry{QueueID: "Q1", To: "a@example.net"}); err != nil {
		t.Fatalf("InsertQueued: %v", err)
	}
	// A queue-id/recipient the milter never recorded must be a no-op, not an error.
	n, err := st.UpdateStatus("Q1", "unknown@example.net", StatusSent)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if n != 0 {
		t.Fatalf("want 0 rows updated, got %d", n)
	}
}

// The journal is read by principals who are only entitled to part of it, so
// the scope is part of the query rather than something the caller remembers to
// apply afterwards. A filter that states no scope is a caller that has not
// decided who is asking, and the safe answer to that is nothing.
func TestSendLogScopeIsMandatory(t *testing.T) {
	st := openTestStore(t)
	ids := map[string]int64{}
	for _, domain := range []string{"first.example.ru", "second.example.ru"} {
		d := addTestDomain(t, st, domain)
		ids[domain] = d.ID
		if _, err := st.AddApplication(d.ID, "app-"+domain, AddressModeWildcard, nil); err != nil {
			t.Fatal(err)
		}
		if err := st.InsertQueued(SendLogEntry{
			QueueID: "Q-" + domain, Domain: domain, AppLogin: "app-" + domain,
			From: "noreply@" + domain, To: "public@example.net", Subject: domain,
		}); err != nil {
			t.Fatalf("InsertQueued: %v", err)
		}
	}

	for name, tc := range map[string]struct {
		filter SendLogFilter
		want   []string
	}{
		"no scope":           {SendLogFilter{}, nil},
		"empty scope":        {SendLogFilter{DomainIDs: []int64{}}, nil},
		"all domains":        {SendLogFilter{AllDomains: true}, []string{"first.example.ru", "second.example.ru"}},
		"one domain":         {SendLogFilter{DomainIDs: []int64{ids["first.example.ru"]}}, []string{"first.example.ru"}},
		"two domains":        {SendLogFilter{DomainIDs: []int64{ids["first.example.ru"], ids["second.example.ru"]}}, []string{"first.example.ru", "second.example.ru"}},
		"unknown domain":     {SendLogFilter{DomainIDs: []int64{9999}}, nil},
		"filter within":      {SendLogFilter{Domain: "first.example.ru", DomainIDs: []int64{ids["first.example.ru"], ids["second.example.ru"]}}, []string{"first.example.ru"}},
		"filter outside":     {SendLogFilter{Domain: "second.example.ru", DomainIDs: []int64{ids["first.example.ru"]}}, nil},
		"app filter outside": {SendLogFilter{AppLogin: "app-second.example.ru", DomainIDs: []int64{ids["first.example.ru"]}}, nil},
	} {
		rows, err := st.QuerySendLog(tc.filter, 50, 0)
		if err != nil {
			t.Fatalf("%s: QuerySendLog: %v", name, err)
		}
		var got []string
		for _, r := range rows {
			got = append(got, r.Domain)
		}
		// Which rows came back is the question here; the page's own order is
		// newest-first and is tested where it matters.
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s: rows for %v, want %v", name, got, tc.want)
		}
		// The count drives pagination, so it has to agree with the page or the
		// UI advertises pages of rows the reader is not allowed to see.
		n, err := st.CountSendLog(tc.filter)
		if err != nil {
			t.Fatalf("%s: CountSendLog: %v", name, err)
		}
		if int(n) != len(tc.want) {
			t.Errorf("%s: count %d, want %d", name, n, len(tc.want))
		}
	}
}

func TestDeleteSendLogBefore(t *testing.T) {
	st := openTestStore(t)

	// Insert one row, then backdate it beyond the retention window by rewriting
	// created_at directly (InsertQueued always stamps "now").
	if err := st.InsertQueued(SendLogEntry{QueueID: "OLD", To: "a@example.net"}); err != nil {
		t.Fatalf("InsertQueued: %v", err)
	}
	old := time.Now().UTC().AddDate(0, 0, -100).Format(time.RFC3339)
	if _, err := st.db.Exec(`UPDATE send_log SET created_at = ? WHERE queue_id = 'OLD'`, old); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if err := st.InsertQueued(SendLogEntry{QueueID: "NEW", To: "b@example.net"}); err != nil {
		t.Fatalf("InsertQueued: %v", err)
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -90)
	n, err := st.DeleteSendLogBefore(cutoff)
	if err != nil {
		t.Fatalf("DeleteSendLogBefore: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 row pruned, got %d", n)
	}
	rows := readSendLog(t, st)
	if len(rows) != 1 || rows[0].QueueID != "NEW" {
		t.Fatalf("retention kept wrong rows: %+v", rows)
	}
}

// A domain deleted and added again under its old name starts with an empty
// log for whoever it is assigned to: its earlier rows belong to no domain and
// only the global role reads them (audit 2026-10-12, L12). Statistics and the
// level-2 count follow the same rule.
func TestADomainAddedAgainStartsWithAnEmptyLog(t *testing.T) {
	st := openTestStore(t)
	old := addTestDomain(t, st, "shop.example")
	if _, err := st.AddApplication(old.ID, "shop", AddressModeWildcard, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertQueued(SendLogEntry{QueueID: "OLD", Domain: "shop.example", AppLogin: "shop", To: "a@example.net"}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteDomain(old.ID); err != nil {
		t.Fatal(err)
	}
	again := addTestDomain(t, st, "shop.example")
	if _, err := st.AddApplication(again.ID, "shop", AddressModeWildcard, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertQueued(SendLogEntry{QueueID: "NEW", Domain: "shop.example", AppLogin: "shop", To: "a@example.net"}); err != nil {
		t.Fatal(err)
	}

	rows, err := st.QuerySendLog(SendLogFilter{DomainIDs: []int64{again.ID}}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].QueueID != "NEW" || rows[0].DomainID != again.ID {
		t.Fatalf("the new domain's log = %+v, want its own row alone", rows)
	}
	all, _ := st.QuerySendLog(SendLogFilter{AllDomains: true}, 10, 0)
	if len(all) != 2 {
		t.Fatalf("the global log has %d rows, want both", len(all))
	}
	if n, err := st.CountMessages(RateLimitScopeDomain, "shop.example", time.Now().Add(-time.Hour)); err != nil || n != 1 {
		t.Fatalf("level-2 count = %d, %v; want the new domain's one message", n, err)
	}
	if stats, err := st.DomainSendStats("shop.example", 90, time.Now().Add(-time.Hour)); err != nil || stats.Total != 1 {
		t.Fatalf("statistics = %+v, %v; want one message", stats, err)
	}
}

// A deleted application's login taken again in another domain starts with
// none of the old one's numbers (review of audit stage 2).
func TestALoginTakenAgainStartsWithNoNumbers(t *testing.T) {
	st := openTestStore(t)
	a := addTestDomain(t, st, "a.example")
	b := addTestDomain(t, st, "b.example")
	old, err := st.AddApplication(a.ID, "news", AddressModeWildcard, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.InsertQueued(SendLogEntry{QueueID: "OLD", Domain: "a.example", AppLogin: "news", To: "x@example.net"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DeleteApplication(old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddApplication(b.ID, "news", AddressModeWildcard, nil); err != nil {
		t.Fatal(err)
	}
	if n, err := st.CountMessages(RateLimitScopeApp, "news", time.Now().Add(-time.Hour)); err != nil || n != 0 {
		t.Fatalf("level-2 count of the new news = %d, %v; want 0", n, err)
	}
	if stats, err := st.AppSendStats("news", 90, time.Now().Add(-time.Hour)); err != nil || stats.Total != 0 {
		t.Fatalf("statistics of the new news = %+v, %v; want none", stats, err)
	}
	if err := st.InsertQueued(SendLogEntry{QueueID: "NEW", Domain: "b.example", AppLogin: "news", To: "x@example.net"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.CountMessages(RateLimitScopeApp, "news", time.Now().Add(-time.Hour)); n != 1 {
		t.Fatalf("level-2 count of the new news = %d, want its own one", n)
	}
}
