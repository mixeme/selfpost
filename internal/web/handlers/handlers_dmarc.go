package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/mixeme/selfpost/internal/dmarc"
	"github.com/mixeme/selfpost/internal/web/validate"
)

// HandleDomainDMARC saves per-domain DMARC rua= settings.
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

	// "inherit" makes the domain follow the default of the user who chose it;
	// every other mode gives the domain an address of its own ("" = none).
	follow := false
	rua := ""
	mode := strings.TrimSpace(r.PostFormValue("dmarc_rua_mode"))
	// A refusal shows the form again with the choice that was made.
	refuse := func(status int, msg string) {
		h.renderDomainSettings(w, r, status, d, settingsView{Err: msg, RuaMode: mode, RuaCustom: strings.TrimSpace(r.PostFormValue("dmarc_rua_email"))})
	}
	switch mode {
	case "keep":
		// The domain follows another user's default and the form left it so.
		// Anything else under this value is a stale or forged form.
		if p, ok := h.principal(r); !ok || !d.DMARCRuaUserID.Valid || d.DMARCRuaUserID.Int64 == p.ID {
			refuse(http.StatusBadRequest, "Choose how aggregate reports are addressed for this domain.")
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/outbound/domains/%d?dmarc=1", d.ID), http.StatusSeeOther)
		return
	case "inherit":
		follow = true
	case "none":
	case "custom":
		email := strings.TrimSpace(r.PostFormValue("dmarc_rua_email"))
		if err := validate.Email(email); err != nil {
			refuse(http.StatusBadRequest, err.Error())
			return
		}
		if email == "" {
			refuse(http.StatusBadRequest, "Enter a custom report address or choose another mode.")
			return
		}
		rua = email
	case "hosted":
		if h.dmarc == nil || !h.dmarc.Enabled() {
			refuse(http.StatusBadRequest, "SelfPost-hosted reports are not enabled on this server.")
			return
		}
		rua = dmarc.HostedReportAddress(h.cfg.Hostname, d.Name)
	default:
		h.renderDomainSettings(w, r, http.StatusBadRequest, d, settingsView{Err: "Choose how aggregate reports are addressed for this domain."})
		return
	}

	var err error
	if p, ok := h.principal(r); follow && ok {
		err = h.store.SetDomainDMARCUser(d.ID, p.ID)
	} else {
		err = h.store.SetDomainDMARCAddress(d.ID, rua)
	}
	if err != nil {
		logf("panel: domain %d: save dmarc rua: %v", d.ID, err)
		refuse(http.StatusInternalServerError, "Could not save DMARC settings. Please check the logs and try again.")
		return
	}
	if h.dmarc != nil && h.dmarc.Enabled() {
		if err := h.dmarc.Resync(); err != nil {
			logf("panel: domain %d: dmarc resync: %v", d.ID, err)
		}
	}
	h.dns.Forget(d.Name)
	http.Redirect(w, r, fmt.Sprintf("/outbound/domains/%d?dmarc=1", d.ID), http.StatusSeeOther)
}
