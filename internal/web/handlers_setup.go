package web

import (
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// handleSetup serves the one-time administrator creation flow at
// /setup/<token> (spec 7.6.1). Once an administrator exists the whole route
// returns 404; an invalid or expired token is indistinguishable from a missing
// page, also 404.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	// Route-specific rate limit, separate from login (spec 7.6.1).
	if !s.setupLimiter.Allow(clientIP(r, s.trustedProxies)) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}

	token := strings.TrimPrefix(r.URL.Path, "/setup/")
	// Reject nested/garbage paths outright.
	if token == "" || strings.Contains(token, "/") {
		http.NotFound(w, r)
		return
	}
	if !s.setup.validate(token) {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.renderSetupForm(w, http.StatusOK, token, "")
	case http.MethodPost:
		s.submitSetup(w, r, token)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) renderSetupForm(w http.ResponseWriter, status int, token, formErr string) {
	s.render(w, status, "setup", map[string]any{
		"Title": "SelfPost — Create administrator",
		"Token": token,
		"Error": formErr,
	})
}

func (s *Server) submitSetup(w http.ResponseWriter, r *http.Request, token string) {
	if err := r.ParseForm(); err != nil {
		s.renderSetupForm(w, http.StatusBadRequest, token, "Invalid form submission.")
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("password_confirm")

	if err := validateUsername(username); err != nil {
		s.renderSetupForm(w, http.StatusBadRequest, token, err.Error())
		return
	}
	if password != confirm {
		s.renderSetupForm(w, http.StatusBadRequest, token, "Passwords do not match.")
		return
	}
	if err := validateAdminPassword(password); err != nil {
		s.renderSetupForm(w, http.StatusBadRequest, token, err.Error())
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		logf("panel: setup: hashing password failed: %v", err)
		s.renderSetupForm(w, http.StatusInternalServerError, token, "Internal error. Please try again.")
		return
	}

	if err := s.store.CreateAdmin(username, string(hash)); err != nil {
		// A concurrent submission may have already created the admin; the
		// id=1 / non-empty-table guard makes this the second writer. Treat it
		// as "setup already done" rather than an error.
		if exists, _ := s.store.AdminExists(); exists {
			s.setup.complete()
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		logf("panel: setup: create admin failed: %v", err)
		s.renderSetupForm(w, http.StatusInternalServerError, token, "Internal error. Please try again.")
		return
	}

	// Setup is now permanently complete: burn the token (spec 7.6.1).
	s.setup.complete()
	logf("panel: administrator %q created; setup link is now disabled", username)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
