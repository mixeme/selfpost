package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
	"golang.org/x/crypto/bcrypt"
)

// Account is every user's own page: each part is its own form and its own
// POST, and none of them touches what another saves.
func TestAccountPageShowsTwoForms(t *testing.T) {
	h, _ := settingsServer(t)
	out := getBody(t, h.HandleAccount, "/account")
	for _, want := range []string{
		`action="/account/profile"`, `name="email"`,
		`action="/account/password"`, `name="current_password"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("account page missing %q", want)
		}
	}
	// A domain's report address is not an Account setting any more.
	for _, gone := range []string{`name="send_log_retention_days"`, `/account/dmarc`, `name="dmarc_default"`, `<h2>DMARC reports</h2>`, `Report authorization`} {
		if strings.Contains(out, gone) {
			t.Errorf("account page shows %q", gone)
		}
	}
}

// The profile form saves the name and the e-mail - a new e-mail with the
// current password - without changing the password or any domain's report
// address.
func TestAccountProfile(t *testing.T) {
	h, password := settingsServer(t)
	before, _ := h.store.GetUser(globalPrincipal.ID)
	d, _ := h.store.AddDomain("example.com", "mail")
	if err := h.store.SetDomainDMARCAddress(d.ID, "reports@hub.example"); err != nil {
		t.Fatal(err)
	}

	rec := postFormAs(h.HandleAccountProfile, globalPrincipal, "/account/profile", nil,
		url.Values{"username": {"operator"}, "email": {"mix@example.org"}, "current_password": {password}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account?done=profile" {
		t.Fatalf("POST /account/profile = %d to %q:\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	u, _ := h.store.GetUser(globalPrincipal.ID)
	if u.Username != "operator" || u.Email != "mix@example.org" {
		t.Errorf("profile = %q / %q", u.Username, u.Email)
	}
	if u.PasswordHash != before.PasswordHash {
		t.Error("saving the profile changed the password")
	}
	if got, _ := h.store.GetDomain(d.ID); got.DMARCRua != "reports@hub.example" {
		t.Errorf("saving the profile moved a domain's report address to %q", got.DMARCRua)
	}

	for _, bad := range []url.Values{
		{"username": {"operator"}, "email": {"not-an-address"}},
		{"username": {"x y"}, "email": {""}},
	} {
		if rec := postFormAs(h.HandleAccountProfile, globalPrincipal, "/account/profile", nil, bad); rec.Code != http.StatusBadRequest {
			t.Errorf("POST /account/profile %v = %d, want 400", bad, rec.Code)
		}
	}
	// A name that is taken is refused and nothing is half-saved.
	if _, err := h.store.CreateUser("taken", "h", store.RoleGlobal, store.Reach{}); err != nil {
		t.Fatal(err)
	}
	rec = postFormAs(h.HandleAccountProfile, globalPrincipal, "/account/profile", nil, url.Values{"username": {"taken"}, "email": {"other@example.org"}, "current_password": {password}})
	if u, _ = h.store.GetUser(globalPrincipal.ID); rec.Code != http.StatusConflict || u.Username != "operator" || u.Email != "mix@example.org" {
		t.Errorf("a taken username = %d, profile now %q / %q", rec.Code, u.Username, u.Email)
	}
}

// linkMailer keeps the links the panel would have mailed.
type linkMailer struct{ sent []string }

func (r *linkMailer) Send(to, subject, body string) error {
	r.sent = append(r.sent, to+" | "+subject+" | "+body)
	return nil
}

// sentBy is what ml has been given once h's links sent after their answers
// have gone.
func sentBy(h *Handlers, ml *linkMailer) []string {
	h.auth.Wait(context.Background())
	return ml.sent
}

// withMail gives h an auth module that mails, as mail.example.org, to a
// linkMailer.
func withMail(t *testing.T, h *Handlers) *linkMailer {
	t.Helper()
	h.auth = auth.New(h.store, auth.Config{Hostname: "mail.example.org"}, h.view, filepath.Join(t.TempDir(), "setup-token"))
	ml := &linkMailer{}
	h.auth.SetMailer(ml)
	return ml
}

// The e-mail is where a reset link goes once confirmed, so changing it asks
// for the password; the new address is sent its link and shows as not
// confirmed, with Send the link again; a confirmed address shows as such and
// keeps its mark when only the name changes.
func TestAccountEmailIsConfirmedByItsLink(t *testing.T) {
	h, password := settingsServer(t)
	ml := withMail(t, h)
	change := url.Values{"username": {"admin"}, "email": {"mix@example.org"}}
	for name, pw := range map[string]string{"no password": "", "wrong password": "nope"} {
		form := url.Values{"username": change["username"], "email": change["email"], "current_password": {pw}}
		if rec := postFormAs(h.HandleAccountProfile, globalPrincipal, "/account/profile", nil, form); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if u, _ := h.store.GetUser(globalPrincipal.ID); u.Email != "" || len(sentBy(h, ml)) != 0 {
		t.Fatalf("a refused change saved %q, sent %d", u.Email, len(sentBy(h, ml)))
	}
	change.Set("current_password", password)
	rec := postFormAs(h.HandleAccountProfile, globalPrincipal, "/account/profile", nil, change)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account?done=profile-link" {
		t.Fatalf("change: %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if len(sentBy(h, ml)) != 1 || !strings.HasPrefix(sentBy(h, ml)[0], "mix@example.org | [SelfPost] Confirm your e-mail | ") ||
		!strings.Contains(sentBy(h, ml)[0], "https://mail.example.org/confirm/") {
		t.Fatalf("sent = %q", sentBy(h, ml))
	}
	out := getBody(t, h.HandleAccount, "/account")
	for _, want := range []string{`<span class="tag is-warning is-light">not confirmed</span>`, `action="/account/email/confirm"`, `A link was sent to`} {
		if !strings.Contains(out, want) {
			t.Errorf("account page missing %q", want)
		}
	}
	// Send the link again: a second link.
	if rec := postFormAs(h.HandleAccountConfirmLink, globalPrincipal, "/account/email/confirm", nil, nil); rec.Code != http.StatusSeeOther || len(sentBy(h, ml)) != 2 {
		t.Fatalf("send again: %d, sent %d", rec.Code, len(sentBy(h, ml)))
	}
	// Confirmed: the tag says so, the form is gone, and a new name keeps it
	// without the password.
	if ok, _ := h.store.ConfirmEmail(globalPrincipal.ID, "mix@example.org", time.Now()); !ok {
		t.Fatal("confirm")
	}
	out = getBody(t, h.HandleAccount, "/account")
	if !strings.Contains(out, `<span class="tag is-success is-light">confirmed</span>`) || strings.Contains(out, `action="/account/email/confirm"`) {
		t.Errorf("a confirmed address:\n%s", out)
	}
	if rec := postFormAs(h.HandleAccountProfile, globalPrincipal, "/account/profile", nil,
		url.Values{"username": {"operator"}, "email": {"mix@example.org"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("a new name: %d", rec.Code)
	}
	if u, _ := h.store.GetUser(globalPrincipal.ID); u.EmailConfirmed.IsZero() || len(sentBy(h, ml)) != 2 {
		t.Errorf("a new name: confirmed %v, sent %d", u.EmailConfirmed, len(sentBy(h, ml)))
	}
}

func TestAccountPassword(t *testing.T) {
	h, password := settingsServer(t)
	const next = "another-long-password-2"
	hashOf := func() string {
		u, _ := h.store.GetUser(globalPrincipal.ID)
		return u.PasswordHash
	}
	before := hashOf()
	for name, form := range map[string]url.Values{
		"wrong current password": {"current_password": {"nope"}, "new_password": {next}, "new_password_confirm": {next}},
		"mismatch":               {"current_password": {password}, "new_password": {next}, "new_password_confirm": {next + "x"}},
		"too weak":               {"current_password": {password}, "new_password": {"short"}, "new_password_confirm": {"short"}},
		"empty":                  {"current_password": {password}},
	} {
		rec := postFormAs(h.HandleAccountPassword, globalPrincipal, "/account/password", nil, form)
		if rec.Code == http.StatusSeeOther || hashOf() != before {
			t.Errorf("%s: %d, password changed: %v", name, rec.Code, hashOf() != before)
		}
	}
	rec := postFormAs(h.HandleAccountPassword, globalPrincipal, "/account/password", nil,
		url.Values{"current_password": {password}, "new_password": {next}, "new_password_confirm": {next}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /account/password = %d:\n%s", rec.Code, rec.Body.String())
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hashOf()), []byte(next)); err != nil {
		t.Errorf("the new password was not stored: %v", err)
	}
}

// A domain administrator has the same Account page; it is not a Server page.
func TestAccountIsOpenToADomainAdministrator(t *testing.T) {
	h, _ := settingsServer(t)
	d, _ := h.store.AddDomain("example.com", "mail")
	p := domainAdmin(t, h.store, "ops", d.ID)
	// A new e-mail asks for the password: the administrator's is set here.
	hash, _ := bcrypt.GenerateFromPassword([]byte("ops-password-1234"), bcrypt.MinCost)
	if err := h.store.SetUserPassword(p.ID, string(hash)); err != nil {
		t.Fatal(err)
	}
	rec := postFormAs(h.HandleAccountProfile, p, "/account/profile", nil, url.Values{"username": {"ops"}, "email": {"ops@example.org"}, "current_password": {"ops-password-1234"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /account/profile as a domain administrator = %d:\n%s", rec.Code, rec.Body.String())
	}
	if u, _ := h.store.GetUser(p.ID); u.Email != "ops@example.org" {
		t.Errorf("e-mail = %q", u.Email)
	}
	// It changed their own row and nobody else's.
	if admin, _ := h.store.GetUser(globalPrincipal.ID); admin.Email != "" {
		t.Errorf("another user's e-mail was changed to %q", admin.Email)
	}
}

// Account is drawn by the shell of the kit, for the signed-in person, with
// their role in the kicker.
func TestAccountPageIsDrawnInTheShell(t *testing.T) {
	h, _ := settingsServer(t)
	out := getBody(t, h.HandleAccount, "/account")
	for _, want := range []string{
		`class="navbar is-primary"`, `Signed in as admin · global`, `<h1 class="title is-3">Account</h1>`,
		`href="/static/panel.css"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("account page missing %q", want)
		}
	}
}

// A refused form is shown again with the error above the boxes and what the
// person typed in the fields — escaped.
func TestAccountRefusalKeepsWhatWasTyped(t *testing.T) {
	h, _ := settingsServer(t)
	rec := postFormAs(h.HandleAccountProfile, globalPrincipal, "/account/profile", nil,
		url.Values{"username": {`o"><b>x</b>`}, "email": {"not-an-address"}})
	out := rec.Body.String()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /account/profile = %d, want 400:\n%s", rec.Code, out)
	}
	if !strings.Contains(out, `notification is-danger is-light`) {
		t.Error("the refusal is not shown as the error flash")
	}
	if !strings.Contains(out, `value="not-an-address"`) {
		t.Error("the e-mail that was typed is not shown again")
	}
	if strings.Contains(out, `<b>x</b>`) {
		t.Errorf("the username was written back unescaped:\n%s", out)
	}

	rec = postFormAs(h.HandleAccountPassword, globalPrincipal, "/account/password", nil,
		url.Values{"current_password": {"nope"}, "new_password": {"another-long-password-2"}, "new_password_confirm": {"another-long-password-2"}})
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Current password is incorrect.") {
		t.Errorf("wrong current password = %d:\n%s", rec.Code, rec.Body.String())
	}
	// A password is never written back.
	if strings.Contains(rec.Body.String(), "another-long-password-2") {
		t.Error("a submitted password is in the page that refused it")
	}
}

func TestAccountResultsAreTheFlash(t *testing.T) {
	h, _ := settingsServer(t)
	for done, want := range map[string]string{
		"profile":  "Profile saved.",
		"password": "Password changed.",
	} {
		out := getBody(t, h.HandleAccount, "/account?done="+done)
		if !strings.Contains(out, `notification is-success is-light`) || !strings.Contains(out, want) {
			t.Errorf("?done=%s: the result is not shown (%q)", done, want)
		}
	}
}

// A domain administrator has the same Account page in the same shell; it has
// no Server group in the menu and says its role.
func TestAccountPageForADomainAdministrator(t *testing.T) {
	h, _ := settingsServer(t)
	d, _ := h.store.AddDomain("example.com", "mail")
	p := domainAdmin(t, h.store, "ops", d.ID)
	out := getBodyAs(t, h.HandleAccount, "/account", p)
	if !strings.Contains(out, `· domain`) || !strings.Contains(out, `action="/account/profile"`) {
		t.Errorf("account page for a domain administrator:\n%s", out)
	}
	for _, gone := range []string{`href="/server/health"`, `href="/overview"`, `href="/server/users"`} {
		if strings.Contains(out, gone) {
			t.Errorf("a domain administrator's menu carries %s", gone)
		}
	}
}

// The Account form's current-password check has its own bucket, per user:
// it stops a guesser at ten, and does not lock the address out of signing in
// (audit 2026-10-12, L15).
func TestAccountPasswordGuessesHaveTheirOwnLimit(t *testing.T) {
	h, _ := settingsServer(t)
	wrong := url.Values{"current_password": {"nope"}, "new_password": {"another-long-password-2"}, "new_password_confirm": {"another-long-password-2"}}
	for i := 0; i < 10; i++ {
		if rec := postFormAs(h.HandleAccountPassword, globalPrincipal, "/account/password", nil, wrong); rec.Code != http.StatusUnauthorized {
			t.Fatalf("guess %d = %d, want 401", i+1, rec.Code)
		}
	}
	if rec := postFormAs(h.HandleAccountPassword, globalPrincipal, "/account/password", nil, wrong); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the eleventh guess = %d, want 429", rec.Code)
	}

	// The same address may still sign in: postFormAs and this request come
	// from httptest's one RemoteAddr.
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"username": {"x"}, "password": {"y"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.auth.HandleLogin(rec, req)
	if rec.Code == http.StatusTooManyRequests {
		t.Error("the password guesses locked the address out of signing in")
	}
}
