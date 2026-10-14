package logtail

import (
	"log"
	"regexp"
	"strings"
	"time"
)

// The sender check is attached to Postfix strictly: while the panel does not
// answer on its socket, mail is deferred (docs/plans/from-header-binding.md).
// How many submissions that happened to is read here, from what Postfix
// writes about it, and shown on Server > Health. It is the figure that also
// sees a panel which is running and not answering.
//
// Postfix says it in lines of one smtpd process - why, then what (seen on
// the e2e stand, 2026-10-12):
//
//	postfix/smtpd[1183]: warning: connect to Milter service unix:/run/selfpost/sender.sock: No such file or directory
//	postfix/smtpd[1183]: NOQUEUE: milter-reject: CONNECT from unknown[10.77.0.1]: 451 4.7.1 Service unavailable - try again later; proto=SMTP
//	postfix/smtpd[1183]: NOQUEUE: milter-reject: EHLO from unknown[10.77.0.1]: 451 4.7.1 Service unavailable - try again later; proto=SMTP helo=<app>
//
// and the client, told "454 4.3.0 Try again later" at AUTH, goes away without
// a line of its own. So what is counted is the connection Postfix turned
// away, once: a client brings a message when it connects, and it will bring
// it again.
//
// It is counted at the warning, which is the one line that names the socket.
// The reject lines do not: the same words are written when OpenDKIM does not
// answer and when the journal milter refuses under a level-2 limit.
//
// A check that is running and stops answering in the middle of a message -
// its socket accepts, nothing replies - is met by cleanup, not smtpd: past the
// DATA command it is cleanup that talks to the milters. Seen on the e2e stand
// with the panel frozen (SIGSTOP) once DATA was answered (2026-10-14):
//
//	postfix/cleanup[1339]: warning: milter unix:/run/selfpost/sender.sock: can't read SMFIC_HEADER reply packet header: Connection timed out
//	postfix/cleanup[1339]: 7BCAF3C75DA0: milter-reject: END-OF-MESSAGE from unknown[10.77.0.1]: 4.7.1 Service unavailable - try again later; from=<…> to=<…> proto=ESMTP helo=<…>
//
// One cleanup process serves message after message and the first warning
// ends the message, so each such warning is a message, counted on its own.
// Until 2.5.0 only smtpd's lines were read, and this case - the one that
// tells a hung panel from a stopped one - was never counted (audit
// 2026-10-12, M13).

// SenderCheckStore is where the count goes, and where the time of the last
// counted line is kept. *store.Store satisfies it.
type SenderCheckStore interface {
	// AddSenderCheckDeferred counts and saves the place in the log reached,
	// in one transaction.
	AddSenderCheckDeferred(at time.Time, n int64, place string, message bool) error
	SenderCheckDeferredAt() (time.Time, error)
	SenderCheckCountedPlace() (string, error)
}

// SenderCheck turns the counting on: the store and the socket Postfix was
// given for the check (SENDER_CHECK_SOCKET), as a path.
type SenderCheck struct {
	Store  SenderCheckStore
	Socket string
}

var milterLineRe = regexp.MustCompile(`^\S+ postfix/(?:[a-z0-9-]+/)?(smtpd|cleanup)\[(\d+)\]: (.*)$`)

const (
	// senderCheckPids bounds the processes remembered; smtpd processes are a
	// few dozen, and a log that names more is not kept track of.
	senderCheckPids = 1024
)

// deferredCounter is the per-run state of the count.
type deferredCounter struct {
	st SenderCheckStore
	// The two warnings, up to and including the socket's name: nobody
	// listening, and a reply that did not come.
	connect, broken string
	failed          map[string]bool // smtpd pid -> its current client was counted already
	// counted is the place in the log of the latest warning counted, by this
	// run or an earlier one, saved with the count. While the tailer catches up
	// after a start it may be reading lines it had read before an unclean
	// stop; a warning at or before that place was counted then. Until 2.5.1
	// this was a time, and mail.log's stamps are whole seconds: a second
	// warning of the same second was passed over (audit 2026-10-12, stage
	// 2b). A place in another file - read again from a rotated log - has no
	// order to compare with, and last, its time, decides.
	counted Place
	last    time.Time
}

func newDeferredCounter(sc *SenderCheck) *deferredCounter {
	last, err := sc.Store.SenderCheckDeferredAt()
	if err != nil {
		log.Printf("log-tailer: sender check statistics: %v", err)
	}
	place, err := sc.Store.SenderCheckCountedPlace()
	if err != nil {
		log.Printf("log-tailer: sender check statistics: %v", err)
	}
	return &deferredCounter{
		last:    last,
		counted: parsePlace(place),
		st:      sc.Store,
		connect: "warning: connect to Milter service unix:" + sc.Socket + ":",
		broken:  "warning: milter unix:" + sc.Socket + ":",
		failed:  make(map[string]bool),
	}
}

// handle looks at one mail.log line, which ends at place. catchingUp is true
// until the tailer has read to the end of the log for the first time.
func (c *deferredCounter) handle(line string, now time.Time, catchingUp bool, place Place) {
	at, rest := lineTime(line, now)
	m := milterLineRe.FindStringSubmatch(rest)
	if m == nil {
		return
	}
	program, pid, text := m[1], m[2], m[3]
	if program == "cleanup" {
		// A message cleanup could not finish because the check did not reply.
		if strings.HasPrefix(text, c.broken) {
			c.count(at, catchingUp, place, true)
		}
		return
	}
	switch {
	case strings.HasPrefix(text, "connect from "):
		delete(c.failed, pid)
	case strings.HasPrefix(text, c.connect) || strings.HasPrefix(text, c.broken):
		// Postfix's own two sentences, from their first word: a warning also
		// quotes what a client sent ("Illegal address syntax ... in MAIL
		// command: <text>"), and a client can send the socket's name.
		// Once for the connection, however many warnings it draws.
		if c.failed[pid] {
			return
		}
		if len(c.failed) >= senderCheckPids {
			c.failed = make(map[string]bool)
		}
		c.failed[pid] = true
		c.count(at, catchingUp, place, false)
	}
}

// count adds, at at, one connection turned away - or, with message, one
// message deferred in the middle - the line ending at place, unless it is a
// line read again.
func (c *deferredCounter) count(at time.Time, catchingUp bool, place Place, message bool) {
	if catchingUp && c.countedAlready(at, place) {
		return
	}
	if at.After(c.last) {
		c.last = at
	}
	c.counted = place
	if err := c.st.AddSenderCheckDeferred(at, 1, place.String(), message); err != nil {
		log.Printf("log-tailer: sender check statistics: %v", err)
	}
}

// countedAlready says whether a warning read while catching up was counted
// before: by its place when it is in the file of the last one counted, by its
// time otherwise.
func (c *deferredCounter) countedAlready(at time.Time, place Place) bool {
	if place.FP != "" && place.FP == c.counted.FP {
		return place.End <= c.counted.End
	}
	return !at.After(c.last)
}
