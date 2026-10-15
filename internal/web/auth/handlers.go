package auth

import (
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/validate"
	"github.com/mixeme/selfpost/internal/web/view"
	"golang.org/x/crypto/bcrypt"
)

// draft-ietf-httpbis-rfc6265bis § 4.1.3.2: a cookie named __Host- is accepted
// only with Secure, Path=/ and no Domain, from a secure origin - so no other
// host can set or overwrite it, and clearing it needs Secure as well.
const (
	sessionCookieBase     = "selfpost_session"
	sessionCookiePrefixed = "__Host-" + sessionCookieBase
)

// BcryptCost is the work factor for every panel password hash — the setup
// account created here and the ones the settings and user pages write
// (security.md). bcrypt records the cost inside the hash, so raising it only
// affects passwords set from now on; existing hashes keep verifying at the
// cost they were written with.
const BcryptCost = 12

func (m *Module) sessionCookie() string {
	if m.cfg.CookieSecure {
		return sessionCookiePrefixed
	}
	return sessionCookieBase
}

func (m *Module) sessionToken(r *http.Request) (string, bool) {
	name := m.sessionCookie()
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
		// RFC 6265 § 5.4: the Cookie header carries every cookie that
		// matches, a parent domain's of the same name included. The prefixed
		// name cannot be planted that way (see sessionCookiePrefixed).
		logf("panel: %s %s carries %d cookies named %q — treating the request as signed out; "+
			"another host on this domain is overwriting the session cookie, clear the cookies for the parent domain",
			r.Method, r.URL.Path, n, name)
		return "", false
	}
}

func (m *Module) clearSessionCookies(w http.ResponseWriter) {
	for _, name := range []string{sessionCookieBase, sessionCookiePrefixed} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   m.cfg.CookieSecure || name == sessionCookiePrefixed,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

// HandleLogin serves the login form (GET) and authenticates (POST).
func (m *Module) HandleLogin(w http.ResponseWriter, r *http.Request) {
	exists, err := m.store.UserExists()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !exists {
		m.view.Render(w, http.StatusOK, "login", view.NewLoginSetupHint(m.cfg.Hostname))
		return
	}

	switch r.Method {
	case http.MethodGet:
		m.renderLogin(w, http.StatusOK, "")
	case http.MethodPost:
		m.submitLogin(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (m *Module) renderLogin(w http.ResponseWriter, status int, formErr string) {
	m.view.Render(w, status, "login", view.NewLogin(m.cfg.Hostname, formErr))
}

func (m *Module) submitLogin(w http.ResponseWriter, r *http.Request) {
	if !m.loginLimiter.Allow(clientIP(r, m.trustedProxies)) {
		m.renderLogin(w, http.StatusTooManyRequests, "Too many attempts. Please wait and try again.")
		return
	}
	if err := r.ParseForm(); err != nil {
		m.renderLogin(w, http.StatusBadRequest, "Invalid form submission.")
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")

	user, err := m.store.GetUserByUsername(username)
	if err != nil {
		if !errors.Is(err, store.ErrUserNotFound) {
			logf("panel: login: get user failed: %v", err)
		}
		// A comparison anyway, so an unknown name is refused in the time
		// a wrong password is: the answer's delay told which names exist
		// (audit 2026-10-12, M8).
		_ = bcrypt.CompareHashAndPassword(noSuchUserHash(), []byte(password))
		m.renderLogin(w, http.StatusUnauthorized, "Invalid username or password.")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		m.renderLogin(w, http.StatusUnauthorized, "Invalid username or password.")
		return
	}

	// The session the browser presents, if any, ends here: a sign-in issues
	// a new token and leaves no other behind it - one planted before the
	// sign-in is worth nothing after (audit 2026-10-12, L16).
	name := m.sessionCookie()
	for _, c := range r.Cookies() {
		if c.Name == name {
			m.sessions.Destroy(c.Value)
		}
	}
	token, err := m.sessions.Create(user.ID)
	if err != nil {
		logf("panel: login: create session failed: %v", err)
		m.renderLogin(w, http.StatusInternalServerError, "Internal error. Please try again.")
		return
	}
	m.setSessionCookie(w, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// noSuchUserHash is a hash of the panel's work factor, compared against when
// a sign-in names no user.
var noSuchUserHash = sync.OnceValue(func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("selfpost: no such user"), BcryptCost)
	if err != nil {
		panic(err)
	}
	return h
})

// setSessionCookie sets the session cookie. SameSite=Lax withholds it from a
// cross-site request unless that is a top-level navigation with a safe method
// (rfc6265bis § 5.8.3, § 5.6.7.1), so a cross-site POST arrives without it.
func (m *Module) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.sessionCookie(),
		Value:    token,
		Path:     "/",
		MaxAge:   m.sessions.MaxAge(),
		HttpOnly: true,
		Secure:   m.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// HandleLogout destroys the session and clears the cookie.
func (m *Module) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := m.sessionCookie()
	for _, c := range r.Cookies() {
		if c.Name == name {
			m.sessions.Destroy(c.Value)
		}
	}
	m.clearSessionCookies(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// HandleSetup serves the one-time administrator creation flow at
// /setup/<token> (security.md).
func (m *Module) HandleSetup(w http.ResponseWriter, r *http.Request) {
	if !m.setupLimiter.Allow(clientIP(r, m.trustedProxies)) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}

	token := strings.TrimPrefix(r.URL.Path, "/setup/")
	if token == "" || strings.Contains(token, "/") {
		http.NotFound(w, r)
		return
	}
	if !m.setup.validate(token) {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		m.renderSetupForm(w, http.StatusOK, token, "")
	case http.MethodPost:
		m.submitSetup(w, r, token)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (m *Module) renderSetupForm(w http.ResponseWriter, status int, token, formErr string) {
	m.view.Render(w, status, "setup", view.NewSetup(token, formErr))
}

func (m *Module) submitSetup(w http.ResponseWriter, r *http.Request, token string) {
	if err := r.ParseForm(); err != nil {
		m.renderSetupForm(w, http.StatusBadRequest, token, "Invalid form submission.")
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("password_confirm")

	if err := validate.Username(username); err != nil {
		m.renderSetupForm(w, http.StatusBadRequest, token, err.Error())
		return
	}
	if password != confirm {
		m.renderSetupForm(w, http.StatusBadRequest, token, "Passwords do not match.")
		return
	}
	if err := validate.AdminPassword(password); err != nil {
		m.renderSetupForm(w, http.StatusBadRequest, token, err.Error())
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		logf("panel: setup: hashing password failed: %v", err)
		m.renderSetupForm(w, http.StatusInternalServerError, token, "Internal error. Please try again.")
		return
	}

	if err := m.store.CreateGlobalUser(username, string(hash)); err != nil {
		if exists, _ := m.store.UserExists(); exists {
			m.setup.complete()
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		logf("panel: setup: create user failed: %v", err)
		m.renderSetupForm(w, http.StatusInternalServerError, token, "Internal error. Please try again.")
		return
	}

	m.setup.complete()
	logf("panel: administrator %q created; setup link is now disabled", username)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
