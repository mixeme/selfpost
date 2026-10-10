package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/mixeme/selfpost/internal/dmarc"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/validate"
	"github.com/mixeme/selfpost/internal/web/view"
)

// HandleDomainDMARC saves where a domain's aggregate reports (rua=) go. The
// form offers exactly three choices: "none", "hosted" (the address SelfPost
// derives for the domain, offered only while hosted reports are enabled) and
// "custom" (an address typed for the domain). What is stored is the address
// alone; "" is no reports.
func (h *Handlers) HandleDomainDMARC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d, ok := h.lookupDomain(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderDomainSettings(w, r, http.StatusBadRequest, d, settingsView{Err: "Invalid form submission."})
		return
	}

	rua := ""
	mode := strings.TrimSpace(r.PostFormValue("dmarc_rua_mode"))
	// A refusal shows the form again with the choice that was made.
	refuse := func(status int, msg string) {
		h.renderDomainSettings(w, r, status, d, settingsView{Err: msg, RuaMode: mode, RuaCustom: strings.TrimSpace(r.PostFormValue("dmarc_rua_email"))})
	}
	switch mode {
	case ruaNone:
	case ruaCustom:
		email := strings.TrimSpace(r.PostFormValue("dmarc_rua_email"))
		if err := validate.Email(email); err != nil {
			refuse(http.StatusBadRequest, err.Error())
			return
		}
		if email == "" {
			refuse(http.StatusBadRequest, "Enter the report address or choose another option.")
			return
		}
		rua = email
	case ruaHosted:
		if h.dmarc == nil || !h.dmarc.Enabled() {
			refuse(http.StatusBadRequest, "SelfPost-hosted reports are not enabled on this server.")
			return
		}
		rua = dmarc.HostedReportAddress(h.cfg.Hostname, d.Name)
	default:
		h.renderDomainSettings(w, r, http.StatusBadRequest, d, settingsView{Err: "Choose how aggregate reports are addressed for this domain."})
		return
	}

	if err := h.store.SetDomainDMARCAddress(d.ID, rua); err != nil {
		logf("panel: domain %d: save dmarc rua: %v", d.ID, err)
		refuse(http.StatusInternalServerError, "Could not save DMARC settings. Please check the logs and try again.")
		return
	}
	h.resyncDMARC("domain report address")
	h.dns.Forget(d.Name)
	http.Redirect(w, r, fmt.Sprintf("/outbound/domains/%d?dmarc=1", d.ID), http.StatusSeeOther)
}

// The three choices of a domain's report address, the values of the select.
const (
	ruaNone   = view.ReportNone
	ruaHosted = view.ReportHosted
	ruaCustom = view.ReportCustom
)

// reportChoice says which of the three choices a domain's stored address is:
// none for "", hosted for the address SelfPost derives for the domain (only
// while hosted reports are enabled — otherwise it is just an address), custom
// for anything else.
func (h *Handlers) reportChoice(d store.Domain) string {
	hosted := h.hostedDMARCAddress(d.Name)
	switch {
	case d.DMARCRua == "":
		return ruaNone
	case hosted != "" && strings.EqualFold(d.DMARCRua, hosted):
		return ruaHosted
	}
	return ruaCustom
}

// resyncDMARC rebuilds the ingest allow-list after something that changes
// where a domain's reports go. A failure is logged, not shown: the setting is
// saved, and the next resync picks it up.
func (h *Handlers) resyncDMARC(why string) {
	if h.dmarc == nil || !h.dmarc.Enabled() {
		return
	}
	if err := h.dmarc.Resync(); err != nil {
		logf("panel: dmarc resync after %s change: %v", why, err)
	}
}
