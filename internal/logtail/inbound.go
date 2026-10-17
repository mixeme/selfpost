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
// writing the rows for refused mail, knowing which domains are inbound,
// putting the upstream's answers on the rows of accepted mail, and pruning.
// *store.Store satisfies it.
type InboundStore interface {
	InsertInboundSpam(e store.InboundSpamEntry) error
	InsertInboundSpamOnce(e store.InboundSpamEntry) (bool, error)
	IsInboundDomain(name string) (bool, error)
	PruneInboundSpam(cutoff time.Time, keep int) (int64, error)
	UpdateInboundUpstream(queueID, recipient, status, relay, reply string, at time.Time) (int64, error)
	ExpireInboundUpstream(queueID string, at time.Time) (int64, error)
	ForgetInboundQueueID(queueID string) error
}

// upstream puts a delivery line's result on the journal rows of a message
// accepted on port 25 (inbound-upstream-result). The tailer offers it every
// delivery line no send-log row took: the rows of that queue id and recipient
// take it, and a line of any other mail - the panel's own, a bounce Postfix
// sends back to a stranger - matches none. relay "none" is a line written
// without a session (Postfix 3.10.13, src/smtp/smtp_trouble.c:273, 421: the
// host is then only in the reply), kept as "". The reply is the upstream's
// text, or Postfix's own about a host it could not reach, cut and made
// printable like a refusal's reason. One update per attempt, never a row of
// its own: the journal's caps stand.
func (j *inboundJournal) upstream(line string, now time.Time, queueID, recipient, status string) {
	at, rest := lineTime(line, now)
	relay, reply := deliveryReply(rest)
	if relay == "none" {
		relay = ""
	}
	if _, err := j.st.UpdateInboundUpstream(queueID, recipient, status, printable(relay, inboundDetailMax),
		printable(reply, store.UpstreamReplyMax), at); err != nil {
		log.Printf("log-tailer: inbound journal: upstream %s/%s -> %s: %v", queueID, recipient, status, err)
	}
}

// expired makes the rows of a port-25 message Postfix gave up on bounced.
func (j *inboundJournal) expired(line string, now time.Time, queueID string) {
	at, _ := lineTime(line, now)
	if _, err := j.st.ExpireInboundUpstream(queueID, at); err != nil {
		log.Printf("log-tailer: inbound journal: expire %s: %v", queueID, err)
	}
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
// Postfix 3.10.13: smtpd's refusal is src/smtpd/smtpd_check.c, log_whatsup():
// "%s: %s: %s from %s: %s;" then " from=<..>", " to=<..>", " proto=",
// " helo=<..>", then - new in 3.10 (RELEASE_NOTES, [Incompat 20241023]) -
// " sasl_method=%s", " sasl_username=%s" and " sasl_sender=%s" for a session
// that has them, nothing after; a milter's at smtpd is src/smtpd/smtpd.c,
// check_milter_reply(), whose smtpd_whatsup() appends " sasl_username=<..>"
// only to an authenticated session. SASL is off on port 25
// (build/postfix-config.sh, smtpd_sasl_auth_enable=no), so neither tail is
// written there; the pattern takes the 3.10 one all the same. "name[addr]" is
// state->namaddr, the address without "IPv6:" (smtpd.h,
// SMTPD_BUILD_NAMADDRPORT). cleanup's is src/cleanup/cleanup_milter.c,
// cleanup_milter_apply(); smtpd hands it to the one cleanup service
// (cleanup_service_name, default "cleanup"; src/smtpd/smtpd.c,
// mail_open_stream()). Its to= is the last recipient cleanup saw, not every
// one (src/cleanup/cleanup_addr.c, cleanup_addr_recipient() overwrites
// state->recip) - assumptions A-15.
//
// Both are matched from the head of the line, after the timestamp and host
// name: the tail of such a line is the sender's own words (HELO name,
// addresses), and a pattern that could start anywhere would let him write
// journal rows.
//
// The reply is the client's text too: a refusal quotes the refused
// recipient, and Postfix quotes it in internal form - quotes removed, "; < >
// =" kept (extract_addr(), tok822_internalize(); smtpd_check_reject() applies
// only printable()) - so a stranger's RCPT TO:<"; from=<x> to=<y> ..."@d>
// spells a whole "; from=<…> to=<…> proto=…" inside the reply. The shortest
// reply ended there, and the row's sender, recipient and domain were the
// stranger's (security audit 2026-10-17, M2, proven on the test server).
// What the client cannot spell is the end of the line: the HELO name is
// logged with a space and ` <>()\";@` turned into "?" (src/smtpd/smtpd.c,
// helo_cmd(), NEUTER_CHARACTERS), and an address that holds "; from=<" or a
// ">" needs a quoted local part, whose quote then stands inside what the
// pattern would take for the sender or the recipient - which the filter
// below refuses. So the reply is the longest text before a "; from=<…>
// to=<…> proto=…" that runs to the end of the line, the HELO name holds none
// of the neutered characters, and each SASL field (never written on port 25)
// is one word. A line it does not fit leaves no row.
var (
	inboundRejectRe = regexp.MustCompile(`^\S+ postfix/(inbound/smtpd|cleanup)\[\d+\]: ([0-9A-Za-z]+): (reject|milter-reject|milter-discard): \S+ from [^\[\s]*\[([0-9A-Fa-f:.]+)\]: (.*); from=<([^>]*)> to=<([^>]*)> proto=\S+(?: helo=<[^ <>()\\";@]*>)?(?: sasl_method=\S*)?(?: sasl_username=\S*)?(?: sasl_sender=\S*)?$`)
	inboundClientRe = regexp.MustCompile(`^\S+ postfix/inbound/smtpd\[\d+\]: ([0-9A-Za-z]+): client=`)
	// A queue id stops being port 25's when the message is queued (qmgr takes
	// it: the filter let it through), when cleanup removes the queue file of
	// a message it did not queue, or when another listener opens a message
	// under the same id - Postfix reuses ids. Postfix 3.10.13,
	// src/qmgr/qmgr_message.c, qmgr_message_read() calls opened()
	// (src/global/opened.c: "%s: from=<%s>, size=%ld, nrcpt=%d") for a queue
	// file cleanup has committed, each time it enters the active queue;
	// src/cleanup/cleanup_api.c, cleanup_flush(): "%s: removed (%s)", with
	// "canceled" or "discarded", once the queue file of a refused, discarded
	// or unfinished message is gone (new in 3.10, HISTORY 20241112) - after
	// the verdict line, which cleanup_milter_inspect() writes earlier in
	// cleanup_flush(). Without it, the id of a message whose client went away
	// in DATA stayed until inboundQueueIDs pushed it out.
	inboundForgetRe = regexp.MustCompile(`^\S+ postfix/(?:qmgr\[\d+\]: ([0-9A-Za-z]+): from=|(?:[a-z0-9-]+/)?smtpd\[\d+\]: ([0-9A-Za-z]+): client=|cleanup\[\d+\]: ([0-9A-Za-z]+): removed \()`)
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
// Refusals are written at the rate strangers send RCPT TO. Postfix's one
// limit on that, smtpd_client_recipient_rate_limit, is per client address and
// off by default (postconf(5), Postfix 3.10.13) and not set here, so between
// two prunes the journal would otherwise grow as fast as someone cared to
// make it.
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
//
// catchingUp says the line is one of those the tailer reads when it starts,
// from where it last saved its place to the end of the file. It saves its
// place every few seconds, so after an unclean stop some of those lines were
// read before; a row is then written only if it is not there already. Lines
// read as they are written are always new, and two identical refusals in one
// second stay two rows.
func (j *inboundJournal) handle(line string, now time.Time, catchingUp bool) {
	at, rest := lineTime(line, now)
	if m := inboundClientRe.FindStringSubmatch(rest); m != nil {
		j.remember(m[1])
		return
	}
	if m := inboundForgetRe.FindStringSubmatch(rest); m != nil {
		delete(j.port25, m[1]+m[2]+m[3])
		// A queue file cleanup removed was never queued: rows the milter wrote
		// for it will get no delivery line (store.ForgetInboundQueueID).
		if m[3] != "" {
			if err := j.st.ForgetInboundQueueID(m[3]); err != nil {
				log.Printf("log-tailer: inbound journal: forget %s: %v", m[3], err)
			}
		}
		return
	}
	m := inboundRejectRe.FindStringSubmatch(rest)
	if m == nil {
		return
	}
	program, queueID, kind, clientIP, reason, from, to := m[1], m[2], m[3], m[4], m[5], m[6], m[7]
	// The sender's address is his own text too, and with a quoted local part it
	// may hold "> to=<…> proto=…" - which would end the sender early and supply
	// the recipient (Postfix 3.10.13 logs addresses in external form, the local
	// part quoted when it holds spaces or specials: postconf(5)
	// info_log_address_format, src/global/info_log_addr_form.c). Such a line
	// shows it: the sender is left with an unclosed quote. An address with a
	// quote, a bracket or a space in it is not one the journal keeps.
	if strings.ContainsAny(from, "\"<> ") || strings.ContainsAny(to, "\"<> ") {
		return
	}
	// A reply that still holds "; from=<" is a line with two tails, one of
	// them written by the client - a refused recipient quoted in the reply
	// (audit 2026-10-17, M2) or a HELO name Postfix failed to neuter. Which
	// is Postfix's is then a guess the journal does not make.
	if strings.Contains(reason, "; from=<") {
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
	// that starts with 4 asks the sender to come back. Postfix 3.10.13:
	// smtpd_check_reject() logs smtpd's whole "%d %s ..." reply through
	// log_whatsup(); cleanup_milter_apply() logs a
	// milter's from its fifth byte (text = resp + 4), so the enhanced code
	// leads - when the milter sent one, which src/milter/milter8.c accepts
	// either way for SMFIR_REPLYCODE (assumptions A-18).
	if e.Decision == store.SpamDecisionReject && strings.HasPrefix(reason, "4") {
		e.Decision = store.SpamDecisionTempfail
	}
	if catchingUp {
		if wrote, err := j.st.InsertInboundSpamOnce(e); err != nil {
			log.Printf("log-tailer: inbound journal: record %s -> %s: %v", from, to, err)
			return
		} else if !wrote {
			return // read before the last stop
		}
	} else if err := j.st.InsertInboundSpam(e); err != nil {
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
// container's. Postfix 3.10.13, src/util/msg_logger.c, msg_logger_print(): the
// stamp is made by the process that logs, with localtime(), and postlogd
// writes it unchanged (src/postlogd/postlogd.c, postlogd_service()); that
// process has the container's zone (TZ is in import_environment,
// src/global/mail_params.h). A line with no readable stamp is dated now.
func lineTime(line string, now time.Time) (time.Time, string) {
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
