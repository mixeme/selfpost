package logtail

import (
	"log"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

// Postfix's line about a message the journal milter refused at MAIL FROM - a
// level-2 limit, a client address off the application's list - is put on the
// row the milter wrote, so the message's page shows it (audit 2026-10-12,
// stage 2b). The refusal has no queue id; it is matched to its row by client
// address, login, sender and time:
//
//	postfix/smtpd[1149]: NOQUEUE: milter-reject: MAIL from unknown[10.77.0.1]: 451 4.7.1 Service unavailable - try again later; from=<billing@sender.e2e.test> proto=ESMTP helo=<e2e-client.e2e.test> sasl_username=<l2app>
//
// Postfix 3.10.13, src/smtpd/smtpd.c, check_milter_reply(): "%s: %s: %s from
// %s: %s;%s" with "NOQUEUE" before the queue file is opened, and
// smtpd_whatsup() for the rest: " from=<..>", " to=<..>", " proto=..",
// " helo=<..>", then " sasl_username=<..>" when the session authenticated
// (assumptions A-20, A-21). smtpd's own refusals are another function,
// src/smtpd/smtpd_check.c, log_whatsup(), whose line says "reject:", not
// "milter-reject:", and since 3.10 ends in " sasl_method=%s sasl_username=%s"
// without the brackets; it is not one of these. The login is read at the end
// of the line, where Postfix puts it last; a line without one is no
// application's refusal - the journal milter refuses at MAIL FROM only an
// authenticated session (milter/ratelimit.go). The HELO name before it is the client's text and can
// end in what looks like the field, but only in a session that has no login,
// and so no refused row to take the line; a row must also be of the line's
// client address and sender, within minutes.
//
// Port 25's smtpd (postfix/inbound/smtpd) is a stranger's and has no such row.
var refusalLineRe = regexp.MustCompile(`^\S+ postfix/(?:([a-z0-9-]+)/)?smtpd\[\d+\]: NOQUEUE: milter-reject: MAIL from [^\[\s]*\[([^\]]+)\]: [45]\d\d .*?; from=<([^>]*)>(?:.* sasl_username=<([^<>]*)>|.*)$`)

// refusalLineWait is how long a line waits for its row: the milter writes the
// row when it refuses, which can be read a moment after the line.
const refusalLineWait = 30 * time.Second

// RefusalStore is where the lines go. *store.Store satisfies it.
type RefusalStore interface {
	AttachRefusalLine(clientIP, login, from string, at time.Time, line string) (bool, error)
	RecoverRefusal(e store.SendLogEntry, at time.Time) (bool, error)
}

// fromRefusalRe is cleanup's line about a message the sender check refused
// for its From: line, as the stand writes it (e2e stand, 2026-10-12; the
// fixture in sendercheck_test.go):
//
//	postfix/cleanup[1339]: E543D3C75DA0: milter-reject: END-OF-MESSAGE from unknown[10.77.0.1]: 5.7.1 From: director@other.e2e.test - not an address this application sends as; from=<alerts@sender.e2e.test> to=<rcpt@sink.e2e.test> proto=ESMTP helo=<e2e-client.e2e.test>
//
// The From: line as the check printed it runs to the last " - " before the
// first semicolon; the reason after it holds neither, and the check prints no
// ";" of the client's (sender.go printable), so that semicolon is Postfix's
// and what follows it - the envelope - is Postfix's own text. Postfix 3.10.13,
// src/cleanup/cleanup_milter.c, cleanup_milter_apply(): "%s: %s: %s from
// %s[%s]: %s;" with the reply from its fifth byte (the "550 " dropped), then
// " from=<..>", " to=<..>" - the last recipient only (assumptions A-15).
var fromRefusalRe = regexp.MustCompile(`^\S+ postfix/(?:[a-z0-9-]+/)?cleanup\[\d+\]: ([0-9A-Za-z]+): milter-reject: END-OF-MESSAGE from \S+: 5\.7\.1 From: ([^;]*) - ([^;]*); from=<([^>]*)> to=<([^>]*)>`)

// clientLoginRe is smtpd's line naming the login a queue id was submitted
// under: "<id>: client=host[addr], sasl_method=PLAIN, sasl_username=<login>".
// Postfix 3.10.13, src/smtpd/smtpd.c, mail_open_stream(): ", sasl_sender=.."
// follows when the client gave AUTH= on MAIL FROM, then ", orig_queue_id=.."
// and ", orig_client=.." for XFORWARD; the login ends at the comma (a login
// holds none - internal/postfix assertMapSafe; assumptions A-23).
var clientLoginRe = regexp.MustCompile(`^\S+ postfix/(?:([a-z0-9-]+)/)?smtpd\[\d+\]: ([0-9A-Za-z]+): client=.*?, sasl_username=([^,\s]+)`)

// refusalLogins bounds the queue ids whose login is remembered.
const refusalLogins = 4096

type pendingRefusal struct {
	clientIP, login, from, line string
	at, until                   time.Time
	start                       Place // where the line starts in the log
}

// pendingRecovery is a From: refusal whose row is looked for once the panel
// has had the time to write it.
type pendingRecovery struct {
	e         store.SendLogEntry
	at, until time.Time
	start     Place // where its client= line - or the refusal - starts
}

// loginLine is the login a queue id was submitted under and where smtpd's
// line saying so starts.
type loginLine struct {
	login string
	start Place
}

// refusalLines puts each refusal line on its row, and keeps for a while the
// lines whose row is not there yet. It also writes, from cleanup's line, the
// row of a From: refusal the panel did not get to write.
type refusalLines struct {
	st        RefusalStore
	pending   []pendingRefusal
	recover   []pendingRecovery
	logins    map[string]loginLine // queue id -> login, from smtpd's client= line
	loginKeys []string
}

// oldest is the earliest place in the file fp a waiting item starts at: the
// tracker saves no offset past it, so a stop before the wait is over reads the
// lines again - the client= line of a refusal to recover among them.
func (r *refusalLines) oldest(fp string) (Place, bool) {
	var p Place
	found := false
	take := func(q Place) {
		if q.FP == fp && (!found || q.End < p.End) {
			p, found = q, true
		}
	}
	for _, x := range r.pending {
		take(x.start)
	}
	for _, x := range r.recover {
		take(x.start)
	}
	return p, found
}

// handle looks at one mail.log line, which starts at start.
func (r *refusalLines) handle(line string, now time.Time, start Place) {
	at, rest := lineTime(line, now)
	if m := clientLoginRe.FindStringSubmatch(rest); m != nil && m[1] != "inbound" {
		r.remember(m[2], loginLine{login: m[3], start: start})
		return
	}
	if m := fromRefusalRe.FindStringSubmatch(rest); m != nil {
		from := start
		login, ok := r.logins[m[1]]
		if ok && login.start.FP == start.FP && login.start.End < start.End {
			from = login.start
		}
		r.recover = append(r.recover, pendingRecovery{
			e: store.SendLogEntry{
				QueueID: m[1], AppLogin: login.login, From: m[4], To: m[5], Domain: domainOf(m[4]),
				Reason: store.ReasonFrom, HeaderFrom: m[2],
			},
			at: at, until: now.Add(refusalLineWait), start: from,
		})
		return
	}
	m := refusalLineRe.FindStringSubmatch(rest)
	if m == nil || m[1] == "inbound" || m[4] == "" {
		return
	}
	// The address in "name[addr]" is smtpd's state->addr, which has no
	// "IPv6:" (Postfix 3.10.13, src/smtpd/smtpd_peer.c, smtpd_peer_init();
	// assumptions A-24).
	ip := net.ParseIP(m[2])
	if ip == nil {
		return
	}
	p := pendingRefusal{clientIP: ip.String(), login: m[4], from: m[3], line: line, at: at, until: now.Add(refusalLineWait), start: start}
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
func (r *refusalLines) remember(queueID string, login loginLine) {
	if r.logins == nil {
		r.logins = make(map[string]loginLine)
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
	ok, err := r.st.AttachRefusalLine(p.clientIP, p.login, p.from, p.at, p.line)
	if err != nil {
		log.Printf("log-tailer: refusal line: %v", err)
		return true // not retried: the store said no for its own reason
	}
	return ok
}
