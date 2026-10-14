package e2e

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

// smtpsAddr is where compose.override.yml publishes the primary implicit-TLS
// submission port (465 in the shipped compose, remapped here to avoid clashing
// with a local deployment on the same host).
const smtpsAddr = "127.0.0.1:20465"

// sendAttempt is one SMTP transaction against the stand. An empty authLogin
// means "connect without AUTH" (negative checks 1/3); from/to are full
// mailbox addresses. headerFrom is the From: line of the message when it is
// not to be the envelope sender (the sender check holds the two to one rule).
type sendAttempt struct {
	authLogin, authPassword string
	from, to                string
	headerFrom              string
	subject, body           string
}

// sendResult records the error (if any) at each stage the transaction
// reached. Only the fields up to and including the first failure are set —
// the rest are nil because that stage was never attempted, not because it
// succeeded silently.
type sendResult struct {
	dialErr error
	authErr error
	mailErr error
	rcptErr error
	dataErr error
}

// ok reports whether the message was fully accepted (DATA closed cleanly).
func (r sendResult) ok() bool {
	return r.dialErr == nil && r.authErr == nil && r.mailErr == nil && r.rcptErr == nil && r.dataErr == nil
}

// firstErr returns whichever stage failed first, or nil if none did.
func (r sendResult) firstErr() error {
	for _, e := range []error{r.dialErr, r.authErr, r.mailErr, r.rcptErr, r.dataErr} {
		if e != nil {
			return e
		}
	}
	return nil
}

// refusedAt says nil only when the transaction was refused at stage
// ("connect", "AUTH", "MAIL", "RCPT" or "DATA") with the server's reply code
// and a reply text containing text. A negative case passes on its own refusal
// and nothing else: a reset or a timeout while Postfix was still starting once
// passed for the level-1 rate limit. Anything else is described for the
// failure message - where it stopped and what came.
func (r sendResult) refusedAt(stage string, code int, text string) error {
	for _, s := range []struct {
		name string
		err  error
	}{{"connect", r.dialErr}, {"AUTH", r.authErr}, {"MAIL", r.mailErr}, {"RCPT", r.rcptErr}, {"DATA", r.dataErr}} {
		if s.err == nil {
			continue
		}
		if s.name != stage {
			return fmt.Errorf("stopped at %s, not at %s: %v", s.name, stage, s.err)
		}
		return replyIs(s.err, code, text)
	}
	return fmt.Errorf("accepted, want refused at %s with %d", stage, code)
}

// replyIs says nil when err is the server's reply code with text in it.
func replyIs(err error, code int, text string) error {
	var reply *textproto.Error
	if !errors.As(err, &reply) {
		return fmt.Errorf("no SMTP reply but %v, want %d ...%s", err, code, text)
	}
	if reply.Code != code || !strings.Contains(reply.Msg, text) {
		return fmt.Errorf("reply %d %s, want %d ...%s", reply.Code, reply.Msg, code, text)
	}
	return nil
}

// attemptSend drives one SMTP transaction over the implicit-TLS port, exactly
// as an application client library would: TLS connect, EHLO, optional AUTH
// PLAIN, MAIL/RCPT/DATA. The certificate is the stand's throwaway self-signed
// one (see stage.go), so verification is skipped — Postfix's
// smtpd_tls_security_level is "may" here just like production, this is purely
// about the test client trusting an unknown CA.
func attemptSend(a sendAttempt) sendResult {
	var res sendResult

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", smtpsAddr, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         selfpostHostname,
	})
	if err != nil {
		res.dialErr = fmt.Errorf("dial %s: %w", smtpsAddr, err)
		return res
	}
	c, err := smtp.NewClient(conn, selfpostHostname)
	if err != nil {
		res.dialErr = fmt.Errorf("smtp handshake: %w", err)
		_ = conn.Close()
		return res
	}
	defer c.Close()

	if err := c.Hello("e2e-client.e2e.test"); err != nil {
		res.dialErr = err
		return res
	}

	if a.authLogin != "" {
		auth := smtp.PlainAuth("", a.authLogin, a.authPassword, selfpostHostname)
		if err := c.Auth(auth); err != nil {
			res.authErr = err
			_ = c.Quit()
			return res
		}
	}

	if err := c.Mail(a.from); err != nil {
		res.mailErr = err
		_ = c.Quit()
		return res
	}
	if err := c.Rcpt(a.to); err != nil {
		res.rcptErr = err
		_ = c.Quit()
		return res
	}
	w, err := c.Data()
	if err != nil {
		res.dataErr = err
		_ = c.Quit()
		return res
	}
	msg := buildMessage(a)
	if _, err := w.Write([]byte(msg)); err != nil {
		res.dataErr = err
	} else if err := w.Close(); err != nil {
		res.dataErr = err
	}
	_ = c.Quit()
	return res
}

// waitForSMTPSReady polls the implicit-TLS port until Postfix greets over
// TLS. Used after a container (re)start: the panel's HTTP port and Postfix's
// smtpd come up independently (postfix-wrapper.sh additionally waits on both
// milter sockets before starting Postfix at all), so a caller that only
// waited for the panel could still race a not-yet-listening smtpd. A bare TCP
// connect is not enough: Docker's port proxy accepts on the host side before
// anything listens in the container and then resets the connection, which is
// how the level-1 rate-limit case once took a reset for its limit.
func waitForSMTPSReady(timeout time.Duration) error {
	return waitFor("smtps port to greet", timeout, 300*time.Millisecond, func() (bool, error) {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", smtpsAddr, &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         selfpostHostname,
		})
		if err != nil {
			return false, err
		}
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		c, err := smtp.NewClient(conn, selfpostHostname) // reads the 220 greeting
		if err != nil {
			_ = conn.Close()
			return false, err
		}
		_ = c.Quit()
		_ = c.Close()
		return true, nil
	})
}

// buildMessage renders a minimal, valid RFC 5322 message. subject is expected
// to carry a unique token so the harness can find this exact message in the
// sink-MX's dump directory afterwards.
func buildMessage(a sendAttempt) string {
	if a.headerFrom != "" {
		a.from = a.headerFrom
	}
	return fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n",
		a.from, a.to, a.subject, time.Now().UTC().Format(time.RFC1123Z), a.body,
	)
}

// mailTwiceOnOneConnection authenticates once and sends MAIL FROM twice on the
// same connection, with a RSET between, and returns how each was answered. No
// message is sent: this is about what the server says to the second
// transaction of a connection whose first was refused.
func mailTwiceOnOneConnection(login, password, from string) [2]error {
	var out [2]error
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", smtpsAddr, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         selfpostHostname,
	})
	if err != nil {
		return [2]error{nil, nil}
	}
	c, err := smtp.NewClient(conn, selfpostHostname)
	if err != nil {
		_ = conn.Close()
		return [2]error{nil, nil}
	}
	defer c.Close()
	if err := c.Hello("e2e-client.e2e.test"); err != nil {
		return [2]error{nil, nil}
	}
	if err := c.Auth(smtp.PlainAuth("", login, password, selfpostHostname)); err != nil {
		return [2]error{nil, nil}
	}
	out[0] = c.Mail(from)
	_ = c.Reset()
	out[1] = c.Mail(from)
	_ = c.Quit()
	return out
}
