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
