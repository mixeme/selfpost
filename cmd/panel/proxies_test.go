package main

import (
	"net"
	"testing"
)

// A bare address is one host however it is written; an IPv4-mapped one in
// IPv6 notation used to become ::/32 (audit 2026-10-12, L4).
func TestParseTrustedProxiesTakesABareAddressAsOneHost(t *testing.T) {
	for _, c := range []struct {
		entry   string
		trusts  []string
		refuses []string
	}{
		{"10.0.0.1", []string{"10.0.0.1"}, []string{"10.0.0.2"}},
		{"::ffff:10.0.0.1", []string{"10.0.0.1", "::ffff:10.0.0.1"}, []string{"10.0.0.2", "::1", "0:0:1::1"}},
		{"2001:db8::1", []string{"2001:db8::1"}, []string{"2001:db8::2"}},
		{"10.0.0.0/8", []string{"10.1.2.3"}, []string{"11.0.0.1"}},
	} {
		nets := parseTrustedProxies(c.entry)
		if len(nets) != 1 {
			t.Fatalf("%q parsed as %v", c.entry, nets)
		}
		for _, ip := range c.trusts {
			if !nets[0].Contains(net.ParseIP(ip)) {
				t.Errorf("%q (%v) does not trust %s", c.entry, nets[0], ip)
			}
		}
		for _, ip := range c.refuses {
			if nets[0].Contains(net.ParseIP(ip)) {
				t.Errorf("%q (%v) trusts %s", c.entry, nets[0], ip)
			}
		}
	}
}
