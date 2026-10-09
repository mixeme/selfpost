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

// domainReportAddress resolves where a domain's DMARC aggregate reports go:
// its own address, or the default of the user it follows. "" is a policy-only
// record.
func (h *Handlers) domainReportAddress(d store.Domain) (string, error) {
	return h.store.DomainDMARCRua(d, h.hostedDMARCAddress(d.Name))
}

func domainNameSet(domains []store.Domain) map[string]bool {
	m := make(map[string]bool, len(domains))
	for _, d := range domains {
		m[d.Name] = true
	}
	return m
}

// domainIDSet is the set of sending domains a principal is limited to; nil
// means no limit (the global role, or a domain user with All).
func domainIDSet(p auth.Principal) map[int64]bool {
	if p.IsGlobal() || p.AllDomains {
		return nil
	}
	m := make(map[int64]bool, len(p.Domains))
	for _, id := range p.Domains {
		m[id] = true
	}
	return m
}
