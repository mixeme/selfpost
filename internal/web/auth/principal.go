package auth

import (
	"context"
	"net/http"

	"github.com/mixeme/selfpost/internal/store"
)

type ctxKey int

const (
	usernameKey  ctxKey = 0
	principalKey ctxKey = 1
)

// Role is a panel user's access level.
type Role = store.Role

const (
	RoleGlobal = store.RoleGlobal
	RoleDomain = store.RoleDomain
)

// Principal is the authenticated panel user attached to a request.
type Principal struct {
	ID       int64
	Username string
	Role     Role
	// What a domain user reaches (store.Reach). All four are unused for the
	// global role, which reaches everything.
	Domains        []int64 // assigned outbound domain ids
	AllDomains     bool    // every outbound domain, including those added later
	InboundDomains []int64 // assigned inbound domain ids
	AllInbound     bool    // every inbound domain, including those added later
}

// IsGlobal reports whether the principal has full panel access.
func (p Principal) IsGlobal() bool {
	return p.Role == RoleGlobal
}

// HasOutbound reports whether the principal reaches any sending domain. A
// group with nothing in it is absent from that user's menu.
func (p Principal) HasOutbound() bool {
	return p.IsGlobal() || p.AllDomains || len(p.Domains) > 0
}

// HasInbound reports whether the principal reaches any inbound domain.
func (p Principal) HasInbound() bool {
	return p.IsGlobal() || p.AllInbound || len(p.InboundDomains) > 0
}

// CanAccessInboundDomain reports whether the principal may access an inbound
// domain id. Inbound is granted separately from outbound: a sending domain of
// the same name gives no access here, and the other way round.
func (p Principal) CanAccessInboundDomain(inboundDomainID int64) bool {
	if p.IsGlobal() || p.AllInbound {
		return true
	}
	for _, id := range p.InboundDomains {
		if id == inboundDomainID {
			return true
		}
	}
	return false
}

// CanAccessDomain reports whether the principal may access a sending domain id.
func (p Principal) CanAccessDomain(domainID int64) bool {
	if p.IsGlobal() || p.AllDomains {
		return true
	}
	for _, id := range p.Domains {
		if id == domainID {
			return true
		}
	}
	return false
}

// CanAccessApp reports whether the principal may access an application.
func (p Principal) CanAccessApp(app store.Application) bool {
	return p.CanAccessDomain(app.DomainID)
}

func principalFromUser(u store.User) Principal {
	return Principal{
		ID:       u.ID,
		Username: u.Username,
		Role:     u.Role,
		Domains:  u.DomainIDs,

		AllDomains:     u.AllDomains,
		InboundDomains: u.InboundDomainIDs,
		AllInbound:     u.AllInboundDomains,
	}
}

func withPrincipal(ctx context.Context, p Principal) context.Context {
	ctx = context.WithValue(ctx, usernameKey, p.Username)
	return context.WithValue(ctx, principalKey, p)
}

// CurrentPrincipal returns the authenticated principal from the request context.
func CurrentPrincipal(ctx context.Context) (Principal, bool) {
	if v, ok := ctx.Value(principalKey).(Principal); ok {
		return v, true
	}
	return Principal{}, false
}

// PrincipalFromRequest returns the authenticated principal from an HTTP request.
func PrincipalFromRequest(r *http.Request) (Principal, bool) {
	return CurrentPrincipal(r.Context())
}

// RequestWithPrincipal attaches a principal for middleware-equivalent tests.
func RequestWithPrincipal(r *http.Request, p Principal) *http.Request {
	return r.WithContext(withPrincipal(r.Context(), p))
}
