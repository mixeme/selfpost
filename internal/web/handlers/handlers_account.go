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

// HandleAccount serves the signed-in user's account settings.
func (h *Handlers) HandleAccount(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		p, ok := h.principal(r)
		if !ok {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		u, err := h.store.GetUser(p.ID)
		if err != nil {
			logf("panel: account: get user failed: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		h.renderAccount(w, r, http.StatusOK, "", u.Username, u.DMARCReportEmail, p.IsGlobal())
	case http.MethodPost:
		h.submitAccount(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handlers) renderAccount(w http.ResponseWriter, r *http.Request, status int, formErr, formUsername, formDMARCEmail string, showDMARC bool) {
	var reportAuth dnscheck.Result
	if showDMARC && formDMARCEmail != "" {
		if hub := dnscheck.EmailDomain(formDMARCEmail); hub != "" {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			reportAuth = h.dns.ReportAuth(ctx, hub)
			cancel()
		}
	}
	data := h.pageBase(r)
	data["Title"] = "SelfPost — settings"
	data["Active"] = "account"
	data["FormUsername"] = formUsername
	data["FormDMARCEmail"] = formDMARCEmail
	data["ShowDMARC"] = showDMARC
	data["ReportAuthName"] = dnscheck.ReportAuthRecordName(dnscheck.EmailDomain(formDMARCEmail))
	data["ReportAuthExample"] = dnscheck.ReportAuthExample()
	data["ReportAuthDNS"] = reportAuth
	data["ReportAuthHub"] = dnscheck.EmailDomain(formDMARCEmail)
	data["Error"] = formErr
	data["Flash"] = accountFlash(r)
	h.view.Render(w, status, "account", data)
}

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

func (h *Handlers) submitAccount(w http.ResponseWriter, r *http.Request) {
	if !h.auth.AllowLoginAttempt(r) {
		p, _ := h.principal(r)
		h.renderAccount(w, r, http.StatusTooManyRequests,
			"Too many attempts. Please wait and try again.", auth.CurrentUser(r), "", p.IsGlobal())
		return
	}
	if err := r.ParseForm(); err != nil {
		p, _ := h.principal(r)
		h.renderAccount(w, r, http.StatusBadRequest, "Invalid form submission.", auth.CurrentUser(r), "", p.IsGlobal())
		return
	}

	p, ok := h.principal(r)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	user, err := h.store.GetUser(p.ID)
	if err != nil {
		logf("panel: account: get user failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	current := r.PostFormValue("current_password")
	password := r.PostFormValue("new_password")
	confirm := r.PostFormValue("new_password_confirm")
	dmarcEmail := strings.TrimSpace(r.PostFormValue("dmarc_report_email"))
	if !p.IsGlobal() {
		dmarcEmail = user.DMARCReportEmail
	}
	if username == "" {
		username = user.Username
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(current)); err != nil {
		h.renderAccount(w, r, http.StatusUnauthorized, "Current password is incorrect.", username, dmarcEmail, p.IsGlobal())
		return
	}

	renaming := username != user.Username
	if renaming {
		if err := validate.Username(username); err != nil {
			h.renderAccount(w, r, http.StatusBadRequest, err.Error(), username, dmarcEmail, p.IsGlobal())
			return
		}
	}

	if p.IsGlobal() {
		if err := validate.Email(dmarcEmail); err != nil {
			h.renderAccount(w, r, http.StatusBadRequest, err.Error(), username, dmarcEmail, true)
			return
		}
	}

	emailChanging := p.IsGlobal() && dmarcEmail != user.DMARCReportEmail

	repassword := password != "" || confirm != ""
	if repassword {
		if password != confirm {
			h.renderAccount(w, r, http.StatusBadRequest, "New passwords do not match.", username, dmarcEmail, p.IsGlobal())
			return
		}
		if err := validate.AdminPassword(password); err != nil {
			h.renderAccount(w, r, http.StatusBadRequest, err.Error(), username, dmarcEmail, p.IsGlobal())
			return
		}
	}
	if !renaming && !repassword && !emailChanging {
		h.renderAccount(w, r, http.StatusBadRequest,
			"Nothing to change: enter a new username, password, or DMARC report address.", username, dmarcEmail, p.IsGlobal())
		return
	}

	hash := user.PasswordHash
	if repassword {
		newHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			logf("panel: account: hashing password failed: %v", err)
			h.renderAccount(w, r, http.StatusInternalServerError,
				"Internal error. Please try again.", username, dmarcEmail, p.IsGlobal())
			return
		}
		hash = string(newHash)
	}

	if err := h.store.UpdateUser(user.ID, username, hash, dmarcEmail); err != nil {
		logf("panel: account: update user failed: %v", err)
		msg := "Could not save the changes. Please check the logs and try again."
		if errors.Is(err, store.ErrUserNotFound) {
			msg = "There is no user account to update."
		}
		if errors.Is(err, store.ErrUserExists) {
			msg = "That username is already in use."
			h.renderAccount(w, r, http.StatusConflict, msg, username, dmarcEmail, p.IsGlobal())
			return
		}
		h.renderAccount(w, r, http.StatusInternalServerError, msg, username, dmarcEmail, p.IsGlobal())
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

	logf("panel: user %d account updated (username: %t, password: %t, dmarc email: %t)", user.ID, renaming, repassword, emailChanging)
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
