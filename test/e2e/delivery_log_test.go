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

// testExpiredMessageIsClosed: a message Postfix gives up on is closed as
// bounced. Postfix logs expiry once per message, "<id>: from=<..>,
// status=expired, returned to sender" (here "force-expired"), and no line per
// recipient (Postfix 3.7.11, src/qmgr/qmgr_active.c,
// qmgr_active_done_25_generic(); assumptions A-8). The queue lifetime cannot
// be waited out, so the message is deferred by hand - defer_transports=smtp,
// under which qmgr hands the recipient to the retry service, "4.3.2 deferred
// transport" (src/qmgr/qmgr_message.c, qmgr_message_resolve()) - and then
// forced to expire: postsuper -e marks it, postqueue -i has qmgr look at it
// at once (man/man1/postsuper.1, postqueue.1).
func testExpiredMessageIsClosed(t *testing.T, sc *scenario) {
	exec := func(args ...string) {
		t.Helper()
		if out, err := h.execIn("selfpost", args...); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	exec("postconf", "-e", "defer_transports=smtp")
	exec("postfix", "reload")
	defer func() {
		// The bounce to the sender waits behind the same setting.
		h.execIn("selfpost", "postconf", "-e", "defer_transports=")
		h.execIn("selfpost", "postfix", "reload")
		h.execIn("selfpost", "postqueue", "-f")
	}()

	token := uniqueToken("expire")
	if res := attemptSend(sendAttempt{
		authLogin: sc.appLogin, authPassword: sc.appPassword,
		from: "alerts@" + senderDomain, headerFrom: "alerts@" + senderDomain, to: recipient,
		subject: "e2e " + token, body: "deferred, then expired",
	}); !res.ok() {
		t.Fatalf("send: %v", res.firstErr())
	}
	row := func() (string, error) {
		return asPanel(`import sqlite3,sys
r=sqlite3.connect("/data/selfpost.db").execute("select queue_id, status from send_log where subject like ? order by id desc",("%"+sys.argv[1]+"%",)).fetchone()
print("" if r is None else r[0]+" "+r[1])`, token)
	}
	var queueID string
	if err := waitFor("the row to be deferred", 30*time.Second, 500*time.Millisecond, func() (bool, error) {
		out, err := row()
		if err != nil {
			return false, err
		}
		if f := strings.Fields(out); len(f) == 2 && f[1] == "deferred" {
			queueID = f[0]
			return true, nil
		}
		return false, fmt.Errorf("row %q", out)
	}); err != nil {
		t.Fatal(err)
	}

	exec("postsuper", "-e", queueID)
	exec("postqueue", "-i", queueID)
	if err := waitFor("the expired message's row to be bounced", 30*time.Second, 500*time.Millisecond, func() (bool, error) {
		out, err := row()
		if err != nil {
			return false, err
		}
		return out == queueID+" bounced", fmt.Errorf("row %q", out)
	}); err != nil {
		out, _ := h.execIn("selfpost", "grep", queueID, "/data/log/mail.log")
		t.Fatalf("%v\nmail.log:\n%s", err, out)
	}
	out, err := h.execIn("selfpost", "grep", queueID+": from=<alerts@"+senderDomain+">, status=force-expired, returned to sender", "/data/log/mail.log")
	if err != nil {
		t.Fatalf("no expiry line in mail.log: %v\n%s", err, out)
	}
}
