package handlers

import (
	"bytes"
	"strings"
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
		"SocketStatus":   health.StatusWarn,
		"Hostname":       "mail.example.org",
		"PTR":            dnscheck.Result{Status: health.StatusOK},
		"OverallStatus":  health.StatusError,
		"OverallHeading": "A component needs attention — see the details below.",
	}
}

// Overview is one verdict per check: six cards in the order of the tables on
// Health, each saying in words what its table would, and each leading there.
func TestHealthCards(t *testing.T) {
	cards := healthCards(checksFixture())
	want := []struct{ name, level, value, href string }{
		{"Machine", "warn", "No reading", "/server/health#machine"},
		{"Processes", "fail", "1 of 2 running", "/server/health#processes"},
		{"TLS certificate", "warn", "Expires in 12 days", "/server/health#certificate"},
		{"Queue", "", "Mail queue is empty", "/outbound/queue"},
		{"Milter sockets", "warn", "1 of 2 answering", "/server/health#sockets"},
		{"Reverse DNS", "", "Forward = reverse", "/server/health#hostname"},
	}
	if len(cards) != len(want) {
		t.Fatalf("%d cards, want %d", len(cards), len(want))
	}
	for i, w := range want {
		c := cards[i]
		if c.Name != w.name || c.Level != w.level || c.Value != w.value || c.Href != w.href || c.Icon == "" {
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

// The two pages share the checks but not the content: Overview shows the
// verdicts and polls its own fragment; Health shows the tables, polls its own,
// and is the only place with the two server actions.
func TestOverviewAndHealthFragments(t *testing.T) {
	h, _ := settingsServer(t)
	render := func(data map[string]any) string {
		t.Helper()
		var buf bytes.Buffer
		if err := h.view.Page("status").ExecuteTemplate(&buf, "content", data); err != nil {
			t.Fatalf("render: %v", err)
		}
		return buf.String()
	}

	full := checksFixture()
	overview := render(map[string]any{
		"Overview": true, "OverallStatus": full["OverallStatus"], "OverallHeading": full["OverallHeading"],
		"Cards": healthCards(full),
	})
	for _, want := range []string{
		"<h1>Overview</h1>", `hx-get="/overview/fragment"`, `href="/server/health#certificate"`,
		"Expires in 12 days", "1 of 2 running", `href="/server/health"`,
	} {
		if !strings.Contains(overview, want) {
			t.Errorf("Overview is missing %q", want)
		}
	}
	for _, gone := range []string{`action="/server/health/reload"`, `action="/server/health/recheck"`, `id="processes"`, "<meter", `hx-get="/server/health/fragment"`} {
		if strings.Contains(overview, gone) {
			t.Errorf("Overview still carries %q — the tables and the actions live on Health", gone)
		}
	}

	healthPage := render(full)
	for _, want := range []string{
		"<h1>Health</h1>", `hx-get="/server/health/fragment"`, `action="/server/health/reload"`,
		`action="/server/health/recheck"`, `id="processes"`, `id="certificate"`, `id="sockets"`, `id="hostname"`, `id="machine"`,
	} {
		if !strings.Contains(healthPage, want) {
			t.Errorf("Health is missing %q", want)
		}
	}
	if strings.Contains(healthPage, `hx-get="/overview/fragment"`) || strings.Contains(healthPage, `id="verdicts"`) {
		t.Error("Health polls or shows the Overview fragment")
	}
}
