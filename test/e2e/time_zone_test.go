package e2e

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// testTimeZone (local-time): the container started with TZ=Europe/Moscow is
// in that zone, all of it. Settings names the zone; a message's page gives
// its times in Moscow's; and Postfix, which takes TZ through
// import_environment, stamps mail.log in it too, so the lines on that page
// read the same clock. Moscow has kept UTC+3 all year since 2014, so a fixed
// zone stands for it here whatever zone data the machine running the tests
// has.
func testTimeZone(t *testing.T, sc *scenario) {
	h.env = append(h.env, "TZ=Europe/Moscow")
	if err := h.recreateSelfpost(); err != nil {
		t.Fatalf("recreate selfpost with TZ: %v", err)
	}
	if err := waitForPanelReady(); err != nil {
		t.Fatalf("panel after setting TZ: %v\n%s", err, h.logs("selfpost"))
	}
	if err := waitForSMTPSReady(60 * time.Second); err != nil {
		t.Fatal(err)
	}
	if out, err := h.execIn("selfpost", "date", "+%Z"); err != nil || strings.TrimSpace(out) != "MSK" {
		t.Fatalf("date +%%Z in the container = %q, %v; want MSK", out, err)
	}
	_, body, err := sc.panel.get("/server/settings")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<h2>Time zone</h2>`, `<dd class="sp-mono">Europe/Moscow</dd>`, `UTC&#43;03:00. Set in`} {
		if !strings.Contains(body, want) {
			t.Fatalf("Settings is missing %q:\n%.4000s", want, body)
		}
	}

	msk := time.FixedZone("MSK", 3*3600)
	sent := time.Now().In(msk)
	resp, _, err := sc.panel.postForm("/server/health/test-message", url.Values{"recipient": {recipient}})
	if err != nil {
		t.Fatal(err)
	}
	page := resp.Request.URL.Path
	if !regexp.MustCompile(`^/outbound/log/\d+$`).MatchString(page) {
		t.Fatalf("send a test message: landed on %s", page)
	}
	// The minute it was sent, or the next one if the clock turned between.
	minutes := []string{sent.Format("2006-01-02 15:04"), sent.Add(time.Minute).Format("2006-01-02 15:04")}
	stamps := []string{sent.Format("Jan _2 15:04"), sent.Add(time.Minute).Format("Jan _2 15:04")}
	if err := waitFor("the test message's page to show it sent", 60*time.Second, time.Second, func() (bool, error) {
		_, b, err := sc.panel.get(page)
		body = b
		return err == nil && strings.Contains(b, `<span class="tag is-success is-light">sent</span>`), err
	}); err != nil {
		t.Fatalf("%v\n%.3000s", err, body)
	}
	if !strings.Contains(body, "<time>"+minutes[0]) && !strings.Contains(body, "<time>"+minutes[1]) {
		t.Errorf("the message's page has no time in Moscow's %s:\n%.4000s", minutes[0], body)
	}
	if !strings.Contains(body, stamps[0]) && !strings.Contains(body, stamps[1]) {
		t.Errorf("the message's mail.log lines are not stamped in Moscow's %s:\n%.4000s\n%s", stamps[0], body, mailLogTail(h, "postfix/"))
	}
	if strings.Contains(body, " UTC<") {
		t.Errorf("a time on the message's page still says UTC:\n%.4000s", body)
	}
}
