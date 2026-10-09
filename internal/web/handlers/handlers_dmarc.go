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
		h.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{FormErr: "Invalid form submission."})
		return
	}

	// "inherit" makes the domain follow the default of the user who chose it;
	// every other mode gives the domain an address of its own ("" = none).
	follow := false
	rua := ""
	switch strings.TrimSpace(r.PostFormValue("dmarc_rua_mode")) {
	case "inherit":
		follow = true
	case "none":
	case "custom":
		email := strings.TrimSpace(r.PostFormValue("dmarc_rua_email"))
		if err := validate.Email(email); err != nil {
			h.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{FormErr: err.Error()})
			return
		}
		if email == "" {
			h.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{FormErr: "Enter a custom report address or choose another mode."})
			return
		}
		rua = email
	case "hosted":
		if h.dmarc == nil || !h.dmarc.Enabled() {
			h.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{FormErr: "SelfPost-hosted reports are not enabled on this server."})
			return
		}
		rua = dmarc.HostedReportAddress(h.cfg.Hostname, d.Name)
	default:
		h.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{FormErr: "Choose how aggregate reports are addressed for this domain."})
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
		h.renderDomainDetail(w, r, http.StatusInternalServerError, d, detailView{FormErr: "Could not save DMARC settings. Please check the logs and try again."})
		return
	}
	if h.dmarc != nil && h.dmarc.Enabled() {
		if err := h.dmarc.Resync(); err != nil {
			logf("panel: domain %d: dmarc resync: %v", d.ID, err)
		}
	}
	h.dns.Forget(d.Name)
	http.Redirect(w, r, fmt.Sprintf("/domains/%d?dmarc=1", d.ID), http.StatusSeeOther)
}
