package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
	"github.com/mixeme/selfpost/internal/web/validate"
	"github.com/mixeme/selfpost/internal/web/view"
)

// Inbound is delegated to domain administrators separately from outbound (plan
// § Who sees what), so its handlers come in three strengths:
//
//   - requireInbound        the list: anyone who reaches at least one inbound
//                           domain — and the list itself is filtered;
//   - requireInboundDomain  one domain (its page, DNS re-check, upstream,
//                           recipients): the global role, or a domain user
//                           this very inbound domain is assigned to;
//   - requireInboundGlobal  adding and deleting a domain: the global role only.
//
// All three answer 404 — for a missing domain, for another tenant's, and for a
// user with no inbound reach alike — so the panel never confirms what exists.

func (h *Handlers) requireInbound(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	if !h.cfg.InboundEnabled || h.inbound == nil {
		http.NotFound(w, r)
		return auth.Principal{}, false
	}
	p, ok := h.principal(r)
	if !ok || !p.HasInbound() {
		http.NotFound(w, r)
		return auth.Principal{}, false
	}
	return p, true
}

func (h *Handlers) requireInboundGlobal(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	if !h.cfg.InboundEnabled || h.inbound == nil {
		http.NotFound(w, r)
		return auth.Principal{}, false
	}
	return h.requireGlobal(w, r)
}

// requireInboundDomain resolves the {id} of the request to an inbound domain
// the principal may work with.
func (h *Handlers) requireInboundDomain(w http.ResponseWriter, r *http.Request) (store.InboundDomain, bool) {
	p, ok := h.requireInbound(w, r)
	if !ok {
		return store.InboundDomain{}, false
	}
	d, ok := h.lookupInbound(w, r)
	if !ok {
		return store.InboundDomain{}, false
	}
	if !p.CanAccessInboundDomain(d.ID) {
		http.NotFound(w, r)
		return store.InboundDomain{}, false
	}
	return d, true
}

// assignedInboundDomains lists the inbound domains a principal reaches. The
// filter is in the query (ListInboundDomainsForUser), not in the template.
func (h *Handlers) assignedInboundDomains(p auth.Principal) ([]store.InboundDomain, error) {
	if p.IsGlobal() {
		return h.inbound.List()
	}
	return h.store.ListInboundDomainsForUser(p.ID)
}

func (h *Handlers) lookupInbound(w http.ResponseWriter, r *http.Request) (store.InboundDomain, bool) {
	if h.inbound == nil {
		http.NotFound(w, r)
		return store.InboundDomain{}, false
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return store.InboundDomain{}, false
	}
	d, err := h.inbound.Get(id)
	if err != nil {
		if errors.Is(err, store.ErrInboundDomainNotFound) {
			http.NotFound(w, r)
			return store.InboundDomain{}, false
		}
		logf("panel: get inbound domain %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return store.InboundDomain{}, false
	}
	return d, true
}

// HandleInboundList is the inbound-relay domain list: every inbound domain for
// the global role, the assigned ones for a domain administrator.
func (h *Handlers) HandleInboundList(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireInbound(w, r); !ok {
		return
	}
	h.renderInboundList(w, r, http.StatusOK, "", "")
}

func (h *Handlers) renderInboundList(w http.ResponseWriter, r *http.Request, status int, formErr, formName string) {
	p, _ := h.principal(r)
	list, err := h.assignedInboundDomains(p)
	if err != nil {
		logf("panel: inbound list: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	flash := ""
	if r.URL.Query().Get("deleted") != "" {
		flash = "Inbound domain deleted."
	}
	on, _, _ := h.inboundFilter()
	page := view.NewInDomains(h.shellMeta(r), p.IsGlobal()).
		WithFilter(on).
		WithRows(h.inboundRows(list, p.IsGlobal())).
		WithResult(flash, formErr, formName)
	h.view.Render(w, status, "in-domains", page)
}

// inboundRows is one row per inbound domain: the verdict of the MX check
// (cached for a few minutes, as on the domain page), the upstream, who is
// accepted and the TLS to the upstream. The checks run side by side.
func (h *Handlers) inboundRows(domains []store.InboundDomain, canDelete bool) []view.InDomainRow {
	rows := make([]view.InDomainRow, len(domains))
	var wg sync.WaitGroup
	for i, d := range domains {
		in := view.InDomainInput{
			ID: d.ID, Name: d.Name, DNSStatus: string(health.StatusUnknown),
			Host: d.Host, Port: d.Port, TLSMode: d.TLSMode,
			RecipientMode: d.RecipientMode, RecipientCount: d.RecipientCount,
		}
		rows[i] = view.NewInDomainRow(in, canDelete)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if h.dns != nil && h.cfg.Hostname != "" {
				in.DNSStatus = string(h.dns.InboundMX(d.Name, h.cfg.Hostname, false).Status)
				rows[i] = view.NewInDomainRow(in, canDelete)
			}
		}()
	}
	wg.Wait()
	return rows
}

// HandleAddInbound validates the name, creates the inbound domain, and
// redirects to its page.
func (h *Handlers) HandleAddInbound(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireInboundGlobal(w, r); !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderInboundList(w, r, http.StatusBadRequest, "Invalid form submission.", "")
		return
	}
	raw := r.PostFormValue("name")
	name := validate.NormalizeDomain(raw)
	if err := validate.Domain(name); err != nil {
		h.renderInboundList(w, r, http.StatusBadRequest, err.Error(), raw)
		return
	}
	d, err := h.inbound.Add(name)
	if err != nil {
		if errors.Is(err, store.ErrInboundDomainExists) {
			h.renderInboundList(w, r, http.StatusConflict, "That inbound domain is already configured.", raw)
			return
		}
		logf("panel: add inbound domain %q: %v", name, err)
		h.renderInboundList(w, r, http.StatusInternalServerError,
			"Could not add the domain. Please check the logs and try again.", raw)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/inbound/domains/%d", d.ID), http.StatusSeeOther)
}

func (h *Handlers) HandleInboundDetail(w http.ResponseWriter, r *http.Request) {
	d, ok := h.requireInboundDomain(w, r)
	if !ok {
		return
	}
	h.renderInboundDetail(w, r, http.StatusOK, d, inboundDetailView{})
}

type inboundDetailView struct {
	FormErr      string
	TransportErr string
	RecipientErr string
}

func (h *Handlers) renderInboundDetail(w http.ResponseWriter, r *http.Request, status int, d store.InboundDomain, extra inboundDetailView) {
	mx := dnscheck.Result{Status: health.StatusUnknown}
	if h.dns != nil && h.cfg.Hostname != "" {
		mx = h.dns.InboundMX(d.Name, h.cfg.Hostname, false)
	}
	on, milter, action := h.inboundFilter()
	page := view.NewInDomain(h.shellMeta(r), view.InDomainInput{
		ID: d.ID, Name: d.Name, DNSStatus: string(mx.Status), Host: d.Host, Port: d.Port, TLSMode: d.TLSMode,
		RecipientMode: d.RecipientMode, RecipientCount: d.RecipientCount, Addresses: d.Recipients,
	}).
		WithMX(h.cfg.Hostname, view.DNSCheck{Status: string(mx.Status), Detail: mx.Detail, Found: mx.Records}).
		WithFilter(on, milter, action).
		WithResult(inboundFlash(r), firstNonEmpty(extra.FormErr, extra.TransportErr, extra.RecipientErr))
	h.view.Render(w, status, "in-domain", page)
}

// inboundFilter says whether the instance filters inbound mail, and with what:
// the milter address and what Postfix does when it is down. It mirrors
// build/postfix-config.sh, which attaches the filter only when the inbound
// relay is on and a milter is set.
func (h *Handlers) inboundFilter() (on bool, milter, action string) {
	if !h.cfg.InboundEnabled || h.cfg.InboundAntispamMilter == "" {
		return false, "", ""
	}
	return true, h.cfg.InboundAntispamMilter, h.cfg.InboundAntispamAction
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func inboundFlash(r *http.Request) string {
	switch {
	case r.URL.Query().Get("saved") != "":
		return "Upstream saved."
	case r.URL.Query().Get("recipients") != "":
		return "Recipients saved."
	case r.URL.Query().Get("rechecked") != "":
		return "DNS re-checked."
	default:
		return ""
	}
}

func (h *Handlers) HandleInboundDNSRecheck(w http.ResponseWriter, r *http.Request) {
	d, ok := h.requireInboundDomain(w, r)
	if !ok {
		return
	}
	if h.dns != nil && h.cfg.Hostname != "" {
		h.dns.InboundMX(d.Name, h.cfg.Hostname, true)
	}
	http.Redirect(w, r, fmt.Sprintf("/inbound/domains/%d?rechecked=1", d.ID), http.StatusSeeOther)
}

func (h *Handlers) HandleInboundTransport(w http.ResponseWriter, r *http.Request) {
	d, ok := h.requireInboundDomain(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderInboundDetail(w, r, http.StatusBadRequest, d, inboundDetailView{TransportErr: "Invalid form submission."})
		return
	}
	host := r.PostFormValue("host")
	port := r.PostFormValue("port")
	tlsMode := r.PostFormValue("tls_mode")
	if err := h.inbound.SetTransport(d.ID, host, port, tlsMode); err != nil {
		h.renderInboundDetail(w, r, http.StatusBadRequest, d, inboundDetailView{TransportErr: err.Error()})
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/inbound/domains/%d?saved=1", d.ID), http.StatusSeeOther)
}

func (h *Handlers) HandleInboundRecipients(w http.ResponseWriter, r *http.Request) {
	d, ok := h.requireInboundDomain(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderInboundDetail(w, r, http.StatusBadRequest, d, inboundDetailView{RecipientErr: "Invalid form submission."})
		return
	}
	mode := r.PostFormValue("recipient_mode")
	addrs := splitAddresses(r.PostFormValue("addresses"))
	if err := h.inbound.SetRecipients(d.ID, mode, addrs); err != nil {
		h.renderInboundDetail(w, r, http.StatusBadRequest, d, inboundDetailView{RecipientErr: err.Error()})
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/inbound/domains/%d?recipients=1", d.ID), http.StatusSeeOther)
}

func (h *Handlers) HandleInboundDeleteConfirm(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireInboundGlobal(w, r); !ok {
		return
	}
	d, ok := h.lookupInbound(w, r)
	if !ok {
		return
	}
	listed := 0
	if d.RecipientMode != store.RecipientModeAny {
		listed = d.RecipientCount
	}
	h.view.Render(w, http.StatusOK, "in-domain-delete",
		view.NewInDomainDelete(h.shellMeta(r), d.ID, d.Name, d.Host != "", listed))
}

func (h *Handlers) HandleInboundDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireInboundGlobal(w, r); !ok {
		return
	}
	d, ok := h.lookupInbound(w, r)
	if !ok {
		return
	}
	if h.dns != nil {
		h.dns.Forget(d.Name)
	}
	if err := h.inbound.Delete(d.ID); err != nil {
		if errors.Is(err, store.ErrInboundDomainNotFound) {
			http.NotFound(w, r)
			return
		}
		logf("panel: delete inbound domain %d: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/inbound/domains?deleted=1", http.StatusSeeOther)
}
