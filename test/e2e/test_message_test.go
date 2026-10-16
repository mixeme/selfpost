package e2e

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// testTestMessage: the test message of Server › Health (test-email) goes to
// the sink from postmaster@ the stand's hostname. The browser lands on its
// row in the Outbound log, which learns its queue id from cleanup's
// Message-ID line and then its status from the delivery line, as any row;
// the sink has it, signed with the key of the stand's own name.
func testTestMessage(t *testing.T, sc *scenario) {
	before := sinkMessagesWith(t, h.stageDir, "Subject: SelfPost test message")
	resp, body, err := sc.panel.postForm("/server/health/test-message", url.Values{"recipient": {recipient}})
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`^/outbound/log/(\d+)$`).FindStringSubmatch(resp.Request.URL.Path)
	if resp.StatusCode != 200 || m == nil {
		t.Fatalf("send a test message: status %d, landed on %s:\n%.800s", resp.StatusCode, resp.Request.URL.Path, body)
	}
	page := resp.Request.URL.Path
	if err := waitFor("the test message to be sent", 60*time.Second, time.Second, func() (bool, error) {
		_, body, err := sc.panel.get(page)
		if err != nil {
			return false, err
		}
		qid := regexp.MustCompile(`<dt>Queue id</dt><dd class="sp-mono">([^<]+)</dd>`).FindStringSubmatch(body)
		if strings.Contains(body, `<span class="tag is-success is-light">sent</span>`) && qid != nil && longQueueID.MatchString(qid[1]) {
			return true, nil
		}
		return false, nil
	}); err != nil {
		_, body, _ := sc.panel.get(page)
		t.Fatalf("%v\n%.3000s\n%s", err, body, mailLogTail(h, "postfix/"))
	}
	if err := waitFor("the sink to have it", 30*time.Second, time.Second, func() (bool, error) {
		return sinkMessagesWith(t, h.stageDir, "Subject: SelfPost test message") > before, nil
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := findSinkMessage(h.stageDir, "Subject: SelfPost test message", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "d="+selfpostHostname) || !strings.Contains(string(raw), "From: SelfPost <postmaster@"+selfpostHostname+">") {
		t.Fatalf("the test message at the sink is not the server's own, signed with its name:\n%.2000s", raw)
	}
}
