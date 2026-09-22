package web

import (
	"path/filepath"
	"testing"
	"time"

	"codeberg.org/mix/selfpost/internal/store"
)

func newTestSessionStore(t *testing.T) *sessionStore {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return newSessionStore(st, 7*24*time.Hour)
}

func TestSessionRename(t *testing.T) {
	s := newTestSessionStore(t)
	token := s.Create("admin")

	s.Rename(token, "operator")

	name, ok := s.Lookup(token)
	if !ok {
		t.Fatal("session lost after rename")
	}
	if name != "operator" {
		t.Fatalf("session username = %q, want %q", name, "operator")
	}
}

// A password change must invalidate every other session (so a cookie captured
// under the old password stops working) while keeping the one performing the
// change signed in.
func TestSessionDestroyOthers(t *testing.T) {
	s := newTestSessionStore(t)
	keep := s.Create("admin")
	other := s.Create("admin")

	s.DestroyOthers(keep)

	if _, ok := s.Lookup(keep); !ok {
		t.Fatal("current session was destroyed")
	}
	if _, ok := s.Lookup(other); ok {
		t.Fatal("other session survived")
	}
}

// A session past its sliding idle expiry must not be honoured.
func TestSessionLookupRejectsExpired(t *testing.T) {
	s := newTestSessionStore(t)
	s.idle = -time.Minute // already expired the instant it's created
	token := s.Create("admin")

	if _, ok := s.Lookup(token); ok {
		t.Fatal("expired session was accepted")
	}
}

// Touch must not rewrite the expiry (or report a renewal) inside the
// once-an-hour throttle window, so an active tab's polling doesn't turn into
// a database write per request.
func TestSessionTouchThrottled(t *testing.T) {
	s := newTestSessionStore(t)
	token := s.Create("admin")

	if s.Touch(token) {
		t.Fatal("touch renewed a session created moments ago")
	}

	// Back-date the session's last renewal by rewriting its expiry, as if it
	// had been created (or last renewed) 2 hours ago rather than moments ago.
	if err := s.store.RenewSession(hashToken(token), time.Now().Add(-2*time.Hour).Add(s.idle)); err != nil {
		t.Fatalf("renew session: %v", err)
	}
	if !s.Touch(token) {
		t.Fatal("touch did not renew a session past the throttle window")
	}
}
