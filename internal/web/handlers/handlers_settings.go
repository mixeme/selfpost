package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
	"github.com/mixeme/selfpost/internal/web/validate"
	"golang.org/x/crypto/bcrypt"
)

// HandleSettings serves the signed-in user's panel settings.
func (h *Handlers) HandleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		p, ok := h.principal(r)
		if !ok {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		u, err := h.store.GetUser(p.ID)
		if err != nil {
			logf("panel: settings: get user failed: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		h.renderSettings(w, r, http.StatusOK, "", u.Username, u.DMARCReportEmail, h.sendLogRetentionDays(), p.IsGlobal())
	case http.MethodPost:
		h.submitSettings(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handlers) renderSettings(w http.ResponseWriter, r *http.Request, status int, formErr, formUsername, formDMARCEmail string, formSendLogRetentionDays int, showDMARC bool) {
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
	data["Active"] = "settings"
	data["FormUsername"] = formUsername
	data["FormDMARCEmail"] = formDMARCEmail
	data["FormSendLogRetentionDays"] = formSendLogRetentionDays
	data["ShowDMARC"] = showDMARC
	data["ReportAuthName"] = dnscheck.ReportAuthRecordName(dnscheck.EmailDomain(formDMARCEmail))
	data["ReportAuthExample"] = dnscheck.ReportAuthExample()
	data["ReportAuthDNS"] = reportAuth
	data["ReportAuthHub"] = dnscheck.EmailDomain(formDMARCEmail)
	data["Error"] = formErr
	data["Flash"] = settingsFlash(r)
	data["L1Messages"] = h.l1Messages()
	data["L1Window"] = h.l1Window()
	data["DMARCIngestEnabled"] = h.cfg.DMARCEnabled
	if h.dmarc != nil && h.cfg.DMARCEnabled {
		data["HostedReportAddress"] = h.dmarc.DefaultHostedSuggestion()
	}
	h.view.Render(w, status, "settings", data)
}

func settingsFlash(r *http.Request) string {
	q := r.URL.Query()
	var parts []string
	if q.Has("u") {
		parts = append(parts, "username")
	}
	if q.Has("p") {
		parts = append(parts, "password")
	}
	if q.Has("e") {
		parts = append(parts, "DMARC report address")
	}
	if q.Has("r") {
		parts = append(parts, "send log retention")
	}
	if len(parts) == 0 {
		return ""
	}
	msg := strings.Join(parts, ", ") + " updated."
	msg = strings.ToUpper(msg[:1]) + msg[1:]
	if q.Has("p") {
		msg += " Any other signed-in sessions were signed out."
	}
	return msg
}

func (h *Handlers) submitSettings(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(r)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	currentRetention := h.sendLogRetentionDays()

	if !h.auth.AllowLoginAttempt(r) {
		h.renderSettings(w, r, http.StatusTooManyRequests,
			"Too many attempts. Please wait and try again.", auth.CurrentUser(r), "", currentRetention, p.IsGlobal())
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderSettings(w, r, http.StatusBadRequest, "Invalid form submission.", auth.CurrentUser(r), "", currentRetention, p.IsGlobal())
		return
	}

	user, err := h.store.GetUser(p.ID)
	if err != nil {
		logf("panel: settings: get user failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	current := r.PostFormValue("current_password")
	password := r.PostFormValue("new_password")
	confirm := r.PostFormValue("new_password_confirm")
	dmarcEmail := strings.TrimSpace(r.PostFormValue("dmarc_report_email"))
	formRetention := currentRetention
	if !p.IsGlobal() {
		dmarcEmail = user.DMARCReportEmail
	} else if raw := strings.TrimSpace(r.PostFormValue("send_log_retention_days")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			h.renderSettings(w, r, http.StatusBadRequest, "Send log retention must be a whole number of days.", username, dmarcEmail, currentRetention, true)
			return
		}
		formRetention = parsed
	}
	if username == "" {
		username = user.Username
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(current)); err != nil {
		h.renderSettings(w, r, http.StatusUnauthorized, "Current password is incorrect.", username, dmarcEmail, formRetention, p.IsGlobal())
		return
	}

	renaming := username != user.Username
	if renaming {
		if err := validate.Username(username); err != nil {
			h.renderSettings(w, r, http.StatusBadRequest, err.Error(), username, dmarcEmail, formRetention, p.IsGlobal())
			return
		}
	}

	if p.IsGlobal() {
		if err := validate.Email(dmarcEmail); err != nil {
			h.renderSettings(w, r, http.StatusBadRequest, err.Error(), username, dmarcEmail, formRetention, true)
			return
		}
	}

	emailChanging := p.IsGlobal() && dmarcEmail != user.DMARCReportEmail

	retentionChanging := false
	if p.IsGlobal() && formRetention != currentRetention {
		if err := store.ValidateSendLogRetentionDays(formRetention); err != nil {
			h.renderSettings(w, r, http.StatusBadRequest, err.Error(), username, dmarcEmail, formRetention, true)
			return
		}
		retentionChanging = true
	}

	repassword := password != "" || confirm != ""
	if repassword {
		if password != confirm {
			h.renderSettings(w, r, http.StatusBadRequest, "New passwords do not match.", username, dmarcEmail, formRetention, p.IsGlobal())
			return
		}
		if err := validate.AdminPassword(password); err != nil {
			h.renderSettings(w, r, http.StatusBadRequest, err.Error(), username, dmarcEmail, formRetention, p.IsGlobal())
			return
		}
	}
	if !renaming && !repassword && !emailChanging && !retentionChanging {
		h.renderSettings(w, r, http.StatusBadRequest,
			"Nothing to change: enter a new username, password, DMARC report address, or send log retention.", username, dmarcEmail, formRetention, p.IsGlobal())
		return
	}

	hash := user.PasswordHash
	if repassword {
		newHash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
		if err != nil {
			logf("panel: settings: hashing password failed: %v", err)
			h.renderSettings(w, r, http.StatusInternalServerError,
				"Internal error. Please try again.", username, dmarcEmail, formRetention, p.IsGlobal())
			return
		}
		hash = string(newHash)
	}

	if renaming || repassword || emailChanging {
		if err := h.store.UpdateUser(user.ID, username, hash, dmarcEmail); err != nil {
			logf("panel: settings: update user failed: %v", err)
			msg := "Could not save the changes. Please check the logs and try again."
			if errors.Is(err, store.ErrUserNotFound) {
				msg = "There is no user account to update."
			}
			if errors.Is(err, store.ErrUserExists) {
				msg = "That username is already in use."
				h.renderSettings(w, r, http.StatusConflict, msg, username, dmarcEmail, formRetention, p.IsGlobal())
				return
			}
			h.renderSettings(w, r, http.StatusInternalServerError, msg, username, dmarcEmail, formRetention, p.IsGlobal())
			return
		}
	}

	if retentionChanging {
		if err := h.store.SetSendLogRetentionDays(formRetention); err != nil {
			logf("panel: settings: set send-log retention failed: %v", err)
			h.renderSettings(w, r, http.StatusInternalServerError,
				"Could not save send log retention. Please check the logs and try again.", username, dmarcEmail, formRetention, true)
			return
		}
	}

	if emailChanging && h.dmarc != nil && h.dmarc.Enabled() {
		if err := h.dmarc.Resync(); err != nil {
			logf("panel: settings: dmarc resync: %v", err)
		}
	}

	if token, ok := h.auth.SessionToken(r); ok {
		if renaming {
			h.auth.RenameSession(token, username)
		}
		if repassword {
			h.auth.DestroyOtherSessions(token)
		}
	}

	logf("panel: user %d settings updated (username: %t, password: %t, dmarc email: %t, retention: %t)", user.ID, renaming, repassword, emailChanging, retentionChanging)
	http.Redirect(w, r, "/settings?"+updatedQuery(renaming, repassword, emailChanging, retentionChanging), http.StatusSeeOther)
}

func updatedQuery(renamed, repassword, emailChanged, retentionChanged bool) string {
	var params []string
	if renamed {
		params = append(params, "u=1")
	}
	if repassword {
		params = append(params, "p=1")
	}
	if emailChanged {
		params = append(params, "e=1")
	}
	if retentionChanged {
		params = append(params, "r=1")
	}
	return strings.Join(params, "&")
}

// sendLogRetentionDays returns the effective delivery-journal retention window.
func (h *Handlers) sendLogRetentionDays() int {
	days, err := h.store.GetSendLogRetentionDays(h.cfg.SendLogRetentionEnvDefault)
	if err != nil {
		logf("panel: send-log retention: %v", err)
		if h.cfg.SendLogRetentionEnvDefault > 0 {
			return h.cfg.SendLogRetentionEnvDefault
		}
		return store.SendLogRetentionDaysDefault
	}
	return days
}
