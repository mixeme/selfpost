package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
	"golang.org/x/crypto/bcrypt"
)

// sessionCookieName is the panel's cookie without TLS (auth.Config{} here).
const sessionCookieName = "selfpost_session"

// signedIn stores a session for the user the way a login does and returns the
// token the browser would hold.
func signedIn(t *testing.T, h *Handlers, username, token string) string {
	t.Helper()
	u, err := h.store.GetUserByUsername(username)
	if err != nil {
		t.Fatalf("get %s: %v", username, err)
	}
	sum := sha256.Sum256([]byte(token))
	if err := h.store.CreateSession(hex.EncodeToString(sum[:]), u.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create session of %s: %v", username, err)
	}
	return token
}

func hasSession(t *testing.T, h *Handlers, token string) bool {
	t.Helper()
	sum := sha256.Sum256([]byte(token))
	_, found, err := h.store.LookupSession(hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatalf("lookup session: %v", err)
	}
	return found
}

// postWithSession is postFormAs for a request that also carries the session
// cookie of the browser it comes from.
func postWithSession(h http.HandlerFunc, p auth.Principal, token, target string, pathValues map[string]string, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	rec := httptest.NewRecorder()
	h(rec, auth.RequestWithPrincipal(req, p))
	return rec
}

// addUser creates a global user with a known password and returns its principal.
func addUser(t *testing.T, h *Handlers, username, password string) auth.Principal {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	id, err := h.store.CreateUser(username, string(hash), store.RoleGlobal, store.Reach{})
	if err != nil {
		t.Fatalf("create %s: %v", username, err)
	}
	return auth.Principal{ID: id, Username: username, Role: auth.RoleGlobal}
}

// Changing your own password signs out your other sessions, and only yours:
// the Account page is open to every role, so it cannot reach into anybody
// else's login.
func TestAccountPasswordSignsOutOnlyTheUsersOtherSessions(t *testing.T) {
	h, _ := settingsServer(t)
	bob := addUser(t, h, "bob", "bobs-long-password-1")
	aliceCurrent := signedIn(t, h, "admin", "admin-current")
	bobCurrent := signedIn(t, h, "bob", "bob-current")
	bobOther := signedIn(t, h, "bob", "bob-other")

	const next = "bobs-new-long-password-2"
	rec := postWithSession(h.HandleAccountPassword, bob, bobCurrent, "/account/password", nil,
		url.Values{"current_password": {"bobs-long-password-1"}, "new_password": {next}, "new_password_confirm": {next}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /account/password = %d:\n%s", rec.Code, rec.Body.String())
	}

	if hasSession(t, h, bobOther) {
		t.Error("the user's other session survived their password change")
	}
	if !hasSession(t, h, bobCurrent) {
		t.Error("the session that changed the password was signed out")
	}
	if !hasSession(t, h, aliceCurrent) {
		t.Error("another user's session was signed out by someone else's password change")
	}
}

// An administrator who sets another user's password on the user form takes
// away every login that user had under the old one.
func TestUserFormPasswordSignsOutThatUsersSessions(t *testing.T) {
	h, _ := settingsServer(t)
	admin := globalPrincipal
	bob := addUser(t, h, "bob", "bobs-long-password-1")
	adminCurrent := signedIn(t, h, "admin", "admin-current")
	bob1 := signedIn(t, h, "bob", "bob-1")
	bob2 := signedIn(t, h, "bob", "bob-2")

	rec := postWithSession(h.HandleUserEdit, admin, adminCurrent, "/server/users/"+idStr(bob.ID), map[string]string{"uid": idStr(bob.ID)},
		url.Values{"username": {"bob"}, "role": {"global"}, "password": {"a-brand-new-long-password"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST user form = %d:\n%s", rec.Code, rec.Body.String())
	}

	if hasSession(t, h, bob1) || hasSession(t, h, bob2) {
		t.Error("the user's sessions survived an administrator setting their password")
	}
	if !hasSession(t, h, adminCurrent) {
		t.Error("the administrator's own session was signed out by editing someone else")
	}
}

// Saving the form without a new password does not sign anyone out.
func TestUserFormWithoutAPasswordKeepsTheSessions(t *testing.T) {
	h, _ := settingsServer(t)
	bob := addUser(t, h, "bob", "bobs-long-password-1")
	bob1 := signedIn(t, h, "bob", "bob-1")

	rec := postWithSession(h.HandleUserEdit, globalPrincipal, "no-session", "/server/users/"+idStr(bob.ID), map[string]string{"uid": idStr(bob.ID)},
		url.Values{"username": {"bob"}, "role": {"global"}, "email": {"bob@example.org"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST user form = %d:\n%s", rec.Code, rec.Body.String())
	}
	if !hasSession(t, h, bob1) {
		t.Error("an edit without a new password signed the user out")
	}
}

// An administrator who sets their own password through the user form is signed
// out everywhere except in the browser they are using.
func TestUserFormOwnPasswordKeepsTheCurrentSession(t *testing.T) {
	h, _ := settingsServer(t)
	current := signedIn(t, h, "admin", "admin-current")
	other := signedIn(t, h, "admin", "admin-other")

	rec := postWithSession(h.HandleUserEdit, globalPrincipal, current, "/server/users/1", map[string]string{"uid": "1"},
		url.Values{"username": {"admin"}, "role": {"global"}, "password": {"a-brand-new-long-password"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST user form = %d:\n%s", rec.Code, rec.Body.String())
	}

	if !hasSession(t, h, current) {
		t.Error("the administrator's current session was signed out")
	}
	if hasSession(t, h, other) {
		t.Error("the administrator's other session survived their own password change")
	}
}
