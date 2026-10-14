package view

import (
	"strconv"
	"time"
)

// The data of Server › Health (templates/health.html, mockup health.html):
// every check behind the cards of Overview, as tables, and the home of the two
// server actions, Re-check DNS and Reload configuration.
//
// The page is two stacks of boxes numbered by importance (the `cards` layout),
// and all but the Configuration box are polled (templates/health_body.html):
// the handler builds the same Health for the page and for its fragment, and
// AsFragment marks what the fragment swaps in out of band.

// Health is the data of the Health page.
type Health struct {
	Meta
	Head         Head
	Flash        *Flash
	Processes    Box
	ProcessRows  []ProcessRow
	ProcessError bool

	// SenderCheck is what the strict check of the From: line has cost: for how
	// long the panel did not answer while Postfix was running, how many
	// connections Postfix turned away and how many messages it deferred in
	// the middle for it. SenderNote is the line under its table.
	SenderCheck Box
	SenderRows  []SenderCheckRow
	SenderNote  Text

	Machine     Box
	MachineRows []MachineRow

	Certificate Box
	CertFacts   []Fact
	CertDetail  string
	CertProblem bool

	Sockets    Box
	SocketRows []SocketRow
	Hostname   Box
	HostFacts  []Fact

	// NotifyMail is the card of the DNS records notifications are signed and
	// authorised with (their values are under Server › Settings); NotifyNote
	// is the line under its table.
	NotifyMail Box
	NotifyRows []MailRow
	NotifyNote Text

	Configuration Box
	Reload        Action
}

// SenderCheckRow is one period of the Sender check table.
type SenderCheckRow struct {
	Period   string
	Down     string // with "≈ " in front when the period holds an estimate
	Deferred string // connections turned away
	Messages string // messages deferred in the middle
}

// MachineRow is one line of the Machine table. A row that is a level (CPU,
// memory) carries a Gauge; one that is a rate (network) carries Usage text; a
// reading that could not be taken carries neither and shows a dash. Detail is
// the supporting text, one line each.
type MachineRow struct {
	Resource string
	Gauge    *Gauge
	Usage    string
	Detail   []string
}

// Gauge is a reading shown as a bar and its figure: Percent fills the bar, Text
// is the figure printed beside it, and Level (ok, warn or fail) colours it.
type Gauge struct {
	Percent int
	Text    string
	Level   string
}

// Bar is the Bulma class that colours the gauge's progress bar.
func (g Gauge) Bar() string {
	switch g.Level {
	case LevelFail:
		return "is-danger"
	case LevelWarn:
		return "is-warning"
	}
	return "is-success"
}

// ProcessRow is one supervised program: its name, its state as a tag and what
// supervisord said about it.
type ProcessRow struct {
	Name   string
	State  Tag
	Detail string
}

// SocketRow is one milter socket. Note qualifies the name ("inbound spam
// filter"); Detail is shown only when the socket is not fine, since "Listening"
// says nothing the tag does not.
type SocketRow struct {
	Name   string
	Note   string
	Path   string
	State  Tag
	Detail string
}

// MailRow is one record of the Notification mail table: its name, the host it
// is published at and its state as a tag.
type MailRow struct {
	Name  string
	Host  string
	State Tag
}

// NewMailRow is the row of one record of the server's own name, from what the
// DNS check found. A record that is not in place is a warning, as on Settings.
func NewMailRow(name, host string, c DNSCheck) MailRow {
	return MailRow{Name: name, Host: host, State: mailTag(c)}
}

// NewHealth builds the page around its content. The handler fills the tables;
// the boxes, the head and the reload action are the page's own.
func NewHealth(m Meta, flash string) *Health {
	m.Title, m.Section, m.Page = "Health", "server", "health"
	h := &Health{
		Meta: m,
		Head: Head{
			Kicker:  "Server",
			Title:   "Health",
			Lead:    Plain("Everything behind the cards on Overview. Refreshes automatically."),
			Actions: []Action{{Label: "Re-check DNS", Icon: "ti-refresh", Post: "/server/health/recheck"}},
		},
		// Numbered by importance; the odd ones are the left stack.
		Processes: Box{No: "01", Title: "Processes", ID: "processes"},
		Sockets:   Box{No: "02", Title: "Milter sockets", ID: "sockets"},
		SenderCheck: Box{No: "03", Title: "Sender check", ID: "sender-check",
			Help: topicLink(HelpSenderCheck, "What is checked and what these numbers are for")},
		Certificate: Box{No: "04", Title: "TLS certificate", ID: "certificate", Help: topicLink(HelpCertificate, "When it warns and what to do")},
		Hostname:    Box{No: "05", Title: "Hostname and reverse DNS", ID: "hostname", Help: topicLink(HelpRDNS, "Forward and reverse DNS")},
		NotifyMail: Box{No: "06", Title: "Notification mail", ID: "notification-mail",
			Help: topicLink(HelpNotifications, "What the panel sends and what makes it arrive")},
		Machine: Box{No: "07", Title: "Machine", ID: "machine", Help: topicLink(HelpChecks, "What these readings mean")},
		Configuration: Box{No: "08", Title: "Configuration", ID: "configuration",
			End: Plain("Safe to run at any time")},
		Reload: Action{Label: "Reload configuration", Icon: "ti-reload", Post: "/server/health/reload"},
	}
	if flash != "" {
		h.Flash = &Flash{Text: Plain(flash)}
	}
	return h
}

// WithNotificationMail fills the Notification mail card: from is the address
// notifications come from, rows the state of each record, and status the
// card's verdict for its head.
func (h *Health) WithNotificationMail(from string, rows []MailRow, status string) *Health {
	h.NotifyMail.End = Verdict(status)
	h.NotifyRows = rows
	h.NotifyNote = Rich("Sent from ", Mono(from), ". What to publish is under ",
		Link("/server/settings#notification-mail", "Settings"), ".")
	return h
}

// WithSenderCheck fills the Sender check card: its three periods, the first
// day counted ("" before any was) and the card's verdict.
func (h *Health) WithSenderCheck(rows []SenderCheckRow, since, status string) *Health {
	h.SenderCheck.End = Verdict(status)
	h.SenderRows = rows
	h.SenderNote = Rich("The panel holds each message's ", Code("From:"),
		" against the application that sends it. While the panel does not answer, Postfix answers the sending application with a temporary error instead of accepting mail unchecked, and the application tries again later. Time is counted only while Postfix itself was running; ≈ marks a period with a start at which that could not be asked, and its time is at most what is shown.")
	if since != "" {
		h.SenderNote = append(h.SenderNote, Rich(" Counted since ", Mono(since), ".")...)
	}
	return h
}

// AsFragment makes the page its polled fragment: the left stack replaces the
// element that polls, and the three readings of the right stack are marked to
// be swapped into place by their ids.
func (h *Health) AsFragment() *Health {
	h.Sockets.OOB, h.Certificate.OOB, h.NotifyMail.OOB = true, true, true
	return h
}

// Downtime prints a length of time the way the Sender check table does: whole
// seconds while they say something, then minutes, then hours.
func Downtime(d time.Duration) string {
	secs := int64(d / time.Second)
	switch {
	case secs < 60:
		return strconv.FormatInt(secs, 10) + " s"
	case secs < 3600:
		return strconv.FormatInt(secs/60, 10) + " min " + strconv.FormatInt(secs%60, 10) + " s"
	}
	return strconv.FormatInt(secs/3600, 10) + " h " + strconv.FormatInt(secs%3600/60, 10) + " min"
}

// Verdict puts a check's status in the end slot of its box, as the mockup does.
func Verdict(status string) Text { return Text{TagOf(status, "")} }
