package view

import (
	"strconv"
	"strings"
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
	"components":  func() any { return KitPage() },
	"login":       func() any { return NewLogin("mail.example.org", "") },
	"setup":       func() any { return NewSetup("0123456789abcdef", "") },
	"login-reset": func() any { return NewReset("mail.example.org", "") },
	"login-reset-set": func() any {
		return NewResetSet("mail.example.org", "admin", "0123456789abcdef", "")
	},
	"confirm-email": func() any {
		return NewConfirm("mail.example.org", "admin", "mix@example.org", "0123456789abcdef")
	},
	"overview": func() any { return overviewFixture() },
	"health":   func() any { return healthFixture(false) },
	"account":  func() any { return accountFixture() },
	"settings": func() any { return settingsFixture() },

	"out-domains":         func() any { return outDomainsFixture() },
	"out-domain":          func() any { return outDomainFixture() },
	"out-domain-settings": func() any { return outDomainSettingsFixture() },
	"out-app":             func() any { return outAppFixture() },
	"out-app-created":     func() any { return outAppCreatedFixture() },
	"out-domain-delete":   func() any { return outDomainDeleteFixture() },

	"in-domains":       func() any { return inDomainsFixture() },
	"in-domain":        func() any { return inDomainFixture() },
	"in-domain-delete": func() any { return inDomainDeleteFixture() },
	"in-log":           func() any { return inLogFixture() },
	"in-filter-lists":  func() any { return inFilterListsFixture() },

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
		{Name: "Machine", Value: "CPU 12%", Sub: "RAM 41%", Icon: "ti-cpu", Href: "/server/health"},
		{Name: "Processes", Value: "4 of 4 running", Sub: "all programs", Icon: "ti-server-cog", Href: "/server/health"},
		{Name: "TLS certificate", Value: "Expires in 12 days", Sub: "mail.example.org", Icon: "ti-certificate", Href: "/server/health", Level: LevelWarn},
		{Name: "Queue", Value: "Empty", Sub: "outbound", Icon: "ti-stack-2", Href: "/outbound/queue"},
		{Name: "Milter sockets", Value: "5 of 5 answering", Sub: "incl. the spam filter", Icon: "ti-plug-connected", Href: "/server/health"},
		{Name: "Reverse DNS", Value: "Forward = reverse", Sub: "mail.example.org", Icon: "ti-arrows-exchange", Href: "/server/health"},
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

// healthFixture is the mockup's Health: a healthy machine and four processes,
// a certificate close to expiry, the milter sockets and a good reverse
// record. refresh renders it as the polled fragment.
func healthFixture(refresh bool) *Health {
	h := NewHealth(admin(), "")
	if refresh {
		h.AsFragment()
	}
	h.WithSenderCheck([]SenderCheckRow{
		{Period: "Today", Down: "0 s", Deferred: "0", Messages: "0"},
		{Period: "Last 30 days", Down: "4 s", Deferred: "0", Messages: "0"},
		{Period: "Last 12 months", Down: "≈ 41 s", Deferred: "3", Messages: "1"},
	}, "2026-10-13", "ok")
	h.Machine.End = Verdict("ok")
	h.MachineRows = []MachineRow{
		{Resource: "CPU", Gauge: &Gauge{Percent: 12, Text: "12%", Level: LevelOK}, Detail: []string{"2 cores · 2 threads"}},
		{Resource: "Memory", Gauge: &Gauge{Percent: 41, Text: "41%", Level: LevelOK}, Detail: []string{"812.0 MiB used of 1.9 GiB"}},
		{Resource: "Network", Usage: "↓ 18.0 KiB/s · ↑ 44.0 KiB/s", Detail: []string{"eth0: 2.1 GiB in, 6.4 GiB out"}},
	}
	h.Processes.End = Verdict("ok")
	for i, name := range []string{"postfix", "journal-milter", "panel", "dmarc-ingest"} {
		h.ProcessRows = append(h.ProcessRows, ProcessRow{Name: name, State: Tag{Status: "ok", Label: "running"},
			Detail: "pid " + strconv.Itoa(41-i) + ", uptime 18 days"})
	}
	h.Certificate.End = Verdict("warn")
	h.CertFacts = []Fact{
		{Label: "Expires", Value: Plain("2026-10-21 09:14"), Mono: true},
		{Label: "Names", Value: Plain("mail.example.org"), Mono: true},
	}
	h.CertDetail, h.CertProblem = "Expires in 12 day(s). Check that renewal on the host still works.", true
	h.Sockets.End = Verdict("ok")
	h.SocketRows = []SocketRow{
		{Name: "dkim-signer", Note: "applications", Path: "unix:/run/selfpost/sign.sock", State: Tag{Status: "ok"}},
		{Name: "dkim-signer", Note: "notifications", Path: "unix:/run/selfpost/sign-local.sock", State: Tag{Status: "ok"}},
		{Name: "sender-check", Path: "unix:/run/selfpost/sender.sock", State: Tag{Status: "ok"}},
		{Name: "send-log", Path: "unix:/run/selfpost/journal.sock", State: Tag{Status: "ok"}},
	}
	h.Hostname.End = Verdict("ok")
	h.HostFacts = []Fact{
		{Label: "Hostname", Value: Plain("mail.example.org"), Mono: true},
		{Label: "Lookup", Value: Rich("→ 203.0.113.25", Br(), "← mail.example.org"), Mono: true},
	}
	h.WithNotificationMail("postmaster@mail.example.org", []MailRow{
		NewMailRow("DKIM", mailFixtureDKIMHost, DNSCheck{Status: "ok"}),
		NewMailRow("SPF", "mail.example.org", DNSCheck{Status: "error"}),
	}, "warn")
	return h
}

// accountFixture is the mockup's Account: the global administrator, their name
// and e-mail.
func accountFixture() *Account {
	a := NewAccount(admin(), "global")
	a.Username, a.Email, a.Notify = "admin", "mix@example.org", true
	// The mockup's address: not confirmed, its link sent at 14:02.
	a.WithEmailState("mix@example.org", false, time.Date(2026, 10, 17, 14, 2, 0, 0, time.UTC))
	return a
}

// mailFixtureDKIMHost is where the mockup publishes the DKIM key of the server's
// own name.
const mailFixtureDKIMHost = "sp2026._domainkey.mail.example.org"

// settingsFixture is the mockup's Settings: thirty days of log, a limit of 600
// messages an hour, notifications on, the records of the server's own name
// with the DKIM one published and the SPF one missing, and Moscow's time.
func settingsFixture() *Settings {
	msk, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		msk = time.FixedZone("Europe/Moscow", 3*3600)
	}
	key := "v=DKIM1; k=rsa; p=" + strings.Repeat("MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A", 10) + "IDAQAB"
	return NewSettings(admin(), "30", FormatRate(600, 3600)).WithInbound("90").
		WithNotifications(true, "postmaster@mail.example.org").
		WithMail([]Record{
			ServerMailDKIM("sp2026", mailFixtureDKIMHost, key, DNSCheck{Status: "ok"}),
			ServerMailSPF("mail.example.org", "v=spf1 a -all", DNSCheck{Status: "error", Detail: "No SPF record is published for mail.example.org."}),
		}, "3 min ago").
		WithTimeZone("Europe/Moscow", msk, time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC))
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
		Computed: "288", Peak: 96, Updated: "2026-10-09 03:00",
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
		[]string{"example.org", "shop.example.org", "notify.acme.io"}, []string{"prod-server", "alerts"}, OutLogFilter{})
	// status is a row's status, or for a refused row "rejected:" and its reason.
	row := func(id int64, at, from, to, subject, status string) OutLogRow {
		status, reason, _ := strings.Cut(status, ":")
		return OutLogRow{Time: at, From: from, To: to, Subject: subject, Status: SendLogTag(status, reason),
			Href: p.DetailHref(id, 1)}
	}
	return p.WithRows([]OutLogRow{
		row(184223, "09-21 14:01:52", "shop@shop.example.org", "m.keller@gmx.de", "Your order #48213 has shipped", "sent"),
		row(184222, "09-21 14:01:50", "shop@shop.example.org", "anna.lind@outlook.com", "Your order #48212 has shipped", "sent"),
		row(184220, "09-21 13:58:07", "alerts@example.org", "noc@example.org", "[FIRING] disk usage above 90 % on db-2", "deferred"),
		row(184211, "09-21 13:41:19", "no-reply@notify.acme.io", "j.doe@nonexistent.example", "Reset your password", "bounced"),
		row(184231, "09-21 13:40:55", "shop@shop.example.org", "k.berg@web.de", "A message from the director", "rejected:from"),
		row(184209, "09-21 13:40:02", "prod@example.org", "billing@partner.example", "Invoice 2026-0917", "rejected:limit"),
		row(184198, "09-21 13:12:44", "shop@shop.example.org", "p.novak@seznam.cz", "We received your return request", "sent"),
	}, 1, 412)
}

// outMessageFixture is the mockup's message page: a deferred alert, with the
// four lines Postfix wrote about it.
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
		{Time: StepTime(time.Time{}), Level: LevelPending,
			Text: Plain("Still in the queue; the next attempt is Postfix's own, and after 5 days it is bounced.")},
	})
	return p.WithDeliveryLog([]LogLine{
		{Time: "13:58:07", Text: "postfix/smtpd[2114]: 4XcB7k2Jm9z1: client=unknown[198.51.100.7], sasl_username=alerts"},
		{Time: "13:58:07", Text: "postfix/cleanup[2120]: 4XcB7k2Jm9z1: message-id=<a81f0c@alertmanager>"},
		{Time: "13:58:07", Text: "postfix/qmgr[2061]: 4XcB7k2Jm9z1: from=<alerts@example.org>, size=2214, nrcpt=1 (queue active)"},
		{Time: "13:58:09", Level: "warn", Text: "postfix/smtp[2123]: 4XcB7k2Jm9z1: to=<noc@example.org>, relay=mx1.example.org[192.0.2.10]:25, delay=1.9, status=deferred (host mx1.example.org said: 451 4.7.1 Greylisted, try again in 5 minutes)"},
	}, "")
}

// dmarcHubFixture is the mockup's DMARC hub: ingest healthy, four recent
// reports, one with failures.
func dmarcHubFixture() *DMARCHub {
	p := NewDMARCHub(admin(), IngestInput{
		OK: true, Last: time.Date(2026, 9, 21, 6, 12, 0, 0, time.UTC), KeptThisWeek: 14, ParseFailures: 0,
		Hosted: "dmarc-reports+<domain>@mail.example.org", Host: "mail.example.org", RetentionMax: 500, RetentionDays: 90,
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
		"Failures come from a source that is not this relay — review before tightening p=.")
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
		Pass: 1903, Fail: 7, Window: "20 Sep", Period: "20 Sep 00:00 – 21 Sep 00:00", Received: "2026-09-21 04:40",
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
	p.WithChecked("1 min ago").WithLog("/inbound/log?domain=lists.example.org")
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

// systemLogFixture is the mockup's System log: nine lines of mail.log, as the
// handler reads them (oldest first), with the deferrals in amber and the
// refusals and the bounce in red.
func systemLogFixture() *SystemLog {
	return NewSystemLog(admin(), []LogLine{
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
	}).WithEmailState("ops@acme.io", true)
}

// userDeleteFixture is the mockup's confirmation: acme-ops, who has all
// outbound domains and acme.io inbound.
func userDeleteFixture() *UserDelete {
	return NewUserDelete(admin(), UserDeleteInput{ID: 3, Username: "acme-ops", AllOutbound: true, Inbound: []string{"acme.io"}})
}

// inLogFixture is the mockup's Inbound log: six rows of a week's mail, as the
// global role sees them - an accepted message scored as spam and one let through
// by an Allow entry, refusals by Postfix and by the filter, and a tempfail.
func inLogFixture() *InLog {
	p := NewInLog(admin(), 90, []string{"lists.example.org", "acme.io"},
		InLogFilter{From: "2026-09-14", To: "2026-09-21"})
	var id int64
	clock := func(c string) time.Time {
		at, err := time.Parse("2006-01-02 15:04:05", "2026-09-21 "+c)
		if err != nil {
			panic(err)
		}
		return at
	}
	// up is what the upstream answered: status, host, reply, when.
	type up struct{ status, relay, reply, at string }
	upRow := func(c, from, client, to, subject, decision, engine, detail string, u up) InLogRow {
		id++
		in := InLogRowInput{ID: id, Listable: true, At: clock(c), From: from, Client: client, To: to, Subject: subject,
			Decision: decision, Engine: engine, Detail: detail,
			Upstream: u.status, UpstreamRelay: u.relay, UpstreamReply: u.reply}
		if u.at != "" {
			in.UpstreamAt = clock(u.at)
		}
		return p.NewRow(in)
	}
	row := func(c, from, client, to, subject, decision, engine, detail string) InLogRow {
		return upRow(c, from, client, to, subject, decision, engine, detail, up{})
	}
	const host = "mx.upstream.example[198.51.100.5]:25"
	sent := upRow("14:00:18", "t.berg@uni.example", "192.0.2.77", "dev@lists.example.org", "Re: [dev] patch series v3",
		"accept", "rspamd", "no action, score 0.40 / 15.00",
		up{"sent", host, "250 2.0.0 Ok: queued as CE05D10261D", "14:00:19"})
	bounced := upRow("13:59:02", "invoice@partner-billing.example", "203.0.113.140", "owner@lists.example.org", "Outstanding invoice — action required",
		"accept", "rspamd", "add header, score 9.80 / 15.00; PHISHING(5.00) FORGED_SENDER(3.00) R_SPF_FAIL(1.00)",
		up{"bounced", host, "550 5.7.1 Message rejected as spam by the upstream filter", "13:59:04"})
	rest := []InLogRow{
		row("13:57:41", "promo@bulk.example", "198.51.100.90", "sales@lists.example.org", "",
			"reject", "postfix", "550 5.1.1 <sales@lists.example.org>: Recipient address rejected: User unknown in relay recipient table"),
		row("13:51:30", "billing@partner.example", "198.51.100.12", "owner@lists.example.org", "",
			"reject", "rspamd", "5.7.1 Spam message rejected"),
		row("13:44:02", "hr@partner.example", "192.0.2.31", "jobs@acme.io", "",
			"tempfail", "rspamd", "4.7.1 Try again later"),
		upRow("13:30:55", "noreply@github.example", "192.0.2.200", "security@lists.example.org", "[advisory] new report",
			"accept", "rspamd", "no action, score -98.10 / 15.00; SELFPOST_ALLOW_FROM(-100.00) R_DKIM_ALLOW(-0.20)",
			up{"deferred", "", "connect to mx.upstream.example[198.51.100.5]:25: Connection refused", "13:45:12"}),
	}
	// The newest row is a message not tried yet; it is the seventh written.
	queued := upRow("14:00:31", "ci@builds.example", "192.0.2.90", "dev@lists.example.org", "[ci] nightly build passed",
		"accept", "rspamd", "no action, score 1.10 / 15.00", up{status: "queued"})
	return p.WithRows(append([]InLogRow{queued, sent, bounced}, rest...), 1, 38)
}

// inFilterListsFixture is the mockup's Filter lists: three senders denied, two
// allowed, and a filter that reads them.
func inFilterListsFixture() *FilterLists {
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	at := func(d, h, m int) time.Time { return time.Date(2026, 9, d, h, m, 0, 0, time.UTC) }
	return NewFilterLists(admin(), FilterListsInput{FilterOn: true, LastWritten: time.Date(2026, 9, 11, 8, 40, 0, 0, time.UTC), RetentionDays: 90}).
		WithEntries([]FilterEntryInput{
			{ID: 1, List: "deny", Kind: "domain", Value: "bulk.example", Note: "newsletter nobody subscribed to", Added: day(9, 2), Hits: 41, LastHit: at(21, 13, 57)},
			{ID: 2, List: "deny", Kind: "ip", Value: "198.51.100.0/24", Note: "dictionary attack on lists.example.org", Added: day(8, 19), Hits: 1203, LastHit: at(21, 9, 12)},
			{ID: 3, List: "deny", Kind: "address", Value: "win@lottery.example", Added: day(7, 30)},
			{ID: 4, List: "allow", Kind: "domain", Value: "partner.example", Note: "invoices were scored as spam", Added: day(9, 11), Hits: 7, LastHit: at(19, 16, 40)},
			{ID: 5, List: "allow", Kind: "address", Value: "noreply@github.example", Note: "security advisories", Added: day(6, 4), Hits: 18, LastHit: at(21, 13, 30)},
		})
}
