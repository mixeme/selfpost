package view

import (
	"testing"
	"time"
)

// ---- The hub

func TestDMARCHubShowsIngestAndTheLatestReports(t *testing.T) {
	out := renderSignedIn(t, "dmarc", dmarcHubFixture())
	pageHas(t, "DMARC reports", out,
		`<h1 class="title is-3">DMARC reports</h1>`, `<code>rua=</code>`,
		`<h2>Ingest</h2>`, `<span class="tag is-success is-light">ok</span>`,
		`<dd>2026-09-21 06:12 UTC</dd>`, `<dd>14 kept · 0 parse failures</dd>`, `<dd class="sp-mono">dmarc@mail.example.org</dd>`, `<dd>500 reports or 90 days</dd>`,
		`<th>Received, UTC</th>`, `09-21 06:12`, `<a href="/outbound/dmarc/domains/1">example.org</a>`,
		"<td>1 903</td>", `<span class="tag is-warning is-light">7</span>`, `<td>0</td><td class="sp-actions"><a href="/outbound/dmarc/reports/41">View</a>`,
	)
}

// Before the first report the box says how to get one, and a quiet ingest is
// amber.
func TestDMARCHubWithoutReports(t *testing.T) {
	p := NewDMARCHub(admin(), IngestInput{Hosted: "dmarc@mail.example.org", Host: "mail.example.org", RetentionMax: 500, RetentionDays: 90})
	out := renderSignedIn(t, "dmarc", p.WithReports(nil))
	pageHas(t, "DMARC reports", out, `<span class="tag is-warning is-light">quiet</span>`, `<dd>None yet</dd>`,
		`publish MX for <span class="sp-mono">mail.example.org</span> on this server`, `No reports yet.`, `0 kept · 0 parse failures`)
	pageLacks(t, "DMARC reports", out, `<table`, `UTC</dd>`)

	one := NewDMARCHub(admin(), IngestInput{OK: true, Last: time.Now(), KeptThisWeek: 1, ParseFailures: 1})
	pageHas(t, "DMARC reports", renderSignedIn(t, "dmarc", one), `1 kept · 1 parse failure</dd>`)
}

// A report of a domain the viewer cannot open is not a link.
func TestDMARCHubLinksOnlyDomainsThatExist(t *testing.T) {
	p := NewDMARCHub(admin(), IngestInput{}).WithReports([]DMARCReportRow{
		{Received: "09-21 06:12", Domain: "gone.example", Reporter: "google.com", Window: "20 Sep", Pass: 1, Href: "/outbound/dmarc/reports/1"},
	})
	out := renderSignedIn(t, "dmarc", p)
	pageHas(t, "DMARC reports", out, `<td>gone.example</td>`)
	pageLacks(t, "DMARC reports", out, `/outbound/dmarc/domains/`)
}

// ---- One sending domain

// The stamp, the lead and the tables say the same thing about the window: how
// much passed, what to do about what did not.
func TestDMARCDomainStampAndLead(t *testing.T) {
	out := renderSignedIn(t, "dmarc-domain", dmarcDomainFixture())
	pageHas(t, "DMARC domain", out,
		`<h1 class="title is-3">shop.example.org</h1>`, `<a href="/outbound/dmarc">DMARC reports</a> /`,
		`class="sp-postmark sp-warn"`, `<span>DMARC</span><b>99.6%</b><span>PASS · 7 DAYS</span>`,
		"11 204 pass · 41 fail in the last 7 days. A third-party source is not aligned. Do not tighten <code>p=</code> until that sender is fixed or removed.",
		`<a class="button" href="/outbound/domains/2"><i class="ti ti-world-upload"></i>Domain page</a>`,
		`<h2>Sources · 7 days</h2>`, `203.0.113.25 <span class="tag is-light">this relay</span>`,
		`<td>0</td><td>41</td><td><span class="tag is-warning is-light">none</span></td>`,
		`<td><span class="tag is-success is-light">none</span></td>`, `<h2>Reports</h2>`, `<td class="sp-muted">Outlook.com</td>`,
	)

	for _, c := range []struct {
		pass, fail int
		level      string
		word       string
	}{
		{500, 0, LevelOK, "100%"}, {9999, 1, LevelWarn, "99.9%"}, {1, 99999, LevelWarn, "0.0%"}, {0, 5, LevelFail, "0.0%"},
	} {
		p := NewDMARCDomain(admin(), 1, "example.org", true, c.pass, c.fail, 7, "")
		if m := p.Head.Postmark; m == nil || m.Level != c.level || m.Word != c.word {
			t.Errorf("%d pass, %d fail are stamped %+v, want %s %s", c.pass, c.fail, m, c.level, c.word)
		}
	}
	// Rounding never turns a failure into a clean 100%.
	if got := passRate(99999, 1); got != "99.9%" {
		t.Errorf("99999 of 100000 is stamped %s", got)
	}
}

// With nothing in the window there is nothing to stamp; and a viewer who does not
// reach the hub is not offered the way back to it.
func TestDMARCDomainWithoutMailOrWithoutHub(t *testing.T) {
	p := NewDMARCDomain(Meta{User: "ops", HasOutbound: true}, 1, "example.org", false, 0, 0, 7, "No messages in the reporting window yet.")
	out := renderSignedIn(t, "dmarc-domain", p.WithSources(nil).WithReports(nil))
	pageHas(t, "DMARC domain", out, `<p class="sp-kicker">DMARC reports /</p>`, `0 pass · 0 fail in the last 7 days. No messages in the reporting window yet.`,
		`No mail from any source in this window.`, `No reports for this domain yet.`)
	pageLacks(t, "DMARC domain", out, `sp-postmark`, `<table`, `<a href="/outbound/dmarc">`)
}

func TestWithCodeOnlyMarksP(t *testing.T) {
	out := renderSignedIn(t, "dmarc-domain", NewDMARCDomain(admin(), 1, "example.org", true, 1, 0, 7, "Raise p= to quarantine, then p=reject.").WithSources(nil).WithReports(nil))
	pageHas(t, "DMARC domain", out, `Raise <code>p=</code> to quarantine, then <code>p=</code>reject.`)
}

// ---- One report

func TestDMARCReportShowsTheReportItsPolicyAndRecords(t *testing.T) {
	out := renderSignedIn(t, "dmarc-report", dmarcReportFixture())
	pageHas(t, "DMARC report", out,
		`<h1 class="title is-4">Outlook.com · 20 Sep</h1>`,
		`<a href="/outbound/dmarc">DMARC reports</a> / <a href="/outbound/dmarc/domains/2">shop.example.org</a> /`,
		"<span>shop.example.org</span><span class=\"tag is-success is-light\">1 903 pass</span><span class=\"tag is-warning is-light\">7 fail</span>",
		`<h2>Report</h2>`, `<dd class="sp-mono sp-small">5f1c9a0e7b2d4e61a3</dd>`, `<dd>20 Sep 00:00 – 21 Sep 00:00 UTC</dd>`, `<dd>2026-09-21 04:40 UTC</dd>`,
		`<h2>Published policy</h2>`, `<dd class="sp-mono">none / none / 100</dd>`, `<dd class="sp-mono">r / r</dd>`, `<dd class="sp-mono sp-small">dmarc@mail.example.org</dd>`,
		`<h2>Records</h2>`, `Parsed from the aggregate XML`,
		`203.0.113.25 <span class="tag is-light">this relay</span>`, "<td>1 903</td>",
		`<td><span class="tag is-success is-light">pass</span></td><td><span class="tag is-success is-light">pass</span></td>`,
		`<td><span class="tag is-warning is-light">none</span></td><td><span class="tag is-danger is-light">fail</span></td><td><span class="tag is-danger is-light">fail</span></td>`,
	)
}

// What a report does not hold is not drawn: no contact, no rua, no fail tag when
// nothing failed, and a report with no records says so.
func TestDMARCReportOmitsWhatItDoesNotHold(t *testing.T) {
	p := NewDMARCReport(admin(), ReportInput{ID: 1, Reporter: "google.com", ReportID: "r1", Domain: "example.org", DomainID: 1, Pass: 5,
		Period: "20 Sep 00:00 – 20 Sep 23:59 UTC", Received: "2026-09-21 04:40 UTC", PolicyP: "none", PolicySP: "none", PolicyPct: 100, PolicyADKIM: "r", PolicyASPF: "r"})
	out := renderSignedIn(t, "dmarc-report", p.WithRecords(nil))
	pageHas(t, "DMARC report", out, `<h1 class="title is-4">google.com</h1>`, `<span class="tag is-success is-light">5 pass</span>`, `This report holds no records.`)
	pageLacks(t, "DMARC report", out, `fail</span>`, `Contact`, `<dt>rua</dt>`, `<table`, `<a href="/outbound/dmarc">`)
}

func TestDMARCPagesEscapeWhatReportersWrote(t *testing.T) {
	evil := `<script>alert(1)</script>`
	hub := NewDMARCHub(admin(), IngestInput{Hosted: evil}).WithReports([]DMARCReportRow{{Domain: evil, Reporter: evil, Window: evil, Received: evil, Href: "/x"}})
	dom := NewDMARCDomain(admin(), 1, evil, true, 1, 1, 7, evil).
		WithSources([]DMARCSourceRow{{Source: evil, Disposition: evil}}).WithReports([]DMARCReportRow{{Reporter: evil, Href: "/x"}})
	rep := NewDMARCReport(admin(), ReportInput{Reporter: evil, ReportID: evil, Domain: evil, Contact: evil, Period: evil, Received: evil,
		PolicyP: evil, PolicyADKIM: evil, Recipient: evil}).
		WithRecords([]DMARCRecordRow{{Source: evil, Disposition: evil, SPF: evil, DKIM: evil, HeaderFrom: evil}})
	for name, c := range map[string]struct {
		page string
		data any
	}{"hub": {"dmarc", hub}, "domain": {"dmarc-domain", dom}, "report": {"dmarc-report", rep}} {
		pageLacks(t, name, renderSignedIn(t, c.page, c.data), `<script>alert`)
	}
}
