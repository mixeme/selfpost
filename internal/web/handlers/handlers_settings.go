package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/view"
)

// Server › Settings holds what is true of the whole instance, whoever is
// signed in: log retention (the outbound log's, and the inbound log's while the
// inbound relay is on) and the read-only view of the rate limits. A
// user's own name, e-mail, password and DMARC default are on Account
// (handlers_account.go). Before placing a setting here, ask whose it is.

// HandleServerSettings shows and saves the instance settings (global role).
func (h *Handlers) HandleServerSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.renderServerSettings(w, r, http.StatusOK, "", h.currentRetentions())
	case http.MethodPost:
		h.submitServerSettings(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// retentions are the two journal retentions the Settings form shows: the
// outbound log's, and the inbound log's while the inbound relay is on.
type retentions struct {
	outbound, inbound int
}

func (h *Handlers) currentRetentions() retentions {
	cur := retentions{outbound: h.sendLogRetentionDays()}
	if h.cfg.InboundEnabled {
		cur.inbound = h.inboundSpamLogRetentionDays()
	}
	return cur
}

func (h *Handlers) renderServerSettings(w http.ResponseWriter, r *http.Request, status int, formErr string, form retentions) {
	page := view.NewSettings(h.shellMeta(r), strconv.Itoa(form.outbound), view.FormatRate(h.l1Messages(), h.l1Window()))
	// The inbound field exists while there is an inbound journal to keep.
	if h.cfg.InboundEnabled {
		page.WithInbound(strconv.Itoa(form.inbound))
	}
	flash := ""
	if r.URL.Query().Has("saved") {
		flash = "Settings saved."
	}
	page.WithResult(flash, formErr)
	h.view.Render(w, status, "settings", page)
}

func (h *Handlers) submitServerSettings(w http.ResponseWriter, r *http.Request) {
	current := h.currentRetentions()
	if err := r.ParseForm(); err != nil {
		h.renderServerSettings(w, r, http.StatusBadRequest, "Invalid form submission.", current)
		return
	}
	days, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("send_log_retention_days")))
	if err != nil {
		h.renderServerSettings(w, r, http.StatusBadRequest, "Send log retention must be a whole number of days.", current)
		return
	}
	form := retentions{outbound: days, inbound: current.inbound}
	if err := store.ValidateSendLogRetentionDays(days); err != nil {
		h.renderServerSettings(w, r, http.StatusBadRequest, err.Error(), form)
		return
	}
	if h.cfg.InboundEnabled {
		inboundDays, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("inbound_spam_log_retention_days")))
		if err != nil {
			h.renderServerSettings(w, r, http.StatusBadRequest, "Inbound log retention must be a whole number of days.", form)
			return
		}
		form.inbound = inboundDays
		if err := store.ValidateInboundSpamLogRetentionDays(inboundDays); err != nil {
			h.renderServerSettings(w, r, http.StatusBadRequest, err.Error(), form)
			return
		}
	}
	if err := h.store.SetSendLogRetentionDays(days); err != nil {
		logf("panel: settings: set send-log retention failed: %v", err)
		h.renderServerSettings(w, r, http.StatusInternalServerError,
			"Could not save send log retention. Please check the logs and try again.", form)
		return
	}
	if h.cfg.InboundEnabled {
		if err := h.store.SetInboundSpamLogRetentionDays(form.inbound); err != nil {
			logf("panel: settings: set inbound-log retention failed: %v", err)
			h.renderServerSettings(w, r, http.StatusInternalServerError,
				"Could not save inbound log retention. Please check the logs and try again.", form)
			return
		}
		logf("panel: instance settings updated (send-log retention: %d days, inbound-log retention: %d days)", days, form.inbound)
	} else {
		logf("panel: instance settings updated (send-log retention: %d days)", days)
	}
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
