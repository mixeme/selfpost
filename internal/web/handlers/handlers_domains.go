package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/validate"
	"github.com/mixeme/selfpost/internal/web/view"
)

// HandleDashboard is the list of sending domains with the verdict of each DNS
// check, their application counts and the mail of the statistics window, plus —
// for the global role — the add-domain form (product.md).
func (h *Handlers) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	h.renderDashboard(w, r, http.StatusOK, "", "")
}

func (h *Handlers) renderDashboard(w http.ResponseWriter, r *http.Request, status int, formErr, formName string) {
	p, ok := h.principal(r)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	domains, err := h.assignedDomains(p)
	if err != nil {
		logf("panel: domains: list domains: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	retention := h.sendLogRetentionDays()
	_, _, days := store.StatsWindow(retention, time.Now())
	page := view.NewOutDomains(h.shellMeta(r), p.IsGlobal(), days).
		WithRows(h.outDomainRows(domains, retention, p.IsGlobal())).
		WithResult(dashboardFlash(r), formErr, formName)
	h.view.Render(w, status, "out-domains", page)
}

// outDomainRows is one row per domain: the verdict of each DNS check (cached for
// a few minutes, as on the domain page), the selector, the applications and the
// mail of the statistics window. The checks run side by side.
func (h *Handlers) outDomainRows(domains []store.Domain, retention int, canDelete bool) []view.OutDomainRow {
	rows := make([]view.OutDomainRow, len(domains))
	var wg sync.WaitGroup
	for i, d := range domains {
		rows[i] = view.OutDomainRow{
			Name: d.Name, Href: view.DomainHref(d.ID), Selector: d.DKIMSelector, Apps: d.AppCount,
			DNS: dnsTags(health.StatusUnknown, health.StatusUnknown, health.StatusUnknown), Activity: "—",
		}
		if canDelete {
			rows[i].DeleteHref = view.DomainHref(d.ID) + "/delete"
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if stats, err := h.store.DomainSendStats(d.Name, retention, d.CreatedAt); err != nil {
				logf("panel: domains: domain %d: send stats: %v", d.ID, err)
			} else {
				rows[i].Activity = view.FormatActivity(stats.Total, stats.PeakPerHour)
			}
			record, err := h.domains.DKIMRecord(d)
			if err != nil {
				logf("panel: domains: domain %d: dkim record: %v", d.ID, err)
				return
			}
			reportEmail, err := h.domainReportAddress(d)
			if err != nil {
				logf("panel: domains: domain %d: dmarc report address: %v", d.ID, err)
				return
			}
			dns, _ := h.domainDNS(d, record, reportEmail, false)
			rows[i].DNS = dnsTags(dns.DKIM.Status, dns.SPF.Status, dns.DMARC.Status)
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
	p, ok := h.requireGlobal(w, r)
	if !ok {
		return
	}
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
	h.followCreator(d.ID, p.ID)
	http.Redirect(w, r, fmt.Sprintf("/outbound/domains/%d", d.ID), http.StatusSeeOther)
}

// followCreator makes a new domain take its DMARC report address from the
// default of whoever created it (plan § Account e-mail). The domain exists
// either way: a failure here leaves it with no report address, which its page
// shows and its DMARC form can change.
func (h *Handlers) followCreator(domainID, userID int64) {
	if err := h.store.SetDomainDMARCUser(domainID, userID); err != nil {
		logf("panel: domain %d: follow user %d's dmarc default: %v", domainID, userID, err)
		return
	}
	if h.dmarc != nil && h.dmarc.Enabled() {
		if err := h.dmarc.Resync(); err != nil {
			logf("panel: domain %d: dmarc resync: %v", domainID, err)
		}
	}
}

// HandleDeleteConfirm shows the cascade warning before a domain is removed.
func (h *Handlers) HandleDeleteConfirm(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	d, ok := h.lookupDomain(w, r)
	if !ok {
		return
	}
	apps, err := h.store.ListApplicationsByDomain(d.ID)
	if err != nil {
		logf("panel: delete domain %d: list applications: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	logins := make([]string, len(apps))
	for i, a := range apps {
		logins[i] = a.Login
	}
	h.view.Render(w, http.StatusOK, "out-domain-delete", view.NewOutDomainDelete(h.shellMeta(r), d.ID, d.Name, logins))
}

// HandleDeleteDomain performs the deletion and returns to the domain list.
func (h *Handlers) HandleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
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
	http.Redirect(w, r, "/outbound/domains?deleted=1", http.StatusSeeOther)
}

// HandleReload re-applies both the OpenDKIM configuration and the Postfix
// sender map on demand (architecture.md § Panel HTTP surface).
func (h *Handlers) HandleReload(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
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
	if h.cfg.InboundEnabled && h.inbound != nil {
		if err := h.inbound.Resync(); err != nil {
			logf("panel: manual reload (inbound): %v", err)
			http.Error(w, "reload failed", http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(w, r, "/server/health?reloaded=1", http.StatusSeeOther)
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
	p, ok := h.principal(r)
	if !ok || !p.CanAccessDomain(d.ID) {
		http.NotFound(w, r)
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
