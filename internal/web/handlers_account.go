package web

import (
	"errors"
	"net/http"
	"strings"

	"codeberg.org/mix/selfpost/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// handleAccount serves the administrator's own account settings: the username
// and password chosen during setup are the only panel credentials (spec 7.6.1),
// and until now they could be changed only by recreating the state. Changing
// them here never touches application SASL logins, which are a separate
// identity system (spec 5.1).
func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.renderAccount(w, r, http.StatusOK, "", currentUser(r))
	case http.MethodPost:
		s.submitAccount(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// renderAccount draws the settings form. formUsername repopulates the username
// field after a rejected submission; the password fields are never repopulated.
func (s *Server) renderAccount(w http.ResponseWriter, r *http.Request, status int, formErr, formUsername string) {
	s.render(w, status, "account", map[string]any{
		"Title":        "SelfPost — account",
		"User":         currentUser(r),
		"Active":       "account",
		"FormUsername": formUsername,
		"Error":        formErr,
		"Flash":        accountFlash(r),
	})
}

// accountFlash maps a fixed redirect flag to a fixed message, so status text
// after a redirect is never attacker-influenced.
func accountFlash(r *http.Request) string {
	switch r.URL.Query().Get("updated") {
	case "username":
		return "Username changed."
	case "password":
		return "Password changed. Any other signed-in sessions were signed out."
	case "both":
		return "Username and password changed. Any other signed-in sessions were signed out."
	default:
		return ""
	}
}

// submitAccount applies a username and/or password change. The current password
// is always required, so a stolen session alone cannot lock the administrator
// out of their own panel, and the attempt is throttled on the same limiter as
// the login form so this route cannot be used to brute-force the password past
// that limit (spec 7.6.5).
func (s *Server) submitAccount(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimiter.Allow(clientIP(r, s.trustedProxies)) {
		s.renderAccount(w, r, http.StatusTooManyRequests,
			"Too many attempts. Please wait and try again.", currentUser(r))
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderAccount(w, r, http.StatusBadRequest, "Invalid form submission.", currentUser(r))
		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	current := r.PostFormValue("current_password")
	password := r.PostFormValue("new_password")
	confirm := r.PostFormValue("new_password_confirm")

	admin, err := s.store.GetAdmin()
	if err != nil {
		logf("panel: account: get admin failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if username == "" {
		username = admin.Username
	}

	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(current)); err != nil {
		s.renderAccount(w, r, http.StatusUnauthorized, "Current password is incorrect.", username)
		return
	}

	renaming := username != admin.Username
	if renaming {
		if err := validateUsername(username); err != nil {
			s.renderAccount(w, r, http.StatusBadRequest, err.Error(), username)
			return
		}
	}

	// An empty pair of new-password fields means "leave the password alone", so
	// the username can be changed on its own.
	repassword := password != "" || confirm != ""
	if repassword {
		if password != confirm {
			s.renderAccount(w, r, http.StatusBadRequest, "New passwords do not match.", username)
			return
		}
		if err := validateAdminPassword(password); err != nil {
			s.renderAccount(w, r, http.StatusBadRequest, err.Error(), username)
			return
		}
	}
	if !renaming && !repassword {
		s.renderAccount(w, r, http.StatusBadRequest,
			"Nothing to change: enter a new username, a new password, or both.", username)
		return
	}

	hash := admin.PasswordHash
	if repassword {
		newHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			logf("panel: account: hashing password failed: %v", err)
			s.renderAccount(w, r, http.StatusInternalServerError,
				"Internal error. Please try again.", username)
			return
		}
		hash = string(newHash)
	}

	if err := s.store.UpdateAdmin(username, hash); err != nil {
		logf("panel: account: update admin failed: %v", err)
		msg := "Could not save the changes. Please check the logs and try again."
		if errors.Is(err, store.ErrNoAdmin) {
			msg = "There is no administrator account to update."
		}
		s.renderAccount(w, r, http.StatusInternalServerError, msg, username)
		return
	}

	// Keep this session usable under the new name, and — when the password
	// changed — drop every other session so a cookie captured under the old
	// password stops working.
	if c, err := r.Cookie(sessionCookie); err == nil {
		if renaming {
			s.sessions.Rename(c.Value, username)
		}
		if repassword {
			s.sessions.DestroyOthers(c.Value)
		}
	}

	logf("panel: administrator account updated (username changed: %t, password changed: %t)", renaming, repassword)
	http.Redirect(w, r, "/account?updated="+updatedFlag(renaming, repassword), http.StatusSeeOther)
}

// updatedFlag names what changed, for the fixed post-redirect flash message.
func updatedFlag(renamed, repassword bool) string {
	switch {
	case renamed && repassword:
		return "both"
	case renamed:
		return "username"
	default:
		return "password"
	}
}
