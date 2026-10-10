package view

import (
	"strconv"
	"time"
)

// pageFixtures is the data each redesigned page is rendered with by the guard
// tests (guard_panel_test.go): one entry per page of the engine, keyed by the
// page's engine name. The guards render the page
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

	"in-domains":       func() any { return inDomainsFixture() },
	"in-domain":        func() any { return inDomainFixture() },
	"in-domain-delete": func() any { return inDomainDeleteFixture() },

	"out-log":      func() any { return outLogFixture() },
	"out-message":  func() any { return outMessageFixture() },
	"out-queue":    func() any { return outQueueFixture() },
	"dmarc":        func() any { return dmarcHubFixture() },
	"dmarc-domain": func() any { return dmarcDomainFixture() },
	"dmarc-report": func() any { return dmarcReportFixture() },

	"system-log":  func() any { return systemLogFixture() },
	"backup":      func() any { return backupFixture() },
	"users":       func() any { return usersFixture() },
	"user":        func() any { return userFixture() },
	"user-delete": func() any { return userDeleteFixture() },
	"help":        func() any { return NewHelp(admin(), true) },
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

// accountFixture is the mockup's Account: the global administrator, their name
// and e-mail.
func accountFixture() *Account {
	a := NewAccount(admin(), "global")
	a.Username, a.Email = "admin", "mix@example.org"
	return a
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
		DMARCRecord(DMARCRecordInput{Host: "_dmarc.example.org", Value: "v=DMARC1; p=none; rua=mailto:example.org@dmarc.mail.example.org",
			Check: DNSCheck{Status: "ok"}, Source: "example.org@dmarc.mail.example.org", SettingsHref: "/outbound/domains/1/settings",
			ReportsHref: "/outbound/dmarc/domains/1"}),
	}, "3 min ago")
	p.WithApplications([]OutAppRow{
		{Login: "prod-server", Senders: []string{"*@example.org"}, Activity: FormatActivity(1102, 96),
			Limits: []Tag{{Status: "ok", Label: "200 / h"}}, Edit: "/outbound/domains/1/applications/1",
			Actions: AppRowActions(1, 1, "prod-server")},
		{Login: "alerts", Senders: []string{"alerts@example.org", "noc@example.org"}, Activity: FormatActivity(182, 14),
			Limits: []Tag{{Label: "domain"}, {Status: "ok", Label: "2 IPs"}}, Edit: "/outbound/domains/1/applications/2",
			Actions: AppRowActions(1, 2, "alerts")},
	})
	p.WithConnection("mail.example.org", true)
	p.WithSeeAlso("/outbound/log?domain=example.org", "/outbound/dmarc/domains/1")
	p.WithRateLimit(true, false)
	return p
}

// outDomainSettingsFixture is the mockup's Domain settings: reports go to an
// address typed for the domain, the rate limit is automatic.
func outDomainSettingsFixture() *OutDomainSettings {
	p := NewOutDomainSettings(admin(), 1, "example.org", 2, true)
	p.WithReportAddress([]Option{
		{Value: ReportNone, Label: "No reports"},
		{Value: ReportHosted, Label: "SelfPost hosted — example.org@dmarc.mail.example.org"},
		{Value: ReportCustom, Label: "A specific address"},
	}, ReportCustom, "reports@acme.io", "mix@example.org")
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

// outLogFixture is the mockup's Outbound log: six messages of three domains on
// the first of 412 pages, one deferred, one bounced, one refused by a rate limit.
func outLogFixture() *OutLog {
	p := NewOutLog(admin(), 30, true,
		[]string{"example.org", "shop.example.org", "notify.acme.io"}, []string{"prod-server", "alerts"}, "", "")
	row := func(id int64, at, from, to, subject, status string) OutLogRow {
		return OutLogRow{Time: at, From: from, To: to, Subject: subject, Status: Tag{Status: status},
			Href: p.DetailHref(id, 1)}
	}
	return p.WithRows([]OutLogRow{
		row(184223, "09-21 14:01:52", "shop@shop.example.org", "m.keller@gmx.de", "Your order #48213 has shipped", "sent"),
		row(184222, "09-21 14:01:50", "shop@shop.example.org", "anna.lind@outlook.com", "Your order #48212 has shipped", "sent"),
		row(184220, "09-21 13:58:07", "alerts@example.org", "noc@example.org", "[FIRING] disk usage above 90 % on db-2", "deferred"),
		row(184211, "09-21 13:41:19", "no-reply@notify.acme.io", "j.doe@nonexistent.example", "Reset your password", "bounced"),
		row(184209, "09-21 13:40:02", "prod@example.org", "billing@partner.example", "Invoice 2026-0917", "rejected"),
		row(184198, "09-21 13:12:44", "shop@shop.example.org", "p.novak@seznam.cz", "We received your return request", "sent"),
	}, 1, 412)
}

// outMessageFixture is the mockup's message page: a deferred alert, with the
// four lines Postfix and OpenDKIM wrote about it.
func outMessageFixture() *OutMessage {
	at := time.Date(2026, 9, 21, 13, 58, 7, 0, time.UTC)
	p := NewOutMessage(admin(), MessageInput{
		ID: 184220, QueueID: "4XcB7k2Jm9z1", Domain: "example.org", DomainHref: "/outbound/domains/1",
		App: "alerts", AppHref: "/outbound/domains/1/applications/2",
		From: "alerts@example.org", To: "noc@example.org", Subject: "[FIRING] disk usage above 90 % on db-2", Status: "deferred",
		Accepted: at, Reported: at.Add(2 * time.Second), BackHref: "/outbound/log",
	})
	p.WithHistory([]Step{
		{Time: StepTime(at), Strong: "Accepted and queued", Text: Plain(" — Postfix accepted the message over an authenticated submission and the journal-milter recorded it.")},
		{Time: StepTime(at.Add(2 * time.Second)), Strong: "Deferred, will be retried", Level: LevelWarn,
			Text: Plain(" — the receiving server could not take the message yet. Postfix retries: first after 5 minutes, then with increasing gaps up to 1 hour 7 minutes, for up to 5 days.")},
		{Time: StepTime(time.Time{}), Strong: "Waiting for a delivery report", Level: LevelPending,
			Text: Plain(" — the next attempt is made by Postfix on its own; the queue shows what it is still holding.")},
	})
	return p.WithDeliveryLog([]LogLine{
		{Time: "13:58:07", Text: "postfix/smtpd[2114]: 4XcB7k2Jm9z1: client=unknown[198.51.100.7], sasl_username=alerts"},
		{Time: "13:58:07", Text: "postfix/cleanup[2120]: 4XcB7k2Jm9z1: message-id=<a81f0c@alertmanager>"},
		{Time: "13:58:07", Text: "opendkim[39]: 4XcB7k2Jm9z1: DKIM-Signature field added (s=sp2026, d=example.org)"},
		{Time: "13:58:09", Level: "warn", Text: "postfix/smtp[2123]: 4XcB7k2Jm9z1: to=<noc@example.org>, relay=mx1.example.org[192.0.2.10]:25, delay=1.9, status=deferred (host mx1.example.org said: 451 4.7.1 Greylisted, try again in 5 minutes)"},
	}, "")
}

// dmarcHubFixture is the mockup's DMARC hub: ingest healthy, four recent
// reports, one with failures.
func dmarcHubFixture() *DMARCHub {
	p := NewDMARCHub(admin(), IngestInput{
		OK: true, Last: time.Date(2026, 9, 21, 6, 12, 0, 0, time.UTC), KeptThisWeek: 14, ParseFailures: 0,
		Hosted: "dmarc@mail.example.org", Host: "mail.example.org", RetentionMax: 500, RetentionDays: 90,
	})
	row := func(id int64, at, domain, reporter, window string, pass, fail int) DMARCReportRow {
		return DMARCReportRow{Received: at, Domain: domain, DomainHref: "/outbound/dmarc/domains/1", Reporter: reporter,
			Window: window, Pass: pass, Fail: fail, Href: DMARCReportHref(id)}
	}
	return p.WithReports([]DMARCReportRow{
		row(41, "09-21 06:12", "example.org", "google.com", "20 Sep", 412, 0),
		row(40, "09-21 04:40", "shop.example.org", "Outlook.com", "20 Sep", 1903, 7),
		row(39, "09-20 23:58", "example.org", "Yahoo", "19 Sep", 38, 0),
		row(38, "09-20 06:09", "notify.acme.io", "google.com", "19 Sep", 61, 0),
	})
}

// dmarcDomainFixture is the mockup's domain page: seven days of shop.example.org
// with one source that is not this relay failing, and three reports.
func dmarcDomainFixture() *DMARCDomain {
	p := NewDMARCDomain(admin(), 2, "shop.example.org", true, 11204, 41, 7,
		"A third-party source is not aligned. Do not tighten p= until that sender is fixed or removed.")
	row := func(id int64, at, reporter, window string, pass, fail int) DMARCReportRow {
		return DMARCReportRow{Received: at, Reporter: reporter, Window: window, Pass: pass, Fail: fail, Href: DMARCReportHref(id)}
	}
	return p.WithSources([]DMARCSourceRow{
		{Source: "203.0.113.25", ThisRelay: true, Pass: 11204, Fail: 0, Disposition: "none"},
		{Source: "198.51.100.44", Pass: 0, Fail: 41, Disposition: "none"},
	}).WithReports([]DMARCReportRow{
		row(40, "09-21 04:40", "Outlook.com", "20 Sep", 1903, 7),
		row(37, "09-21 02:15", "google.com", "20 Sep", 6410, 0),
		row(35, "09-20 04:38", "Outlook.com", "19 Sep", 1877, 34),
	})
}

// dmarcReportFixture is the mockup's report: Outlook.com's day for
// shop.example.org, two sources, one of them failing both checks.
func dmarcReportFixture() *DMARCReport {
	return NewDMARCReport(admin(), ReportInput{
		ID: 40, Reporter: "Outlook.com", ReportID: "5f1c9a0e7b2d4e61a3", Domain: "shop.example.org", DomainID: 2,
		Pass: 1903, Fail: 7, Window: "20 Sep", Period: "20 Sep 00:00 – 21 Sep 00:00 UTC", Received: "2026-09-21 04:40 UTC",
		PolicyP: "none", PolicySP: "none", PolicyPct: 100, PolicyADKIM: "r", PolicyASPF: "r",
		Recipient: "dmarc@mail.example.org", Hub: true,
	}).WithRecords([]DMARCRecordRow{
		{Source: "203.0.113.25", ThisRelay: true, Count: 1903, Disposition: "none", SPF: "pass", DKIM: "pass", HeaderFrom: "shop.example.org"},
		{Source: "198.51.100.44", Count: 7, Disposition: "none", SPF: "fail", DKIM: "fail", HeaderFrom: "shop.example.org"},
	})
}

// inDomainsFixture is the mockup's Inbound domains: two domains, one that lists
// its four recipients and needs TLS to its upstream, one that accepts any
// address and takes whatever its upstream offers.
func inDomainsFixture() *InDomains {
	row := func(in InDomainInput) InDomainRow { return NewInDomainRow(in, true) }
	return NewInDomains(admin(), true).WithFilter(true).WithRows([]InDomainRow{
		row(InDomainInput{ID: 1, Name: "lists.example.org", DNSStatus: "ok", Host: "mx.internal", Port: 25,
			TLSMode: "encrypt", RecipientMode: "list", RecipientCount: 4}),
		row(InDomainInput{ID: 2, Name: "acme.io", DNSStatus: "ok", Host: "10.0.4.12", Port: 2525,
			TLSMode: "may", RecipientMode: "any"}),
	})
}

// inDomainDeleteFixture is the mockup's confirmation: a domain with an upstream
// and four listed recipients.
func inDomainDeleteFixture() *InDomainDelete {
	return NewInDomainDelete(admin(), 1, "lists.example.org", true, 4)
}

// inDomainFixture is the mockup's domain page: the MX published and pointing
// here, an upstream that needs TLS, four listed recipients and a spam filter
// that is on.
func inDomainFixture() *InDomain {
	p := NewInDomain(admin(), InDomainInput{ID: 1, Name: "lists.example.org", DNSStatus: "ok", Host: "mx.internal", Port: 25,
		TLSMode: "encrypt", RecipientMode: "list", RecipientCount: 4,
		Addresses: []string{"announce@lists.example.org", "dev@lists.example.org", "owner@lists.example.org", "security@lists.example.org"}})
	p.WithMX("mail.example.org", DNSCheck{Status: "ok", Found: []string{"10 mail.example.org."}})
	p.WithFilter(true, "inet:antispam:11332", "accept")
	return p
}

// outQueueFixture is the queue as Postfix prints it for the mockup's one waiting
// message, greylisted by the receiving server, and the retry policy it shows.
func outQueueFixture() *OutQueue {
	listing := "-Queue ID-  --Size-- ----Arrival Time---- -Sender/Recipient-------\n" +
		"4XcB7k2Jm9z1     3174 Sun Sep 21 13:58:07  alerts@example.org\n" +
		"(host mx1.example.org[192.0.2.10] said: 451 4.7.1 Greylisted, try again in 5 minutes (in reply to RCPT TO command))\n" +
		"                                         noc@example.org\n" +
		"\n" +
		"-- 3 Kbytes in 1 Request.\n"
	return NewOutQueue(admin(), listing, "").WithRetryPolicy("5 minutes", "about 1 hour 7 minutes", "5 days", false)
}

// systemLogFixture is the mockup's System log: ten lines of mail.log, as the
// handler reads them (oldest first), with the deferrals in amber and the
// refusals and the bounce in red.
func systemLogFixture() *SystemLog {
	return NewSystemLog(admin(), []LogLine{
		{Time: "Sep 21 13:00:00", Text: "opendkim[39]: key table reloaded (3 domains)"},
		{Time: "Sep 21 13:40:02", Text: "journal-milter[40]: rate limit: app=prod-server 201/200 in 3600s — tempfail at MAIL FROM", Level: "warn"},
		{Time: "Sep 21 13:41:19", Text: "postfix/smtp[2098]: 4XcB1c3Rk8z1: to=<j.doe@nonexistent.example>, status=bounced (Host or domain name not found)", Level: "error"},
		{Time: "Sep 21 13:44:02", Text: "postfix/smtp[2101]: 4XcB2m7Hd4z1: to=<jobs@acme.io>, relay=none, status=deferred (connect to 10.0.4.12[10.0.4.12]:2525: Connection timed out)", Level: "warn"},
		{Time: "Sep 21 13:57:41", Text: "postfix/smtpd[2119]: NOQUEUE: reject: RCPT from bulk.example[198.51.100.90]: 550 5.1.1 <sales@lists.example.org>: Recipient address rejected: unknown recipient", Level: "error"},
		{Time: "Sep 21 13:58:09", Text: "postfix/smtp[2123]: 4XcB7k2Jm9z1: to=<noc@example.org>, relay=mx1.example.org[192.0.2.10]:25, status=deferred (451 4.7.1 Greylisted, try again in 5 minutes)", Level: "warn"},
		{Time: "Sep 21 14:00:18", Text: "postfix/smtp[2130]: 4XcB8z5Tn1z1: to=<dev@lists.example.org>, relay=mx.internal[10.0.4.2]:25, status=sent (250 2.0.0 Ok: queued)"},
		{Time: "Sep 21 14:00:18", Text: "postfix/smtpd[2129]: connect from mail.uni.example[192.0.2.77]"},
		{Time: "Sep 21 14:01:52", Text: "postfix/qmgr[58]: 4XcB9q0Lw2z1: removed"},
		{Time: "Sep 21 14:01:52", Text: "postfix/smtp[2131]: 4XcB9q0Lw2z1: to=<m.keller@gmx.de>, relay=mx00.gmx.net[212.227.15.9]:25, delay=0.8, status=sent (250 Requested mail action okay)"},
	}, "")
}

// backupFixture is the mockup's Backup: nothing refused, the password minimum
// of the encryption fields at twelve.
func backupFixture() *Backup {
	return NewBackup(admin(), 12, "")
}

// usersFixture is the mockup's Users: the signed-in global administrator and
// two domain administrators, one of them just created.
func usersFixture() *Users {
	return NewUsers(admin(), true, "User created.").WithRows([]UserRow{
		NewUserRow(UserRowInput{ID: 1, Username: "admin", Email: "mix@example.org", Global: true, You: true}),
		NewUserRow(UserRowInput{ID: 2, Username: "shop-team", Email: "team@shop.example.org", Outbound: []string{"shop.example.org"}}),
		NewUserRow(UserRowInput{ID: 3, Username: "acme-ops", Email: "ops@acme.io", AllOutbound: true, Inbound: []string{"acme.io"}}),
	})
}

// userFixture is the mockup's user form, filled with acme-ops: a domain
// administrator on all outbound domains and on acme.io inbound.
func userFixture() *UserForm {
	return NewUserForm(admin(), UserFormInput{
		ID: 3, Name: "acme-ops", Username: "acme-ops", Email: "ops@acme.io", Role: "domain", CanDelete: true,
		PasswordMin: 12, ShowInbound: true,
		AllOut:     true,
		OutDomains: []DomainChoice{{ID: 1, Name: "example.org"}, {ID: 2, Name: "shop.example.org"}, {ID: 3, Name: "notify.acme.io"}},
		InDomains:  []DomainChoice{{ID: 1, Name: "lists.example.org"}, {ID: 2, Name: "acme.io", Checked: true}},
	})
}

// userDeleteFixture is the mockup's confirmation: acme-ops, who has all
// outbound domains and acme.io inbound.
func userDeleteFixture() *UserDelete {
	return NewUserDelete(admin(), UserDeleteInput{ID: 3, Username: "acme-ops", AllOutbound: true, Inbound: []string{"acme.io"}})
}
