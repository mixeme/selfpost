package auth

import (
	"testing"

	"github.com/mixeme/selfpost/internal/store"
)

// Outbound and inbound are granted separately: an id on one list opens nothing
// on the other, All opens a whole direction, and the global role opens both.
func TestPrincipalReach(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		p                       Principal
		out7, out8, in7, in8    bool
		hasOutbound, hasInbound bool
	}{
		{"global", Principal{Role: RoleGlobal}, true, true, true, true, true, true},
		{"outbound 7", Principal{Role: RoleDomain, Domains: []int64{7}}, true, false, false, false, true, false},
		{"inbound 7", Principal{Role: RoleDomain, InboundDomains: []int64{7}}, false, false, true, false, false, true},
		{"all outbound", Principal{Role: RoleDomain, AllDomains: true}, true, true, false, false, true, false},
		{"all inbound, outbound 8", Principal{Role: RoleDomain, AllInbound: true, Domains: []int64{8}}, false, true, true, true, true, true},
		{"nothing", Principal{Role: RoleDomain}, false, false, false, false, false, false},
	} {
		got := []bool{
			tc.p.CanAccessDomain(7), tc.p.CanAccessDomain(8), tc.p.CanAccessInboundDomain(7), tc.p.CanAccessInboundDomain(8),
			tc.p.HasOutbound(), tc.p.HasInbound(),
		}
		want := []bool{tc.out7, tc.out8, tc.in7, tc.in8, tc.hasOutbound, tc.hasInbound}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s: [out 7, out 8, in 7, in 8, has out, has in] = %v, want %v", tc.name, got, want)
				break
			}
		}
	}
	if !(Principal{Role: RoleDomain, Domains: []int64{7}}).CanAccessApp(store.Application{DomainID: 7}) {
		t.Error("an application follows its sending domain")
	}
}

// The principal a request carries is the user's whole reach, flags included.
func TestPrincipalFromUser(t *testing.T) {
	p := principalFromUser(store.User{
		ID: 3, Username: "ops", Role: store.RoleDomain,
		DomainIDs: []int64{1}, AllDomains: true, InboundDomainIDs: []int64{2, 4}, AllInboundDomains: true,
	})
	if p.ID != 3 || p.Username != "ops" || p.Role != RoleDomain || !p.AllDomains || !p.AllInbound ||
		len(p.Domains) != 1 || len(p.InboundDomains) != 2 {
		t.Errorf("principal = %+v", p)
	}
}
