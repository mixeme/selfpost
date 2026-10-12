package logtail

import (
	"testing"
	"time"
)

type deferredStore struct {
	days map[string]int64
	last time.Time
}

func (d *deferredStore) SenderCheckDeferredAt() (time.Time, error) { return d.last, nil }

func (d *deferredStore) AddSenderCheckDeferred(at time.Time, n int64) error {
	if d.days == nil {
		d.days = make(map[string]int64)
	}
	d.days[at.UTC().Format("2006-01-02")] += n
	if at.After(d.last) {
		d.last = at
	}
	return nil
}

// The lines of process 1183 and of cleanup are as the e2e stand wrote them
// with the panel stopped and for a refused From: (2026-10-12; the stamps are
// put in ISO form). Around them are the lines of two other things that use
// the same words - a level-2 refusal of the journal milter, OpenDKIM not
// answering - and, composed from Postfix's source rather than seen, a check
// that stops answering in the middle of a message.
func TestDeferredCounter(t *testing.T) {
	st := &deferredStore{}
	c := newDeferredCounter(&SenderCheck{Store: st, Socket: "/run/selfpost/sender.sock"})
	now := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	for _, line := range []string{
		// The journal milter's own temporary refusal: no warning, not counted.
		`2026-10-12T16:22:13.100000+00:00 mail postfix/smtpd[1149]: connect from unknown[10.77.0.1]`,
		`2026-10-12T16:22:13.200000+00:00 mail postfix/smtpd[1149]: NOQUEUE: milter-reject: MAIL from unknown[10.77.0.1]: 451 4.7.1 Service unavailable - try again later; from=<billing@sender.e2e.test> proto=ESMTP helo=<e2e-client.e2e.test> sasl_username=<l2app>`,

		// The check does not answer: one connection, counted once.
		`2026-10-12T16:22:57.000000+00:00 mail postfix/smtpd[1183]: connect from unknown[10.77.0.1]`,
		`2026-10-12T16:22:57.100000+00:00 mail postfix/smtpd[1183]: warning: connect to Milter service unix:/run/selfpost/sender.sock: No such file or directory`,
		`2026-10-12T16:22:57.200000+00:00 mail postfix/smtpd[1183]: NOQUEUE: milter-reject: CONNECT from unknown[10.77.0.1]: 451 4.7.1 Service unavailable - try again later; proto=SMTP`,
		`2026-10-12T16:22:57.300000+00:00 mail postfix/smtpd[1183]: NOQUEUE: milter-reject: EHLO from unknown[10.77.0.1]: 451 4.7.1 Service unavailable - try again later; proto=SMTP helo=<e2e-client.e2e.test>`,
		`2026-10-12T16:22:57.400000+00:00 mail postfix/smtpd[1183]: disconnect from unknown[10.77.0.1] ehlo=1 auth=0/1 quit=1 commands=2/3`,

		// The same process takes another client, and the check answers again:
		// a level-2 refusal there is not the check's.
		`2026-10-12T16:23:30.000000+00:00 mail postfix/smtpd[1183]: connect from unknown[10.77.0.1]`,
		`2026-10-12T16:23:30.100000+00:00 mail postfix/smtpd[1183]: NOQUEUE: milter-reject: MAIL from unknown[10.77.0.1]: 451 4.7.1 Service unavailable - try again later; from=<a@sender.e2e.test> proto=ESMTP helo=<x>`,

		// A client that writes the socket's name where Postfix will quote it
		// in a warning, and then runs into a level-2 refusal: not counted.
		`2026-10-12T16:23:40.000000+00:00 mail postfix/smtpd[1190]: connect from unknown[10.77.0.1]`,
		`2026-10-12T16:23:40.100000+00:00 mail postfix/smtpd[1190]: warning: Illegal address syntax from unknown[10.77.0.1] in MAIL command: <warning: connect to Milter service unix:/run/selfpost/sender.sock: milter>`,
		`2026-10-12T16:23:40.200000+00:00 mail postfix/smtpd[1190]: NOQUEUE: milter-reject: MAIL from unknown[10.77.0.1]: 451 4.7.1 Service unavailable - try again later; from=<a@sender.e2e.test> proto=ESMTP helo=<x>`,

		// OpenDKIM does not answer: another socket, not counted.
		`2026-10-12T16:24:00.000000+00:00 mail postfix/smtpd[1201]: connect from unknown[10.77.0.1]`,
		`2026-10-12T16:24:00.100000+00:00 mail postfix/smtpd[1201]: warning: connect to Milter service unix:/run/opendkim/opendkim.sock: Connection refused`,
		`2026-10-12T16:24:00.200000+00:00 mail postfix/smtpd[1201]: NOQUEUE: milter-reject: CONNECT from unknown[10.77.0.1]: 451 4.7.1 Service unavailable - try again later; proto=SMTP`,

		// The check stops answering in the middle of a message, the next day.
		`2026-10-13T00:00:05.000000+00:00 mail postfix/smtpd[1300]: connect from unknown[10.77.0.1]`,
		`2026-10-13T00:00:21.000000+00:00 mail postfix/smtpd[1300]: warning: milter unix:/run/selfpost/sender.sock: can't read SMFIC_EOH reply packet header: Connection timed out`,
		`2026-10-13T00:00:21.050000+00:00 mail postfix/smtpd[1300]: warning: milter unix:/run/selfpost/sender.sock: can't read SMFIC_BODYEOB reply packet header: Connection timed out`,
		`2026-10-13T00:00:21.100000+00:00 mail postfix/cleanup[1195]: 4XcB7k2Jm9z1: milter-reject: END-OF-MESSAGE from unknown[10.77.0.1]: 4.7.1 Service unavailable - try again later; from=<a@sender.e2e.test> to=<b@sink.e2e.test> proto=ESMTP helo=<x>`,

		// The check's own verdict is a refusal, not a deferral, and it is
		// cleanup that writes it.
		`2026-10-13T00:01:00.000000+00:00 mail postfix/cleanup[1195]: B6B484831946: milter-reject: END-OF-MESSAGE from unknown[10.77.0.1]: 5.7.1 From: director@other.e2e.test - not an address this application sends as; from=<alerts@sender.e2e.test> to=<rcpt@sink.e2e.test> proto=ESMTP helo=<x>`,
	} {
		c.handle(line, now, false)
	}
	if len(st.days) != 2 || st.days["2026-10-12"] != 1 || st.days["2026-10-13"] != 1 {
		t.Fatalf("deferred per day = %v, want one on each of the two days", st.days)
	}
}

// After an unclean stop the tailer reads again some lines it had read. What
// was counted then is not counted now; what was written while the panel was
// down - the very lines this count exists for - is.
func TestDeferredCounterDoesNotCountALogReadAgain(t *testing.T) {
	st := &deferredStore{}
	now := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	outage := func(pid, at string) []string {
		return []string{
			at + `0+00:00 mail postfix/smtpd[` + pid + `]: connect from unknown[10.77.0.1]`,
			at + `1+00:00 mail postfix/smtpd[` + pid + `]: warning: connect to Milter service unix:/run/selfpost/sender.sock: Connection refused`,
		}
	}
	first := append(outage("11", "2026-10-12T16:00:00.00000"), outage("12", "2026-10-12T16:00:05.00000")...)

	c := newDeferredCounter(&SenderCheck{Store: st, Socket: "/run/selfpost/sender.sock"})
	for _, line := range first {
		c.handle(line, now, false)
	}
	if st.days["2026-10-12"] != 2 {
		t.Fatalf("first run counted %d, want 2", st.days["2026-10-12"])
	}

	// A new run: the same lines again, then what the outage added.
	c = newDeferredCounter(&SenderCheck{Store: st, Socket: "/run/selfpost/sender.sock"})
	for _, line := range append(first, outage("13", "2026-10-12T16:00:09.00000")...) {
		c.handle(line, now, true)
	}
	if st.days["2026-10-12"] != 3 {
		t.Fatalf("after the restart the day has %d, want 3", st.days["2026-10-12"])
	}
}
