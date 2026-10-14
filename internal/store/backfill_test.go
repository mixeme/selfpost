package store

import (
	"strings"
	"testing"
)

// backfill0008 runs the UPDATE statements of migration 0008 again: the store
// is already at its head, so the rows a test prepares get the backfill as
// they would have on an upgrade.
func backfill0008(t *testing.T, st *Store) {
	t.Helper()
	raw, err := migrationsFS.ReadFile("migrations/0008_tenant_boundary.sql")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, l := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "--") {
			lines = append(lines, l)
		}
	}
	for _, stmt := range strings.Split(strings.Join(lines, "\n"), ";") {
		if strings.HasPrefix(strings.TrimSpace(stmt), "UPDATE") {
			if _, err := st.db.Exec(stmt); err != nil {
				t.Fatalf("%s: %v", stmt, err)
			}
		}
	}
}

// The backfill follows the rule new rows follow - the application's domain,
// then the domain of the row's name - and gives a row only to a domain that
// already existed when it was written (review of audit stage 2).
func TestBackfillOf0008(t *testing.T) {
	st := openTestStore(t)
	d := addTestDomain(t, st, "a.example")
	if _, err := st.AddApplication(d.ID, "shop", AddressModeWildcard, nil); err != nil {
		t.Fatal(err)
	}
	in, err := st.AddInboundDomain("in.example")
	if err != nil {
		t.Fatal(err)
	}
	// The domain, its application and the inbound domain were created on 1 Oct.
	for _, q := range []string{
		`UPDATE domains SET created_at = '2026-10-01T00:00:00Z'`,
		`UPDATE applications SET created_at = '2026-10-01T00:00:00Z'`,
		`UPDATE inbound_domains SET created_at = '2026-10-01T00:00:00Z'`,
	} {
		if _, err := st.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rows := []struct{ queue, domain, login, at string }{
		{"BEFORE", "a.example", "shop", "2026-09-01T00:00:00Z"},  // a predecessor's: nobody's
		{"AFTER", "a.example", "shop", "2026-10-02T00:00:00Z"},   // the application's
		{"NULLSENDER", "", "shop", "2026-10-02T00:00:00Z"},       // no domain of its own: the application's
		{"GONEAPP", "a.example", "gone", "2026-10-02T00:00:00Z"}, // the application is gone: by name
		{"STRANGER", "b.example", "gone", "2026-10-02T00:00:00Z"},
	}
	for _, r := range rows {
		if _, err := st.db.Exec(`INSERT INTO send_log (queue_id, domain, app_login, from_addr, to_addr, subject, status, created_at, updated_at)
			VALUES (?, ?, ?, '', 'x@example.net', '', 'sent', ?, ?)`, r.queue, r.domain, r.login, r.at, r.at); err != nil {
			t.Fatal(err)
		}
	}
	for _, at := range []string{"2026-09-01T00:00:00Z", "2026-10-02T00:00:00Z"} {
		if _, err := st.db.Exec(`INSERT INTO inbound_spam_log (inbound_domain, client_ip, from_addr, to_addr, subject, decision, engine, detail, created_at)
			VALUES ('in.example', '203.0.113.7', 's@x', 'r@in.example', '', 'accept', 'rspamd', '', ?)`, at); err != nil {
			t.Fatal(err)
		}
	}

	backfill0008(t, st)

	want := map[string]int64{"BEFORE": 0, "AFTER": d.ID, "NULLSENDER": d.ID, "GONEAPP": d.ID, "STRANGER": 0}
	for queue, id := range want {
		var got int64
		if err := st.db.QueryRow(`SELECT COALESCE(domain_id, 0) FROM send_log WHERE queue_id = ?`, queue).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != id {
			t.Errorf("%s: domain_id %d, want %d", queue, got, id)
		}
	}
	var before, after int64
	st.db.QueryRow(`SELECT COALESCE(inbound_domain_id, 0) FROM inbound_spam_log WHERE created_at = '2026-09-01T00:00:00Z'`).Scan(&before)
	st.db.QueryRow(`SELECT COALESCE(inbound_domain_id, 0) FROM inbound_spam_log WHERE created_at = '2026-10-02T00:00:00Z'`).Scan(&after)
	if before != 0 || after != in.ID {
		t.Errorf("inbound rows: before %d, after %d; want 0 and %d", before, after, in.ID)
	}
}
