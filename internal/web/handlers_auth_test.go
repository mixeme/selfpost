package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The __Host- prefix is only valid on a Secure cookie: getting this condition
// backwards would make the development instance fail to log in at all, and
// silently — the browser discards the Set-Cookie and the panel just shows the
// login form again.
func TestSessionCookieNameFollowsCookieSecure(t *testing.T) {
	secure := &Server{cfg: Config{CookieSecure: true}}
	if got := secure.sessionCookie(); got != "__Host-selfpost_session" {
		t.Errorf("with TLS the cookie is named %q, want the __Host- prefixed name", got)
	}
	plain := &Server{cfg: Config{CookieSecure: false}}
	if got := plain.sessionCookie(); got != "selfpost_session" {
		t.Errorf("without TLS the cookie is named %q, want the bare name", got)
	}
}

// A neighbouring host on the same registrable domain can set a cookie by the
// same name; the browser then sends both, oldest first. Picking one at random
// would leave the administrator in a login loop with no explanation, so the
// request counts as signed out instead.
func TestSessionTokenRejectsDuplicates(t *testing.T) {
	s := &Server{cfg: Config{CookieSecure: false}}
	r := httptest.NewRequest(http.MethodGet, "http://panel.example.com/domains", nil)
	r.AddCookie(&http.Cookie{Name: "selfpost_session", Value: "planted-by-a-neighbour"})
	r.AddCookie(&http.Cookie{Name: "selfpost_session", Value: "the-real-session"})

	if token, ok := s.sessionToken(r); ok {
		t.Fatalf("duplicate cookies accepted, token = %q", token)
	}
}

func TestSessionTokenReadsOneCookie(t *testing.T) {
	s := &Server{cfg: Config{CookieSecure: true}}
	r := httptest.NewRequest(http.MethodGet, "http://panel.example.com/domains", nil)
	r.AddCookie(&http.Cookie{Name: "__Host-selfpost_session", Value: "the-real-session"})

	token, ok := s.sessionToken(r)
	if !ok || token != "the-real-session" {
		t.Fatalf("sessionToken = %q, %t; want the cookie's value", token, ok)
	}
}

// A cookie under the other deployment's name is not this deployment's session:
// after an upgrade the pre-14 cookie must not be honoured as if it were the
// prefixed one.
func TestSessionTokenIgnoresTheOtherName(t *testing.T) {
	s := &Server{cfg: Config{CookieSecure: true}}
	r := httptest.NewRequest(http.MethodGet, "http://panel.example.com/domains", nil)
	r.AddCookie(&http.Cookie{Name: "selfpost_session", Value: "left-over-from-an-older-build"})

	if _, ok := s.sessionToken(r); ok {
		t.Fatal("the unprefixed cookie was accepted on a TLS deployment")
	}
}

func TestRequireAuthRejectsDuplicateCookies(t *testing.T) {
	s := &Server{cfg: Config{CookieSecure: false}, sessions: newSessionStore()}
	token := s.sessions.Create("admin")

	reached := false
	h := s.requireAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

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

// Signing out has to expire the cookie under both names, or the cookie left
// over from a pre-__Host- build stays in the browser for the rest of its life.
func TestLogoutClearsBothCookieNames(t *testing.T) {
	s := &Server{cfg: Config{CookieSecure: true}, sessions: newSessionStore()}
	token := s.sessions.Create("admin")

	r := httptest.NewRequest(http.MethodPost, "http://panel.example.com/logout", nil)
	r.Host = "panel.example.com"
	r.AddCookie(&http.Cookie{Name: "__Host-selfpost_session", Value: token})
	rec := httptest.NewRecorder()
	s.handleLogout(rec, r)

	if _, ok := s.sessions.Lookup(token); ok {
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
