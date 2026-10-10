package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/store"
)

// The user form sets the account e-mail: create stores it, an edit changes it,
// clearing the field clears it, and a post that does not mention the field at
// all leaves it alone.
func TestUserFormSetsTheAccountEmail(t *testing.T) {
	h, st := inboundHandlers(t)
	out1, _ := st.AddDomain("one.example.com", "mail")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})

	rec := send(h.HandleUserNew, &root, "POST", "/server/users/new", nil, url.Values{
		"username": {"ops"}, "email": {"  ops@acme.io "}, "password": {"a-long-enough-password-1"}, "role": {"domain"},
		"domain_ids": {idStr(out1.ID)},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create with an e-mail = %d:\n%s", rec.Code, rec.Body.String())
	}
	u, _ := st.GetUserByUsername("ops")
	if u.Email != "ops@acme.io" || len(u.DomainIDs) != 1 {
		t.Fatalf("created user = %+v", u)
	}

	path, pv := "/server/users/"+idStr(u.ID), map[string]string{"uid": idStr(u.ID)}
	post := func(extra url.Values) store.User {
		t.Helper()
		form := url.Values{"username": {"ops"}, "role": {"domain"}, "domain_ids": {idStr(out1.ID)}}
		for k, v := range extra {
			form[k] = v
		}
		if rec := send(h.HandleUserEdit, &root, "POST", path, pv, form); rec.Code != http.StatusSeeOther {
			t.Fatalf("POST = %d:\n%s", rec.Code, rec.Body.String())
		}
		got, _ := st.GetUser(u.ID)
		return got
	}
	if got := post(url.Values{"email": {"new@acme.io"}}); got.Email != "new@acme.io" {
		t.Errorf("changed e-mail = %q", got.Email)
	}
	if got := post(nil); got.Email != "new@acme.io" {
		t.Errorf("a post without the field changed the e-mail to %q", got.Email)
	}
	if got := post(url.Values{"email": {""}}); got.Email != "" {
		t.Errorf("clearing the field left %q", got.Email)
	}

	// Creating without one is fine.
	rec = send(h.HandleUserNew, &root, "POST", "/server/users/new", nil, url.Values{
		"username": {"plain"}, "password": {"a-long-enough-password-1"}, "role": {"global"}})
	if plain, _ := st.GetUserByUsername("plain"); rec.Code != http.StatusSeeOther || plain.Email != "" {
		t.Errorf("create without an e-mail = %d, %q", rec.Code, plain.Email)
	}
}

// A bad address is refused on both forms: nothing is saved, and the form comes
// back with what was typed, the refusal as the flash.
func TestUserFormRefusesABadEmailAndKeepsWhatWasTyped(t *testing.T) {
	h, st := inboundHandlers(t)
	out1, _ := st.AddDomain("one.example.com", "mail")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	u := userWith(t, st, "ops", store.RoleDomain, store.Reach{DomainIDs: []int64{out1.ID}})
	if err := st.UpdateUser(u.ID, "ops", "test-hash", "ops@acme.io"); err != nil {
		t.Fatal(err)
	}

	rec := send(h.HandleUserEdit, &root, "POST", "/server/users/"+idStr(u.ID), map[string]string{"uid": idStr(u.ID)},
		url.Values{"username": {"ops"}, "email": {"not-an-address"}, "role": {"domain"}, "domain_ids": {idStr(out1.ID)}})
	body := rec.Body.String()
	for _, want := range []string{`class="notification is-danger is-light"`, "enter a valid email address",
		`name="email" type="email" value="not-an-address"`, `<h1 class="title is-3">ops</h1>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the refused edit form is missing %q", want)
		}
	}
	if after, _ := st.GetUser(u.ID); rec.Code != http.StatusBadRequest || after.Email != "ops@acme.io" {
		t.Errorf("a bad address on edit = %d, stored e-mail %q", rec.Code, after.Email)
	}

	rec = send(h.HandleUserNew, &root, "POST", "/server/users/new", nil, url.Values{
		"username": {"fresh"}, "email": {"nobody@"}, "password": {"a-long-enough-password-1"}, "role": {"global"}})
	body = rec.Body.String()
	for _, want := range []string{"enter a valid email address", `name="email" type="email" value="nobody@"`, `name="username" value="fresh"`,
		`<h1 class="title is-3">Create user</h1>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the refused create form is missing %q", want)
		}
	}
	if fresh, _ := st.GetUserByUsername("fresh"); rec.Code != http.StatusBadRequest || fresh.ID != 0 {
		t.Errorf("a bad address on create = %d, user created: %v", rec.Code, fresh.ID != 0)
	}
}
