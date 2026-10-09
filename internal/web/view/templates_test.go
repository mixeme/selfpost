package view

import (
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

// The version and the legal lines come from Render, not from each handler's
// data, so the footer is only correct as long as every page composes with the
// layout. It is asserted on every page, not trusted. Appropriate Legal Notices
// (copyright, licence, source, no warranty) appear on every page, signed out
// ones included; the running version only on the signed-in ones.
func TestLayoutShowsTheVersionOnlyWhenSignedIn(t *testing.T) {
	engine := kitEngine(t) // version 9.9.9-test
	legalBits := []string{
		"Copyright © 2026 Mikhail Yenuchenko",
		`href="/license"`,
		"License (AGPL-3.0)",
		`href="https://github.com/mixeme/selfpost"`,
		"Source",
		"No warranty",
	}
	signedOut := map[string]bool{"login": true, "setup": true}
	for name, fixture := range pageFixtures {
		rec := httptest.NewRecorder()
		engine.Render(rec, http.StatusOK, name, fixture())
		if rec.Code != http.StatusOK {
			t.Fatalf("render %s: status %d: %s", name, rec.Code, rec.Body.String())
		}
		out := rec.Body.String()
		if signedOut[name] {
			if strings.Contains(out, "9.9.9-test") {
				t.Errorf("the %s page shows the version to unauthenticated visitors:\n%s", name, out)
			}
		} else if !strings.Contains(out, "SelfPost 9.9.9-test") {
			t.Errorf("page %q does not show the version in the layout footer", name)
		}
		for _, want := range legalBits {
			if !strings.Contains(out, want) {
				t.Errorf("%s page is missing legal notice %q", name, want)
			}
		}
		// The help lives on the Help page: no page carries a drawer for it.
		for _, gone := range []string{`id="help-off"`, `help-drawer`, `help-pane`, `for="help-`, `help-scrim`} {
			if strings.Contains(out, gone) {
				t.Errorf("page %q still carries the help drawer (%q)", name, gone)
			}
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
	for _, file := range []string{"templates/layout.html"} {
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
