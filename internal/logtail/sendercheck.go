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
// answer and when the journal milter refuses under a level-2 limit, and for a
// check that stops answering in the middle of a message they are written by
// cleanup, another process, under a queue id. The warning for that case is
// "milter unix:<socket>: can't read … reply packet header" (Postfix's
// milter8.c; not seen on the stand).

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
}

func newDeferredCounter(sc *SenderCheck) *deferredCounter {
	return &deferredCounter{
		st:      sc.Store,
		connect: "warning: connect to Milter service unix:" + sc.Socket + ":",
		broken:  "warning: milter unix:" + sc.Socket + ":",
		failed:  make(map[string]bool),
	}
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
		if err := c.st.AddSenderCheckDeferred(at, 1); err != nil {
			log.Printf("log-tailer: sender check statistics: %v", err)
		}
	}
}
