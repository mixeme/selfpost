package logtail

import (
	"testing"
	"time"
)

type refusalProbe struct {
	rows  int // rows waiting for a line
	lines []string
	who   []string
}

func (r *refusalProbe) AttachRefusalLine(login, from string, _ time.Time, line string) (bool, error) {
	if r.rows == 0 {
		return false, nil
	}
	r.rows--
	r.lines = append(r.lines, line)
	r.who = append(r.who, login+"|"+from)
	return true, nil
}

// The NOQUEUE line of a refusal at MAIL FROM goes on its row; a line whose
// row is not written yet waits and is tried again; port 25's and other
// lines are not refusals of an application.
func TestRefusalLines(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	st := &refusalProbe{}
	r := &refusalLines{st: st}
	limit := `2026-10-14T12:00:00+00:00 mail postfix/smtpd[1149]: NOQUEUE: milter-reject: MAIL from unknown[10.77.0.1]: 451 4.7.1 Service unavailable - try again later; from=<billing@sender.e2e.test> proto=ESMTP helo=<e2e-client.e2e.test> sasl_username=<l2app>`
	for _, other := range []string{
		`2026-10-14T12:00:00+00:00 mail postfix/inbound/smtpd[372]: NOQUEUE: milter-reject: MAIL from unknown[203.0.113.7]: 451 4.7.1 Service unavailable - try again later; from=<a@stranger.example> proto=ESMTP helo=<x>`,
		`2026-10-14T12:00:00+00:00 mail postfix/smtpd[11]: NOQUEUE: milter-reject: CONNECT from unknown[10.77.0.1]: 451 4.7.1 Service unavailable - try again later; proto=SMTP`,
		`2026-10-14T12:00:00+00:00 mail postfix/smtpd[11]: NOQUEUE: reject: RCPT from unknown[10.77.0.1]: 553 5.7.1 <a@x>: Sender address rejected: not owned by user app1; from=<a@x> to=<b@y> proto=ESMTP helo=<x>`,
	} {
		r.handle(other, now)
	}
	if len(r.pending) != 0 {
		t.Fatalf("lines that are not an application's refusal at MAIL wait: %+v", r.pending)
	}

	r.handle(limit, now) // its row is not there yet
	if len(r.pending) != 1 {
		t.Fatalf("pending %d, want the line waiting", len(r.pending))
	}
	st.rows = 1 // the milter's row is written
	r.retry(now.Add(time.Second))
	if len(r.pending) != 0 || len(st.lines) != 1 || st.who[0] != "l2app|billing@sender.e2e.test" {
		t.Fatalf("after the retry: pending %d, attached %v", len(r.pending), st.who)
	}

	r.handle(limit, now) // no row ever comes
	r.retry(now.Add(refusalLineWait + time.Second))
	if len(r.pending) != 0 {
		t.Fatal("a line kept waiting past its time")
	}
}
