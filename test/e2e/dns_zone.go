package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// dnsAddr is where compose.override.yml publishes CoreDNS's UDP port, so the
// test process (running on the host, not inside the e2e network) can resolve
// through the same fake zone Postfix and the panel see via the `dns:` compose
// directive pointing containers at CoreDNS's static 10.77.0.10.
const dnsAddr = "127.0.0.1:20053"

// e2eResolver returns a *net.Resolver that only ever talks to the fake zone's
// CoreDNS, for both direct assertions in tests and as the DKIM verifier's
// LookupTXT.
func e2eResolver() *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, dnsAddr)
		},
	}
}

// txtRecord is one name -> TXT value pair the harness publishes into the fake
// zone (the DKIM record a domain's page told us to publish).
type txtRecord struct {
	name  string // relative to e2e.test, e.g. "selfpost._domainkey.sender"
	value string
}

// writeZone (re)writes the whole authoritative db.zone for e2e.test: the fixed
// records CoreDNS/sink need to be reachable, plus whatever TXT records the
// test has published so far. CoreDNS's `reload 2s` (see dns/Corefile) picks up
// the new mtime with no signal needed.
func writeZone(stageDir string, extra []txtRecord) error {
	var b strings.Builder
	fmt.Fprintf(&b, "$ORIGIN e2e.test.\n$TTL 300\n")
	fmt.Fprintf(&b, "@       IN SOA  ns.e2e.test. admin.e2e.test. ( %d 3600 900 604800 300 )\n", time.Now().Unix())
	fmt.Fprintf(&b, "@       IN NS   ns.e2e.test.\n")
	fmt.Fprintf(&b, "ns      IN A    10.77.0.10\n")
	// The sink-MX: Postfix's outbound delivery for the recipient domain used by
	// every positive-path send resolves here via the domain's implicit MX
	// fallback to its own A record (RFC 5321) — no explicit MX record needed.
	fmt.Fprintf(&b, "sink    IN A    10.77.0.11\n")
	for _, r := range extra {
		fmt.Fprintf(&b, "%s IN TXT %s\n", r.name, chunkTXT(r.value))
	}
	return os.WriteFile(filepath.Join(stageDir, "dns-stage", "db.zone"), []byte(b.String()), 0o644)
}

// chunkTXT splits a TXT value into <255-byte quoted strings (the DNS
// <character-string> limit), which resolvers concatenate back into one value.
// A DKIM RSA-2048 public key's base64 comfortably exceeds 255 bytes on its
// own, so this is required, not defensive.
func chunkTXT(value string) string {
	const max = 255
	var parts []string
	for len(value) > max {
		parts = append(parts, value[:max])
		value = value[max:]
	}
	parts = append(parts, value)
	var quoted []string
	for _, p := range parts {
		quoted = append(quoted, `"`+strings.ReplaceAll(p, `"`, `\"`)+`"`)
	}
	return strings.Join(quoted, " ")
}

// publishTXT rewrites the zone with rec added/replacing any prior record of
// the same name, then blocks until CoreDNS actually serves the new value —
// so the caller never has to sleep-and-hope for the reload interval to pass.
func publishTXT(stageDir string, existing []txtRecord, rec txtRecord) ([]txtRecord, error) {
	updated := make([]txtRecord, 0, len(existing)+1)
	for _, r := range existing {
		if r.name != rec.name {
			updated = append(updated, r)
		}
	}
	updated = append(updated, rec)
	if err := writeZone(stageDir, updated); err != nil {
		return nil, err
	}
	res := e2eResolver()
	fqdn := rec.name + ".e2e.test"
	err := waitFor(fmt.Sprintf("CoreDNS to serve TXT %s", fqdn), 20*time.Second, 300*time.Millisecond, func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		txt, err := res.LookupTXT(ctx, fqdn)
		if err != nil {
			return false, err
		}
		for _, t := range txt {
			if t == rec.value {
				return true, nil
			}
		}
		return false, fmt.Errorf("got %v, want %q", txt, rec.value)
	})
	return updated, err
}

// lookupTXTFunc adapts e2eResolver to the shape dkim.VerifyOptions.LookupTXT
// wants (a synchronous domain -> []string call, no context).
func lookupTXTFunc() func(string) ([]string, error) {
	res := e2eResolver()
	return func(domain string) ([]string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return res.LookupTXT(ctx, domain)
	}
}
