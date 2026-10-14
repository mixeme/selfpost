package e2e

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// otherDomain is a second sending domain of the stand: registered, with a DKIM
// key of its own, and no application of the first domain has any right to it.
const otherDomain = "other.e2e.test"

// testSenderCheck is from-header-binding's "done when": an application is
// refused for another tenant's From:, for the hostname's and for an address
// off its list, and accepted for its own. The envelope sender is the
// application's own every time - Postfix would refuse anything else before
// the header was looked at.
func testSenderCheck(t *testing.T, sc *scenario) {
	if _, err := sc.panel.addDomain(otherDomain); err != nil {
		t.Fatalf("add the second domain: %v", err)
	}
	listLogin, listPassword, err := sc.panel.addApplication(sc.domainID, "app-list", "list", "billing@"+senderDomain)
	if err == nil {
		err = waitForSenderMap(listLogin, listPassword, "billing@"+senderDomain)
	}
	if err != nil {
		t.Fatalf("add the application with a list: %v", err)
	}

	type who struct{ login, password, envelope string }
	wildcard := who{sc.appLogin, sc.appPassword, "alerts@" + senderDomain}
	list := who{listLogin, listPassword, "billing@" + senderDomain}

	for _, c := range []struct {
		name   string
		as     who
		header string
		pass   bool
	}{
		{"own address", wildcard, "alerts@" + senderDomain, true},
		{"another address of the domain, with a name", wildcard, `"The Shop" <orders@` + senderDomain + `>`, true},
		{"the listed address", list, "billing@" + senderDomain, true},
		{"the listed address with an extension, in envelope and header", who{listLogin, listPassword, "billing+march@" + senderDomain}, "billing+march@" + senderDomain, true},
		// What a mail library does with a long display name: the line is
		// folded, and Postfix hands the milter both of its lines.
		{"a long name, folded onto a second line", wildcard, "\"The Shop, its orders and its returns desk\"\r\n <orders@" + senderDomain + ">", true},

		{"another registered domain", wildcard, "director@" + otherDomain, false},
		{"the server's own name", wildcard, "postmaster@" + selfpostHostname, false},
		{"a domain the server knows nothing of", wildcard, "someone@elsewhere.e2e.test", false},
		{"an address off the list", list, "director@" + senderDomain, false},
		{"another sender in the display name", wildcard, `"director@` + otherDomain + `" <alerts@` + senderDomain + `>`, false},
	} {
		token := uniqueToken("from")
		res := attemptSend(sendAttempt{
			authLogin: c.as.login, authPassword: c.as.password,
			from: c.as.envelope, headerFrom: c.header, to: recipient,
			subject: "e2e " + token, body: "sender check: " + c.name,
		})
		if c.pass {
			if !res.ok() {
				t.Errorf("%s: From: %s was refused: %v", c.name, c.header, res.firstErr())
				continue
			}
			if _, err := findSinkMessage(h.stageDir, token, 30*time.Second); err != nil {
				t.Errorf("%s: accepted, and never reached the sink: %v", c.name, err)
			}
			continue
		}
		if res.ok() {
			t.Errorf("%s: From: %s was accepted", c.name, c.header)
			continue
		}
		// The verdict comes at the end of the headers, which is the end of
		// DATA for the client; it is permanent and says what is wrong.
		if e := fmt.Sprint(res.dataErr); res.dataErr == nil || !strings.HasPrefix(e, "550 ") || !strings.Contains(e, "5.7.1 From:") {
			t.Errorf("%s: want 550 5.7.1 naming the From: line at the end of DATA, got %v", c.name, res.firstErr())
		} else if reason := e[strings.LastIndex(e, " - ")+3:]; strings.Contains(reason, otherDomain) {
			// What the client wrote is said back; that the other domain is
			// a sender of this server is not (audit 2026-10-12, L8).
			t.Errorf("%s: the reason %q names %s", c.name, reason, otherDomain)
		}
		if _, err := findSinkMessage(h.stageDir, token, 2*time.Second); err == nil {
			t.Errorf("%s: the refused message reached the sink", c.name)
		}
	}

	// A refusal is a row of the Outbound log whose tag says why.
	if err := waitFor("the refusals to appear in the Outbound log", 15*time.Second, 500*time.Millisecond, func() (bool, error) {
		rows, err := sc.panel.sendLogRows(senderDomain, "")
		if err != nil {
			return false, err
		}
		if n := strings.Count(rows, `">wrong From</span>`); n < 5 {
			return false, fmt.Errorf("%d rows tagged wrong From, want 5", n)
		}
		return true, nil
	}); err != nil {
		t.Error(err)
	}
}

// testPanelDownDefersMail: the sender check is the panel's and Postfix
// attaches it strictly, so with the panel stopped mail is deferred - a
// temporary error, nothing accepted unchecked - and the container stays up
// (crashexit fires on PROCESS_STATE_FATAL only; a supervisor stop is STOPPED).
// Once the panel is back it has counted both what that cost: the time it did
// not answer under a running Postfix, and the message that was deferred.
//
// Until 2.4.0 this step proved the opposite, that mail passes with the panel
// down: the journal milter still fails open, but it shares the process with
// the check, and the check does not.
func testPanelDownDefersMail(t *testing.T, sc *scenario) {
	supervisorctl := func(verb string) error {
		_, err := h.execIn("selfpost", "supervisorctl", "-c", "/etc/supervisor/supervisord.conf", verb, "panel")
		return err
	}
	if err := supervisorctl("stop"); err != nil {
		t.Fatalf("stop panel: %v", err)
	}
	t.Cleanup(func() { _ = supervisorctl("start") })

	res := attemptSend(sendAttempt{
		authLogin: sc.appLogin, authPassword: sc.appPassword,
		from: "alerts@" + senderDomain, to: recipient,
		subject: "e2e " + uniqueToken("paneldown"), body: "must be deferred",
	})
	if res.ok() {
		t.Fatal("mail was accepted with the panel down: the sender check did not hold it")
	}
	// Temporary, and before any message is offered: Postfix answers 451 from
	// the connection on, and "454 4.3.0 Try again later" at AUTH, which is
	// where a client that authenticates gives up.
	var reply *textproto.Error
	if err := res.firstErr(); !errors.As(err, &reply) || reply.Code/100 != 4 {
		t.Fatalf("want a temporary 4xx reply with the panel down, got %v", err)
	}
	if err := checkContainerAlive(h); err != nil {
		t.Fatalf("container did not survive the panel stopping: %v", err)
	}
	// What Postfix wrote about it is what the panel counts from; shown so that
	// a change of Postfix's wording is seen here and not guessed at.
	if out, err := h.execIn("selfpost", "sh", "-c", "grep -i 'milter' /data/log/mail.log | tail -n 6"); err == nil {
		t.Logf("mail.log on the deferred message:\n%s", out)
	}
	time.Sleep(2 * time.Second) // downtime worth a whole second or two

	if err := supervisorctl("start"); err != nil {
		t.Fatalf("restart panel: %v", err)
	}
	if err := waitFor("panel HTTP to answer again", 15*time.Second, 300*time.Millisecond, func() (bool, error) {
		_, err := sc.panel.status()
		return err == nil, err
	}); err != nil {
		t.Fatal(err)
	}

	// Mail flows again.
	token := uniqueToken("panelback")
	res = attemptSend(sendAttempt{
		authLogin: sc.appLogin, authPassword: sc.appPassword,
		from: "alerts@" + senderDomain, to: recipient,
		subject: "e2e " + token, body: "must be accepted again",
	})
	if !res.ok() {
		t.Fatalf("mail is still refused with the panel back: %v", res.firstErr())
	}

	// Server > Health, "Sender check", today: some downtime, one message.
	today := regexp.MustCompile(`<tr><td>Today</td><td>([^<]*)</td><td>(\d+)</td></tr>`)
	if err := waitFor("Health to show the downtime and the deferred message", 20*time.Second, 500*time.Millisecond, func() (bool, error) {
		_, body, err := sc.panel.get("/server/health")
		if err != nil {
			return false, err
		}
		m := today.FindStringSubmatch(body)
		if m == nil {
			return false, fmt.Errorf("no Sender check row for today on Health")
		}
		deferred, _ := strconv.Atoi(m[2])
		if m[1] == "0 s" || deferred < 1 {
			return false, fmt.Errorf("today: panel not answering %q, messages deferred %d", m[1], deferred)
		}
		if !strings.Contains(body, `<h2>Sender check</h2><div class="sp-end"><span class="tag is-warning is-light">warn</span>`) {
			return false, fmt.Errorf("the card does not warn about mail deferred a moment ago")
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
}

// senderCheckDeferredToday reads Server > Health's "Sender check" row for
// today: the messages deferred because the check did not answer.
func senderCheckDeferredToday(sc *scenario) (int, error) {
	_, body, err := sc.panel.get("/server/health")
	if err != nil {
		return 0, err
	}
	m := regexp.MustCompile(`<tr><td>Today</td><td>([^<]*)</td><td>(\d+)</td></tr>`).FindStringSubmatch(body)
	if m == nil {
		return 0, fmt.Errorf("no Sender check row for today on Health")
	}
	return strconv.Atoi(m[2])
}

// testPanelHungMidMessage: a panel that is running and stops answering in the
// middle of a message - its socket still accepts, nothing replies. The
// message is deferred, and the count on Health sees it. Past the DATA
// command Postfix's cleanup, not smtpd, talks to the milters, so the warning
// that names the socket is cleanup's (audit 2026-10-12, M13: the counter read
// smtpd's lines only). The panel is frozen with SIGSTOP once DATA is
// answered, and thawed after the client has its reply.
func testPanelHungMidMessage(t *testing.T, sc *scenario) {
	before, err := senderCheckDeferredToday(sc)
	if err != nil {
		t.Fatal(err)
	}
	pidOut, err := h.execIn("selfpost", "supervisorctl", "-c", "/etc/supervisor/supervisord.conf", "pid", "panel")
	if err != nil {
		t.Fatalf("panel pid: %v (%s)", err, pidOut)
	}
	pid := strings.TrimSpace(pidOut)
	if _, err := strconv.Atoi(pid); err != nil {
		t.Fatalf("panel pid: %q", pidOut)
	}
	signal := func(sig string) error {
		// kill is the shell's: the image has no kill(1).
		_, err := h.execIn("selfpost", "sh", "-c", "kill -"+sig+" "+pid)
		return err
	}
	thawed := false
	thaw := func() {
		if !thawed {
			thawed = true
			_ = signal("CONT")
		}
	}
	t.Cleanup(thaw)

	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", smtpsAddr, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         selfpostHostname,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(3 * time.Minute))
	c, err := smtp.NewClient(conn, selfpostHostname)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	from := "alerts@" + senderDomain
	if err := c.Hello("e2e-client.e2e.test"); err != nil {
		t.Fatal(err)
	}
	if err := c.Auth(smtp.PlainAuth("", sc.appLogin, sc.appPassword, selfpostHostname)); err != nil {
		t.Fatal(err)
	}
	if err := c.Mail(from); err != nil {
		t.Fatal(err)
	}
	if err := c.Rcpt(recipient); err != nil {
		t.Fatal(err)
	}
	w, err := c.Data()
	if err != nil {
		t.Fatal(err)
	}
	if err := signal("STOP"); err != nil {
		t.Fatalf("freeze the panel: %v", err)
	}
	started := time.Now()
	_, err = w.Write([]byte(buildMessage(sendAttempt{from: from, to: recipient, subject: "e2e " + uniqueToken("panelhung"), body: "must be deferred"})))
	if err == nil {
		err = w.Close()
	}
	t.Logf("end of DATA answered after %s: %v", time.Since(started).Round(time.Second), err)
	thaw()
	var reply *textproto.Error
	if !errors.As(err, &reply) || reply.Code/100 != 4 {
		t.Fatalf("want a temporary 4xx at the end of DATA with the panel frozen, got %v", err)
	}
	_ = c.Quit()

	// What Postfix wrote, so that the program and the wording are seen here
	// and not assumed.
	if out, err := h.execIn("selfpost", "sh", "-c", "grep -i 'milter' /data/log/mail.log | tail -n 8"); err == nil {
		t.Logf("mail.log on the frozen panel:\n%s", out)
	}

	if err := waitFor("Health to count the message deferred mid-message", 30*time.Second, 500*time.Millisecond, func() (bool, error) {
		n, err := senderCheckDeferredToday(sc)
		if err != nil {
			return false, err
		}
		if n <= before {
			return false, fmt.Errorf("messages deferred today: %d, as before", n)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
}
