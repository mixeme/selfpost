package e2e

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"testing"
	"time"
)

// limitDomain is a sending domain of its own for the domain-limit case, so
// that its ceiling of one touches no other step.
const limitDomain = "limit.e2e.test"

// testDomainLimitNotReservedByAStranger is the "done when" of the security
// audit's M1 (2026-10-17): the level-2 domain limit is the login's domain's.
// MAIL FROM reaches the journal milter before Postfix holds it to a login, so
// a client with no login naming a domain there used to take that domain's
// slots. Here a domain has a limit of one message an hour; a stranger opens
// the submission port, names the domain in MAIL FROM three times without
// signing in, and the domain's own application still sends its one message -
// and is refused the second, so the limit is in force.
func testDomainLimitNotReservedByAStranger(t *testing.T, sc *scenario) {
	domainID, err := sc.panel.addDomain(limitDomain)
	if err != nil {
		t.Fatalf("add %s: %v", limitDomain, err)
	}
	login, password, err := sc.panel.addApplication(domainID, "limit-app", "wildcard", "")
	if err == nil {
		err = waitForSenderMap(login, password, "alerts@"+limitDomain)
	}
	if err != nil {
		t.Fatalf("add the application: %v", err)
	}
	resp, body, err := sc.panel.postForm("/outbound/domains/"+domainID+"/settings/ratelimit", url.Values{
		"mode": {"manual"}, "max_messages": {"1"}, "window_seconds": {"3600"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/outbound/domains/"+domainID {
		t.Fatalf("save the domain limit: status %d, landed on %s:\n%.800s", resp.StatusCode, resp.Request.URL.Path, body)
	}

	if err := strangerMailFrom("x@"+limitDomain, 3); err != nil {
		t.Fatalf("the stranger's MAIL FROM: %v", err)
	}

	send := func(token string) sendResult {
		return attemptSend(sendAttempt{
			authLogin: login, authPassword: password,
			from: "alerts@" + limitDomain, to: recipient, subject: "e2e " + uniqueToken(token), body: "ok",
		})
	}
	if res := send("m1-own"); !res.ok() {
		t.Fatalf("the domain's own message after a stranger's MAIL FROM: %v", res.firstErr())
	}
	if err := send("m1-second").refusedAt("MAIL", 451, "4.7.1 Service unavailable"); err != nil {
		t.Fatalf("the second message over the domain's limit of one: %v", err)
	}
}

// strangerMailFrom opens the implicit-TLS submission port, says EHLO and
// sends MAIL FROM:<from> n times with RSET between, never signing in. Each
// MAIL FROM is answered 250: the journal milter continues, and Postfix
// refuses such a client only at RCPT (smtpd_delay_reject).
func strangerMailFrom(from string, n int) error {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", smtpsAddr, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         selfpostHostname,
	})
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, selfpostHostname)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer c.Close()
	if err := c.Hello("stranger.e2e.test"); err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		if err := c.Mail(from); err != nil {
			return fmt.Errorf("MAIL FROM %d: %w", i+1, err)
		}
		if err := c.Reset(); err != nil {
			return err
		}
	}
	_ = c.Quit()
	return nil
}
