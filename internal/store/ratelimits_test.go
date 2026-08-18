package store

import (
	"fmt"
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

func TestRateLimitActive(t *testing.T) {
	inactive := []RateLimit{
		{},
		{Scope: RateLimitScopeDomain, AllowedIPs: []string{"203.0.113.1"}}, // no ceiling
		{Scope: RateLimitScopeDomain, MaxMessages: 5},                     // no window
		{Scope: RateLimitScopeApp, MaxMessages: 5},                        // no window
	}
	for i, rl := range inactive {
		if rl.Active() {
			t.Fatalf("case %d: %+v should be inactive", i, rl)
		}
	}
	active := []RateLimit{
		{Scope: RateLimitScopeDomain, MaxMessages: 5, WindowSeconds: 60},
		{Scope: RateLimitScopeApp, MaxMessages: 5, WindowSeconds: 60},
	}
	for i, rl := range active {
		if !rl.Active() {
			t.Fatalf("case %d: %+v should be active", i, rl)
		}
	}
}

func TestAutoRateLimitRecalc(t *testing.T) {
	st := openTestStore(t)
	d, _ := st.AddDomain("example.com", "selfpost")
	a, _ := st.AddApplication(d.ID, "app1", AddressModeWildcard, nil)

	for i := 0; i < 10; i++ {
		if err := st.InsertQueued(SendLogEntry{
			QueueID: fmt.Sprintf("Q%d", i), Domain: "example.com", AppLogin: "app1", To: "t@x.net",
		}); err != nil {
			t.Fatalf("InsertQueued: %v", err)
		}
	}

	if err := st.SetRateLimit(RateLimit{
		Scope: RateLimitScopeDomain, RefID: d.ID, Mode: RateLimitModeAuto,
		AutoMultiplier: 2.0, WindowSeconds: 3600,
	}); err != nil {
		t.Fatalf("SetRateLimit domain: %v", err)
	}
	if err := st.RecalcAutoRateLimit(RateLimitScopeDomain, d.ID, 90, 100, 3600); err != nil {
		t.Fatalf("RecalcAutoRateLimit domain: %v", err)
	}
	rl, ok, err := st.GetRateLimit(RateLimitScopeDomain, d.ID)
	if err != nil || !ok {
		t.Fatalf("GetRateLimit: ok=%v err=%v", ok, err)
	}
	if !rl.Active() || rl.MaxMessages > 100 {
		t.Fatalf("domain auto limit = %+v", rl)
	}
	if rl.WindowSeconds != 3600 {
		t.Fatalf("window = %d, want 3600", rl.WindowSeconds)
	}

	// Domain limit at ceiling; app auto is capped at L1 independently.
	_ = st.SetRateLimit(RateLimit{
		Scope: RateLimitScopeDomain, RefID: d.ID, Mode: RateLimitModeManual,
		MaxMessages: 100, WindowSeconds: 3600,
	})
	if err := st.SetRateLimit(RateLimit{
		Scope: RateLimitScopeApp, RefID: a.ID, Mode: RateLimitModeAuto,
		AutoMultiplier: 2.0,
	}); err != nil {
		t.Fatalf("SetRateLimit app: %v", err)
	}
	if err := st.RecalcAutoRateLimit(RateLimitScopeApp, a.ID, 90, 100, 3600); err != nil {
		t.Fatalf("RecalcAutoRateLimit app: %v", err)
	}
	appRL, ok, _ := st.GetRateLimit(RateLimitScopeApp, a.ID)
	if !ok || !appRL.Active() || appRL.MaxMessages > 100 {
		t.Fatalf("app auto at L1 cap should still be active: %+v", appRL)
	}

	_ = st.SetRateLimit(RateLimit{
		Scope: RateLimitScopeDomain, RefID: d.ID, Mode: RateLimitModeManual,
		MaxMessages: 40, WindowSeconds: 3600,
	})
	if err := st.RecalcAutoRateLimit(RateLimitScopeApp, a.ID, 90, 100, 3600); err != nil {
		t.Fatalf("RecalcAutoRateLimit app: %v", err)
	}
	appRL, ok, _ = st.GetRateLimit(RateLimitScopeApp, a.ID)
	if !ok || !appRL.Active() {
		t.Fatalf("app auto should remain active with domain at 40: %+v", appRL)
	}

	// Milter reads the stored ceiling via RateLimit(name/login).
	milterRL, ok, err := st.RateLimit(RateLimitScopeDomain, "example.com")
	if err != nil || !ok || milterRL.MaxMessages != 40 {
		t.Fatalf("milter domain limit = %+v ok=%v err=%v", milterRL, ok, err)
	}
}

func TestAutoRateLimitZeroTrafficInactive(t *testing.T) {
	st := openTestStore(t)
	d, _ := st.AddDomain("quiet.com", "selfpost")
	if err := st.SetRateLimit(RateLimit{
		Scope: RateLimitScopeDomain, RefID: d.ID, Mode: RateLimitModeAuto,
		AutoMultiplier: 2.5,
	}); err != nil {
		t.Fatalf("SetRateLimit: %v", err)
	}
	if err := st.RecalcAutoRateLimit(RateLimitScopeDomain, d.ID, 90, 100, 3600); err != nil {
		t.Fatalf("RecalcAutoRateLimit: %v", err)
	}
	rl, ok, _ := st.GetRateLimit(RateLimitScopeDomain, d.ID)
	if ok && rl.Active() {
		t.Fatalf("zero traffic auto should be inactive: %+v", rl)
	}
}
