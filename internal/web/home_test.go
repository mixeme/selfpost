package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/web/auth"
)

// The root sends each user to the first thing they reach.
func TestRedirectHomeFollowsTheReach(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    auth.Principal
		want string
	}{
		{"global", auth.Principal{Role: auth.RoleGlobal}, "/overview"},
		{"outbound", auth.Principal{Role: auth.RoleDomain, Domains: []int64{1}}, "/outbound/domains"},
		{"both", auth.Principal{Role: auth.RoleDomain, AllDomains: true, AllInbound: true}, "/outbound/domains"},
		{"inbound only", auth.Principal{Role: auth.RoleDomain, InboundDomains: []int64{1}}, "/inbound/domains"},
	} {
		rec := httptest.NewRecorder()
		redirectHome(rec, auth.RequestWithPrincipal(httptest.NewRequest(http.MethodGet, "/", nil), tc.p))
		if got := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || got != tc.want {
			t.Errorf("%s: %d to %q, want 303 to %q", tc.name, rec.Code, got, tc.want)
		}
	}
}

// Everything under /server/ is the global role's, and the subtree is guarded
// where the routes are registered: a domain administrator — whatever they are
// assigned — and a request with no principal get 404 from the router itself,
// before any handler runs. The handlers here are nil; reaching one would panic.
func TestServerSubtreeIsGlobalOnly(t *testing.T) {
	_, authed := (&Server{cfg: Config{InboundEnabled: true, DMARCEnabled: true}}).muxes()
	everything := auth.Principal{Role: auth.RoleDomain, AllDomains: true, AllInbound: true}
	checked := 0
	for _, pattern := range routes.always {
		method, target := concrete(pattern)
		if !strings.HasPrefix(target, "/server/") {
			continue
		}
		if got := answers(authed, method, target); got == "" {
			continue // not built yet; TestRoutesFollowNavigation reports it
		}
		checked++
		for who, req := range map[string]*http.Request{
			"a domain administrator with All on both lists": auth.RequestWithPrincipal(httptest.NewRequest(method, target, nil), everything),
			"no principal": httptest.NewRequest(method, target, nil),
		} {
			rec := httptest.NewRecorder()
			authed.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s %s as %s = %d, want 404", method, target, who, rec.Code)
			}
		}
	}
	if checked < 15 {
		t.Fatalf("only %d /server/ routes were checked", checked)
	}
}
