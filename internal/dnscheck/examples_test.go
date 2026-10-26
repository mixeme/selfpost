package dnscheck

import (
	"context"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/health"
)

// The record names the server's host, never its addresses: a move to another
// address then leaves every domain's SPF as it is.
func TestSPFExample(t *testing.T) {
	if got, want := SPFExample("mail.example.com"), "v=spf1 a:mail.example.com -all"; got != want {
		t.Errorf("SPFExample = %q, want %q", got, want)
	}
}

func TestDMARCExample(t *testing.T) {
	if got := DMARCExample(""); got != "v=DMARC1; p=none" {
		t.Errorf("empty = %q", got)
	}
	want := "v=DMARC1; p=none; rua=mailto:reports@hub.example"
	if got := DMARCExample("reports@hub.example"); got != want {
		t.Errorf("with rua = %q, want %q", got, want)
	}
}

func TestExternalReportAuth(t *testing.T) {
	name, value, ok := ExternalReportAuth("shop.com", "reports@hub.com")
	if !ok || name != "_report._dmarc.hub.com" || value != "v=DMARC1;" {
		t.Fatalf("external = (%q, %q, %v)", name, value, ok)
	}
	if _, _, ok := ExternalReportAuth("shop.com", "dmarc@shop.com"); ok {
		t.Fatal("same domain should not need external auth")
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
	if want := SPFExample("mail.example.com"); !strings.Contains(spf.Detail, want) {
		t.Errorf("SPF advice %q does not suggest %q", spf.Detail, want)
	}

	dmarc := c.checkDMARC(context.Background(), Query{Name: "example.com"})
	if dmarc.Status != health.StatusWarn {
		t.Fatalf("DMARC status = %q, want warn (%s)", dmarc.Status, dmarc.Detail)
	}
	if want := DMARCExample(""); !strings.Contains(dmarc.Detail, want) {
		t.Errorf("DMARC advice %q does not suggest %q", dmarc.Detail, want)
	}
}
