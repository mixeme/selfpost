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
// Postfix says it in lines of one smtpd process - why, then what:
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
// The reject lines do not name the milter, and the same words are written
// when OpenDKIM does not answer or when the journal milter refuses under a
// level-2 limit. So a process is remembered from the warning that names this
// socket, and counted at its next reject line.

// SenderCheckStore is where the count goes. *store.Store satisfies it.
type SenderCheckStore interface {
	AddSenderCheckDeferred(at time.Time, n int64) error
}

// SenderCheck turns the counting on: the store and the socket Postfix was
// given for the check (SENDER_CHECK_SOCKET), as a path.
type SenderCheck struct {
	Store  SenderCheckStore
	Socket string
}

var smtpdLineRe = regexp.MustCompile(`^\S+ postfix/(?:[a-z0-9-]+/)?smtpd\[(\d+)\]: (.*)$`)

const (
	milterUnavailable = "Service unavailable - try again later"
	// senderCheckPids bounds the processes remembered; smtpd processes are a
	// few dozen, and a log that names more is not kept track of.
	senderCheckPids = 1024
)

// deferredCounter is the per-run state of the count.
type deferredCounter struct {
	st     SenderCheckStore
	needle string          // the socket as Postfix names it in a warning
	failed map[string]bool // smtpd pid -> the check did not answer its current client
}

func newDeferredCounter(sc *SenderCheck) *deferredCounter {
	return &deferredCounter{st: sc.Store, needle: "unix:" + sc.Socket, failed: make(map[string]bool)}
}

// handle looks at one mail.log line.
func (c *deferredCounter) handle(line string, now time.Time) {
	at, rest := lineTime(line, now)
	m := smtpdLineRe.FindStringSubmatch(rest)
	if m == nil {
		return
	}
	pid, text := m[1], m[2]
	switch {
	case strings.HasPrefix(text, "connect from "):
		delete(c.failed, pid)
	case strings.HasPrefix(text, "warning: ") && strings.Contains(text, c.needle) && strings.Contains(strings.ToLower(text), "milter"):
		// "connect to Milter service <socket>: …" when nobody listens, and
		// "milter <socket>: can't read … reply packet header: …" when the
		// check stopped answering in the middle of a message.
		if len(c.failed) >= senderCheckPids {
			c.failed = make(map[string]bool)
		}
		c.failed[pid] = true
	case c.failed[pid] && strings.Contains(text, "milter-reject: ") && strings.Contains(text, milterUnavailable):
		// Once for the connection, however many commands the client tries.
		delete(c.failed, pid)
		if err := c.st.AddSenderCheckDeferred(at, 1); err != nil {
			log.Printf("log-tailer: sender check statistics: %v", err)
		}
	}
}
