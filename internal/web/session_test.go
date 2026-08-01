package web

import "testing"

func TestSessionRename(t *testing.T) {
	s := newSessionStore()
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
	s := newSessionStore()
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
