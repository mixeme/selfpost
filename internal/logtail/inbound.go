package logtail

import (
	"context"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

// InboundStore is what the tailer needs of the inbound antispam journal:
// writing the rows for refused mail, knowing which domains are inbound, and
// pruning. *store.Store satisfies it.
type InboundStore interface {
	InsertInboundSpam(e store.InboundSpamEntry) error
	IsInboundDomain(name string) (bool, error)
	PruneInboundSpam(cutoff time.Time, keep int) (int64, error)
}

// Inbound turns on the inbound half of the tailer: journal rows for mail that
// port 25 refused, and the journal's retention. Run takes nil when the inbound
// relay is off.
type Inbound struct {
	Store     InboundStore
	Retention RetentionDays
}

// The inbound journal milter writes a row for mail that was accepted; mail
// that was refused never reaches it, and what is known about it is one line
// of mail.log:
//
//	postfix/inbound/smtpd[41]: NOQUEUE: reject: RCPT from mx.example.net[203.0.113.9]: 550 5.1.1 <nobody@in.example.com>: Recipient address rejected: User unknown in relay recipient table; from=<a@example.net> to=<nobody@in.example.com> proto=ESMTP helo=<mx.example.net>
//	postfix/cleanup[52]: 4A1B2C3D: milter-reject: END-OF-MESSAGE from mx.example.net[203.0.113.9]: 5.7.1 Spam message rejected; from=<a@example.net> to=<b@in.example.com> proto=ESMTP helo=<mx.example.net>
//
// The first is Postfix's own policy, written by the port-25 smtpd, which
// build/postfix-config.sh names postfix/inbound so that its lines are told
// from those of the submission ports. The second is the filter's verdict at
// end-of-message, which cleanup(8) writes under one name for every listener -
// so it counts only for a queue id the port-25 smtpd was seen to open
// (inboundClientRe).
//
// Both are matched from the head of the line, after the timestamp and host
// name: the tail of such a line is the sender's own words (HELO name,
// addresses), and a pattern that could start anywhere would let him write
// journal rows.
var (
	inboundRejectRe = regexp.MustCompile(`^\S+ postfix/(inbound/smtpd|cleanup)\[\d+\]: ([0-9A-Za-z]+): (reject|milter-reject|milter-hold|milter-discard): \S+ from [^\[\s]*\[([0-9A-Fa-f:.]+)\]: (.*); from=<([^>]*)> to=<([^>]*)> proto=\S+(?: helo=<[^>]*>)?$`)
	inboundClientRe = regexp.MustCompile(`^\S+ postfix/inbound/smtpd\[\d+\]: ([0-9A-Za-z]+): client=`)
)

// A row holds one line of reason; Postfix's reply text carries the far end's
// bytes and is cut and reduced to printable ASCII like any of them.
const inboundDetailMax = 255

// inboundQueueIDs is how many port-25 queue ids are remembered while waiting
// for cleanup's verdict line on them. A message is in that state for the
// seconds its DATA takes; the cap only bounds a flood.
const inboundQueueIDs = 4096

// inboundJournal is the per-run state of the inbound half.
type inboundJournal struct {
	st InboundStore
	// port25 holds the queue ids the port-25 smtpd opened and cleanup has not
	// reported on yet; order is their arrival, for dropping the oldest.
	port25 map[string]struct{}
	order  []string
}

func newInboundJournal(st InboundStore) *inboundJournal {
	return &inboundJournal{st: st, port25: make(map[string]struct{})}
}

// handle looks at one mail.log line and writes a journal row when it is a
// refusal of mail for an inbound domain. Errors are logged; the tailer goes on.
func (j *inboundJournal) handle(line string, now time.Time) {
	at, rest := lineTime(line, now)
	if m := inboundClientRe.FindStringSubmatch(rest); m != nil {
		j.remember(m[1])
		return
	}
	m := inboundRejectRe.FindStringSubmatch(rest)
	if m == nil {
		return
	}
	program, queueID, kind, clientIP, reason, from, to := m[1], m[2], m[3], m[4], m[5], m[6], m[7]
	if program == "cleanup" {
		if _, ok := j.port25[queueID]; !ok {
			return // a submission-port message, or one opened before this run
		}
		delete(j.port25, queueID)
	}

	// Only mail for an inbound domain is the relay's. "Relay access denied"
	// for a stranger's domain is every scanner on the internet, and a line
	// with no recipient (size limit at MAIL) belongs to no domain.
	domain := ""
	if i := strings.LastIndexByte(to, '@'); i >= 0 {
		domain = strings.ToLower(to[i+1:])
	}
	if domain == "" {
		return
	}
	ok, err := j.st.IsInboundDomain(domain)
	if err != nil {
		log.Printf("log-tailer: inbound journal: look up %s: %v", domain, err)
		return
	}
	if !ok {
		return
	}

	e := store.InboundSpamEntry{
		InboundDomain: domain,
		ClientIP:      clientIP,
		From:          from,
		To:            to,
		Decision:      store.SpamDecisionReject,
		Engine:        store.SpamEngineRspamd,
		Detail:        printable(reason, inboundDetailMax),
		At:            at,
	}
	switch {
	case kind == "reject":
		e.Engine = store.SpamEnginePostfix
	case kind == "milter-hold":
		e.Decision = store.SpamDecisionQuarantine
	}
	// "450 4.7.1 …" from Postfix, "4.7.1 …" from a milter: either way a reply
	// that starts with 4 asks the sender to come back.
	if e.Decision == store.SpamDecisionReject && strings.HasPrefix(reason, "4") {
		e.Decision = store.SpamDecisionTempfail
	}
	if err := j.st.InsertInboundSpam(e); err != nil {
		log.Printf("log-tailer: inbound journal: record %s -> %s: %v", from, to, err)
	}
}

func (j *inboundJournal) remember(queueID string) {
	if _, ok := j.port25[queueID]; ok {
		return
	}
	if len(j.order) >= inboundQueueIDs {
		delete(j.port25, j.order[0])
		j.order = j.order[1:]
	}
	j.port25[queueID] = struct{}{}
	j.order = append(j.order, queueID)
}

// printable cuts s to max bytes and replaces everything outside printable
// ASCII.
func printable(s string, max int) string {
	if len(s) > max {
		s = s[:max]
	}
	b := []byte(s)
	for i, c := range b {
		if c < 0x20 || c > 0x7e {
			b[i] = '?'
		}
	}
	return string(b)
}

// lineTime reads the time at the head of a mail.log line and returns it with
// the rest of the line. A refusal is dated by its line and not by when the
// tailer got to it: after the panel was down the two are hours apart. The
// syslog format carries neither year nor zone - the year is taken from now
// (a stamp that would then lie in the future is last year's), the zone is the
// container's, which is what postlogd wrote it in. A line with no readable
// stamp is dated now.
func lineTime(line string, now time.Time) (time.Time, string) {
	if m := isoStampRe.FindStringSubmatch(line); m != nil {
		rest := strings.TrimSpace(line[len(m[0]):])
		if t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(m[0])); err == nil {
			return t.UTC(), rest
		}
		return now, rest
	}
	if m := syslogStampRe.FindStringSubmatch(line); m != nil {
		rest := strings.TrimSpace(line[len(m[0]):])
		stamp := strings.Join(strings.Fields(m[1]), " ")
		t, err := time.ParseInLocation("Jan 2 15:04:05", stamp, time.Local)
		if err != nil {
			return now, rest
		}
		t = t.AddDate(now.In(time.Local).Year(), 0, 0)
		if t.After(now.Add(24 * time.Hour)) {
			t = t.AddDate(-1, 0, 0)
		}
		return t.UTC(), rest
	}
	return now, line
}

// The journal keeps at most this many rows whatever their age: refusals are
// written at the rate strangers knock on port 25.
const inboundSpamLogMaxRows = 10000

// inboundRetentionLoop prunes the inbound journal at once and then on the
// cadence of the send log.
func inboundRetentionLoop(ctx context.Context, in *Inbound) {
	prune := func() {
		days := in.Retention()
		if days <= 0 {
			days = store.InboundSpamLogRetentionDaysDefault
		}
		n, err := in.Store.PruneInboundSpam(time.Now().UTC().AddDate(0, 0, -days), inboundSpamLogMaxRows)
		if err != nil {
			log.Printf("log-tailer: inbound journal prune: %v", err)
			return
		}
		if n > 0 {
			log.Printf("log-tailer: pruned %d inbound journal rows (older than %d days or beyond %d rows)", n, days, inboundSpamLogMaxRows)
		}
	}

	prune()
	t := time.NewTicker(retentionInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			prune()
		}
	}
}
