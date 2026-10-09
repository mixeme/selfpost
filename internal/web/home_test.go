package web

import (
	"net/http"
	"net/http/httptest"
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
		{"global", auth.Principal{Role: auth.RoleGlobal}, "/status"},
		{"outbound", auth.Principal{Role: auth.RoleDomain, Domains: []int64{1}}, "/domains"},
		{"both", auth.Principal{Role: auth.RoleDomain, AllDomains: true, AllInbound: true}, "/domains"},
		{"inbound only", auth.Principal{Role: auth.RoleDomain, InboundDomains: []int64{1}}, "/inbound"},
	} {
		rec := httptest.NewRecorder()
		redirectHome(rec, auth.RequestWithPrincipal(httptest.NewRequest(http.MethodGet, "/", nil), tc.p))
		if got := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || got != tc.want {
			t.Errorf("%s: %d to %q, want 303 to %q", tc.name, rec.Code, got, tc.want)
		}
	}
}
