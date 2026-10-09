package view

// The data of the Outbound queue (templates/out_queue.html, mockup
// out-queue.html): what Postfix is still holding, and the retry policy that
// decides how long it holds it. The queue box polls; the page and its fragment,
// GET /outbound/queue/fragment, render the same box
// (templates/out_queue_body.html).
//
// Until the queue is a table (docs/roadmap.md, parse postqueue -j) the box
// shows postqueue's own listing in a log pane, as the system log shows
// mail.log: the mockup's table is not drawn from data the panel has.
//
// A page is a typed struct that embeds Meta, built by a constructor and filled
// by With… methods; the handler reads Postfix, the view decides what is said and
// how it is worded.

import "strings"

const outboundQueue = "/outbound/queue"

// OutQueue is the data of the Outbound queue. It is for the global role.
type OutQueue struct {
	Meta
	Head Head

	Queue Box
	Lines []LogLine
	// Empty is shown in place of the listing when there is none: the queue holds
	// nothing, or it could not be read.
	Empty EmptyState

	Retries Box
	Policy  []Fact
}

// NewOutQueue builds the page. listing is what postqueue printed, as it was
// read, and errText is why it could not be read, empty when it could.
func NewOutQueue(m Meta, listing, errText string) *OutQueue {
	m.Title, m.Section, m.Page = "Outbound queue", "outbound", "queue"
	p := &OutQueue{
		Meta: m,
		Head: Head{
			Kicker: "Outbound",
			Title:  "Outbound queue",
			Lead:   Rich("What applications handed over and the receiving server has not yet taken, as ", Code("postqueue -p"), " lists it."),
		},
		Queue: Box{No: "01", Title: "Waiting", ID: "out-queue", Poll: outboundQueue + "/fragment",
			End: Plain("Refreshes on its own")},
		Empty:   EmptyState{Icon: "ti-stack-2", Text: Plain("Queue is empty.")},
		Retries: Box{No: "02", Title: "How retries work"},
	}
	if errText != "" {
		p.Empty = EmptyState{Icon: "ti-alert-circle", Text: Plain(errText)}
		return p
	}
	if strings.TrimSpace(listing) == "" {
		return p
	}
	for _, line := range strings.Split(strings.TrimRight(listing, "\r\n"), "\n") {
		p.Lines = append(p.Lines, LogLine{Text: strings.TrimRight(line, "\r")})
	}
	return p
}

// WithRetryPolicy fills the second box: the first retry, the cap the later ones
// double up to and how long a message is kept, each as the handler words it.
// fromDefaults says the Postfix configuration could not be read and these are
// the compiled-in values.
func (p *OutQueue) WithRetryPolicy(firstRetry, backoffCap, lifetime string, fromDefaults bool) *OutQueue {
	p.Retries.End = Plain("This Postfix's policy, read at panel start")
	if fromDefaults {
		p.Retries.End = Plain("Compiled-in defaults: the Postfix configuration could not be read")
	}
	p.Policy = []Fact{
		{Label: "First retry", Value: Plain(firstRetry), Big: true},
		{Label: "Later retries", Value: Plain("doubling"), Big: true, Note: Plain("capped at " + backoffCap)},
		{Label: "Kept in queue", Value: Plain(lifetime), Big: true},
		{Label: "Then", Value: Plain("bounced"), Big: true, Note: Plain("no attempt counter, only time")},
	}
	return p
}
