package view

// The data of Overview (templates/overview.html, mockup overview.html): the
// answer to "is mail flowing?" in a postmark and six cards, and under them the
// two kinds of domain at a glance. Everything behind the cards is on Health
// (health.go).

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Overview is the data of the Overview page. Cards are the checks of Health
// reduced to a verdict each; the head's postmark and sentence are derived from
// them by NewOverview, so the stamp and the cards cannot disagree.
//
// The inbound box is drawn only when the inbound relay is on (InboundOn):
// without it the outbound box takes the whole width and nothing on the page
// points at /inbound/domains.
type Overview struct {
	Meta
	Head  Head
	Cards []HealthCard

	Health Box

	Outbound      Box
	Days          int
	OutboundRows  []OverviewDomain
	OutboundEmpty EmptyState

	InboundOn    bool
	Inbound      Box
	InboundRows  []OverviewInbound
	InboundEmpty EmptyState
}

// OverviewDomain is one row of the Outbound domains table: the domain, the
// verdict of each DNS check as a tag (DKIM, SPF, DMARC), how many applications
// it has and how much mail it sent in the window (see FormatMessages).
type OverviewDomain struct {
	Name     string
	Href     string
	DNS      []Tag
	Apps     int
	Messages string
}

// OverviewInbound is one row of the Inbound domains table: the domain, its MX
// check as a tag and the upstream it forwards to.
type OverviewInbound struct {
	Name     string
	Href     string
	MX       Tag
	Upstream string
}

// NewOverview builds the page. host is the server's name (the kicker) and may
// be empty; updated is when the checks were read; days is the length of the
// send window of the last column.
func NewOverview(m Meta, host string, cards []HealthCard, updated time.Time, days int, inboundOn bool) *Overview {
	m.Title, m.Section, m.Page = "Overview", "overview", ""
	o := &Overview{
		Meta:  m,
		Cards: cards,
		Days:  days,
		Head:  overviewHead(host, cards, updated),
		Health: Box{No: "01", Title: "Server health", ID: "health", Poll: "/overview/fragment",
			End:  Rich(Link("/server/health", "All details")),
			Help: topicLink(HelpChecks, "What these checks mean")},
		Outbound: Box{No: "02", Title: "Outbound domains", End: Rich(Link("/outbound/domains", "Manage"))},
		OutboundEmpty: EmptyState{Icon: "ti-world-upload",
			Text: Plain("No outbound domains yet. Add the first one under Outbound.")},
		InboundOn: inboundOn,
		Inbound:   Box{No: "03", Title: "Inbound domains", End: Rich(Link("/inbound/domains", "Manage"))},
		InboundEmpty: EmptyState{Icon: "ti-world-download",
			Text: Plain("No inbound domains yet. Mail is not accepted for any.")},
	}
	return o
}

// HeadOOB is the head as the polled fragment carries it: the same head, marked
// to be swapped into the page by its id. The page itself never has the mark.
func (o *Overview) HeadOOB() Head {
	h := o.Head
	h.OOB = true
	return h
}

// overviewHead is the stamp and the sentence under it. The stamp takes the
// worst card, counts the cards that are not fine, and the lead names them with
// their detail so the operator sees what to open without scanning the grid. A
// card that could not be read is a warning, never a pass (cardLevel).
func overviewHead(host string, cards []HealthCard, updated time.Time) Head {
	level, bad := LevelOK, []HealthCard{}
	for _, c := range cards {
		if c.Level == "" {
			continue
		}
		bad = append(bad, c)
		if c.Level == LevelFail || level == LevelOK {
			level = c.Level
		}
	}

	title, word, count := "Everything is running normally.", "OK", len(cards)
	switch level {
	case LevelWarn:
		title, word, count = "Running, with warnings.", "WARN", len(bad)
	case LevelFail:
		title, word, count = "A component needs attention.", "FAIL", len(bad)
	}

	h := Head{
		Postmark: &Postmark{Top: "SELFPOST", Word: word, Bottom: fmt.Sprintf("%d OF %d CHECKS", count, len(cards)), Level: level},
		Kicker:   host,
		Title:    title,
		ID:       "overview-head",
		Note:     []string{"Updated " + updated.UTC().Format("15:04:05") + " UTC"},
	}
	if len(bad) > 0 {
		lead := Text{{Text: "Needs attention: "}}
		for i, c := range bad {
			if i > 0 {
				lead = append(lead, Inline{Text: ", "})
			}
			lead = append(lead, Link(c.Href, c.Name), Inline{Text: " (" + c.Value + ")"})
		}
		h.Lead = append(lead, Inline{Text: "."})
	}
	return h
}

// FormatCount writes a whole number with its thousands separated by a no-break
// space: "1 284".
func FormatCount(n int64) string {
	digits := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 && digits[i-1] != '-' {
			b.WriteString("\u00a0")
		}
		b.WriteRune(r)
	}
	return b.String()
}

// FormatMessages writes a message count as the Overview table shows it:
// "1 284 msg".
func FormatMessages(n int64) string { return FormatCount(n) + "\u00a0msg" }
