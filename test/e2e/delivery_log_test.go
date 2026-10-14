package e2e

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"testing"
	"time"
)

var sendLogDetailsRe = regexp.MustCompile(`href="(/outbound/log/\d+[^"]*)"`)

// firstDeliveryPage returns the address of the page of the newest row in the
// sender domain's send log.
func firstDeliveryPage(sc *scenario) (string, error) {
	rows, err := sc.panel.sendLogRows(senderDomain, "")
	if err != nil {
		return "", fmt.Errorf("read the send log: %w", err)
	}
	m := sendLogDetailsRe.FindStringSubmatch(rows)
	if m == nil {
		return "", fmt.Errorf("no Details link in the send log:\n%s", rows)
	}
	return html.UnescapeString(m[1]), nil
}

// testDeliveryLogSurvivesRotation checks that a message's page still lists
// what Postfix wrote about it after mail.log has been rotated: the lines are
// kept in the database by the log tailer, not searched for in the current
// file (architecture.md § Log tailer, Stored lines). The message is the first
// one delivered, whose page send_verify_dkim_and_status noted.
func testDeliveryLogSurvivesRotation(t *testing.T, sc *scenario) {
	page := sc.deliveryPage

	// What the page has to show: the line that opened the queue id and the
	// delivery line. The tailer has read both - the row is "sent" - but it
	// writes them one at a time, so the page is polled rather than read once.
	wanted := []string{"client=", "status=sent"}
	shows := func() (bool, error) {
		_, body, err := sc.panel.get(page)
		if err != nil {
			return false, err
		}
		for _, w := range wanted {
			if !strings.Contains(body, w) {
				return false, fmt.Errorf("%s does not show a %q line", page, w)
			}
		}
		return true, nil
	}
	if err := waitFor("the message's page to list its log lines", 15*time.Second, 500*time.Millisecond, shows); err != nil {
		t.Fatal(err)
	}

	// The rotation ends with `postfix reload`, which fails while Postfix is
	// still starting - and this step follows one that restarts the container.
	// (Postfix 3.7.11, conf/postfix-script: "reload" and "status" both test
	// "master -t" and exit 1 while master does not hold its pid lock.)
	if err := waitFor("Postfix to be running", 30*time.Second, 500*time.Millisecond, func() (bool, error) {
		if _, err := h.execIn("selfpost", "postfix", "status"); err != nil {
			return false, err
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.execIn("selfpost", "logrotate", "-f", "/etc/logrotate.d/mail"); err != nil {
		t.Fatalf("logrotate -f: %v", err)
	}
	// The current file must no longer hold the delivery line, or the page
	// finding it there would prove nothing. grep exits 1 on no match.
	if out, err := h.execIn("selfpost", "sh", "-c", "grep -c 'status=sent' /data/log/mail.log || true"); err != nil || strings.TrimSpace(out) != "0" {
		t.Fatalf("mail.log still holds delivery lines after the rotation (count=%q err=%v)", strings.TrimSpace(out), err)
	}

	if ok, err := shows(); !ok {
		t.Fatalf("after mail.log was rotated: %v", err)
	}
}
