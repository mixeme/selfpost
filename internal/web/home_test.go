package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
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

// The test above walks the routes table; a /server/ route registered beside
// it, or past the server() guard, would not be looked at. Every pattern under
// /server/ that web.go registers goes through server() and is in the table.
func TestEveryServerRouteIsGuardedAndListed(t *testing.T) {
	src, err := os.ReadFile("web.go")
	if err != nil {
		t.Fatal(err)
	}
	found := regexp.MustCompile(`([\w.]+)\("((?:GET|POST|PUT|DELETE|PATCH|HEAD) )?(/server/[^"]*)"`).FindAllStringSubmatch(string(src), -1)
	if len(found) < 15 {
		t.Fatalf("only %d /server/ patterns found in web.go", len(found))
	}
	for _, m := range found {
		pattern := m[2] + m[3]
		if m[1] != "server" {
			t.Errorf("%q is registered by %s, not server(): the subtree's guard is skipped", pattern, m[1])
		}
		if !slices.Contains(routes.always, pattern) {
			t.Errorf("%q is not in routes.always, so no test opens it as a domain administrator", pattern)
		}
	}
}
