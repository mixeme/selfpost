package store

import (
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// 2.0 starts from one baseline: the 1.x chain is gone from the binary, not
// carried along (docs/schema-migrations.md).
func TestBaselineIsTheOnlyMigration(t *testing.T) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) == 0 || names[0] != "0001_init.sql" {
		t.Fatalf("migrations = %v, want the chain to start at 0001_init.sql", names)
	}
	baseline, _ := migrationsFS.ReadFile("migrations/0001_init.sql")
	for _, gone := range []string{"CREATE TABLE admin", "DROP TABLE", "ALTER TABLE", "dmarc_report_email", "allowed_ips    ", "domain_admin"} {
		if strings.Contains(string(baseline), gone) {
			t.Errorf("the baseline still contains %q — a leftover of the 1.x chain", gone)
		}
	}
}

// The schema a fresh data directory gets, table by table and column by
// column. A change here is a schema change: it ships as a new migration file
// and a row in docs/schema-migrations.md, never as an edit of the baseline
// after 2.0.0.
func TestBaselineSchema(t *testing.T) {
	st := openTestStore(t)
	want := map[string]string{
		"settings":              "key value",
		"users":                 "id username password_hash role email dmarc_default_mode dmarc_default_address all_domains all_inbound_domains created_at",
		"sessions":              "token_hash user_id created_at expires_at",
		"domains":               "id name dkim_selector dmarc_rua dmarc_rua_user_id created_at",
		"user_domains":          "user_id domain_id",
		"applications":          "id domain_id login address_mode auth_ip_restrict auth_allowed_ips created_at",
		"application_addresses": "id application_id address",
		"rate_limits":           "id scope ref_id max_messages window_seconds mode auto_multiplier auto_updated_at",
		"send_log":              "id queue_id domain app_login from_addr to_addr subject status created_at updated_at",
		"logtail_state":         "path fingerprint read_offset updated_at",
		"inbound_domains":       "id name recipient_mode created_at",
		"inbound_transports":    "inbound_domain_id host port tls_mode",
		"inbound_recipients":    "id inbound_domain_id address",
		"user_inbound_domains":  "user_id inbound_domain_id",
		"dmarc_reports":         "id domain reporter report_id period_begin period_end received_at contact_email policy_p policy_sp policy_pct policy_adkim policy_aspf pass_count fail_count recipient",
		"dmarc_report_records":  "id report_row_id source_ip count disposition spf_result dkim_result header_from",
	}
	rows, err := st.db.Query("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	for _, name := range tables {
		cols, ok := want[name]
		if !ok {
			t.Errorf("unexpected table %q", name)
			continue
		}
		if got := strings.Join(tableColumns(t, st, name), " "); got != cols {
			t.Errorf("table %s has columns\n  %s\nwant\n  %s", name, got, cols)
		}
		delete(want, name)
	}
	for name := range want {
		t.Errorf("table %q is missing", name)
	}

	var indexes []string
	rows, err = st.db.Query("SELECT name FROM sqlite_master WHERE type = 'index' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		indexes = append(indexes, name)
	}
	rows.Close()
	wantIdx := []string{
		"idx_dmarc_report_records_report", "idx_dmarc_reports_domain", "idx_dmarc_reports_received",
		"idx_send_log_app_login_created_at", "idx_send_log_created_at", "idx_send_log_domain", "idx_send_log_queue_id",
		"idx_sessions_expires_at",
	}
	sort.Strings(wantIdx)
	if strings.Join(indexes, " ") != strings.Join(wantIdx, " ") {
		t.Errorf("indexes = %v\nwant %v", indexes, wantIdx)
	}

	var version, appID int64
	if err := st.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Errorf("user_version = %d, %v; want 1", version, err)
	}
	if err := st.db.QueryRow("PRAGMA application_id").Scan(&appID); err != nil || appID != applicationID {
		t.Errorf("application_id = %#x, %v; want %#x", appID, err, applicationID)
	}
}

func tableColumns(t *testing.T, st *Store, table string) []string {
	t.Helper()
	rows, err := st.db.Query("SELECT name FROM pragma_table_info(?) ORDER BY cid", table)
	if err != nil {
		t.Fatalf("columns of %s: %v", table, err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, name)
	}
	return cols
}

// Reopening a 2.x database applies nothing twice and keeps its data.
func TestReopenKeepsTheDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.CreateGlobalUser("admin", "hash"); err != nil {
		t.Fatalf("CreateGlobalUser: %v", err)
	}
	st.Close()

	st, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st.Close()
	if ok, err := st.UserExists(); err != nil || !ok {
		t.Fatalf("after reopen: user exists = %v, %v", ok, err)
	}
}

// A 1.x data directory must be refused, loudly. Its user_version (10 at the
// last 1.x release) would otherwise read as "the baseline is already applied",
// and the panel would start on tables it does not know.
func TestOpenRefusesA1xDatabase(t *testing.T) {
	for _, version := range []string{"1", "10"} {
		path := filepath.Join(t.TempDir(), "old.db")
		db, err := sql.Open("sqlite", "file:"+path)
		if err != nil {
			t.Fatalf("create old db: %v", err)
		}
		for _, stmt := range []string{
			"CREATE TABLE admin (id INTEGER PRIMARY KEY CHECK (id = 1), username TEXT NOT NULL, password_hash TEXT NOT NULL, created_at TEXT NOT NULL)",
			"INSERT INTO admin VALUES (1, 'admin', 'hash', '2026-01-01T00:00:00Z')",
			"PRAGMA user_version = " + version,
		} {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("%s: %v", stmt, err)
			}
		}
		db.Close()

		st, err := Open(path)
		if err == nil {
			st.Close()
			t.Fatalf("a 1.x database at version %s was opened", version)
		}
		if !errors.Is(err, ErrForeignSchema) {
			t.Fatalf("version %s: error = %v, want ErrForeignSchema", version, err)
		}

		// Refusing must not have touched the file: the operator still has
		// their 1.x data to go back to.
		db, _ = sql.Open("sqlite", "file:"+path)
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM admin").Scan(&n); err != nil || n != 1 {
			t.Errorf("version %s: the refused database was altered (admin rows = %d, %v)", version, n, err)
		}
		var tables int
		_ = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'").Scan(&tables)
		if tables != 1 {
			t.Errorf("version %s: the refused database now has %d tables", version, tables)
		}
		db.Close()
	}
}

// A database written by a newer build is not guessed at either.
func TestOpenRefusesANewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := st.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if st, err := Open(path); err == nil {
		st.Close()
		t.Fatal("a database at schema version 99 was opened")
	}
}

// The checks the baseline puts on a user row, and what the new assignment
// table does when either side goes away.
func TestBaselineConstraints(t *testing.T) {
	st := openTestStore(t)
	insert := func(role, mode string, all int) error {
		_, err := st.db.Exec(
			"INSERT INTO users (username, password_hash, role, dmarc_default_mode, all_domains, created_at) VALUES (?, 'h', ?, ?, ?, 'now')",
			role+mode, role, mode, all)
		return err
	}
	if err := insert("global", "none", 0); err != nil {
		t.Fatalf("a plain global user: %v", err)
	}
	if err := insert("domain", "hosted", 1); err != nil {
		t.Fatalf("a domain user with All: %v", err)
	}
	if err := insert("domain_admin", "none", 0); err == nil {
		t.Error("the 1.x role name domain_admin was accepted")
	}
	if err := insert("global", "profile", 0); err == nil {
		t.Error("an unknown dmarc_default_mode was accepted")
	}
	if err := insert("domain", "custom", 2); err == nil {
		t.Error("all_domains = 2 was accepted")
	}

	u, err := st.GetUserByUsername("domainhosted")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if !u.AllDomains || u.AllInboundDomains || u.DMARCDefaultMode != DMARCDefaultHosted {
		t.Errorf("flags read back as %+v", u)
	}

	in, err := st.AddInboundDomain("in.example.com")
	if err != nil {
		t.Fatalf("AddInboundDomain: %v", err)
	}
	other, err := st.AddInboundDomain("other.example.com")
	if err != nil {
		t.Fatalf("AddInboundDomain: %v", err)
	}
	for _, id := range []int64{in.ID, other.ID} {
		if _, err := st.db.Exec("INSERT INTO user_inbound_domains (user_id, inbound_domain_id) VALUES (?, ?)", u.ID, id); err != nil {
			t.Fatalf("assign inbound domain: %v", err)
		}
	}
	if _, err := st.db.Exec("INSERT INTO user_inbound_domains (user_id, inbound_domain_id) VALUES (?, ?)", u.ID, in.ID+100); err == nil {
		t.Error("an assignment to a missing inbound domain was accepted")
	}
	u, _ = st.GetUser(u.ID)
	if len(u.InboundDomainIDs) != 2 || len(u.DomainIDs) != 0 {
		t.Fatalf("assignments = inbound %v, outbound %v; the two lists are separate", u.InboundDomainIDs, u.DomainIDs)
	}

	if err := st.DeleteInboundDomain(in.ID); err != nil {
		t.Fatalf("DeleteInboundDomain: %v", err)
	}
	u, _ = st.GetUser(u.ID)
	if len(u.InboundDomainIDs) != 1 || u.InboundDomainIDs[0] != other.ID {
		t.Errorf("after deleting an inbound domain its assignment stayed: %v", u.InboundDomainIDs)
	}
	if err := st.DeleteUser(u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	var n int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM user_inbound_domains").Scan(&n); err != nil || n != 0 {
		t.Errorf("after deleting the user %d inbound assignment(s) stayed (%v)", n, err)
	}
}
