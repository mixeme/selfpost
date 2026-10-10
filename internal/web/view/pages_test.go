package view

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// renderSignedIn renders a page of the kit through the engine, as a handler
// would, with the inbound relay and DMARC switched on.
func renderSignedIn(t *testing.T, page string, data any) string {
	t.Helper()
	engine := newGuardEngine(t)
	rec := httptest.NewRecorder()
	engine.Render(rec, http.StatusOK, page, data)
	if rec.Code != http.StatusOK {
		t.Fatalf("render %s: status %d: %s", page, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func renderFragment(t *testing.T, name string, data any) string {
	t.Helper()
	engine := newGuardEngine(t)
	rec := httptest.NewRecorder()
	engine.RenderFragment(rec, http.StatusOK, name, data)
	if rec.Code != http.StatusOK {
		t.Fatalf("render fragment %s: status %d: %s", name, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func pageHas(t *testing.T, page, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("%s is missing %q", page, want)
		}
	}
}

func pageLacks(t *testing.T, page, out string, gones ...string) {
	t.Helper()
	for _, gone := range gones {
		if strings.Contains(out, gone) {
			t.Errorf("%s carries %q", page, gone)
		}
	}
}

// ---- Overview

// The stamp, the sentence and the cards say the same thing: the worst card
// colours the stamp, the number counts the cards that are not fine, and the
// lead names them with their detail and a way to the table behind each.
func TestOverviewHeadFollowsTheCards(t *testing.T) {
	out := renderSignedIn(t, "overview", overviewFixture())
	pageHas(t, "Overview", out,
		`<h1 class="title is-3">Running, with warnings.</h1>`,
		`class="sp-postmark sp-warn"`, `<b>WARN</b>`, `1 OF 6 CHECKS`,
		`Needs attention: <a href="/server/health#certificate">TLS certificate</a> (Expires in 12 days).`,
		`Updated 14:02:31 UTC`, `<p class="sp-kicker">mail.example.org</p>`,
		`class="sp-warn"`, // the certificate card
		`href="/server/health"`, `>All details</a>`,
	)

	cards := overviewFixture().Cards
	for i := range cards {
		cards[i].Level = ""
	}
	ok := NewOverview(admin(), "mail.example.org", cards, time.Now(), 30, true)
	if p := ok.Head.Postmark; p.Level != LevelOK || p.Word != "OK" || p.Bottom != "6 OF 6 CHECKS" {
		t.Errorf("a healthy panel is stamped %+v", *p)
	}
	if ok.Head.Lead != nil {
		t.Errorf("a healthy panel names something that needs attention: %v", ok.Head.Lead)
	}

	cards[1].Level, cards[2].Level = LevelWarn, LevelFail
	bad := NewOverview(admin(), "", cards, time.Now(), 30, true)
	if p := bad.Head.Postmark; p.Level != LevelFail || p.Word != "FAIL" || p.Bottom != "2 OF 6 CHECKS" {
		t.Errorf("a failing card does not make the stamp FAIL: %+v", *p)
	}
	if bad.Head.Kicker != "" {
		t.Errorf("kicker %q without a host name", bad.Head.Kicker)
	}
}

func TestOverviewListsBothKindsOfDomain(t *testing.T) {
	out := renderSignedIn(t, "overview", overviewFixture())
	pageHas(t, "Overview", out,
		`<h2>Outbound domains</h2>`, `<h2>Inbound domains</h2>`,
		`<th>30 days</th>`, "1 284 msg", "18 920 msg",
		`<a href="/outbound/domains/1"><strong>example.org</strong></a>`,
		`<span class="tag is-danger is-light">SPF</span>`, `<span class="tag is-warning is-light">DMARC</span>`,
		`<a href="/inbound/domains/2"><strong>acme.io</strong></a>`, `10.0.4.12:2525`,
		`href="/outbound/domains">Manage`, `href="/inbound/domains">Manage`,
	)
}

// With the inbound relay off the page neither draws nor links the inbound box.
func TestOverviewWithoutInboundHasNoInboundBox(t *testing.T) {
	o := NewOverview(admin(), "mail.example.org", overviewFixture().Cards, time.Now(), 30, false)
	out := renderSignedIn(t, "overview", o)
	pageHas(t, "Overview", out, `<h2>Outbound domains</h2>`)
	pageLacks(t, "Overview", out, `Inbound domains`, `<th>MX</th>`)
	if got := strings.Count(out, `>Manage</a>`); got != 1 {
		t.Errorf("%d Manage links, want the outbound one only", got)
	}
	if strings.Contains(out, `class="column is-7"`) {
		t.Error("the outbound box is squeezed into a column with nothing beside it")
	}
}

func TestOverviewEmptyTablesSayWhatIsAbsent(t *testing.T) {
	o := NewOverview(admin(), "mail.example.org", overviewFixture().Cards, time.Now(), 30, true)
	out := renderSignedIn(t, "overview", o)
	pageHas(t, "Overview", out, `No outbound domains yet.`, `No inbound domains yet.`)
	pageLacks(t, "Overview", out, `<table`)
}

// A domain name is typed by a person and an upstream is typed by a person.
func TestOverviewEscapesWhatPeopleTyped(t *testing.T) {
	o := overviewFixture()
	o.OutboundRows[0].Name = `<script>alert(1)</script>`
	o.InboundRows[0].Upstream = `"><img src=x>`
	out := renderSignedIn(t, "overview", o)
	pageLacks(t, "Overview", out, `<script>alert(1)`, `<img src=x>`)
}

func TestFormatMessagesGroupsThousands(t *testing.T) {
	for n, want := range map[int64]string{0: "0 msg", 402: "402 msg", 1284: "1 284 msg", 1234567: "1 234 567 msg"} {
		if got := FormatMessages(n); got != want {
			t.Errorf("FormatMessages(%d) = %q, want %q", n, got, want)
		}
	}
}

// ---- Health

func TestHealthShowsEveryCheckAndBothActions(t *testing.T) {
	out := renderSignedIn(t, "health", healthFixture(false))
	pageHas(t, "Health", out,
		`<h1 class="title is-3">Health</h1>`,
		// the two actions: Re-check DNS in the head, Reload configuration under the tables
		`action="/server/health/recheck"`, `action="/server/health/reload"`,
		// the anchors the Overview cards lead to
		`id="machine"`, `id="processes"`, `id="certificate"`, `id="sockets"`, `id="hostname"`, `id="configuration"`,
		// the machine card: bars carry their reading in an attribute (the CSP rules out sizing them with a style)
		`<progress class="progress is-small is-success mb-1" value="12" max="100">`, `value="41"`,
		`↓ 18.0 KiB/s`, `eth0: 2.1 GiB in, 6.4 GiB out`,
		`pid 41, uptime 18 days`, `<span class="tag is-success is-light">running</span>`,
		`2026-10-21 09:14 UTC`, `class="mt-4 has-text-danger"`, `Check that renewal on the host still works.`,
		`inet:127.0.0.1:8891`, `→ 203.0.113.25<br>← mail.example.org`,
		`hx-get="/server/health/fragment"`,
	)
	pageLacks(t, "Health", out, `hx-swap-oob`, `/overview/fragment`)
	if got := strings.Count(out, `class="columns"`); got != 2 {
		t.Errorf("Health has %d rows of boxes, want 2", got)
	}
	// The Configuration box is outside the polled rows: a refresh must not re-render a control.
	if strings.Index(out, `id="configuration"`) < strings.Index(out, `id="health-checks"`) ||
		strings.Contains(out[strings.Index(out, `id="health-machine"`):strings.Index(out, `id="configuration"`)], `action="/server/health/reload"`) {
		t.Error("the reload control is inside the polled rows")
	}
}

// The fragment is the polled part of the page and nothing else: the same rows,
// the second one marked to be swapped in by its id, no shell and no actions.
func TestHealthFragmentIsThePolledRows(t *testing.T) {
	page := renderSignedIn(t, "health", healthFixture(false))
	frag := renderFragment(t, "health_body", healthFixture(true))

	rows := func(s string) string {
		a := strings.Index(s, `<div class="columns" id="health-machine"`)
		b := strings.Index(s, `id="configuration"`)
		if b < 0 {
			b = len(s)
		}
		return s[a:b]
	}
	// Same markup, apart from the out-of-band mark and what follows the rows on the page.
	got := strings.Replace(strings.TrimSpace(frag), ` hx-swap-oob="true"`, "", 1)
	want := strings.TrimSpace(rows(page))
	if !strings.HasPrefix(want, got) {
		t.Errorf("the fragment is not the page's polled rows\nfragment: %.400s\npage:     %.400s", got, want)
	}
	pageHas(t, "fragment", frag, `id="health-machine" data-poll`, `id="health-checks" hx-swap-oob="true"`)
	pageLacks(t, "fragment", frag, `<h1`, `navbar`, `action="/server/health/reload"`, `action="/server/health/recheck"`)
}

// A machine whose counters could not be read keeps its rows: the figure becomes
// a dash and the reason stays in the detail column.
func TestHealthMachineWithoutReadings(t *testing.T) {
	h := healthFixture(false)
	h.MachineRows = []MachineRow{
		{Resource: "CPU", Detail: []string{"The kernel's processor counters (/proc/stat) could not be read here."}},
		{Resource: "Network", Detail: []string{"The kernel's network counters (/proc/net/dev) could not be read here."}},
	}
	out := renderSignedIn(t, "health", h)
	pageHas(t, "Health", out, `<span class="sp-muted">—</span>`, `/proc/stat`, `/proc/net/dev`)
	pageLacks(t, "Health", out, `<progress`)
}

func TestHealthGaugeColours(t *testing.T) {
	for level, want := range map[string]string{LevelOK: "is-success", LevelWarn: "is-warning", LevelFail: "is-danger", "": "is-success"} {
		if got := (Gauge{Level: level}).Bar(); got != want {
			t.Errorf("a %q gauge is %s, want %s", level, got, want)
		}
	}
}

func TestHealthStatesTheFixtureDoesNotDraw(t *testing.T) {
	h := healthFixture(false)
	h.ProcessError = true
	h.ProcessRows = nil
	h.CertFacts, h.CertDetail, h.CertProblem = nil, "No certificate path is configured (TLS_CERT_FILE).", true
	h.SocketRows = []SocketRow{{Name: "OpenDKIM", Path: "/run/opendkim/opendkim.sock", State: Tag{Status: "error"},
		Detail: "OpenDKIM socket missing: mail cannot leave."}}
	h.SocketRows = append(h.SocketRows, SocketRow{Name: "rspamd", Note: "inbound spam filter", Path: "inet:antispam:11332", State: Tag{Status: "ok"}})
	h.HostFacts = []Fact{{Label: "Hostname", Value: Plain("(SELFPOST_HOSTNAME is not set)"), Mono: true}}
	h.Flash = &Flash{Text: Plain("DNS re-checked.")}
	out := renderSignedIn(t, "health", h)
	pageHas(t, "Health", out,
		`Could not ask supervisord for the process list.`,
		`No certificate path is configured`,
		`<span class="tag is-danger is-light">error</span>`, `OpenDKIM socket missing: mail cannot leave.`,
		`SELFPOST_HOSTNAME is not set`, `rspamd <span class="sp-muted">· inbound spam filter</span>`,
		`<div class="notification is-success is-light">`, `DNS re-checked.`,
	)
	// the flash sits between the head and the first row, as the kit puts it
	if strings.Index(out, `class="notification`) > strings.Index(out, `id="health-machine"`) ||
		strings.Index(out, `class="notification`) < strings.Index(out, `class="sp-head"`) {
		t.Error("the flash is not between page_head and the first box")
	}
}

func TestNewHealthFlash(t *testing.T) {
	if h := NewHealth(admin(), ""); h.Flash != nil {
		t.Error("a flash without a message")
	}
	h := NewHealth(admin(), "DNS re-checked.")
	if h.Flash == nil || h.Flash.Error {
		t.Errorf("flash = %+v", h.Flash)
	}
}

// ---- Account

func TestAccountHasTwoFormsEachItsOwnPost(t *testing.T) {
	out := renderSignedIn(t, "account", accountFixture())
	pageHas(t, "Account", out,
		`<h1 class="title is-3">Account</h1>`, `Signed in as admin · global`,
		`action="/account/profile"`, `name="username"`, `name="email"`, `value="mix@example.org"`,
		`action="/account/password"`, `name="current_password"`, `name="new_password"`, `name="new_password_confirm"`,
		`Your own address. A domain's report address can be filled with it from Domain settings.`,
	)
	// Account is Profile and Password; where a domain's reports go is the
	// domain's own setting.
	pageLacks(t, "Account", out, `send_log_retention_days`, `/account/dmarc`, `dmarc_default`, `<h2>DMARC reports</h2>`,
		`Report authorization`, `default report address`, `id="dmarc"`)
	// No form asks for another's fields: two forms, two submit buttons.
	if got := strings.Count(out, `<form class="sp-form" method="post"`); got != 2 {
		t.Errorf("Account has %d forms, want 2", got)
	}
	// Two equal columns, one box each.
	if got := strings.Count(out, `<div class="column is-6">`); got != 2 {
		t.Errorf("Account has %d equal columns, want 2", got)
	}
}

func TestAccountResultAndRefusalAreTheFlash(t *testing.T) {
	out := renderSignedIn(t, "account", accountFixture().WithResult("Profile saved.", ""))
	pageHas(t, "Account", out, `<div class="notification is-success is-light">`, `Profile saved.`)

	out = renderSignedIn(t, "account", accountFixture().WithResult("", `Current password is <b>incorrect</b>.`))
	pageHas(t, "Account", out, `<div class="notification is-danger is-light">`, `Current password is &lt;b&gt;incorrect&lt;/b&gt;.`)
	pageLacks(t, "Account", out, `<b>incorrect</b>`)
}

// What a person typed comes back escaped when a form is shown again.
func TestAccountEscapesTheFormValues(t *testing.T) {
	a := accountFixture()
	a.Username, a.Email = `"><script>1</script>`, `a"onfocus="x`
	out := renderSignedIn(t, "account", a)
	pageLacks(t, "Account", out, `<script>1`, `"onfocus="x`)
}

// Event notifications are not built (roadmap: panel-notifications), and a page
// carries no box for a feature that does not exist.
func TestAccountAndSettingsHaveNoNotificationsBox(t *testing.T) {
	for name, out := range map[string]string{
		"Account":  renderSignedIn(t, "account", accountFixture()),
		"Settings": renderSignedIn(t, "settings", settingsFixture()),
	} {
		pageLacks(t, name, out, `<h2>Notifications</h2>`, `type="checkbox"`, `/account/notifications`, `name="notify"`)
	}
}

// ---- Settings

func TestSettingsIsOneFormWithOneSaveButton(t *testing.T) {
	out := renderSignedIn(t, "settings", settingsFixture())
	if got := strings.Count(out, `<form class="sp-form"`); got != 1 {
		t.Errorf("Settings has %d forms, want 1", got)
	}
	pageHas(t, "Settings", out, `Save settings`, `<h1 class="title is-3">Settings</h1>`, `href="/account"`)
}

func TestSettingsShowsTheRefusalAndKeepsWhatWasTyped(t *testing.T) {
	s := NewSettings(admin(), "3", FormatRate(600, 3600)).WithResult("", "Send log retention must be between 7 and 365.")
	out := renderSignedIn(t, "settings", s)
	pageHas(t, "Settings", out, `<div class="notification is-danger is-light">`, `between 7 and 365`, `value="3"`)

	out = renderSignedIn(t, "settings", settingsFixture().WithResult("Settings saved.", ""))
	pageHas(t, "Settings", out, `<div class="notification is-success is-light">`, `Settings saved.`)
}

func TestFormatRate(t *testing.T) {
	for _, c := range []struct {
		messages, window int
		want             string
	}{
		{600, 3600, "600 / h"}, {30, 60, "30 / min"}, {5000, 86400, "5 000 / day"},
		{100, 7200, "100 / 2 h"}, {10, 90, "10 / 90 s"},
	} {
		if got := FormatRate(c.messages, c.window); got != c.want {
			t.Errorf("FormatRate(%d, %d) = %q, want %q", c.messages, c.window, got, c.want)
		}
	}
}

// ---- Overview polling

// ids lists every id="…" of a piece of HTML.
func ids(out string) []string {
	var all []string
	for _, m := range regexp.MustCompile(`\sid="([^"]+)"`).FindAllStringSubmatch(out, -1) {
		all = append(all, m[1])
	}
	return all
}

func unique(t *testing.T, where string, all []string) {
	t.Helper()
	seen := map[string]bool{}
	for _, id := range all {
		if seen[id] {
			t.Errorf("%s: id %q appears twice", where, id)
		}
		seen[id] = true
	}
}

// The Server health box is the element that polls, and the page never carries
// the out-of-band mark: that belongs to the fragment.
func TestOverviewPageHasThePollAttributesAndNoOOBMark(t *testing.T) {
	out := renderSignedIn(t, "overview", overviewFixture())
	pageHas(t, "Overview", out,
		`<div class="box" id="health" data-poll aria-live="polite" hx-get="/overview/fragment" hx-trigger="load" hx-swap="outerHTML">`,
		`<div class="sp-head" id="overview-head">`)
	pageLacks(t, "Overview", out, `hx-swap-oob`)
	unique(t, "Overview", ids(out))
}

// The fragment is the box (the element that asked) and the head, out of band —
// nothing of the document around them — and answering a poll with it leaves the
// page with each id once: the box replaces the box, the head replaces the head.
func TestOverviewFragmentIsTheBoxAndTheHeadOutOfBand(t *testing.T) {
	frag := renderFragment(t, "overview_poll", overviewFixture())
	pageHas(t, "fragment", frag,
		`<div class="sp-head" id="overview-head" hx-swap-oob="true">`, `class="sp-postmark sp-warn"`, `Updated 14:02:31 UTC`,
		`<div class="box" id="health" data-poll aria-live="polite" hx-get="/overview/fragment" hx-trigger="load" hx-swap="outerHTML">`,
		`Expires in 12 days`)
	pageLacks(t, "fragment", frag, `<html`, `<body`, `navbar`, `<table`, `Outbound domains`)
	unique(t, "fragment", ids(frag))
	if got := strings.Count(frag, `hx-swap-oob`); got != 1 {
		t.Errorf("%d out-of-band marks in the fragment, want the head's alone", got)
	}

	// The box is the same markup the page holds.
	page := renderSignedIn(t, "overview", overviewFixture())
	boxHTML := strings.TrimSpace(frag[strings.Index(frag, `<div class="box" id="health"`):])
	if !strings.Contains(page, boxHTML) {
		t.Error("the fragment's box is not the page's box")
	}
	// What a poll leaves behind: the page with its box and head replaced by the
	// fragment's. Each id is still there once, and the oob mark is gone with the swap.
	pageIDs, fragIDs := ids(page), ids(frag)
	for _, id := range fragIDs {
		n := 0
		for _, p := range pageIDs {
			if p == id {
				n++
			}
		}
		if n != 1 {
			t.Errorf("the fragment's id %q is in the page %d times, so a swap would not replace exactly one element", id, n)
		}
	}
}
