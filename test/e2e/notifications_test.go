package e2e

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The server's own name in the run, and where the administrator is written to:
// a mailbox behind the sink-MX, so a notification can be read back.
const (
	serverName = "mail.e2e.test"
	adminEmail = "admin@sink.e2e.test"
)

// serverMailRecord scrapes the DKIM record of the server's own name off
// Server › Settings, where the panel tells the administrator to publish it.
func (c *panelClient) serverMailRecord() (name, value string, err error) {
	_, body, err := c.get("/server/settings")
	if err != nil {
		return "", "", err
	}
	name, ok := extractCodeRow(body, "Host / name")
	if !ok {
		return "", "", fmt.Errorf("could not find the server's DKIM record name on Settings")
	}
	value, ok = extractCodeRow(body, "Value to publish")
	if !ok {
		return "", "", fmt.Errorf("could not find the server's DKIM record value on Settings")
	}
	return name, value, nil
}

// setAccountEmail saves the signed-in user's profile with an e-mail and
// notifications on.
func (c *panelClient) setAccountEmail(username, email string) error {
	resp, body, err := c.postForm("/account/profile", url.Values{
		"username": {username},
		"email":    {email},
		"notify":   {"on"},
	})
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("save profile: status %d: %s", resp.StatusCode, body)
	}
	_, page, err := c.get("/account")
	if err != nil {
		return err
	}
	if !strings.Contains(page, email) {
		return fmt.Errorf("the Account page does not show %s after saving it", email)
	}
	return nil
}

// testNotificationsSetUp gives the administrator a mailbox and publishes the
// DKIM record of the server's own name, as an operator would after reading
// Server › Settings. It runs before the step that makes a rate limit refuse
// mail, so that the refusal finds someone to tell.
func testNotificationsSetUp(t *testing.T, sc *scenario) {
	if err := sc.panel.setAccountEmail(adminUser, adminEmail); err != nil {
		t.Fatalf("set the administrator's e-mail: %v", err)
	}

	name, value, err := sc.panel.serverMailRecord()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, "._domainkey."+serverName) {
		t.Fatalf("the record on Settings is %q, want a DKIM key of %s", name, serverName)
	}
	rel, ok := trimZoneSuffix(name)
	if !ok {
		t.Fatalf("record name %q is not under e2e.test", name)
	}
	records, err := publishTXT(h.stageDir, sc.zoneRecords, txtRecord{name: rel, value: value})
	if err != nil {
		t.Fatalf("publish the server's dkim record to the fake zone: %v", err)
	}
	sc.zoneRecords = records
}

// testRateLimitRefusalIsNotified checks the whole path of a notification: the
// level-2 refusal of the previous step is noticed by the notifier (it looks
// every minute), written to the administrator, submitted by the unprivileged
// panel through sendmail, signed by OpenDKIM with the key of the server's own
// name and delivered. It also holds what a notification is not: a row of the
// Outbound log. That it is said once is for the unit tests to hold
// (internal/notify): here it would be a minute of waiting for nothing to happen.
func testRateLimitRefusalIsNotified(t *testing.T, sc *scenario) {
	subject := "[SelfPost] " + senderDomain + ": a rate limit is refusing mail"
	raw, err := findSinkMessage(h.stageDir, subject, 100*time.Second)
	if err != nil {
		t.Fatalf("no notification reached %s: %v\npanel log:\n%s", adminEmail, err, tailLines(h.logs("selfpost"), "notify", 20))
	}
	msg := string(raw)
	for _, want := range []string{
		"From: SelfPost <postmaster@" + serverName + ">",
		"To: <" + adminEmail + ">",
		"Auto-Submitted: auto-generated",
		"refused",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the notification lacks %q:\n%s", want, msg)
		}
	}
	if err := verifyDKIM(raw, serverName); err != nil {
		t.Errorf("the notification is not signed as %s with the key Settings shows: %v", serverName, err)
	}

	// The panel's own mail is not an application's: no row in the log.
	rows, err := sc.panel.sendLogRows(senderDomain, "")
	if err != nil {
		t.Fatalf("read the send log: %v", err)
	}
	if strings.Contains(rows, "rate limit is refusing") || strings.Contains(rows, "postmaster@"+serverName) {
		t.Errorf("a notification is listed in the Outbound log:\n%s", rows)
	}
}

// tailLines returns the last n lines of log that contain filter.
func tailLines(log, filter string, n int) string {
	var keep []string
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, filter) {
			keep = append(keep, line)
		}
	}
	if len(keep) > n {
		keep = keep[len(keep)-n:]
	}
	return strings.Join(keep, "\n")
}
