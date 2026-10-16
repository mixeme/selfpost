package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"os/exec"
	"strings"
	"time"
)

const (
	sendmailPath    = "/usr/sbin/sendmail"
	sendmailTimeout = 20 * time.Second
)

// Mailer hands notifications to Postfix by local submission, as
// postmaster@<hostname>. They are the panel's own mail: they pass no SASL
// login, are not rows of the send log and count against no rate limit.
// The panel's signer signs them with the key of the server's own name
// (non_smtpd_milters in build/postfix-config.sh, domain.Service.SetServerName).
type Mailer struct {
	hostname string
	// run submits a finished message; a field so tests can read it instead.
	run func(from, to string, msg []byte) error
}

// NewMailer returns a mailer for the server named hostname.
func NewMailer(hostname string) *Mailer {
	return &Mailer{hostname: hostname, run: sendmail}
}

// From is the address notifications are sent from.
func From(hostname string) string { return "postmaster@" + hostname }

// Send submits one message to one recipient.
func (m *Mailer) Send(to, subject, body string) error {
	id, err := NewMessageID(m.hostname)
	if err != nil {
		return err
	}
	return m.SendWithID(to, subject, body, id)
}

// NewMessageID makes a Message-ID of the server's own name, for a message the
// caller wants to know again in mail.log (the panel's test message).
func NewMessageID(hostname string) (string, error) { return messageID(hostname) }

// SendWithID is Send with the Message-ID given: cleanup logs it with the
// message's queue id, which is how the panel finds its test message in
// mail.log (logtail.AwaitMessageID).
func (m *Mailer) SendWithID(to, subject, body, id string) error {
	// The address is a panel user's, validated when it was saved. It goes into
	// a header and onto a command line all the same, so what cannot be part of
	// an address is refused here as well (security.md).
	if to == "" || strings.ContainsAny(to, " \t\r\n<>,;\"") || strings.HasPrefix(to, "-") || !strings.Contains(to, "@") {
		// (validate.AccountEmail refuses the same, so a user is told at the
		// form and not here, once a minute, in the log.)
		return fmt.Errorf("not an address a notification can go to: %q", to)
	}
	from := From(m.hostname)

	var b bytes.Buffer
	fmt.Fprintf(&b, "From: SelfPost <%s>\r\n", from)
	fmt.Fprintf(&b, "To: <%s>\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", oneLine(subject)))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: %s\r\n", id)
	// Tells a vacation responder and a ticket system that nobody wrote this.
	b.WriteString("Auto-Submitted: auto-generated\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n"))
	return m.run(from, to, b.Bytes())
}

// oneLine keeps a subject on its line whatever went into it.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func messageID(hostname string) (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("message id: %w", err)
	}
	return fmt.Sprintf("<%s.%d@%s>", hex.EncodeToString(raw[:]), time.Now().Unix(), hostname), nil
}

// ErrTimeout is sendmail not answering in time: postdrop may have queued the
// message all the same.
var ErrTimeout = errors.New("sendmail timed out")

// sendmail submits msg through Postfix's sendmail command. The argument list is
// fixed apart from the two addresses, which follow "--" so that neither can be
// read as an option; nothing goes through a shell (security.md). -i keeps a
// line holding a single dot from ending the message.
func sendmail(from, to string, msg []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), sendmailTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, sendmailPath, "-i", "-f", from, "--", to)
	cmd.Stdin = bytes.NewReader(msg)
	// sendmail hands the message to postdrop; if that child outlives the
	// timeout holding the output pipe, do not wait for it for ever.
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%w after %s", ErrTimeout, sendmailTimeout)
	}
	if err != nil {
		return fmt.Errorf("sendmail: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
