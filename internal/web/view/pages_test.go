package view

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
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
		`Needs attention: <a href="/server/health">TLS certificate</a> (Expires in 12 days).`,
		`Updated 14:02:31<br>refreshes on its own`, `<p class="sp-kicker">mail.example.org</p>`,
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
		`id="machine"`, `id="processes"`, `id="certificate"`, `id="sockets"`, `id="hostname"`, `id="notification-mail"`, `id="configuration"`,
		// the machine card: bars carry their reading in an attribute (the CSP rules out sizing them with a style)
		`<progress class="progress is-small is-success mb-1" value="12" max="100">`, `value="41"`,
		`↓ 18.0 KiB/s`, `eth0: 2.1 GiB in, 6.4 GiB out`,
		`pid 41, uptime 18 days`, `<span class="tag is-success is-light">running</span>`,
		`2026-10-21 09:14`, `class="mt-4 has-text-danger"`, `Check that renewal on the host still works.`,
		`dkim-signer <span class="sp-muted">· notifications</span>`, `unix:/run/selfpost/sign-local.sock`, `→ 203.0.113.25<br>← mail.example.org`,
		`hx-get="/server/health/fragment"`,
	)
	pageLacks(t, "Health", out, `hx-swap-oob`, `/overview/fragment`)
	if got := strings.Count(out, `class="columns sp-cards"`); got != 1 {
		t.Errorf("Health has %d sets of cards, want 1", got)
	}
	// The boxes are numbered by importance and read in rows: the odd ones are
	// the left stack, the even ones the right.
	order := []string{`id="processes"`, `id="sender-check"`, `id="hostname"`, `id="machine"`,
		`id="sockets"`, `id="certificate"`, `id="notification-mail"`, `id="configuration"`}
	for i := 1; i < len(order); i++ {
		if !strings.Contains(out, order[i-1]) || strings.Index(out, order[i]) < strings.Index(out, order[i-1]) {
			t.Errorf("%s does not come after %s", order[i], order[i-1])
		}
	}
	for no, title := range []string{"Processes", "Milter sockets", "Sender check", "TLS certificate",
		"Hostname and reverse DNS", "Notification mail", "Machine", "Configuration"} {
		if want := `<span class="sp-no">0` + strconv.Itoa(no+1) + `</span><h2>` + title + `</h2>`; !strings.Contains(out, want) {
			t.Errorf("Health is missing %s", want)
		}
	}
	pageHas(t, "Health", out, `<tr><td>Last 12 months</td><td>≈ 41 s</td><td>3</td><td>1</td></tr>`,
		`Counted since <span class="sp-mono">2026-10-13</span>.`, `href="/help#sender-check"`)
	// The Configuration box is outside the polled stack: a refresh must not re-render a control.
	if strings.Contains(out[strings.Index(out, `id="health-stack"`):strings.Index(out, `id="sockets"`)], `action="/server/health/reload"`) {
		t.Error("the reload control is inside the polled stack")
	}
}

// The fragment is the polled part of the page and nothing else: the left
// stack as it is, and the three readings of the right stack, each marked to be
// swapped in by its id. No shell and no actions.
func TestHealthFragmentIsThePolledBoxes(t *testing.T) {
	page := renderSignedIn(t, "health", healthFixture(false))
	frag := renderFragment(t, "health_body", healthFixture(true))

	// The polled boxes of s: from the left stack to the Configuration box, or
	// to the end of a fragment. On the page the right stack's column opens
	// between them; the fragment has no such line.
	polled := func(s string) string {
		s = s[strings.Index(s, `<div class="column is-6" id="health-stack"`):]
		if b := strings.Index(s, `<div class="box" id="configuration"`); b > 0 {
			s = s[:b]
		}
		s = strings.ReplaceAll(s, ` hx-swap-oob="true"`, "")
		return strings.Join(strings.Fields(strings.Replace(s, `<div class="column is-6">`, "", 1)), " ")
	}
	if got, want := polled(frag), polled(page); got != want {
		t.Errorf("the fragment is not the page's polled boxes\nfragment: %.600s\npage:     %.600s", got, want)
	}
	pageHas(t, "fragment", frag, `id="health-stack" data-poll`, `id="sockets" hx-swap-oob="true"`,
		`id="certificate" hx-swap-oob="true"`, `id="notification-mail" hx-swap-oob="true"`, `id="sender-check"`)
	pageLacks(t, "fragment", frag, `<h1`, `navbar`, `action="/server/health/reload"`, `action="/server/health/recheck"`)
}

// The Notification mail card says which record is in place and which is not,
// with the host each is published at; a missing record is a warning, since
// notifications are sent without it. The way to the values is a link.
func TestHealthNotificationMailCard(t *testing.T) {
	out := renderSignedIn(t, "health", healthFixture(false))
	pageHas(t, "Health", out,
		`<h2>Notification mail</h2><div class="sp-end"><span class="tag is-warning is-light">warn</span></div>`,
		`href="/help#notifications"`,
		`DKIM<br><span class="sp-muted sp-mono sp-small">sp2026._domainkey.mail.example.org</span></td><td><span class="tag is-success is-light">published</span>`,
		`SPF<br><span class="sp-muted sp-mono sp-small">mail.example.org</span></td><td><span class="tag is-warning is-light">missing</span>`,
		`Sent from <span class="sp-mono">postmaster@mail.example.org</span>`,
		`href="/server/settings#notification-mail"`,
	)
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
	h.SocketRows = []SocketRow{{Name: "dkim-signer", Note: "applications", Path: "/run/selfpost/sign.sock", State: Tag{Status: "error"},
		Detail: "dkim-signer socket missing: mail cannot leave."}}
	h.SocketRows = append(h.SocketRows, SocketRow{Name: "rspamd", Note: "inbound spam filter", Path: "inet:antispam:11332", State: Tag{Status: "ok"}})
	h.HostFacts = []Fact{{Label: "Hostname", Value: Plain("(SELFPOST_HOSTNAME is not set)"), Mono: true}}
	h.Flash = &Flash{Text: Plain("DNS re-checked.")}
	out := renderSignedIn(t, "health", h)
	pageHas(t, "Health", out,
		`Could not ask supervisord for the process list.`,
		`No certificate path is configured`,
		`<span class="tag is-danger is-light">error</span>`, `dkim-signer socket missing: mail cannot leave.`,
		`SELFPOST_HOSTNAME is not set`, `rspamd <span class="sp-muted">· inbound spam filter</span>`,
		`<div class="notification is-success is-light">`, `DNS re-checked.`,
	)
	// the flash sits between the head and the first row, as the kit puts it
	if strings.Index(out, `class="notification`) > strings.Index(out, `id="health-stack"`) ||
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
		`Your own address. Saving a new one sends it a link to confirm it; once confirmed it is where a link to reset your password can go. A domain's report address can be filled with it from Domain settings.`,
		`E-mail <span class="tag is-warning is-light">not confirmed</span>`,
		`<input class="input" id="profile_password" name="current_password" type="password" autocomplete="current-password">`,
		`action="/account/email/confirm"`, `A link was sent to <span class="sp-mono">mix@example.org</span> at 14:02; it works once, for 24 hours.`,
	)
	// Account is Profile and Password; where a domain's reports go is the
	// domain's own setting.
	pageLacks(t, "Account", out, `send_log_retention_days`, `/account/dmarc`, `dmarc_default`, `<h2>DMARC reports</h2>`,
		`Report authorization`, `default report address`, `id="dmarc"`)
	// No form asks for another's fields: the profile, the password and,
	// while the address is not confirmed, its link again.
	if got := strings.Count(out, `<form class="sp-form`); got != 3 {
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

// The checkbox under the e-mail is the user's own switch for notifications: it
// shows what is stored, and an unchecked box is the absence of the attribute.
func TestAccountShowsTheNotifyCheckbox(t *testing.T) {
	a := accountFixture()
	out := renderSignedIn(t, "account", a)
	pageHas(t, "Account", out, `<input type="checkbox" name="notify" checked> Send me notifications`, `href="/help#notifications"`)

	a.Notify = false
	out = renderSignedIn(t, "account", a)
	pageHas(t, "Account", out, `<input type="checkbox" name="notify"> Send me notifications`)
	pageLacks(t, "Account", out, `name="notify" checked`)
}

// ---- Settings

func TestSettingsIsOneFormWithOneSaveButton(t *testing.T) {
	out := renderSignedIn(t, "settings", settingsFixture())
	if got := strings.Count(out, `<form class="sp-form"`); got != 1 {
		t.Errorf("Settings has %d forms, want 1", got)
	}
	pageHas(t, "Settings", out, `Save settings`, `<h1 class="title is-3">Settings</h1>`, `href="/account"`)
}

// The time zone is shown, not set: its name and offset, and a TZ the image
// does not know is said to give UTC rather than named as the zone in use.
func TestSettingsShowsTheTimeZone(t *testing.T) {
	pageHas(t, "Settings", renderSignedIn(t, "settings", settingsFixture()),
		`<h2>Time zone</h2>`, `<dd class="sp-mono">Europe/Moscow</dd>`, `UTC&#43;03:00. Set in <code>.env</code> (<code>TZ</code>); restart to change.`,
		`and so are the stamps of <code>mail.log</code>.`)
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	pageHas(t, "Settings without TZ", renderSignedIn(t, "settings", NewSettings(admin(), "30", "600 / h").WithTimeZone("", time.UTC, now)),
		`<dd class="sp-mono">UTC</dd>`, `UTC&#43;00:00. Set in`)
	pageHas(t, "Settings with a wrong TZ", renderSignedIn(t, "settings", NewSettings(admin(), "30", "600 / h").WithTimeZone("Mars/Olympus", time.UTC, now)),
		`<dd class="sp-mono">Mars/Olympus</dd>`, `Not a zone this image knows, so the times are UTC.`)
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no zone America/New_York here: %v", err)
	}
	pageHas(t, "Settings in New York", renderSignedIn(t, "settings", NewSettings(admin(), "30", "600 / h").WithTimeZone("America/New_York", ny, now)),
		`UTC-04:00. Set in`)
}

// The panel shows times in the container's zone, with no zone written
// beside them (local-time): the row of a log, a message's page, the head
// of Overview.
func TestTimesAreTheContainersZone(t *testing.T) {
	inZone(t, "Europe/Moscow")
	at := time.Date(2026, 9, 21, 22, 30, 5, 0, time.UTC)
	if got := FormatLogTime(at); got != "09-22 01:30:05" {
		t.Errorf("FormatLogTime = %q, want the 22nd at 01:30:05 in Moscow", got)
	}
	if got := formatStamp(at); got != "2026-09-22 01:30:05" {
		t.Errorf("formatStamp = %q", got)
	}
	if got := FormatReceived(at); got != "09-22 01:30" {
		t.Errorf("FormatReceived = %q", got)
	}
}

// Notifications are one switch for the instance, saved with the rest of the
// page, and the records of the server's own name are drawn like a domain's.
func TestSettingsNotificationsSwitchAndRecords(t *testing.T) {
	s := settingsFixture()
	out := renderSignedIn(t, "settings", s)
	pageHas(t, "Settings", out,
		`<input type="checkbox" name="notify" checked> Send notification e-mails`,
		`From <span class="sp-mono">postmaster@mail.example.org</span>, to every user who has an e-mail and has not turned them off under <a href="/account">Account</a>.`,
		`id="notification-mail"`, `href="/help#notifications"`,
		// DKIM is published; SPF is not, and says what that costs.
		`<h3>DKIM</h3><span class="tag is-success is-light">published</span><small>Not a secret · selector <code>sp2026</code></small>`,
		`<h3>SPF</h3><span class="tag is-warning is-light">missing</span><small>For the server&#39;s own name, not for your domains</small>`,
		`value="sp2026._domainkey.mail.example.org"`, `value="v=spf1 a -all"`,
		`Notifications are sent without it, and a receiver may file them as spam.`,
	)

	// Switched off, and both records in place: the box is unchecked and the
	// warning under SPF is gone.
	s.Notify = false
	s.WithMail([]Record{
		ServerMailDKIM("sp2026", mailFixtureDKIMHost, "v=DKIM1; k=rsa; p=AAAA", DNSCheck{Status: "ok"}),
		ServerMailSPF("mail.example.org", "v=spf1 a -all", DNSCheck{Status: "ok"}),
	}, "")
	out = renderSignedIn(t, "settings", s)
	pageHas(t, "Settings", out, `<input type="checkbox" name="notify"> Send notification e-mails`,
		`<h3>SPF</h3><span class="tag is-success is-light">published</span>`,
		`<div class="sp-end">DNS records of the server&#39;s own name</div>`)
	pageLacks(t, "Settings", out, `name="notify" checked`, `a receiver may file them as spam`, `· checked`)
}

// The records of the server's own name can be checked again from Settings,
// where they are shown: Re-check DNS in the page head, outside the form, and
// the box says when they were checked.
func TestSettingsRechecksTheMailRecords(t *testing.T) {
	out := renderSignedIn(t, "settings", settingsFixture())
	pageHas(t, "Settings", out,
		`<div class="buttons"><form method="post" action="/server/settings/dns-recheck"><button type="submit" class="button"><i class="ti ti-refresh"></i>Re-check DNS</button></form></div>`,
		`<div class="sp-end">The server&#39;s own name · checked 3 min ago</div>`)
	form := out[strings.Index(out, `<form class="sp-form" method="post" action="/server/settings">`):]
	form = form[:strings.Index(form, "</form>")]
	for _, outside := range []string{"dns-recheck", `id="notification-mail"`, `id="rate-limits"`} {
		if strings.Contains(form, outside) {
			t.Errorf("the Settings form holds %s; only the fields and Save settings belong in it", outside)
		}
	}
	if !strings.Contains(form, `name="notify"`) || !strings.Contains(form, `Save settings`) {
		t.Errorf("the Settings form lost its switch or its button:\n%s", form)
	}
}

// A record of the server's own name that is wrong or missing is a warning, never
// the red of a sending domain's: notifications are sent without it.
func TestMailRecordsAreWarningsNotErrors(t *testing.T) {
	for _, c := range []struct {
		check DNSCheck
		want  Tag
	}{
		{DNSCheck{Status: "ok"}, Tag{Status: "published", Label: "published"}},
		{DNSCheck{Status: "error"}, Tag{Status: "warn", Label: "missing"}},
		{DNSCheck{Status: "error", Found: []string{"v=spf1 -all"}}, Tag{Status: "warn", Label: "mismatch"}},
		{DNSCheck{Status: "warn"}, Tag{Status: "warn", Label: "weak"}},
		{DNSCheck{Status: "unknown"}, Tag{Status: "unknown", Label: "not checked"}},
	} {
		if got := NewMailRow("SPF", "mail.example.org", c.check).State; got != c.want {
			t.Errorf("a record that checked as %+v is %+v, want %+v", c.check, got, c.want)
		}
	}
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
		`<div class="sp-head" id="overview-head" hx-swap-oob="true">`, `class="sp-postmark sp-warn"`, `Updated 14:02:31`,
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

// With the inbound relay on the retention box has a second field, for the Inbound
// log; with it off the field is not there, since there is no journal to keep.
func TestSettingsHasTheInboundRetentionOnlyWithTheRelay(t *testing.T) {
	out := renderSignedIn(t, "settings", settingsFixture())
	pageHas(t, "Settings", out,
		`for="inbound_spam_log_retention_days">Keep inbound log rows, days</label>`,
		`name="inbound_spam_log_retention_days" type="number" min="7" max="365" step="1" value="90" required`,
		`The inbound log also never holds more than 10 000 rows: refused mail is logged at the rate strangers knock on port 25.`,
		`name="send_log_retention_days"`)
	if got := strings.Count(out, `<form class="sp-form"`); got != 1 {
		t.Errorf("Settings has %d forms, want 1", got)
	}
	pageLacks(t, "Settings", renderSignedIn(t, "settings", NewSettings(admin(), "30", "600 / h")),
		`inbound_spam_log_retention_days`, `Keep inbound log rows`, `10 000 rows`)

	// A refused value comes back as typed.
	out = renderSignedIn(t, "settings", NewSettings(admin(), "30", "600 / h").WithInbound("3").WithResult("", "Inbound log retention must be between 7 and 365."))
	pageHas(t, "Settings", out, `value="3"`, `between 7 and 365`)
}
