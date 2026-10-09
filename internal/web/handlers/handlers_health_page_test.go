package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
	"github.com/mixeme/selfpost/internal/web/view"
)

// healthReading is one plausible reading of every check Health shows, so a test
// can render the page and vary the one part it is about.
func healthReading() map[string]any {
	c := checksFixture()
	c["Processes"] = []health.Process{
		{Name: "opendkim", State: "RUNNING", Detail: "pid 21", Status: health.StatusOK},
		{Name: "postfix", State: "FATAL", Detail: "exited too quickly", Status: health.StatusError},
	}
	c["Cert"] = health.Certificate{
		Path: "/etc/postfix/tls/fullchain.pem", Subject: "mail.example.com",
		NotAfter: time.Date(2026, 11, 8, 9, 14, 0, 0, time.UTC), DaysLeft: 30,
		Status: health.StatusOK, Detail: "Valid for another 30 day(s).",
	}
	c["Machine"] = health.Machine{
		CPU: health.CPU{
			Measured: true, BusyPct: 12.4, Cores: 4, Threads: 4,
			Status: health.StatusOK, Detail: "4 cores · 4 threads",
		},
		Memory: health.Memory{
			Measured: true, TotalBytes: 4 << 30, UsedBytes: 2 << 30, UsedPct: 50,
			Status: health.StatusOK, Detail: "2.0 GiB used of 4.0 GiB.",
		},
		Network: health.Network{
			Measured: true, RxRate: 2048, TxRate: 1024, Status: health.StatusOK,
			Interfaces: []health.Interface{{Name: "eth0", RxBytes: 1 << 20, TxBytes: 1 << 19, RxRate: 2048, TxRate: 1024, Measured: true}},
		},
		Window: 5 * time.Second,
		Status: health.StatusOK,
	}
	c["Sockets"] = []health.Socket{
		{Name: "OpenDKIM", Path: "/run/opendkim/opendkim.sock", Present: true, Status: health.StatusOK, Detail: "Listening"},
		{Name: "send-log", Path: "/run/selfpost/journal.sock", Status: health.StatusWarn, Detail: "send-log socket missing: the log stops being written."},
	}
	c["SocketStatus"] = health.StatusWarn
	c["Hostname"] = "mail.example.com"
	c["PTR"] = dnscheck.Result{
		Status:  health.StatusError,
		Detail:  "No address has a reverse record.",
		Records: []string{"203.0.113.10 → no PTR record"},
	}
	return c
}

func renderHealth(t *testing.T, h *Handlers, c map[string]any, flash string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.view.Render(rec, 200, "health", healthPage(view.Meta{User: "admin", IsGlobal: true}, c, flash))
	if rec.Code != 200 {
		t.Fatalf("render health: %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// Health shows every check of the reading, with the verdict of each box in its
// head, the anchors the Overview cards lead to, and the two server actions.
func TestHealthPageRendersEveryCheck(t *testing.T) {
	h, _ := settingsServer(t)
	out := renderHealth(t, h, healthReading(), "")
	for _, want := range []string{
		"opendkim", "fatal", "exited too quickly",
		"mail.example.com", "203.0.113.10 → no PTR record", "2026-11-08 09:14 UTC",
		`action="/server/health/reload"`, `action="/server/health/recheck"`,
		`hx-get="/server/health/fragment"`,
		`<span class="tag is-danger is-light">error</span>`, // postfix, and the reverse DNS box
		`id="processes"`, `id="machine"`, `id="certificate"`, `id="sockets"`, `id="hostname"`,
		// The machine card: the bars carry their reading in an attribute (the
		// CSP rules out sizing them with a style), and the figures are printed
		// beside them for anything that does not render a bar.
		`<progress class="progress is-small is-success mb-1" value="12"`, `value="50"`,
		"4 cores · 4 threads", "2.0 GiB used of 4.0 GiB",
		"eth0: 1.0 MiB in, 512.0 KiB out",
		// A socket that is not fine says why; one that is does not repeat "Listening".
		"send-log socket missing: the log stops being written.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("health page is missing %q", want)
		}
	}
	if strings.Contains(out, "Listening") {
		t.Error("a socket that answers repeats its detail beside the tag")
	}
	// The reload control stays outside the polled rows.
	body, conf := strings.Index(out, `id="health-machine"`), strings.Index(out, `id="configuration"`)
	if body < 0 || conf < body {
		t.Fatal("polled rows or the Configuration box missing or out of order")
	}
	if frag := out[body:conf]; strings.Contains(frag, `action="/server/health/reload"`) || !strings.Contains(frag, `id="hostname"`) {
		t.Error("the hostname box is outside the polled rows, or the reload control inside them")
	}
}

// A machine whose counters could not be read — no /proc, or a first reading
// with nothing to compare against — leaves its rows in place and blank, the
// same way an unreachable supervisord costs one line and not the page.
func TestHealthPageWithoutMachineMetrics(t *testing.T) {
	h, _ := settingsServer(t)
	c := healthReading()
	c["Machine"] = health.Machine{
		CPU:     health.CPU{Status: health.StatusUnknown, Detail: "The kernel's processor counters (/proc/stat) could not be read here."},
		Memory:  health.Memory{Status: health.StatusUnknown, Detail: "The kernel's memory counters (/proc/meminfo) could not be read here."},
		Network: health.Network{Status: health.StatusUnknown, Detail: "The kernel's network counters (/proc/net/dev) could not be read here."},
		Status:  health.StatusUnknown,
	}
	out := renderHealth(t, h, c, "")
	if strings.Contains(out, "<progress") {
		t.Error("a bar was drawn for a reading that does not exist")
	}
	for _, want := range []string{"/proc/stat", "/proc/meminfo", "/proc/net/dev", `<span class="tag is-light">unknown</span>`} {
		if !strings.Contains(out, want) {
			t.Errorf("degraded machine box is missing %q", want)
		}
	}
}

func TestHealthPageStatesOfTheOtherChecks(t *testing.T) {
	h, _ := settingsServer(t)
	c := healthReading()
	c["ProcessError"] = true
	c["ProcessStatus"] = health.StatusUnknown
	c["Processes"] = []health.Process(nil)
	c["Cert"] = health.Certificate{Status: health.StatusUnknown, Detail: "No certificate path is configured (TLS_CERT_FILE)."}
	c["Hostname"] = ""
	c["PTR"] = dnscheck.Result{Status: health.StatusUnknown}
	out := renderHealth(t, h, c, "DNS re-checked.")
	for _, want := range []string{
		"Could not ask supervisord for the process list.",
		"No certificate path is configured (TLS_CERT_FILE).",
		"(SELFPOST_HOSTNAME is not set)",
		`<div class="notification is-success is-light">`, "DNS re-checked.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("health page is missing %q", want)
		}
	}
}

// The fragment is what the page holds in its polled rows, read again.
func TestHealthFragmentCarriesTheSameRows(t *testing.T) {
	h, _ := settingsServer(t)
	h.machine = &health.MachineSampler{}
	h.cfg.Hostname = "mail.example.com"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/server/health/fragment", nil)
	h.HandleHealthFragment(rec, auth.RequestWithPrincipal(req, globalPrincipal))
	out := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("GET /server/health/fragment = %d:\n%s", rec.Code, out)
	}
	for _, want := range []string{
		`id="health-machine" data-poll`, `hx-get="/server/health/fragment"`,
		`id="health-checks" hx-swap-oob="true"`, `id="machine"`, `id="processes"`, `id="certificate"`, `id="sockets"`, `id="hostname"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("health fragment is missing %q", want)
		}
	}
	for _, gone := range []string{"<h1", "navbar", `action="/server/health/reload"`} {
		if strings.Contains(out, gone) {
			t.Errorf("health fragment carries %q", gone)
		}
	}
}

// Overview is one verdict per check, each leading to its table on Health, and
// the two kinds of domain; it shows none of the tables and none of the actions.
func TestOverviewPageShowsVerdictsAndDomains(t *testing.T) {
	h, _ := settingsServer(t)
	h.machine = &health.MachineSampler{}
	h.domains = domain.NewService(h.store, domain.NewOpenDKIM(t.TempDir()), nil, "selfpost")
	h.cfg.Hostname = "mail.example.org"
	d, err := h.store.AddDomain("example.org", "mail")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	if err := h.store.InsertQueued(store.SendLogEntry{QueueID: "Q1", Domain: d.Name, AppLogin: "app", From: "noreply@" + d.Name, To: "public@example.net", Subject: "Hello"}); err != nil {
		t.Fatalf("InsertQueued: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/overview", nil)
	h.HandleOverview(rec, auth.RequestWithPrincipal(req, globalPrincipal))
	out := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("GET /overview = %d:\n%s", rec.Code, out)
	}
	for _, want := range []string{
		`<h1 class="title is-3">`, `class="sp-postmark`, `<p class="sp-kicker">mail.example.org</p>`,
		`href="/server/health#certificate"`, `href="/server/health#sockets"`, `href="/outbound/queue"`,
		`<h2>Outbound domains</h2>`, `<a href="/outbound/domains/1"><strong>example.org</strong></a>`,
		"DKIM", "SPF", "DMARC", "1\u00a0msg", `<th>30 days</th>`,
		`href="/server/health"`,
		`id="health" data-poll aria-live="polite" hx-get="/overview/fragment" hx-trigger="load" hx-swap="outerHTML"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Overview is missing %q", want)
		}
	}
	// The inbound relay has no service in this test, so its box is not drawn.
	for _, gone := range []string{
		`action="/server/health/reload"`, `action="/server/health/recheck"`, `id="processes"`, "<progress",
		`hx-get="/server/health/fragment"`, `hx-swap-oob`, `Inbound domains`, `href="/inbound/domains"`,
	} {
		if strings.Contains(out, gone) {
			t.Errorf("Overview carries %q — the tables and the actions live on Health", gone)
		}
	}
}

// The fragment of Overview is the Server health box and the page head, marked
// out of band; the page has the poll attributes on that box and no such mark.
func TestOverviewFragmentIsTheBoxAndTheHead(t *testing.T) {
	h, _ := settingsServer(t)
	h.machine = &health.MachineSampler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/overview/fragment", nil)
	h.HandleOverviewFragment(rec, auth.RequestWithPrincipal(req, globalPrincipal))
	out := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("GET /overview/fragment = %d:\n%s", rec.Code, out)
	}
	if got := strings.Count(out, `<span class="sp-h-name">`); got != 6 {
		t.Errorf("the fragment holds %d cards, want 6", got)
	}
	for _, want := range []string{
		`<div class="sp-head" id="overview-head" hx-swap-oob="true">`, `class="sp-postmark`, "Updated ",
		`id="health" data-poll aria-live="polite" hx-get="/overview/fragment" hx-trigger="load" hx-swap="outerHTML"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the fragment is missing %q", want)
		}
	}
	for _, gone := range []string{"<html", "navbar", "<table", "Outbound domains"} {
		if strings.Contains(out, gone) {
			t.Errorf("the fragment carries %q", gone)
		}
	}
}
