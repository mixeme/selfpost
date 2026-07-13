package store

import (
	"testing"
	"time"
)

// readSendLog returns every send_log row ordered by id. Phase 6 has no read
// query yet (the monitoring UI is Phase 7), so tests read the table directly.
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

	// Two recipients on the same queue-id → two independent rows (spec 7.3.3).
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
