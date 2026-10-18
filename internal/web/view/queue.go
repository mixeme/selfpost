package view

// The data of the two queues, Outbound › Queue and Inbound › Queue
// (templates/out_queue.html and in_queue.html, mockups out-queue.html and
// in-queue.html): what Postfix still holds, a row per recipient, and the retry
// policy that decides how long it holds it. Both are one Postfix queue, split
// by the recipient's domain: a recipient of an inbound domain is the Inbound
// queue's, every other the Outbound queue's. The Waiting box polls; a page and
// its fragment render the same box (templates/queue_body.html).
//
// A page is a typed struct that embeds Meta, built by a constructor and filled
// by With… methods; the handler reads Postfix, the view decides what is said and
// how it is worded.

import (
	"fmt"
	"sort"
	"time"
)

const (
	outboundQueue = "/outbound/queue"
	inboundQueue  = "/inbound/queue"
)

// maxQueueRows is how many recipients a queue page lists. A backlog of
// thousands is one receiver or upstream failing, and the oldest rows say which;
// the page polls, and a table of every one would be drawn again each time.
const maxQueueRows = 200

// QueueEntry is one recipient of a message Postfix holds, as the handler hands
// it over: the message's queue id, which of Postfix's queues it is in, when it
// arrived and its size, its sender and this recipient, why the recipient's
// last attempt did not finish (empty when there was none), and the page of the
// message in the Outbound log ("" when it has none, or for the Inbound queue).
type QueueEntry struct {
	ID        string
	Queue     string // "deferred", "active", "hold", "incoming", "maildrop"
	Arrival   time.Time
	Size      int64
	Sender    string
	Recipient string
	Reason    string
	Href      string
}

// QueueRow is one row of the Waiting table.
type QueueRow struct {
	ID      string
	Arrived string
	From    string
	To      string
	Reply   string
	Href    string
}

// Queue is the data of either queue page. It is for the global role.
type Queue struct {
	Meta
	Head Head

	Queue Box
	Rows  []QueueRow
	// Details is the table's last column: a row of the Outbound queue opens
	// its message in the Outbound log; the Inbound queue has no such page.
	Details bool
	// Empty is shown in place of the table when there is no row: the queue
	// holds nothing for this direction, or it could not be read.
	Empty EmptyState
	// Foot says that the table is cut at maxQueueRows; nil when it is not.
	Foot *Foot

	Retries Box
	Policy  []Fact
	// then is the note under "bounced": what the mockup of each page says.
	then string
}

// NewOutQueue builds Outbound › Queue from the recipients waiting for a
// receiving server. errText is why the queue could not be read, empty when it
// could.
func NewOutQueue(m Meta, entries []QueueEntry, errText string) *Queue {
	m.Title, m.Section, m.Page = "Outbound queue", "outbound", "queue"
	p := &Queue{
		Meta: m,
		Head: Head{
			Kicker: "Outbound",
			Title:  "Outbound queue",
			Lead:   Plain("Messages accepted from applications and still waiting for the receiving server. Refreshes on its own."),
		},
		Queue: Box{No: "01", Title: "Waiting", ID: "out-queue", Poll: outboundQueue + "/fragment",
			Help: topicLink(HelpQueue, "What waits here and how Postfix retries")},
		Details: true,
		Empty:   EmptyState{Icon: "ti-stack-2", Text: Plain("Nothing is waiting. Every message has reached its receiving server.")},
		Retries: Box{No: "02", Title: "How retries work"},
		then:    "no attempt counter, only time",
	}
	return p.fill(entries, errText)
}

// NewInQueue builds Inbound › Queue from the recipients of inbound domains
// waiting for their upstream.
func NewInQueue(m Meta, entries []QueueEntry, errText string) *Queue {
	m.Title, m.Section, m.Page = "Inbound queue", "inbound", "queue"
	p := &Queue{
		Meta: m,
		Head: Head{
			Kicker: "Inbound",
			Title:  "Inbound queue",
			Lead: Rich("Mail accepted on port 25 and still waiting for its upstream. The same Postfix queue as ",
				Link(outboundQueue, "outbound"), ", filtered by recipient domain."),
		},
		Queue: Box{No: "01", Title: "Waiting", ID: "in-queue", Poll: inboundQueue + "/fragment",
			Help: topicLink(HelpInboundQueue, "What waits here and why")},
		Empty:   EmptyState{Icon: "ti-stack-2", Text: Plain("Nothing is waiting. Every accepted message has reached its upstream.")},
		Retries: Box{No: "02", Title: "How retries work"},
		then:    "back to the original sender",
	}
	return p.fill(entries, errText)
}

// fill lays the rows out, oldest first - the message that has waited longest
// is the one that says what is wrong - and heads the box with the count of
// messages and their size, or with "Refreshes on its own" while there is none.
func (p *Queue) fill(entries []QueueEntry, errText string) *Queue {
	p.Queue.End = Plain("Refreshes on its own")
	if errText != "" {
		p.Empty = EmptyState{Icon: "ti-alert-circle", Text: Plain(errText)}
		return p
	}
	if len(entries) == 0 {
		return p
	}
	sorted := append([]QueueEntry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].Arrival.Equal(sorted[j].Arrival) {
			return sorted[i].Arrival.Before(sorted[j].Arrival)
		}
		return sorted[i].ID < sorted[j].ID
	})
	sizes := map[string]int64{}
	for _, e := range sorted {
		sizes[e.ID] = e.Size
	}
	var total int64
	for _, s := range sizes {
		total += s
	}
	p.Queue.End = Plain(countOf(len(sizes), "message") + " · " + byteSize(total))
	for i, e := range sorted {
		if i == maxQueueRows {
			p.Foot = &Foot{Text: Plain(fmt.Sprintf("The oldest %d of %d recipients waiting. postqueue -p on the server lists them all.", maxQueueRows, len(sorted)))}
			break
		}
		from := e.Sender
		if from == "" {
			from = "<>" // a bounce has no sender
		}
		p.Rows = append(p.Rows, QueueRow{
			ID: e.ID, Arrived: e.Arrival.Local().Format("01-02 15:04:05"),
			From: from, To: e.Recipient, Reply: queueReply(e), Href: e.Href,
		})
	}
	return p
}

// queueReply is the Last reply cell: the reason Postfix keeps for the
// recipient's last attempt, or, before there was one, where the message is.
func queueReply(e QueueEntry) string {
	if e.Reason != "" {
		return e.Reason
	}
	switch e.Queue {
	case "active":
		return "Being delivered now"
	case "hold":
		return "On hold: not tried until it is released"
	case "incoming", "maildrop":
		return "Just arrived, not tried yet"
	}
	return "Not tried yet"
}

// countOf is "1 message", "3 messages".
func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// byteSize is a size as the mockup writes it: "3.1 kB" for 3174 bytes, counted
// in 1024s as postqueue counts its Kbytes.
func byteSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f kB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

// WithRetryPolicy fills the second box: the first retry, the cap the later ones
// double up to and how long a message is kept, each as the handler words it.
// fromDefaults says the Postfix configuration could not be read and these are
// the compiled-in values.
func (p *Queue) WithRetryPolicy(firstRetry, backoffCap, lifetime string, fromDefaults bool) *Queue {
	p.Retries.End = Plain("This Postfix's policy, read at panel start")
	if fromDefaults {
		p.Retries.End = Plain("Compiled-in defaults: the Postfix configuration could not be read")
	}
	p.Policy = []Fact{
		{Label: "First retry", Value: Plain(firstRetry), Big: true},
		{Label: "Later retries", Value: Plain("doubling"), Big: true, Note: Plain("capped at " + backoffCap)},
		{Label: "Kept in queue", Value: Plain(lifetime), Big: true},
		{Label: "Then", Value: Plain("bounced"), Big: true, Note: Plain(p.then)},
	}
	return p
}
