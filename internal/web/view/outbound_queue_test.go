package view

import (
	"strings"
	"testing"
)

// ---- Outbound queue

// The page prints postqueue's listing as it is and polls from its box; the
// retry policy sits under it, outside the poll.
func TestOutQueueShowsTheListingAndPollsItsBox(t *testing.T) {
	out := renderSignedIn(t, "out-queue", outQueueFixture())
	pageHas(t, "Outbound queue", out,
		`<h1 class="title is-3">Outbound queue</h1>`, `<h2>Waiting</h2>`,
		`id="out-queue" data-poll aria-live="polite" hx-get="/outbound/queue/fragment" hx-trigger="load" hx-swap="outerHTML"`,
		`class="sp-log"`, `-Queue ID-  --Size-- `, `4XcB7k2Jm9z1     3174 Sun Sep 21 13:58:07  alerts@example.org`,
		`-- 3 Kbytes in 1 Request.`,
		`aria-current="page"><i class="ti ti-stack-2"></i>Queue</a>`,
	)
	// What Postfix printed is escaped, not interpreted.
	pageHas(t, "Outbound queue", out, `(host mx1.example.org[192.0.2.10] said: 451 4.7.1 Greylisted`)
	pageLacks(t, "Outbound queue", out, "<table", "Queue is empty.")

	frag := renderFragment(t, "out_queue_body", outQueueFixture())
	if !strings.HasPrefix(frag, `<div class="box" id="out-queue" data-poll`) || strings.Contains(frag, "sp-head") || strings.Contains(frag, "How retries work") {
		t.Errorf("the fragment is not the polled box alone:\n%.200s", frag)
	}
	if strings.Count(out, `class="sp-log"`) != 1 || strings.Count(frag, `class="sp-log"`) != 1 {
		t.Error("the page and its fragment should each carry the listing once")
	}
}

// The policy is the page's, wherever it was read from: its facts, and a head
// that says so when they are the compiled-in defaults.
func TestOutQueueStatesTheRetryPolicy(t *testing.T) {
	out := renderSignedIn(t, "out-queue", NewOutQueue(admin(), "", "").WithRetryPolicy("10 minutes", "about 1 hour 7 minutes", "2 days", false))
	pageHas(t, "Outbound queue", out,
		`<h2>How retries work</h2>`, `This Postfix&#39;s policy, read at panel start`,
		`<dd class="sp-big">10 minutes</dd>`, `<dd class="sp-big">doubling</dd>`, `capped at about 1 hour 7 minutes`,
		`<dd class="sp-big">2 days</dd>`, `<dd class="sp-big">bounced</dd>`, `no attempt counter, only time`,
	)
	pageLacks(t, "Outbound queue", out, "Compiled-in defaults")

	out = renderSignedIn(t, "out-queue", NewOutQueue(admin(), "", "").WithRetryPolicy("5 minutes", "about 1 hour 7 minutes", "5 days", true))
	pageHas(t, "Outbound queue", out, "Compiled-in defaults: the Postfix configuration could not be read")
	pageLacks(t, "Outbound queue", out, "read at panel start")
}

// With nothing to list the box says why instead: the queue holds nothing, or
// it could not be read — and never both.
func TestOutQueueSaysWhyItHasNoListing(t *testing.T) {
	out := renderSignedIn(t, "out-queue", NewOutQueue(admin(), "", ""))
	pageHas(t, "empty queue", out, `class="sp-empty"`, "Queue is empty.")
	pageLacks(t, "empty queue", out, `class="sp-log"`, "Could not read")

	out = renderSignedIn(t, "out-queue", NewOutQueue(admin(), "ignored", "Could not read the mail queue."))
	pageHas(t, "unreadable queue", out, `class="sp-empty"`, "Could not read the mail queue.")
	pageLacks(t, "unreadable queue", out, `class="sp-log"`, "Queue is empty.", "ignored")
}
