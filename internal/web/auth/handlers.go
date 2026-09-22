package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/validate"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookieBase     = "selfpost_session"
	sessionCookiePrefixed = "__Host-" + sessionCookieBase
)

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
	exists, err := m.store.AdminExists()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !exists {
		m.view.Render(w, http.StatusOK, "login", map[string]any{
			"Title":     "SelfPost — Sign in",
			"Active":    "login",
			"SetupHint": true,
		})
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
	m.view.Render(w, status, "login", map[string]any{
		"Title":  "SelfPost — Sign in",
		"Active": "login",
		"Error":  formErr,
	})
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

	admin, err := m.store.GetAdmin()
	if err != nil {
		if !errors.Is(err, store.ErrNoAdmin) {
			logf("panel: login: get admin failed: %v", err)
		}
		m.renderLogin(w, http.StatusUnauthorized, "Invalid username or password.")
		return
	}

	pwErr := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password))
	if username != admin.Username || pwErr != nil {
		m.renderLogin(w, http.StatusUnauthorized, "Invalid username or password.")
		return
	}

	token := m.sessions.Create(admin.Username)
	m.setSessionCookie(w, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

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
	m.view.Render(w, status, "setup", map[string]any{
		"Title":  "SelfPost — Create administrator",
		"Active": "setup",
		"Token":  token,
		"Error":  formErr,
	})
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

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		logf("panel: setup: hashing password failed: %v", err)
		m.renderSetupForm(w, http.StatusInternalServerError, token, "Internal error. Please try again.")
		return
	}

	if err := m.store.CreateAdmin(username, string(hash)); err != nil {
		if exists, _ := m.store.AdminExists(); exists {
			m.setup.complete()
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		logf("panel: setup: create admin failed: %v", err)
		m.renderSetupForm(w, http.StatusInternalServerError, token, "Internal error. Please try again.")
		return
	}

	m.setup.complete()
	logf("panel: administrator %q created; setup link is now disabled", username)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
