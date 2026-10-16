package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/notify"
)

// recordingMailer stands in for notify.Mailer: what was sent, or the error to
// answer with.
type recordingMailer struct {
	to, subject, body, id []string
	err                   error
}

func (m *recordingMailer) SendWithID(to, subject, body, id string) error {
	if m.err != nil {
		return m.err
	}
	m.to, m.subject, m.body, m.id = append(m.to, to), append(m.subject, subject), append(m.body, body), append(m.id, id)
	return nil
}

// The test message (test-email): one address, from postmaster@ the hostname,
// written to the send log before it goes and opened there; refused for a
// list or a non-address with the form kept; not left in the log when Postfix
// does not take it; ten an hour.
func TestTestMessage(t *testing.T) {
	h, _, _ := serverMailStand(t)
	mail := &recordingMailer{}
	h.testMail = mail
	send := func(to string) (int, string, string) {
		rec := postFormAs(h.HandleTestMessage, globalPrincipal, "/server/health/test-message", nil, url.Values{"recipient": {to}})
		return rec.Code, rec.Header().Get("Location"), rec.Body.String()
	}

	for _, bad := range []string{"", "a@example.net, b@example.net", "not an address", "-oQ@example.net"} {
		code, _, body := send(bad)
		if code != http.StatusBadRequest || !strings.Contains(body, `<p class="help is-danger">Enter one e-mail address.</p>`) {
			t.Fatalf("recipient %q = %d:\n%.800s", bad, code, body)
		}
	}
	if len(mail.to) != 0 {
		t.Fatalf("something was sent for a refused recipient: %q", mail.to)
	}

	code, loc, _ := send("ops@elsewhere.example")
	if code != http.StatusSeeOther || !strings.HasPrefix(loc, "/outbound/log/") {
		t.Fatalf("send = %d to %q", code, loc)
	}
	if len(mail.to) != 1 || mail.to[0] != "ops@elsewhere.example" || mail.subject[0] != "SelfPost test message" ||
		!strings.HasPrefix(mail.id[0], "<") || !strings.HasSuffix(mail.id[0], "@"+notifyHost+">") {
		t.Fatalf("sent %+v", mail)
	}
	out := getBody(t, h.HandleDelivery, loc)
	if !strings.Contains(out, "ops@elsewhere.example") || !strings.Contains(out, "postmaster@"+notifyHost) {
		t.Errorf("the test message's page:\n%.2000s", out)
	}

	// Postfix does not take it: nothing stays in the log, the form says so.
	mail.err = errors.New("sendmail: exit status 75")
	code, _, body := send("ops@elsewhere.example")
	if code != http.StatusBadGateway || !strings.Contains(body, "Postfix did not take the message") ||
		!strings.Contains(body, `value="ops@elsewhere.example"`) {
		t.Fatalf("a refused hand-over = %d:\n%.800s", code, body)
	}
	if n, _ := h.store.CountTestMessagesSince("postmaster@"+notifyHost, timeAgo()); n != 1 {
		t.Fatalf("%d test messages in the log, want the one Postfix took", n)
	}

	// Ten an hour, whoever asks.
	mail.err = nil
	for i := 0; i < 9; i++ {
		if code, _, _ := send("ops@elsewhere.example"); code != http.StatusSeeOther {
			t.Fatalf("message %d of ten = %d", i+2, code)
		}
	}
	if code, _, body := send("ops@elsewhere.example"); code != http.StatusTooManyRequests || !strings.Contains(body, "Ten test messages have gone in the last hour.") {
		t.Fatalf("the eleventh = %d:\n%.600s", code, body)
	}
}

func timeAgo() time.Time { return time.Now().Add(-time.Hour) }

// sendmail out of time may have queued the message all the same: the row
// stays and the browser goes to it.
func TestTestMessageTimeoutKeepsTheRow(t *testing.T) {
	h, _, _ := serverMailStand(t)
	h.testMail = &recordingMailer{err: fmt.Errorf("%w after 20s", notify.ErrTimeout)}
	rec := postFormAs(h.HandleTestMessage, globalPrincipal, "/server/health/test-message", nil, url.Values{"recipient": {"ops@elsewhere.example"}})
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/outbound/log/") {
		t.Fatalf("a timed-out hand-over = %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if n, _ := h.store.CountTestMessagesSince("postmaster@"+notifyHost, timeAgo()); n != 1 {
		t.Fatalf("%d rows, want the one kept", n)
	}
}
