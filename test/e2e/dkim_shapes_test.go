package e2e

import (
	"strings"
	"testing"
	"time"
)

// testSignatureOverAwkwardHeaders: the panel's signature verifies, by dkimpy -
// an implementation that is not the signer's library - over the header shapes
// Postfix hands a milter in ways of its own: a folded value (lines joined by
// "\n"), a value led by a tab, one with no space after the colon, a header
// twice, and an 8-bit body (Postfix 3.10.13, src/milter/milter8.c:2329-2400,
// milter8_header(); build/postfix-config.sh, disable_mime_output_conversion).
// The unit tests verify with go-msgauth, the signer's own library, over a
// message they rebuild themselves; this is what Postfix delivers (review of
// panel-dkim-signing, F5).
func testSignatureOverAwkwardHeaders(t *testing.T, sc *scenario) {
	token := uniqueToken("shapes")
	raw := strings.Join([]string{
		"From: =?utf-8?b?0J7RgtC00LXQuyDRgdCx0YvRgtCw?= <alerts@" + senderDomain + ">",
		"To: first@sink.e2e.test",
		"To: second@sink.e2e.test",
		"Cc:third@sink.e2e.test",
		"Subject: e2e " + token + " with a subject long enough",
		"\tto be folded onto a second line",
		"X-Tab:\tled by a tab",
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: 8bit",
		"List-Unsubscribe: <https://" + senderDomain + "/u/1>",
		"List-Unsubscribe-Post: List-Unsubscribe=One-Click",
		"",
		"Привет - an 8-bit body, sent as it is.",
		"A line with trailing white space   ",
		"",
		"",
	}, "\r\n")
	res := attemptSend(sendAttempt{
		authLogin: sc.appLogin, authPassword: sc.appPassword,
		from: "alerts@" + senderDomain, to: recipient, raw: raw,
	})
	if !res.ok() {
		t.Fatalf("send: %v", res.firstErr())
	}
	file, delivered, err := findSinkFile(h.stageDir, token, 30*time.Second)
	if err != nil {
		t.Fatalf("the message never reached the sink: %v", err)
	}
	if err := checkPanelSignature(h, file, delivered, senderDomain); err != nil {
		t.Fatalf("%v\n%s", err, delivered)
	}
	for _, want := range []string{"Привет", "List-Unsubscribe-Post"} {
		if !strings.Contains(string(delivered), want) {
			t.Errorf("the delivered message lacks %q:\n%s", want, delivered)
		}
	}
	// Both To: lines, the one Cc:, the Subject and List-Unsubscribe-Post are
	// signed, the shown ones once more.
	signedNames := strings.ToLower(dkimTagsOf(t, delivered)["h"])
	for _, want := range []string{"to:to:", "cc:", "subject:", "list-unsubscribe-post:"} {
		if !strings.Contains(signedNames, want) {
			t.Errorf("h=%s lacks %q", signedNames, want)
		}
	}
}

// dkimTagsOf is the tags of the message's one DKIM-Signature.
func dkimTagsOf(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	head, _, _ := strings.Cut(text, "\n\n")
	var value string
	in := false
	for _, line := range strings.Split(head, "\n") {
		switch {
		case strings.HasPrefix(strings.ToLower(line), "dkim-signature:"):
			in, value = true, line[len("dkim-signature:"):]
		case in && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")):
			value += line
		default:
			in = false
		}
	}
	if value == "" {
		t.Fatalf("no DKIM-Signature in:\n%s", raw)
	}
	return dkimTags(value)
}
