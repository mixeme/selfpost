package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"codeberg.org/mix/selfpost/internal/backup"
	"codeberg.org/mix/selfpost/internal/domain"
	"codeberg.org/mix/selfpost/internal/store"
)

// maxImportBytes caps a domain-import upload. A domain export is a small JSON
// document (a DKIM key and a handful of credentials); this leaves generous head
// room while refusing anything large enough to be an abuse attempt.
const maxImportBytes = 1 << 20 // 1 MiB

// handleBackup streams a full-server backup as a download (spec 7.5.A). It is an
// authenticated admin action (this handler sits behind the auth middleware). The
// archive carries DKIM private keys, the admin password hash and SASL
// credentials, so it is served with no-store and as an attachment to discourage
// caching of secret material.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	filename := fmt.Sprintf("selfpost-backup-%s.tar.gz", time.Now().UTC().Format("20060102-150405"))

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-store")

	if err := backup.Create(w, backup.Params{
		DataDir: s.cfg.DataDir,
		DBPath:  s.cfg.DBPath,
		Version: s.cfg.Version,
	}); err != nil {
		// Headers (and possibly some bytes) may already be on the wire, so we
		// cannot switch to a clean error page; log it and let the truncated
		// download fail loudly on the client side.
		logf("panel: full backup failed: %v", err)
		return
	}
}

// handleExportDomain streams a single-domain export as a secret download (spec
// 7.5.B). Like the full backup it is POST-only (state is not changed, but the
// response contains the domain's DKIM private key and application passwords, so
// it must not be prefetchable or cached).
func (s *Server) handleExportDomain(w http.ResponseWriter, r *http.Request) {
	d, ok := s.lookupDomain(w, r)
	if !ok {
		return
	}
	exp, err := s.domains.Export(d.ID)
	if err != nil {
		logf("panel: export domain %d: %v", d.ID, err)
		http.Error(w, "export failed", http.StatusInternalServerError)
		return
	}
	body, err := json.MarshalIndent(exp, "", "  ")
	if err != nil {
		logf("panel: export domain %d: encode: %v", d.ID, err)
		http.Error(w, "export failed", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("selfpost-domain-%s.json", d.Name)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

// handleImportDomain accepts an uploaded domain-export file and re-creates the
// domain on this instance (spec 7.5.B). The domain name is normalised and
// validated here (spec 7.6.2); the domain service validates the selector, each
// login and address, and the DKIM key before writing anything. On success it
// redirects to the new domain's page; on failure it re-renders the dashboard
// with a friendly message.
func (s *Server) handleImportDomain(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
	if err := r.ParseMultipartForm(maxImportBytes); err != nil {
		s.renderDashboard(w, r, http.StatusBadRequest, "", "", "Could not read the uploaded file (too large or not a valid upload).")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		s.renderDashboard(w, r, http.StatusBadRequest, "", "", "Choose a domain export file to import.")
		return
	}
	defer file.Close()

	var exp domain.DomainExport
	dec := json.NewDecoder(file)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&exp); err != nil {
		s.renderDashboard(w, r, http.StatusBadRequest, "", "", "That file is not a valid SelfPost domain export.")
		return
	}

	// Normalise and validate the domain name before it reaches the service, the
	// same gate the add-domain form uses (spec 7.6.2).
	exp.Domain = normalizeDomain(exp.Domain)
	if err := validateDomain(exp.Domain); err != nil {
		s.renderDashboard(w, r, http.StatusBadRequest, "", "", "Invalid domain in export file: "+err.Error())
		return
	}

	d, err := s.domains.Import(exp)
	if err != nil {
		logf("panel: import domain %q: %v", exp.Domain, err)
		status, msg := importErrorMessage(err)
		s.renderDashboard(w, r, status, "", "", msg)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/domains/%d?imported=1", d.ID), http.StatusSeeOther)
}

// importErrorMessage maps a domain-import failure (already logged by the caller)
// to an HTTP status and a user-facing message. Duplicate domain/login are called
// out specifically; other failures — validation errors describing what is wrong
// with the file, or an internal write/reload problem — are surfaced verbatim to
// this admin-only panel so the operator can act on them.
func importErrorMessage(err error) (int, string) {
	switch {
	case errors.Is(err, store.ErrDomainExists):
		return http.StatusConflict, "A domain with that name already exists here. Delete it first, or import into a fresh instance."
	case errors.Is(err, store.ErrLoginExists):
		return http.StatusConflict, "One of the application logins in the file is already in use on this instance. Application logins must be unique across all domains."
	default:
		return http.StatusBadRequest, "Could not import the domain: " + err.Error()
	}
}
