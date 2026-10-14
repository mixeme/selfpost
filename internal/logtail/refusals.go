package logtail

import (
	"log"
	"regexp"
	"time"
)

// Postfix's line about a message the journal milter refused at MAIL FROM - a
// level-2 limit, a client address off the application's list - is put on the
// row the milter wrote, so the message's page shows it (audit 2026-10-12,
// stage 2b). The refusal has no queue id; the line is matched to its row by
// sender, login and time. As the stand writes it:
//
//	postfix/smtpd[1149]: NOQUEUE: milter-reject: MAIL from unknown[10.77.0.1]: 451 4.7.1 Service unavailable - try again later; from=<billing@sender.e2e.test> proto=ESMTP helo=<e2e-client.e2e.test>
//
// Port 25's smtpd (postfix/inbound/smtpd) is a stranger's and has no such row.
var refusalLineRe = regexp.MustCompile(`^\S+ postfix/(?:([a-z0-9-]+)/)?smtpd\[\d+\]: NOQUEUE: milter-reject: MAIL from \S+: [45]\d\d .*?; from=<([^>]*)>(?:.*? sasl_username=<?([^>\s]*)>?)?`)

// refusalLineWait is how long a line waits for its row: the milter writes the
// row when it refuses, which can be read a moment after the line.
const refusalLineWait = 30 * time.Second

// RefusalStore is where the lines go. *store.Store satisfies it.
type RefusalStore interface {
	AttachRefusalLine(login, from string, at time.Time, line string) (bool, error)
}

type pendingRefusal struct {
	login, from, line string
	at, until         time.Time
}

// refusalLines puts each refusal line on its row, and keeps for a while the
// lines whose row is not there yet.
type refusalLines struct {
	st      RefusalStore
	pending []pendingRefusal
}

// handle looks at one mail.log line.
func (r *refusalLines) handle(line string, now time.Time) {
	at, rest := lineTime(line, now)
	m := refusalLineRe.FindStringSubmatch(rest)
	if m == nil || m[1] == "inbound" {
		return
	}
	p := pendingRefusal{login: m[3], from: m[2], line: line, at: at, until: now.Add(refusalLineWait)}
	if !r.attach(p) {
		r.pending = append(r.pending, p)
	}
}

// retry tries the waiting lines again and lets go of those that waited long
// enough.
func (r *refusalLines) retry(now time.Time) {
	kept := r.pending[:0]
	for _, p := range r.pending {
		if r.attach(p) || now.After(p.until) {
			continue
		}
		kept = append(kept, p)
	}
	r.pending = kept
}

func (r *refusalLines) attach(p pendingRefusal) bool {
	ok, err := r.st.AttachRefusalLine(p.login, p.from, p.at, p.line)
	if err != nil {
		log.Printf("log-tailer: refusal line: %v", err)
		return true // not retried: the store said no for its own reason
	}
	return ok
}
