package view

// The data of the Outbound log pages: the list of messages
// (templates/out_log.html, mockup out-log.html) and one message
// (out_message.html, mockup out-message.html). The list polls: the table and
// its foot are one region (templates/out_log_rows.html) that the page and its
// fragment, GET /outbound/log/fragment, render alike.
//
// A page is a typed struct that embeds Meta, built by a constructor and filled
// by With… methods; the handler reads the journal, the view decides what is
// said and how it is worded.

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

const outboundLog = "/outbound/log"

// logQuery is the query string of a page of the log: the filters and the page,
// each left out when it says nothing, so the first unfiltered page is the bare
// path.
func logQuery(domain, app string, page int) string {
	q := url.Values{}
	if domain != "" {
		q.Set("domain", domain)
	}
	if app != "" {
		q.Set("app", app)
	}
	if page > 1 {
		q.Set("p", strconv.Itoa(page))
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// LogHref is the address of a page of the Outbound log.
func LogHref(domain, app string, page int) string {
	return outboundLog + logQuery(domain, app, page)
}

// FormatLogTime is the time of a row of the log: month, day and the time of day
// in UTC, as the table's "Time, UTC" column shows it.
func FormatLogTime(t time.Time) string { return t.UTC().Format("01-02 15:04:05") }

// formatStamp is a moment on the page of one message, with its zone.
func formatStamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05") + " UTC" }

// ---- The log

// OutLog is the data of the Outbound log: the messages of the domains the
// viewer reaches, filtered by domain and application, a page at a time.
type OutLog struct {
	Meta
	Head Head

	Messages Box
	// PollURL is the address of the fragment that answers with the table again
	// for the same filters and page (the polled region is the table, not the box:
	// a filter being chosen in the head must not be reset under the hand).
	PollURL string
	Rows    []OutLogRow
	Empty   EmptyState
	Foot    Foot

	domain, app string
}

// OutLogRow is one message of the table: when it was accepted, who it was from
// and to, its subject (shown whole in the cell's title when the column clips
// it), its status as a tag and the way to the message's own page.
type OutLogRow struct {
	Time    string
	From    string
	To      string
	Subject string
	Status  Tag
	Href    string
}

// NewOutLog builds the log for a viewer. domains and apps are the choices of
// the two filters (only what the viewer reaches), domain and app the ones in
// force. retentionDays is how long rows are kept; canSettings, the global role,
// adds the way to change it.
func NewOutLog(m Meta, retentionDays int, canSettings bool, domains, apps []string, domain, app string) *OutLog {
	m.Title, m.Section, m.Page = "Outbound log", "outbound", "log"
	lead := Rich("Every message an application handed to the relay. Rows older than " + plural(int64(retentionDays), "day", "days") + " are deleted")
	if canSettings {
		lead = append(lead, Inline{Text: " — change that in "}, Link("/server/settings", "Settings"))
	}
	lead = append(lead, Inline{Text: "."})
	options := func(any string, values []string) []Option {
		out := []Option{{Value: "", Label: any}}
		for _, v := range values {
			out = append(out, Option{Value: v, Label: v})
		}
		return out
	}
	return &OutLog{
		Meta: m,
		Head: Head{Kicker: "Outbound", Title: "Outbound log", Lead: lead},
		Messages: Box{No: "01", Title: "Messages", Help: topicLink(HelpLog, "What the statuses mean"), Filter: &Filter{
			Action: outboundLog,
			Selects: []Select{
				{Name: "domain", Label: "Domain", Options: options("All domains", domains), Selected: domain},
				{Name: "app", Label: "Application", Options: options("All applications", apps), Selected: app},
			},
		}},
		Empty:   EmptyState{Icon: "ti-list-details", Text: Plain("No messages logged yet.")},
		PollURL: outboundLog + "/fragment" + logQuery(domain, app, 1),
		Foot:    Foot{ID: "out-log-foot", End: Plain("New rows appear on their own")},
		domain:  domain, app: app,
	}
}

// DetailHref is the address of one message's page, carrying the filters and the
// page it is opened from so that its way back returns to the same list.
func (p *OutLog) DetailHref(id int64, page int) string {
	return outboundLog + "/" + strconv.FormatInt(id, 10) + logQuery(p.domain, p.app, page)
}

// WithRows sets the table for page of last, and the foot under it: where the
// viewer is, and the way to the newer and older rows. The poll asks for the same
// page again.
func (p *OutLog) WithRows(rows []OutLogRow, page, last int) *OutLog {
	p.Rows = rows
	p.PollURL = outboundLog + "/fragment" + logQuery(p.domain, p.app, page)
	if len(rows) == 0 {
		return p
	}
	text := Rich("Page " + strconv.Itoa(page) + " of " + strconv.Itoa(last))
	if page > 1 {
		text = append(text, Inline{Text: " · "}, Link(LogHref(p.domain, p.app, page-1), "← Newer"))
	}
	p.Foot.Text = text
	if page < last {
		p.Foot.Link = &Anchor{Href: LogHref(p.domain, p.app, page+1), Label: "Older →"}
	}
	return p
}

// Fragment marks the foot to be swapped into the page by its id: the polled
// fragment is the table, and the count under it follows out of band.
func (p *OutLog) Fragment() *OutLog {
	p.Foot.OOB = true
	return p
}

// ---- One message

// MessageInput is what the journal holds about one message and the pages it can
// be linked to. DomainHref and AppHref are empty when the domain or the
// application no longer exists, or the viewer does not reach it: the name is
// then plain text.
type MessageInput struct {
	ID         int64
	QueueID    string
	Domain     string
	DomainHref string
	App        string
	AppHref    string
	From       string
	To         string
	Subject    string
	Status     string
	Accepted   time.Time
	Reported   time.Time
	// BackHref is the log the message was opened from, filters and page kept.
	BackHref string
}

// OutMessage is the data of one message's page: what the journal recorded, the
// steps it went through and what Postfix wrote about it.
type OutMessage struct {
	Meta
	Head Head

	Message Box
	Facts   []Fact

	History Box
	Steps   []Step

	Delivery Box
	LogLines []LogLine
	// LogEmpty says why there are no lines: a message that never reached the
	// queue, or whose lines have been rotated away, is an ordinary outcome.
	LogEmpty EmptyState
}

// dash is the value of a field a row does not have.
const dash = "—"

func orDash(s string) string {
	if s == "" {
		return dash
	}
	return s
}

// NewOutMessage builds the page of one message. Its name is its subject, and
// the line under it is who it was from and to with its status.
func NewOutMessage(m Meta, in MessageInput) *OutMessage {
	title := in.Subject
	if title == "" {
		title = "(no subject)"
	}
	m.Title, m.Section, m.Page = title+" · Outbound log", "outbound", "log"

	value := func(text, href string) Text {
		if href == "" {
			return Plain(orDash(text))
		}
		return Rich(Link(href, text))
	}
	return &OutMessage{
		Meta: m,
		Head: Head{
			Crumbs:  []Crumb{{Href: in.BackHref, Label: "Outbound log"}},
			Title:   title,
			Compact: true,
			Route: Route{
				{Text: orDash(in.From)}, {Arrow: true}, {Text: orDash(in.To)},
				{Tag: &Tag{Status: in.Status}},
			},
		},
		Message: Box{No: "01", Title: "Message"},
		Facts: []Fact{
			{Label: "Domain", Value: value(in.Domain, in.DomainHref)},
			{Label: "Application", Value: value(in.App, in.AppHref), Mono: true},
			{Label: "Accepted", Value: Plain(formatStamp(in.Accepted))},
			{Label: "Status reported", Value: Plain(formatStamp(in.Reported))},
			{Label: "Queue id", Value: Plain(orDash(in.QueueID)), Mono: true},
			{Label: "Journal id", Value: Plain(strconv.FormatInt(in.ID, 10)), Mono: true},
		},
		History:  Box{No: "02", Title: "History", Help: topicLink(HelpLog, "What the statuses mean")},
		Delivery: Box{No: "03", Title: "Delivery log", End: Rich("Lines from ", Code("mail.log"), " with this queue id")},
		LogEmpty: EmptyState{Icon: "ti-file-text"},
	}
}

// WithHistory sets the steps the message went through, oldest first.
func (p *OutMessage) WithHistory(steps []Step) *OutMessage {
	p.Steps = steps
	return p
}

// WithDeliveryLog sets the lines Postfix wrote about the message. When there are
// none, note says why.
func (p *OutMessage) WithDeliveryLog(lines []LogLine, note string) *OutMessage {
	p.LogLines = lines
	p.LogEmpty.Text = Plain(note)
	return p
}

// StepTime is the time of a step of the history: the moment, or "not yet" for a
// step that has not happened.
func StepTime(t time.Time) string {
	if t.IsZero() {
		return "not yet"
	}
	return formatStamp(t)
}

// StepLevel is the level of a history step from the level of its outcome (ok,
// warn, error or anything else for "nothing to report"): a step that has not
// happened is pending whatever its outcome.
func StepLevel(outcome string, happened bool) string {
	switch {
	case !happened:
		return LevelPending
	case outcome == "warn":
		return LevelWarn
	case outcome == "error":
		return LevelFail
	}
	return ""
}

// LogLineLevel is the level a line of the delivery log is drawn at: a delivery
// that was put off is amber, one that failed for good red. Postfix writes the
// outcome of an attempt as status=… in the line itself.
func LogLineLevel(text string) string {
	switch {
	case strings.Contains(text, "status=bounced"), strings.Contains(text, "status=expired"),
		strings.Contains(text, " fatal:"), strings.Contains(text, " error:"):
		return "error"
	case strings.Contains(text, "status=deferred"), strings.Contains(text, " warning:"):
		return "warn"
	}
	return ""
}
