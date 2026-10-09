package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mixeme/selfpost/internal/web/auth"
)

// route is one entry of the authenticated mux, named the way web.go registers
// it so a route added there without a guard is visible as a missing case here.
type route struct {
	method  string
	target  string
	handler func(*Handlers) http.HandlerFunc
	// pathValues are the {id}-style segments the router would have bound.
	pathValues map[string]string
}

// globalOnlyRoutes is every page and action that only a global administrator
// may reach: the panel's users, the whole-server backup and domain import, the
// machine-wide status and log views, and the domain lifecycle. A domain
// administrator is answered 404 rather than 403 so the panel does not confirm
// that the page exists (security.md).
var globalOnlyRoutes = []route{
	{"GET", "/server/components", func(h *Handlers) http.HandlerFunc { return h.HandleComponents }, nil},

	{"GET", "/server/users", func(h *Handlers) http.HandlerFunc { return h.HandleUsers }, nil},
	{"GET", "/server/users/new", func(h *Handlers) http.HandlerFunc { return h.HandleUserNew }, nil},
	{"POST", "/server/users/new", func(h *Handlers) http.HandlerFunc { return h.HandleUserNew }, nil},
	{"GET", "/server/users/1", func(h *Handlers) http.HandlerFunc { return h.HandleUserEdit }, map[string]string{"uid": "1"}},
	{"POST", "/server/users/1", func(h *Handlers) http.HandlerFunc { return h.HandleUserEdit }, map[string]string{"uid": "1"}},
	{"GET", "/server/users/1/delete", func(h *Handlers) http.HandlerFunc { return h.HandleUserDeleteConfirm }, map[string]string{"uid": "1"}},
	{"POST", "/server/users/1/delete", func(h *Handlers) http.HandlerFunc { return h.HandleUserDelete }, map[string]string{"uid": "1"}},

	{"GET", "/server/backup", func(h *Handlers) http.HandlerFunc { return h.HandleBackupPage }, nil},
	{"POST", "/server/backup", func(h *Handlers) http.HandlerFunc { return h.HandleBackup }, nil},
	{"POST", "/server/backup/import", func(h *Handlers) http.HandlerFunc { return h.HandleImportDomain }, nil},

	{"GET", "/overview", func(h *Handlers) http.HandlerFunc { return h.HandleStatus }, nil},
	{"GET", "/overview/fragment", func(h *Handlers) http.HandlerFunc { return h.HandleStatusFragment }, nil},
	{"POST", "/server/health/recheck", func(h *Handlers) http.HandlerFunc { return h.HandleStatusRecheck }, nil},

	{"GET", "/outbound/queue", func(h *Handlers) http.HandlerFunc { return h.HandleMailQueue }, nil},
	{"GET", "/outbound/queue/fragment", func(h *Handlers) http.HandlerFunc { return h.HandleMailQueueBody }, nil},
	{"GET", "/server/log", func(h *Handlers) http.HandlerFunc { return h.HandleSystemLog }, nil},
	{"GET", "/server/log/fragment", func(h *Handlers) http.HandlerFunc { return h.HandleSystemLogBody }, nil},

	{"POST", "/outbound/domains", func(h *Handlers) http.HandlerFunc { return h.HandleAddDomain }, nil},
	{"GET", "/outbound/domains/1/delete", func(h *Handlers) http.HandlerFunc { return h.HandleDeleteConfirm }, map[string]string{"id": "1"}},
	{"POST", "/outbound/domains/1/delete", func(h *Handlers) http.HandlerFunc { return h.HandleDeleteDomain }, map[string]string{"id": "1"}},
	{"POST", "/server/health/reload", func(h *Handlers) http.HandlerFunc { return h.HandleReload }, nil},

	// Inbound: only adding and deleting a domain are global. The list and the
	// pages of one inbound domain are delegated per domain and have their own
	// table in handlers_delegation_test.go.
	{"POST", "/inbound/domains", func(h *Handlers) http.HandlerFunc { return h.HandleAddInbound }, nil},
	{"GET", "/inbound/domains/1/delete", func(h *Handlers) http.HandlerFunc { return h.HandleInboundDeleteConfirm }, map[string]string{"id": "1"}},
	{"POST", "/inbound/domains/1/delete", func(h *Handlers) http.HandlerFunc { return h.HandleInboundDelete }, map[string]string{"id": "1"}},
}

// A domain administrator has an account on the panel, so authentication is not
// what keeps them off these pages — the per-handler role check is. Each of them
// is reached here with a valid session for a principal that owns a domain, the
// case the send-log leak (P0, code-review.md) showed is easy to get wrong.
func TestGlobalOnlyRoutesAnswerADomainAdmin404(t *testing.T) {
	h, domains := serverWithTwoDomains(t)
	p := domainAdmin(t, h.store, "global-only", domains["first.example.ru"].ID)

	for _, rt := range globalOnlyRoutes {
		rec := call(h, rt, p)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s as a domain administrator = %d, want 404:\n%s",
				rt.method, rt.target, rec.Code, rec.Body.String())
		}
	}
}

// The same 404 covers a request that carries no principal at all: the auth
// middleware normally redirects those, but a handler must not depend on
// middleware it cannot see for the role it enforces itself.
func TestGlobalOnlyRoutesAnswerAnUnknownPrincipal404(t *testing.T) {
	h, _ := serverWithTwoDomains(t)

	for _, rt := range globalOnlyRoutes {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(rt.method, rt.target, nil)
		for k, v := range rt.pathValues {
			req.SetPathValue(k, v)
		}
		rt.handler(h)(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s with no principal = %d, want 404", rt.method, rt.target, rec.Code)
		}
	}
}

// The 404s above would also pass if a handler were broken into always returning
// one, so at least the two pages that need nothing but the store and the view
// have to be shown opening for a global administrator.
func TestGlobalOnlyRoutesOpenForAGlobalAdministrator(t *testing.T) {
	h, _ := serverWithTwoDomains(t)

	for _, target := range []string{"/server/users", "/server/backup"} {
		rt := getRoute(t, target)
		if rec := call(h, rt, globalPrincipal); rec.Code != http.StatusOK {
			t.Errorf("GET %s as a global administrator = %d, want 200:\n%s",
				target, rec.Code, rec.Body.String())
		}
	}
}

func getRoute(t *testing.T, target string) route {
	t.Helper()
	for _, rt := range globalOnlyRoutes {
		if rt.method == http.MethodGet && rt.target == target {
			return rt
		}
	}
	t.Fatalf("no GET %s among the global-only routes", target)
	return route{}
}

func call(h *Handlers, rt route, p auth.Principal) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(rt.method, rt.target, nil)
	req = auth.RequestWithPrincipal(req, p)
	for k, v := range rt.pathValues {
		req.SetPathValue(k, v)
	}
	rt.handler(h)(rec, req)
	return rec
}
