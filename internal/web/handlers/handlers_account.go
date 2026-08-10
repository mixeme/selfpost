package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
	"github.com/mixeme/selfpost/internal/web/validate"
	"golang.org/x/crypto/bcrypt"
)

// HandleAccount serves the administrator's own account settings: the username
// and password chosen during setup are the only panel credentials
// (security.md), and until now they could be changed only by recreating the
// state. Changing them here never touches application SASL logins, which are a
// separate identity system (architecture.md § Mail path).
func (h *Handlers) HandleAccount(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		admin, err := h.store.GetAdmin()
		if err != nil {
			logf("panel: account: get admin failed: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		h.renderAccount(w, r, http.StatusOK, "", admin.Username, admin.DMARCReportEmail)
	case http.MethodPost:
		h.submitAccount(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// renderAccount draws the settings form. formUsername and formDMARCEmail
// repopulate fields after a rejected submission; password fields are never
// repopulated.
func (h *Handlers) renderAccount(w http.ResponseWriter, r *http.Request, status int, formErr, formUsername, formDMARCEmail string) {
	var reportAuth dnscheck.Result
	if hub := dnscheck.EmailDomain(formDMARCEmail); hub != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		reportAuth = h.dns.ReportAuth(ctx, hub)
		cancel()
	}
	h.view.Render(w, status, "account", map[string]any{
		"Title":              "SelfPost — settings",
		"User":               auth.CurrentUser(r),
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
func (h *Handlers) submitAccount(w http.ResponseWriter, r *http.Request) {
	if !h.auth.AllowLoginAttempt(r) {
		h.renderAccount(w, r, http.StatusTooManyRequests,
			"Too many attempts. Please wait and try again.", auth.CurrentUser(r), "")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderAccount(w, r, http.StatusBadRequest, "Invalid form submission.", auth.CurrentUser(r), "")
		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	current := r.PostFormValue("current_password")
	password := r.PostFormValue("new_password")
	confirm := r.PostFormValue("new_password_confirm")
	dmarcEmail := strings.TrimSpace(r.PostFormValue("dmarc_report_email"))

	admin, err := h.store.GetAdmin()
	if err != nil {
		logf("panel: account: get admin failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if username == "" {
		username = admin.Username
	}

	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(current)); err != nil {
		h.renderAccount(w, r, http.StatusUnauthorized, "Current password is incorrect.", username, dmarcEmail)
		return
	}

	renaming := username != admin.Username
	if renaming {
		if err := validate.Username(username); err != nil {
			h.renderAccount(w, r, http.StatusBadRequest, err.Error(), username, dmarcEmail)
			return
		}
	}

	if err := validate.Email(dmarcEmail); err != nil {
		h.renderAccount(w, r, http.StatusBadRequest, err.Error(), username, dmarcEmail)
		return
	}

	emailChanging := dmarcEmail != admin.DMARCReportEmail

	repassword := password != "" || confirm != ""
	if repassword {
		if password != confirm {
			h.renderAccount(w, r, http.StatusBadRequest, "New passwords do not match.", username, dmarcEmail)
			return
		}
		if err := validate.AdminPassword(password); err != nil {
			h.renderAccount(w, r, http.StatusBadRequest, err.Error(), username, dmarcEmail)
			return
		}
	}
	if !renaming && !repassword && !emailChanging {
		h.renderAccount(w, r, http.StatusBadRequest,
			"Nothing to change: enter a new username, password, or DMARC report address.", username, dmarcEmail)
		return
	}

	hash := admin.PasswordHash
	if repassword {
		newHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			logf("panel: account: hashing password failed: %v", err)
			h.renderAccount(w, r, http.StatusInternalServerError,
				"Internal error. Please try again.", username, dmarcEmail)
			return
		}
		hash = string(newHash)
	}

	if err := h.store.UpdateAdmin(username, hash, dmarcEmail); err != nil {
		logf("panel: account: update admin failed: %v", err)
		msg := "Could not save the changes. Please check the logs and try again."
		if errors.Is(err, store.ErrNoAdmin) {
			msg = "There is no administrator account to update."
		}
		h.renderAccount(w, r, http.StatusInternalServerError, msg, username, dmarcEmail)
		return
	}

	if token, ok := h.auth.SessionToken(r); ok {
		if renaming {
			h.auth.RenameSession(token, username)
		}
		if repassword {
			h.auth.DestroyOtherSessions(token)
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
