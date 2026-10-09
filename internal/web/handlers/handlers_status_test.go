package handlers

import (
	"testing"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/health"
)

// checksFixture is one reading of every server check, shaped as healthChecks
// returns it.
func checksFixture() map[string]any {
	return map[string]any{
		"Processes": []health.Process{
			{Name: "opendkim", State: "RUNNING", Status: health.StatusOK},
			{Name: "postfix", State: "FATAL", Status: health.StatusError},
		},
		"ProcessError":  false,
		"ProcessStatus": health.StatusError,
		"QueueSummary":  "Mail queue is empty",
		"QueueError":    "",
		"QueueStatus":   health.StatusOK,
		"Machine":       health.Machine{Status: health.StatusUnknown},
		"Cert":          health.Certificate{Subject: "mail.example.org", DaysLeft: 12, Status: health.StatusWarn, Detail: "Expires soon."},
		"Sockets": []health.Socket{
			{Name: "OpenDKIM", Present: true, Status: health.StatusOK},
			{Name: "send-log", Present: false, Status: health.StatusWarn},
		},
		"SocketStatus": health.StatusWarn,
		"Hostname":     "mail.example.org",
		"PTR":          dnscheck.Result{Status: health.StatusOK},
	}
}

// Overview is one verdict per check: six cards in the order of the tables on
// Health, each saying in words what its table would, and each leading there.
func TestHealthCards(t *testing.T) {
	cards := healthCards(checksFixture())
	want := []struct{ name, level, value, sub, href string }{
		{"Machine", "warn", "No reading", "", "/server/health#machine"},
		{"Processes", "fail", "1 of 2 running", "postfix", "/server/health#processes"},
		{"TLS certificate", "warn", "Expires in 12 days", "mail.example.org", "/server/health#certificate"},
		{"Queue", "", "Empty", "outbound", "/outbound/queue"},
		{"Milter sockets", "warn", "1 of 2 answering", "OpenDKIM, send-log", "/server/health#sockets"},
		{"Reverse DNS", "", "Forward = reverse", "mail.example.org", "/server/health#hostname"},
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

	// A check that could not run must not look fine.
	c := checksFixture()
	c["ProcessError"] = true
	c["ProcessStatus"] = health.StatusUnknown
	c["PTR"] = dnscheck.Result{Status: health.StatusUnknown}
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
