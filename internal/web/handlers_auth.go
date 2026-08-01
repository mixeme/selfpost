package web

import (
	"errors"
	"net/http"
	"strings"

	"codeberg.org/mix/selfpost/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// The panel session cookie's two possible names. In the production shape —
// TLS in front, so CookieSecure — it carries the __Host- prefix, which turns
// what the cookie's attributes merely promise into something the browser
// enforces: Secure, Path=/ and, the point of the exercise, no Domain
// attribute, so no other host may set a cookie by this name (phase 14.B).
// The prefix is only valid on a Secure cookie, so a development instance on
// plain HTTP has to keep the bare name: with the prefix the browser would
// discard the Set-Cookie outright and logging in would silently never stick.
const (
	sessionCookieBase     = "selfpost_session"
	sessionCookiePrefixed = "__Host-" + sessionCookieBase
)

// sessionCookie is the session cookie's name for this deployment.
func (s *Server) sessionCookie() string {
	if s.cfg.CookieSecure {
		return sessionCookiePrefixed
	}
	return sessionCookieBase
}

// sessionToken returns the session token the request carries, if exactly one
// cookie of that name is present.
//
// It walks r.Cookies() rather than calling r.Cookie, which silently returns
// the first match. Two cookies with the same name mean somebody other than
// this panel set one of them — a host on the same registrable domain can,
// with Domain=example.com, and the browser will then send both — and RFC 6265
// makes the older one come first, so "the first match" is precisely the
// attacker's. The value cannot be forged into a valid session, so the effect
// is denial of service, not compromise; refusing the request and saying so in
// the log is what makes it diagnosable instead of an endless login loop. The
// __Host- prefix prevents this outright, but only where it applies — this
// check also covers the plain-HTTP development shape (phase 14.B).
func (s *Server) sessionToken(r *http.Request) (string, bool) {
	name := s.sessionCookie()
	var token string
	var n int
	for _, c := range r.Cookies() {
		if c.Name == name {
			n++
			token = c.Value
		}
	}
	switch n {
	case 0:
		return "", false
	case 1:
		return token, true
	default:
		logf("panel: %s %s carries %d cookies named %q — treating the request as signed out; "+
			"another host on this domain is overwriting the session cookie, clear the cookies for the parent domain",
			r.Method, r.URL.Path, n, name)
		return "", false
	}
}

// clearSessionCookies expires the session cookie under both names, so an
// upgrade that switches to the __Host- prefix does not leave the old cookie
// sitting in the browser until it is closed.
func (s *Server) clearSessionCookies(w http.ResponseWriter) {
	for _, name := range []string{sessionCookieBase, sessionCookiePrefixed} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			// The prefixed name is only accepted at all when Secure is set,
			// including on the expiring copy.
			Secure:   s.cfg.CookieSecure || name == sessionCookiePrefixed,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

// handleLogin serves the login form (GET) and authenticates (POST). Until an
// administrator exists there is nobody to log in, so it points at setup.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	exists, err := s.store.AdminExists()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !exists {
		// No admin yet: login is meaningless. Send a clear message rather than
		// a failing form.
		s.render(w, http.StatusOK, "login", map[string]any{
			"Title":     "SelfPost — Sign in",
			"SetupHint": true,
		})
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.renderLogin(w, http.StatusOK, "")
	case http.MethodPost:
		s.submitLogin(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) renderLogin(w http.ResponseWriter, status int, formErr string) {
	s.render(w, status, "login", map[string]any{
		"Title": "SelfPost — Sign in",
		"Error": formErr,
	})
}

func (s *Server) submitLogin(w http.ResponseWriter, r *http.Request) {
	// Brute-force throttle by client IP (spec 7.6.5).
	if !s.loginLimiter.Allow(clientIP(r, s.trustedProxies)) {
		s.renderLogin(w, http.StatusTooManyRequests, "Too many attempts. Please wait and try again.")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderLogin(w, http.StatusBadRequest, "Invalid form submission.")
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")

	admin, err := s.store.GetAdmin()
	if err != nil {
		if !errors.Is(err, store.ErrNoAdmin) {
			logf("panel: login: get admin failed: %v", err)
		}
		s.renderLogin(w, http.StatusUnauthorized, "Invalid username or password.")
		return
	}

	// Always run bcrypt so timing does not distinguish "wrong user" from
	// "wrong password", and compare the username too.
	pwErr := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password))
	if username != admin.Username || pwErr != nil {
		s.renderLogin(w, http.StatusUnauthorized, "Invalid username or password.")
		return
	}

	token := s.sessions.Create(admin.Username)
	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCookie(),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleLogout destroys the session and clears the cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Destroy every token presented under the session cookie's name: if a
	// shadowing duplicate is present (see sessionToken) one of them is the
	// real session, and a value that names no session is simply not found.
	name := s.sessionCookie()
	for _, c := range r.Cookies() {
		if c.Name == name {
			s.sessions.Destroy(c.Value)
		}
	}
	s.clearSessionCookies(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
