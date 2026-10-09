package view

import (
	"strconv"
	"time"
)

// pageFixtures is the data each redesigned page is rendered with by the guard
// tests (guard_panel_test.go): one entry per page that has left
// legacy_pages.txt, keyed by the page's engine name. The guards render the page
// through Engine.Render with this value and hold the result to the design
// contract — its class vocabulary, its page structure and the component
// skeleton recorded for the mockup of the same name in
// docs/assets/panel-redesign/panel/outlines.json.
//
// A fixture therefore has to fill the page the way its mockup is filled: the
// same boxes, the same number of records. It is also what the evidence
// screenshots are compared against, so use plausible values, not "foo".
//
// This file belongs to the implementation, not to the guards: the step that
// restyles a page adds its fixture here in the same commit.
var pageFixtures = map[string]func() any{
	"components": func() any { return KitPage() },
	"login":      func() any { return NewLogin("mail.example.org", "") },
	"setup":      func() any { return NewSetup("0123456789abcdef", "") },
	"overview":   func() any { return overviewFixture() },
	"health":     func() any { return healthFixture(false) },
	"account":    func() any { return accountFixture() },
	"settings":   func() any { return settingsFixture() },

	"out-domains":         func() any { return outDomainsFixture() },
	"out-domain":          func() any { return outDomainFixture() },
	"out-domain-settings": func() any { return outDomainSettingsFixture() },
	"out-app":             func() any { return outAppFixture() },
	"out-app-created":     func() any { return outAppCreatedFixture() },
	"out-domain-delete":   func() any { return outDomainDeleteFixture() },
}

// admin is the signed-in global administrator every fixture is rendered for.
func admin() Meta { return Meta{User: "admin", IsGlobal: true} }

// overviewFixture is the mockup's Overview: one certificate warning among six
// checks, three outbound domains and two inbound.
func overviewFixture() *Overview {
	cards := []HealthCard{
		{Name: "Machine", Value: "CPU 12%", Sub: "RAM 41%", Icon: "ti-cpu", Href: "/server/health#machine"},
		{Name: "Processes", Value: "5 of 5 running", Sub: "all programs", Icon: "ti-server-cog", Href: "/server/health#processes"},
		{Name: "TLS certificate", Value: "Expires in 12 days", Sub: "mail.example.org", Icon: "ti-certificate", Href: "/server/health#certificate", Level: LevelWarn},
		{Name: "Queue", Value: "Empty", Sub: "outbound", Icon: "ti-stack-2", Href: "/outbound/queue"},
		{Name: "Milter sockets", Value: "2 of 2 answering", Sub: "OpenDKIM, send-log", Icon: "ti-plug-connected", Href: "/server/health#sockets"},
		{Name: "Reverse DNS", Value: "Forward = reverse", Sub: "mail.example.org", Icon: "ti-arrows-exchange", Href: "/server/health#hostname"},
	}
	o := NewOverview(admin(), "mail.example.org", cards, time.Date(2026, 10, 9, 14, 2, 31, 0, time.UTC), 30, true)
	dns := func(dkim, spf, dmarc string) []Tag {
		return []Tag{{Status: dkim, Label: "DKIM"}, {Status: spf, Label: "SPF"}, {Status: dmarc, Label: "DMARC"}}
	}
	o.OutboundRows = []OverviewDomain{
		{Name: "example.org", Href: "/outbound/domains/1", DNS: dns("ok", "error", "ok"), Apps: 2, Messages: FormatMessages(1284)},
		{Name: "shop.example.org", Href: "/outbound/domains/2", DNS: dns("ok", "ok", "ok"), Apps: 1, Messages: FormatMessages(18920)},
		{Name: "notify.acme.io", Href: "/outbound/domains/3", DNS: dns("ok", "ok", "warn"), Apps: 3, Messages: FormatMessages(402)},
	}
	o.InboundRows = []OverviewInbound{
		{Name: "lists.example.org", Href: "/inbound/domains/1", MX: Tag{Status: "ok", Label: "MX"}, Upstream: "mx.internal:25"},
		{Name: "acme.io", Href: "/inbound/domains/2", MX: Tag{Status: "ok", Label: "MX"}, Upstream: "10.0.4.12:2525"},
	}
	return o
}

// healthFixture is the mockup's Health: a healthy machine and five processes,
// a certificate close to expiry, the two milter sockets and a good reverse
// record. refresh renders it as the polled fragment.
func healthFixture(refresh bool) *Health {
	h := NewHealth(admin(), "")
	h.Refresh = refresh
	h.Machine.End = Verdict("ok")
	h.MachineRows = []MachineRow{
		{Resource: "CPU", Gauge: &Gauge{Percent: 12, Text: "12%", Level: LevelOK}, Detail: []string{"2 cores · 2 threads"}},
		{Resource: "Memory", Gauge: &Gauge{Percent: 41, Text: "41%", Level: LevelOK}, Detail: []string{"812.0 MiB used of 1.9 GiB"}},
		{Resource: "Network", Usage: "↓ 18.0 KiB/s · ↑ 44.0 KiB/s", Detail: []string{"eth0: 2.1 GiB in, 6.4 GiB out"}},
	}
	h.Processes.End = Verdict("ok")
	for i, name := range []string{"postfix", "opendkim", "journal-milter", "panel", "dmarc-ingest"} {
		h.ProcessRows = append(h.ProcessRows, ProcessRow{Name: name, State: Tag{Status: "ok", Label: "running"},
			Detail: "pid " + strconv.Itoa(41-i) + ", uptime 18 days"})
	}
	h.Certificate.End = Verdict("warn")
	h.CertFacts = []Fact{
		{Label: "Expires", Value: Plain("2026-10-21 09:14 UTC"), Mono: true},
		{Label: "Names", Value: Plain("mail.example.org"), Mono: true},
	}
	h.CertDetail, h.CertProblem = "Expires in 12 day(s). Check that renewal on the host still works.", true
	h.Sockets.End = Verdict("ok")
	h.SocketRows = []SocketRow{
		{Name: "OpenDKIM", Path: "inet:127.0.0.1:8891", State: Tag{Status: "ok"}},
		{Name: "send-log", Path: "unix:/run/selfpost/journal.sock", State: Tag{Status: "ok"}},
	}
	h.Hostname.End = Verdict("ok")
	h.HostFacts = []Fact{
		{Label: "Hostname", Value: Plain("mail.example.org"), Mono: true},
		{Label: "Lookup", Value: Rich("→ 203.0.113.25", Br(), "← mail.example.org"), Mono: true},
	}
	return h
}

// accountFixture is the mockup's Account: the global administrator, their
// e-mail as the DMARC default, and the authorization record that default needs.
func accountFixture() *Account {
	m := admin()
	a := NewAccount(m, "global", true, "mix@example.org")
	a.Username, a.Email = "admin", "mix@example.org"
	a.DMARCSelected = DMARCChoiceAccount
	a.WithDomainUse(2, 3)
	return a.WithAuthorization("*._report._dmarc.example.org", "v=DMARC1;", "ok", "", nil)
}

// settingsFixture is the mockup's Settings: thirty days of log, a limit of 600
// messages an hour.
func settingsFixture() *Settings {
	return NewSettings(admin(), "30", FormatRate(600, 3600))
}

// outDomainsFixture is the mockup's Outbound domains: three domains, the first
// with a wrong SPF record and the last with a weak DMARC one.
func outDomainsFixture() *OutDomains {
	dns := func(dkim, spf, dmarc string) []Tag {
		return []Tag{{Status: dkim, Label: "DKIM"}, {Status: spf, Label: "SPF"}, {Status: dmarc, Label: "DMARC"}}
	}
	row := func(id, name string, tags []Tag, selector string, apps int, total, peak int64) OutDomainRow {
		return OutDomainRow{Name: name, Href: "/outbound/domains/" + id, DeleteHref: "/outbound/domains/" + id + "/delete",
			DNS: tags, Selector: selector, Apps: apps, Activity: FormatActivity(total, peak)}
	}
	return NewOutDomains(admin(), true, 30).WithRows([]OutDomainRow{
		row("1", "example.org", dns("ok", "error", "ok"), "sp2026", 2, 1284, 96),
		row("2", "shop.example.org", dns("ok", "ok", "ok"), "sp2026", 1, 18920, 410),
		row("3", "notify.acme.io", dns("ok", "ok", "warn"), "sp2025", 3, 402, 22),
	})
}

// outDomainFixture is the mockup's domain page: the DKIM and DMARC records
// published, the SPF one pointing at the wrong server, two applications.
func outDomainFixture() *OutDomain {
	p := NewOutDomain(admin(), 1, "example.org", true)
	p.WithStats(1284, 96, "1.8", 30)
	p.WithRecords([]Record{
		DKIMRecord("sp2026", "sp2026._domainkey.example.org",
			"v=DKIM1; k=rsa; p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAx3kqV0mJd8e2f1Yb7Qw5uT9sLr4nHc6pZaXo1vKe0yWm2gBt8RjN5dCq3hUf7lPs9aEi4oMz6xTb1kGv0nYw…IDAQAB",
			DNSCheck{Status: "ok"}),
		SPFRecord("example.org", "v=spf1 ip4:203.0.113.25 ~all", DNSCheck{Status: "error",
			Detail: "The published record does not authorize 203.0.113.25. Add ip4:203.0.113.25 before ~all.",
			Found:  []string{"v=spf1 include:_spf.google.com ~all"}}),
		DMARCRecord(DMARCRecordInput{Host: "_dmarc.example.org", Value: "v=DMARC1; p=none; rua=mailto:dmarc@mail.example.org",
			Check: DNSCheck{Status: "ok"}, Source: "admin's default", SettingsHref: "/outbound/domains/1/settings",
			ReportsHref: "/outbound/dmarc/domains/1"}),
	}, "3 min ago")
	p.WithApplications([]OutAppRow{
		{Login: "prod-server", Senders: []string{"*@example.org"}, Activity: FormatActivity(1102, 96),
			Limits: []Tag{{Status: "ok", Label: "200 / h"}}, Edit: "/outbound/domains/1/applications/1"},
		{Login: "alerts", Senders: []string{"alerts@example.org", "noc@example.org"}, Activity: FormatActivity(182, 14),
			Limits: []Tag{{Label: "domain"}, {Status: "ok", Label: "2 IPs"}}, Edit: "/outbound/domains/1/applications/2"},
	})
	p.WithConnection("mail.example.org", true)
	p.WithSeeAlso("/outbound/log?domain=example.org", "/outbound/dmarc/domains/1")
	p.WithRateLimit(true, false)
	return p
}

// outDomainSettingsFixture is the mockup's Domain settings: reports go to the
// default of the signed-in administrator, the rate limit is automatic.
func outDomainSettingsFixture() *OutDomainSettings {
	p := NewOutDomainSettings(admin(), 1, "example.org", 2, true)
	p.WithReportAddress([]Option{
		{Value: "inherit", Label: "admin's default — mix@example.org"},
		{Value: "hosted", Label: "SelfPost hosted (example.org@dmarc.mail.example.org)"},
		{Value: "none", Label: "No aggregate reports"},
		{Value: "custom", Label: "Custom address"},
	}, "inherit", "", Rich("A default belongs to a user and is set under their ", Link("/account#dmarc", "Account"),
		"; a domain follows one named user, so two people sharing a domain never pull it two ways. Changing this changes the DMARC record to publish."))
	p.WithExport(12)
	return p.WithRateLimit(DomainRateLimit{
		Active: true, Auto: true, MaxMessages: "288", Window: "3600", Multiplier: "2.5",
		Computed: "288", Peak: 96, Updated: "2026-10-09 03:00 UTC",
		L1Messages: 600, L1Window: 3600, MinMultiplier: "1.5", MaxMultiplier: "5", DefaultMultiplier: "2.5",
	})
}

// outAppFixture is the mockup's application form, filled with prod-server: any
// address of the domain, no client IPs, a manual limit of 200 an hour.
func outAppFixture() *OutApp {
	p := NewOutApp(admin(), 1, "example.org", "prod-server")
	p.WithApplication(1, 1, 1102, 96, "1.5", 30)
	return p.WithForm(OutAppForm{
		Login: "prod-server", Mode: AddressWildcard, LimitMode: LimitManual, MaxMessages: "200", Window: "3600", Multiplier: "2.5",
	}, OutAppState{Limit: true}, AppRateLimit{L1Messages: 600, L1Window: 3600, MinMultiplier: "1.5", MaxMultiplier: "5",
		DefaultMultiplier: "2.5", DomainLimit: "288 / h", Peak: 96})
}

// outAppCreatedFixture is the mockup's password page: a new password for
// prod-server, which may send as the whole domain.
func outAppCreatedFixture() *OutAppCreated {
	return NewOutAppCreated(admin(), 1, "example.org", "prod-server", "kT7v-Qm2x-Lp9d-Ue4s-Hn6b-Rz3w", false,
		"mail.example.org", true, []string{"*@example.org"})
}

// outDomainDeleteFixture is the mockup's confirmation: the domain has the two
// applications that go with it.
func outDomainDeleteFixture() *OutDomainDelete {
	return NewOutDomainDelete(admin(), 1, "example.org", []string{"prod-server", "alerts"})
}
