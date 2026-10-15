package store

import (
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// 2.0 starts from one baseline: the 1.x chain is gone from the binary, not
// carried along (docs/schema-migrations.md): the chain starts at the
// baseline, and the baseline holds none of the 1.x schema.
func TestChainStartsAtTheBaseline(t *testing.T) {
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
		"users":                 "id username password_hash role email all_domains all_inbound_domains created_at notify",
		"sessions":              "token_hash user_id created_at expires_at",
		"domains":               "id name dkim_selector dmarc_rua created_at",
		"user_domains":          "user_id domain_id",
		"applications":          "id domain_id login address_mode auth_ip_restrict auth_allowed_ips created_at",
		"application_addresses": "id application_id address",
		"rate_limits":           "id scope ref_id max_messages window_seconds mode auto_multiplier auto_updated_at",
		"send_log":              "id queue_id domain app_login from_addr to_addr subject status created_at updated_at reason header_from domain_id refusal_line client_ip recovered",
		"logtail_state":         "path fingerprint read_offset updated_at",
		"inbound_domains":       "id name recipient_mode created_at",
		"inbound_transports":    "inbound_domain_id host port tls_mode",
		"inbound_recipients":    "id inbound_domain_id address",
		"user_inbound_domains":  "user_id inbound_domain_id",
		"dmarc_reports":         "id domain reporter report_id period_begin period_end received_at contact_email policy_p policy_sp policy_pct policy_adkim policy_aspf pass_count fail_count recipient",
		"dmarc_report_records":  "id report_row_id source_ip count disposition spf_result dkim_result header_from",
		"inbound_spam_log":      "id inbound_domain client_ip from_addr to_addr subject decision engine detail created_at inbound_domain_id",
		"inbound_spam_list":     "id list_type entry_type value note created_at",
		"send_log_lines":        "id queue_id logged_at line",
		"notify_state":          "key phase count mark since sent_at",
		"notify_addressees":     "key address told_at",
		"sender_check_days":     "day down_seconds deferred messages estimated",
		"delivery_log_days":     "day reconcile_closed reconcile_missed reconcile_contradicted tailer_blind_seconds queued_over_lifetime",
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
		"idx_applications_login_nocase", "idx_dmarc_report_records_report", "idx_dmarc_reports_domain", "idx_dmarc_reports_received",
		"idx_inbound_spam_log_created_at", "idx_inbound_spam_log_domain_created_at", "idx_inbound_spam_log_domain_id_created_at",
		"idx_send_log_app_login", "idx_send_log_app_login_created_at", "idx_send_log_created_at", "idx_send_log_domain", "idx_send_log_domain_id", "idx_send_log_domain_id_created_at",
		"idx_send_log_lines_queue_id", "idx_send_log_open", "idx_send_log_queue_id", "idx_send_log_refusal_line", "idx_send_log_rejected",
		"idx_sessions_expires_at",
	}
	sort.Strings(wantIdx)
	if strings.Join(indexes, " ") != strings.Join(wantIdx, " ") {
		t.Errorf("indexes = %v\nwant %v", indexes, wantIdx)
	}

	var version, appID int64
	if err := st.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 11 {
		t.Errorf("user_version = %d, %v; want 11", version, err)
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

// A 2.0.x data directory - the baseline and nothing after it - is brought to
// the head by opening it, with what it holds left alone.
func TestOpenMigratesA20Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	baseline, err := migrationsFS.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatalf("read the baseline: %v", err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("create a 2.0 db: %v", err)
	}
	for _, stmt := range []string{
		string(baseline),
		"PRAGMA user_version = 1",
		"INSERT INTO users (username, password_hash, role, created_at) VALUES ('admin', 'hash', 'global', '2026-10-11T00:00:00Z')",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("prepare a 2.0 db: %v", err)
		}
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open a 2.0 database: %v", err)
	}
	defer st.Close()
	var version int
	if err := st.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 11 {
		t.Errorf("user_version = %d, %v; want 11", version, err)
	}
	if ok, err := st.UserExists(); err != nil || !ok {
		t.Errorf("the user of the 2.0 database: exists = %v, %v", ok, err)
	}
	for _, table := range []string{"inbound_spam_log", "inbound_spam_list", "send_log_lines", "notify_state"} {
		if len(tableColumns(t, st, table)) == 0 {
			t.Errorf("table %s was not created", table)
		}
	}
}

// What the antispam tables refuse: a decision, engine or list the panel does
// not know, and the same entry twice in one list.
func TestInboundSpamConstraints(t *testing.T) {
	st := openTestStore(t)
	logRow := func(decision, engine string) error {
		_, err := st.db.Exec(
			"INSERT INTO inbound_spam_log (inbound_domain, client_ip, from_addr, to_addr, subject, decision, engine, detail, created_at)"+
				" VALUES ('in.example.com', '192.0.2.1', 'a@example.net', 'b@in.example.com', '', ?, ?, '', '2026-10-11T00:00:00Z')",
			decision, engine)
		return err
	}
	if err := logRow("reject", "postfix"); err != nil {
		t.Fatalf("a reject by postfix: %v", err)
	}
	if err := logRow("bounce", "postfix"); err == nil {
		t.Error("decision bounce was accepted")
	}
	if err := logRow("accept", "spamassassin"); err == nil {
		t.Error("engine spamassassin was accepted")
	}

	entry := func(list, kind, value string) error {
		_, err := st.db.Exec(
			"INSERT INTO inbound_spam_list (list_type, entry_type, value, created_at) VALUES (?, ?, ?, '2026-10-11T00:00:00Z')",
			list, kind, value)
		return err
	}
	if err := entry("allow", "address", "billing@vendor.example"); err != nil {
		t.Fatalf("an allow entry: %v", err)
	}
	if err := entry("allow", "address", "billing@vendor.example"); err == nil {
		t.Error("the same entry was added to one list twice")
	}
	if err := entry("deny", "address", "billing@vendor.example"); err != nil {
		t.Errorf("the same value on the other list: %v", err)
	}
	if err := entry("block", "address", "x@example.net"); err == nil {
		t.Error("list type block was accepted")
	}
	if err := entry("deny", "subject", "pills"); err == nil {
		t.Error("entry type subject was accepted")
	}
	var note string
	if err := st.db.QueryRow("SELECT note FROM inbound_spam_list WHERE list_type = 'allow'").Scan(&note); err != nil || note != "" {
		t.Errorf("note of an entry added without one = %q, %v", note, err)
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
	insert := func(name, role string, all int) error {
		_, err := st.db.Exec(
			"INSERT INTO users (username, password_hash, role, all_domains, created_at) VALUES (?, 'h', ?, ?, 'now')",
			name, role, all)
		return err
	}
	if err := insert("plain", "global", 0); err != nil {
		t.Fatalf("a plain global user: %v", err)
	}
	if err := insert("everything", "domain", 1); err != nil {
		t.Fatalf("a domain user with All: %v", err)
	}
	if err := insert("old-role", "domain_admin", 0); err == nil {
		t.Error("the 1.x role name domain_admin was accepted")
	}
	if err := insert("two", "domain", 2); err == nil {
		t.Error("all_domains = 2 was accepted")
	}

	u, err := st.GetUserByUsername("everything")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if !u.AllDomains || u.AllInboundDomains {
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

// Two processes opening one file that needs migrating - the panel and
// dmarc-ingest after an upgrade - each apply every migration at most once:
// both read the old version, both wait for the write lock, and the second
// finds the work done (assumptions D-S3). Before _txlock=immediate and the
// re-read, the second ran 0001 again and failed with "already exists".
func TestTwoOpensMigrateOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selfpost.db")
	// The blocker holds the write lock while both opens read user_version 0
	// and queue on their first BEGIN IMMEDIATE.
	blocker, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	blocker.SetMaxOpenConns(1)
	if _, err := blocker.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}

	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			st, err := Open(path)
			if err == nil {
				err = st.Close()
			}
			errs <- err
		}()
	}
	time.Sleep(500 * time.Millisecond) // both are waiting on the lock by now
	if _, err := blocker.Exec("COMMIT"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
	}

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var version int
	if err := st.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	entries, _ := fs.ReadDir(migrationsFS, "migrations")
	if version != len(entries) {
		t.Fatalf("user_version %d, want %d", version, len(entries))
	}
}
