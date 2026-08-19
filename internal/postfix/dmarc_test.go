package postfix

import (
	"strings"
	"testing"
)

func TestRenderDMARCMaps(t *testing.T) {
	recip, transport, relay, err := renderDMARCMaps(DMARCMapsConfig{
		Recipients: []string{"dmarc-reports@mail.example.com"},
		Domains:    []string{"mail.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(recip), "dmarc-reports@mail.example.com OK") {
		t.Fatalf("recipients:\n%s", recip)
	}
	if !strings.Contains(string(transport), "dmarc-ingest:") {
		t.Fatalf("transport:\n%s", transport)
	}
	if !strings.Contains(string(relay), "mail.example.com OK") {
		t.Fatalf("relay:\n%s", relay)
	}
}

func TestRenderDMARCMapsEmpty(t *testing.T) {
	recip, transport, relay, err := renderDMARCMaps(DMARCMapsConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if len(recip)+len(transport)+len(relay) != 0 {
		t.Fatalf("expected empty maps")
	}
}

func TestRenderDMARCMapsRejectsInjectionTokens(t *testing.T) {
	_, _, _, err := renderDMARCMaps(DMARCMapsConfig{
		Recipients: []string{"dmarc@example.com\nattacker@example.com"},
		Domains:    []string{"example.com"},
	})
	if err == nil {
		t.Fatal("expected recipient token validation error")
	}

	_, _, _, err = renderDMARCMaps(DMARCMapsConfig{
		Recipients: []string{"dmarc@example.com"},
		Domains:    []string{"example.com attacker"},
	})
	if err == nil {
		t.Fatal("expected domain token validation error")
	}
}
