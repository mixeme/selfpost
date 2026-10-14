package e2e

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-msgauth/dkim"
)

// passedFromLines reads the From: lines the sender check passes, the file its
// unit test reads too, with the application's domain put in.
func passedFromLines(repoRoot, domain string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "internal", "milter", "testdata", "from-lines-passed.txt"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		s, err := strconv.Unquote(line)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", line, err)
		}
		out = append(out, strings.ReplaceAll(s, "{domain}", domain))
	}
	return out, nil
}

// testSignedForItsDomainOnly: the sender check and OpenDKIM read From: with
// two parsers, and that they agree was argued, never run (audit 2026-10-12,
// stage 2). Every From: line the check passes for an application is sent as
// that application, and what arrives must be signed for its domain - with a
// signature that validates - and for no other: a line the check reads as the
// application's own and OpenDKIM as another sender's would be signed with
// that sender's key.
func testSignedForItsDomainOnly(t *testing.T, sc *scenario) {
	lines, err := passedFromLines(h.repoRoot, senderDomain)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		token := uniqueToken("dkimfrom")
		res := attemptSend(sendAttempt{
			authLogin: sc.appLogin, authPassword: sc.appPassword,
			from: "alerts@" + senderDomain, headerFrom: line, to: recipient,
			subject: "e2e " + token, body: "which key signs this",
		})
		if !res.ok() {
			t.Errorf("From: %q was refused: %v", line, res.firstErr())
			continue
		}
		raw, err := findSinkMessage(h.stageDir, token, 30*time.Second)
		if err != nil {
			t.Errorf("From: %q never reached the sink: %v", line, err)
			continue
		}
		verifications, err := dkim.VerifyWithOptions(bytes.NewReader(raw), &dkim.VerifyOptions{LookupTXT: lookupTXTFunc()})
		if err != nil {
			t.Errorf("From: %q: dkim verify: %v", line, err)
			continue
		}
		valid := false
		for _, v := range verifications {
			if v.Domain != senderDomain {
				t.Errorf("From: %q was signed for %s", line, v.Domain)
			}
			if v.Domain == senderDomain && v.Err == nil {
				valid = true
			}
		}
		if !valid {
			t.Errorf("From: %q has no valid signature for %s (%d signature(s))", line, senderDomain, len(verifications))
		}
	}
}
