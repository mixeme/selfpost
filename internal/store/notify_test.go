package store

import (
	"strings"
	"testing"
	"time"
)

// The instance switch is on until someone turns it off: a database that never
// saw the setting sends notifications.
func TestNotificationsEnabledByDefault(t *testing.T) {
	s := openTestStore(t)
	if on, err := s.NotificationsEnabled(); err != nil || !on {
		t.Fatalf("a fresh database: enabled = %v, %v; want on", on, err)
	}
	if err := s.SetNotificationsEnabled(false); err != nil {
		t.Fatal(err)
	}
	if on, err := s.NotificationsEnabled(); err != nil || on {
		t.Errorf("after turning off: enabled = %v, %v", on, err)
	}
	if err := s.SetNotificationsEnabled(true); err != nil {
		t.Fatal(err)
	}
	if on, err := s.NotificationsEnabled(); err != nil || !on {
		t.Errorf("after turning on again: enabled = %v, %v", on, err)
	}
}

// Who is written to: every global user about everything, a domain user about
// the domains on their list or all of them - and nobody without an e-mail or
// with their own switch off.
func TestNotifyRecipients(t *testing.T) {
	s := openTestStore(t)
	mine, err := s.AddDomain("mine.example.com", "sp")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AddDomain("other.example.com", "sp")
	if err != nil {
		t.Fatal(err)
	}

	user := func(name string, role Role, reach Reach, email string, notify bool) {
		t.Helper()
		id, err := s.CreateUser(name, "hash", role, reach)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if err := s.UpdateUser(id, name, "hash", email); err != nil {
			t.Fatalf("update %s: %v", name, err)
		}
		if u, err := s.GetUser(id); err != nil || !u.Notify {
			t.Fatalf("%s: a new user's switch is %v, %v; want on", name, u.Notify, err)
		}
		if !notify {
			if err := s.SetUserNotify(id, false); err != nil {
				t.Fatalf("switch off %s: %v", name, err)
			}
		}
	}
	user("root", RoleGlobal, Reach{}, "root@example.org", true)
	user("root2", RoleGlobal, Reach{}, "root@example.org", true) // the same mailbox
	user("quiet", RoleGlobal, Reach{}, "quiet@example.org", false)
	user("nomail", RoleGlobal, Reach{}, "", true)
	user("mine", RoleDomain, Reach{DomainIDs: []int64{mine.ID}}, "mine@example.org", true)
	user("theirs", RoleDomain, Reach{DomainIDs: []int64{other.ID}}, "theirs@example.org", true)
	user("every", RoleDomain, Reach{AllDomains: true}, "every@example.org", true)
	user("mineoff", RoleDomain, Reach{DomainIDs: []int64{mine.ID}}, "mineoff@example.org", false)

	for _, c := range []struct{ domain, want string }{
		{"", "root@example.org"},
		{"mine.example.com", "every@example.org mine@example.org root@example.org"},
		{"other.example.com", "every@example.org root@example.org theirs@example.org"},
		{"gone.example.com", "every@example.org root@example.org"},
	} {
		got, err := s.NotifyRecipients(c.domain)
		if err != nil {
			t.Fatalf("recipients for %q: %v", c.domain, err)
		}
		if strings.Join(got, " ") != c.want {
			t.Errorf("recipients for %q = %q, want %q", c.domain, strings.Join(got, " "), c.want)
		}
	}

	if err := s.SetUserNotify(99999, true); err != ErrUserNotFound {
		t.Errorf("switch of a user that does not exist: %v", err)
	}
}

// What has been said is remembered by key, replaced whole, and forgotten.
func TestNotifyStateRoundTrip(t *testing.T) {
	s := openTestStore(t)
	if _, ok, err := s.GetNotifyState("dns:example.com"); err != nil || ok {
		t.Fatalf("no state yet: ok = %v, %v", ok, err)
	}
	since := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	st := NotifyState{Key: "dns:example.com", Phase: "pending", Count: 1, Since: since}
	if err := s.SetNotifyState(st); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetNotifyState("dns:example.com")
	if err != nil || !ok || got.Phase != "pending" || got.Count != 1 || !got.Since.Equal(since) || !got.SentAt.IsZero() {
		t.Fatalf("state = %+v, %v, %v", got, ok, err)
	}

	st.Phase, st.Count, st.Mark, st.SentAt = "open", 0, "x", since.Add(time.Hour)
	if err := s.SetNotifyState(st); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.GetNotifyState("dns:example.com")
	if got.Phase != "open" || got.Mark != "x" || !got.SentAt.Equal(since.Add(time.Hour)) {
		t.Errorf("replaced state = %+v", got)
	}

	for _, key := range []string{"dns:other.example.com", "queue", "limit:example.com:app"} {
		if err := s.SetNotifyState(NotifyState{Key: key, Phase: "open", Since: since}); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := s.ListNotifyStateKeys("dns:")
	if err != nil || strings.Join(keys, " ") != "dns:example.com dns:other.example.com" {
		t.Errorf("dns keys = %v, %v", keys, err)
	}
	if err := s.DeleteNotifyState("dns:example.com"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetNotifyState("dns:example.com"); ok {
		t.Error("state still there after delete")
	}
}

// Refused mail is counted per sending domain from a point in time - and only
// what was refused under a limit. The row's own domain is what the client
// typed in MAIL FROM; the login's registered domain is what counts. A login
// that is nobody's is not counted, nor is one refused with no limit set (its
// client address was not on its list).
func TestLimitRefusalsSince(t *testing.T) {
	s := openTestStore(t)
	d, err := s.AddDomain("example.com", "sp")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AddDomain("other.example", "sp")
	if err != nil {
		t.Fatal(err)
	}
	app := func(domainID int64, login string) int64 {
		t.Helper()
		a, err := s.AddApplication(domainID, login, AddressModeWildcard, nil)
		if err != nil {
			t.Fatalf("add %s: %v", login, err)
		}
		return a.ID
	}
	limit := func(scope string, id int64) {
		t.Helper()
		if err := s.SetRateLimit(RateLimit{Scope: scope, RefID: id, MaxMessages: 1, WindowSeconds: 3600}); err != nil {
			t.Fatalf("set limit: %v", err)
		}
	}
	limit(RateLimitScopeApp, app(d.ID, "app1"))
	limit(RateLimitScopeApp, app(d.ID, "app2"))
	app(d.ID, "nolimit")         // refused for its client address
	app(other.ID, "byitsdomain") // limited through its domain
	limit(RateLimitScopeDomain, other.ID)

	for _, e := range []SendLogEntry{
		{Domain: "example.com", AppLogin: "app1", From: "a@example.com"},
		{Domain: "victim.example", AppLogin: "app1", From: "a@victim.example"}, // a domain the client made up
		{Domain: "other.example", AppLogin: "app2", From: "b@other.example"},   // another tenant's name
		{Domain: "example.com", AppLogin: "nolimit", From: "c@example.com"},
		{Domain: "example.com", AppLogin: "ghost", From: "d@example.com"},
		{Domain: "other.example", AppLogin: "byitsdomain", From: "e@other.example"},
	} {
		if err := s.InsertRejected(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.InsertQueued(SendLogEntry{QueueID: "ABC123", Domain: "example.com", AppLogin: "app1", To: "x@example.net"}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertRejected(SendLogEntry{Domain: "example.com", AppLogin: "app2", From: "old@example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE send_log SET created_at = '2026-01-01T00:00:00Z' WHERE from_addr = 'old@example.com'`); err != nil {
		t.Fatal(err)
	}

	got, err := s.LimitRefusalsSince(time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("refusals = %+v, want two domains", got)
	}
	if g := got[0]; g.Domain != "example.com" || strings.Join(g.Apps, " ") != "app1 app2" || g.Count != 3 || g.First.IsZero() {
		t.Errorf("example.com = %+v; want app1 (2, one under a made-up domain) and app2 (1, under another tenant's name)", g)
	}
	if g := got[1]; g.Domain != "other.example" || strings.Join(g.Apps, " ") != "byitsdomain" || g.Count != 1 {
		t.Errorf("other.example = %+v", g)
	}

	// The question is answered from the index of the refused rows alone.
	var plan []string
	rows, err := s.db.Query(`EXPLAIN QUERY PLAN SELECT COUNT(*) FROM send_log l WHERE l.status = 'rejected' AND l.created_at >= '2026-10-01T00:00:00Z'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	rows.Close()
	if !strings.Contains(strings.Join(plan, "; "), "idx_send_log_rejected") {
		t.Errorf("refused rows are not found through their index: %v", plan)
	}
}
