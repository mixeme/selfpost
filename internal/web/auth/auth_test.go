package auth

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/view"
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

// mustCreate signs the named user in, creating the user first if there is none
// by that name.
func mustCreate(t *testing.T, s *sessionStore, username string) string {
	t.Helper()
	u, err := s.store.GetUserByUsername(username)
	if errors.Is(err, store.ErrUserNotFound) {
		var id int64
		id, err = s.store.CreateUser(username, "hash", store.RoleGlobal, store.Reach{})
		u.ID = id
	}
	if err != nil {
		t.Fatalf("user %q: %v", username, err)
	}
	token, err := s.Create(u.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return token
}

func mustView(t *testing.T) *view.Engine {
	t.Helper()
	v, err := view.New("test")
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	return v
}

func testModule(t *testing.T, cookieSecure bool) *Module {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, Config{CookieSecure: cookieSecure}, mustView(t), "")
}

func TestSessionCookieNameFollowsCookieSecure(t *testing.T) {
	secure := testModule(t, true)
	if got := secure.sessionCookie(); got != "__Host-selfpost_session" {
		t.Errorf("with TLS the cookie is named %q, want the __Host- prefixed name", got)
	}
	plain := testModule(t, false)
	if got := plain.sessionCookie(); got != "selfpost_session" {
		t.Errorf("without TLS the cookie is named %q, want the bare name", got)
	}
}

func TestSessionTokenRejectsDuplicates(t *testing.T) {
	m := testModule(t, false)
	r := httptest.NewRequest(http.MethodGet, "http://panel.example.com/domains", nil)
	r.AddCookie(&http.Cookie{Name: "selfpost_session", Value: "planted-by-a-neighbour"})
	r.AddCookie(&http.Cookie{Name: "selfpost_session", Value: "the-real-session"})

	if token, ok := m.sessionToken(r); ok {
		t.Fatalf("duplicate cookies accepted, token = %q", token)
	}
}

func TestSessionTokenReadsOneCookie(t *testing.T) {
	m := testModule(t, true)
	r := httptest.NewRequest(http.MethodGet, "http://panel.example.com/domains", nil)
	r.AddCookie(&http.Cookie{Name: "__Host-selfpost_session", Value: "the-real-session"})

	token, ok := m.sessionToken(r)
	if !ok || token != "the-real-session" {
		t.Fatalf("sessionToken = %q, %t; want the cookie's value", token, ok)
	}
}

func TestSessionTokenIgnoresTheOtherName(t *testing.T) {
	m := testModule(t, true)
	r := httptest.NewRequest(http.MethodGet, "http://panel.example.com/domains", nil)
	r.AddCookie(&http.Cookie{Name: "selfpost_session", Value: "left-over-from-an-older-build"})

	if _, ok := m.sessionToken(r); ok {
		t.Fatal("the unprefixed cookie was accepted on a TLS deployment")
	}
}

func TestRequireAuthRejectsDuplicateCookies(t *testing.T) {
	m := testModule(t, false)
	token := mustCreate(t, m.sessions, "admin")

	reached := false
	h := m.RequireAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	r := httptest.NewRequest(http.MethodGet, "http://panel.example.com/domains", nil)
	r.AddCookie(&http.Cookie{Name: "selfpost_session", Value: "planted-by-a-neighbour"})
	r.AddCookie(&http.Cookie{Name: "selfpost_session", Value: token})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if reached {
		t.Fatal("the handler ran even though the session cookie was shadowed")
	}
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("status = %d, Location = %q; want a redirect to /login", rec.Code, rec.Header().Get("Location"))
	}
}

func TestLogoutClearsBothCookieNames(t *testing.T) {
	m := testModule(t, true)
	token := mustCreate(t, m.sessions, "admin")

	r := httptest.NewRequest(http.MethodPost, "http://panel.example.com/logout", nil)
	r.Host = "panel.example.com"
	r.AddCookie(&http.Cookie{Name: "__Host-selfpost_session", Value: token})
	rec := httptest.NewRecorder()
	m.HandleLogout(rec, r)

	if _, ok := m.sessions.Lookup(token); ok {
		t.Error("the session survived sign-out")
	}
	set := rec.Header().Values("Set-Cookie")
	for _, name := range []string{"selfpost_session=", "__Host-selfpost_session="} {
		var found bool
		for _, c := range set {
			if strings.HasPrefix(c, name) && strings.Contains(c, "Max-Age=0") {
				found = true
			}
		}
		if !found {
			t.Errorf("sign-out does not expire a cookie named %q: %v", strings.TrimSuffix(name, "="), set)
		}
	}
}

// A session that could not be stored must not turn into a cookie: the browser
// would look signed in, and every request it made would be bounced to /login
// with no explanation. Only the sessions table is broken here, so the request
// gets past the user lookup and password check and fails exactly where the
// session is written.
func TestLoginSetsNoCookieWhenTheSessionCannotBeStored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-horse-battery"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := st.CreateGlobalUser("admin", string(hash)); err != nil {
		t.Fatalf("create user: %v", err)
	}
	dropSessionsTable(t, path)

	m := New(st, Config{}, mustView(t), "")
	form := url.Values{"username": {"admin"}, "password": {"correct-horse-battery"}}
	r := httptest.NewRequest(http.MethodPost, "http://panel.example.com/login",
		strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	m.HandleLogin(rec, r)

	if got := rec.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("a session cookie was issued for a session that was never stored: %v", got)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (the login failed)", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("the browser was sent to %q as if it were signed in", loc)
	}
}

// dropSessionsTable breaks session persistence while leaving the rest of the
// schema usable. The SQLite driver is registered by internal/store.
func dropSessionsTable(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open database directly: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("DROP TABLE sessions"); err != nil {
		t.Fatalf("drop sessions table: %v", err)
	}
}

func TestSessionDestroyOthers(t *testing.T) {
	s := newTestSessionStore(t)
	keep := mustCreate(t, s, "admin")
	other := mustCreate(t, s, "admin")
	elsewhere := mustCreate(t, s, "operator")
	admin, err := s.store.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}

	s.DestroyOthers(admin.ID, keep)

	if _, ok := s.Lookup(keep); !ok {
		t.Fatal("current session was destroyed")
	}
	if _, ok := s.Lookup(other); ok {
		t.Fatal("other session survived")
	}
	if _, ok := s.Lookup(elsewhere); !ok {
		t.Fatal("another user's session was destroyed")
	}
}

func TestSessionLookupRejectsExpired(t *testing.T) {
	s := newTestSessionStore(t)
	s.idle = -time.Minute
	token := mustCreate(t, s, "admin")

	if _, ok := s.Lookup(token); ok {
		t.Fatal("expired session was accepted")
	}
}

func TestSessionTouchThrottled(t *testing.T) {
	s := newTestSessionStore(t)
	token := mustCreate(t, s, "admin")

	if s.Touch(token) {
		t.Fatal("touch renewed a session created moments ago")
	}

	if err := s.store.RenewSession(hashToken(token), time.Now().Add(-2*time.Hour).Add(s.idle)); err != nil {
		t.Fatalf("renew session: %v", err)
	}
	if !s.Touch(token) {
		t.Fatal("touch did not renew a session past the throttle window")
	}
}

// requireAuthAs runs one request carrying token through RequireAuth and
// returns the response and the principal the handler saw (nil when it never
// ran). It is a POST, an activity request of the kind that renews a session.
func requireAuthAs(t *testing.T, m *Module, token string) (*httptest.ResponseRecorder, *Principal) {
	t.Helper()
	var seen *Principal
	h := m.RequireAuth(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if p, ok := CurrentPrincipal(r.Context()); ok {
			seen = &p
		}
	}))
	r := httptest.NewRequest(http.MethodPost, "http://panel.example.com/domains", nil)
	r.AddCookie(&http.Cookie{Name: m.sessionCookie(), Value: token})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec, seen
}

func mustUser(t *testing.T, m *Module, username string) int64 {
	t.Helper()
	id, err := m.store.CreateUser(username, "hash", store.RoleGlobal, store.Reach{})
	if err != nil {
		t.Fatalf("create user %q: %v", username, err)
	}
	return id
}

// A session is the person's, not the name's: when the user is deleted the
// cookie is dead, and a new user created under the same name does not inherit
// it (with that account's role).
func TestRequireAuthDoesNotRebindASessionToANewUserWithTheSameName(t *testing.T) {
	m := testModule(t, false)
	mustUser(t, m, "admin")
	id := mustUser(t, m, "alice")
	token := mustCreate(t, m.sessions, "alice")

	if err := m.store.DeleteUser(id); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	mustUser(t, m, "alice")

	rec, seen := requireAuthAs(t, m, token)
	if seen != nil {
		t.Fatalf("the old cookie signed in as %q after the user was deleted and the name reused", seen.Username)
	}
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("status = %d, Location = %q; want a redirect to /login", rec.Code, rec.Header().Get("Location"))
	}
}

// Renaming a user (the Account page, or an administrator on the user form)
// leaves every session of theirs signed in, under the new name.
func TestRenamingAUserKeepsTheirOtherSessions(t *testing.T) {
	m := testModule(t, false)
	id := mustUser(t, m, "alice")
	current := mustCreate(t, m.sessions, "alice")
	other := mustCreate(t, m.sessions, "alice")

	if err := m.store.UpdateUser(id, "alicia", "hash", ""); err != nil {
		t.Fatalf("rename: %v", err)
	}

	for name, token := range map[string]string{"current": current, "other": other} {
		_, seen := requireAuthAs(t, m, token)
		if seen == nil {
			t.Errorf("the %s session was lost by the rename", name)
			continue
		}
		if seen.Username != "alicia" || seen.ID != id {
			t.Errorf("the %s session is %q (id %d) after the rename, want alicia (id %d)", name, seen.Username, seen.ID, id)
		}
	}
}

// A session whose user is gone is not renewed by being used: the renewal would
// keep a dead cookie alive for as long as someone presses it.
func TestRequireAuthDoesNotRenewASessionWhoseUserIsGone(t *testing.T) {
	m := testModule(t, false)
	mustUser(t, m, "admin")
	id := mustUser(t, m, "alice")
	token := mustCreate(t, m.sessions, "alice")
	// Past the renewal throttle, so a Touch would write.
	stale := time.Now().Add(-2 * time.Hour).Add(m.sessions.idle)
	if err := m.store.RenewSession(hashToken(token), stale); err != nil {
		t.Fatalf("age session: %v", err)
	}
	if err := m.store.DeleteUser(id); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	rec, _ := requireAuthAs(t, m, token)

	if got := rec.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("a session of a missing user was renewed: %v", got)
	}
	if row, found, _ := m.store.LookupSession(hashToken(token)); found && !row.ExpiresAt.Equal(stale.UTC().Truncate(time.Second)) {
		t.Errorf("expiry of a missing user's session moved to %v", row.ExpiresAt)
	}
}
