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
			dns, _ := h.domainDNS(d, record, d.DMARCRua, false)
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
	_, ok := h.requireGlobal(w, r)
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
	http.Redirect(w, r, fmt.Sprintf("/outbound/domains/%d", d.ID), http.StatusSeeOther)
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

// HandleReload writes again, from the database, every file the panel keeps
// for the mail path and has OpenDKIM and Postfix re-read them - what the
// first start after a restore does (cmd/panel mailStack.Resync): OpenDKIM's
// tables, the sender map, with the inbound relay on its maps and the filter
// lists, with DMARC reports on their maps (architecture.md § Panel HTTP
// surface). Before 2.5.4 it left the last two out, and said "regenerated"
// over them (audit 2026-10-12, L13). A failure is a message on Health that
// names the step; the log has the error.
func (h *Handlers) HandleReload(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	type step struct {
		name string
		run  func() error
	}
	steps := []step{
		{"OpenDKIM's tables", h.domains.Resync},
		{"the sender map", h.apps.Resync},
	}
	if h.cfg.InboundEnabled && h.inbound != nil {
		steps = append(steps, step{"the inbound relay maps", h.inbound.Resync})
	}
	if h.cfg.InboundEnabled && h.lists != nil {
		steps = append(steps, step{"the filter lists", h.lists.Sync})
	}
	if h.dmarc != nil && h.dmarc.Enabled() {
		steps = append(steps, step{"the DMARC report maps", h.dmarc.Resync})
	}
	for _, s := range steps {
		if err := s.run(); err != nil {
			logf("panel: manual reload (%s): %v", s.name, err)
			page := h.healthView(r, "")
			page.Flash = &view.Flash{Error: true, Text: view.Plain("Reload failed at " + s.name + "; what came before it was written and re-read. The panel's log says why.")}
			h.view.Render(w, http.StatusInternalServerError, "health", page)
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
