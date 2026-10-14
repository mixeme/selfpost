package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/store"
)

// The users form's writes, driven through the handlers (audit 2026-10-12, M7
// and M9: the save is one store call now, and these pin what it does).

// A save that changes name, e-mail and role lands whole; promoted to global
// the user keeps no assignment.
func TestUserFormSavesTheWholeChange(t *testing.T) {
	h, st := inboundHandlers(t)
	out1, _ := st.AddDomain("out.example.com", "mail")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	u := userWith(t, st, "shop", store.RoleDomain, store.Reach{DomainIDs: []int64{out1.ID}})
	rec := send(h.HandleUserEdit, &root, "POST", "/server/users/"+idStr(u.ID), map[string]string{"uid": idStr(u.ID)},
		url.Values{"username": {"shop2"}, "email": {"shop@example.com"}, "role": {"global"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST = %d:\n%s", rec.Code, rec.Body.String())
	}
	after, err := st.GetUser(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Username != "shop2" || after.Email != "shop@example.com" || after.Role != store.RoleGlobal || !after.Reach().Empty() {
		t.Fatalf("after the save: %+v", after)
	}

	// And back to a domain user, with an assignment.
	rec = send(h.HandleUserEdit, &root, "POST", "/server/users/"+idStr(u.ID), map[string]string{"uid": idStr(u.ID)},
		url.Values{"username": {"shop2"}, "email": {"shop@example.com"}, "role": {"domain"}, "domain_ids": {idStr(out1.ID)}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST = %d:\n%s", rec.Code, rec.Body.String())
	}
	after, _ = st.GetUser(u.ID)
	if after.Role != store.RoleDomain || len(after.DomainIDs) != 1 || after.DomainIDs[0] != out1.ID {
		t.Fatalf("demoted: %+v", after)
	}
}

// The last global administrator is not demoted, and a refused save changes
// nothing - not the name typed in the same form either.
func TestUserFormKeepsTheLastGlobal(t *testing.T) {
	h, st := inboundHandlers(t)
	out1, _ := st.AddDomain("out.example.com", "mail")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	rec := send(h.HandleUserEdit, &root, "POST", "/server/users/"+idStr(root.ID), map[string]string{"uid": idStr(root.ID)},
		url.Values{"username": {"renamed"}, "role": {"domain"}, "domain_ids": {idStr(out1.ID)}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "You cannot demote yourself without another global administrator.") {
		t.Fatalf("demoting the last global = %d:\n%s", rec.Code, rec.Body.String())
	}
	after, _ := st.GetUser(root.ID)
	if after.Username != "root" || after.Role != store.RoleGlobal {
		t.Fatalf("the refused save changed the user: %+v", after)
	}
}

// A domain user must keep something assigned; the refusal changes nothing.
func TestUserFormRefusesADomainUserWithNothingAssigned(t *testing.T) {
	h, st := inboundHandlers(t)
	out1, _ := st.AddDomain("out.example.com", "mail")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	u := userWith(t, st, "shop", store.RoleDomain, store.Reach{DomainIDs: []int64{out1.ID}})
	rec := send(h.HandleUserEdit, &root, "POST", "/server/users/"+idStr(u.ID), map[string]string{"uid": idStr(u.ID)},
		url.Values{"username": {"renamed"}, "role": {"domain"}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Assign at least one outbound or inbound domain") {
		t.Fatalf("nothing assigned = %d:\n%s", rec.Code, rec.Body.String())
	}
	after, _ := st.GetUser(u.ID)
	if after.Username != "shop" || len(after.DomainIDs) != 1 {
		t.Fatalf("the refused save changed the user: %+v", after)
	}
}

// A user is created with the e-mail and the assignment in the one save.
func TestUserFormCreatesWithEmailAndReach(t *testing.T) {
	h, st := inboundHandlers(t)
	out1, _ := st.AddDomain("out.example.com", "mail")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	rec := send(h.HandleUserNew, &root, "POST", "/server/users/new", nil,
		url.Values{"username": {"fresh"}, "password": {"a-long-enough-password"}, "email": {"fresh@example.com"},
			"role": {"domain"}, "domain_ids": {idStr(out1.ID)}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create = %d:\n%s", rec.Code, rec.Body.String())
	}
	u, err := st.GetUserByUsername("fresh")
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "fresh@example.com" || u.Role != store.RoleDomain || len(u.DomainIDs) != 1 {
		t.Fatalf("created %+v", u)
	}
}
