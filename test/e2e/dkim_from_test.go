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

// fromLines reads one of the files of From: lines the sender check's unit
// test reads too, with the placeholders put in: {domain} the application's,
// {other} another sender of the server, each also in capitals, and
// {other_qp} with its dots as quoted pairs.
func fromLines(repoRoot, file, domain, other string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "internal", "milter", "testdata", file))
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
		out = append(out, strings.NewReplacer(
			"{domain}", domain, "{DOMAIN}", strings.ToUpper(domain),
			"{other_qp}", strings.ReplaceAll(other, ".", `\.`),
			"{other}", other, "{OTHER}", strings.ToUpper(other),
		).Replace(s))
	}
	return out, nil
}

// testSignedForItsDomainOnly: every From: line the check passes for an
// application is sent as that application, and what arrives must be signed
// for its domain - with a signature that validates over these headers - and
// for no other (audit 2026-10-12, stage 2). Until 2.8.0 OpenDKIM chose the
// key by the line and this was the check that the two read it alike; the
// panel's signer takes the login's key, and the run holds it to that.
func testSignedForItsDomainOnly(t *testing.T, sc *scenario) {
	lines, err := fromLines(h.repoRoot, "from-lines-passed.txt", senderDomain, otherDomain)
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

// testLoginCaseMatters: SASL compares the login byte for byte, so an
// application's login in other capitals does not authenticate. Every lookup on
// the mail path is by the exact login and relies on that (review of audit
// stage 2, F13).
func testLoginCaseMatters(t *testing.T, sc *scenario) {
	res := attemptSend(sendAttempt{
		authLogin: strings.ToUpper(sc.appLogin), authPassword: sc.appPassword,
		from: "alerts@" + senderDomain, to: recipient, subject: "e2e " + uniqueToken("case"), body: "x",
	})
	if err := res.refusedAt("AUTH", 535, ""); err != nil {
		t.Fatalf("AUTH as %s: %v", strings.ToUpper(sc.appLogin), err)
	}
}
