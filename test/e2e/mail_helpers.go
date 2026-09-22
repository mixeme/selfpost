package e2e

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/emersion/go-msgauth/dkim"
)

// findSinkMessage polls the sink-MX's dump directory (see test/e2e/sink) for
// a file whose contents contain token — the unique Subject-line marker every
// test send carries — and returns it. Polling a directory listing rather than
// sleeping a fixed duration is what keeps this deterministic even under a
// slow CI runner (plan C.4).
func findSinkMessage(stageDir, token string, timeout time.Duration) ([]byte, error) {
	dir := filepath.Join(stageDir, "mail-stage")
	var found []byte
	err := waitFor(fmt.Sprintf("sink-MX to receive a message tagged %q", token), timeout, 300*time.Millisecond, func() (bool, error) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			if bytes.Contains(b, []byte(token)) {
				found = b
				return true, nil
			}
		}
		return false, fmt.Errorf("no dumped message contains %q yet (%d on disk)", token, len(entries))
	})
	return found, err
}

// verifyDKIM checks that raw (a message dumped by the sink) carries a DKIM
// signature for domain that validates against the public key published in the
// fake zone — i.e. exactly what a real receiver would check, using DNS the
// harness itself controls instead of net.DefaultResolver (plan C.4).
func verifyDKIM(raw []byte, domain string) error {
	verifications, err := dkim.VerifyWithOptions(bytes.NewReader(raw), &dkim.VerifyOptions{
		LookupTXT: lookupTXTFunc(),
	})
	if err != nil {
		return fmt.Errorf("dkim verify: %w", err)
	}
	for _, v := range verifications {
		if v.Domain == domain {
			if v.Err != nil {
				return fmt.Errorf("dkim signature for %s did not validate: %w", domain, v.Err)
			}
			return nil
		}
	}
	return fmt.Errorf("no DKIM signature found for domain %s (got %d signature(s))", domain, len(verifications))
}

// connectFromPattern matches Postfix's smtpd "connect from ...[ADDR]" log
// line, which is how the harness learns the source address Postfix itself
// observed for a just-made connection (needed to configure a level-2 rate
// limit's allowed-IPs list — the address a host-published port is seen as
// inside the container depends on Docker's NAT and isn't worth hard-coding).
var connectFromPattern = regexp.MustCompile(`connect from [^\[]*\[([0-9a-fA-F.:]+)\]`)

// lastSMTPClientIP reads mail.log inside the selfpost container and returns
// the most recent address Postfix's smtpd logged a connection from.
func lastSMTPClientIP(s *stack) (string, error) {
	out, err := s.execIn("selfpost", "tail", "-n", "200", "/data/log/mail.log")
	if err != nil {
		return "", err
	}
	matches := connectFromPattern.FindAllStringSubmatch(out, -1)
	if len(matches) == 0 {
		return "", fmt.Errorf("no \"connect from\" line in mail.log yet")
	}
	return matches[len(matches)-1][1], nil
}
