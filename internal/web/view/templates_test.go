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
	"time"

	"github.com/mixeme/selfpost/internal/health"
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

// The section index each long page shows in the navigation column works by
// overriding an empty "sections" block defined in the layout, which only holds
// as long as the layout is parsed before the page's own files (see pageFiles).
// Reverse that order and every index would silently disappear — the empty
// definition would win and no page would fail to render — so the two ends are
// asserted here: the long pages produce a list, and a page that defines nothing
// produces nothing at all.
func TestSectionIndexIsOnTheLongPagesOnly(t *testing.T) {
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Anchors the index links to, taken from the page's own cards.
	wantAnchors := map[string]string{
		"status":        `href="#certificate"`,
		"domain_detail": `href="#danger"`,
	}
	for name, page := range engine.Pages() {
		var buf bytes.Buffer
		// The domain page's index hides the freshly generated credential entry
		// unless one is on the page, so the data map carries the key it reads.
		if err := page.ExecuteTemplate(&buf, "sections", map[string]any{"NewCred": nil}); err != nil {
			t.Fatalf("execute sections for %q: %v", name, err)
		}
		out := buf.String()
		anchor, wanted := wantAnchors[name]
		switch {
		case wanted && !strings.Contains(out, anchor):
			t.Errorf("page %q shows no section index (expected %s):\n%s", name, anchor, out)
		case !wanted && strings.TrimSpace(out) != "":
			t.Errorf("page %q is not long enough to carry a section index:\n%s", name, out)
		}
	}
}

// A section link that points at no card is a link that does nothing, and
// nothing about rendering the page says so. Every anchor the index offers must
// name an element the same page defines an id for.
func TestSectionLinksPointAtCardsThatExist(t *testing.T) {
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// The pages that carry an index; both are checked with a credential shown,
	// which is the domain page's one conditional entry.
	for _, name := range []string{"status", "domain_detail"} {
		var index bytes.Buffer
		if err := engine.Page(name).ExecuteTemplate(&index, "sections", map[string]any{"NewCred": true}); err != nil {
			t.Fatalf("execute sections for %q: %v", name, err)
		}
		// The cards are spread over the page's template files, so the ids are
		// collected from the files rather than from a rendered page — rendering
		// one would need the whole of a handler's data map.
		ids := map[string]bool{}
		for _, file := range pageFiles[name] {
			body, err := fs.ReadFile(assetsFS, file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			// Cards only: a form field's id is not somewhere a section link may
			// land, so matching those too would weaken the check.
			for _, m := range regexp.MustCompile(`class="card[^"]*" id="([a-z-]+)"`).FindAllStringSubmatch(string(body), -1) {
				ids[m[1]] = true
			}
		}
		for _, m := range regexp.MustCompile(`href="#([a-z-]+)"`).FindAllStringSubmatch(index.String(), -1) {
			if !ids[m[1]] {
				t.Errorf("page %q indexes #%s, which no card on it carries", name, m[1])
			}
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
		var buf bytes.Buffer
		err := engine.Page(name).ExecuteTemplate(&buf, "layout.html", map[string]any{
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
	var buf bytes.Buffer
	if err := engine.Page("login").ExecuteTemplate(&buf, "layout.html", map[string]any{
		"Title": "t", "Active": "", "Version": "9.9.9-test",
		"Copyright": "Copyright © 2026 Mikhail Yenuchenko",
		"SourceURL": "https://github.com/mixeme/selfpost",
	}); err != nil {
		t.Fatalf("execute login: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "9.9.9-test") {
		t.Errorf("the login page shows the version to unauthenticated visitors:\n%s", out)
	}
	for _, want := range legalBits {
		if !strings.Contains(out, want) {
			t.Errorf("login page is missing legal notice %q", want)
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
	err = engine.Page("dashboard").ExecuteTemplate(&buf, "nav", map[string]any{
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
	if strings.Contains(out, `href="/mail-queue"`) {
		t.Errorf("active page still links to itself:\n%s", out)
	}
	if !strings.Contains(out, `href="/deliveries"`) {
		t.Errorf("inactive pages are not linked:\n%s", out)
	}
}

func TestNavLeadsWithStatusAndPointsDomainsAtItsOwnPath(t *testing.T) {
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var buf bytes.Buffer
	if err := engine.Page("status").ExecuteTemplate(&buf, "nav", map[string]any{
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
	if !strings.Contains(out, `href="/domains"`) {
		t.Errorf("Domains does not link to /domains:\n%s", out)
	}
	if strings.Index(out, "Status") > strings.Index(out, "Domains") {
		t.Errorf("Status is not the first navigation entry:\n%s", out)
	}
}

// Whether a page takes the whole column or the reading measure is declared by
// the page's own "wide" block (see layout.html), which the layout stamps into
// <main>'s class list. A page that loses the block does not fail to render — it
// silently comes back at the measure, with its table squeezed into two thirds
// of the column — so the set is asserted here, in both directions.
func TestOnlyThePagesMadeOfDataDeclareThemselvesWide(t *testing.T) {
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	wide := map[string]bool{"account": true, "deliveries": true, "delivery": true, "mail_queue": true, "system_log": true}
	for name, page := range engine.Pages() {
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

// Drill-down pages carry an up-link directly under the heading and above the
// cards. A link at the bottom of a form is easy to miss and drifts from the
// rest of the panel, so the shared back_link template is mandatory on those
// pages and TestDrillDownPagesPlaceBackLinkAboveContent guards its position.
func TestDrillDownPagesPlaceBackLinkAboveContent(t *testing.T) {
	drillDown := map[string]bool{
		"user_form.html":     true,
		"domain_detail.html": true,
		"domain_delete.html": true,
		"delivery.html":      true,
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
			t.Errorf(`%s links to "/", which is now the status redirect; link to /domains (or the intended page) instead`, name)
		}
	})
}

// The reload action is a server-health control and lives only on the status
// page.
func TestReloadFormLivesOnlyOnTheStatusPage(t *testing.T) {
	forEachTemplate(t, func(name, body string) {
		if strings.Contains(body, `action="/reload"`) && name != "status.html" {
			t.Errorf("%s still posts to /reload; the reload control belongs on the status page", name)
		}
	})
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
	out := renderStatusPage(t, statusPageData())
	for _, want := range []string{
		"opendkim", "FATAL", "Mail queue is empty", "mail.example.com",
		"203.0.113.10 → no PTR record", `action="/reload"`,
		`hx-get="/status/fragment"`, `class="st st-error"`,
		// The machine card: the bars carry their reading in an attribute
		// (the CSP rules out sizing them with a style), and the figures are
		// printed beside them for anything that does not render a meter.
		`<meter value="12"`, `<meter value="50"`,
		"load average 0.31, 0.24, 0.19", "2.0 GiB used of 4.0 GiB",
		"eth0: 1.0 MiB in, 512.0 KiB out",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status page is missing %q", want)
		}
	}
}

// A machine whose counters could not be read — no /proc, or a first reading
// with nothing to compare against — must leave the card in place with its rows
// blank, the same way an unreachable supervisord costs one line and not the
// page.
func TestStatusPageWithoutMachineMetrics(t *testing.T) {
	data := statusPageData()
	data["Machine"] = health.Machine{
		CPU:     health.CPU{Status: health.StatusUnknown, Detail: "The kernel's processor counters (/proc/stat) could not be read here."},
		Memory:  health.Memory{Status: health.StatusUnknown, Detail: "The kernel's memory counters (/proc/meminfo) could not be read here."},
		Network: health.Network{Status: health.StatusUnknown, Detail: "The kernel's network counters (/proc/net/dev) could not be read here."},
		Status:  health.StatusUnknown,
	}

	out := renderStatusPage(t, data)
	if strings.Contains(out, "<meter") {
		t.Error("a bar was drawn for a reading that does not exist")
	}
	for _, want := range []string{
		`<h2>Machine <span class="st st-unknown">`,
		"/proc/stat", "/proc/meminfo", "/proc/net/dev",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("degraded machine card is missing %q", want)
		}
	}
}

func renderStatusPage(t *testing.T, data map[string]any) string {
	t.Helper()
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var buf bytes.Buffer
	if err := engine.Page("status").ExecuteTemplate(&buf, "layout.html", data); err != nil {
		t.Fatalf("execute status page: %v", err)
	}
	return buf.String()
}

// statusPageData is one plausible reading of every check the status page shows,
// so a test can render the page and vary the one part it is about.
func statusPageData() map[string]any {
	return map[string]any{
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
		"Machine": health.Machine{
			CPU: health.CPU{
				Measured: true, BusyPct: 12.4, Cores: 4,
				Load: [3]float64{0.31, 0.24, 0.19}, HasLoad: true,
				Status: health.StatusOK, Detail: "4 core(s) · load average 0.31, 0.24, 0.19",
			},
			Memory: health.Memory{
				Measured: true, TotalBytes: 4 << 30, UsedBytes: 2 << 30, UsedPct: 50,
				Status: health.StatusOK, Detail: "2.0 GiB used of 4.0 GiB; 2.0 GiB available to new work.",
			},
			Network: health.Network{
				Measured: true, RxRate: 2048, TxRate: 1024,
				Interfaces: []health.Interface{
					{Name: "eth0", RxBytes: 1 << 20, TxBytes: 1 << 19, RxRate: 2048, TxRate: 1024, Measured: true},
				},
				Status: health.StatusOK,
			},
			Window: 5 * time.Second,
			Status: health.StatusOK,
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
	}
}

// dnscheckResult mirrors dnscheck.Result's shape for the template test, so the
// view package's template tests do not depend on the checker's constructor.
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
