package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/dmarc"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
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

type countingMaps struct{ n int }

func (c *countingMaps) RebuildDMARCMaps(postfix.DMARCMapsConfig) error { c.n++; return nil }

// Changing the e-mail of a user whose default report address is "my account
// e-mail" rebuilds the ingest allow-list, exactly as the Account page does; for
// anyone else, or when the address is not changed, nothing is rebuilt.
func TestUserFormEmailChangeResyncsDMARCOnlyForTheAccountDefault(t *testing.T) {
	h, st := inboundHandlers(t)
	maps := &countingMaps{}
	h.dmarc = dmarc.NewService(st, maps, "mail.example.org", true)
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	follower := userWith(t, st, "follower", store.RoleGlobal, store.Reach{})
	other := userWith(t, st, "other", store.RoleGlobal, store.Reach{})
	if err := st.SetDMARCDefault(follower.ID, store.DMARCDefaultAccount, ""); err != nil {
		t.Fatal(err)
	}
	save := func(who auth.Principal, email string) {
		t.Helper()
		rec := send(h.HandleUserEdit, &root, "POST", "/server/users/"+idStr(who.ID), map[string]string{"uid": idStr(who.ID)},
			url.Values{"username": {who.Username}, "email": {email}, "role": {"global"}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("save %s = %d:\n%s", who.Username, rec.Code, rec.Body.String())
		}
	}

	save(other, "other@example.org")
	if maps.n != 0 {
		t.Errorf("a user whose default is not the account e-mail rebuilt the maps %d time(s)", maps.n)
	}
	save(follower, "follower@example.org")
	if maps.n != 1 {
		t.Errorf("changing the account e-mail of a follower rebuilt the maps %d time(s), want 1", maps.n)
	}
	save(follower, "follower@example.org")
	if maps.n != 1 {
		t.Errorf("saving the same address again rebuilt the maps (%d)", maps.n)
	}
}

// Deleting a user leaves every domain that follows their default report address
// with none; the confirmation says which, and says nothing of the kind when no
// domain follows.
func TestUserDeleteConfirmationNamesTheDomainsThatLoseTheirReportAddress(t *testing.T) {
	h, st := inboundHandlers(t)
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	u := userWith(t, st, "shop", store.RoleGlobal, store.Reach{})
	path, pv := "/server/users/"+idStr(u.ID)+"/delete", map[string]string{"uid": idStr(u.ID)}
	page := func() string { return send(h.HandleUserDeleteConfirm, &root, "GET", path, pv, nil).Body.String() }

	if body := page(); strings.Contains(body, "DMARC report address") {
		t.Errorf("no domain follows the user, yet the page speaks of DMARC:\n%s", body)
	}

	var follows []store.Domain
	for _, name := range []string{"a.example.com", "b.example.com", "c.example.com", "d.example.com"} {
		d, _ := st.AddDomain(name, "mail")
		if err := st.SetDomainDMARCUser(d.ID, u.ID); err != nil {
			t.Fatal(err)
		}
		follows = append(follows, d)
	}
	elsewhere, _ := st.AddDomain("z.example.com", "mail")
	if err := st.SetDomainDMARCUser(elsewhere.ID, root.ID); err != nil {
		t.Fatal(err)
	}

	body := page()
	want := `<strong>a.example.com</strong>, <strong>b.example.com</strong>, <strong>c.example.com</strong> and 1 more will have no DMARC report address until someone sets one.`
	if !strings.Contains(body, want) {
		t.Errorf("the delete page is missing %q:\n%s", want, body)
	}
	if strings.Contains(body, "z.example.com") || strings.Contains(body, "d.example.com") {
		t.Error("the page names a domain that does not follow this user, or spells out a fourth name")
	}

	// What it says is what happens: the follower domains end up with no address.
	if rec := send(h.HandleUserDelete, &root, "POST", path, pv, url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("delete = %d", rec.Code)
	}
	for _, d := range follows {
		got, _ := st.GetDomain(d.ID)
		rua, err := st.DomainDMARCRua(got, "")
		if err != nil || rua != "" {
			t.Errorf("%s still reports to %q (%v) after its user was deleted", d.Name, rua, err)
		}
	}
}
