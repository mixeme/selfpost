package view

import (
	"strings"
	"testing"
	"time"
)

// ---- Outbound domains

// The global role adds and deletes domains; a domain administrator sees the
// list alone, so the page has no add box, no Delete link, and numbers the table
// box 01.
func TestOutDomainsOffersAddAndDeleteToTheGlobalRoleOnly(t *testing.T) {
	out := renderSignedIn(t, "out-domains", outDomainsFixture())
	pageHas(t, "Outbound domains", out,
		`<h1 class="title is-3">Outbound domains</h1>`, `<h2>Add a domain</h2>`, `action="/outbound/domains"`, `name="name"`,
		`href="/server/backup#import"`, `<th>30 days</th>`, `>Delete</a>`, `href="/outbound/domains/2/delete"`,
		`3 domains · DNS cached a few minutes`, "1 284 msg · peak 96/h", `class="sp-muted sp-mono">sp2025<`,
		`<span class="tag is-danger is-light">SPF</span>`, `<span class="tag is-warning is-light">DMARC</span>`,
	)

	p := NewOutDomains(Meta{User: "ops", HasOutbound: true}, false, 30).WithRows([]OutDomainRow{
		{Name: "example.org", Href: "/outbound/domains/1", Selector: "sp2026", Apps: 1, Activity: "—",
			DNS: []Tag{{Status: "ok", Label: "DKIM"}}},
	})
	out = renderSignedIn(t, "out-domains", p)
	pageHas(t, "Outbound domains", out, `<span class="sp-no">01</span><h2>Domains</h2>`, `1 domain · DNS cached`)
	pageLacks(t, "Outbound domains", out, `Add a domain`, `name="name"`, `>Delete</a>`, `/delete`)
}

func TestOutDomainsEmptyAndRefused(t *testing.T) {
	p := NewOutDomains(admin(), true, 30).WithRows(nil).WithResult("", "That domain is already configured.", "example.org")
	out := renderSignedIn(t, "out-domains", p)
	pageHas(t, "Outbound domains", out, `No outbound domains yet. Add the first one above.`,
		`<div class="notification is-danger is-light">`, `That domain is already configured.`, `value="example.org"`, `0 domains`)
	pageLacks(t, "Outbound domains", out, `<table`)
	// the flash sits between the head and the first box
	if strings.Index(out, `class="notification`) < strings.Index(out, `class="sp-head"`) ||
		strings.Index(out, `class="notification`) > strings.Index(out, `class="box"`) {
		t.Error("the flash is not between page_head and the first box")
	}

	out = renderSignedIn(t, "out-domains", NewOutDomains(admin(), true, 30).WithResult("Domain deleted.", "", ""))
	pageHas(t, "Outbound domains", out, `<div class="notification is-success is-light">`, `Domain deleted.`)
}

func TestOutDomainsEscapesWhatPeopleTyped(t *testing.T) {
	p := NewOutDomains(admin(), true, 30).WithRows([]OutDomainRow{
		{Name: `<script>alert(1)</script>`, Href: "/outbound/domains/1", Selector: `"><img src=x>`},
	}).WithResult("", "bad", `"><i>`)
	out := renderSignedIn(t, "out-domains", p)
	pageLacks(t, "Outbound domains", out, `<script>alert(1)`, `<img src=x>`, `<i>`)
}

// ---- One outbound domain

// The stamp, the box head and the menu say the same thing about the records:
// the worst one colours the stamp, the number counts those that are not fine.
func TestOutDomainStampFollowsTheRecords(t *testing.T) {
	out := renderSignedIn(t, "out-domain", outDomainFixture())
	pageHas(t, "Domain", out,
		`<h1 class="title is-3">example.org</h1>`, `class="sp-postmark sp-fail"`, `<b>FAIL</b>`, `1 OF 3 RECORDS`,
		`<a href="/outbound/domains">Outbound domains</a> /`,
		"1 284 messages in 30 days · peak 96 msg/h · average 1.8 msg/h",
		`Checked 3 min ago`, `<span class="tag is-danger is-light">1</span>`, // the menu's count
		`action="/outbound/domains/1/dns-recheck"`, `href="/outbound/domains/1/applications/new"`,
		`<span class="tag is-success is-light">auto</span>`,
	)

	ok := func(status string) Record { return Record{Name: "X", Status: Tag{Status: status}} }
	for _, c := range []struct {
		records           []Record
		level, word, over string
		menu              bool
	}{
		{[]Record{ok("published"), ok("published"), ok("published")}, LevelOK, "OK", "3 OF 3 RECORDS", false},
		{[]Record{ok("published"), ok("warn"), ok("unknown")}, LevelWarn, "WARN", "2 OF 3 RECORDS", true},
		{[]Record{ok("warn"), ok("mismatch"), ok("fail"), ok("published")}, LevelFail, "FAIL", "3 OF 4 RECORDS", true},
	} {
		p := outDomainFixture().WithRecords(c.records, "")
		if m := p.Head.Postmark; m == nil || m.Level != c.level || m.Word != c.word || m.Bottom != c.over || m.Top != "DNS" {
			t.Errorf("records %v are stamped %+v, want %s %s", c.records, m, c.word, c.over)
		}
		if got := p.dnsCount() != nil; got != c.menu {
			t.Errorf("the menu count for %v is shown: %v, want %v", c.records, got, c.menu)
		}
	}
	// No time to report: the box says nothing about when it was checked.
	if p := NewOutDomain(admin(), 1, "example.org", true).WithRecords(nil, ""); p.DNS.End != nil || p.Head.Postmark != nil {
		t.Error("a domain without records has a stamp or a check time")
	}
}

func TestOutDomainRecordsSayWhatDNSHolds(t *testing.T) {
	out := renderSignedIn(t, "out-domain", outDomainFixture())
	pageHas(t, "Domain", out,
		`<h3>DKIM</h3>`, `<span class="tag is-success is-light">published</span>`, `selector <code>sp2026</code>`,
		`sp2026._domainkey.example.org`, `<textarea class="textarea" rows="3" readonly>`,
		`<h3>SPF</h3>`, `<span class="tag is-danger is-light">mismatch</span>`, `v=spf1 ip4:203.0.113.25 ~all`,
		`In DNS now`, `v=spf1 include:_spf.google.com ~all`, `Shallow check: <code>include:</code> is not followed.`,
		`<h3>DMARC</h3>`, `Report address: <a href="/outbound/domains/1/settings#reports">example.org@dmarc.mail.example.org</a>`,
		`<code>p=none</code> does not affect delivery.`, `<a href="/outbound/dmarc/domains/1">reports</a>`,
	)
	if got := strings.Count(out, `In DNS now`); got != 1 {
		t.Errorf("%d records show what DNS holds, want the mismatching one alone", got)
	}
	if got := strings.Count(out, `data-copy`); got != 6 {
		t.Errorf("%d copy buttons, want a host and a value for each of the three records", got)
	}

	// A record DNS does not hold has nothing to compare: the explanation goes
	// under the value to publish. A weak one is amber.
	records := []Record{
		DKIMRecord("sp2026", "sp2026._domainkey.example.org", "v=DKIM1; p=KEY", DNSCheck{Status: "error", Detail: "No DKIM record was found."}),
		SPFRecord("example.org", "v=spf1 -all", DNSCheck{Status: "warn", Detail: "The record ends in ~all.", Found: []string{"v=spf1 ~all"}}),
		DMARCRecord(DMARCRecordInput{Host: "_dmarc.example.org", Value: "v=DMARC1; p=none", Check: DNSCheck{Status: "unknown", Detail: "DNS could not be reached."},
			Source: "no reports", SettingsHref: "/outbound/domains/1/settings", SameDomain: true}),
		ReportAuthRecord("_report._dmarc.hub.example", "v=DMARC1;", DNSCheck{Status: "error", Detail: "Not authorized."}),
	}
	out = renderSignedIn(t, "out-domain", outDomainFixture().WithRecords(records, "just now"))
	pageHas(t, "Domain", out,
		`<span class="tag is-danger is-light">missing</span>`, `No DKIM record was found.`,
		`<span class="tag is-warning is-light">weak</span>`, `The record ends in ~all.`, `v=spf1 ~all`,
		`<span class="tag is-light">not checked</span>`, `DNS could not be reached.`, `where SelfPost receives no mail`,
		`<h3>Report authorization</h3>`, `_report._dmarc.hub.example`, `Needed because the address is on another domain than the senders`,
		`4 OF 4 RECORDS`,
	)
	pageLacks(t, "Domain", out, `<a href="/outbound/dmarc/domains/1">reports</a>`) // no page of reports was given
}

func TestOutDomainApplicationsTable(t *testing.T) {
	out := renderSignedIn(t, "out-domain", outDomainFixture())
	pageHas(t, "Domain", out,
		`<th>Login</th><th>May send as</th><th>30 days</th><th>Limits</th>`,
		`<td class="sp-mono sp-nowrap"><strong>prod-server</strong></td>`, `*@<wbr>example.org`,
		`alerts@<wbr>example.org, noc@<wbr>example.org`, "1 102 msg · peak 96/h",
		`<span class="tag is-success is-light">200 / h</span>`, `<span class="tag is-light">domain</span> <span class="tag is-success is-light">2 IPs</span>`,
		`<a href="/outbound/domains/1/applications/2">Edit</a>`,
	)
	// A state change is never a link: New password and Delete are POST forms in
	// the row, each with the question panel.js asks first, drawn like the Edit
	// link beside them. Edit leads on to the application's page.
	pageHas(t, "Domain", out,
		`<td class="sp-actions"><a href="/outbound/domains/1/applications/1">Edit</a>`+
			`<form method="post" action="/outbound/domains/1/applications/1/password" data-confirm="Generate a new password for prod-server? The current password stops working immediately."><button type="submit">New password</button></form>`+
			`<form method="post" action="/outbound/domains/1/applications/1/delete" data-confirm="Delete application prod-server? Its credentials stop working immediately."><button type="submit" class="has-text-danger">Delete</button></form></td>`,
		`action="/outbound/domains/1/applications/2/password" data-confirm="Generate a new password for alerts?`,
		`action="/outbound/domains/1/applications/2/delete" data-confirm="Delete application alerts?`,
		`New password and Delete ask for confirmation; the old password stops working at once.`,
	)

	p := outDomainFixture().WithApplications(nil)
	out = renderSignedIn(t, "out-domain", p)
	pageHas(t, "Domain", out, `No applications yet.`)
	if strings.Contains(out[strings.Index(out, `id="apps"`):], `<table`) {
		t.Error("an empty Applications box draws a table")
	}
	if p.Menu().Groups[0].Items[1].Tag != nil {
		t.Error("the menu counts no applications")
	}
	if tag := outDomainFixture().Menu().Groups[0].Items[1].Tag; tag == nil || tag.Label != "2" {
		t.Errorf("the menu's count of applications = %v, want 2", tag)
	}
}

func TestOutDomainConnection(t *testing.T) {
	out := renderSignedIn(t, "out-domain", outDomainFixture())
	pageHas(t, "Domain", out, `<h2>Connection</h2>`, `mail.example.org`, `SSL/TLS (implicit)`, `STARTTLS (submission)`,
		`Application login, required on every port`)

	out = renderSignedIn(t, "out-domain", outDomainFixture().WithConnection("", false))
	pageHas(t, "Domain", out, `SELFPOST_HOSTNAME is not set`)
	pageLacks(t, "Domain", out, `STARTTLS`, `Port 587`)
}

// The side menu leads to the boxes, to the pages about the same domain and to
// the settings; the entries the viewer cannot use are not there.
func TestOutDomainMenuFollowsTheViewer(t *testing.T) {
	out := renderSignedIn(t, "out-domain", outDomainFixture())
	pageHas(t, "Domain", out,
		`href="#dns"`, `href="#apps"`, `href="#connection"`, `href="/outbound/log?domain=example.org"`, `href="/outbound/dmarc/domains/1"`,
		`href="/outbound/domains/1/settings#reports"`, `href="/outbound/domains/1/settings#limit"`, `href="/outbound/domains/1/settings#export"`,
		`href="/outbound/domains/1/delete"`, `id="dns"`, `id="apps"`, `id="connection"`,
		`class="sp-help" href="/help#dns"`, `class="sp-help" href="/help#apps"`,
	)

	p := NewOutDomain(Meta{User: "ops", HasOutbound: true}, 1, "example.org", false).WithRecords(nil, "")
	menu := p.Menu()
	if len(menu.Groups) != 2 {
		t.Fatalf("the menu has %d groups with nothing to see also, want 2", len(menu.Groups))
	}
	for _, it := range menu.Groups[1].Items {
		if it.Danger {
			t.Error("a domain administrator is offered Delete domain")
		}
	}
	if it := menu.Groups[1].Items[1]; it.Tag != nil {
		t.Errorf("a domain without a limit of its own has the tag %v", it.Tag)
	}
	if tag := outDomainFixture().WithRateLimit(false, true).Menu().Groups[2].Items[1].Tag; tag == nil || tag.Label != "manual" {
		t.Errorf("a manual limit's menu tag = %v", tag)
	}
}

func TestOutDomainResultIsTheFlash(t *testing.T) {
	out := renderSignedIn(t, "out-domain", outDomainFixture().WithResult("DNS re-checked."))
	pageHas(t, "Domain", out, `<div class="notification is-success is-light">`, `DNS re-checked.`)
	pageLacks(t, "Domain", renderSignedIn(t, "out-domain", outDomainFixture()), `notification`)
}

// ---- Domain settings

func TestOutDomainSettingsHasAFormPerBox(t *testing.T) {
	out := renderSignedIn(t, "out-domain-settings", outDomainSettingsFixture())
	pageHas(t, "Settings", out,
		`<h1 class="title is-3">Domain settings</h1>`,
		`<a href="/outbound/domains">Outbound domains</a> /`, `<a href="/outbound/domains/1">example.org</a> /`,
		`id="reports"`, `id="limit"`, `id="export"`,
		`action="/outbound/domains/1/settings/reports"`, `name="dmarc_rua_mode"`, `data-custom-mode="custom"`, `data-custom-address`,
		`name="dmarc_rua_email"`, `value="reports@acme.io"`, `<option value="none">No reports</option>`,
		`<option value="hosted">SelfPost hosted — example.org@dmarc.mail.example.org</option>`,
		`<option value="custom" selected>A specific address</option>`,
		`<button type="button" class="button" data-fill="rua_email" data-fill-value="mix@example.org"><i class="ti ti-mail"></i>Use my e-mail</button>`,
		`SelfPost hosted sends them to an address on this server`, `Changing this changes the DMARC record to publish.`,
		`action="/outbound/domains/1/settings/export"`, `name="encrypt" value="1" checked data-encrypt-toggle`, `data-encrypt-fields`,
		`name="password"`, `name="password_confirm"`, `minlength="12"`, `at least 12 characters`,
		`action="/outbound/domains/1/settings/ratelimit"`, `name="mode"`, `data-ratelimit-mode`, `data-manual-fields`, `data-auto-fields`,
		`name="max_messages"`, `max="600"`, `name="window_seconds"`, `name="auto_multiplier"`, `min="1.5" max="5"`, `Multiplier (1.5–5, default 2.5)`,
		`Computed limit: <strong>288</strong> messages / 3600 s. Peak was 96 msg/h. Last recalculated 2026-10-09 03:00 UTC.`,
		`<option value="auto" selected>`, `<span class="tag is-success is-light">active</span>`, `class="sp-help" href="/help#limits"`,
		`600 messages / 3600 s per client IP, set in <code>.env</code>`,
		`action="/outbound/domains/1/settings/ratelimit/recalc"`, `name="clear" value="1"`, `data-confirm="Remove the domain rate limit?`,
		`<h2>Delete domain</h2>`, `Removes the DKIM key and both applications.`, `href="/outbound/domains/1/delete"`, `Delete example.org…`,
	)
	// A domain's limit is for every sender: it never asks for client IPs.
	pageLacks(t, "Settings", out, `name="auth_allowed_ips"`, `auth_ip_restrict`)
	// The rate-limit form's Save button is outside the form and points at it.
	pageHas(t, "Settings", out, `<form class="sp-form mb-3" id="rate-limit"`, `form="rate-limit">Save limit`)
}

func TestOutDomainSettingsStatesTheFixtureDoesNotDraw(t *testing.T) {
	// A manual limit: nothing to recalculate, and the mode says manual.
	p := outDomainSettingsFixture().WithRateLimit(DomainRateLimit{
		Active: true, MaxMessages: "100", Window: "3600", Multiplier: "2.5",
		L1Messages: 600, L1Window: 3600, MinMultiplier: "1.5", MaxMultiplier: "5", DefaultMultiplier: "2.5",
	})
	out := renderSignedIn(t, "out-domain-settings", p)
	pageHas(t, "Settings", out, `<option value="manual" selected>`, `value="100"`, `name="clear"`)
	pageLacks(t, "Settings", out, `/ratelimit/recalc`)

	// No limit of its own: inactive, nothing to remove, nothing to recalculate.
	p = outDomainSettingsFixture().WithRateLimit(DomainRateLimit{
		Window: "3600", L1Messages: 600, L1Window: 3600, MinMultiplier: "1.5", MaxMultiplier: "5", DefaultMultiplier: "2.5",
	})
	out = renderSignedIn(t, "out-domain-settings", p)
	pageHas(t, "Settings", out, `<span class="tag is-light">inactive</span>`, `placeholder="600"`)
	pageLacks(t, "Settings", out, `name="clear"`, `/ratelimit/recalc`)

	// An automatic limit with nothing computed yet says why.
	p = outDomainSettingsFixture().WithRateLimit(DomainRateLimit{
		Active: false, Auto: true, L1Messages: 600, L1Window: 3600, MinMultiplier: "1.5", MaxMultiplier: "5", DefaultMultiplier: "2.5",
	})
	pageHas(t, "Settings", renderSignedIn(t, "out-domain-settings", p), `No limit is computed yet: zero traffic keeps auto inactive`)

	// A domain administrator cannot delete the domain: no Delete box.
	p = NewOutDomainSettings(Meta{User: "ops", HasOutbound: true}, 1, "example.org", 1, false).
		WithExport(12).WithRateLimit(DomainRateLimit{L1Messages: 600, L1Window: 3600})
	out = renderSignedIn(t, "out-domain-settings", p)
	pageHas(t, "Settings", out, `<h2>Rate limit</h2>`, `<h2>Export domain</h2>`)
	pageLacks(t, "Settings", out, `Delete domain`, `/delete`)

	// A refusal comes back as the flash, with the custom address that was typed.
	p = outDomainSettingsFixture().WithReportAddress([]Option{{Value: "custom", Label: "A specific address"}}, "custom", `x"><i>`, "").
		WithResult("", "Enter a valid e-mail address.")
	out = renderSignedIn(t, "out-domain-settings", p)
	pageHas(t, "Settings", out, `<div class="notification is-danger is-light">`, `Enter a valid e-mail address.`, `<option value="custom" selected>`)
	pageLacks(t, "Settings", out, `<i>`)
	// Without a profile e-mail there is nothing to fill with: no button. Without
	// hosted reports on the server the sentence about them is not there either.
	pageLacks(t, "Settings", out, `data-fill`, `Use my e-mail`, `SelfPost hosted`)
}

// The fill button carries the viewer's profile e-mail escaped, and only when
// there is one.
func TestReportAddressFillButton(t *testing.T) {
	opts := []Option{{Value: ReportNone, Label: "No reports"}, {Value: ReportCustom, Label: "A specific address"}}
	out := renderSignedIn(t, "out-domain-settings", outDomainSettingsFixture().WithReportAddress(opts, ReportNone, "", `a"><i>@x.example`))
	pageHas(t, "Settings", out, `data-fill="rua_email" data-fill-value="a&#34;&gt;&lt;i&gt;@x.example"`)
	pageLacks(t, "Settings", out, `<i>@`)
	out = renderSignedIn(t, "out-domain-settings", outDomainSettingsFixture().WithReportAddress(opts, ReportNone, "", ""))
	pageLacks(t, "Settings", out, `data-fill`, `Use my e-mail`)
}

func TestDeleteBoxNamesTheApplicationsItTakes(t *testing.T) {
	for n, want := range map[int]string{
		0: "Removes the DKIM key.", 1: "Removes the DKIM key and its application.",
		2: "Removes the DKIM key and both applications.", 5: "Removes the DKIM key and all 5 applications.",
	} {
		p := NewOutDomainSettings(admin(), 1, "example.org", n, true)
		if got := p.DeleteText[0].Text; got != want+" Asks once more before doing it." {
			t.Errorf("%d applications: %q", n, got)
		}
	}
}

// ---- Application form

func TestOutAppFormIsOneForm(t *testing.T) {
	out := renderSignedIn(t, "out-app", outAppFixture())
	pageHas(t, "Application", out,
		`<h1 class="title is-3 sp-mono">prod-server</h1>`, `<p class="sp-kicker"><a href="/outbound/domains">Outbound domains</a> / <a href="/outbound/domains/1">example.org</a> / Applications /</p>`,
		"1 102 messages in 30 days · peak 96 msg/h · average 1.5 msg/h",
		// the head's two buttons: both are POSTs, and each asks first
		`action="/outbound/domains/1/applications/1/password" data-confirm="Generate a new password for prod-server?`,
		`action="/outbound/domains/1/applications/1/delete" data-confirm="Delete application prod-server?`,
		// one form, one POST, the fields of all three parts
		`<form class="sp-form" method="post" action="/outbound/domains/1/applications/1">`,
		`name="login" value="prod-server" readonly`, `name="mode" data-list-mode="list"`, `<option value="wildcard" selected>`,
		`data-addresses`, `name="addresses"`, `placeholder="alerts@example.org"`,
		`name="auth_ip_restrict" value="1"`, `> Restrict to listed IPs`, `name="auth_allowed_ips"`, `<span class="tag is-light">off</span>`,
		`name="rl_mode" data-ratelimit-mode`, `<option value="manual" selected>`, `name="max_messages"`, `value="200"`, `max="600"`,
		`name="window_seconds"`, `value="3600"`, `data-auto-fields`, `name="auto_multiplier"`, `Multiplier (1.5–5, default 2.5)`,
		`Overrides the domain limit (288 / h) for this application`, `<span class="tag is-success is-light">active</span>`,
		`Save application`, `href="/outbound/domains/1">Cancel</a>`,
	)
	if got := strings.Count(out, `<form class="sp-form"`); got != 1 {
		t.Errorf("the page has %d forms of its own, want the one", got)
	}
	if got := strings.Count(out, `type="submit" class="button is-primary"`); got != 1 {
		t.Errorf("%d primary buttons, want the one that saves the whole form", got)
	}
	pageLacks(t, "Application", out, `/ratelimit/recalc`, `Recalculate`, `checked`)
}

func TestOutAppNewFormAddsAnApplication(t *testing.T) {
	p := NewOutApp(admin(), 1, "example.org", "").WithForm(OutAppForm{
		Mode: AddressWildcard, LimitMode: LimitDomain, Window: "3600", Multiplier: "2.5",
	}, OutAppState{}, AppRateLimit{L1Messages: 600, L1Window: 3600, MinMultiplier: "1.5", MaxMultiplier: "5", DefaultMultiplier: "2.5"})
	out := renderSignedIn(t, "out-app", p)
	pageHas(t, "New application", out,
		`<h1 class="title is-3">New application</h1>`, `action="/outbound/domains/1/applications/new"`, `Create application`,
		`name="login" value=""`, `Unique across domains; letters, digits, <code>.</code> <code>-</code> <code>_</code>.`,
		`A password is generated when it is created and shown once.`, `<option value="domain" selected>`,
		`<span class="tag is-light">inactive</span>`, `placeholder="alerts@example.org"`,
	)
	pageLacks(t, "New application", out, ` readonly`, `data-confirm`, `/password`)
	if p.Head.Actions != nil {
		t.Error("the form that adds an application has buttons for one that does not exist")
	}
}

// What was posted comes back with the refusal; what the page says is stored
// stays what is stored.
func TestOutAppFormShowsTheRefusalWithWhatWasTyped(t *testing.T) {
	typed := OutAppForm{
		Login: `x"><b>`, Mode: AddressList, Addresses: "a@example.org\nb@<i>", IPRestrict: true, AllowedIPs: "203.0.113.10\n<script>",
		LimitMode: LimitAuto, MaxMessages: "", Window: "3600", Multiplier: "9",
	}
	p := NewOutApp(admin(), 1, "example.org", "").WithForm(typed, OutAppState{},
		AppRateLimit{L1Messages: 600, L1Window: 3600, MinMultiplier: "1.5", MaxMultiplier: "5", DefaultMultiplier: "2.5"}).
		WithResult("", "multiplier must be between 1.5 and 5.0")
	out := renderSignedIn(t, "out-app", p)
	pageHas(t, "New application", out, `<div class="notification is-danger is-light">`, `multiplier must be between 1.5 and 5.0`,
		`<option value="list" selected>`, `<option value="auto" selected>`, `auth_ip_restrict" value="1" checked`, `value="9"`)
	pageLacks(t, "New application", out, `<b>`, `<i>`, `<script>`)
	if strings.Index(out, `class="notification`) > strings.Index(out, `<form class="sp-form"`) {
		t.Error("the refusal is not above the form")
	}
}

// An application whose limit is automatic can be recalculated; the button is in
// the submit row, after the one that saves, so Enter in a field saves.
func TestOutAppAutoLimitHasRecalculate(t *testing.T) {
	p := outAppFixture().WithForm(OutAppForm{Login: "prod-server", Mode: AddressWildcard, LimitMode: LimitAuto, Window: "3600", Multiplier: "2.5"},
		OutAppState{Limit: true, Auto: true},
		AppRateLimit{L1Messages: 600, L1Window: 3600, MinMultiplier: "1.5", MaxMultiplier: "5", DefaultMultiplier: "2.5",
			Computed: "288", Peak: 96, Updated: "2026-10-09 03:00 UTC"})
	out := renderSignedIn(t, "out-app", p)
	pageHas(t, "Application", out, `formaction="/outbound/domains/1/applications/1/ratelimit/recalc"`, `Recalculate rate limit`,
		`Computed limit: <strong>288</strong> messages / 3600 s.`)
	if strings.Index(out, `Save application`) > strings.Index(out, `Recalculate rate limit`) {
		t.Error("Recalculate comes before Save, so Enter in a field would recalculate")
	}
	// Chosen but not saved: there is nothing to recalculate yet.
	p = outAppFixture().WithForm(OutAppForm{Login: "prod-server", Mode: AddressWildcard, LimitMode: LimitAuto},
		OutAppState{}, AppRateLimit{L1Messages: 600})
	pageLacks(t, "Application", renderSignedIn(t, "out-app", p), `Recalculate`)
}

func TestOutAppFallsBackToLevelOneWhenTheDomainHasNoLimit(t *testing.T) {
	p := outAppFixture().WithForm(OutAppForm{Login: "prod-server"}, OutAppState{}, AppRateLimit{L1Messages: 600})
	pageHas(t, "Application", renderSignedIn(t, "out-app", p), `Overrides the domain limit, if it has one, or level 1 for this application`)
}

// ---- The password, shown once

func TestOutAppCreatedShowsLoginAndPasswordOnce(t *testing.T) {
	out := renderSignedIn(t, "out-app-created", outAppCreatedFixture())
	pageHas(t, "New password", out,
		`<h1 class="title is-3">New password for <span class="sp-mono">prod-server</span></h1>`,
		`<div class="box sp-credential">`, `<h2>Shown once</h2>`, `Not stored — copy it now`, `ti-eye`,
		`<input class="input" readonly value="prod-server">`, `<input class="input" readonly value="kT7v-Qm2x-Lp9d-Ue4s-Hn6b-Rz3w">`,
		`The previous password stopped working when this one was generated.`,
		`href="/outbound/domains/1">Done — back to example.org</a>`,
		`<h2>Connect with</h2>`, `mail.example.org`, `465 SSL/TLS · 587 STARTTLS`, `*@example.org`,
	)
	if got := strings.Count(out, `data-copy`); got != 2 {
		t.Errorf("%d copy buttons, want one for the login and one for the password", got)
	}
	// nothing on the page changes anything: Done is a link, so the only form is the shell's Sign out
	if got := strings.Count(out, `<form`); got != 1 || !strings.Contains(out, `action="/logout"`) {
		t.Errorf("%d forms on the password page, want the shell's Sign out alone", got)
	}

	// An application created just now had no password before.
	p := NewOutAppCreated(admin(), 1, "example.org", "alerts", "pw", true, "", false, []string{"alerts@example.org", "noc@example.org"})
	out = renderSignedIn(t, "out-app-created", p)
	pageHas(t, "New password", out, `If this one is lost, generate another from the application&#39;s page.`,
		`<dd class="sp-mono">alerts@example.org, noc@example.org</dd>`, `SELFPOST_HOSTNAME is not set`)
	pageLacks(t, "New password", out, `stopped working`, `STARTTLS`)
}

func TestOutAppCreatedEscapes(t *testing.T) {
	p := NewOutAppCreated(admin(), 1, "example.org", `<b>x</b>`, `"><script>1</script>`, false, "h", false, []string{`<i>`})
	pageLacks(t, "New password", renderSignedIn(t, "out-app-created", p), `<b>x`, `<script>1`, `<i>`)
}

// ---- Delete an outbound domain

func TestOutDomainDeleteNamesWhatGoesWithIt(t *testing.T) {
	out := renderSignedIn(t, "out-domain-delete", outDomainDeleteFixture())
	pageHas(t, "Delete", out,
		`<h1 class="title is-3">Delete example.org</h1>`, `<div class="box sp-danger-zone">`, `<h2>This cannot be undone</h2>`,
		`Deleting <strong>example.org</strong> will:`, `permanently delete its DKIM signing key;`,
		`delete <strong>both applications</strong> (<span class="sp-mono">prod-server</span>, <span class="sp-mono">alerts</span>) with their credentials and sender bindings;`,
		`reload OpenDKIM so the domain is no longer signed.`, `Inbound domains are not touched.`,
		`<form method="post" action="/outbound/domains/1/delete">`, `class="button is-danger"`, `Delete example.org</button>`,
		`href="/outbound/domains/1">Keep it</a>`,
		`<h2>Moving it instead?</h2>`, `<a href="/outbound/domains/1/settings#export">Export the domain</a>`,
	)

	// No applications: nothing to say about them.
	out = renderSignedIn(t, "out-domain-delete", NewOutDomainDelete(admin(), 1, "example.org", nil))
	pageLacks(t, "Delete", out, `applications`, `credentials`)
	pageHas(t, "Delete", out, `permanently delete its DKIM signing key;`, `reload OpenDKIM`)
	out = renderSignedIn(t, "out-domain-delete", NewOutDomainDelete(admin(), 1, "example.org", []string{"only"}))
	pageHas(t, "Delete", out, `<strong>its application</strong> (<span class="sp-mono">only</span>)`)
}

// ---- Helpers

func TestFormatAge(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	for want, ago := range map[string]time.Duration{
		"just now": 20 * time.Second, "1 min ago": time.Minute, "3 min ago": 3*time.Minute + 59*time.Second,
		"2 h ago": 2 * time.Hour, "2026-10-07": 72 * time.Hour,
	} {
		if got := FormatAge(now.Add(-ago), now); got != want {
			t.Errorf("%v ago = %q, want %q", ago, got, want)
		}
	}
	if FormatAge(time.Time{}, now) != "" {
		t.Error("an unknown time is reported")
	}
}

func TestDNSStatusTag(t *testing.T) {
	for _, c := range []struct {
		status string
		found  bool
		want   Tag
	}{
		{"ok", true, Tag{"published", "published"}}, {"warn", true, Tag{"warn", "weak"}},
		{"error", true, Tag{"mismatch", "mismatch"}}, {"error", false, Tag{"fail", "missing"}},
		{"unknown", false, Tag{"unknown", "not checked"}}, {"", false, Tag{"unknown", "not checked"}},
	} {
		if got := DNSStatusTag(c.status, c.found); got != c.want {
			t.Errorf("DNSStatusTag(%q, %v) = %v, want %v", c.status, c.found, got, c.want)
		}
	}
	// What is not fine never reads as a pass in the stamp.
	for status, want := range map[string]string{"published": LevelOK, "warn": LevelWarn, "unknown": LevelWarn, "mismatch": LevelFail, "fail": LevelFail} {
		if got := tagLevel(status); got != want {
			t.Errorf("tagLevel(%q) = %s, want %s", status, got, want)
		}
	}
}

func TestStatsLead(t *testing.T) {
	if got := StatsLead(1284, 96, "1.8", 30)[0].Text; got != "1 284 messages in 30 days · peak 96 msg/h · average 1.8 msg/h" {
		t.Errorf("lead = %q", got)
	}
	if got := StatsLead(1, 1, "0.0", 1)[0].Text; got != "1 message in 1 day · peak 1 msg/h · average 0.0 msg/h" {
		t.Errorf("lead = %q", got)
	}
}

// An Action that deletes something or ends a working password asks first: the
// question is the form's data-confirm, which panel.js reads, and nothing else
// of the markup changes.
func TestActionConfirmIsOnlyAnAttribute(t *testing.T) {
	plain := partial(t, "action", Action{Label: "Delete", Post: "/x", Danger: true})
	asked := partial(t, "action", Action{Label: "Delete", Post: "/x", Danger: true, Confirm: `Delete "x"?`})
	mustNotContain(t, plain, "data-confirm")
	mustContain(t, asked, `<form method="post" action="/x" data-confirm="Delete &#34;x&#34;?">`)
	if strings.Replace(asked, ` data-confirm="Delete &#34;x&#34;?"`, "", 1) != plain {
		t.Errorf("Confirm changes more than the attribute:\n%s\n%s", plain, asked)
	}
}
