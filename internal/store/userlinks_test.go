package store

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func linkTestUser(t *testing.T, st *Store, email string) User {
	t.Helper()
	if err := st.CreateGlobalUser("admin", "hash"); err != nil {
		t.Fatal(err)
	}
	u, err := st.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateUser(u.ID, u.Username, u.PasswordHash, email); err != nil {
		t.Fatal(err)
	}
	u, _ = st.GetUser(u.ID)
	return u
}

// A link is taken once, not after it expires, and only for its purpose; a
// newer one of the same purpose replaces it.
func TestUserLinks(t *testing.T) {
	st := openTestStore(t)
	u := linkTestUser(t, st, "admin@example.org")
	now := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)

	if err := st.CreateUserLink("h1", u.ID, LinkReset, u.Email, now, now.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PeekUserLink("h1", LinkConfirm, now); !errors.Is(err, ErrNoLink) {
		t.Errorf("a reset link taken as a confirmation one: %v", err)
	}
	if l, err := st.PeekUserLink("h1", LinkReset, now); err != nil || l.UserID != u.ID || l.Email != u.Email {
		t.Fatalf("peek = %+v, %v", l, err)
	}
	if _, err := st.PeekUserLink("h1", LinkReset, now.Add(30*time.Minute)); !errors.Is(err, ErrNoLink) {
		t.Errorf("an expired link was valid: %v", err)
	}
	// A second reset link leaves the first working: a request of anyone's
	// must not kill the link in the user's mailbox.
	if err := st.CreateUserLink("h2", u.ID, LinkReset, u.Email, now.Add(time.Minute), now.Add(31*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if l, err := st.LatestUserLink(u.ID, LinkReset, now.Add(2*time.Minute)); err != nil || !l.CreatedAt.Equal(now.Add(time.Minute)) {
		t.Errorf("latest = %+v, %v", l, err)
	}
	if _, err := st.TakeUserLink("h1", LinkReset, now.Add(2*time.Minute)); err != nil {
		t.Errorf("the first reset link died with the second: %v", err)
	}
	if _, err := st.TakeUserLink("h2", LinkReset, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("take: %v", err)
	}
	if _, err := st.TakeUserLink("h2", LinkReset, now.Add(3*time.Minute)); !errors.Is(err, ErrNoLink) {
		t.Errorf("a link was taken twice: %v", err)
	}
	if _, err := st.LatestUserLink(u.ID, LinkReset, now.Add(3*time.Minute)); !errors.Is(err, ErrNoLink) {
		t.Errorf("a taken link is still the latest: %v", err)
	}
	// A second confirmation link replaces the first: it confirms the
	// address saved last.
	if err := st.CreateUserLink("c1", u.ID, LinkConfirm, u.Email, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUserLink("c2", u.ID, LinkConfirm, u.Email, now.Add(time.Minute), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.TakeUserLink("c1", LinkConfirm, now.Add(2*time.Minute)); !errors.Is(err, ErrNoLink) {
		t.Errorf("the replaced confirmation link was taken: %v", err)
	}
}

// A reset link sets the password only while the account's address is still
// the confirmed one it was sent to; then it and the user's other reset links
// are gone. A link that does not hold changes nothing and is not used up.
func TestTakeResetLink(t *testing.T) {
	st := openTestStore(t)
	u := linkTestUser(t, st, "admin@example.org")
	now := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	for _, h := range []string{"r1", "r2"} {
		if err := st.CreateUserLink(h, u.ID, LinkReset, u.Email, now, now.Add(30*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	// Not confirmed yet: refused, and the link is still there.
	if _, err := st.TakeResetLink("r1", "new-hash", now); !errors.Is(err, ErrNoLink) {
		t.Fatalf("an unconfirmed address: %v", err)
	}
	if _, err := st.PeekUserLink("r1", LinkReset, now); err != nil {
		t.Fatalf("a refused take used the link up: %v", err)
	}
	if ok, _ := st.ConfirmEmail(u.ID, u.Email, now); !ok {
		t.Fatal("confirm")
	}
	id, err := st.TakeResetLink("r1", "new-hash", now)
	if err != nil || id != u.ID {
		t.Fatalf("take = %d, %v", id, err)
	}
	if got, _ := st.GetUser(u.ID); got.PasswordHash != "new-hash" {
		t.Errorf("hash = %q", got.PasswordHash)
	}
	if _, err := st.PeekUserLink("r2", LinkReset, now); !errors.Is(err, ErrNoLink) {
		t.Errorf("another reset link survived the reset: %v", err)
	}
	// A link of an address the account no longer has does not hold.
	if err := st.CreateUserLink("r3", u.ID, LinkReset, u.Email, now, now.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateUser(u.ID, u.Username, "new-hash", "other@example.org"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.TakeResetLink("r3", "third-hash", now); !errors.Is(err, ErrNoLink) {
		t.Errorf("a link of the old address set the password: %v", err)
	}
	if err := st.DeleteAllUserLinks(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PeekUserLink("r3", LinkReset, now); !errors.Is(err, ErrNoLink) {
		t.Errorf("a link survived DeleteAllUserLinks: %v", err)
	}
}

// Of two requests with one link, one takes it.
func TestUserLinkTakenOnce(t *testing.T) {
	st := openTestStore(t)
	u := linkTestUser(t, st, "admin@example.org")
	now := time.Now()
	if err := st.CreateUserLink("h", u.ID, LinkReset, u.Email, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	took := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.TakeUserLink("h", LinkReset, now); err == nil {
				mu.Lock()
				took++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if took != 1 {
		t.Errorf("the link was taken %d times", took)
	}
}

// The mark follows the address: confirmed only while it is the address the
// link was sent to, cleared when the address changes, kept when it does not.
func TestEmailConfirmation(t *testing.T) {
	st := openTestStore(t)
	u := linkTestUser(t, st, "admin@example.org")
	if !u.EmailConfirmed.IsZero() {
		t.Fatal("a new address is confirmed")
	}
	now := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	if ok, err := st.ConfirmEmail(u.ID, "old@example.org", now); err != nil || ok {
		t.Errorf("another address confirmed: %v %v", ok, err)
	}
	if ok, err := st.ConfirmEmail(u.ID, "admin@example.org", now); err != nil || !ok {
		t.Fatalf("confirm: %v %v", ok, err)
	}
	u, _ = st.GetUser(u.ID)
	if !u.EmailConfirmed.Equal(now) {
		t.Fatalf("confirmed at %v", u.EmailConfirmed)
	}
	// Saving the same address keeps the mark; another clears it.
	if err := st.UpdateUser(u.ID, "admin2", u.PasswordHash, u.Email); err != nil {
		t.Fatal(err)
	}
	if u, _ = st.GetUser(u.ID); u.EmailConfirmed.IsZero() {
		t.Error("the same address lost its mark")
	}
	if err := st.SaveUser(UserChange{ID: u.ID, Username: u.Username, Email: "new@example.org", Role: RoleGlobal}); err != nil {
		t.Fatal(err)
	}
	if u, _ = st.GetUser(u.ID); !u.EmailConfirmed.IsZero() {
		t.Error("a new address kept the old one's mark")
	}
	if ok, _ := st.ConfirmEmail(u.ID, "admin@example.org", now); ok {
		t.Error("a link of the old address confirmed the new one")
	}
}
