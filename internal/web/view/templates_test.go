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
	if strings.Contains(out, `href="/inbound"`) || strings.Contains(out, "Inbound") {
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
	if err := engine.Page("status").ExecuteTemplate(&buf, "nav", map[string]any{
		"User":           "admin",
		"Active":         "status",
		"IsGlobal":       true,
		"InboundEnabled": true,
	}); err != nil {
		t.Fatalf("execute nav: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `href="/inbound"`) || !strings.Contains(out, "Inbound") {
		t.Errorf("Inbound nav is missing while InboundEnabled is true:\n%s", out)
	}
	dom := strings.Index(out, `href="/domains"`)
	inb := strings.Index(out, `href="/inbound"`)
	if dom < 0 || inb < 0 || inb < dom {
		t.Errorf("Inbound should follow Domains:\n%s", out)
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
	wide := map[string]bool{
		"settings": true, "deliveries": true, "delivery": true, "mail_queue": true,
		"status": true, "system_log": true, "domain_detail": true,
		"inbound": true, "inbound_domain": true, "dmarc": true,
	}
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

// The domain page pairs cards the same way Status does: three .split rows
// (DKIM+SPF|DMARC, connection|add-app, export|danger). DNS status, Applications
// and Domain settings are full-width; DNS status and Domain settings (and the
// application Edit panel) use .check-cols. Losing a row silently stacks again.
func TestDomainDetailPageHasPairedCards(t *testing.T) {
	body, err := fs.ReadFile(assetsFS, "templates/domain_detail.html")
	if err != nil {
		t.Fatalf("read domain_detail: %v", err)
	}
	src := string(body)
	if got := strings.Count(src, `class="split"`); got != 3 {
		t.Errorf("domain detail has %d .split rows, want 3", got)
	}
	if !strings.Contains(src, `class="check-cols"`) {
		t.Error("domain detail is missing the check-cols grid")
	}
	if !strings.Contains(src, `class="panel-toggle t-edit"`) {
		t.Error("application Edit should be a single panel-toggle")
	}
	if strings.Contains(src, `panel-toggle t-mode`) || strings.Contains(src, `panel-toggle t-limit`) ||
		strings.Contains(src, `panel-mode`) || strings.Contains(src, `panel-limit`) {
		t.Error("application Edit mode and Rate limit should be one Edit button")
	}
	for _, id := range []string{
		`id="dkim-spf"`, `id="dns-status"`, `id="dmarc"`,
		`id="connection"`, `id="add-application"`, `id="applications"`,
		`id="domain-settings"`, `id="export"`, `id="danger"`,
	} {
		if !strings.Contains(src, id) {
			t.Errorf("domain detail is missing %s", id)
		}
	}
	if strings.Contains(src, `id="rate-limit"`) {
		t.Error("domain rate limit should live inside domain-settings, not its own card")
	}
	if strings.Contains(src, `id="d_ips"`) {
		t.Error("domain rate limit must not ask for client IPs")
	}
	if !strings.Contains(src, "{{.L1Messages}}") && !strings.Contains(src, "{{$.L1Messages}}") {
		t.Error("domain rate limit should show the L1 message count")
	}
	if !strings.Contains(src, "Level&nbsp;1 backstop") {
		t.Error("domain rate limit should show a Level 1 backstop line")
	}
	if !strings.Contains(src, "Restrict to listed IPs") {
		t.Error("application should offer client IP allow-list")
	}
	if strings.Contains(src, `id="spf-dmarc"`) {
		t.Error("SPF should sit with DKIM, not with DMARC")
	}
}

func TestSettingsPageDocumentsRateLimits(t *testing.T) {
	body, err := fs.ReadFile(assetsFS, "templates/settings.html")
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	src := string(body)
	if !strings.Contains(src, `id="rate-limits"`) {
		t.Error("settings should include a sending rate limits card")
	}
	if !strings.Contains(src, `id="deliveries-retention"`) {
		t.Error("settings should include a send log retention card for global administrators")
	}
	for _, want := range []string{
		"RATE_LIMIT_MESSAGES_PER_IP",
		"Level 2 — domain",
		"Level 2 — application",
		"{{.L1Messages}} messages / {{.L1Window}} seconds",
		`name="send_log_retention_days"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("settings rate limits card missing %q", want)
		}
	}
}

func TestDrillDownPagesPlaceBackLinkAboveContent(t *testing.T) {
	drillDown := map[string]bool{
		"user_form.html":      true,
		"user_delete.html":    true,
		"domain_detail.html":  true,
		"domain_delete.html":  true,
		"inbound_domain.html": true,
		"inbound_delete.html": true,
		"dmarc_domain.html":   true,
		"dmarc_report.html":   true,
		"delivery.html":       true,
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
		// Three .split rows inside the polled fragment: machine|processes,
		// queue|certificate, and sockets|hostname. Ids stay on the cards.
		`id="processes"`, `id="machine"`, `id="queue"`, `id="certificate"`, `id="sockets"`, `id="hostname"`,
		`action="/status/recheck"`,
		// The machine card: the bars carry their reading in an attribute
		// (the CSP rules out sizing them with a style), and the figures are
		// printed beside them for anything that does not render a meter.
		`<meter value="12"`, `<meter value="50"`,
		"4 cores · 4 threads", "2.0 GiB used of 4.0 GiB",
		"eth0: 1.0 MiB in, 512.0 KiB out",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status page is missing %q", want)
		}
	}
	if got := strings.Count(out, `class="split"`); got != 3 {
		t.Errorf("status page has %d .split rows, want 3", got)
	}
	// Hostname must live inside the fragment so a poll refresh keeps it beside
	// sockets; Configuration stays outside (static reload control).
	body := strings.Index(out, `id="status-body"`)
	conf := strings.Index(out, `id="configuration"`)
	if body < 0 || conf < 0 || conf < body {
		t.Fatal("status-body or configuration card missing or out of order")
	}
	frag := out[body:conf]
	if !strings.Contains(frag, `id="hostname"`) {
		t.Error("hostname card is outside the polled status-body fragment")
	}
	if strings.Contains(frag, `id="configuration"`) || strings.Contains(frag, `action="/reload"`) {
		t.Error("configuration reload must stay outside the polled fragment")
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
				Measured: true, BusyPct: 12.4, Cores: 4, Threads: 4,
				Load: [3]float64{0.31, 0.24, 0.19}, HasLoad: true,
				Status: health.StatusOK, Detail: "4 cores · 4 threads",
			},
			Memory: health.Memory{
				Measured: true, TotalBytes: 4 << 30, UsedBytes: 2 << 30, UsedPct: 50,
				Status: health.StatusOK, Detail: "2.0 GiB used of 4.0 GiB.",
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
			{Name: "OpenDKIM", Path: "/run/opendkim/opendkim.sock", Present: true, Status: health.StatusOK, Detail: "Listening"},
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

func TestMailQueuePageRendersRetryPolicy(t *testing.T) {
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var buf bytes.Buffer
	if err := engine.Page("mail_queue").ExecuteTemplate(&buf, "layout.html", map[string]any{
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
		`hx-get="/mail-queue/body"`,
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
	if err := engine.Page("help").ExecuteTemplate(&buf, "layout.html", map[string]any{
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
	var buf bytes.Buffer
	if err := engine.Page("login").ExecuteTemplate(&buf, "layout.html", map[string]any{
		"Title": "t", "Active": "",
	}); err != nil {
		t.Fatalf("execute login: %v", err)
	}
	if strings.Contains(buf.String(), "help-drawer") {
		t.Error("login page should not include the help drawer")
	}
}

func TestStatusPageHasHelpEntry(t *testing.T) {
	out := renderStatusPage(t, statusPageData())
	if !strings.Contains(out, `for="help-status"`) {
		t.Error("status page is missing the help entry point")
	}
}

func TestDomainDetailHasHelpOnCards(t *testing.T) {
	body, err := fs.ReadFile(assetsFS, "templates/domain_detail.html")
	if err != nil {
		t.Fatalf("read domain_detail: %v", err)
	}
	src := string(body)
	for _, want := range []string{
		`"ID" "dns"`, `"ID" "records"`, `"ID" "dmarc"`, `"ID" "connection"`,
		`"ID" "apps"`, `"ID" "domain-settings"`, `"ID" "export"`,
		`card-head`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("domain_detail missing %q", want)
		}
	}
}

func TestNavIncludesHelp(t *testing.T) {
	engine, err := New("test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var buf bytes.Buffer
	if err := engine.Page("dashboard").ExecuteTemplate(&buf, "nav", map[string]any{
		"User": "admin", "Active": "domains", "IsGlobal": true,
	}); err != nil {
		t.Fatalf("execute nav: %v", err)
	}
	if !strings.Contains(buf.String(), `href="/help"`) {
		t.Error("nav is missing Help link")
	}
}
