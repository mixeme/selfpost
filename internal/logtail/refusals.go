package logtail

import (
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/store"
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
	RecoverRefusal(e store.SendLogEntry, at time.Time) (bool, error)
}

// fromRefusalRe is cleanup's line about a message the sender check refused
// for its From: line, as the stand writes it:
//
//	postfix/cleanup[1339]: E543D3C75DA0: milter-reject: END-OF-MESSAGE from unknown[10.77.0.1]: 5.7.1 From: director@other.e2e.test - not an address this application sends as; from=<alerts@sender.e2e.test> to=<rcpt@sink.e2e.test> proto=ESMTP helo=<e2e-client.e2e.test>
//
// The From: line as the check printed it runs to the last " - " before the
// semicolon; the reason after it holds neither.
var fromRefusalRe = regexp.MustCompile(`^\S+ postfix/(?:[a-z0-9-]+/)?cleanup\[\d+\]: ([0-9A-Za-z]+): milter-reject: END-OF-MESSAGE from \S+: 5\.7\.1 From: (.*) - ([^;]*); from=<([^>]*)> to=<([^>]*)>`)

// clientLoginRe is smtpd's line naming the login a queue id was submitted
// under: "<id>: client=host[addr], sasl_method=PLAIN, sasl_username=<login>".
var clientLoginRe = regexp.MustCompile(`^\S+ postfix/(?:([a-z0-9-]+)/)?smtpd\[\d+\]: ([0-9A-Za-z]+): client=.*?, sasl_username=(\S+)`)

// refusalLogins bounds the queue ids whose login is remembered.
const refusalLogins = 4096

type pendingRefusal struct {
	login, from, line string
	at, until         time.Time
}

// pendingRecovery is a From: refusal whose row is looked for once the panel
// has had the time to write it.
type pendingRecovery struct {
	e         store.SendLogEntry
	at, until time.Time
}

// refusalLines puts each refusal line on its row, and keeps for a while the
// lines whose row is not there yet. It also writes, from cleanup's line, the
// row of a From: refusal the panel did not get to write.
type refusalLines struct {
	st        RefusalStore
	pending   []pendingRefusal
	recover   []pendingRecovery
	logins    map[string]string // queue id -> login, from smtpd's client= line
	loginKeys []string
}

// handle looks at one mail.log line.
func (r *refusalLines) handle(line string, now time.Time) {
	at, rest := lineTime(line, now)
	if m := clientLoginRe.FindStringSubmatch(rest); m != nil && m[1] != "inbound" {
		r.remember(m[2], m[3])
		return
	}
	if m := fromRefusalRe.FindStringSubmatch(rest); m != nil {
		r.recover = append(r.recover, pendingRecovery{
			e: store.SendLogEntry{
				QueueID: m[1], AppLogin: r.logins[m[1]], From: m[4], To: m[5], Domain: domainOf(m[4]),
				Reason: store.ReasonFrom, HeaderFrom: m[2],
			},
			at: at, until: now.Add(refusalLineWait),
		})
		return
	}
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
// enough; and writes, once their wait is over, the rows of From: refusals the
// panel did not write.
func (r *refusalLines) retry(now time.Time) {
	kept := r.pending[:0]
	for _, p := range r.pending {
		if r.attach(p) || now.After(p.until) {
			continue
		}
		kept = append(kept, p)
	}
	r.pending = kept

	waiting := r.recover[:0]
	for _, p := range r.recover {
		if !now.After(p.until) {
			waiting = append(waiting, p)
			continue
		}
		wrote, err := r.st.RecoverRefusal(p.e, p.at)
		if err != nil {
			log.Printf("log-tailer: recover refusal: %v", err)
		} else if wrote {
			log.Printf("log-tailer: wrote the refusal of %s (%s) from mail.log: the panel had not written it", p.e.QueueID, p.e.From)
		}
	}
	r.recover = waiting
}

// remember keeps the login a queue id was submitted under, the oldest going
// first beyond refusalLogins.
func (r *refusalLines) remember(queueID, login string) {
	if r.logins == nil {
		r.logins = make(map[string]string)
	}
	if _, ok := r.logins[queueID]; !ok {
		r.loginKeys = append(r.loginKeys, queueID)
		if len(r.loginKeys) > refusalLogins {
			delete(r.logins, r.loginKeys[0])
			r.loginKeys = r.loginKeys[1:]
		}
	}
	r.logins[queueID] = login
}

// domainOf is the domain of an address, lower-cased; "" without one.
func domainOf(addr string) string {
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		return strings.ToLower(addr[i+1:])
	}
	return ""
}

func (r *refusalLines) attach(p pendingRefusal) bool {
	ok, err := r.st.AttachRefusalLine(p.login, p.from, p.at, p.line)
	if err != nil {
		log.Printf("log-tailer: refusal line: %v", err)
		return true // not retried: the store said no for its own reason
	}
	return ok
}
