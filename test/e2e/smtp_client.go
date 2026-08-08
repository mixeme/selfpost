package e2e

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"time"
)

// smtpsAddr is where compose.override.yml publishes the primary implicit-TLS
// submission port (465 in the shipped compose, remapped here to avoid clashing
// with a local deployment on the same host).
const smtpsAddr = "127.0.0.1:20465"

// sendAttempt is one SMTP transaction against the stand. An empty authLogin
// means "connect without AUTH" (negative checks 1/3); from/to are full
// mailbox addresses.
type sendAttempt struct {
	authLogin, authPassword string
	from, to                string
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

// waitForSMTPSReady polls the implicit-TLS port until a bare TCP connect
// succeeds. Used after a container (re)start: the panel's HTTP port and
// Postfix's smtpd come up independently (postfix-wrapper.sh additionally
// waits on both milter sockets before starting Postfix at all), so a caller
// that only waited for the panel could still race a not-yet-listening smtpd.
func waitForSMTPSReady(timeout time.Duration) error {
	return waitFor("smtps port to accept connections", timeout, 300*time.Millisecond, func() (bool, error) {
		conn, err := net.DialTimeout("tcp", smtpsAddr, 2*time.Second)
		if err != nil {
			return false, err
		}
		_ = conn.Close()
		return true, nil
	})
}

// buildMessage renders a minimal, valid RFC 5322 message. subject is expected
// to carry a unique token so the harness can find this exact message in the
// sink-MX's dump directory afterwards.
func buildMessage(a sendAttempt) string {
	return fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n",
		a.from, a.to, a.subject, time.Now().UTC().Format(time.RFC1123Z), a.body,
	)
}
