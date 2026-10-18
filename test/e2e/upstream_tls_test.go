package e2e

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// verifiedDomain is an inbound domain of its own for Verified TLS, so that its
// upstream - a next hop with a TLS mode of its own - touches no other step.
const verifiedDomain = "verified.e2e.test"

// testVerifiedUpstreamTLS is upstream-tls-verify (owner, 2026-10-18): the
// panel saves Verified TLS for an upstream given by name and writes Postfix's
// secure level for it, refuses it for one given by address, and Postfix's
// client trusts Debian's authorities and so refuses a certificate none of
// them issued. The stand has no upstream that speaks TLS (smtp-sink does
// not), so the last is asked of posttls-finger - Postfix's own TLS client,
// with the client's CA file - against the stand's own port 25, whose
// certificate is self-signed: "Untrusted", never "Verified" (Postfix 3.10.13,
// src/tls/tls_misc.c:1252, the summary line's first word).
func testVerifiedUpstreamTLS(t *testing.T, sc *scenario) {
	resp, body, err := sc.panel.postForm("/inbound/domains", url.Values{"name": {verifiedDomain}})
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`^/inbound/domains/(\d+)$`).FindStringSubmatch(resp.Request.URL.Path)
	if m == nil {
		t.Fatalf("add %s: landed on %s:\n%.800s", verifiedDomain, resp.Request.URL.Path, body)
	}
	base := "/inbound/domains/" + m[1]
	defer sc.panel.postForm(base+"/delete", url.Values{})

	// By address: refused, nothing saved.
	resp, body, err = sc.panel.postForm(base+"/upstream", url.Values{"host": {"10.77.0.11"}, "port": {"25"}, "tls_mode": {"secure"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 || !strings.Contains(body, "give the upstream by name") {
		t.Fatalf("Verified TLS to an address: status %d:\n%.800s", resp.StatusCode, body)
	}
	// By name: saved, and the policy map says secure for that next hop.
	resp, body, err = sc.panel.postForm(base+"/upstream", url.Values{"host": {"sink"}, "port": {"25"}, "tls_mode": {"secure"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || resp.Request.URL.Path != base {
		t.Fatalf("Verified TLS to a name: status %d, landed on %s:\n%.800s", resp.StatusCode, resp.Request.URL.Path, body)
	}
	if out, err := h.execIn("selfpost", "cat", "/data/postfix/tls_policy"); err != nil || !strings.Contains(out, "[sink]:25 secure\n") {
		t.Errorf("tls_policy has no secure line for [sink]:25 (%v):\n%s", err, out)
	}
	if _, page, err := sc.panel.get(base); err != nil || !strings.Contains(page, "over verified TLS") {
		t.Errorf("the domain's page does not say verified TLS (%v)", err)
	}

	// The client's trust: the bundle is set and there, and a self-signed
	// certificate is not trusted with it.
	out, err := h.execIn("selfpost", "sh", "-c", `f=$(postconf -h smtp_tls_CAfile) && echo "$f" && test -s "$f" && echo present`)
	if err != nil || !strings.Contains(out, "/etc/ssl/certs/ca-certificates.crt\npresent") {
		t.Fatalf("smtp_tls_CAfile (%v):\n%s", err, out)
	}
	out, _ = h.execIn("selfpost", "posttls-finger", "-c", "-l", "secure", "-F", "/etc/ssl/certs/ca-certificates.crt", "[127.0.0.1]:25")
	if !strings.Contains(out, "Untrusted TLS connection established") || strings.Contains(out, "Verified TLS connection") {
		t.Errorf("posttls-finger -l secure against the stand's self-signed port 25:\n%s", out)
	}
}
