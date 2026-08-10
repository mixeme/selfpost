package web

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// handleAccount serves the administrator's own account settings: the username
// and password chosen during setup are the only panel credentials
// (security.md), and until now they could be changed only by recreating the
// state. Changing them here never touches application SASL logins, which are a
// separate identity system (architecture.md § Mail path).
func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		admin, err := s.store.GetAdmin()
		if err != nil {
			logf("panel: account: get admin failed: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		s.renderAccount(w, r, http.StatusOK, "", admin.Username, admin.DMARCReportEmail)
	case http.MethodPost:
		s.submitAccount(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// renderAccount draws the settings form. formUsername and formDMARCEmail
// repopulate fields after a rejected submission; password fields are never
// repopulated.
func (s *Server) renderAccount(w http.ResponseWriter, r *http.Request, status int, formErr, formUsername, formDMARCEmail string) {
	var reportAuth dnscheck.Result
	if hub := dnscheck.EmailDomain(formDMARCEmail); hub != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		reportAuth = s.dns.ReportAuth(ctx, hub)
		cancel()
	}
	s.render(w, status, "account", map[string]any{
		"Title":              "SelfPost — settings",
		"User":               currentUser(r),
		"Active":             "account",
		"FormUsername":       formUsername,
		"FormDMARCEmail":     formDMARCEmail,
		"ReportAuthName":     dnscheck.ReportAuthRecordName(dnscheck.EmailDomain(formDMARCEmail)),
		"ReportAuthExample":  dnscheck.ReportAuthExample(),
		"ReportAuthDNS":      reportAuth,
		"ReportAuthHub":      dnscheck.EmailDomain(formDMARCEmail),
		"Error":              formErr,
		"Flash":              accountFlash(r),
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
	case "email":
		return "DMARC report address updated."
	case "username-email":
		return "Username and DMARC report address updated."
	case "password-email":
		return "Password and DMARC report address updated. Any other signed-in sessions were signed out."
	case "all":
		return "Settings updated. Any other signed-in sessions were signed out."
	default:
		return ""
	}
}

// submitAccount applies a username and/or password change. The current password
// is always required, so a stolen session alone cannot lock the administrator
// out of their own panel, and the attempt is throttled on the same limiter as
// the login form so this route cannot be used to brute-force the password past
// that limit (security.md).
func (s *Server) submitAccount(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimiter.Allow(clientIP(r, s.trustedProxies)) {
		s.renderAccount(w, r, http.StatusTooManyRequests,
			"Too many attempts. Please wait and try again.", currentUser(r), "")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderAccount(w, r, http.StatusBadRequest, "Invalid form submission.", currentUser(r), "")
		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	current := r.PostFormValue("current_password")
	password := r.PostFormValue("new_password")
	confirm := r.PostFormValue("new_password_confirm")
	dmarcEmail := strings.TrimSpace(r.PostFormValue("dmarc_report_email"))

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
		s.renderAccount(w, r, http.StatusUnauthorized, "Current password is incorrect.", username, dmarcEmail)
		return
	}

	renaming := username != admin.Username
	if renaming {
		if err := validateUsername(username); err != nil {
			s.renderAccount(w, r, http.StatusBadRequest, err.Error(), username, dmarcEmail)
			return
		}
	}

	if err := validateEmail(dmarcEmail); err != nil {
		s.renderAccount(w, r, http.StatusBadRequest, err.Error(), username, dmarcEmail)
		return
	}

	emailChanging := dmarcEmail != admin.DMARCReportEmail

	repassword := password != "" || confirm != ""
	if repassword {
		if password != confirm {
			s.renderAccount(w, r, http.StatusBadRequest, "New passwords do not match.", username, dmarcEmail)
			return
		}
		if err := validateAdminPassword(password); err != nil {
			s.renderAccount(w, r, http.StatusBadRequest, err.Error(), username, dmarcEmail)
			return
		}
	}
	if !renaming && !repassword && !emailChanging {
		s.renderAccount(w, r, http.StatusBadRequest,
			"Nothing to change: enter a new username, password, or DMARC report address.", username, dmarcEmail)
		return
	}

	hash := admin.PasswordHash
	if repassword {
		newHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			logf("panel: account: hashing password failed: %v", err)
			s.renderAccount(w, r, http.StatusInternalServerError,
				"Internal error. Please try again.", username, dmarcEmail)
			return
		}
		hash = string(newHash)
	}

	if err := s.store.UpdateAdmin(username, hash, dmarcEmail); err != nil {
		logf("panel: account: update admin failed: %v", err)
		msg := "Could not save the changes. Please check the logs and try again."
		if errors.Is(err, store.ErrNoAdmin) {
			msg = "There is no administrator account to update."
		}
		s.renderAccount(w, r, http.StatusInternalServerError, msg, username, dmarcEmail)
		return
	}

	if token, ok := s.sessionToken(r); ok {
		if renaming {
			s.sessions.Rename(token, username)
		}
		if repassword {
			s.sessions.DestroyOthers(token)
		}
	}

	logf("panel: administrator account updated (username: %t, password: %t, dmarc email: %t)", renaming, repassword, emailChanging)
	http.Redirect(w, r, "/account?updated="+updatedFlag(renaming, repassword, emailChanging), http.StatusSeeOther)
}

func updatedFlag(renamed, repassword, emailChanged bool) string {
	switch {
	case renamed && repassword && emailChanged:
		return "all"
	case renamed && emailChanged:
		return "username-email"
	case repassword && emailChanged:
		return "password-email"
	case renamed && repassword:
		return "both"
	case renamed:
		return "username"
	case repassword:
		return "password"
	default:
		return "email"
	}
}
