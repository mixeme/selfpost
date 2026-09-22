package auth

import (
	"context"
	"net/http"

	"github.com/mixeme/selfpost/internal/store"
)

type ctxKey int

const (
	usernameKey   ctxKey = 0
	principalKey  ctxKey = 1
)

// Role is a panel user's access level.
type Role = store.Role

const (
	RoleGlobal      = store.RoleGlobal
	RoleDomainAdmin = store.RoleDomainAdmin
)

// Principal is the authenticated panel user attached to a request.
type Principal struct {
	ID       int64
	Username string
	Role     Role
	Domains  []int64 // assigned domain IDs; empty for global (all domains)
}

// IsGlobal reports whether the principal has full panel access.
func (p Principal) IsGlobal() bool {
	return p.Role == RoleGlobal
}

// CanAccessDomain reports whether the principal may access a domain id.
func (p Principal) CanAccessDomain(domainID int64) bool {
	if p.IsGlobal() {
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
