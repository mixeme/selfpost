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

// Refused mail is counted per domain and application from a point in time;
// delivered mail and older refusals are not.
func TestLimitRefusalsSince(t *testing.T) {
	s := openTestStore(t)
	for _, e := range []SendLogEntry{
		{Domain: "example.com", AppLogin: "app1", From: "a@example.com"},
		{Domain: "example.com", AppLogin: "app1", From: "a@example.com"},
		{Domain: "example.com", AppLogin: "app2", From: "b@example.com"},
	} {
		if err := s.InsertRejected(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.InsertQueued(SendLogEntry{QueueID: "ABC123", Domain: "example.com", AppLogin: "app3", To: "x@example.net"}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertRejected(SendLogEntry{Domain: "old.example.com", AppLogin: "old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE send_log SET created_at = '2026-01-01T00:00:00Z' WHERE app_login = 'old'`); err != nil {
		t.Fatal(err)
	}

	got, err := s.LimitRefusalsSince(time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].AppLogin != "app1" || got[0].Count != 2 || got[1].AppLogin != "app2" || got[1].Count != 1 {
		t.Fatalf("refusals = %+v", got)
	}
	if got[0].Domain != "example.com" || got[0].First.IsZero() {
		t.Errorf("first refusal = %+v", got[0])
	}
}
