package e2e

import (
	"fmt"
	"testing"
	"time"
)

// testLevel2RateLimit is plan C.4 negative check 5: a limit set through the
// panel (not the environment) rejects the message that exceeds it, and the
// rejection is visible in the send log — the panel -> DB -> milter path.
func testLevel2RateLimit(t *testing.T, sc *scenario) {
	ip, err := lastSMTPClientIP(h)
	if err != nil {
		t.Fatalf("determine observed SMTP client IP: %v", err)
	}
	sc.clientIP = ip

	login, password, err := sc.panel.addApplication(sc.domainID, "l2app", "wildcard", "")
	if err != nil {
		t.Fatalf("add application: %v", err)
	}
	if err := waitForSenderMap(login, password, "billing@"+senderDomain); err != nil {
		t.Fatal(err)
	}
	sc.l2AppLogin, sc.l2AppPassword = login, password

	appID, err := sc.panel.applicationID(sc.domainID, login)
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.panel.setApplicationLimit(sc.domainID, appID, 1, 3600); err != nil {
		t.Fatalf("save application rate limit: %v", err)
	}

	from := "billing@" + senderDomain

	first := attemptSend(sendAttempt{
		authLogin: login, authPassword: password,
		from: from, to: recipient, subject: "e2e " + uniqueToken("l2-first"), body: "ok",
	})
	if !first.ok() {
		t.Fatalf("first message under the level-2 limit was rejected: %v", first.firstErr())
	}

	second := attemptSend(sendAttempt{
		authLogin: login, authPassword: password,
		from: from, to: recipient, subject: "e2e " + uniqueToken("l2-second"), body: "should be rejected",
	})
	// The milter answers tempfail at MAIL FROM, which Postfix words as below.
	if err := second.refusedAt("MAIL", 451, "4.7.1 Service unavailable"); err != nil {
		t.Fatalf("the second message over the level-2 limit: %v", err)
	}

	// The limit holds for every message of a connection, not for its first
	// only. The milter library hands the milter a fresh session after any reply
	// that is not "continue" - the refusal above is one - and Postfix does not
	// introduce the client again, so a second MAIL FROM on the same connection
	// used to arrive with no client address and was let through (found by the
	// technical review of 2026-10-12; the same hole let an application past its
	// client-address list).
	errs := mailTwiceOnOneConnection(login, password, from)
	for i, err := range errs {
		if err := replyIs(err, 451, "4.7.1 Service unavailable"); err != nil {
			t.Fatalf("over the level-2 limit, MAIL FROM %d of one connection: %v", i+1, err)
		}
	}

	if err := waitFor("a rate-limited row for l2app in the send log", 15*time.Second, 500*time.Millisecond, func() (bool, error) {
		rows, err := sc.panel.sendLogRows(senderDomain, login)
		if err != nil {
			return false, err
		}
		if containsCell(rows, "rate-limited") {
			return true, nil
		}
		return false, fmt.Errorf("no rate-limited row for l2app yet")
	}); err != nil {
		t.Fatal(err)
	}
}

// testSenderLoginMismatch is plan C.4 negative check 2: an authenticated
// application cannot send as a sender it does not own (architecture.md § Mail
// path, reject_sender_login_mismatch) — the core anti-spoofing control.
func testSenderLoginMismatch(t *testing.T, sc *scenario) {
	res := attemptSend(sendAttempt{
		authLogin: sc.appLogin, authPassword: sc.appPassword,
		from: "someone@not-" + senderDomain, to: recipient,
		subject: "e2e " + uniqueToken("mismatch"), body: "should be rejected",
	})
	// Postfix evaluates smtpd_sender_restrictions with smtpd_delay_reject=yes
	// (the default): the mismatch is detected at MAIL FROM but the reject is
	// only sent back at RCPT TO.
	if err := res.refusedAt("RCPT", 553, "5.7.1"); err != nil {
		t.Fatalf("a sender the application does not own: %v", err)
	}
	if err := replyIs(res.rcptErr, 553, "Sender address rejected: not owned by user"); err != nil {
		t.Fatalf("a sender the application does not own: %v", err)
	}
}

// testNoAuthRejected is plan C.4 negative check 1: no SASL session, no mail.
// The sender address used here belongs to no registered domain, so the
// rejection is unambiguously about the missing AUTH and not sender ownership.
func testNoAuthRejected(t *testing.T, sc *scenario) {
	res := attemptSend(sendAttempt{
		from: "anyone@unregistered.e2e.test", to: recipient,
		subject: "e2e " + uniqueToken("noauth"), body: "should be rejected",
	})
	if err := res.refusedAt("RCPT", 554, "Client host rejected: Access denied"); err != nil {
		t.Fatalf("an unauthenticated send: %v", err)
	}
}

// testForeignRelayRejected is plan C.4 negative check 3: a direct proof that
// the submission port is not an open relay — even the exact recipient the
// positive path just delivered to is refused without AUTH. The refusal is
// smtpd_client_restrictions (permit_sasl_authenticated, reject), which on 465
// and 587 comes before reject_unauth_destination, so the reply is the same as
// check 1's; reject_unauth_destination's own "Relay access denied" is held on
// port 25 (testInboundRelay), where clients do not authenticate.
func testForeignRelayRejected(t *testing.T, sc *scenario) {
	res := attemptSend(sendAttempt{
		from: "anyone@unregistered.e2e.test", to: recipient,
		subject: "e2e " + uniqueToken("relay"), body: "should be rejected",
	})
	if err := res.refusedAt("RCPT", 554, "Client host rejected: Access denied"); err != nil {
		t.Fatalf("an unauthenticated relay to an external destination: %v", err)
	}
}

// checkContainerAlive confirms the container is still up after a component
// stop — the crashexit listener must not have brought it down.
func checkContainerAlive(s *stack) error {
	out, err := s.execIn("selfpost", "true")
	if err != nil {
		return fmt.Errorf("container not responding to exec: %v (%s)", err, out)
	}
	return nil
}

// testSessionSurvivesRestart is plan C.4 negative/regression check 8 (moved
// here from the manual B.1 stand check, plan item C.4's closing note): the
// login session (SQLite-backed, plan B.1) must survive `docker restart`.
func testSessionSurvivesRestart(t *testing.T, sc *scenario) {
	if err := h.restartSelfpost(); err != nil {
		t.Fatalf("restart selfpost: %v", err)
	}
	if err := waitFor("panel HTTP to answer after restart", 30*time.Second, 500*time.Millisecond, func() (bool, error) {
		_, err := sc.panel.status()
		return err == nil, err
	}); err != nil {
		t.Fatal(err)
	}
	// The panel and Postfix come up independently after a restart (see
	// waitForSMTPSReady) — the next subtest sends mail, so make sure smtpd is
	// actually listening before this one returns.
	if err := waitForSMTPSReady(30 * time.Second); err != nil {
		t.Fatal(err)
	}
	resp, err := sc.panel.status()
	if err != nil {
		t.Fatalf("GET /overview after restart: %v", err)
	}
	if resp.Request.URL.Path != "/overview" {
		t.Fatalf("session did not survive restart: landed on %s instead of /overview", resp.Request.URL.Path)
	}
}

// testLevel1RateLimit is plan C.4 negative check 4: the native Postfix anvil
// backstop (smtpd_client_message_rate_limit, guide § Rate limiting), set by
// the override to RATE_LIMIT_MESSAGES_PER_IP=50, rejects once exceeded. It
// uses a dedicated application with no level-2 limit of its own, and retries
// well past that count, so the result is unambiguous regardless of how much of
// the shared per-IP budget earlier subtests already spent (they stay well
// under 50 between them).
//
// Only anvil's own reply counts, and only after at least one message of the
// loop went through: the case once passed on "limit tripped on attempt 1/60"
// when the connection was reset because Postfix was still starting - and,
// held to anvil's reply, it turned out the first message had always been
// refused for another reason: the new login was not yet in the sender map an
// smtpd from before the panel's reload held (waitForSenderMap).
func testLevel1RateLimit(t *testing.T, sc *scenario) {
	login, password, err := sc.panel.addApplication(sc.domainID, "l1app", "wildcard", "")
	if err != nil {
		t.Fatalf("add application: %v", err)
	}
	send := func() sendResult {
		return attemptSend(sendAttempt{
			authLogin: login, authPassword: password,
			from: "l1@" + senderDomain, to: recipient,
			subject: "e2e " + uniqueToken("l1"), body: "rate limit probe",
		})
	}

	if err := waitForSenderMap(login, password, "l1@"+senderDomain); err != nil {
		t.Fatal(err)
	}
	if res := send(); !res.ok() {
		t.Fatalf("the first message of %s, before any limit could trip: %v", login, res.firstErr())
	}

	const maxAttempts = 60
	for i := 1; i < maxAttempts; i++ {
		res := send()
		if res.ok() {
			continue
		}
		if err := res.refusedAt("MAIL", 450, "4.7.1 Error: too much mail from"); err != nil {
			t.Fatalf("attempt %d/%d: %v", i+1, maxAttempts, err)
		}
		t.Logf("level-1 limit tripped on attempt %d/%d: %v", i+1, maxAttempts, res.firstErr())
		return
	}
	t.Fatalf("level-1 rate limit (RATE_LIMIT_MESSAGES_PER_IP=50) never tripped after %d sends", maxAttempts)
}
