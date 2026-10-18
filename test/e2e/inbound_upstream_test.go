package e2e

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// longQueueID is the shape of a long queue id: six or more characters of the
// time in seconds and four of the microseconds, base 52 without vowels, "z",
// the inode in base 51, which has no "z" (Postfix 3.10.13,
// src/global/mail_queue.h, MQID_LG_*).
var longQueueID = regexp.MustCompile(`^[0-9B-DF-HJ-NP-TV-Zb-df-hj-np-tv-z]{10,}z[0-9B-DF-HJ-NP-TV-Zb-df-hj-np-tv-y]+$`)

// testInboundUpstreamDown: an inbound domain whose upstream refuses
// connections (the sink's address, a port nothing listens on). Port 25 takes
// the message; Postfix cannot hand it on and logs "relay=none ...
// status=deferred (connect to ...: Connection refused)" (Postfix 3.10.13,
// src/smtp/smtp_trouble.c:273, 421), which the Inbound log shows as deferred
// with that reason and no host. Then the message is made to expire - postsuper
// -e, postqueue -i, as testExpiredMessageIsClosed does - and its row is
// bounced, its last reply kept. The queue is emptied after: the bounce to the
// stranger has nowhere to go on the stand. Runs inside testInboundRelay.
func testInboundUpstreamDown(t *testing.T, sc *scenario, message func(to, token string) string) {
	const domain = "down.e2e.test"
	to := "user@" + domain
	resp, body, err := sc.panel.postForm("/inbound/domains", url.Values{"name": {domain}})
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`^/inbound/domains/(\d+)$`).FindStringSubmatch(resp.Request.URL.Path)
	if m == nil {
		t.Fatalf("add inbound domain: landed on %s:\n%.800s", resp.Request.URL.Path, body)
	}
	base := "/inbound/domains/" + m[1]
	for path, form := range map[string]url.Values{
		base + "/upstream":   {"host": {"10.77.0.11"}, "port": {"2526"}, "tls_mode": {"none"}},
		base + "/recipients": {"recipient_mode": {"list"}, "addresses": {to}},
	} {
		resp, body, err := sc.panel.postForm(path, form)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 || resp.Request.URL.Path != base {
			t.Fatalf("POST %s: status %d, landed on %s:\n%.800s", path, resp.StatusCode, resp.Request.URL.Path, body)
		}
	}
	defer h.execIn("selfpost", "postsuper", "-d", "ALL")

	token := uniqueToken("inbound-down")
	if res := deliverPort25("someone@stranger.e2e.test", to, message(to, token)); !res.ok() {
		t.Fatalf("port 25 did not accept mail for %s: %v", to, res.firstErr())
	}
	row := func() (string, error) {
		return asPanel(`import sqlite3,sys
r=sqlite3.connect("/data/selfpost.db").execute("select queue_id, upstream_status, upstream_relay, upstream_reply from inbound_spam_log where subject like ? order by id desc",("%"+sys.argv[1]+"%",)).fetchone()
print("" if r is None else "|".join(r))`, token)
	}
	var queueID string
	if err := waitFor("the row to be deferred", 60*time.Second, time.Second, func() (bool, error) {
		out, err := row()
		if err != nil {
			return false, err
		}
		f := strings.Split(out, "|")
		if len(f) == 4 && f[1] == "deferred" {
			if f[2] != "" || !strings.HasPrefix(f[3], "connect to 10.77.0.11[10.77.0.11]:2526: Connection refused") {
				return false, fmt.Errorf("deferred with host %q, reply %q", f[2], f[3])
			}
			queueID = f[0]
			// A long queue id (enable_long_queue_ids=yes,
			// build/postfix-config.sh): the time, "z", the inode.
			if !longQueueID.MatchString(queueID) {
				return false, fmt.Errorf("queue id %q is not a long one", queueID)
			}
			return true, nil
		}
		return false, fmt.Errorf("row %q\n%s", out, mailLogTail(h, "postfix/"))
	}); err != nil {
		t.Fatal(err)
	}

	_, body, err = sc.panel.get("/inbound/log")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `<span class="tag is-warning is-light">deferred</span><h2>Upstream</h2>`) ||
		!strings.Contains(body, `<tr><th>Reply</th><td class="sp-mono sp-small">connect to 10.77.0.11[10.77.0.11]:2526: Connection refused</td></tr>`) ||
		!strings.Contains(body, `Postfix tries again; the message is in the queue`) {
		t.Fatalf("the Inbound log does not show the deferral:\n%.4000s", body)
	}

	// The message waits on Inbound › Queue with the upstream's reason, and not
	// on Outbound › Queue: one Postfix queue, split by the recipient's domain.
	_, body, err = sc.panel.get("/inbound/queue")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `<td class="sp-mono">`+queueID+`</td>`) ||
		!strings.Contains(body, `<td class="sp-muted">connect to 10.77.0.11[10.77.0.11]:2526: Connection refused</td>`) {
		t.Fatalf("Inbound › Queue does not list the deferred message %s:\n%.4000s", queueID, body)
	}
	if _, body, err = sc.panel.get("/outbound/queue"); err != nil {
		t.Fatal(err)
	} else if strings.Contains(body, queueID) {
		t.Fatalf("Outbound › Queue lists the inbound message %s", queueID)
	}

	for _, args := range [][]string{{"postsuper", "-e", queueID}, {"postqueue", "-i", queueID}} {
		if out, err := h.execIn("selfpost", args...); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	if err := waitFor("the expired row to be bounced", 30*time.Second, 500*time.Millisecond, func() (bool, error) {
		out, err := row()
		if err != nil {
			return false, err
		}
		f := strings.Split(out, "|")
		if len(f) == 4 && f[1] == "bounced" &&
			strings.HasPrefix(f[3], "expired in the queue; last reply: connect to 10.77.0.11[10.77.0.11]:2526") {
			return true, nil
		}
		return false, fmt.Errorf("row %q\n%s", out, mailLogTail(h, "postfix/"))
	}); err != nil {
		t.Fatal(err)
	}
}
