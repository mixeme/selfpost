package auth

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	token := m.sessions.Create("admin")

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
	token := m.sessions.Create("admin")

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

func TestSessionLookupRejectsExpired(t *testing.T) {
	s := newTestSessionStore(t)
	s.idle = -time.Minute
	token := s.Create("admin")

	if _, ok := s.Lookup(token); ok {
		t.Fatal("expired session was accepted")
	}
}

func TestSessionTouchThrottled(t *testing.T) {
	s := newTestSessionStore(t)
	token := s.Create("admin")

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
