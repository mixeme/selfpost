package e2e

import (
	"strings"
	"testing"
	"time"
)

// testSenderLine: a Sender: line is held to the rule the From: line is held
// to (the owner's decision, 2026-10-16): the application's own address passes
// and is signed; another domain's is refused for good at the end of DATA,
// "550 5.7.1 Sender: ...", and is a row of the Outbound log tagged "wrong
// Sender" (internal/milter/sender.go, judge()).
func testSenderLine(t *testing.T, sc *scenario) {
	message := func(token, sender string) string {
		return strings.Join([]string{
			"From: Alerts <alerts@" + senderDomain + ">",
			"Sender: " + sender,
			"To: " + recipient,
			"Subject: e2e " + token,
			"Date: " + time.Now().UTC().Format(time.RFC1123Z),
			"",
			"sender line check",
			"",
		}, "\r\n")
	}

	own := uniqueToken("senderown")
	res := attemptSend(sendAttempt{
		authLogin: sc.appLogin, authPassword: sc.appPassword,
		from: "alerts@" + senderDomain, to: recipient, raw: message(own, "mailer@"+senderDomain),
	})
	if !res.ok() {
		t.Fatalf("its own address in Sender: was refused: %v", res.firstErr())
	}
	file, delivered, err := findSinkFile(h.stageDir, own, 30*time.Second)
	if err != nil {
		t.Fatalf("never reached the sink: %v", err)
	}
	if err := checkPanelSignature(h, file, delivered, senderDomain); err != nil {
		t.Fatal(err)
	}
	if signedNames := strings.ToLower(dkimTagsOf(t, delivered)["h"]); !strings.Contains(signedNames, "sender:") {
		t.Errorf("h=%s does not sign Sender", signedNames)
	}

	foreign := uniqueToken("senderforeign")
	res = attemptSend(sendAttempt{
		authLogin: sc.appLogin, authPassword: sc.appPassword,
		from: "alerts@" + senderDomain, to: recipient, raw: message(foreign, "ceo@"+otherDomain),
	})
	if err := res.refusedAt("DATA", 550, "5.7.1 Sender:"); err != nil {
		t.Fatalf("another domain in Sender: %v", err)
	}
	if _, err := findSinkMessage(h.stageDir, foreign, 2*time.Second); err == nil {
		t.Error("the refused message reached the sink")
	}
	if err := waitFor("the Sender: refusal in the Outbound log", 15*time.Second, 500*time.Millisecond, func() (bool, error) {
		rows, err := sc.panel.sendLogRows(senderDomain, "")
		if err != nil {
			return false, err
		}
		return strings.Contains(rows, `">wrong Sender</span>`), nil
	}); err != nil {
		t.Error(err)
	}
}
