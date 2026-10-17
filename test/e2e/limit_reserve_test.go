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
// a client with no login naming a domain there used to take one of that
// domain's slots and hold it for as long as its transaction stayed open.
// Here a domain has a limit of one message an hour; a stranger opens the
// submission port, names the domain in MAIL FROM without signing in and
// keeps the transaction open, and meanwhile the domain's own application
// sends its one message - and is refused the second, so the limit is in
// force. (A RSET between the stranger's MAIL FROMs would end the
// transaction, which releases the slot under the old code as well: the
// review of stage 1 found the first version of this step passing on it.)
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

	stranger, err := strangerHoldsMailFrom("x@" + limitDomain)
	if err != nil {
		t.Fatalf("the stranger's MAIL FROM: %v", err)
	}
	defer stranger()

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

// strangerHoldsMailFrom opens the implicit-TLS submission port, says EHLO
// and MAIL FROM:<from> without signing in, and leaves the transaction open
// until the returned func is called. The MAIL FROM is answered 250: the
// journal milter continues, and Postfix refuses such a client only at RCPT
// (smtpd_delay_reject).
func strangerHoldsMailFrom(from string) (func(), error) {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", smtpsAddr, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         selfpostHostname,
	})
	if err != nil {
		return nil, err
	}
	c, err := smtp.NewClient(conn, selfpostHostname)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := c.Hello("stranger.e2e.test"); err != nil {
		_ = c.Close()
		return nil, err
	}
	if err := c.Mail(from); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("MAIL FROM: %w", err)
	}
	return func() { _ = c.Quit(); _ = c.Close() }, nil
}
