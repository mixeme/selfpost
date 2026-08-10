package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
	"github.com/mixeme/selfpost/internal/web/validate"
)

// domainRow is one line of the domain list: the stored domain plus the rolled-up
// verdict of its published DNS records, so the operator sees which domains still
// need a record published without opening each one.
type domainRow struct {
	store.Domain
	DNS health.Status
}

// HandleDashboard is the authenticated landing page: the list of sending
// domains with their DKIM/selector and application counts, plus the add-domain
// form (product.md).
func (h *Handlers) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	h.renderDashboard(w, r, http.StatusOK, "", "")
}

func (h *Handlers) renderDashboard(w http.ResponseWriter, r *http.Request, status int, formErr, formName string) {
	domains, err := h.domains.List()
	if err != nil {
		logf("panel: dashboard: list domains: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.view.Render(w, status, "dashboard", map[string]any{
		"Title":    "SelfPost",
		"User":     auth.CurrentUser(r),
		"Active":   "domains",
		"Domains":  h.domainRows(domains),
		"Error":    formErr,
		"FormName": formName,
		"Flash":    dashboardFlash(r),
	})
}

func (h *Handlers) domainRows(domains []store.Domain) []domainRow {
	profileEmail := ""
	if admin, err := h.store.GetAdmin(); err == nil {
		profileEmail = admin.DMARCReportEmail
	}
	rows := make([]domainRow, len(domains))
	var wg sync.WaitGroup
	for i, d := range domains {
		rows[i] = domainRow{Domain: d, DNS: health.StatusUnknown}
		wg.Add(1)
		go func() {
			defer wg.Done()
			record, err := h.domains.DKIMRecord(d)
			if err != nil {
				logf("panel: dashboard: domain %d: dkim record: %v", d.ID, err)
				return
			}
			dns, _ := h.domainDNS(d, record, profileEmail, false)
			rows[i].DNS = dns.Overall
		}()
	}
	wg.Wait()
	return rows
}

func dashboardFlash(r *http.Request) string {
	if r.URL.Query().Get("deleted") != "" {
		return "Domain deleted."
	}
	return ""
}

// HandleAddDomain validates the submitted name, creates the domain (DKIM key +
// OpenDKIM reload), and redirects to the domain's page so the DNS record to
// publish is shown (product.md).
func (h *Handlers) HandleAddDomain(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderDashboard(w, r, http.StatusBadRequest, "Invalid form submission.", "")
		return
	}
	raw := r.PostFormValue("name")
	name := validate.NormalizeDomain(raw)
	if err := validate.Domain(name); err != nil {
		h.renderDashboard(w, r, http.StatusBadRequest, err.Error(), raw)
		return
	}

	d, err := h.domains.Add(name)
	if err != nil {
		if errors.Is(err, store.ErrDomainExists) {
			h.renderDashboard(w, r, http.StatusConflict, "That domain is already configured.", raw)
			return
		}
		logf("panel: add domain %q: %v", name, err)
		h.renderDashboard(w, r, http.StatusInternalServerError,
			"Could not add the domain. Please check the logs and try again.", raw)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/domains/%d", d.ID), http.StatusSeeOther)
}

// HandleDeleteConfirm shows the cascade warning before a domain is removed.
func (h *Handlers) HandleDeleteConfirm(w http.ResponseWriter, r *http.Request) {
	d, ok := h.lookupDomain(w, r)
	if !ok {
		return
	}
	h.view.Render(w, http.StatusOK, "domain_delete", map[string]any{
		"Title":  "SelfPost — delete " + d.Name,
		"User":   auth.CurrentUser(r),
		"Active": "domains",
		"Domain": d,
	})
}

// HandleDeleteDomain performs the deletion and returns to the domain list.
func (h *Handlers) HandleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	id, ok := parseDomainID(w, r)
	if !ok {
		return
	}
	if d, err := h.domains.Get(id); err == nil {
		defer h.dns.Forget(d.Name)
	}
	if err := h.domains.Delete(id); err != nil {
		if errors.Is(err, store.ErrDomainNotFound) {
			http.NotFound(w, r)
			return
		}
		logf("panel: delete domain %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/domains?deleted=1", http.StatusSeeOther)
}

// HandleReload re-applies both the OpenDKIM configuration and the Postfix
// sender map on demand (architecture.md § Panel HTTP surface).
func (h *Handlers) HandleReload(w http.ResponseWriter, r *http.Request) {
	if err := h.domains.Resync(); err != nil {
		logf("panel: manual reload (opendkim): %v", err)
		http.Error(w, "reload failed", http.StatusInternalServerError)
		return
	}
	if err := h.apps.Resync(); err != nil {
		logf("panel: manual reload (postfix): %v", err)
		http.Error(w, "reload failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/status?reloaded=1", http.StatusSeeOther)
}

func (h *Handlers) lookupDomain(w http.ResponseWriter, r *http.Request) (store.Domain, bool) {
	id, ok := parseDomainID(w, r)
	if !ok {
		return store.Domain{}, false
	}
	d, err := h.domains.Get(id)
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
