package store

import (
	"testing"
	"time"
)

func sessionExpiry() time.Time { return time.Now().Add(time.Hour) }

// A session belongs to a person, not to a name: once the user is deleted the
// cookie must be dead for good, including when somebody else is later created
// under the same username.
func TestSessionDoesNotOutliveItsUserOrPassToANewOneWithTheSameName(t *testing.T) {
	st := openTestStore(t)
	if _, err := st.CreateUser("admin", "h", RoleGlobal, Reach{}); err != nil {
		t.Fatal(err)
	}
	a, err := st.CreateUser("alice", "h", RoleGlobal, Reach{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession("old-token", a, sessionExpiry()); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := st.DeleteUser(a); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := st.CreateUser("alice", "h", RoleGlobal, Reach{}); err != nil {
		t.Fatal(err)
	}

	if _, found, err := st.LookupSession("old-token"); err != nil || found {
		t.Fatalf("LookupSession after the user was deleted and the name reused = found %t, err %v; want no session", found, err)
	}
}

// Renaming a user must not detach the sessions they already have.
func TestSessionsFollowTheUserThroughARename(t *testing.T) {
	st := openTestStore(t)
	id, err := st.CreateUser("alice", "h", RoleGlobal, Reach{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession("one", id, sessionExpiry()); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession("two", id, sessionExpiry()); err != nil {
		t.Fatal(err)
	}

	if err := st.UpdateUser(id, "alicia", "h", ""); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	for _, token := range []string{"one", "two"} {
		row, found, err := st.LookupSession(token)
		if err != nil || !found {
			t.Fatalf("session %q after the rename = found %t, err %v; want it kept", token, found, err)
		}
		if row.UserID != id {
			t.Errorf("session %q belongs to user %d after the rename, want %d", token, row.UserID, id)
		}
		if u, err := st.GetUser(row.UserID); err != nil || u.Username != "alicia" {
			t.Errorf("session %q resolves to %q (%v) after the rename, want alicia", token, u.Username, err)
		}
	}
}

// A password change signs out the other sessions of the person who changed it,
// not of anyone else.
func TestDeleteOtherSessionsTouchesOnlyThatUsersSessions(t *testing.T) {
	st := openTestStore(t)
	ids := map[string]int64{}
	for _, name := range []string{"alice", "bob"} {
		id, err := st.CreateUser(name, "h", RoleGlobal, Reach{})
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = id
	}
	for token, name := range map[string]string{"alice-1": "alice", "bob-1": "bob", "bob-2": "bob"} {
		if err := st.CreateSession(token, ids[name], sessionExpiry()); err != nil {
			t.Fatal(err)
		}
	}

	if err := st.DeleteOtherSessions(ids["bob"], "bob-1"); err != nil {
		t.Fatalf("DeleteOtherSessions: %v", err)
	}

	for token, want := range map[string]bool{"alice-1": true, "bob-1": true, "bob-2": false} {
		if _, found, _ := st.LookupSession(token); found != want {
			t.Errorf("session %q present = %t, want %t", token, found, want)
		}
	}
}

// Setting somebody's password from the user form ends all of their sessions.
func TestDeleteUserSessionsTouchesOnlyThatUser(t *testing.T) {
	st := openTestStore(t)
	alice, _ := st.CreateUser("alice", "h", RoleGlobal, Reach{})
	bob, _ := st.CreateUser("bob", "h", RoleGlobal, Reach{})
	for token, id := range map[string]int64{"alice-1": alice, "bob-1": bob, "bob-2": bob} {
		if err := st.CreateSession(token, id, sessionExpiry()); err != nil {
			t.Fatal(err)
		}
	}

	if err := st.DeleteUserSessions(bob); err != nil {
		t.Fatalf("DeleteUserSessions: %v", err)
	}

	for token, want := range map[string]bool{"alice-1": true, "bob-1": false, "bob-2": false} {
		if _, found, _ := st.LookupSession(token); found != want {
			t.Errorf("session %q present = %t, want %t", token, found, want)
		}
	}
}

// A session cannot be written for a user that does not exist.
func TestCreateSessionRefusesAMissingUser(t *testing.T) {
	st := openTestStore(t)
	if err := st.CreateSession("orphan", 42, sessionExpiry()); err == nil {
		t.Fatal("CreateSession for a missing user succeeded")
	}
}
