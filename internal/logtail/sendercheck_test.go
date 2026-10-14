package logtail

import (
	"testing"
	"time"
)

type deferredStore struct {
	days  map[string]int64 // connections turned away
	msgs  map[string]int64 // messages deferred in the middle
	last  time.Time
	place string
}

func (d *deferredStore) SenderCheckDeferredAt() (time.Time, error) { return d.last, nil }

func (d *deferredStore) SenderCheckCountedPlace() (string, error) { return d.place, nil }

func (d *deferredStore) AddSenderCheckDeferred(at time.Time, n int64, place string, message bool) error {
	d.place = place
	if d.days == nil {
		d.days, d.msgs = make(map[string]int64), make(map[string]int64)
	}
	if message {
		d.msgs[at.UTC().Format("2006-01-02")] += n
	} else {
		d.days[at.UTC().Format("2006-01-02")] += n
	}
	if at.After(d.last) {
		d.last = at
	}
	return nil
}

// The lines of process 1183 and of cleanup are as the e2e stand wrote them
// with the panel stopped and for a refused From: (2026-10-12), and with the
// panel frozen in the middle of a message (cleanup 1339, 2026-10-14); the
// stamps are put in ISO form. Around them are the lines of two other things
// that use the same words - a level-2 refusal of the journal milter, OpenDKIM
// not answering.
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

		// The check stops answering in the middle of a message, the next day:
		// cleanup meets it. Two messages through the same cleanup process are
		// two messages.
		`2026-10-13T00:00:05.000000+00:00 mail postfix/smtpd[1300]: connect from unknown[10.77.0.1]`,
		`2026-10-13T00:00:35.000000+00:00 mail postfix/cleanup[1339]: warning: milter unix:/run/selfpost/sender.sock: can't read SMFIC_HEADER reply packet header: Connection timed out`,
		`2026-10-13T00:00:35.100000+00:00 mail postfix/cleanup[1339]: 7BCAF3C75DA0: milter-reject: END-OF-MESSAGE from unknown[10.77.0.1]: 4.7.1 Service unavailable - try again later; from=<alerts@sender.e2e.test> to=<rcpt@sink.e2e.test> proto=ESMTP helo=<e2e-client.e2e.test>`,
		`2026-10-13T00:01:20.000000+00:00 mail postfix/cleanup[1339]: warning: milter unix:/run/selfpost/sender.sock: can't read SMFIC_HEADER reply packet header: Connection timed out`,
		`2026-10-13T00:01:20.100000+00:00 mail postfix/cleanup[1339]: 8C1D43C75DA0: milter-reject: END-OF-MESSAGE from unknown[10.77.0.1]: 4.7.1 Service unavailable - try again later; from=<alerts@sender.e2e.test> to=<rcpt@sink.e2e.test> proto=ESMTP helo=<e2e-client.e2e.test>`,

		// OpenDKIM hung in the middle of a message: another socket.
		`2026-10-13T00:01:30.000000+00:00 mail postfix/cleanup[1339]: warning: milter unix:/run/opendkim/opendkim.sock: can't read SMFIC_HEADER reply packet header: Connection timed out`,

		// The check's own verdict is a refusal, not a deferral, and it is
		// cleanup that writes it.
		`2026-10-13T00:01:00.000000+00:00 mail postfix/cleanup[1195]: B6B484831946: milter-reject: END-OF-MESSAGE from unknown[10.77.0.1]: 5.7.1 From: director@other.e2e.test - not an address this application sends as; from=<alerts@sender.e2e.test> to=<rcpt@sink.e2e.test> proto=ESMTP helo=<x>`,
	} {
		c.handle(line, now, false, Place{})
	}
	if st.days["2026-10-12"] != 1 || st.days["2026-10-13"] != 0 || st.msgs["2026-10-13"] != 2 || len(st.msgs) != 1 {
		t.Fatalf("connections %v, messages %v; want one connection on the first day and two messages on the second", st.days, st.msgs)
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
		c.handle(line, now, false, Place{})
	}
	if st.days["2026-10-12"] != 2 {
		t.Fatalf("first run counted %d, want 2", st.days["2026-10-12"])
	}

	// A new run: the same lines again, then what the outage added.
	c = newDeferredCounter(&SenderCheck{Store: st, Socket: "/run/selfpost/sender.sock"})
	for _, line := range append(first, outage("13", "2026-10-12T16:00:09.00000")...) {
		c.handle(line, now, true, Place{})
	}
	if st.days["2026-10-12"] != 3 {
		t.Fatalf("after the restart the day has %d, want 3", st.days["2026-10-12"])
	}
}

// Two warnings of the same second, the panel stopped uncleanly between them:
// the next start reads both again, and the place of the first - not its
// second - tells them apart. Each is counted once (audit 2026-10-12, stage
// 2b; a time alone passed the second over).
func TestDeferredCounterRemembersAPlaceNotASecond(t *testing.T) {
	st := &deferredStore{}
	now := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	lines := []string{
		`2026-10-12T16:00:00+00:00 mail postfix/smtpd[11]: connect from unknown[10.77.0.1]`,
		`2026-10-12T16:00:01+00:00 mail postfix/smtpd[11]: warning: connect to Milter service unix:/run/selfpost/sender.sock: Connection refused`,
		`2026-10-12T16:00:01+00:00 mail postfix/smtpd[12]: connect from unknown[10.77.0.1]`,
		`2026-10-12T16:00:01+00:00 mail postfix/smtpd[12]: warning: connect to Milter service unix:/run/selfpost/sender.sock: Connection refused`,
	}
	// The place each line ends at, in one file.
	places := make([]Place, len(lines))
	end := int64(0)
	for i, l := range lines {
		end += int64(len(l)) + 1
		places[i] = Place{FP: "abc:1.2", End: end}
	}

	c := newDeferredCounter(&SenderCheck{Store: st, Socket: "/run/selfpost/sender.sock"})
	for i := 0; i < 2; i++ { // stopped after the first warning
		c.handle(lines[i], now, false, places[i])
	}
	if st.days["2026-10-12"] != 1 {
		t.Fatalf("before the stop: %d, want 1", st.days["2026-10-12"])
	}

	// Started again: the saved offset was from before the stop, so all four
	// lines are read again.
	c = newDeferredCounter(&SenderCheck{Store: st, Socket: "/run/selfpost/sender.sock"})
	for i, l := range lines {
		c.handle(l, now, true, places[i])
	}
	if st.days["2026-10-12"] != 2 {
		t.Fatalf("after the restart the day has %d, want 2", st.days["2026-10-12"])
	}
}
