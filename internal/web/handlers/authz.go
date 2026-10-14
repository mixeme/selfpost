package handlers

import (
	"net/http"

	"github.com/mixeme/selfpost/internal/dmarc"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
	"github.com/mixeme/selfpost/internal/web/view"
)

func (h *Handlers) principal(r *http.Request) (auth.Principal, bool) {
	return auth.PrincipalFromRequest(r)
}

func (h *Handlers) requireGlobal(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	p, ok := h.principal(r)
	if !ok || !p.IsGlobal() {
		http.NotFound(w, r)
		return auth.Principal{}, false
	}
	return p, true
}

// shellMeta is what the shell of a page of the component kit needs to know
// about the person looking at it: their name and which groups of the menu they
// reach. The page's constructor adds its title and where it sits in the menu.
func (h *Handlers) shellMeta(r *http.Request) view.Meta {
	p, _ := h.principal(r)
	user := auth.CurrentUser(r)
	if user == "" {
		user = p.Username
	}
	return view.Meta{User: user, IsGlobal: p.IsGlobal(), HasOutbound: p.HasOutbound(), HasInbound: p.HasInbound()}
}

func (h *Handlers) assignedDomains(p auth.Principal) ([]store.Domain, error) {
	if p.IsGlobal() {
		return h.store.ListDomains()
	}
	return h.store.ListDomainsForUser(p.ID)
}

// hostedDMARCAddress is the SelfPost-hosted report mailbox for a sending
// domain, "" when report ingest is off on this server.
func (h *Handlers) hostedDMARCAddress(domainName string) string {
	if h.dmarc == nil || !h.cfg.DMARCEnabled {
		return ""
	}
	return dmarc.HostedReportAddress(h.cfg.Hostname, domainName)
}

func domainNameSet(domains []store.Domain) map[string]bool {
	m := make(map[string]bool, len(domains))
	for _, d := range domains {
		m[d.Name] = true
	}
	return m
}

// domainIDSet is the set of the ids of domains; a send-log row's DomainID is
// in it when the row belongs to one of them (0, a row of no domain, never is).
func domainIDSet(domains []store.Domain) map[int64]bool {
	m := make(map[int64]bool, len(domains))
	for _, d := range domains {
		m[d.ID] = true
	}
	return m
}
