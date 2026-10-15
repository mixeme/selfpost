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
	"regexp"
	"strconv"
	"strings"
	"time"
)

const outboundLog = "/outbound/log"

// OutLogFilter is what the filters of the Outbound log are set to, as the
// strings the form sends: a domain, an application, a status of a row
// (OutLogStatuses) and two days (2006-01-02). Empty means no narrowing.
type OutLogFilter struct {
	Domain string
	App    string
	Status string
	From   string
	To     string
}

// OutLogStatuses are the statuses a row of the log has, the choices of the
// status filter: rejected is the three kinds of refusal together - a limit,
// a client address, a From: line - which the table tells apart by their tags
// (owner, 2026-10-13: the list without the split).
var OutLogStatuses = []string{"queued", "sent", "deferred", "bounced", "rejected"}

// query is the query string of a page of the log: the filters and the page,
// each left out when it says nothing, so the first unfiltered page is the bare
// path.
func (f OutLogFilter) query(page int) string {
	q := url.Values{}
	for name, v := range map[string]string{"domain": f.Domain, "app": f.App, "status": f.Status, "from": f.From, "to": f.To} {
		if v != "" {
			q.Set(name, v)
		}
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
func LogHref(f OutLogFilter, page int) string {
	return outboundLog + f.query(page)
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

	filter OutLogFilter
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
// those two filters (only what the viewer reaches), f the filters in force. retentionDays is how long rows are kept; canSettings, the global role,
// adds the way to change it.
func NewOutLog(m Meta, retentionDays int, canSettings bool, domains, apps []string, f OutLogFilter) *OutLog {
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
	p := &OutLog{
		Meta: m,
		Head: Head{Kicker: "Outbound", Title: "Outbound log", Lead: lead},
		Messages: Box{No: "01", Title: "Messages", Help: topicLink(HelpLog, "What the statuses mean"), Filter: &Filter{
			Action: outboundLog,
			Selects: []Select{
				{Name: "domain", Label: "Domain", Options: options("All domains", domains), Selected: f.Domain},
				{Name: "app", Label: "Application", Options: options("All applications", apps), Selected: f.App},
				{Name: "status", Label: "Status", Options: options("Any status", OutLogStatuses), Selected: f.Status},
			},
			Dates: []DateInput{
				{Name: "from", Label: "From date", Value: f.From},
				{Name: "to", Label: "To date", Value: f.To},
			},
		}},
		Empty:   EmptyState{Icon: "ti-list-details", Text: Plain("No messages logged yet.")},
		PollURL: outboundLog + "/fragment" + f.query(1),
		Foot:    Foot{ID: "out-log-foot", End: Plain("New rows appear on their own")},
		filter:  f,
	}
	// Filtered to nothing is not an empty log (review of audit stage 4),
	// as on the Inbound log.
	if f != (OutLogFilter{}) {
		p.Empty.Text = Plain("No messages match these filters.")
	}
	return p
}

// DetailHref is the address of one message's page, carrying the filters and the
// page it is opened from so that its way back returns to the same list.
func (p *OutLog) DetailHref(id int64, page int) string {
	return outboundLog + "/" + strconv.FormatInt(id, 10) + p.filter.query(page)
}

// WithRows sets the table for page of last, and the foot under it: where the
// viewer is, and the way to the newer and older rows. The poll asks for the same
// page again.
func (p *OutLog) WithRows(rows []OutLogRow, page, last int) *OutLog {
	p.Rows = rows
	p.PollURL = outboundLog + "/fragment" + p.filter.query(page)
	if len(rows) == 0 {
		return p
	}
	text := Rich("Page " + strconv.Itoa(page) + " of " + strconv.Itoa(last))
	if page > 1 {
		text = append(text, Inline{Text: " · "}, Link(LogHref(p.filter, page-1), "← Newer"))
	}
	p.Foot.Text = text
	if page < last {
		p.Foot.Link = &Anchor{Href: LogHref(p.filter, page+1), Label: "Older →"}
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
	// Reason and HeaderFrom are a refused row's: why it was refused
	// (store.Reason*) and, for its From: line, what that line said.
	Reason     string
	HeaderFrom string
	// Recovered is a refused row the log tailer wrote from mail.log: the
	// panel stopped before it wrote the row itself.
	Recovered bool
	Accepted  time.Time
	Reported  time.Time
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
	// queue, or that predates the stored lines, is an ordinary outcome.
	LogEmpty EmptyState
}

// SendLogTag is the tag of a send-log row. A refused row says why in the tag,
// so that the log answers it without the message being opened: the reasons
// are store.Reason*, and a row from before they were recorded is "rejected".
func SendLogTag(status, reason string) Tag {
	if status != "rejected" {
		return Tag{Status: status}
	}
	switch reason {
	case "from":
		return Tag{Status: "reject", Label: "wrong From"}
	case "client":
		return Tag{Status: "reject", Label: "wrong client"}
	case "limit":
		return Tag{Status: "rate-limited"}
	}
	return Tag{Status: status}
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
	tag := SendLogTag(in.Status, in.Reason)
	facts := []Fact{
		{Label: "Domain", Value: value(in.Domain, in.DomainHref)},
		{Label: "Application", Value: value(in.App, in.AppHref), Mono: true},
	}
	history := Box{No: "02", Title: "History", Help: topicLink(HelpLog, "What the statuses mean")}
	delivery := Box{No: "03", Title: "Delivery log", End: Rich("Lines from ", Code("mail.log"), " with this queue id")}
	if in.Status == "rejected" {
		// A refused message was never accepted and never queued: its one date
		// is the refusal. One refused for its From: line was given a queue id
		// before its headers were judged; one refused at MAIL FROM was not,
		// and its box holds Postfix's one line about the refusal.
		facts = append(facts, Fact{Label: "Refused", Value: Plain(formatStamp(in.Accepted))})
		if in.Reason == "from" {
			facts = append(facts, Fact{Label: "From: line", Value: Plain(orDash(in.HeaderFrom)), Mono: true, Small: true})
			history.Help = topicLink(HelpSenderCheck, "What an application may write in From:")
		}
		if in.Recovered {
			// cleanup's line names the message's last recipient only (Postfix
			// 3.7.11, src/cleanup/cleanup_milter.c, cleanup_milter_apply(),
			// " to=<%s>" of state->recip; assumptions A-15).
			facts = append(facts, Fact{Label: "Written", Value: Rich("from ", Code("mail.log"), " after a restart: the panel stopped before it wrote the refusal. Postfix's line names the last recipient only")})
		}
		if in.QueueID != "" {
			facts = append(facts, Fact{Label: "Queue id", Value: Rich(Mono(in.QueueID), " - given, the message was not queued")})
		} else {
			facts = append(facts, Fact{Label: "Queue id", Value: Rich("none - refused at ", Mono("MAIL FROM"), ", before one is given")})
			delivery.End = Rich("The line from ", Code("mail.log"), " for this refusal")
		}
	} else {
		facts = append(facts,
			Fact{Label: "Accepted", Value: Plain(formatStamp(in.Accepted))},
			Fact{Label: "Status reported", Value: Plain(formatStamp(in.Reported))},
			Fact{Label: "Queue id", Value: Plain(orDash(in.QueueID)), Mono: true})
	}
	facts = append(facts, Fact{Label: "Journal id", Value: Plain(strconv.FormatInt(in.ID, 10)), Mono: true})
	return &OutMessage{
		Meta: m,
		Head: Head{
			Crumbs:  []Crumb{{Href: in.BackHref, Label: "Outbound log"}},
			Title:   title,
			Compact: true,
			Route: Route{
				{Text: orDash(in.From)}, {Arrow: true}, {Text: orDash(in.To)},
				{Tag: &tag},
			},
		},
		Message:  Box{No: "01", Title: "Message"},
		Facts:    facts,
		History:  history,
		Delivery: delivery,
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
	// A refusal is as permanent as its reply: "550"/"5.7.1" red, "451" amber.
	if m := rejectCodeRe.FindStringSubmatch(text); m != nil {
		if m[1] == "5" {
			return "error"
		}
		return "warn"
	}
	return ""
}

// rejectCodeRe finds the reply of a refusal Postfix logged: the code or the
// enhanced status after "reject: <stage> from <client>[<address>]:", from
// smtpd ("reject:") and cleanup ("milter-reject:") alike.
var rejectCodeRe = regexp.MustCompile(`reject: \S+ from [^\]]*\]: ([45])(?:\d\d|\.\d+\.\d+) `)
