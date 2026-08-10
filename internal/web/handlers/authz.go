package handlers

import (
	"net/http"

	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
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

func (h *Handlers) pageBase(r *http.Request) map[string]any {
	p, _ := h.principal(r)
	return map[string]any{
		"User":     auth.CurrentUser(r),
		"IsGlobal": p.IsGlobal(),
	}
}

func (h *Handlers) assignedDomains(p auth.Principal) ([]store.Domain, error) {
	if p.IsGlobal() {
		return h.store.ListDomains()
	}
	all, err := h.store.ListDomains()
	if err != nil {
		return nil, err
	}
	var out []store.Domain
	for _, d := range all {
		if p.CanAccessDomain(d.ID) {
			out = append(out, d)
		}
	}
	return out, nil
}

func domainNameSet(domains []store.Domain) map[string]bool {
	m := make(map[string]bool, len(domains))
	for _, d := range domains {
		m[d.Name] = true
	}
	return m
}

func domainIDSet(p auth.Principal) map[int64]bool {
	if p.IsGlobal() {
		return nil
	}
	m := make(map[int64]bool, len(p.Domains))
	for _, id := range p.Domains {
		m[id] = true
	}
	return m
}
