package store

import (
	"strings"
	"testing"
	"time"
)

// explain returns SQLite's plan for query, one step a line.
func explain(t *testing.T, s *Store, query string, args ...any) string {
	t.Helper()
	rows, err := s.db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain: %v\n%s", err, query)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	return strings.Join(plan, "\n")
}

// The send log is the one table that grows with the mail. The questions
// asked of it by time or by scope - per message (the level-2 count), per log
// line (a refusal line), every few minutes (the sweep, the notifier), per
// page and per statistics view - are answered from an index: measured with
// EXPLAIN QUERY PLAN against the statements the store runs, not copies of
// them (audit 2026-10-12, M2; stage 3). Those by queue id (UpdateStatus,
// ExpireMessage, RecoverRefusal) go by idx_send_log_queue_id and are not
// pinned here. SQLite chooses without statistics here, as it does on a
// server - nothing runs ANALYZE.
func TestSendLogQueriesUseTheirIndexes(t *testing.T) {
	s := openTestStore(t)
	const since = "2026-10-01T00:00:00Z"
	page := func(f SendLogFilter) (string, []any) {
		where, args := sendLogWhere(f)
		return sendLogPageQuery(where), append(args, 50, 0)
	}
	oneDomain, oneDomainArgs := page(SendLogFilter{DomainIDs: []int64{1}})
	oneApp, oneAppArgs := page(SendLogFilter{AllDomains: true, AppLogin: "app1"})
	byName, byNameArgs := page(SendLogFilter{AllDomains: true, Domain: "example.com"})

	for _, c := range []struct {
		name  string
		query string
		args  []any
		want  []string // steps the plan must have
		sorts bool     // whether it may sort the rows it reads
	}{
		{"level-2 count of a domain", countMessagesQuery(domainByName), []any{"example.com", StatusRejected, since},
			[]string{"SEARCH send_log USING INDEX idx_send_log_domain_id_created_at (domain_id=? AND created_at>?)"}, false},
		{"level-2 count of an application", countMessagesQuery(appByLogin), []any{"app1", StatusRejected, since},
			[]string{"SEARCH send_log USING INDEX idx_send_log_app_login_created_at (app_login=? AND created_at>?)"}, false},
		{"busiest hour of a domain", peakPerHourQuery(domainByName), []any{"example.com", StatusRejected, since},
			[]string{"SEARCH send_log USING INDEX idx_send_log_domain_id_created_at (domain_id=? AND created_at>?)"}, true},
		{"busiest hour of an application", peakPerHourQuery(appByLogin), []any{"app1", StatusRejected, since},
			[]string{"SEARCH send_log USING INDEX idx_send_log_app_login_created_at (app_login=? AND created_at>?)"}, true},
		{"the rows of queued messages", sendLogRefsQuery(2), []any{"4XcB7k2Jm9z1", "5F00AA12"},
			[]string{"SEARCH send_log USING INDEX idx_send_log_queue_id (queue_id=?)"}, true},
		{"open rows", listOpenQuery, []any{since},
			[]string{"SEARCH send_log USING INDEX idx_send_log_open (created_at<?)"}, true},
		{"open rows counted", countOpenQuery, []any{since},
			[]string{"SEARCH send_log USING INDEX idx_send_log_open (created_at<?)"}, false},
		{"a page of one domain", oneDomain, oneDomainArgs,
			[]string{"SEARCH send_log USING INDEX idx_send_log_domain_id (domain_id=?)"}, false},
		{"a page of one application", oneApp, oneAppArgs,
			[]string{"SEARCH send_log USING INDEX idx_send_log_app_login (app_login=?)"}, false},
		{"a page of a domain by name", byName, byNameArgs,
			[]string{"SEARCH send_log USING INDEX idx_send_log_domain (domain=?)"}, false},
		{"a refusal line", attachRefusalLineQuery,
			[]any{"line", ReasonLimit, ReasonClient, "a@example.com", "192.0.2.1", since, since, since, "app1"},
			[]string{
				"SEARCH send_log USING INDEX idx_send_log_rejected (created_at>? AND created_at<?)",
				"SEARCH send_log USING COVERING INDEX idx_send_log_refusal_line (refusal_line=?)",
			}, true},
		{"refusals under a limit", limitRefusalsQuery, []any{since},
			[]string{"SEARCH l USING INDEX idx_send_log_rejected (created_at>?)"}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			plan := explain(t, s, c.query, c.args...)
			for _, want := range c.want {
				if !strings.Contains(plan, want) {
					t.Errorf("the plan has no %q:\n%s", want, plan)
				}
			}
			for _, line := range strings.Split(plan, "\n") {
				if strings.HasPrefix(line, "SCAN send_log") || strings.HasPrefix(line, "SCAN l") {
					t.Errorf("the plan walks the whole send log:\n%s", plan)
				}
				if !c.sorts && strings.Contains(line, "TEMP B-TREE FOR ORDER BY") {
					t.Errorf("the plan sorts the rows it reads:\n%s", plan)
				}
			}
		})
	}

	// The partial indexes are used because the statuses are written into the
	// statements as the index's WHERE has them; the constants must say the
	// same.
	for _, st := range []string{StatusQueued, StatusDeferred} {
		if !strings.Contains(openRows, "'"+st+"'") {
			t.Errorf("openRows does not name status %q: %s", st, openRows)
		}
	}
	for _, q := range []string{attachRefusalLineQuery, limitRefusalsQuery} {
		if !strings.Contains(q, "status = '"+StatusRejected+"'") {
			t.Errorf("a refusal query does not name status %q", StatusRejected)
		}
	}
}

// The Outbound log's days are whole UTC days: from the first's midnight up
// to, not including, midnight after the last; the status is a row's.
func TestSendLogFilterDaysAndStatus(t *testing.T) {
	s := openTestStore(t)
	stamps := []string{"2026-09-13T23:59:59Z", "2026-09-14T00:00:00Z", "2026-09-21T23:59:59Z", "2026-09-22T00:00:00Z"}
	for i, at := range stamps {
		if err := s.InsertQueued(SendLogEntry{QueueID: "Q" + at[:2] + string(rune('A'+i)), Domain: "d.example", From: "a@d.example", To: "b@x.example"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`UPDATE send_log SET created_at = ? WHERE id = (SELECT MAX(id) FROM send_log)`, at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`UPDATE send_log SET status = 'sent' WHERE created_at = '2026-09-14T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	day := func(d int) time.Time { return time.Date(2026, 9, d, 15, 0, 0, 0, time.FixedZone("x", 3*3600)) }
	got := func(f SendLogFilter) []string {
		f.AllDomains = true
		rows, err := s.QuerySendLog(f, 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		n, err := s.CountSendLog(f)
		if err != nil || n != int64(len(rows)) {
			t.Fatalf("count %d, %v; rows %d", n, err, len(rows))
		}
		var out []string
		for _, r := range rows {
			out = append(out, r.CreatedAt.UTC().Format(time.RFC3339))
		}
		return out
	}
	if g := got(SendLogFilter{Since: day(14), Until: day(21)}); strings.Join(g, " ") != "2026-09-21T23:59:59Z 2026-09-14T00:00:00Z" {
		t.Errorf("14 to 21 = %v", g)
	}
	if g := got(SendLogFilter{Status: StatusSent}); strings.Join(g, " ") != "2026-09-14T00:00:00Z" {
		t.Errorf("sent = %v", g)
	}
	if g := got(SendLogFilter{Status: StatusQueued, Until: day(13)}); strings.Join(g, " ") != "2026-09-13T23:59:59Z" {
		t.Errorf("queued until 13 = %v", g)
	}
}
