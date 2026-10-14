package store

import (
	"testing"
	"time"
)

func TestInsertInboundSpam(t *testing.T) {
	st := openTestStore(t)
	e := InboundSpamEntry{
		InboundDomain: "in.example.com", ClientIP: "203.0.113.7",
		From: "sender@example.net", To: "a@in.example.com", Subject: "Hello",
		Decision: SpamDecisionAccept, Engine: SpamEngineRspamd, Detail: "no action, score 0.10 / 15.00",
	}
	if err := st.InsertInboundSpam(e); err != nil {
		t.Fatalf("InsertInboundSpam: %v", err)
	}
	var got InboundSpamEntry
	var created string
	err := st.db.QueryRow(
		"SELECT inbound_domain, client_ip, from_addr, to_addr, subject, decision, engine, detail, created_at FROM inbound_spam_log",
	).Scan(&got.InboundDomain, &got.ClientIP, &got.From, &got.To, &got.Subject, &got.Decision, &got.Engine, &got.Detail, &created)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != e {
		t.Errorf("row = %+v\nwant  %+v", got, e)
	}
	if at, err := time.Parse(time.RFC3339, created); err != nil || time.Since(at) > time.Minute {
		t.Errorf("created_at = %q (%v)", created, err)
	}

	// Every decision and engine the code names is one the table takes.
	for _, d := range []string{SpamDecisionAccept, SpamDecisionReject, SpamDecisionTempfail, SpamDecisionQuarantine} {
		for _, eng := range []string{SpamEngineRspamd, SpamEnginePostfix, SpamEngineSelfPost} {
			e.Decision, e.Engine = d, eng
			if err := st.InsertInboundSpam(e); err != nil {
				t.Errorf("decision %s by %s: %v", d, eng, err)
			}
		}
	}
	e.Decision = "bounce"
	if err := st.InsertInboundSpam(e); err == nil {
		t.Error("an unknown decision was stored")
	}
}

func TestIsInboundDomain(t *testing.T) {
	st := openTestStore(t)
	if _, err := st.AddInboundDomain("in.example.com"); err != nil {
		t.Fatalf("AddInboundDomain: %v", err)
	}
	for name, want := range map[string]bool{
		"in.example.com": true, "IN.Example.COM": true,
		"example.com": false, "": false, "other.example.com": false,
	} {
		if got, err := st.IsInboundDomain(name); err != nil || got != want {
			t.Errorf("IsInboundDomain(%q) = %v, %v; want %v", name, got, err, want)
		}
	}
}

func TestInsertInboundSpamKeepsItsOwnTime(t *testing.T) {
	st := openTestStore(t)
	at := time.Date(2026, 10, 1, 9, 30, 0, 0, time.FixedZone("msk", 3*3600))
	err := st.InsertInboundSpam(InboundSpamEntry{
		InboundDomain: "in.example.com", ClientIP: "203.0.113.7", To: "a@in.example.com",
		Decision: SpamDecisionReject, Engine: SpamEnginePostfix, At: at,
	})
	if err != nil {
		t.Fatalf("InsertInboundSpam: %v", err)
	}
	var created string
	if err := st.db.QueryRow("SELECT created_at FROM inbound_spam_log").Scan(&created); err != nil || created != "2026-10-01T06:30:00Z" {
		t.Errorf("created_at = %q, %v", created, err)
	}
}

func TestPruneInboundSpam(t *testing.T) {
	st := openTestStore(t)
	now := time.Now().UTC()
	add := func(to string, age time.Duration) {
		t.Helper()
		err := st.InsertInboundSpam(InboundSpamEntry{
			InboundDomain: "in.example.com", ClientIP: "203.0.113.7", To: to,
			Decision: SpamDecisionReject, Engine: SpamEnginePostfix, At: now.Add(-age),
		})
		if err != nil {
			t.Fatalf("InsertInboundSpam: %v", err)
		}
	}
	left := func() string {
		t.Helper()
		rows, err := st.db.Query("SELECT to_addr FROM inbound_spam_log ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out string
		for rows.Next() {
			var to string
			if err := rows.Scan(&to); err != nil {
				t.Fatal(err)
			}
			out += to + " "
		}
		return out
	}

	add("old", 100*24*time.Hour)
	add("a", 3*time.Hour)
	add("b", 2*time.Hour)
	add("c", time.Hour)

	// Age first: nothing else goes while the rest fits.
	if n, err := st.PruneInboundSpam(now.AddDate(0, 0, -90), 10); err != nil || n != 1 {
		t.Fatalf("prune by age = %d, %v", n, err)
	}
	if got := left(); got != "a b c " {
		t.Fatalf("left = %q", got)
	}
	// Then the cap, oldest first.
	if n, err := st.PruneInboundSpam(now.AddDate(0, 0, -90), 2); err != nil || n != 1 {
		t.Fatalf("prune by count = %d, %v", n, err)
	}
	if got := left(); got != "b c " {
		t.Fatalf("left = %q", got)
	}
	if n, err := st.PruneInboundSpam(now.AddDate(0, 0, -90), 2); err != nil || n != 0 {
		t.Errorf("a second prune removed %d, %v", n, err)
	}
}

func TestInboundSpamLogRetentionDays(t *testing.T) {
	st := openTestStore(t)
	if days, err := st.GetInboundSpamLogRetentionDays(30); err != nil || days != 30 {
		t.Errorf("unset, env 30: %d, %v", days, err)
	}
	if days, err := st.GetInboundSpamLogRetentionDays(3); err != nil || days != InboundSpamLogRetentionDaysDefault {
		t.Errorf("unset, env out of range: %d, %v", days, err)
	}
	if err := st.EnsureInboundSpamLogRetentionDays(30); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if err := st.EnsureInboundSpamLogRetentionDays(60); err != nil {
		t.Fatalf("Ensure again: %v", err)
	}
	if days, _ := st.GetInboundSpamLogRetentionDays(90); days != 30 {
		t.Errorf("after Ensure(30), Ensure(60): %d, want the first seed kept", days)
	}
	if err := st.SetInboundSpamLogRetentionDays(6); err == nil {
		t.Error("6 days was accepted")
	}
	if err := st.SetInboundSpamLogRetentionDays(365); err != nil {
		t.Errorf("365 days: %v", err)
	}
	if days, _ := st.GetInboundSpamLogRetentionDays(90); days != 365 {
		t.Errorf("after Set(365): %d", days)
	}
}

func TestQueryInboundSpamFilters(t *testing.T) {
	st := openTestStore(t)
	day := func(d, h int) time.Time { return time.Date(2026, 9, d, h, 0, 0, 0, time.UTC) }
	add := func(domain, to, decision string, at time.Time) {
		t.Helper()
		err := st.InsertInboundSpam(InboundSpamEntry{
			InboundDomain: domain, ClientIP: "203.0.113.7", From: "s@example.net", To: to,
			Subject: "S " + to, Decision: decision, Engine: SpamEngineRspamd, Detail: "d", At: at,
		})
		if err != nil {
			t.Fatalf("InsertInboundSpam: %v", err)
		}
	}
	ids := map[string]int64{}
	for _, name := range []string{"a.example", "b.example"} {
		d, err := st.AddInboundDomain(name)
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = d.ID
	}
	add("a.example", "1@a.example", SpamDecisionAccept, day(20, 23))
	add("a.example", "2@a.example", SpamDecisionReject, day(21, 0))
	add("b.example", "3@b.example", SpamDecisionAccept, day(21, 12))
	add("b.example", "4@b.example", SpamDecisionTempfail, day(22, 0))
	// Written last but dated earliest: the tailer reads a refusal after the fact.
	add("a.example", "5@a.example", SpamDecisionReject, day(19, 8))

	to := func(f InboundSpamFilter) string {
		t.Helper()
		rows, err := st.QueryInboundSpam(f, 50, 0)
		if err != nil {
			t.Fatalf("QueryInboundSpam(%+v): %v", f, err)
		}
		n, err := st.CountInboundSpam(f)
		if err != nil || n != int64(len(rows)) {
			t.Fatalf("CountInboundSpam(%+v) = %d, %v; the query returned %d", f, n, err, len(rows))
		}
		var out string
		for _, r := range rows {
			out += r.To[:1]
		}
		return out
	}

	// Newest first by time, not by the order of writing.
	if got := to(InboundSpamFilter{}); got != "43215" {
		t.Errorf("no filter = %q, want 43215", got)
	}
	if got := to(InboundSpamFilter{Domain: "a.example"}); got != "215" {
		t.Errorf("domain = %q, want 215", got)
	}
	if got := to(InboundSpamFilter{Decision: SpamDecisionReject}); got != "25" {
		t.Errorf("decision = %q, want 25", got)
	}
	// Days are whole UTC days and both ends are in: 21 st is 00:00 up to 23:59.
	if got := to(InboundSpamFilter{Since: day(21, 15), Until: day(21, 3)}); got != "32" {
		t.Errorf("one day = %q, want 32", got)
	}
	if got := to(InboundSpamFilter{Since: day(22, 0)}); got != "4" {
		t.Errorf("since = %q, want 4", got)
	}
	if got := to(InboundSpamFilter{Until: day(20, 0)}); got != "15" {
		t.Errorf("until = %q, want 15", got)
	}
	// A time in another zone is read as the UTC day it falls on.
	msk := time.FixedZone("msk", 3*3600)
	if got := to(InboundSpamFilter{Since: time.Date(2026, 9, 22, 2, 0, 0, 0, msk)}); got != "432" {
		t.Errorf("since in another zone = %q, want 432 (it is the 21st in UTC)", got)
	}
	if got := to(InboundSpamFilter{Domain: "b.example", Decision: SpamDecisionAccept, Since: day(21, 0), Until: day(21, 0)}); got != "3" {
		t.Errorf("all filters = %q, want 3", got)
	}

	// The scope: nil is everything, a set is its domains, an empty set nothing,
	// and a chosen domain outside the set is nothing too.
	if got := to(InboundSpamFilter{DomainIDs: []int64{ids["b.example"]}}); got != "43" {
		t.Errorf("scope = %q, want 43", got)
	}
	if got := to(InboundSpamFilter{DomainIDs: []int64{}}); got != "" {
		t.Errorf("empty scope = %q, want nothing", got)
	}
	if got := to(InboundSpamFilter{DomainIDs: []int64{ids["b.example"]}, Domain: "a.example"}); got != "" {
		t.Errorf("a chosen domain outside the scope = %q, want nothing", got)
	}

	// Paging.
	rows, err := st.QueryInboundSpam(InboundSpamFilter{}, 2, 2)
	if err != nil || len(rows) != 2 || rows[0].To != "2@a.example" || rows[1].To != "1@a.example" {
		t.Errorf("page 2 of 2 rows = %+v, %v", rows, err)
	}
	if rows[0].ID == 0 || !rows[0].CreatedAt.Equal(day(21, 0)) || rows[0].Subject != "S 2@a.example" ||
		rows[0].Engine != SpamEngineRspamd || rows[0].Detail != "d" || rows[0].ClientIP != "203.0.113.7" || rows[0].From != "s@example.net" {
		t.Errorf("a row read back = %+v", rows[0])
	}

	// A value is data, not SQL.
	if got := to(InboundSpamFilter{Domain: "a.example' OR '1'='1"}); got != "" {
		t.Errorf("injected domain = %q", got)
	}
}

func TestListSpamListHits(t *testing.T) {
	st := openTestStore(t)
	add := func(engine, decision, detail string) {
		t.Helper()
		err := st.InsertInboundSpam(InboundSpamEntry{
			InboundDomain: "in.example.com", ClientIP: "203.0.113.7", From: "a@example.net", To: "a@in.example.com",
			Decision: decision, Engine: engine, Detail: detail, At: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	add(SpamEngineRspamd, SpamDecisionReject, "5.7.1 Refused by this server's deny list")
	add(SpamEngineRspamd, SpamDecisionAccept, "no action, score -99.00 / 15.00; SELFPOST_ALLOW_IP(-100.00)")
	add(SpamEngineRspamd, SpamDecisionReject, "5.7.1 Spam message rejected")
	add(SpamEnginePostfix, SpamDecisionReject, "550 5.1.1 this server's deny list")
	// a mark with the characters LIKE would treat as wildcards is text
	add(SpamEngineRspamd, SpamDecisionAccept, "no action; SELFPOSTXALLOWX(1.00)")

	hits, err := st.ListSpamListHits("this server's deny list", "SELFPOST_ALLOW_")
	if err != nil || len(hits) != 2 {
		t.Fatalf("hits = %+v, %v; want the two rows a list decided", hits, err)
	}
	h := hits[0]
	if h.From != "a@example.net" || h.ClientIP != "203.0.113.7" || h.Decision != SpamDecisionReject || h.CreatedAt.Hour() != 9 {
		t.Errorf("hit = %+v", h)
	}
	if hits, err := st.ListSpamListHits(); err != nil || hits != nil {
		t.Errorf("no marks: %v, %v", hits, err)
	}
}

// The cap removes the oldest by when it happened. After the panel was down the
// tailer writes old refusals with the newest ids, and they are not what stays.
func TestPruneInboundSpamCapIsByTime(t *testing.T) {
	st := openTestStore(t)
	now := time.Now().UTC()
	for _, r := range []struct {
		to  string
		age time.Duration
	}{{"new-1", time.Minute}, {"new-2", 2 * time.Minute}, {"backdated-old", 5 * time.Hour}} {
		err := st.InsertInboundSpam(InboundSpamEntry{
			InboundDomain: "in.example.com", ClientIP: "203.0.113.7", To: r.to,
			Decision: SpamDecisionReject, Engine: SpamEnginePostfix, At: now.Add(-r.age),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if n, err := st.PruneInboundSpam(time.Time{}, 2); err != nil || n != 1 {
		t.Fatalf("prune = %d, %v", n, err)
	}
	rows, err := st.QueryInboundSpam(InboundSpamFilter{}, 10, 0)
	if err != nil || len(rows) != 2 || rows[0].To != "new-1" || rows[1].To != "new-2" {
		t.Errorf("left = %+v, %v; want the two newest by time", rows, err)
	}
}

func TestInsertInboundSpamOnce(t *testing.T) {
	st := openTestStore(t)
	e := InboundSpamEntry{
		InboundDomain: "in.example.com", ClientIP: "203.0.113.7", From: "a@example.net", To: "nobody@in.example.com",
		Decision: SpamDecisionReject, Engine: SpamEnginePostfix, Detail: "550 5.1.1 no",
		At: time.Date(2026, 10, 12, 9, 0, 5, 0, time.UTC),
	}
	for i, want := range []bool{true, false, false} {
		if wrote, err := st.InsertInboundSpamOnce(e); err != nil || wrote != want {
			t.Fatalf("attempt %d: wrote %v, %v; want %v", i+1, wrote, err, want)
		}
	}
	// Anything that tells two refusals apart makes a second row.
	later, other := e, e
	later.At = e.At.Add(time.Second)
	other.To = "somebody@in.example.com"
	for name, v := range map[string]InboundSpamEntry{"a second later": later, "another recipient": other} {
		if wrote, err := st.InsertInboundSpamOnce(v); err != nil || !wrote {
			t.Errorf("%s: wrote %v, %v", name, wrote, err)
		}
	}
	if n, err := st.CountInboundSpam(InboundSpamFilter{}); err != nil || n != 3 {
		t.Errorf("rows = %d, %v; want 3", n, err)
	}
}
