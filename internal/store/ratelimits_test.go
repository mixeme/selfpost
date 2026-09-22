package store

import (
	"testing"
	"time"
)

func TestRateLimitSetGetDelete(t *testing.T) {
	st := openTestStore(t)
	d, err := st.AddDomain("example.com", "selfpost")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}

	if _, ok, err := st.GetRateLimit(RateLimitScopeDomain, d.ID); err != nil || ok {
		t.Fatalf("GetRateLimit on empty: ok=%v err=%v", ok, err)
	}

	want := RateLimit{
		Scope:         RateLimitScopeDomain,
		RefID:         d.ID,
		AllowedIPs:    []string{"203.0.113.1", "203.0.113.2"},
		MaxMessages:   100,
		WindowSeconds: 3600,
	}
	if err := st.SetRateLimit(want); err != nil {
		t.Fatalf("SetRateLimit: %v", err)
	}

	got, ok, err := st.GetRateLimit(RateLimitScopeDomain, d.ID)
	if err != nil || !ok {
		t.Fatalf("GetRateLimit after set: ok=%v err=%v", ok, err)
	}
	if got.MaxMessages != 100 || got.WindowSeconds != 3600 || len(got.AllowedIPs) != 2 ||
		got.AllowedIPs[0] != "203.0.113.1" || got.AllowedIPs[1] != "203.0.113.2" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}

	// Upsert replaces in place (UNIQUE(scope, ref_id)).
	want.MaxMessages = 5
	want.AllowedIPs = []string{"198.51.100.9"}
	if err := st.SetRateLimit(want); err != nil {
		t.Fatalf("SetRateLimit upsert: %v", err)
	}
	got, _, _ = st.GetRateLimit(RateLimitScopeDomain, d.ID)
	if got.MaxMessages != 5 || len(got.AllowedIPs) != 1 || got.AllowedIPs[0] != "198.51.100.9" {
		t.Fatalf("upsert did not replace: %+v", got)
	}

	if err := st.DeleteRateLimit(RateLimitScopeDomain, d.ID); err != nil {
		t.Fatalf("DeleteRateLimit: %v", err)
	}
	if _, ok, _ := st.GetRateLimit(RateLimitScopeDomain, d.ID); ok {
		t.Fatalf("limit still present after delete")
	}
}

func TestRateLimitByNameAndLogin(t *testing.T) {
	st := openTestStore(t)
	d, _ := st.AddDomain("example.com", "selfpost")
	a, err := st.AddApplication(d.ID, "app1", AddressModeWildcard, nil)
	if err != nil {
		t.Fatalf("AddApplication: %v", err)
	}

	if err := st.SetRateLimit(RateLimit{Scope: RateLimitScopeDomain, RefID: d.ID, AllowedIPs: []string{"203.0.113.1"}, MaxMessages: 10, WindowSeconds: 60}); err != nil {
		t.Fatalf("set domain limit: %v", err)
	}
	if err := st.SetRateLimit(RateLimit{Scope: RateLimitScopeApp, RefID: a.ID, AllowedIPs: []string{"203.0.113.2"}, MaxMessages: 3, WindowSeconds: 60}); err != nil {
		t.Fatalf("set app limit: %v", err)
	}

	// The milter resolves limits by domain name and by SASL login.
	rl, ok, err := st.RateLimit(RateLimitScopeDomain, "example.com")
	if err != nil || !ok || rl.MaxMessages != 10 {
		t.Fatalf("RateLimit domain: ok=%v err=%v rl=%+v", ok, err, rl)
	}
	rl, ok, err = st.RateLimit(RateLimitScopeApp, "app1")
	if err != nil || !ok || rl.MaxMessages != 3 {
		t.Fatalf("RateLimit app: ok=%v err=%v rl=%+v", ok, err, rl)
	}
	if _, ok, _ := st.RateLimit(RateLimitScopeDomain, "unknown.example"); ok {
		t.Fatalf("RateLimit for unknown domain should be not-ok")
	}
}

func TestCountMessagesDistinctAndWindowed(t *testing.T) {
	st := openTestStore(t)

	// Two recipients share a queue-id → one message. A second message → two.
	for _, to := range []string{"a@x.net", "b@x.net"} {
		if err := st.InsertQueued(SendLogEntry{QueueID: "Q1", Domain: "example.com", AppLogin: "app1", To: to}); err != nil {
			t.Fatalf("InsertQueued: %v", err)
		}
	}
	if err := st.InsertQueued(SendLogEntry{QueueID: "Q2", Domain: "example.com", AppLogin: "app1", To: "c@x.net"}); err != nil {
		t.Fatalf("InsertQueued: %v", err)
	}
	// A rejected row must not count toward the window.
	if err := st.InsertRejected(SendLogEntry{Domain: "example.com", AppLogin: "app1", From: "s@example.com"}); err != nil {
		t.Fatalf("InsertRejected: %v", err)
	}

	n, err := st.CountMessages(RateLimitScopeDomain, "example.com", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("CountMessages: %v", err)
	}
	if n != 2 {
		t.Fatalf("distinct-message count = %d, want 2 (two queue-ids, rejected excluded)", n)
	}
	n, _ = st.CountMessages(RateLimitScopeApp, "app1", time.Now().Add(-time.Hour))
	if n != 2 {
		t.Fatalf("app count = %d, want 2", n)
	}

	// Backdate Q1 beyond the window: only Q2 remains inside a 30-minute window.
	old := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	if _, err := st.db.Exec(`UPDATE send_log SET created_at = ? WHERE queue_id = 'Q1'`, old); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	n, _ = st.CountMessages(RateLimitScopeDomain, "example.com", time.Now().Add(-30*time.Minute))
	if n != 1 {
		t.Fatalf("windowed count = %d, want 1 (Q1 aged out)", n)
	}
}

func TestDeleteRateLimitsForDomain(t *testing.T) {
	st := openTestStore(t)
	d, _ := st.AddDomain("example.com", "selfpost")
	a, _ := st.AddApplication(d.ID, "app1", AddressModeWildcard, nil)
	other, _ := st.AddDomain("other.example", "selfpost")

	_ = st.SetRateLimit(RateLimit{Scope: RateLimitScopeDomain, RefID: d.ID, AllowedIPs: []string{"203.0.113.1"}, MaxMessages: 10, WindowSeconds: 60})
	_ = st.SetRateLimit(RateLimit{Scope: RateLimitScopeApp, RefID: a.ID, AllowedIPs: []string{"203.0.113.2"}, MaxMessages: 3, WindowSeconds: 60})
	_ = st.SetRateLimit(RateLimit{Scope: RateLimitScopeDomain, RefID: other.ID, AllowedIPs: []string{"203.0.113.9"}, MaxMessages: 1, WindowSeconds: 60})

	if err := st.DeleteRateLimitsForDomain(d.ID); err != nil {
		t.Fatalf("DeleteRateLimitsForDomain: %v", err)
	}
	if _, ok, _ := st.GetRateLimit(RateLimitScopeDomain, d.ID); ok {
		t.Fatalf("domain limit survived")
	}
	if _, ok, _ := st.GetRateLimit(RateLimitScopeApp, a.ID); ok {
		t.Fatalf("application limit survived")
	}
	// The unrelated domain's limit is untouched.
	if _, ok, _ := st.GetRateLimit(RateLimitScopeDomain, other.ID); !ok {
		t.Fatalf("unrelated domain limit was deleted")
	}
}

func TestRateLimitActiveAndAllowsIP(t *testing.T) {
	inactive := []RateLimit{
		{},
		{AllowedIPs: []string{"203.0.113.1"}}, // no ceiling
		{AllowedIPs: []string{"203.0.113.1"}, MaxMessages: 5}, // no window
		{MaxMessages: 5, WindowSeconds: 60},                   // no IPs
	}
	for i, rl := range inactive {
		if rl.Active() {
			t.Fatalf("case %d: %+v should be inactive", i, rl)
		}
	}
	active := RateLimit{AllowedIPs: []string{"203.0.113.1", "2001:db8::1"}, MaxMessages: 5, WindowSeconds: 60}
	if !active.Active() {
		t.Fatalf("should be active: %+v", active)
	}
	if !active.AllowsIP("203.0.113.1") || !active.AllowsIP("2001:db8::1") {
		t.Fatalf("registered IPs should match")
	}
	// Equivalent textual form of the IPv6 address must still match.
	if !active.AllowsIP("2001:0db8:0000:0000:0000:0000:0000:0001") {
		t.Fatalf("expanded IPv6 form should match")
	}
	if active.AllowsIP("198.51.100.7") || active.AllowsIP("not-an-ip") || active.AllowsIP("") {
		t.Fatalf("unregistered/invalid IPs must not match")
	}
}
