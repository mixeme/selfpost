package handlers

import (
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/web/view"
)

// checksFixture is one reading of every server check, shaped as healthChecks
// returns it.
func checksFixture() healthReadings {
	return healthReadings{
		Processes: []health.Process{
			{Name: "panel", State: "RUNNING", Status: health.StatusOK},
			{Name: "postfix", State: "FATAL", Status: health.StatusError},
		},
		ProcessError:  false,
		ProcessStatus: health.StatusError,
		QueueSummary:  "Mail queue is empty",
		QueueError:    "",
		QueueStatus:   health.StatusOK,
		Machine:       health.Machine{Status: health.StatusUnknown},
		Cert:          health.Certificate{Subject: "mail.example.org", DaysLeft: 12, Status: health.StatusWarn, Detail: "Expires soon."},
		Sockets: []health.Socket{
			{Name: "dkim-signer", Present: true, Status: health.StatusOK},
			{Name: "send-log", Present: false, Status: health.StatusWarn},
		},
		SocketStatus: health.StatusWarn,
		Hostname:     "mail.example.org",
		PTR:          dnscheck.Result{Status: health.StatusOK},
	}
}

// Overview is one verdict per check: six cards in the order of the tables on
// Health, each saying in words what its table would, and each leading there.
func TestHealthCards(t *testing.T) {
	cards := healthCards(checksFixture())
	want := []struct{ name, level, value, sub, href string }{
		{"Machine", "warn", "No reading", "", "/server/health"},
		{"Processes", "fail", "1 of 2 running", "postfix", "/server/health"},
		{"TLS certificate", "warn", "Expires in 12 days", "mail.example.org", "/server/health"},
		{"Queue", "", "Empty", "outbound", "/outbound/queue"},
		{"Milter sockets", "warn", "1 of 2 answering", "send-log", "/server/health"},
		{"Reverse DNS", "", "Forward = reverse", "mail.example.org", "/server/health"},
	}
	if len(cards) != len(want) {
		t.Fatalf("%d cards, want %d", len(cards), len(want))
	}
	for i, w := range want {
		c := cards[i]
		if c.Name != w.name || c.Level != w.level || c.Value != w.value || c.Sub != w.sub || c.Href != w.href || c.Icon == "" {
			t.Errorf("card %d = %+v, want %+v", i, c, w)
		}
	}

	// With the inbound relay on, the one queue is both directions'.
	in := checksFixture()
	in.Inbound = true
	if q := healthCards(in)[3]; q.Name != "Queues" || q.Value != "Both empty" || q.Sub != "outbound and inbound" {
		t.Errorf("queue card with the relay on = %+v", q)
	}

	// A check that could not run must not look fine.
	c := checksFixture()
	c.ProcessError = true
	c.ProcessStatus = health.StatusUnknown
	c.PTR = dnscheck.Result{Status: health.StatusUnknown}
	cards = healthCards(c)
	if cards[1].Level != "warn" || cards[1].Value != "Could not be read" || cards[5].Level != "warn" || cards[5].Value != "Not checked" {
		t.Errorf("unverified checks render as %+v and %+v", cards[1], cards[5])
	}
}

// The Queue card has room for a few words: postqueue's own last line is cut
// down to what it says.
func TestQueueCardValue(t *testing.T) {
	for in, want := range map[string]string{
		"":                           "Empty",
		"Mail queue is empty":        "Empty",
		"3 Kbytes in 2 Requests.":    "2 queued",
		"1 Kbytes in 1 Request.":     "1 queued",
		"something postfix invented": "something postfix invented",
	} {
		if got := queueCardValue(in); got != want {
			t.Errorf("queueCardValue(%q) = %q, want %q", in, got, want)
		}
	}
}

// The Processes card counts the programs that are meant to stay up. The one-shot
// reload helper idles between runs: it is neither running nor missing.
func TestProcessesCardLeavesOutTheIdleOneShot(t *testing.T) {
	c := checksFixture()
	c.Processes = []health.Process{
		{Name: "panel", State: "RUNNING", Status: health.StatusOK},
		{Name: "postfix", State: "RUNNING", Status: health.StatusOK},
		{Name: "postfix-reload", State: "STOPPED", Status: health.StatusOK, OneShot: true},
	}
	c.ProcessStatus = health.StatusOK
	card := healthCards(c)[1]
	if card.Value != "2 of 2 running" || card.Sub != "all programs" {
		t.Errorf("card = %q / %q, want 2 of 2 running / all programs", card.Value, card.Sub)
	}

	c.Processes = []health.Process{
		{Name: "panel", State: "RUNNING", Status: health.StatusOK},
		{Name: "postfix-reload", State: "FATAL", Status: health.StatusError, OneShot: true},
	}
	card = healthCards(c)[1]
	if card.Value != "1 of 1 running" || card.Sub != "postfix-reload" {
		t.Errorf("card = %q / %q, want 1 of 1 running / postfix-reload", card.Value, card.Sub)
	}
}

// An idle one-shot helper is "on demand", not "stopped"; a daemon keeps its
// state word, and a one-shot in trouble keeps its own.
func TestProcessRowOfAnIdleOneShotSaysOnDemand(t *testing.T) {
	idle := processRow(health.Process{Name: "postfix-reload", State: "STOPPED", Detail: "Not started", Status: health.StatusOK, OneShot: true})
	if idle.State.Label != "on demand" || idle.State.Status != "off" || !strings.Contains(idle.Detail, "not run since the container started") {
		t.Errorf("idle one-shot = %+v", idle)
	}
	ran := processRow(health.Process{Name: "postfix-reload", State: "EXITED", Detail: "Oct 10 08:42 PM", Status: health.StatusOK, OneShot: true})
	if ran.State.Label != "on demand" || !strings.HasSuffix(ran.Detail, "last run: Oct 10 08:42 PM") {
		t.Errorf("one-shot that ran = %+v", ran)
	}
	bad := processRow(health.Process{Name: "postfix-reload", State: "FATAL", Detail: "Exited too quickly", Status: health.StatusError, OneShot: true})
	if bad.State.Label != "fatal" || bad.State.Status != "error" || bad.Detail != "Exited too quickly" {
		t.Errorf("one-shot in trouble = %+v", bad)
	}
	daemon := processRow(health.Process{Name: "postfix", State: "STOPPED", Detail: "Oct 10 08:42 PM", Status: health.StatusError})
	if daemon.State.Label != "stopped" || daemon.State.Status != "error" {
		t.Errorf("stopped daemon = %+v", daemon)
	}
}

// Health's Hostname card shows Postfix's greeting on a row of its own, and
// warns when it names another host; the certificate's names carry whether
// they are the hostname's.
func TestHealthGreetingAndCertificateName(t *testing.T) {
	c := checksFixture()
	c.Banner = "mail.example.org ESMTP"
	matches := true
	c.Cert.Names, c.Cert.MatchesHost = []string{"mail.example.org"}, &matches
	page := healthPage(view.Meta{}, c, "")
	var greeting *view.Fact
	for i := range page.HostFacts {
		if page.HostFacts[i].Label == "Greeting" {
			greeting = &page.HostFacts[i]
		}
	}
	if greeting == nil || greeting.Value[0].Text != "220 mail.example.org ESMTP" || !greeting.Wide || greeting.Note != nil {
		t.Fatalf("greeting = %+v", greeting)
	}
	names := func(p *view.Health) *view.Tag {
		for _, f := range p.CertFacts {
			if f.Label == "Names" {
				return f.LabelTag
			}
		}
		return nil
	}
	if tag := names(page); tag == nil || tag.Label != "matches" {
		t.Errorf("names = %+v", page.CertFacts)
	}

	c.Banner = "3f2a9c1d0e7b ESMTP"
	matches = false
	page = healthPage(view.Meta{}, c, "")
	if end := page.Hostname.End; len(end) == 0 || end[0].Tag == nil || end[0].Tag.Status != "warn" {
		t.Errorf("a greeting with a container id: card %+v, want warn", page.Hostname.End)
	}
	if tag := names(page); tag == nil || tag.Status != "fail" || tag.Label != "not for the hostname" {
		t.Errorf("a certificate for another name: %+v", tag)
	}
}
