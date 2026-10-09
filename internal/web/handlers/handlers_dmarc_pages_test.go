package handlers

import (
	"net/http"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/dmarc"
	"github.com/mixeme/selfpost/internal/store"
)

// The DMARC pages, rendered from the real stores and the handlers. Their markup
// is held to the mockups by the guards in internal/web/view; what is checked
// here is that the handlers fill the typed pages from what is stored and that
// each page reaches only what its viewer may see.

// dmarcStand is a panel with report ingest on and one report stored for the
// stand's domain: a relay that passed and a source that failed both checks.
type dmarcStand struct {
	appStand
	reportID int64
}

func newDMARCStand(t *testing.T) dmarcStand {
	t.Helper()
	s := dmarcStand{appStand: newAppStand(t)}
	s.h.cfg.DMARCEnabled, s.h.cfg.Hostname = true, "mail.example.org"
	s.h.dmarc = dmarc.NewService(s.h.store, nil, "mail.example.org", true)
	received := time.Now().UTC().Add(-2 * time.Hour)
	id, err := s.h.store.InsertDMARCReport(store.DMARCReport{
		Domain: "example.org", Reporter: "Outlook.com", ReportID: "5f1c9a0e7b2d4e61a3",
		PeriodBegin: received.Add(-30 * time.Hour), PeriodEnd: received.Add(-6 * time.Hour), ReceivedAt: received,
		ContactEmail: "noreply-dmarc@outlook.example", PolicyP: "none", PolicySP: "none", PolicyPct: 100,
		PolicyADKIM: "r", PolicyASPF: "r", PassCount: 1903, FailCount: 7, Recipient: "dmarc-reports@mail.example.org",
		Records: []store.DMARCReportRecord{
			{SourceIP: "203.0.113.25", Count: 1903, Disposition: "none", SPFResult: "pass", DKIMResult: "pass", HeaderFrom: "example.org"},
			{SourceIP: "198.51.100.44", Count: 7, Disposition: "none", SPFResult: "fail", DKIMResult: "fail", HeaderFrom: "example.org"},
		},
	})
	if err != nil {
		t.Fatalf("InsertDMARCReport: %v", err)
	}
	s.reportID = id
	return s
}

func TestDMARCHubShowsIngestAndRecentReports(t *testing.T) {
	s := newDMARCStand(t)
	body := getBody(t, s.h.HandleDMARCList, "/outbound/dmarc")
	has(t, "hub", body,
		`<h1 class="title is-3">DMARC reports</h1>`, `<h2>Ingest</h2>`, `<span class="tag is-success is-light">ok</span>`,
		`1 kept · 0 parse failures`, `dmarc-reports@mail.example.org`, `500 reports or 90 days`,
		`<h2>Recent reports</h2>`, `<a href="/outbound/dmarc/domains/1">example.org</a>`, `Outlook.com`,
		"<td>1\u00a0903</td>", `<span class="tag is-warning is-light">7</span>`, `href="/outbound/dmarc/reports/`+idStr(s.reportID)+`">View</a>`)
	lacks(t, "hub", body, `None yet`)
}

// Before the first report the page says how to get one rather than showing an
// empty table next to a healthy-looking stamp.
func TestDMARCHubBeforeTheFirstReport(t *testing.T) {
	s := newAppStand(t)
	s.h.cfg.DMARCEnabled, s.h.cfg.Hostname = true, "mail.example.org"
	s.h.dmarc = dmarc.NewService(s.h.store, nil, "mail.example.org", true)
	body := getBody(t, s.h.HandleDMARCList, "/outbound/dmarc")
	has(t, "hub", body, `<span class="tag is-warning is-light">quiet</span>`, `None yet`, `<code>rua=</code>`,
		`publish MX for <span class="sp-mono">mail.example.org</span>`, `No reports yet.`)
	lacks(t, "hub", body, `<table`)
}

// A report names its reporter and its domain in its own words, and what a
// remote sender chose to write is text on the page, never markup.
func TestDMARCPagesEscapeWhatReportersWrote(t *testing.T) {
	s := newDMARCStand(t)
	id, err := s.h.store.InsertDMARCReport(store.DMARCReport{
		Domain: "example.org", Reporter: `<script>alert(1)</script>`, ReportID: `"><img src=x>`,
		PeriodBegin: time.Now().Add(-48 * time.Hour), PeriodEnd: time.Now().Add(-24 * time.Hour), ReceivedAt: time.Now(),
		ContactEmail: `"><b>x</b>`, PolicyP: `<i>none</i>`, PassCount: 1, FailCount: 1,
		Records: []store.DMARCReportRecord{{SourceIP: `<u>1.2.3.4</u>`, Count: 2, Disposition: `<s>none</s>`, SPFResult: `<em>x`, DKIMResult: "fail", HeaderFrom: `<p>`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"hub":    getBody(t, s.h.HandleDMARCList, "/outbound/dmarc"),
		"domain": pageOf(t, s.h.HandleDMARCDomain, "/outbound/dmarc/domains/1"),
		"report": reportPage(t, s, id),
	} {
		lacks(t, name, body, `<script>alert`, `<img src=x>`, `<b>x</b>`, `<i>none</i>`, `<u>1.2.3.4`, `<s>none`, `<em>x`)
	}
	has(t, "report", reportPage(t, s, id), `&lt;script&gt;alert(1)&lt;/script&gt;`, `&lt;u&gt;1.2.3.4&lt;/u&gt;`)
}

func reportPage(t *testing.T, s dmarcStand, id int64) string {
	t.Helper()
	rec := send(s.h.HandleDMARCReport, &globalPrincipal, "GET", "/outbound/dmarc/reports/"+idStr(id), map[string]string{"id": idStr(id)}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("report %d = %d:\n%s", id, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// The page of a domain with sources: the table of where its mail came from, the
// stamp that says how much of it passed and the advice that follows. A source
// that is this server is told apart from one that is not.
func TestDMARCDomainShowsSourcesAndWhatToDoAboutThem(t *testing.T) {
	s := newDMARCStand(t)
	s.h.dns = offlineDNS
	body := pageOf(t, s.h.HandleDMARCDomain, "/outbound/dmarc/domains/1")
	has(t, "domain", body,
		`<h1 class="title is-3">example.org</h1>`, `<a href="/outbound/dmarc">DMARC reports</a> /`,
		`class="sp-postmark sp-warn"`, `<b>99.6%</b>`, `PASS · 7 DAYS`,
		"1\u00a0903 pass · 7 fail in the last 7 days. A third-party source is not aligned. Do not tighten <code>p=</code>",
		`href="/outbound/domains/1"`, `<h2>Sources · 7 days</h2>`, `198.51.100.44`, `203.0.113.25`, `<h2>Reports</h2>`,
		`href="/outbound/dmarc/reports/`+idStr(s.reportID)+`">View</a>`)
	// 198.51.100.44 passed nothing, so its disposition is amber.
	has(t, "domain", body, `<td>0</td><td>7</td><td><span class="tag is-warning is-light">none</span></td>`)
	lacks(t, "domain", body, `this relay`)
}

func TestDMARCDomainWithoutReportsSaysSo(t *testing.T) {
	s := newAppStand(t)
	s.h.cfg.DMARCEnabled, s.h.cfg.Hostname = true, "mail.example.org"
	s.h.dmarc = dmarc.NewService(s.h.store, nil, "mail.example.org", true)
	body := pageOf(t, s.h.HandleDMARCDomain, "/outbound/dmarc/domains/1")
	has(t, "domain", body, `No reports for this domain yet.`, `No mail from any source in this window.`,
		`0 pass · 0 fail in the last 7 days. No messages in the reporting window yet.`)
	lacks(t, "domain", body, `sp-postmark`, `<table`)
}

func TestDMARCReportShowsPolicyAndRecords(t *testing.T) {
	s := newDMARCStand(t)
	body := reportPage(t, s, s.reportID)
	has(t, "report", body,
		`<h1 class="title is-4">Outlook.com · `, `<a href="/outbound/dmarc">DMARC reports</a> /`, `<a href="/outbound/dmarc/domains/1">example.org</a> /`,
		"<span class=\"tag is-success is-light\">1\u00a0903 pass</span>", `<span class="tag is-warning is-light">7 fail</span>`,
		`5f1c9a0e7b2d4e61a3`, `noreply-dmarc@outlook.example`, ` UTC</dd>`,
		`none / none / 100`, `<dd class="sp-mono">r / r</dd>`, `dmarc-reports@mail.example.org`,
		`<h2>Records</h2>`, `Parsed from the aggregate XML`, `203.0.113.25`, `198.51.100.44`,
		`<span class="tag is-danger is-light">fail</span>`, `<span class="tag is-success is-light">pass</span>`)
}

// DMARC is a feature of the deployment: with ingest off none of its pages
// exists, whoever asks.
func TestDMARCPagesAreAbsentWithIngestOff(t *testing.T) {
	s := newDMARCStand(t)
	s.h.cfg.DMARCEnabled = false
	for name, h := range map[string]http.HandlerFunc{
		"hub": s.h.HandleDMARCList, "domain": s.h.HandleDMARCDomain, "report": s.h.HandleDMARCReport,
	} {
		rec := send(h, &globalPrincipal, "GET", "/outbound/dmarc", map[string]string{"id": "1"}, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s with ingest off = %d, want 404", name, rec.Code)
		}
	}
}

// A domain administrator reaches the reports of the domains assigned to them
// and nothing else: not the hub, not another tenant's domain page, not another
// tenant's report. A report that does not exist is the same 404 as one that is
// not theirs.
func TestDMARCPagesStayWithinTheViewersDomains(t *testing.T) {
	s := newDMARCStand(t)
	s.h.dns = offlineDNS
	other, err := s.h.store.AddDomain("other.example.net", "mail")
	if err != nil {
		t.Fatal(err)
	}
	otherReport, err := s.h.store.InsertDMARCReport(store.DMARCReport{
		Domain: other.Name, Reporter: "google.com", ReportID: "other", ReceivedAt: time.Now(),
		PeriodBegin: time.Now().Add(-24 * time.Hour), PeriodEnd: time.Now(), PassCount: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	ops := domainAdmin(t, s.h.store, "ops", s.d.ID)

	// Their own domain and report are theirs; the way back to the hub is not
	// offered, since the hub is not theirs.
	own := send(s.h.HandleDMARCDomain, &ops, "GET", "/outbound/dmarc/domains/1", map[string]string{"id": "1"}, nil)
	if own.Code != http.StatusOK {
		t.Fatalf("own domain = %d", own.Code)
	}
	has(t, "domain for its administrator", own.Body.String(), `<h1 class="title is-3">example.org</h1>`)
	lacks(t, "domain for its administrator", own.Body.String(), `<a href="/outbound/dmarc">`)
	rep := send(s.h.HandleDMARCReport, &ops, "GET", "/outbound/dmarc/reports/"+idStr(s.reportID), map[string]string{"id": idStr(s.reportID)}, nil)
	if rep.Code != http.StatusOK {
		t.Fatalf("own report = %d", rep.Code)
	}
	lacks(t, "report for its administrator", rep.Body.String(), `<a href="/outbound/dmarc">`)

	for name, code := range map[string]int{
		"hub": send(s.h.HandleDMARCList, &ops, "GET", "/outbound/dmarc", nil, nil).Code,
		"another tenant's domain": send(s.h.HandleDMARCDomain, &ops, "GET", "/outbound/dmarc/domains/"+idStr(other.ID),
			map[string]string{"id": idStr(other.ID)}, nil).Code,
		"another tenant's report": send(s.h.HandleDMARCReport, &ops, "GET", "/outbound/dmarc/reports/"+idStr(otherReport),
			map[string]string{"id": idStr(otherReport)}, nil).Code,
		"a report that does not exist": send(s.h.HandleDMARCReport, &ops, "GET", "/outbound/dmarc/reports/999",
			map[string]string{"id": "999"}, nil).Code,
	} {
		if code != http.StatusNotFound {
			t.Errorf("%s for a domain administrator = %d, want 404", name, code)
		}
	}
}
