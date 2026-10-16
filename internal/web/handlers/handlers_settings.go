package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/notify"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/view"
)

// Server › Settings holds what is true of the whole instance, whoever is
// signed in: log retention (the outbound log's, and the inbound log's while the
// inbound relay is on), the read-only view of the rate limits, the switch for
// notification e-mails and the DNS records of the server's own name those are
// sent from. A
// user's own name, e-mail, password and DMARC default are on Account
// (handlers_account.go). Before placing a setting here, ask whose it is.

// HandleServerSettings shows and saves the instance settings (global role).
func (h *Handlers) HandleServerSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	// The routes are method-bound (GET and POST), so it is one or the other.
	if r.Method == http.MethodPost {
		h.submitServerSettings(w, r)
		return
	}
	h.renderServerSettings(w, r, http.StatusOK, "", h.currentRetentions())
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
	on, err := h.store.NotificationsEnabled()
	if err != nil {
		// Shown as on: the form saves what it shows, and a failed read must not
		// become "off" at the next Save.
		logf("panel: settings: read the notifications switch: %v", err)
		on = true
	}
	page.WithNotifications(on, notify.From(h.cfg.Hostname))
	mail := h.serverMail(false)
	page.WithMail(mail.records, view.FormatAge(mail.checked, time.Now()))
	page.WithTimeZone(h.cfg.TimeZone, time.Local, time.Now())
	// The inbound field exists while there is an inbound journal to keep.
	if h.cfg.InboundEnabled {
		page.WithInbound(strconv.Itoa(form.inbound))
	}
	flash := ""
	switch {
	case r.URL.Query().Has("saved"):
		flash = "Settings saved."
	case r.URL.Query().Has("rechecked"):
		flash = "DNS re-checked."
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
	// An unticked checkbox is not sent at all: absent is off.
	// HTML Living Standard, form-control-infrastructure.html#constructing-entry-list:
	// a checkbox whose checkedness is false is skipped.
	notifications := r.PostFormValue("notify") != ""
	if err := h.store.SetSendLogRetentionDays(days); err != nil {
		logf("panel: settings: set send-log retention failed: %v", err)
		h.renderServerSettings(w, r, http.StatusInternalServerError,
			"Could not save send log retention. Please check the logs and try again.", form)
		return
	}
	if err := h.store.SetNotificationsEnabled(notifications); err != nil {
		logf("panel: settings: set the notifications switch failed: %v", err)
		h.renderServerSettings(w, r, http.StatusInternalServerError,
			"Could not save the notifications switch. Please check the logs and try again.", form)
		return
	}
	if h.cfg.InboundEnabled {
		if err := h.store.SetInboundSpamLogRetentionDays(form.inbound); err != nil {
			logf("panel: settings: set inbound-log retention failed: %v", err)
			h.renderServerSettings(w, r, http.StatusInternalServerError,
				"Could not save inbound log retention. Please check the logs and try again.", form)
			return
		}
		logf("panel: instance settings updated (send-log retention: %d days, inbound-log retention: %d days, notifications: %t)", days, form.inbound, notifications)
	} else {
		logf("panel: instance settings updated (send-log retention: %d days, notifications: %t)", days, notifications)
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

// serverMailState is the server's own name as a sender of notifications: the
// records to publish for it, as Settings draws them, and their state, as
// Health lists it.
type serverMailState struct {
	records []view.Record
	rows    []view.MailRow
	// checked is when the records were looked up; zero when they were not.
	checked time.Time
	// status is the verdict of Health's card: ok, warn or unknown. A record
	// that is not in place is a warning, never an error: without them the
	// notifications are still sent.
	status string
}

// serverMail reads the records of the server's own name and checks them in DNS
// (cached like a domain's; force skips the cache, for Re-check DNS). When the
// DKIM key cannot be read the page is still shown: the DKIM record is left out,
// the SPF record is shown unchecked, and the card does not claim it is fine.
func (h *Handlers) serverMail(force bool) serverMailState {
	host := h.cfg.Hostname
	spfOf := func(c view.DNSCheck) (view.Record, view.MailRow) {
		return view.ServerMailSPF(host, dnscheck.ServerMailSPF, c), view.NewMailRow("SPF", host, c)
	}
	mail, err := h.domains.ServerMail()
	if err != nil {
		logf("panel: server mail: %v", err)
		rec, row := spfOf(view.DNSCheck{Status: string(health.StatusUnknown)})
		return serverMailState{records: []view.Record{rec}, rows: []view.MailRow{row}, status: string(health.StatusUnknown)}
	}
	srv := h.dns.Server(host, false)
	check := h.dns.ServerMail(dnscheck.Query{
		Name: host, Selector: mail.Selector, ExpectedDKIM: mail.Record.Value,
		Hostname: srv.Hostname, ServerIPs: srv.IPs,
	}, force)
	dkim := dnsCheck(check.DKIM)
	spfRec, spfRow := spfOf(dnsCheck(check.SPF))
	status := check.Overall
	if status == health.StatusError {
		status = health.StatusWarn
	}
	return serverMailState{
		records: []view.Record{view.ServerMailDKIM(mail.Selector, mail.Record.Name, mail.Record.Value, dkim), spfRec},
		rows:    []view.MailRow{view.NewMailRow("DKIM", mail.Record.Name, dkim), spfRow},
		status:  string(status),
		checked: check.CheckedAt,
	}
}

// HandleSettingsDNSRecheck re-checks the records of the server's own name
// from Server › Settings, where they are shown, and returns there (global
// role). The server's address is looked up again too: the SPF check reads it.
// The same lookups as Re-check DNS on Health.
func (h *Handlers) HandleSettingsDNSRecheck(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	h.dns.Server(h.cfg.Hostname, true)
	h.serverMail(true)
	http.Redirect(w, r, "/server/settings?rechecked=1", http.StatusSeeOther)
}
