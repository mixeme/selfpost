package logtail

import (
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

type refusalProbe struct {
	rows      int // rows waiting for a line
	lines     []string
	who       []string
	recovered []store.SendLogEntry
}

func (r *refusalProbe) RecoverRefusal(e store.SendLogEntry, _ time.Time) (bool, error) {
	r.recovered = append(r.recovered, e)
	return true, nil
}

func (r *refusalProbe) AttachRefusalLine(clientIP, login, from string, _ time.Time, line string) (bool, error) {
	if r.rows == 0 {
		return false, nil
	}
	r.rows--
	r.lines = append(r.lines, line)
	r.who = append(r.who, clientIP+"|"+login+"|"+from)
	return true, nil
}

// The mail.log lines in this file are composed by hand, with the e2e stand's
// names and addresses (smtpd.c check_milter_reply(), mail_open_stream();
// smtpd_check.c log_whatsup(); cleanup_milter.c cleanup_milter_apply(),
// Postfix 3.10.13), with the stamp Postfix 3.10 writes
// (src/util/msg_logger.c, "%b %d %H:%M:%S").
//
// The NOQUEUE line of a refusal at MAIL FROM goes on its row; a line whose
// row is not written yet waits and is tried again; port 25's and other
// lines are not refusals of an application.
func TestRefusalLines(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	st := &refusalProbe{}
	r := &refusalLines{st: st}
	limit := `Oct 14 12:00:00 mail postfix/smtpd[1149]: NOQUEUE: milter-reject: MAIL from unknown[10.77.0.1]: 451 4.7.1 Service unavailable - try again later; from=<billing@sender.e2e.test> proto=ESMTP helo=<e2e-client.e2e.test> sasl_username=<l2app>`
	for _, other := range []string{
		`Oct 14 12:00:00 mail postfix/inbound/smtpd[372]: NOQUEUE: milter-reject: MAIL from unknown[203.0.113.7]: 451 4.7.1 Service unavailable - try again later; from=<a@stranger.example> proto=ESMTP helo=<x>`,
		`Oct 14 12:00:00 mail postfix/smtpd[11]: NOQUEUE: milter-reject: CONNECT from unknown[10.77.0.1]: 451 4.7.1 Service unavailable - try again later; proto=SMTP`,
		`Oct 14 12:00:00 mail postfix/smtpd[11]: NOQUEUE: reject: RCPT from unknown[10.77.0.1]: 553 5.7.1 <a@x>: Sender address rejected: not owned by user app1; from=<a@x> to=<b@y> proto=ESMTP helo=<x> sasl_method=PLAIN sasl_username=app1`,
		// No login: no application's refusal (the panel down refuses every
		// session, an unauthenticated one too).
		`Oct 14 12:00:00 mail postfix/smtpd[12]: NOQUEUE: milter-reject: MAIL from unknown[10.77.0.1]: 451 4.7.1 Service unavailable - try again later; from=<billing@sender.e2e.test> proto=ESMTP helo=<x>`,
	} {
		r.handle(other, now, Place{})
	}
	if len(r.pending) != 0 {
		t.Fatalf("lines that are not an application's refusal at MAIL wait: %+v", r.pending)
	}

	r.handle(limit, now, Place{}) // its row is not there yet
	if len(r.pending) != 1 {
		t.Fatalf("pending %d, want the line waiting", len(r.pending))
	}
	st.rows = 1 // the milter's row is written
	r.retry(now.Add(time.Second))
	if len(r.pending) != 0 || len(st.lines) != 1 || st.who[0] != "10.77.0.1|l2app|billing@sender.e2e.test" {
		t.Fatalf("after the retry: pending %d, attached %v", len(r.pending), st.who)
	}

	r.handle(limit, now, Place{}) // no row ever comes
	r.retry(now.Add(refusalLineWait + time.Second))
	if len(r.pending) != 0 {
		t.Fatal("a line kept waiting past its time")
	}
}

// cleanup's line about a From: refusal is the row's, written by the panel; once
// the panel has had the time to, the line is handed to RecoverRefusal, which
// writes the row only if it is missing - with the login smtpd named for the
// queue id, the sender, the recipient and the From: line as printed.
func TestFromRefusalIsRecoveredAfterTheWait(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	st := &refusalProbe{}
	r := &refusalLines{st: st}
	r.handle(`Oct 14 12:00:00 mail postfix/smtpd[11]: E543D3C75DA0: client=unknown[10.77.0.1], sasl_method=PLAIN, sasl_username=app1`, now, Place{})
	r.handle(`Oct 14 12:00:01 mail postfix/cleanup[13]: E543D3C75DA0: milter-reject: END-OF-MESSAGE from unknown[10.77.0.1]: 5.7.1 From: "The - Shop" <director@other.e2e.test> - it names another sender of this server; from=<alerts@sender.e2e.test> to=<rcpt@sink.e2e.test> proto=ESMTP helo=<x>`, now, Place{})
	r.retry(now.Add(time.Second))
	if len(st.recovered) != 0 {
		t.Fatal("recovered before the panel had the time to write the row")
	}
	r.retry(now.Add(refusalLineWait + time.Second))
	if len(st.recovered) != 1 {
		t.Fatalf("recovered %d, want 1", len(st.recovered))
	}
	e := st.recovered[0]
	want := store.SendLogEntry{QueueID: "E543D3C75DA0", AppLogin: "app1", From: "alerts@sender.e2e.test", To: "rcpt@sink.e2e.test",
		Domain: "sender.e2e.test", Reason: store.ReasonFrom, HeaderFrom: `"The - Shop" <director@other.e2e.test>`}
	if e != want {
		t.Fatalf("recovered %+v, want %+v", e, want)
	}

	// A refusal for the Sender: line (2.8.0) is recovered as one: composed
	// after the From: line above, with the check's reply as sender.go writes
	// it.
	st = &refusalProbe{}
	r = &refusalLines{st: st}
	r.handle(`Oct 14 12:00:00 mail postfix/smtpd[11]: F1A2B3C4D5E6: client=unknown[10.77.0.1], sasl_method=PLAIN, sasl_username=app1`, now, Place{})
	r.handle(`Oct 14 12:00:01 mail postfix/cleanup[13]: F1A2B3C4D5E6: milter-reject: END-OF-MESSAGE from unknown[10.77.0.1]: 5.7.1 Sender: ceo@bank.example - not an address this application sends as; from=<alerts@sender.e2e.test> to=<rcpt@sink.e2e.test> proto=ESMTP helo=<x>`, now, Place{})
	r.retry(now.Add(refusalLineWait + time.Second))
	if len(st.recovered) != 1 || st.recovered[0].Reason != store.ReasonSender || st.recovered[0].HeaderFrom != "ceo@bank.example" || st.recovered[0].From != "alerts@sender.e2e.test" {
		t.Fatalf("recovered %+v, want one Sender: refusal", st.recovered)
	}
}

// While a line or a recovery waits, the offset saved for its file stays at the
// earliest line it needs - for a recovery the client= line that names its
// login - so a stop before the wait is over reads them again (review of stage
// 2b).
func TestWaitingLinesHoldTheSavedOffset(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	r := &refusalLines{st: &refusalProbe{}}
	if _, held := r.oldest("fp"); held {
		t.Fatal("nothing waits, yet the offset is held")
	}
	r.handle(`Oct 14 12:00:00 mail postfix/smtpd[11]: Q1: client=unknown[10.77.0.1], sasl_method=PLAIN, sasl_username=app1`, now, Place{FP: "fp", End: 100})
	r.handle(`Oct 14 12:00:01 mail postfix/cleanup[13]: Q1: milter-reject: END-OF-MESSAGE from unknown[10.77.0.1]: 5.7.1 From: d@other.example - not an address this application sends as; from=<a@sender.example> to=<r@x> proto=ESMTP helo=<x>`, now, Place{FP: "fp", End: 300})
	if p, held := r.oldest("fp"); !held || p.End != 100 {
		t.Fatalf("held at %+v %v, want the client= line at 100", p, held)
	}
	if _, held := r.oldest("another"); held {
		t.Fatal("a place in another file holds this one")
	}
	r.retry(now.Add(refusalLineWait + time.Second))
	if _, held := r.oldest("fp"); held {
		t.Fatal("still held after the wait")
	}

	// The tracker clamps what it saves to the hold.
	cs := &captureStore{}
	tr := &tracker{st: cs, path: "x", fp: "fp", hold: func(string) (Place, bool) { return Place{FP: "fp", End: 50}, true }}
	tr.record(nil, 400, true)
	if cs.state.Offset != 50 {
		t.Fatalf("saved %d, want the hold at 50", cs.state.Offset)
	}
}
