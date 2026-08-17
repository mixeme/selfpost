package postfix

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenderInboundMaps(t *testing.T) {
	routes := []InboundRoute{
		{
			Domain: "zeta.example", Host: "192.0.2.20", Port: 25,
			TLSMode: "none", RecipientMode: "any",
		},
		{
			Domain: "lists.example.com", Host: "10.0.0.8", Port: 25,
			TLSMode: "encrypt", RecipientMode: "list",
			Recipients: []string{"staff@lists.example.com", "abuse@lists.example.com"},
		},
		{
			Domain: "pending.example", Host: "", Port: 25,
			TLSMode: "may", RecipientMode: "list",
		},
	}
	relay, transport, recipients, tlsPolicy, err := renderInboundMaps(routes)
	if err != nil {
		t.Fatal(err)
	}
	wantRelay := "lists.example.com OK\nzeta.example OK\n"
	if string(relay) != wantRelay {
		t.Errorf("relay_domains =\n%q\nwant\n%q", relay, wantRelay)
	}
	wantTransport := "lists.example.com smtp:[10.0.0.8]:25\nzeta.example smtp:[192.0.2.20]:25\n"
	if string(transport) != wantTransport {
		t.Errorf("transport =\n%q\nwant\n%q", transport, wantTransport)
	}
	wantRecipients := "abuse@lists.example.com OK\nstaff@lists.example.com OK\n@zeta.example OK\n"
	if string(recipients) != wantRecipients {
		t.Errorf("relay_recipients =\n%q\nwant\n%q", recipients, wantRecipients)
	}
	wantTLS := "[10.0.0.8]:25 encrypt\n[192.0.2.20]:25 none\n"
	if string(tlsPolicy) != wantTLS {
		t.Errorf("tls_policy =\n%q\nwant\n%q", tlsPolicy, wantTLS)
	}
}

func TestRenderInboundMapsIPv6(t *testing.T) {
	routes := []InboundRoute{{
		Domain: "v6.example", Host: "2001:db8::1", Port: 25,
		TLSMode: "may", RecipientMode: "any",
	}}
	_, transport, _, tlsPolicy, err := renderInboundMaps(routes)
	if err != nil {
		t.Fatal(err)
	}
	if string(transport) != "v6.example smtp:[2001:db8::1]:25\n" {
		t.Errorf("transport = %q", transport)
	}
	if string(tlsPolicy) != "[2001:db8::1]:25 may\n" {
		t.Errorf("tls_policy = %q", tlsPolicy)
	}
}

func TestRenderInboundMapsRejectsInjection(t *testing.T) {
	bad := []InboundRoute{
		{Domain: "ex ample.com", Host: "10.0.0.1", Port: 25, TLSMode: "may", RecipientMode: "any"},
		{Domain: "example.com", Host: "10.0.0.1\nrelay", Port: 25, TLSMode: "may", RecipientMode: "any"},
		{Domain: "example.com", Host: "10.0.0.1", Port: 25, TLSMode: "evil", RecipientMode: "any"},
		{Domain: "example.com", Host: "10.0.0.1", Port: 25, TLSMode: "may", RecipientMode: "list",
			Recipients: []string{"a@example.com OK\nb@evil.com"}},
	}
	for i, r := range bad {
		if _, _, _, _, err := renderInboundMaps([]InboundRoute{r}); err == nil {
			t.Errorf("case %d: expected injection rejection", i)
		}
	}
}

func TestRebuildInboundMapsWritesAndReloads(t *testing.T) {
	p, reloads := newTestPostfix(t)
	err := p.RebuildInboundMaps([]InboundRoute{{
		Domain: "lists.example.com", Host: "10.0.0.8", Port: 25,
		TLSMode: "encrypt", RecipientMode: "any",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if *reloads != 1 {
		t.Errorf("reload called %d times, want 1", *reloads)
	}
	rd, _, _, _ := p.inboundMapPaths()
	data, err := os.ReadFile(rd)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "lists.example.com OK\n" {
		t.Errorf("relay_domains file = %q", data)
	}
	if filepath.Base(rd) != "relay_domains" {
		t.Errorf("unexpected path %s", rd)
	}
}

func TestRebuildInboundMapsEmptyOmitsPending(t *testing.T) {
	p, _ := newTestPostfix(t)
	if err := p.RebuildInboundMaps(nil); err != nil {
		t.Fatal(err)
	}
	rd, tr, rc, tl := p.inboundMapPaths()
	for _, path := range []string{rd, tr, rc, tl} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) != 0 {
			t.Errorf("%s not empty: %q", path, data)
		}
	}
}
