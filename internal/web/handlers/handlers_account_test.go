package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// Account is every user's own page: each part is its own form and its own
// POST, and none of them touches what another saves.
func TestAccountPageShowsThreeForms(t *testing.T) {
	h, _ := settingsServer(t)
	out := getBody(t, h.HandleAccount, "/account")
	for _, want := range []string{
		`action="/account/profile"`, `name="email"`,
		`action="/account/password"`, `name="current_password"`,
		`action="/account/dmarc"`, `name="dmarc_default"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("account page missing %q", want)
		}
	}
	for _, gone := range []string{`name="send_log_retention_days"`, `value="hosted"`} {
		if strings.Contains(out, gone) {
			t.Errorf("account page shows %q (instance setting, or hosted reports with ingest off)", gone)
		}
	}
}

// The profile form saves the name and the e-mail without asking for the
// password and without changing the password or the DMARC default.
func TestAccountProfile(t *testing.T) {
	h, _ := settingsServer(t)
	before, _ := h.store.GetUser(globalPrincipal.ID)

	rec := postFormAs(h.HandleAccountProfile, globalPrincipal, "/account/profile", nil,
		url.Values{"username": {"operator"}, "email": {"mix@example.org"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account?done=profile" {
		t.Fatalf("POST /account/profile = %d to %q:\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	u, _ := h.store.GetUser(globalPrincipal.ID)
	if u.Username != "operator" || u.Email != "mix@example.org" {
		t.Errorf("profile = %q / %q", u.Username, u.Email)
	}
	if u.PasswordHash != before.PasswordHash || u.DMARCDefaultMode != store.DMARCDefaultNone {
		t.Errorf("saving the profile changed the password or the DMARC default (%q)", u.DMARCDefaultMode)
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
	rec = postFormAs(h.HandleAccountProfile, globalPrincipal, "/account/profile", nil, url.Values{"username": {"taken"}, "email": {"other@example.org"}})
	if u, _ = h.store.GetUser(globalPrincipal.ID); rec.Code != http.StatusConflict || u.Username != "operator" || u.Email != "mix@example.org" {
		t.Errorf("a taken username = %d, profile now %q / %q", rec.Code, u.Username, u.Email)
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

// The default report address is the user's choice between four modes; the
// address field belongs to "another address" alone.
func TestAccountDMARCDefault(t *testing.T) {
	h, _ := settingsServer(t)
	save := func(form url.Values) (int, store.User) {
		rec := postFormAs(h.HandleAccountDMARC, globalPrincipal, "/account/dmarc", nil, form)
		u, _ := h.store.GetUser(globalPrincipal.ID)
		return rec.Code, u
	}

	// "My account e-mail" needs an e-mail.
	if code, u := save(url.Values{"dmarc_default": {"account"}}); code != http.StatusBadRequest || u.DMARCDefaultMode != store.DMARCDefaultNone {
		t.Errorf("account e-mail without an e-mail = %d, mode %q", code, u.DMARCDefaultMode)
	}
	// Hosted needs report ingest, which this server has off.
	if code, u := save(url.Values{"dmarc_default": {"hosted"}}); code != http.StatusBadRequest || u.DMARCDefaultMode != store.DMARCDefaultNone {
		t.Errorf("hosted with ingest off = %d, mode %q", code, u.DMARCDefaultMode)
	}
	for _, bad := range []url.Values{
		{"dmarc_default": {"custom"}},
		{"dmarc_default": {"custom"}, "dmarc_default_address": {"nope"}},
		{"dmarc_default": {"profile"}},
		{},
	} {
		if code, _ := save(bad); code != http.StatusBadRequest {
			t.Errorf("%v = %d, want 400", bad, code)
		}
	}

	code, u := save(url.Values{"dmarc_default": {"custom"}, "dmarc_default_address": {"dmarc@hub.example"}})
	if code != http.StatusSeeOther || u.DMARCDefaultMode != store.DMARCDefaultCustom || u.DMARCDefaultAddress != "dmarc@hub.example" {
		t.Errorf("another address = %d, %q %q", code, u.DMARCDefaultMode, u.DMARCDefaultAddress)
	}
	// A domain that follows this user now resolves to it.
	d, _ := h.store.AddDomain("example.com", "mail")
	h.followCreator(d.ID, globalPrincipal.ID)
	d, _ = h.store.GetDomain(d.ID)
	if rua, err := h.domainReportAddress(d); err != nil || rua != "dmarc@hub.example" {
		t.Errorf("a following domain reports to %q, %v", rua, err)
	}

	if rec := postFormAs(h.HandleAccountProfile, globalPrincipal, "/account/profile", nil, url.Values{"username": {"admin"}, "email": {"mix@example.org"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("save e-mail = %d", rec.Code)
	}
	code, u = save(url.Values{"dmarc_default": {"account"}, "dmarc_default_address": {"left-over@hub.example"}})
	if code != http.StatusSeeOther || u.DMARCDefaultMode != store.DMARCDefaultAccount || u.DMARCDefaultAddress != "" {
		t.Errorf("my account e-mail = %d, %q %q", code, u.DMARCDefaultMode, u.DMARCDefaultAddress)
	}
	if rua, _ := h.domainReportAddress(d); rua != "mix@example.org" {
		t.Errorf("the following domain reports to %q, want the account e-mail", rua)
	}
	if code, u = save(url.Values{"dmarc_default": {"none"}}); code != http.StatusSeeOther || u.DMARCDefaultMode != store.DMARCDefaultNone {
		t.Errorf("none = %d, %q", code, u.DMARCDefaultMode)
	}
}

// A domain administrator has the same Account page; it is not a Server page.
func TestAccountIsOpenToADomainAdministrator(t *testing.T) {
	h, _ := settingsServer(t)
	d, _ := h.store.AddDomain("example.com", "mail")
	p := domainAdmin(t, h.store, "ops", d.ID)
	rec := postFormAs(h.HandleAccountProfile, p, "/account/profile", nil, url.Values{"username": {"ops"}, "email": {"ops@example.org"}})
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
		`id="dmarc"`, `href="/static/panel.css"`,
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
		"dmarc":    "Default report address saved.",
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

// The default address needs an authorization record when it is on another
// domain; with "no reports" nothing is asked of DNS and no record is drawn. The
// count of following domains is the user's own.
func TestAccountReportAuthorizationFollowsTheStoredDefault(t *testing.T) {
	h, _ := settingsServer(t)
	d, _ := h.store.AddDomain("example.com", "mail")
	h.followCreator(d.ID, globalPrincipal.ID)
	if _, err := h.store.AddDomain("other.example", "mail"); err != nil {
		t.Fatal(err)
	}
	out := getBody(t, h.HandleAccount, "/account")
	if strings.Contains(out, "Report authorization") {
		t.Error("an authorization record for a default of no reports")
	}
	if !strings.Contains(out, "1 of 2 now") {
		t.Errorf("the domains following the default are not counted:\n%s", out)
	}
}
