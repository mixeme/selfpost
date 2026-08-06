package dnscheck

import (
	"context"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/health"
)

func TestSPFExample(t *testing.T) {
	cases := []struct {
		name     string
		hostname string
		ips      []string
		want     string
	}{
		{"ipv4", "mail.example.com", []string{"203.0.113.10"}, "v=spf1 ip4:203.0.113.10 -all"},
		{"both families", "mail.example.com", []string{"203.0.113.10", "2001:db8::1"},
			"v=spf1 ip4:203.0.113.10 ip6:2001:db8::1 -all"},
		// The hostname does not resolve, so there is no address to name; an "a:"
		// mechanism still gives the operator a publishable record.
		{"no addresses", "mail.example.com", nil, "v=spf1 a:mail.example.com -all"},
		{"unparsable addresses", "mail.example.com", []string{"not-an-ip"}, "v=spf1 a:mail.example.com -all"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SPFExample(c.hostname, c.ips); got != c.want {
				t.Errorf("SPFExample = %q, want %q", got, c.want)
			}
		})
	}
}

// The record the panel shows and the one a failed check suggests must be the
// same string, or the operator is told two different things on one page.
func TestMissingRecordChecksSuggestTheShownExample(t *testing.T) {
	f := &fakeResolver{}
	c := newTestChecker(f)

	spf := c.checkSPF(context.Background(), Query{
		Name:      "example.com",
		Hostname:  "mail.example.com",
		ServerIPs: []string{"203.0.113.10"},
	})
	if spf.Status != health.StatusError {
		t.Fatalf("SPF status = %q, want error (%s)", spf.Status, spf.Detail)
	}
	if want := SPFExample("mail.example.com", []string{"203.0.113.10"}); !strings.Contains(spf.Detail, want) {
		t.Errorf("SPF advice %q does not suggest %q", spf.Detail, want)
	}

	dmarc := c.checkDMARC(context.Background(), "example.com")
	if dmarc.Status != health.StatusWarn {
		t.Fatalf("DMARC status = %q, want warn (%s)", dmarc.Status, dmarc.Detail)
	}
	if want := DMARCExample("example.com"); !strings.Contains(dmarc.Detail, want) {
		t.Errorf("DMARC advice %q does not suggest %q", dmarc.Detail, want)
	}
}
