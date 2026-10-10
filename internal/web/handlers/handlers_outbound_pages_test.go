package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/app"
	"github.com/mixeme/selfpost/internal/store"
)

// The pages of the Outbound group, rendered from the real stores and the
// handlers, with the refusals each form can come back with. Their markup is
// held to the mockups by the guards in internal/web/view; what is checked here
// is that the handlers fill the typed pages from what is stored and that every
// state a form can end in lands on the page that owns it.

func has(t *testing.T, where, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("%s is missing %q:\n%s", where, w, body)
		}
	}
}

func lacks(t *testing.T, where, body string, gone ...string) {
	t.Helper()
	for _, g := range gone {
		if strings.Contains(body, g) {
			t.Errorf("%s carries %q", where, g)
		}
	}
}

// pageOf reads a page of domain 1 as the global administrator, binding the
// path's {id} the way the router would.
func pageOf(t *testing.T, h http.HandlerFunc, target string) string {
	t.Helper()
	rec := send(h, &globalPrincipal, "GET", target, map[string]string{"id": "1"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200:\n%s", target, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestDomainsListShowsTheAddFormToTheGlobalRoleOnly(t *testing.T) {
	s := newAppStand(t)
	body := getBody(t, s.h.HandleDashboard, "/outbound/domains")
	has(t, "list", body, `<h1 class="title is-3">Outbound domains</h1>`, `<h2>Add a domain</h2>`, `action="/outbound/domains"`,
		`<strong>example.org</strong>`, `href="/outbound/domains/1/delete"`, `1 domain · DNS cached`, `>DKIM</span>`, `>SPF</span>`, `>DMARC</span>`, `mail</td>`)

	ops := domainAdmin(t, s.h.store, "ops", s.d.ID)
	body = getBodyAs(t, s.h.HandleDashboard, "/outbound/domains", ops)
	has(t, "list for a domain administrator", body, `<strong>example.org</strong>`, `href="/outbound/domains/1"`)
	lacks(t, "list for a domain administrator", body, `Add a domain`, `/delete`)

	// What the viewer does not reach is not listed.
	other, err := s.h.store.AddDomain("other.example.net", "mail")
	if err != nil {
		t.Fatal(err)
	}
	lacks(t, "list for a domain administrator", getBodyAs(t, s.h.HandleDashboard, "/outbound/domains", ops), other.Name)
	has(t, "list", getBody(t, s.h.HandleDashboard, "/outbound/domains"), other.Name)
}

// A refused name comes back on the list, with what was typed in the field.
func TestAddDomainRefusalKeepsWhatWasTyped(t *testing.T) {
	s := newAppStand(t)
	rec := postFormAs(s.h.HandleAddDomain, globalPrincipal, "/outbound/domains", nil, url.Values{"name": {`Not A Domain "<b>`}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a bad name = %d, want 400:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	has(t, "refused add", body, `<div class="notification is-danger is-light">`, `<h2>Add a domain</h2>`, `<strong>example.org</strong>`)
	lacks(t, "refused add", body, `<b>`)
	if domains, _ := s.h.store.ListDomains(); len(domains) != 1 {
		t.Errorf("a refused name left %d domains", len(domains))
	}
}

func TestDomainPageShowsRecordsApplicationsAndConnection(t *testing.T) {
	s := newAppStand(t)
	s.h.cfg.Hostname, s.h.cfg.SubmissionEnabled = "mail.example.org", true
	a, _, err := s.h.apps.CreateWithSettings(s.d.ID, "prod-server", app.Settings{
		Mode: store.AddressModeList, Addresses: []string{"alerts@example.org"},
		AuthIPRestrict: true, AuthAllowedIPs: []string{"203.0.113.10", "203.0.113.11"},
		Limit: app.Limit{Mode: store.RateLimitModeManual, MaxMessages: 200, WindowSeconds: 3600},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := pageOf(t, s.h.HandleDomainDetail, "/outbound/domains/1")
	has(t, "domain", body,
		`<h1 class="title is-3">example.org</h1>`, `class="sp-postmark`, `<h3>DKIM</h3>`, `<h3>SPF</h3>`, `<h3>DMARC</h3>`,
		`mail._domainkey.example.org`, `v=DKIM1`, `_dmarc.example.org`, `v=DMARC1; p=none`, `v=spf1 `,
		`<td class="sp-mono sp-nowrap"><strong>prod-server</strong></td>`, `alerts@<wbr>example.org`, `200 / h`, `2 IPs`,
		`href="/outbound/domains/1/applications/`+idStr(a.ID)+`">Edit</a>`,
		`mail.example.org`, `STARTTLS (submission)`, `action="/outbound/domains/1/dns-recheck"`,
		`href="/outbound/log?domain=example.org"`, `href="/outbound/domains/1/settings#reports"`, `href="/outbound/domains/1/delete"`)

	// The flash the redirects carry.
	rec := send(s.h.HandleDomainDetail, &globalPrincipal, "GET", "/outbound/domains/1?appsaved=1", s.paths, nil)
	has(t, "domain after a save", rec.Body.String(), `<div class="notification is-success is-light">`, `Application saved.`)

	// A domain administrator does not get the global role's delete entry.
	ops := domainAdmin(t, s.h.store, "ops", s.d.ID)
	rec = send(s.h.HandleDomainDetail, &ops, "GET", "/outbound/domains/1", s.paths, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("domain page for its administrator = %d", rec.Code)
	}
	lacks(t, "domain for a domain administrator", rec.Body.String(), `href="/outbound/domains/1/delete"`)
}

func TestDomainPageWithoutApplicationsSaysSo(t *testing.T) {
	s := newAppStand(t)
	body := pageOf(t, s.h.HandleDomainDetail, "/outbound/domains/1")
	has(t, "domain", body, `No applications yet.`)
	lacks(t, "domain", body, `<th>Login</th>`)
}

// The rest of the settings page, beside the report address (the three choices
// are in handlers_dmarc_test.go).
func TestDomainSettingsPageHasItsBoxes(t *testing.T) {
	s := newAppStand(t)
	body := pageOf(t, s.h.HandleDomainSettings, "/outbound/domains/1/settings")
	has(t, "settings", body, `<h1 class="title is-3">Domain settings</h1>`,
		`action="/outbound/domains/1/settings/reports"`, `<option value="none" selected>No reports</option>`, `<option value="custom">A specific address</option>`,
		`action="/outbound/domains/1/settings/ratelimit"`, `action="/outbound/domains/1/settings/export"`,
		`name="encrypt" value="1" checked`, `minlength="12"`, `<h2>Delete domain</h2>`, `Removes the DKIM key.`)
	lacks(t, "settings", body, `value="hosted"`, `name="clear"`, `ratelimit/recalc`)
}

func TestDomainSettingsShowsTheRateLimit(t *testing.T) {
	s := newAppStand(t)
	if err := s.h.domains.SaveRateLimit(s.d.ID, store.RateLimit{Mode: store.RateLimitModeManual, MaxMessages: 250, WindowSeconds: 3600}); err != nil {
		t.Fatal(err)
	}
	body := pageOf(t, s.h.HandleDomainSettings, "/outbound/domains/1/settings")
	has(t, "settings", body, `<option value="manual" selected>`, `value="250"`, `<span class="tag is-success is-light">active</span>`,
		`name="clear" value="1"`, `max="600"`, `600 messages / 3600 s per client IP`, `Multiplier (1.5–5, default 2.5)`)
	lacks(t, "settings", body, `ratelimit/recalc`)
}

// Each form of the settings page answers its refusal on the settings page, with
// the status it always had and the message in the flash.
func TestDomainSettingsRefusalsStayOnTheSettingsPage(t *testing.T) {
	s := newAppStand(t)
	post := func(h http.HandlerFunc, path string, form url.Values) (int, string) {
		rec := postFormAs(h, globalPrincipal, "/outbound/domains/1/settings/"+path, s.paths, form)
		return rec.Code, rec.Body.String()
	}

	code, body := post(s.h.HandleDomainRateLimit, "ratelimit", url.Values{"mode": {"manual"}, "max_messages": {"601"}})
	if code != http.StatusBadRequest {
		t.Fatalf("a limit above level 1 = %d", code)
	}
	has(t, "rate limit refusal", body, `<div class="notification is-danger is-light">`, `level-1 backstop (600)`, `<h1 class="title is-3">Domain settings</h1>`)

	code, body = post(s.h.HandleDomainRateLimitRecalc, "ratelimit/recalc", url.Values{})
	if code != http.StatusBadRequest {
		t.Fatalf("recalculating a limit that is not automatic = %d", code)
	}
	has(t, "recalculation refusal", body, `rate limit not in auto mode`, `<h1 class="title is-3">Domain settings</h1>`)

	code, body = post(s.h.HandleDomainDMARC, "reports", url.Values{"dmarc_rua_mode": {"custom"}, "dmarc_rua_email": {"not an address<b>"}})
	if code != http.StatusBadRequest {
		t.Fatalf("a bad report address = %d", code)
	}
	has(t, "report address refusal", body, `<div class="notification is-danger is-light">`, `<option value="custom" selected>`, `not an address&lt;b&gt;`)
	lacks(t, "report address refusal", body, `<b>`)

	code, body = post(s.h.HandleDomainDMARC, "reports", url.Values{"dmarc_rua_mode": {"custom"}})
	has(t, "report address refusal", body, `Enter the report address or choose another option.`)

	code, body = post(s.h.HandleDomainDMARC, "reports", url.Values{"dmarc_rua_mode": {"hosted"}})
	if code != http.StatusBadRequest {
		t.Fatalf("hosted reports with ingest off = %d", code)
	}
	has(t, "report address refusal", body, `SelfPost-hosted reports are not enabled on this server.`)

	code, body = post(s.h.HandleDomainDMARC, "reports", url.Values{"dmarc_rua_mode": {"bogus"}})
	if code != http.StatusBadRequest {
		t.Fatalf("an unknown mode = %d", code)
	}
	has(t, "report address refusal", body, `Choose how aggregate reports are addressed for this domain.`)

	code, body = post(s.h.HandleExportDomain, "export", url.Values{"encrypt": {"1"}, "password": {"short"}, "password_confirm": {"short"}})
	if code != http.StatusBadRequest {
		t.Fatalf("a short export password = %d", code)
	}
	has(t, "export refusal", body, `The encryption password must be at least 12 characters.`, `<h1 class="title is-3">Domain settings</h1>`)
	code, body = post(s.h.HandleExportDomain, "export", url.Values{"encrypt": {"1"}, "password": {"long-enough-password"}, "password_confirm": {"another-one-entirely"}})
	has(t, "export refusal", body, `The two passwords do not match.`)
}

func TestApplicationNewFormIsBlank(t *testing.T) {
	s := newAppStand(t)
	body := pageOf(t, s.h.HandleApplicationNew, "/outbound/domains/1/applications/new")
	has(t, "new application", body, `<h1 class="title is-3">New application</h1>`,
		`action="/outbound/domains/1/applications/new"`, `Create application`, `name="login" value=""`,
		`<option value="wildcard" selected>`, `<option value="domain" selected>`, `value="3600"`, `placeholder="alerts@example.org"`)
	lacks(t, "new application", body, ` readonly`, `/password`, `/delete`, `Recalculate`)
}

func TestApplicationEditFormShowsWhatIsStored(t *testing.T) {
	s := newAppStand(t)
	a, _, err := s.h.apps.CreateWithSettings(s.d.ID, "prod-server", app.Settings{
		Mode: store.AddressModeList, Addresses: []string{"alerts@example.org", "noc@example.org"},
		AuthIPRestrict: true, AuthAllowedIPs: []string{"203.0.113.10"},
		Limit: app.Limit{Mode: store.RateLimitModeManual, MaxMessages: 200, WindowSeconds: 1800},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.h.domains.SaveRateLimit(s.d.ID, store.RateLimit{Mode: store.RateLimitModeManual, MaxMessages: 288, WindowSeconds: 3600}); err != nil {
		t.Fatal(err)
	}
	own := "/outbound/domains/1/applications/" + idStr(a.ID)
	rec := send(s.h.HandleApplicationEdit, &globalPrincipal, "GET", own, map[string]string{"id": "1", "aid": idStr(a.ID)}, nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("edit form = %d:\n%s", rec.Code, body)
	}
	has(t, "edit form", body, `<h1 class="title is-3 sp-mono">prod-server</h1>`, `action="`+own+`"`, `name="login" value="prod-server" readonly`,
		`<option value="list" selected>`, "alerts@example.org\nnoc@example.org</textarea>", "203.0.113.10</textarea>", ` checked> Restrict to listed IPs`,
		`<option value="manual" selected>`, `value="200"`, `value="1800"`, `Overrides the domain limit (288 / h)`,
		`action="`+own+`/password" data-confirm=`, `action="`+own+`/delete" data-confirm=`, `Save application`)
	lacks(t, "edit form", body, `Recalculate`)

	// An automatic limit offers its recalculation.
	if err := s.h.apps.SaveRateLimit(a.ID, store.RateLimit{Mode: store.RateLimitModeAuto, AutoMultiplier: 2, WindowSeconds: 3600, MaxMessages: 40}); err != nil {
		t.Fatal(err)
	}
	rec = send(s.h.HandleApplicationEdit, &globalPrincipal, "GET", own, map[string]string{"id": "1", "aid": idStr(a.ID)}, nil)
	has(t, "edit form", rec.Body.String(), `formaction="`+own+`/ratelimit/recalc"`, `<option value="auto" selected>`, `Computed limit: <strong>40</strong>`)
}

// A refusal of the application form comes back on the form with what was typed,
// the status it always had, and creates or changes nothing.
func TestApplicationFormRefusalsComeBackOnTheForm(t *testing.T) {
	s := newAppStand(t)
	typed := url.Values{
		"login": {"has space"}, "mode": {"list"}, "addresses": {"a@example.org"}, "auth_ip_restrict": {"1"},
		"auth_allowed_ips": {"203.0.113.10"}, "rl_mode": {"manual"}, "max_messages": {"601"}, "window_seconds": {"900"},
	}
	rec := postFormAs(s.h.HandleApplicationCreate, globalPrincipal, "/outbound/domains/1/applications/new", s.paths, typed)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a limit above level 1 = %d:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	has(t, "refused create", body, `<div class="notification is-danger is-light">`, `level-1 backstop (600)`,
		`name="login" value="has space"`, `<option value="list" selected>`, "a@example.org</textarea>", "203.0.113.10</textarea>",
		` checked> Restrict to listed IPs`, `<option value="manual" selected>`, `value="601"`, `value="900"`, `Create application`)
	lacks(t, "refused create", body, `Shown once`)
	if rec.Header().Get("Cache-Control") == "no-store" {
		t.Error("a refusal is marked as a page that shows a secret")
	}

	typed.Set("max_messages", "100")
	typed.Set("login", "taken")
	if _, _, err := s.h.apps.CreateWithSettings(s.d.ID, "taken", app.Settings{Mode: store.AddressModeWildcard}, nil); err != nil {
		t.Fatal(err)
	}
	rec = postFormAs(s.h.HandleApplicationCreate, globalPrincipal, "/outbound/domains/1/applications/new", s.paths, typed)
	if rec.Code != http.StatusConflict {
		t.Fatalf("a login that exists = %d, want 409:\n%s", rec.Code, rec.Body.String())
	}
	has(t, "refused create", rec.Body.String(), `That login is already in use. Choose another.`, `name="login" value="taken"`)
}

func TestApplicationSaveRefusalKeepsTheLoginFixed(t *testing.T) {
	s := newAppStand(t)
	a, _, err := s.h.apps.CreateWithSettings(s.d.ID, "prod-server", app.Settings{Mode: store.AddressModeWildcard}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{"id": "1", "aid": idStr(a.ID)}
	own := "/outbound/domains/1/applications/" + idStr(a.ID)
	rec := postFormAs(s.h.HandleApplicationSave, globalPrincipal, own, paths, url.Values{
		"login": {"renamed"}, "mode": {"wildcard"}, "rl_mode": {"manual"}, "max_messages": {"500"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a changed login = %d", rec.Code)
	}
	body := rec.Body.String()
	has(t, "refused save", body, `prod-server: the login of an application cannot be changed`, `name="login" value="prod-server" readonly`,
		`<option value="manual" selected>`, `value="500"`)
	lacks(t, "refused save", body, `value="renamed"`)

	rec = postFormAs(s.h.HandleApplicationSave, globalPrincipal, own, paths, url.Values{"mode": {"wildcard"}, "rl_mode": {"manual"}, "max_messages": {"601"}})
	has(t, "refused save", rec.Body.String(), `prod-server: message limit cannot exceed the level-1 backstop (600)`)

	// The recalculation of a limit that is not automatic is refused on the form too.
	rec = postFormAs(s.h.HandleAppRateLimitRecalc, globalPrincipal, own+"/ratelimit/recalc", paths, url.Values{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("recalculating a limit that is not automatic = %d", rec.Code)
	}
	has(t, "refused recalculation", rec.Body.String(), `prod-server: rate limit not in auto mode`, `Save application`)
}

// The password page is the response of its POST: shown once, in no cache, and
// the same for a new application and for a new password.
func TestPasswordIsShownOnceAndNeverCached(t *testing.T) {
	s := newAppStand(t)
	s.h.cfg.Hostname, s.h.cfg.SubmissionEnabled = "mail.example.org", true
	rec := postFormAs(s.h.HandleApplicationCreate, globalPrincipal, "/outbound/domains/1/applications/new", s.paths, url.Values{
		"login": {"prod-server"}, "mode": {"list"}, "addresses": {"alerts@example.org"},
	})
	if rec.Code != http.StatusCreated || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("create = %d, Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	password := s.sasl["prod-server"]
	has(t, "created", rec.Body.String(), `<h2>Shown once</h2>`, `value="`+password+`"`, `value="prod-server"`,
		`If this one is lost, generate another from the application&#39;s page.`, `alerts@example.org`, `465 SSL/TLS · 587 STARTTLS`,
		`href="/outbound/domains/1">Done — back to example.org</a>`)
	lacks(t, "created", rec.Body.String(), `stopped working`)

	a, _, _ := s.app(t, "prod-server")
	paths := map[string]string{"id": "1", "aid": idStr(a.ID)}
	rec = postFormAs(s.h.HandleRegenPassword, globalPrincipal, "/outbound/domains/1/applications/1/password", paths, url.Values{})
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("new password = %d, Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	renewed := s.sasl["prod-server"]
	if renewed == password {
		t.Fatal("the password did not change")
	}
	has(t, "new password", rec.Body.String(), `value="`+renewed+`"`, `The previous password stopped working when this one was generated.`)
	lacks(t, "new password", rec.Body.String(), `value="`+password+`"`)
}

func TestDeleteConfirmNamesTheApplicationsThatGoWithTheDomain(t *testing.T) {
	s := newAppStand(t)
	rec := send(s.h.HandleDeleteConfirm, &globalPrincipal, "GET", "/outbound/domains/1/delete", s.paths, nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("delete confirmation = %d:\n%s", rec.Code, body)
	}
	has(t, "delete", body, `<h1 class="title is-3">Delete example.org</h1>`, `permanently delete its DKIM signing key;`,
		`<form method="post" action="/outbound/domains/1/delete">`, `Delete example.org</button>`, `href="/outbound/domains/1/settings#export"`)
	lacks(t, "delete", body, `credentials`)

	for _, login := range []string{"prod-server", "alerts"} {
		if _, _, err := s.h.apps.CreateWithSettings(s.d.ID, login, app.Settings{Mode: store.AddressModeWildcard}, nil); err != nil {
			t.Fatal(err)
		}
	}
	rec = send(s.h.HandleDeleteConfirm, &globalPrincipal, "GET", "/outbound/domains/1/delete", s.paths, nil)
	has(t, "delete", rec.Body.String(), `<strong>both applications</strong>`, `<span class="sp-mono">alerts</span>, <span class="sp-mono">prod-server</span>`)
}
