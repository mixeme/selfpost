package web

import (
	"bytes"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
	"time"

	"codeberg.org/mix/selfpost/internal/health"
)

// The navigation is rendered from the layout, not copied into each page, so
// every page template must resolve it. This is what makes "the nav is on every
// authenticated page" a structural property instead of a checklist item.
func TestEveryPageResolvesNav(t *testing.T) {
	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	for name, page := range tmpl.pages {
		if page.Lookup("nav") == nil {
			t.Errorf("page %q does not resolve the shared nav template", name)
		}
	}
}

func TestNavMarksActivePage(t *testing.T) {
	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	var buf bytes.Buffer
	err = tmpl.pages["dashboard"].ExecuteTemplate(&buf, "nav", map[string]any{
		"User":   "admin",
		"Active": "queue",
	})
	if err != nil {
		t.Fatalf("execute nav: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `<span aria-current="page">Queue</span>`) {
		t.Errorf("active page is not marked:\n%s", out)
	}
	if strings.Contains(out, `href="/queue"`) {
		t.Errorf("active page still links to itself:\n%s", out)
	}
	if !strings.Contains(out, `href="/sendlog"`) {
		t.Errorf("inactive pages are not linked:\n%s", out)
	}
}

func TestNavLeadsWithStatusAndPointsDomainsAtItsOwnPath(t *testing.T) {
	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.pages["status"].ExecuteTemplate(&buf, "nav", map[string]any{
		"User":   "admin",
		"Active": "status",
	}); err != nil {
		t.Fatalf("execute nav: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `<span aria-current="page">Status</span>`) {
		t.Errorf("the status page is not marked active:\n%s", out)
	}
	if !strings.Contains(out, `href="/domains"`) {
		t.Errorf("Domains does not link to /domains:\n%s", out)
	}
	if strings.Index(out, "Status") > strings.Index(out, "Domains") {
		t.Errorf("Status is not the first navigation entry:\n%s", out)
	}
}

// Since the panel root now redirects to the status page, a link left pointing at
// "/" silently lands on the wrong screen instead of failing — so no template may
// contain one (phase 13.C).
func TestNoTemplateLinksToTheBareRoot(t *testing.T) {
	forEachTemplate(t, func(name, body string) {
		if strings.Contains(body, `href="/"`) {
			t.Errorf(`%s links to "/", which is now the status redirect; link to /domains (or the intended page) instead`, name)
		}
	})
}

// The reload action is a server-health control and lives only on the status
// page (phase 13.D).
func TestReloadFormLivesOnlyOnTheStatusPage(t *testing.T) {
	forEachTemplate(t, func(name, body string) {
		if strings.Contains(body, `action="/reload"`) && name != "status.html" {
			t.Errorf("%s still posts to /reload; the reload control belongs on the status page", name)
		}
	})
}

// The panel's Content-Security-Policy is a plain default-src 'self' with no
// inline exemption (phase 14.A), which makes inline script and inline style a
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
	body, err := fs.ReadFile(assetsFS, "templates/layout.html")
	if err != nil {
		t.Fatalf("read layout: %v", err)
	}
	refs := regexp.MustCompile(`(?:src|href)="/static/([^"]+)"`).FindAllStringSubmatch(string(body), -1)
	if len(refs) == 0 {
		t.Fatal("the layout references no static assets at all")
	}
	for _, m := range refs {
		if _, err := fs.Stat(assetsFS, "static/"+m[1]); err != nil {
			t.Errorf("layout references /static/%s, which is not embedded: %v", m[1], err)
		}
	}
}

func TestStatusPageRendersEveryCheck(t *testing.T) {
	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	var buf bytes.Buffer
	err = tmpl.pages["status"].ExecuteTemplate(&buf, "layout.html", map[string]any{
		"Title":  "SelfPost — status",
		"User":   "admin",
		"Active": "status",
		"Processes": []health.Process{
			{Name: "opendkim", State: "RUNNING", Detail: "pid 21", Status: health.StatusOK},
			{Name: "postfix", State: "FATAL", Detail: "exited too quickly", Status: health.StatusError},
		},
		"ProcessStatus": health.StatusError,
		"QueueSummary":  "Mail queue is empty",
		"QueueStatus":   health.StatusOK,
		"Cert": health.Certificate{
			Path: "/etc/postfix/tls/fullchain.pem", Subject: "mail.example.com",
			NotAfter: time.Now().Add(30 * 24 * time.Hour), DaysLeft: 30,
			Status: health.StatusOK, Detail: "Valid for another 30 day(s).",
		},
		"Sockets": []health.Socket{
			{Name: "OpenDKIM", Path: "/run/opendkim/opendkim.sock", Present: true, Status: health.StatusOK, Detail: "Listening."},
		},
		"SocketStatus":   health.StatusOK,
		"OverallStatus":  health.StatusError,
		"OverallHeading": "A component needs attention — see the details below.",
		"Hostname":       "mail.example.com",
		"PTR": dnscheckResult{
			Status:  health.StatusError,
			Detail:  "No address has a reverse record.",
			Records: []string{"203.0.113.10 → no PTR record"},
		},
	})
	if err != nil {
		t.Fatalf("execute status page: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"opendkim", "FATAL", "Mail queue is empty", "mail.example.com",
		"203.0.113.10 → no PTR record", `action="/reload"`,
		`hx-get="/status/fragment"`, `class="st st-error"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status page is missing %q", want)
		}
	}
}

// dnscheckResult mirrors dnscheck.Result's shape for the template test, so the
// web package's template tests do not depend on the checker's constructor.
type dnscheckResult struct {
	Status  health.Status
	Detail  string
	Records []string
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
