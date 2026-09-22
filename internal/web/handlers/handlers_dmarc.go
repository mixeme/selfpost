package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"

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

	var rua sql.NullString
	switch strings.TrimSpace(r.PostFormValue("dmarc_rua_mode")) {
	case "inherit":
		rua = sql.NullString{}
	case "none":
		rua = sql.NullString{Valid: true, String: ""}
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
		rua = sql.NullString{Valid: true, String: email}
	default:
		h.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{FormErr: "Choose how aggregate reports are addressed for this domain."})
		return
	}

	if err := h.store.UpdateDomainDMARCRua(d.ID, rua); err != nil {
		logf("panel: domain %d: save dmarc rua: %v", d.ID, err)
		h.renderDomainDetail(w, r, http.StatusInternalServerError, d, detailView{FormErr: "Could not save DMARC settings. Please check the logs and try again."})
		return
	}
	h.dns.Forget(d.Name)
	http.Redirect(w, r, fmt.Sprintf("/domains/%d?dmarc=1", d.ID), http.StatusSeeOther)
}
