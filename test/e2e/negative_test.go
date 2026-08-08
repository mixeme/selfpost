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
	sc.l2AppLogin, sc.l2AppPassword = login, password

	appID, err := sc.panel.applicationID(sc.domainID, login)
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.panel.setRateLimit(fmt.Sprintf("/applications/%s/ratelimit", appID), ip, 1, 3600); err != nil {
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
	if second.ok() {
		t.Fatal("second message exceeding the level-2 limit was accepted, want rejected")
	}
	if second.mailErr == nil {
		t.Fatalf("expected the level-2 rejection at MAIL FROM, got: dial=%v auth=%v rcpt=%v data=%v",
			second.dialErr, second.authErr, second.rcptErr, second.dataErr)
	}

	if err := waitFor("a rejected row for l2app in the send log", 15*time.Second, 500*time.Millisecond, func() (bool, error) {
		rows, err := sc.panel.sendLogRows(senderDomain, login)
		if err != nil {
			return false, err
		}
		if containsCell(rows, "rejected") {
			return true, nil
		}
		return false, fmt.Errorf("no rejected row for l2app yet")
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
	if res.ok() {
		t.Fatal("send with a sender the application does not own was accepted, want rejected")
	}
	// Postfix evaluates smtpd_sender_restrictions with smtpd_delay_reject=yes
	// (the default): the mismatch is detected at MAIL FROM but the reject is
	// only sent back at RCPT TO, so the error can land on either call here.
	if res.mailErr == nil && res.rcptErr == nil {
		t.Fatalf("expected the mismatch rejected at MAIL or RCPT, got: dial=%v auth=%v data=%v",
			res.dialErr, res.authErr, res.dataErr)
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
	if res.ok() {
		t.Fatal("unauthenticated send was accepted, want rejected")
	}
}

// testForeignRelayRejected is plan C.4 negative check 3: a direct proof that
// this is not an open relay — even the exact recipient the positive path just
// delivered to is refused without AUTH (reject_unauth_destination).
func testForeignRelayRejected(t *testing.T, sc *scenario) {
	res := attemptSend(sendAttempt{
		from: "anyone@unregistered.e2e.test", to: recipient,
		subject: "e2e " + uniqueToken("relay"), body: "should be rejected",
	})
	if res.ok() {
		t.Fatal("unauthenticated relay to an external destination was accepted, want reject_unauth_destination")
	}
	if res.rcptErr == nil && res.mailErr == nil {
		t.Fatalf("expected rejection at MAIL or RCPT, got: dial=%v auth=%v data=%v", res.dialErr, res.authErr, res.dataErr)
	}
}

// testJournalMilterFailOpen is plan C.4 negative check 6: the journal-milter
// is monitoring-only and fails open (architecture.md § Mail path) — stopping
// the panel process (which owns the milter socket) must not block mail, and
// must not crash the container (crashexit only fires on PROCESS_STATE_FATAL, a
// clean supervisor stop is STOPPED, see build/crashexit.py).
func testJournalMilterFailOpen(t *testing.T, sc *scenario) {
	if _, err := h.execIn("selfpost", "supervisorctl", "-c", "/etc/supervisor/supervisord.conf", "stop", "panel"); err != nil {
		t.Fatalf("stop panel: %v", err)
	}
	t.Cleanup(func() {
		_, _ = h.execIn("selfpost", "supervisorctl", "-c", "/etc/supervisor/supervisord.conf", "start", "panel")
	})

	res := attemptSend(sendAttempt{
		authLogin: sc.appLogin, authPassword: sc.appPassword,
		from: "alerts@" + senderDomain, to: recipient,
		subject: "e2e " + uniqueToken("failopen"), body: "must still be accepted",
	})
	if !res.ok() {
		t.Fatalf("send with the journal-milter down was rejected, want fail-open accept: dial=%v auth=%v mail=%v rcpt=%v data=%v",
			res.dialErr, res.authErr, res.mailErr, res.rcptErr, res.dataErr)
	}

	if err := checkContainerAlive(h); err != nil {
		t.Fatalf("container did not survive the panel stopping: %v", err)
	}

	// Restart the panel (also undone by t.Cleanup, redundantly and harmlessly,
	// in case a later step needs it sooner than cleanup order guarantees).
	if _, err := h.execIn("selfpost", "supervisorctl", "-c", "/etc/supervisor/supervisord.conf", "start", "panel"); err != nil {
		t.Fatalf("restart panel: %v", err)
	}
	if err := waitFor("panel HTTP to answer again", 15*time.Second, 300*time.Millisecond, func() (bool, error) {
		_, err := sc.panel.status()
		return err == nil, err
	}); err != nil {
		t.Fatal(err)
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
		t.Fatalf("GET /status after restart: %v", err)
	}
	if resp.Request.URL.Path != "/status" {
		t.Fatalf("session did not survive restart: landed on %s instead of /status", resp.Request.URL.Path)
	}
}

// testLevel1RateLimit is plan C.4 negative check 4: the native Postfix anvil
// backstop (smtpd_client_message_rate_limit, guide § Rate limiting), set by
// the override to RATE_LIMIT_MESSAGES_PER_IP=50, rejects once exceeded. It
// uses a dedicated application with no level-2 limit of its own, and retries
// well past that count, so the result is unambiguous regardless of how much of
// the shared per-IP budget earlier subtests already spent (they stay well
// under 50 between them).
func testLevel1RateLimit(t *testing.T, sc *scenario) {
	login, password, err := sc.panel.addApplication(sc.domainID, "l1app", "wildcard", "")
	if err != nil {
		t.Fatalf("add application: %v", err)
	}

	const maxAttempts = 60
	for i := 0; i < maxAttempts; i++ {
		res := attemptSend(sendAttempt{
			authLogin: login, authPassword: password,
			from: "l1@" + senderDomain, to: recipient,
			subject: "e2e " + uniqueToken("l1"), body: "rate limit probe",
		})
		if !res.ok() {
			t.Logf("level-1 limit tripped on attempt %d/%d: %v", i+1, maxAttempts, res.firstErr())
			return
		}
	}
	t.Fatalf("level-1 rate limit (RATE_LIMIT_MESSAGES_PER_IP=5) never tripped after %d sends", maxAttempts)
}
