package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/mixeme/selfpost/internal/store"
)

// Server › Settings holds what is true of the whole instance, whoever is
// signed in: log retention and the read-only view of the rate limits. A
// user's own name, e-mail, password and DMARC default are on Account
// (handlers_account.go). Before placing a setting here, ask whose it is.

// HandleServerSettings shows and saves the instance settings (global role).
func (h *Handlers) HandleServerSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.renderServerSettings(w, r, http.StatusOK, "", h.sendLogRetentionDays())
	case http.MethodPost:
		h.submitServerSettings(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handlers) renderServerSettings(w http.ResponseWriter, r *http.Request, status int, formErr string, formRetention int) {
	data := h.pageBase(r)
	data["Title"] = "SelfPost — settings"
	data["Active"] = "settings"
	data["AccountPage"] = false
	data["FormSendLogRetentionDays"] = formRetention
	data["Error"] = formErr
	if r.URL.Query().Has("saved") {
		data["Flash"] = "Settings saved."
	}
	data["L1Messages"] = h.l1Messages()
	data["L1Window"] = h.l1Window()
	h.view.Render(w, status, "settings", data)
}

func (h *Handlers) submitServerSettings(w http.ResponseWriter, r *http.Request) {
	current := h.sendLogRetentionDays()
	if err := r.ParseForm(); err != nil {
		h.renderServerSettings(w, r, http.StatusBadRequest, "Invalid form submission.", current)
		return
	}
	days, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("send_log_retention_days")))
	if err != nil {
		h.renderServerSettings(w, r, http.StatusBadRequest, "Send log retention must be a whole number of days.", current)
		return
	}
	if err := store.ValidateSendLogRetentionDays(days); err != nil {
		h.renderServerSettings(w, r, http.StatusBadRequest, err.Error(), days)
		return
	}
	if err := h.store.SetSendLogRetentionDays(days); err != nil {
		logf("panel: settings: set send-log retention failed: %v", err)
		h.renderServerSettings(w, r, http.StatusInternalServerError,
			"Could not save send log retention. Please check the logs and try again.", days)
		return
	}
	logf("panel: instance settings updated (send-log retention: %d days)", days)
	http.Redirect(w, r, "/server/settings?saved=1", http.StatusSeeOther)
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
