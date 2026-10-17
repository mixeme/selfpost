package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

// An address the global administrator types for a user is unconfirmed until
// that user opens its link: creating the user with one, or saving a new one,
// sends it; the form tags it either way.
func TestUserFormSendsANewAddressItsLink(t *testing.T) {
	h, st := inboundHandlers(t)
	ml := withMail(t, h)
	out1, _ := st.AddDomain("one.example.com", "mail")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})

	rec := send(h.HandleUserNew, &root, "POST", "/server/users/new", nil, url.Values{
		"username": {"ops"}, "email": {"ops@acme.io"}, "password": {"a-long-enough-password-1"}, "role": {"domain"},
		"domain_ids": {idStr(out1.ID)},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create = %d:\n%s", rec.Code, rec.Body.String())
	}
	if len(sentBy(h, ml)) != 1 || !strings.HasPrefix(sentBy(h, ml)[0], "ops@acme.io | [SelfPost] Confirm your e-mail | ") {
		t.Fatalf("sent = %q", sentBy(h, ml))
	}
	u, _ := st.GetUserByUsername("ops")
	path, pv := "/server/users/"+idStr(u.ID), map[string]string{"uid": idStr(u.ID)}
	if out := send(h.HandleUserEdit, &root, "GET", path, pv, nil).Body.String(); !strings.Contains(out, `<span class="tag is-warning is-light">not confirmed</span>`) {
		t.Errorf("an unconfirmed address is not tagged:\n%s", out)
	}
	if ok, _ := st.ConfirmEmail(u.ID, "ops@acme.io", time.Now()); !ok {
		t.Fatal("confirm")
	}
	if out := send(h.HandleUserEdit, &root, "GET", path, pv, nil).Body.String(); !strings.Contains(out, `<span class="tag is-success is-light">confirmed</span>`) {
		t.Errorf("a confirmed address is not tagged:\n%s", out)
	}
	// The same address saved again keeps its mark and sends nothing; a new
	// one loses it and is sent its link.
	form := url.Values{"username": {"ops"}, "role": {"domain"}, "domain_ids": {idStr(out1.ID)}, "email": {"ops@acme.io"}}
	if rec := send(h.HandleUserEdit, &root, "POST", path, pv, form); rec.Code != http.StatusSeeOther || len(sentBy(h, ml)) != 1 {
		t.Fatalf("the same address: %d, sent %d", rec.Code, len(sentBy(h, ml)))
	}
	form.Set("email", "new@acme.io")
	if rec := send(h.HandleUserEdit, &root, "POST", path, pv, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("a new address: %d", rec.Code)
	}
	if got, _ := st.GetUser(u.ID); !got.EmailConfirmed.IsZero() || len(sentBy(h, ml)) != 2 || !strings.HasPrefix(sentBy(h, ml)[1], "new@acme.io | ") {
		t.Errorf("a new address: confirmed %v, sent %q", got.EmailConfirmed, sentBy(h, ml))
	}
}
