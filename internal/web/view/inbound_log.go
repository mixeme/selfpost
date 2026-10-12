package view

// The data of the Inbound log (templates/in_log.html, mockup in-log.html): what
// port 25 decided about each message offered for an inbound domain, one row per
// recipient. Unlike the Outbound log it does not poll: a decision does not change
// after it is made, and the page is read to find one row, not watched.
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

const inboundLog = "/inbound/log"

// InLogFilter is what the filters of the Inbound log are set to, as the strings
// the form sends: a domain name, a decision and two days (2006-01-02). Empty
// means no narrowing.
type InLogFilter struct {
	Domain   string
	Decision string
	From     string
	To       string
}

func (f InLogFilter) active() bool { return f != InLogFilter{} }

// query is the query string of a page of the log: the filters and the page, each
// left out when it says nothing, so the first unfiltered page is the bare path.
func (f InLogFilter) query(page int) string {
	q := url.Values{}
	for name, v := range map[string]string{"domain": f.Domain, "decision": f.Decision, "from": f.From, "to": f.To} {
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

// InLogHref is the address of a page of the Inbound log.
func InLogHref(f InLogFilter, page int) string { return inboundLog + f.query(page) }

// InLog is the data of the Inbound log: the decisions about mail for the inbound
// domains the viewer reaches, filtered by domain, decision and day, a page at a
// time.
type InLog struct {
	Meta
	Head Head

	Messages Box
	Rows     []InLogRow
	Empty    EmptyState
	Foot     Foot

	filter InLogFilter
}

// InLogRow is one recipient of one message: when it was decided, who the mail
// was from (and the address of the server that offered it) and for, its subject,
// the decision as a tag that opens the sheet saying who decided and why, and —
// for the global role, who alone edits the filter lists — the way to correct
// the decision.
type InLogRow struct {
	Time    string
	From    string
	Client  string
	To      string
	Subject string
	Why     Sheet
	Action  *Anchor
}

// InLogRowInput is what the journal holds about one row. Subject, Detail and
// From may be empty: refused mail is read from mail.log, which has no subject,
// and a filter that said nothing has no detail.
type InLogRowInput struct {
	ID int64
	// Listable says the sender is an address the filter lists would take. A
	// row whose sender they would refuse offers no way into them.
	Listable bool
	At       time.Time
	From     string
	Client   string
	To       string
	Subject  string
	Decision string
	Engine   string
	Detail   string
}

// NewInLog builds the log for a viewer. domains are the choices of the domain
// filter (only the inbound domains the viewer reaches) and filter is what is in
// force. retentionDays is how long rows are kept; the global role, who can
// change it and who edits the lists, is read from m.
func NewInLog(m Meta, retentionDays int, domains []string, filter InLogFilter) *InLog {
	m.Title, m.Section, m.Page = "Inbound log", "inbound", "log"
	lead := Rich("What port 25 decided about each message, one row per recipient. Accepted mail is recorded as it passes, refused mail is read from ",
		Code("mail.log"), "; the filter's verdict appears when the spam filter is on. Rows older than "+plural(int64(retentionDays), "day", "days")+" are deleted")
	if m.IsGlobal {
		lead = append(lead, Inline{Text: " — change that in "}, Link("/server/settings", "Settings"))
	}
	lead = append(lead, Inline{Text: "."})

	domainOptions := []Option{{Value: "", Label: "All domains"}}
	for _, d := range domains {
		domainOptions = append(domainOptions, Option{Value: d, Label: d})
	}
	decisionOptions := []Option{{Value: "", Label: "Any decision"}}
	for _, d := range []string{"accept", "reject", "tempfail", "quarantine"} {
		decisionOptions = append(decisionOptions, Option{Value: d, Label: d})
	}
	p := &InLog{
		Meta: m,
		Head: Head{Kicker: "Inbound", Title: "Inbound log", Lead: lead},
		Messages: Box{No: "01", Title: "Messages", Help: topicLink(HelpInboundLog, "What the decisions mean"), Filter: &Filter{
			Action: inboundLog,
			Selects: []Select{
				{Name: "domain", Label: "Domain", Options: domainOptions, Selected: filter.Domain},
				{Name: "decision", Label: "Decision", Options: decisionOptions, Selected: filter.Decision},
			},
			Dates: []DateInput{
				{Name: "from", Label: "From date", Value: filter.From},
				{Name: "to", Label: "To date", Value: filter.To},
			},
		}},
		Empty:  EmptyState{Icon: "ti-list-details", Text: Plain("No messages logged yet.")},
		filter: filter,
	}
	if filter.active() {
		p.Empty.Text = Plain("No messages match these filters.")
	}
	return p
}

// NewRow words one row of the journal for the viewer of the page.
func (p *InLog) NewRow(in InLogRowInput) InLogRow {
	r := InLogRow{
		Time: FormatLogTime(in.At), From: orDash(in.From), Client: in.Client, To: in.To, Subject: in.Subject,
		Why: Sheet{
			ID: "why-" + strconv.FormatInt(in.ID, 10), Tag: Tag{Status: in.Decision}, Title: "Why",
			Rows: whyRows(in.Decision, in.Engine, in.Detail, in.Subject),
		},
	}
	if p.IsGlobal && in.Listable {
		r.Action = senderAction(in.Decision, in.Engine, in.From)
	}
	return r
}

// whyRows is the content of a row's sheet: who decided, what they said, and
// the subject whole, which the table clips. What was said has one of two
// shapes, and the decision says which. Mail that passed was written by the
// inbound journal milter with the filter's verdict - an action and a score,
// then the symbols after a semicolon. Mail that was refused or put off was
// read from mail.log by the tailer, with the reply it was given.
func whyRows(decision, engine, detail, subject string) []SheetRow {
	rows := []SheetRow{{Label: "Decided by", Lines: []string{engine}}}
	switch {
	case detail == "":
	case decision == "reject" || decision == "tempfail":
		rows = append(rows, SheetRow{Label: "Reply", Lines: []string{detail}, Mono: true})
	default:
		verdict, symbols, _ := strings.Cut(detail, "; ")
		rows = append(rows, SheetRow{Label: "Verdict", Lines: []string{verdict}})
		if symbols != "" {
			rows = append(rows, SheetRow{Label: "Symbols", Lines: strings.Fields(symbols), Mono: true})
		}
	}
	if subject != "" {
		rows = append(rows, SheetRow{Label: "Subject", Lines: []string{subject}})
	}
	return rows
}

// senderAction is the way to correct a decision from its row, if there is one:
// a message that got through is the one to deny its sender for, and a message
// the filter turned away, put off or held is the one to allow its sender for.
// A refusal by Postfix's own policy (an unknown recipient, say) offers nothing:
// the message is refused already and the lists would not change what Postfix
// thinks of the recipient. A row with no sender has nobody to list.
func senderAction(decision, engine, from string) *Anchor {
	if from == "" {
		return nil
	}
	list := ""
	switch {
	case decision == "accept":
		list = "deny"
	case engine == "rspamd":
		list = "allow"
	default:
		return nil
	}
	label := "Deny sender"
	if list == "allow" {
		label = "Allow sender"
	}
	return &Anchor{Href: FilterListsHref(list, "address", from), Label: label}
}

// WithRows sets the table for page of last, and the foot under it: where the
// viewer is, and the way to the newer and older rows. For the global role the
// foot also says where the sender links lead.
func (p *InLog) WithRows(rows []InLogRow, page, last int) *InLog {
	p.Rows = rows
	if len(rows) == 0 {
		return p
	}
	text := Rich("Page " + strconv.Itoa(page) + " of " + strconv.Itoa(last))
	if page > 1 {
		text = append(text, Inline{Text: " · "}, Link(InLogHref(p.filter, page-1), "← Newer"))
	}
	p.Foot.Text = text
	if page < last {
		p.Foot.Link = &Anchor{Href: InLogHref(p.filter, page+1), Label: "Older →"}
	}
	if p.IsGlobal {
		p.Foot.End = Rich("A decision opens its reason · Allow / Deny sender opens ", Link(inboundFilterLists, "Filter lists"), " with the entry filled in")
	} else {
		p.Foot.End = Plain("A decision opens its reason")
	}
	return p
}
