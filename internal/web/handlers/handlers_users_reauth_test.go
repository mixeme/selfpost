package handlers

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
	"golang.org/x/crypto/bcrypt"
)

// testAccountPassword is the signed-in user's own password in the user-form
// tests: what reauth compares account_password with.
const testAccountPassword = "the-signed-in-users-password"

// withAccountPassword gives p's stored user testAccountPassword and h the
// auth module whose limiter reauth uses (the user form asks for the
// signed-in user's password, audit 2026-10-17 A2).
func withAccountPassword(t *testing.T, h *Handlers, st *store.Store, p auth.Principal) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(testAccountPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.GetUser(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateUser(u.ID, u.Username, string(hash), u.Email); err != nil {
		t.Fatal(err)
	}
	if h.auth == nil {
		h.auth = auth.New(st, auth.Config{}, h.view, filepath.Join(t.TempDir(), "setup-token"))
	}
}

// A session alone is not enough to hand out an account (audit 2026-10-17,
// A2): adding a user, and a save that sets a password, an e-mail or a role -
// the signed-in user's own included - ask for the signed-in user's password;
// so does deleting a user. Missing or wrong, it is 401 on the same page and
// nothing changes; a save of the name and the reach alone is not asked.
func TestUserFormAsksForTheAccountPassword(t *testing.T) {
	h, st := inboundHandlers(t)
	out, _ := st.AddDomain("out.example.com", "mail")
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	withAccountPassword(t, h, st, root)
	const newPassword = "a-long-enough-password-1"
	refused := func(what string, rec interface {
		Result() *http.Response
	}, body string) {
		t.Helper()
		if code := rec.Result().StatusCode; code != http.StatusUnauthorized || !strings.Contains(body, "Your account password is incorrect.") {
			t.Errorf("%s = %d, want 401 with the refusal:\n%.800s", what, code, body)
		}
	}

	// Create: refused without and with a wrong password, the form kept.
	form := url.Values{"username": {"ops"}, "password": {newPassword}, "role": {"domain"}, "domain_ids": {idStr(out.ID)}}
	for _, pw := range []string{"", "wrong-password-entirely"} {
		form.Set("account_password", pw)
		rec := send(h.HandleUserNew, &root, "POST", "/server/users/new", nil, form)
		refused("create with account password "+pw, rec, rec.Body.String())
		if !strings.Contains(rec.Body.String(), `value="ops"`) || !strings.Contains(rec.Body.String(), `name="account_password" type="password" autocomplete="current-password" required`) {
			t.Errorf("the refused create form lost the name or the field:\n%.800s", rec.Body.String())
		}
		if u, _ := st.GetUserByUsername("ops"); u.ID != 0 {
			t.Fatalf("a user was created with account password %q", pw)
		}
	}
	form.Set("account_password", testAccountPassword)
	if rec := send(h.HandleUserNew, &root, "POST", "/server/users/new", nil, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("create with the right password = %d:\n%s", rec.Code, rec.Body.String())
	}
	ops, _ := st.GetUserByUsername("ops")

	// Update: each of password, e-mail, role is asked; the own password too.
	edit := func(u store.User, f url.Values) (int, string) {
		rec := send(h.HandleUserEdit, &root, "POST", "/server/users/"+idStr(u.ID), map[string]string{"uid": idStr(u.ID)}, f)
		return rec.Code, rec.Body.String()
	}
	base := func() url.Values {
		return url.Values{"username": {"ops"}, "role": {"domain"}, "domain_ids": {idStr(out.ID)}, "email": {""}}
	}
	for name, change := range map[string]func(url.Values){
		"password": func(f url.Values) { f.Set("password", "another-long-password-2") },
		"e-mail":   func(f url.Values) { f.Set("email", "thief@example.net") },
		"role":     func(f url.Values) { f.Set("role", "global") },
	} {
		f := base()
		change(f)
		code, body := edit(ops, f)
		if code != http.StatusUnauthorized || !strings.Contains(body, "Your account password is incorrect.") {
			t.Errorf("a %s change without the account password = %d:\n%.600s", name, code, body)
		}
		if got, _ := st.GetUser(ops.ID); got.PasswordHash != ops.PasswordHash || got.Email != ops.Email || got.Role != ops.Role {
			t.Errorf("a refused %s change changed the user: %+v", name, got)
		}
	}
	ownForm := url.Values{"username": {"root"}, "role": {"global"}, "password": {"the-thiefs-own-password"}}
	rec := send(h.HandleUserEdit, &root, "POST", "/server/users/"+idStr(root.ID), map[string]string{"uid": idStr(root.ID)}, ownForm)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("setting one's own password without the current one = %d", rec.Code)
	}
	if got, _ := st.GetUser(root.ID); bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte(testAccountPassword)) != nil {
		t.Error("a refused own-password change replaced the password")
	}

	// Name and reach alone: not asked.
	if code, body := edit(ops, base()); code != http.StatusSeeOther {
		t.Errorf("a save of the reach alone = %d:\n%.600s", code, body)
	}
	// With the password: the change goes through.
	f := base()
	f.Set("password", "another-long-password-2")
	f.Set("account_password", testAccountPassword)
	if code, body := edit(ops, f); code != http.StatusSeeOther {
		t.Errorf("a password change with the account password = %d:\n%.600s", code, body)
	}

	// Delete: refused on its own page, then done with the password.
	path, pv := "/server/users/"+idStr(ops.ID)+"/delete", map[string]string{"uid": idStr(ops.ID)}
	rec = send(h.HandleUserDelete, &root, "POST", path, pv, url.Values{"account_password": {"wrong-password-entirely"}})
	refused("delete with a wrong password", rec, rec.Body.String())
	if !strings.Contains(rec.Body.String(), "Delete ops") || !strings.Contains(rec.Body.String(), `name="account_password"`) {
		t.Errorf("the refused delete is not the confirmation page:\n%.800s", rec.Body.String())
	}
	if u, _ := st.GetUser(ops.ID); u.ID == 0 {
		t.Fatal("a refused delete removed the user")
	}
	if rec := send(h.HandleUserDelete, &root, "POST", path, pv, url.Values{"account_password": {testAccountPassword}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("delete with the right password = %d:\n%s", rec.Code, rec.Body.String())
	}
}
