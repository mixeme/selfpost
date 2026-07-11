package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"codeberg.org/mix/selfpost/internal/store"
)

// handleDashboard is the authenticated landing page: the list of sending
// domains with their DKIM/selector and application counts, plus the add-domain
// form (spec 7.2.2). Applications and the send log arrive in later phases.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	s.renderDashboard(w, r, http.StatusOK, "", "")
}

// renderDashboard renders the domain list. formErr and formName repopulate the
// add-domain form after a rejected submission; flash surfaces a one-shot status
// message keyed by a redirect query flag (never reflected user input).
func (s *Server) renderDashboard(w http.ResponseWriter, r *http.Request, status int, formErr, formName string) {
	domains, err := s.domains.List()
	if err != nil {
		logf("panel: dashboard: list domains: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.render(w, status, "dashboard", map[string]any{
		"Title":    "SelfPost",
		"User":     currentUser(r),
		"Domains":  domains,
		"Error":    formErr,
		"FormName": formName,
		"Flash":    dashboardFlash(r),
	})
}

// dashboardFlash maps a fixed redirect flag to a fixed message, so status text
// after a redirect is never attacker-influenced.
func dashboardFlash(r *http.Request) string {
	switch {
	case r.URL.Query().Get("reloaded") != "":
		return "Configuration reloaded."
	case r.URL.Query().Get("deleted") != "":
		return "Domain deleted."
	default:
		return ""
	}
}

// handleAddDomain validates the submitted name, creates the domain (DKIM key +
// OpenDKIM reload), and redirects to the domain's page so the DNS record to
// publish is shown (spec 7.2.3).
func (s *Server) handleAddDomain(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderDashboard(w, r, http.StatusBadRequest, "Invalid form submission.", "")
		return
	}
	raw := r.PostFormValue("name")
	name := normalizeDomain(raw)
	if err := validateDomain(name); err != nil {
		s.renderDashboard(w, r, http.StatusBadRequest, err.Error(), raw)
		return
	}

	d, err := s.domains.Add(name)
	if err != nil {
		if errors.Is(err, store.ErrDomainExists) {
			s.renderDashboard(w, r, http.StatusConflict, "That domain is already configured.", raw)
			return
		}
		logf("panel: add domain %q: %v", name, err)
		s.renderDashboard(w, r, http.StatusInternalServerError,
			"Could not add the domain. Please check the logs and try again.", raw)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/domains/%d", d.ID), http.StatusSeeOther)
}

// handleDomainDetail shows a single domain and its DKIM DNS record (spec 7.2.10).
func (s *Server) handleDomainDetail(w http.ResponseWriter, r *http.Request) {
	d, ok := s.lookupDomain(w, r)
	if !ok {
		return
	}
	record, err := s.domains.DKIMRecord(d)
	if err != nil {
		logf("panel: domain %d: dkim record: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.render(w, http.StatusOK, "domain_detail", map[string]any{
		"Title":  "SelfPost — " + d.Name,
		"User":   currentUser(r),
		"Domain": d,
		"Record": record,
	})
}

// handleDeleteConfirm shows the cascade warning before a domain is removed: the
// panel must explicitly state that all bound applications go with it (spec 7.2.4).
func (s *Server) handleDeleteConfirm(w http.ResponseWriter, r *http.Request) {
	d, ok := s.lookupDomain(w, r)
	if !ok {
		return
	}
	s.render(w, http.StatusOK, "domain_delete", map[string]any{
		"Title":  "SelfPost — delete " + d.Name,
		"User":   currentUser(r),
		"Domain": d,
	})
}

// handleDeleteDomain performs the deletion (cascade + DKIM key + OpenDKIM reload)
// and returns to the domain list.
func (s *Server) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	id, ok := parseDomainID(w, r)
	if !ok {
		return
	}
	if err := s.domains.Delete(id); err != nil {
		if errors.Is(err, store.ErrDomainNotFound) {
			http.NotFound(w, r)
			return
		}
		logf("panel: delete domain %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/?deleted=1", http.StatusSeeOther)
}

// handleReload re-applies the OpenDKIM configuration on demand (spec 7.2.12).
// The Postfix side of the reload button lands in Phase 5.
func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if err := s.domains.Resync(); err != nil {
		logf("panel: manual reload: %v", err)
		http.Error(w, "reload failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/?reloaded=1", http.StatusSeeOther)
}

// lookupDomain resolves the {id} path value to a domain, writing a 404 for a
// bad id or a missing domain and reporting ok=false in that case.
func (s *Server) lookupDomain(w http.ResponseWriter, r *http.Request) (store.Domain, bool) {
	id, ok := parseDomainID(w, r)
	if !ok {
		return store.Domain{}, false
	}
	d, err := s.domains.Get(id)
	if err != nil {
		if errors.Is(err, store.ErrDomainNotFound) {
			http.NotFound(w, r)
			return store.Domain{}, false
		}
		logf("panel: get domain %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return store.Domain{}, false
	}
	return d, true
}

func parseDomainID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}
