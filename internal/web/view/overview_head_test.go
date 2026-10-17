package view

import (
	"testing"
	"time"
)

// The head says whether mail flows and how many things need attention, and
// the lead names each with its value inside the sentence (mockup
// overview.html; the owner's "1", 2026-10-17).
func TestOverviewHeadSentences(t *testing.T) {
	ok := HealthCard{Name: "Processes", Value: "5 of 5 running", Href: "/server/health"}
	cert := HealthCard{Name: "TLS certificate", Value: "Expires in 12 days", Href: "/server/health", Level: LevelWarn}
	cpu := HealthCard{Name: "Machine", Value: "CPU 98 %", Href: "/server/health", Level: LevelWarn}
	postfix := HealthCard{Name: "Processes", Value: "4 of 5 running", Href: "/server/health", Level: LevelFail}
	for _, c := range []struct {
		name        string
		cards       []HealthCard
		title, lead string
	}{
		{"all fine", []HealthCard{ok}, "Mail is flowing. Nothing needs attention.", ""},
		{"one warning", []HealthCard{ok, cert}, "Mail is flowing. One thing needs attention.", "TLS certificate: expires in 12 days."},
		{"two warnings", []HealthCard{cert, cpu}, "Mail is flowing. 2 things need attention.", "TLS certificate: expires in 12 days; Machine: CPU 98 %."},
		{"a failure", []HealthCard{postfix, cert}, "Mail may not be flowing. 2 things need attention.", "Processes: 4 of 5 running; TLS certificate: expires in 12 days."},
	} {
		h := overviewHead("mail.example.org", c.cards, time.Now())
		if h.Title != c.title {
			t.Errorf("%s: title %q, want %q", c.name, h.Title, c.title)
		}
		var lead string
		for _, in := range h.Lead {
			lead += in.Text
		}
		if lead != c.lead {
			t.Errorf("%s: lead %q, want %q", c.name, lead, c.lead)
		}
	}
}
