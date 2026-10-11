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
