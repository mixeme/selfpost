package store

import (
	"strings"
	"testing"
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

// The send log is the one table that grows with the mail, and every question
// asked of it on a path that runs per message, per log line or every few
// minutes is answered from an index: measured with EXPLAIN QUERY PLAN against
// the statements the store runs, not copies of them (audit 2026-10-12, M2;
// stage 3). SQLite chooses without statistics here, as it does on a server -
// nothing runs ANALYZE.
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
