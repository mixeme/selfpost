package web

import (
	"errors"
	"net/http"
	"strings"

	"codeberg.org/mix/selfpost/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// sessionCookie is the name of the panel session cookie.
const sessionCookie = "selfpost_session"

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
	if !s.loginLimiter.Allow(clientIP(r)) {
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
		Name:     sessionCookie,
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
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.Destroy(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
