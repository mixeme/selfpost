package view

// The data of the DMARC report pages: the hub (templates/dmarc.html, mockup
// dmarc.html), one sending domain (dmarc_domain.html, mockup dmarc-domain.html)
// and one aggregate report (dmarc_report.html, mockup dmarc-report.html).
//
// A page is a typed struct that embeds Meta, built by a constructor and filled
// by With… methods; the handler reads the report store, the view decides what is
// said and how it is worded.

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const outboundDMARC = "/outbound/dmarc"

// DMARCDomainHref is the address of a sending domain's reports.
func DMARCDomainHref(id int64) string { return outboundDMARC + "/domains/" + strconv.FormatInt(id, 10) }

// DMARCReportHref is the address of one report.
func DMARCReportHref(id int64) string { return outboundDMARC + "/reports/" + strconv.FormatInt(id, 10) }

// FormatReceived is the time a report was received, as the tables show it.
func FormatReceived(t time.Time) string { return t.UTC().Format("01-02 15:04") }

// ---- The hub

// DMARCHub is the data of the DMARC reports hub: whether reports are arriving
// and the latest of them. The ingest box describes the whole instance (the
// hosted mailbox, the reports kept across every domain), so it is drawn only
// for the viewer it belongs to — see WithoutIngest.
type DMARCHub struct {
	Meta
	Head Head

	IngestOn    bool
	Ingest      Box
	IngestFacts []Fact

	Recent Box
	Rows   []DMARCReportRow
	Empty  EmptyState
}

// DMARCReportRow is one report in a table: when it was received, for which
// domain (DomainHref is empty when the domain is gone or not the viewer's), who
// sent it, the window it covers and its pass and fail counts. Domain is left
// out on the page of one domain.
type DMARCReportRow struct {
	Received   string
	Domain     string
	DomainHref string
	Reporter   string
	Window     string
	Pass       int
	Fail       int
	Href       string
}

// PassText is the pass count as the table writes it.
func (r DMARCReportRow) PassText() string { return FormatCount(int64(r.Pass)) }

// FailTag is the fail count as a tag: failures are the thing to look at, so a
// non-zero count is amber and a zero is plain text.
func (r DMARCReportRow) FailTag() Tag {
	return Tag{Status: "warn", Label: FormatCount(int64(r.Fail))}
}

// IngestInput is the state of report ingest: whether it is healthy, when the
// last report arrived (zero when none has), what the week kept, the address
// reports can be sent to and how long they are kept.
type IngestInput struct {
	OK            bool
	Last          time.Time
	KeptThisWeek  int
	ParseFailures int
	Hosted        string
	// Host is the name that must have an MX record on this server for the hosted
	// address to receive anything.
	Host          string
	RetentionMax  int
	RetentionDays int
}

// NewDMARCHub builds the hub.
func NewDMARCHub(m Meta, in IngestInput) *DMARCHub {
	m.Title, m.Section, m.Page = "DMARC reports", "outbound", "dmarc"
	status, label := "ok", "ok"
	if !in.OK {
		status, label = "quiet", "quiet"
	}
	last := Fact{Label: "Last report", Value: Plain("None yet")}
	if in.Last.IsZero() {
		last.Note = Rich("Point ", Code("rua="), " at the hosted address below and publish MX for ", Mono(in.Host), " on this server.")
	} else {
		last.Value = Plain(in.Last.UTC().Format("2006-01-02 15:04") + " UTC")
	}
	return &DMARCHub{
		Meta: m,
		Head: Head{
			Kicker: "Outbound",
			Title:  "DMARC reports",
			Lead: Rich("How receivers saw mail claiming to be from your domains. Aggregate (", Code("rua="),
				") only; open a report for the parsed XML."),
		},
		IngestOn: true,
		Ingest:   Box{No: "01", Title: "Ingest", End: Rich(TagOf(status, label)), Help: topicLink(HelpDMARC, "What the reports are and where they go")},
		IngestFacts: []Fact{
			last,
			{Label: "This week", Value: Plain(FormatCount(int64(in.KeptThisWeek)) + " kept · " + plural(int64(in.ParseFailures), "parse failure", "parse failures"))},
			{Label: "Hosted addresses", Value: Plain(in.Hosted), Mono: true},
			{Label: "Retention", Value: Plain(FormatCount(int64(in.RetentionMax)) + " reports or " + plural(int64(in.RetentionDays), "day", "days"))},
		},
		Recent: Box{No: "02", Title: "Recent reports"},
		Empty:  EmptyState{Icon: "ti-report-analytics", Text: Plain("No reports yet.")},
	}
}

// WithoutIngest leaves the ingest box out, for a viewer who reaches some of the
// domains and not the instance: the box is about all of them, so what it says
// is not theirs to read. The reports box is then the page's first and only one.
func (p *DMARCHub) WithoutIngest() *DMARCHub {
	p.IngestOn = false
	p.Recent.No = "01"
	p.Recent.Help = p.Ingest.Help
	return p
}

// WithReports sets the table of the latest reports.
func (p *DMARCHub) WithReports(rows []DMARCReportRow) *DMARCHub {
	p.Rows = rows
	return p
}

// ---- One sending domain

// DMARCDomain is the data of a domain's reports: how its mail fared over the
// window, which sources it came from and the reports that said so.
type DMARCDomain struct {
	Meta
	Head Head

	Sources      Box
	SourceRows   []DMARCSourceRow
	SourcesEmpty EmptyState

	Reports      Box
	Rows         []DMARCReportRow
	ReportsEmpty EmptyState
}

// DMARCSourceRow is one sending address over the window: its pass and fail
// counts and what receivers did with its mail. ThisRelay marks the server the
// panel runs on.
type DMARCSourceRow struct {
	Source      string
	ThisRelay   bool
	Pass        int
	Fail        int
	Disposition string
}

// PassText is the pass count as the table writes it.
func (r DMARCSourceRow) PassText() string { return FormatCount(int64(r.Pass)) }

// FailText is the fail count as the table writes it.
func (r DMARCSourceRow) FailText() string { return FormatCount(int64(r.Fail)) }

// Relay is the tag of the row that is this server.
func (r DMARCSourceRow) Relay() Tag { return Tag{Label: "this relay"} }

// Verdict is the disposition as a tag: green while the source passes, amber
// once it fails.
func (r DMARCSourceRow) Verdict() Tag {
	status := "ok"
	if r.Fail > 0 {
		status = "warn"
	}
	return Tag{Status: status, Label: r.Disposition}
}

// passRate writes the share of messages that passed as the postmark prints it.
// It rounds down: a domain that has failures is never stamped 100%.
func passRate(pass, fail int) string {
	if fail == 0 {
		return "100%"
	}
	total := float64(pass + fail)
	return fmt.Sprintf("%.1f%%", math.Floor(float64(pass)*1000/total)/10)
}

// withCode turns every p= in a sentence into a code span.
func withCode(s string) Text {
	var t Text
	for i, part := range strings.Split(s, "p=") {
		if i > 0 {
			t = append(t, Code("p="))
		}
		if part != "" {
			t = append(t, Inline{Text: part})
		}
	}
	return t
}

// NewDMARCDomain builds the page of a domain's reports. pass and fail are the
// messages of the last days days, hint the advice on tightening p= that follows
// from them and the sources. hub is whether the viewer reaches the hub: the
// trail leads back to it only then.
func NewDMARCDomain(m Meta, id int64, name string, hub bool, pass, fail, days int, hint string) *DMARCDomain {
	m.Title, m.Section, m.Page = name+" · DMARC reports", "outbound", "dmarc"
	crumb := Crumb{Label: "DMARC reports"}
	if hub {
		crumb.Href = outboundDMARC
	}
	window := plural(int64(days), "day", "days")
	p := &DMARCDomain{
		Meta: m,
		Head: Head{
			Crumbs: []Crumb{crumb},
			Title:  name,
			Lead: append(Rich(FormatCount(int64(pass))+" pass · "+FormatCount(int64(fail))+" fail in the last "+window+". "),
				withCode(hint)...),
			Actions: []Action{{Label: "Domain page", Icon: "ti-world-upload", Href: DomainHref(id)}},
		},
		Sources:      Box{No: "01", Title: fmt.Sprintf("Sources · %d days", days), Help: topicLink(HelpDMARC, "How to read pass, fail and sources")},
		SourcesEmpty: EmptyState{Icon: "ti-world-upload", Text: Plain("No mail from any source in this window.")},
		Reports:      Box{No: "02", Title: "Reports"},
		ReportsEmpty: EmptyState{Icon: "ti-report-analytics", Text: Plain("No reports for this domain yet.")},
	}
	if pass+fail > 0 {
		level := LevelOK
		switch {
		case pass == 0:
			level = LevelFail
		case fail > 0:
			level = LevelWarn
		}
		p.Head.Postmark = &Postmark{Top: "DMARC", Word: passRate(pass, fail), Bottom: fmt.Sprintf("PASS · %d DAYS", days), Level: level}
	}
	return p
}

// WithSources sets the table of the sources.
func (p *DMARCDomain) WithSources(rows []DMARCSourceRow) *DMARCDomain {
	p.SourceRows = rows
	return p
}

// WithReports sets the table of the domain's reports.
func (p *DMARCDomain) WithReports(rows []DMARCReportRow) *DMARCDomain {
	p.Rows = rows
	return p
}

// ---- One report

// DMARCReport is the data of one aggregate report: who sent it and when, the
// policy it was written against and the records it holds.
type DMARCReport struct {
	Meta
	Head Head

	Report      Box
	ReportFacts []Fact

	Policy      Box
	PolicyFacts []Fact

	Records      Box
	RecordRows   []DMARCRecordRow
	RecordsEmpty EmptyState
}

// DMARCRecordRow is one source in a report: how many messages, what receivers
// did with them and what SPF and DKIM said.
type DMARCRecordRow struct {
	Source      string
	ThisRelay   bool
	Count       int
	Disposition string
	SPF         string
	DKIM        string
	HeaderFrom  string
}

// CountText is the message count as the table writes it.
func (r DMARCRecordRow) CountText() string { return FormatCount(int64(r.Count)) }

// Relay is the tag of the row that is this server.
func (r DMARCRecordRow) Relay() Tag { return Tag{Label: "this relay"} }

// Verdict is the disposition as a tag: green when either check passed, amber
// when neither did.
func (r DMARCRecordRow) Verdict() Tag {
	status := "warn"
	if r.SPF == "pass" || r.DKIM == "pass" {
		status = "ok"
	}
	return Tag{Status: status, Label: r.Disposition}
}

// SPFTag is the SPF result as a tag.
func (r DMARCRecordRow) SPFTag() Tag { return resultTag(r.SPF) }

// DKIMTag is the DKIM result as a tag.
func (r DMARCRecordRow) DKIMTag() Tag { return resultTag(r.DKIM) }

// resultTag is a check's result as a tag: green for pass, red for anything else.
func resultTag(result string) Tag {
	if result == "pass" {
		return Tag{Status: "ok", Label: result}
	}
	return Tag{Status: "fail", Label: result}
}

// ReportInput is what a report holds, with the times already written the way
// the page shows them.
type ReportInput struct {
	ID          int64
	Reporter    string
	ReportID    string
	Domain      string
	DomainID    int64
	Contact     string
	Pass, Fail  int
	Window      string // the window in the title: "20 Sep"
	Period      string // the window with its times: "20 Sep 00:00 – 21 Sep 00:00 UTC"
	Received    string // "2026-09-21 04:40 UTC"
	PolicyP     string
	PolicySP    string
	PolicyPct   int
	PolicyADKIM string
	PolicyASPF  string
	Recipient   string
	// Hub is whether the viewer reaches the hub: the trail leads back to it only then.
	Hub bool
}

// NewDMARCReport builds the page of one report.
func NewDMARCReport(m Meta, in ReportInput) *DMARCReport {
	title := in.Reporter
	if in.Window != "" {
		title += " · " + in.Window
	}
	m.Title, m.Section, m.Page = title+" · DMARC", "outbound", "dmarc"
	hub := Crumb{Label: "DMARC reports"}
	if in.Hub {
		hub.Href = outboundDMARC
	}
	route := Route{{Text: in.Domain}, {Tag: &Tag{Status: "ok", Label: FormatCount(int64(in.Pass)) + " pass"}}}
	if in.Fail > 0 {
		route = append(route, RoutePart{Tag: &Tag{Status: "warn", Label: FormatCount(int64(in.Fail)) + " fail"}})
	}
	report := []Fact{
		{Label: "Reporter", Value: Plain(in.Reporter)},
		{Label: "Report id", Value: Plain(in.ReportID), Mono: true, Small: true},
		{Label: "Window", Value: Plain(in.Period)},
		{Label: "Received", Value: Plain(in.Received)},
	}
	if in.Contact != "" {
		report = append(report, Fact{Label: "Contact", Value: Plain(in.Contact)})
	}
	policy := []Fact{
		{Label: "p / sp / pct", Value: Plain(fmt.Sprintf("%s / %s / %d", in.PolicyP, in.PolicySP, in.PolicyPct)), Mono: true},
		{Label: "adkim / aspf", Value: Plain(in.PolicyADKIM + " / " + in.PolicyASPF), Mono: true},
	}
	if in.Recipient != "" {
		policy = append(policy, Fact{Label: "rua", Value: Plain(in.Recipient), Mono: true, Small: true})
	}
	return &DMARCReport{
		Meta: m,
		Head: Head{
			Crumbs:  []Crumb{hub, {Href: DMARCDomainHref(in.DomainID), Label: in.Domain}},
			Title:   title,
			Compact: true,
			Route:   route,
		},
		Report:       Box{No: "01", Title: "Report", Help: topicLink(HelpDMARC, "What an aggregate report is")},
		ReportFacts:  report,
		Policy:       Box{No: "02", Title: "Published policy"},
		PolicyFacts:  policy,
		Records:      Box{No: "03", Title: "Records", End: Plain("Parsed from the aggregate XML")},
		RecordsEmpty: EmptyState{Icon: "ti-report-analytics", Text: Plain("This report holds no records.")},
	}
}

// WithRecords sets the table of the report's records.
func (p *DMARCReport) WithRecords(rows []DMARCRecordRow) *DMARCReport {
	p.RecordRows = rows
	return p
}
