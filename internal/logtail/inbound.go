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
// A message the filter holds (milter-hold) is not read here: it goes on to the
// inbound journal milter, which writes its row with the subject.
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
//
// For the same reason the reply is the shortest text before "; from=<", and
// the HELO name - the last thing on the line, which Postfix logs as the client
// wrote it, spaces and brackets included - is whatever is left. Taking the
// longest reply instead would let a HELO name that itself spells
// "; from=<…> to=<…> proto=…" supply the sender, the recipient and the reply
// of the row (security review, 2026-10-12).
var (
	inboundRejectRe = regexp.MustCompile(`^\S+ postfix/(inbound/smtpd|cleanup)\[\d+\]: ([0-9A-Za-z]+): (reject|milter-reject|milter-discard): \S+ from [^\[\s]*\[([0-9A-Fa-f:.]+)\]: (.*?); from=<([^>]*)> to=<([^>]*)> proto=\S+(?: helo=<.*>)?$`)
	inboundClientRe = regexp.MustCompile(`^\S+ postfix/inbound/smtpd\[\d+\]: ([0-9A-Za-z]+): client=`)
	// A queue id stops being port 25's when the message is queued (qmgr takes
	// it: the filter let it through) or when another listener opens a message
	// under the same id - Postfix reuses ids.
	inboundForgetRe = regexp.MustCompile(`^\S+ postfix/(?:qmgr\[\d+\]: ([0-9A-Za-z]+): from=|(?:[a-z0-9-]+/)?smtpd\[\d+\]: ([0-9A-Za-z]+): client=)`)
)

// A row holds one line of reason; Postfix's reply text carries the far end's
// bytes and is cut and reduced to printable ASCII like any of them.
const inboundDetailMax = 255

// inboundQueueIDs is how many port-25 queue ids are remembered while waiting
// for cleanup's verdict line on them. An id is forgotten when the verdict
// comes, when the message is queued, or when another listener reuses it; the
// cap bounds what is left - messages the client abandoned - and a flood.
const inboundQueueIDs = 4096

// inboundPruneEvery is how many rows the tailer writes before it has the
// journal cut back to its row cap, without waiting for the six-hourly prune.
// Refusals are written at the rate strangers send RCPT TO, which no limit of
// Postfix's counts, so between two prunes the journal would otherwise grow as
// fast as someone cared to make it.
const inboundPruneEvery = 500

// inboundJournal is the per-run state of the inbound half.
type inboundJournal struct {
	st InboundStore
	// port25 holds the queue ids the port-25 smtpd opened and nobody has
	// accounted for yet, each with the number of its arrival; order is those
	// arrivals, for dropping the oldest. An id that was forgotten and opened
	// again is in order twice: the number tells the stale entry from the live
	// one when the stale one comes up for dropping.
	port25 map[string]uint64
	order  []queued
	seq    uint64
	// written counts the rows since the journal was last cut to its cap.
	written int
}

type queued struct {
	id  string
	seq uint64
}

func newInboundJournal(st InboundStore) *inboundJournal {
	return &inboundJournal{st: st, port25: make(map[string]uint64)}
}

// handle looks at one mail.log line and writes a journal row when it is a
// refusal of mail for an inbound domain. Errors are logged; the tailer goes on.
func (j *inboundJournal) handle(line string, now time.Time) {
	at, rest := lineTime(line, now)
	if m := inboundClientRe.FindStringSubmatch(rest); m != nil {
		j.remember(m[1])
		return
	}
	if m := inboundForgetRe.FindStringSubmatch(rest); m != nil {
		delete(j.port25, m[1]+m[2])
		return
	}
	m := inboundRejectRe.FindStringSubmatch(rest)
	if m == nil {
		return
	}
	program, queueID, kind, clientIP, reason, from, to := m[1], m[2], m[3], m[4], m[5], m[6], m[7]
	// The sender's address is his own text too, and with a quoted local part it
	// may hold "> to=<…> proto=…" - which would end the sender early and supply
	// the recipient. Such a line shows it: the sender is left with an unclosed
	// quote. An address with a quote, a bracket or a space in it is not one the
	// journal keeps.
	if strings.ContainsAny(from, "\"<> ") || strings.ContainsAny(to, "\"<> ") {
		return
	}
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
	if kind == "reject" {
		e.Engine = store.SpamEnginePostfix
	}
	// "450 4.7.1 …" from Postfix, "4.7.1 …" from a milter: either way a reply
	// that starts with 4 asks the sender to come back.
	if e.Decision == store.SpamDecisionReject && strings.HasPrefix(reason, "4") {
		e.Decision = store.SpamDecisionTempfail
	}
	if err := j.st.InsertInboundSpam(e); err != nil {
		log.Printf("log-tailer: inbound journal: record %s -> %s: %v", from, to, err)
		return
	}
	if j.written++; j.written >= inboundPruneEvery {
		j.written = 0
		// The zero time removes nothing by age: this is the cap alone.
		if _, err := j.st.PruneInboundSpam(time.Time{}, inboundSpamLogMaxRows); err != nil {
			log.Printf("log-tailer: inbound journal: cut to %d rows: %v", inboundSpamLogMaxRows, err)
		}
	}
}

func (j *inboundJournal) remember(queueID string) {
	if _, ok := j.port25[queueID]; ok {
		return
	}
	for len(j.order) >= inboundQueueIDs {
		oldest := j.order[0]
		j.order = j.order[1:]
		if j.port25[oldest.id] == oldest.seq {
			delete(j.port25, oldest.id)
		}
	}
	j.seq++
	j.port25[queueID] = j.seq
	j.order = append(j.order, queued{queueID, j.seq})
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
