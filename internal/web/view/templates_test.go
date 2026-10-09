package view

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"strings"
	"testing"
)

// The navigation is rendered from the layout, not copied into each page, so
// every page template must resolve it. This is what makes "the nav is on every
// authenticated page" a structural property instead of a checklist item.
func TestEveryPageResolvesNav(t *testing.T) {
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for name, page := range engine.Pages() {
		if page.Lookup("nav") == nil {
			t.Errorf("page %q does not resolve the shared nav template", name)
		}
	}
}

// The version comes from render(), not from each handler's data map, so the
// footer is only correct as long as every page composes with the layout and
// render keeps supplying the key. Both are asserted here rather than trusted.
// Appropriate Legal Notices (copyright, licence, source, no warranty) must
// appear on every page, including the signed-out ones.
func TestLayoutShowsTheVersionOnlyWhenSignedIn(t *testing.T) {
	engine, err := New("9.9.9-test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	legalBits := []string{
		"Copyright © 2026 Mikhail Yenuchenko",
		`href="/license"`,
		"License (AGPL-3.0)",
		`href="https://github.com/mixeme/selfpost"`,
		"Source",
		"No warranty",
	}
	rendered := 0
	for name := range engine.Pages() {
		if kitPages[name] {
			continue // the kit layout has its own tests (components_test.go)
		}
		var buf bytes.Buffer
		err := engine.Page(name).ExecuteTemplate(&buf, "layout_legacy.html", map[string]any{
			"Title": "t", "User": "admin", "Active": "", "Version": "9.9.9-test",
			"Copyright": "Copyright © 2026 Mikhail Yenuchenko",
			"SourceURL": "https://github.com/mixeme/selfpost",
		})
		if err != nil {
			// Pages whose content block needs more data than this cannot be
			// rendered here; the footer is in the shared layout, so one page
			// that does render proves it for all of them.
			continue
		}
		rendered++
		out := buf.String()
		if !strings.Contains(out, "SelfPost 9.9.9-test") {
			t.Errorf("page %q does not show the version in the layout footer", name)
		}
		for _, want := range legalBits {
			if !strings.Contains(out, want) {
				t.Errorf("page %q is missing legal notice %q", name, want)
			}
		}
	}
	if rendered == 0 {
		t.Fatal("no page rendered, so the footer was never actually checked")
	}

	// Signed out (login, setup) the version must not be advertised, but the
	// Appropriate Legal Notices must still be present.
	// Both signed-out pages go through Engine.Render, which supplies the
	// engine's version and the legal lines itself.
	signedOut := map[string]any{
		"login": NewLogin("mail.example.org", ""),
		"setup": NewSetup("token", ""),
	}
	for name, data := range signedOut {
		rec := httptest.NewRecorder()
		engine.Render(rec, http.StatusOK, name, data)
		if rec.Code != http.StatusOK {
			t.Fatalf("render %s: status %d: %s", name, rec.Code, rec.Body.String())
		}
		out := rec.Body.String()
		if strings.Contains(out, "9.9.9-test") {
			t.Errorf("the %s page shows the version to unauthenticated visitors:\n%s", name, out)
		}
		for _, want := range legalBits {
			if !strings.Contains(out, want) {
				t.Errorf("%s page is missing legal notice %q", name, want)
			}
		}
	}
}

func TestRenderSuppliesTheVersion(t *testing.T) {
	engine, err := New("9.9.9-test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	data := map[string]any{"Title": "t", "User": "admin"}
	engine.Render(rec, http.StatusOK, "backup", data)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := data["Version"]; got != "9.9.9-test" {
		t.Errorf("render did not supply Version (got %v)", got)
	}
	if got := data["Copyright"]; got != "Copyright © 2026 Mikhail Yenuchenko" {
		t.Errorf("render did not supply Copyright (got %v)", got)
	}
	if got := data["SourceURL"]; got != "https://github.com/mixeme/selfpost" {
		t.Errorf("render did not supply SourceURL (got %v)", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "SelfPost 9.9.9-test") {
		t.Errorf("rendered page does not show the version:\n%s", body)
	}
	if !strings.Contains(body, `href="/license"`) || !strings.Contains(body, "No warranty") {
		t.Errorf("rendered page is missing Appropriate Legal Notices:\n%s", body)
	}
}

func TestNavMarksActivePage(t *testing.T) {
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var buf bytes.Buffer
	err = engine.Page("help").ExecuteTemplate(&buf, "nav", map[string]any{
		"User":     "admin",
		"Active":   "mail_queue",
		"IsGlobal": true,
	})
	if err != nil {
		t.Fatalf("execute nav: %v", err)
	}
	out := buf.String()
	// The label is checked apart from the opening tag because each entry now
	// carries an icon between the two.
	if !strings.Contains(out, `<span aria-current="page">`) || !strings.Contains(out, `Mail queue</span>`) {
		t.Errorf("active page is not marked:\n%s", out)
	}
	if strings.Contains(out, `href="/outbound/queue"`) {
		t.Errorf("active page still links to itself:\n%s", out)
	}
	if !strings.Contains(out, `href="/outbound/log"`) {
		t.Errorf("inactive pages are not linked:\n%s", out)
	}
}

func TestNavLeadsWithStatusAndPointsDomainsAtItsOwnPath(t *testing.T) {
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var buf bytes.Buffer
	if err := engine.Page("help").ExecuteTemplate(&buf, "nav", map[string]any{
		"User":     "admin",
		"Active":   "status",
		"IsGlobal": true,
	}); err != nil {
		t.Fatalf("execute nav: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `<span aria-current="page">`) || !strings.Contains(out, `Status</span>`) {
		t.Errorf("the status page is not marked active:\n%s", out)
	}
	if !strings.Contains(out, `href="/outbound/domains"`) {
		t.Errorf("Domains does not link to /outbound/domains:\n%s", out)
	}
	if strings.Index(out, "Status") > strings.Index(out, "Domains") {
		t.Errorf("Status is not the first navigation entry:\n%s", out)
	}
	if strings.Contains(out, `href="/inbound/domains"`) || strings.Contains(out, "Inbound") {
		t.Errorf("Inbound nav is shown while InboundEnabled is unset:\n%s", out)
	}
}

func TestNavShowsInboundWhenEnabled(t *testing.T) {
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	engine.SetInboundEnabled(true)
	var buf bytes.Buffer
	if err := engine.Page("help").ExecuteTemplate(&buf, "nav", map[string]any{
		"User":           "admin",
		"Active":         "status",
		"IsGlobal":       true,
		"InboundEnabled": true,
	}); err != nil {
		t.Fatalf("execute nav: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `href="/inbound/domains"`) || !strings.Contains(out, "Inbound") {
		t.Errorf("Inbound nav is missing while InboundEnabled is true:\n%s", out)
	}
	dom := strings.Index(out, `href="/outbound/domains"`)
	inb := strings.Index(out, `href="/inbound/domains"`)
	if dom < 0 || inb < 0 || inb < dom {
		t.Errorf("Inbound should follow Domains:\n%s", out)
	}
}

// Whether a page takes the whole column or the reading measure is declared by
// the page's own "wide" block (see layout_legacy.html), which the layout stamps into
// <main>'s class list. A page that loses the block does not fail to render — it
// silently comes back at the measure, with its table squeezed into two thirds
// of the column — so the set is asserted here, in both directions.
func TestOnlyThePagesMadeOfDataDeclareThemselvesWide(t *testing.T) {
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	wide := map[string]bool{
		"mail_queue": true,
		"system_log": true,
		"inbound":    true, "inbound_domain": true,
	}
	for name, page := range engine.Pages() {
		if kitPages[name] {
			continue // "wide" belongs to the legacy layout; the kit's width comes from its columns
		}
		var buf bytes.Buffer
		if err := page.ExecuteTemplate(&buf, "wide", nil); err != nil {
			t.Fatalf("execute the wide block of %s: %v", name, err)
		}
		got := strings.TrimSpace(buf.String())
		switch {
		case wide[name] && got != "wide":
			t.Errorf("page %q no longer declares itself wide (%q); its data falls back to the reading measure", name, got)
		case !wide[name] && got != "":
			t.Errorf("page %q declares itself %q; only the pages that are tables of data, raw log lines or side-by-side cards take the whole column", name, got)
		}
	}
}

func TestSettingsPageDocumentsRateLimits(t *testing.T) {
	out := renderSignedIn(t, "settings", settingsFixture())
	for _, want := range []string{
		`id="rate-limits"`,
		"Sending rate limits",
		"Level 1 · per client IP", "600 / h", "<code>.env</code>",
		"Level 2 · domain, application", `href="/outbound/domains"`,
		`name="send_log_retention_days"`, `value="30"`, "Keep outbound log rows, days",
		`action="/server/settings"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("settings page missing %q", want)
		}
	}
	// What belongs to a user is on Account.
	for _, gone := range []string{`name="current_password"`, `name="username"`, `name="dmarc_default"`, `name="email"`} {
		if strings.Contains(out, gone) {
			t.Errorf("the instance settings page carries the user's %s", gone)
		}
	}
}

func TestDrillDownPagesPlaceBackLinkAboveContent(t *testing.T) {
	drillDown := map[string]bool{
		"user_form.html":      true,
		"user_delete.html":    true,
		"inbound_domain.html": true,
		"inbound_delete.html": true,
	}
	forEachTemplate(t, func(name, body string) {
		if !drillDown[name] {
			return
		}
		if !strings.Contains(body, `template "back_link"`) {
			t.Errorf("%s is a drill-down page but does not use the shared back_link template", name)
		}
		backIdx := strings.Index(body, `template "back_link"`)
		cardIdx := strings.Index(body, `class="card`)
		if cardIdx >= 0 && backIdx > cardIdx {
			t.Errorf("%s places the back link after the first card", name)
		}
	})
}

// Since the panel root redirects to the status page, a link left pointing at
// "/" silently lands on the wrong screen instead of failing — so no template may
// contain one.
func TestNoTemplateLinksToTheBareRoot(t *testing.T) {
	forEachTemplate(t, func(name, body string) {
		if strings.Contains(body, `href="/"`) {
			t.Errorf(`%s links to "/", which is now the status redirect; link to /outbound/domains (or the intended page) instead`, name)
		}
	})
}

// The reload action is a server-health control and lives only on the Health
// page (as a partial call: its path is in the page's data, so no template but
// the view code names it).
func TestReloadFormLivesOnlyOnTheHealthPage(t *testing.T) {
	forEachTemplate(t, func(name, body string) {
		if strings.Contains(body, "/server/health/reload") {
			t.Errorf("%s names /server/health/reload; the reload control belongs to NewHealth", name)
		}
	})
	if got := renderSignedIn(t, "health", healthFixture(false)); !strings.Contains(got, `action="/server/health/reload"`) {
		t.Error("Health has no Reload configuration form")
	}
	for _, page := range []string{"overview", "account", "settings"} {
		if strings.Contains(renderSignedIn(t, page, pageFixtures[page]()), "/server/health/reload") {
			t.Errorf("%s offers the reload control", page)
		}
	}
}

// The panel's Content-Security-Policy is a plain default-src 'self' with no
// inline exemption, which makes inline script and inline style a
// failure mode rather than a style question: an onclick= handler or a
// style="..." attribute added to a template does not error, it silently stops
// working in the browser. Behaviour belongs in static/panel.js (triggered from
// a data- attribute), appearance in static/panel.css.
func TestNoTemplateUsesInlineScriptOrStyle(t *testing.T) {
	inlineHandler := regexp.MustCompile(`\son[a-z]+\s*=`)
	inlineStyle := regexp.MustCompile(`\sstyle\s*=|<style[\s>]`)
	scriptTag := regexp.MustCompile(`<script[^>]*>`)

	forEachTemplate(t, func(name, body string) {
		if m := inlineHandler.FindString(body); m != "" {
			t.Errorf("%s has an inline event handler (%q); the CSP blocks it — move the behaviour into static/panel.js",
				name, strings.TrimSpace(m))
		}
		if m := inlineStyle.FindString(body); m != "" {
			t.Errorf("%s has an inline style (%q); the CSP blocks it — move the rule into static/panel.css",
				name, strings.TrimSpace(m))
		}
		for _, tag := range scriptTag.FindAllString(body, -1) {
			if !strings.Contains(tag, "src=") {
				t.Errorf("%s has an inline script (%q); the CSP blocks it — put the code in static/panel.js", name, tag)
			}
		}
	})
}

// default-src 'self' also means every asset a page pulls in must be one this
// server actually serves, so a typo in a /static path is a blocked request,
// not a 404 in the page's own colours.
func TestLayoutReferencesOnlyEmbeddedAssets(t *testing.T) {
	for _, file := range []string{"templates/layout_legacy.html", "templates/layout.html"} {
		body, err := fs.ReadFile(assetsFS, file)
		if err != nil {
			t.Fatalf("read layout: %v", err)
		}
		refs := regexp.MustCompile(`(?:src|href)="/static/([^"]+)"`).FindAllStringSubmatch(string(body), -1)
		if len(refs) == 0 {
			t.Fatalf("%s references no static assets at all", file)
		}
		for _, m := range refs {
			if _, err := fs.Stat(assetsFS, "static/"+m[1]); err != nil {
				t.Errorf("%s references /static/%s, which is not embedded: %v", file, m[1], err)
			}
		}
	}
}

// forEachTemplate runs fn over every embedded template's source.
func forEachTemplate(t *testing.T, fn func(name, body string)) {
	t.Helper()
	entries, err := fs.ReadDir(assetsFS, "templates")
	if err != nil {
		t.Fatalf("read templates: %v", err)
	}
	for _, e := range entries {
		body, err := fs.ReadFile(assetsFS, path.Join("templates", e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		fn(e.Name(), string(body))
	}
}

func TestMailQueuePageRendersRetryPolicy(t *testing.T) {
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var buf bytes.Buffer
	if err := engine.Page("mail_queue").ExecuteTemplate(&buf, "layout_legacy.html", map[string]any{
		"Title": "t", "User": "admin", "Active": "mail_queue", "Version": "test",
		"Copyright":  "Copyright © 2026 Mikhail Yenuchenko",
		"SourceURL":  "https://github.com/mixeme/selfpost",
		"FirstRetry": "10 minutes", "BackoffCap": "about 1 hour 7 minutes",
		"QueueLifetime": "2 days",
	}); err != nil {
		t.Fatalf("execute mail_queue: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"How delivery retries work",
		"10 minutes",
		"about 1 hour 7 minutes",
		"2 days",
		`id="retry-policy"`,
		`hx-get="/outbound/queue/fragment"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("mail_queue is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "compiled-in defaults") {
		t.Error("RetryFromDefaults was unset; the fallback note should stay off")
	}
}

func TestAuthenticatedLayoutIncludesHelpDrawer(t *testing.T) {
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var buf bytes.Buffer
	if err := engine.Page("help").ExecuteTemplate(&buf, "layout_legacy.html", map[string]any{
		"Title": "t", "User": "admin", "Active": "help", "IsGlobal": true,
	}); err != nil {
		t.Fatalf("execute help layout: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		`id="help-off"`, `class="help-drawer"`, `help-pane-status`,
		`for="help-dns"`, `href="/help"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("authenticated layout missing %q", want)
		}
	}
}

func TestLoginPageOmitsHelpDrawer(t *testing.T) {
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for name, data := range map[string]any{
		"login": NewLogin("", ""),
		"setup": NewSetup("token", ""),
	} {
		rec := httptest.NewRecorder()
		engine.Render(rec, http.StatusOK, name, data)
		if rec.Code != http.StatusOK {
			t.Fatalf("render %s: status %d: %s", name, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "help-drawer") {
			t.Errorf("%s page should not include the help drawer", name)
		}
	}
}

// The help drawer is gone from the pages of the kit: Overview points at the
// section of the Help page that explains its checks, from the head of its box.
func TestOverviewLinksToItsHelpTopic(t *testing.T) {
	out := renderSignedIn(t, "overview", overviewFixture())
	if !strings.Contains(out, `class="sp-help" href="/help#checks"`) {
		t.Error("the Server health box has no link to its Help topic")
	}
	if strings.Contains(out, "help-drawer") {
		t.Error("a page of the kit carries the old help drawer")
	}
}

func TestNavIncludesHelp(t *testing.T) {
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var buf bytes.Buffer
	if err := engine.Page("help").ExecuteTemplate(&buf, "nav", map[string]any{
		"User": "admin", "Active": "domains", "IsGlobal": true,
	}); err != nil {
		t.Fatalf("execute nav: %v", err)
	}
	if !strings.Contains(buf.String(), `href="/help"`) {
		t.Error("nav is missing Help link")
	}
}

// A group with nothing assigned is absent from that user's menu: a domain
// administrator who was given inbound domains only sees Inbound and none of the
// sending pages, and the mark takes them to what they have.
func TestLegacyNavFollowsTheUsersReach(t *testing.T) {
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	nav := func(data map[string]any) string {
		t.Helper()
		data["User"], data["Active"] = "ops", ""
		data["InboundEnabled"], data["DMARCEnabled"] = true, true
		var buf bytes.Buffer
		if err := engine.Page("help").ExecuteTemplate(&buf, "nav", data); err != nil {
			t.Fatalf("execute nav: %v", err)
		}
		return buf.String()
	}
	has := func(out, href string) bool { return strings.Contains(out, `href="`+href+`"`) }

	out := nav(map[string]any{"HasInbound": true})
	if !has(out, "/inbound/domains") {
		t.Errorf("an inbound-only administrator has no Inbound entry:\n%s", out)
	}
	for _, gone := range []string{"/outbound/domains", "/outbound/log", "/outbound/dmarc", "/overview", "/server/users", "/outbound/queue"} {
		if has(out, gone) {
			t.Errorf("an inbound-only administrator is offered %s", gone)
		}
	}

	out = nav(map[string]any{"HasOutbound": true})
	if has(out, "/inbound/domains") || !has(out, "/outbound/domains") || !has(out, "/outbound/log") || !has(out, "/outbound/dmarc") {
		t.Errorf("an outbound-only administrator's menu is wrong:\n%s", out)
	}

	out = nav(map[string]any{"IsGlobal": true})
	for _, want := range []string{"/overview", "/outbound/domains", "/inbound/domains", "/outbound/dmarc", "/outbound/log", "/server/users"} {
		if !has(out, want) {
			t.Errorf("the global role lost %s", want)
		}
	}
}
