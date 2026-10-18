package view

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// ---- Outbound queue

// The page is the mockup's table, a row per recipient with its last reply and
// the link to the message, and it polls from its box; the retry policy sits
// under it, outside the poll.
func TestOutQueueShowsTheTableAndPollsItsBox(t *testing.T) {
	out := renderSignedIn(t, "out-queue", outQueueFixture())
	pageHas(t, "Outbound queue", out,
		`<h1 class="title is-3">Outbound queue</h1>`, `<h2>Waiting</h2>`,
		`Messages accepted from applications and still waiting for the receiving server. Refreshes on its own.`,
		`id="out-queue" data-poll aria-live="polite" hx-get="/outbound/queue/fragment" hx-trigger="load" hx-swap="outerHTML"`,
		`1 message · 3.1 kB`, `href="/help#queue"`,
		`<th>Queue id</th><th>Arrived</th><th>From</th><th>To</th><th>Last reply</th><th></th>`,
		`<td class="sp-mono">4XcB7k2Jm9z1</td><td class="sp-mono sp-muted sp-nowrap sp-small">09-21 13:58:07</td>`,
		`<td>alerts@<wbr>example.org</td><td>noc@<wbr>example.org</td>`,
		`<td class="sp-muted">451 4.7.1 Greylisted, try again in 5 minutes</td><td class="sp-actions"><a href="/outbound/log/41">Details</a></td>`,
		`aria-current="page"><i class="ti ti-stack-2"></i>Queue</a>`,
	)
	pageLacks(t, "Outbound queue", out, `class="sp-log"`, "Nothing is waiting", "postqueue -p")

	frag := renderFragment(t, "queue_body", outQueueFixture())
	if !strings.HasPrefix(frag, `<div class="box" id="out-queue" data-poll`) || strings.Contains(frag, "sp-head") || strings.Contains(frag, "How retries work") {
		t.Errorf("the fragment is not the polled box alone:\n%.200s", frag)
	}
	if strings.Count(out, "4XcB7k2Jm9z1") != 1 || strings.Count(frag, "4XcB7k2Jm9z1") != 1 {
		t.Error("the page and its fragment should each carry the row once")
	}
}

// Rows come oldest first, a recipient each; the head counts messages and their
// size once however many recipients a message has; a recipient without a
// reason says where its message is, a bounce shows its empty sender, and a
// recipient whose message has no send-log row has no Details link.
func TestOutQueueRowsAndHead(t *testing.T) {
	t0 := time.Date(2026, 10, 18, 9, 0, 0, 0, time.Local)
	entries := []QueueEntry{
		{ID: "NEWER", Queue: "active", Arrival: t0.Add(time.Hour), Size: 2048, Sender: "a@example.com", Recipient: "x@example.net", Href: "/outbound/log/2"},
		{ID: "OLDER", Queue: "deferred", Arrival: t0, Size: 1000, Sender: "", Recipient: "b@example.net", Reason: "connect to mx.example.net[203.0.113.9]:25: Connection timed out"},
		{ID: "OLDER", Queue: "deferred", Arrival: t0, Size: 1000, Sender: "", Recipient: "c@example.net", Reason: "connect to mx.example.net[203.0.113.9]:25: Connection timed out"},
		{ID: "HELD", Queue: "hold", Arrival: t0.Add(2 * time.Hour), Size: 100, Sender: "a@example.com", Recipient: "y@example.net", Href: "/outbound/log/3"},
	}
	out := renderSignedIn(t, "out-queue", NewOutQueue(admin(), entries, ""))
	pageHas(t, "queue", out, `3 messages · 3.1 kB`, `&lt;&gt;`, `Being delivered now`, `On hold: not tried until it is released`)
	if i, j, k := strings.Index(out, "OLDER"), strings.Index(out, "NEWER"), strings.Index(out, "HELD"); i < 0 || !(i < j && j < k) {
		t.Errorf("rows are not oldest first: OLDER %d, NEWER %d, HELD %d", i, j, k)
	}
	if strings.Count(out, `<td class="sp-actions"></td>`) != 2 {
		t.Error("the two recipients of a message without a send-log row should have an empty Details cell")
	}
}

// A backlog is cut at maxQueueRows, oldest kept, and the foot says how many
// there are.
func TestOutQueueCutsALongBacklog(t *testing.T) {
	t0 := time.Date(2026, 10, 18, 9, 0, 0, 0, time.Local)
	var entries []QueueEntry
	for i := 0; i < maxQueueRows+5; i++ {
		entries = append(entries, QueueEntry{ID: fmt.Sprintf("Q%04d", i), Queue: "deferred", Arrival: t0.Add(time.Duration(i) * time.Minute), Recipient: "r@example.net"})
	}
	out := renderSignedIn(t, "out-queue", NewOutQueue(admin(), entries, ""))
	pageHas(t, "long queue", out, "Q0000", fmt.Sprintf("Q%04d", maxQueueRows-1),
		fmt.Sprintf("The oldest %d of %d recipients waiting.", maxQueueRows, maxQueueRows+5), `class="sp-box-foot"`)
	pageLacks(t, "long queue", out, fmt.Sprintf("Q%04d", maxQueueRows))
}

// The policy is the page's, wherever it was read from: its facts, and a head
// that says so when they are the compiled-in defaults.
func TestOutQueueStatesTheRetryPolicy(t *testing.T) {
	out := renderSignedIn(t, "out-queue", NewOutQueue(admin(), nil, "").WithRetryPolicy("10 minutes", "about 1 hour 7 minutes", "2 days", false))
	pageHas(t, "Outbound queue", out,
		`<h2>How retries work</h2>`, `This Postfix&#39;s policy, read at panel start`,
		`<dd class="sp-big">10 minutes</dd>`, `<dd class="sp-big">doubling</dd>`, `capped at about 1 hour 7 minutes`,
		`<dd class="sp-big">2 days</dd>`, `<dd class="sp-big">bounced</dd>`, `no attempt counter, only time`,
	)
	pageLacks(t, "Outbound queue", out, "Compiled-in defaults")

	out = renderSignedIn(t, "out-queue", NewOutQueue(admin(), nil, "").WithRetryPolicy("5 minutes", "about 1 hour 7 minutes", "5 days", true))
	pageHas(t, "Outbound queue", out, "Compiled-in defaults: the Postfix configuration could not be read")
	pageLacks(t, "Outbound queue", out, "read at panel start")
}

// With nothing to list the box says why instead: the queue holds nothing, or
// it could not be read — and never both.
func TestOutQueueSaysWhyItHasNoRows(t *testing.T) {
	out := renderSignedIn(t, "out-queue", NewOutQueue(admin(), nil, ""))
	pageHas(t, "empty queue", out, `class="sp-empty"`, "Nothing is waiting. Every message has reached its receiving server.", "Refreshes on its own")
	pageLacks(t, "empty queue", out, "<table", "Could not read")

	out = renderSignedIn(t, "out-queue", NewOutQueue(admin(), []QueueEntry{{ID: "IGNORED"}}, "Could not read the mail queue."))
	pageHas(t, "unreadable queue", out, `class="sp-empty"`, "Could not read the mail queue.")
	pageLacks(t, "unreadable queue", out, "<table", "Nothing is waiting", "IGNORED")
}

// ---- Inbound queue

// The mockup's Inbound queue: nothing waiting, the help link, and the lead
// that links the Outbound queue; with rows, the same table without Details,
// since an inbound message has no page of its own.
func TestInQueue(t *testing.T) {
	out := renderSignedIn(t, "in-queue", inQueueFixture())
	pageHas(t, "Inbound queue", out,
		`<h1 class="title is-3">Inbound queue</h1>`,
		`The same Postfix queue as <a href="/outbound/queue">outbound</a>, filtered by recipient domain.`,
		`id="in-queue" data-poll aria-live="polite" hx-get="/inbound/queue/fragment" hx-trigger="load" hx-swap="outerHTML"`,
		`Nothing is waiting. Every accepted message has reached its upstream.`, `href="/help#inbound-queue"`,
		`back to the original sender`,
	)
	rows := []QueueEntry{{ID: "IN0001", Queue: "deferred", Arrival: time.Date(2026, 10, 18, 9, 0, 0, 0, time.Local), Size: 5000,
		Sender: "someone@example.net", Recipient: "staff@lists.example.com", Reason: "451 4.7.1 Greylisted"}}
	out = renderSignedIn(t, "in-queue", NewInQueue(admin(), rows, ""))
	pageHas(t, "Inbound queue", out, `1 message · 4.9 kB`, `<td class="sp-mono">IN0001</td>`, `staff@<wbr>lists.example.com`, `451 4.7.1 Greylisted`,
		`<th>Last reply</th></tr>`)
	pageLacks(t, "Inbound queue", out, "Details", `sp-actions`)
}
