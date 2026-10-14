package logtail

import (
	"log"
	"regexp"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

// A message's page shows what Postfix wrote about it. mail.log is rotated
// daily and fourteen files are kept, while a send-log row lives for the
// retention window, so the lines are kept in the database as the tailer reads
// them (architecture.md § Log tailer).
//
// queueLineRe finds the queue id at the head of a line, after the timestamp
// (already taken off by lineTime), the host name and "program[pid]:":
//
//	mail postfix/smtp[26]: 41E862C00D9E: to=<a@example.net>, relay=…, status=sent (250 OK)
//
// It is matched from the start of the line and nowhere else: the tail of a
// line is the far end's words - a reply, a HELO name - and a pattern that
// could start anywhere would let them file a line under a queue id of their
// choosing.
//
// What stands in that place is not always a queue id: "NOQUEUE:" and
// "warning:" stand there too. A queue id is hexadecimal in capitals, or, with
// enable_long_queue_ids, digits and letters without vowels; neither word is
// either.
//
// Postfix 3.7.11, src/global/mail_queue.c, mail_queue_enter(), with
// src/global/mail_queue.h MQID_*: a short id is "%05X" of the microseconds
// and "%lX" of the inode (6 to 21 characters); a long one is the seconds (6+)
// and microseconds (4) in src/global/safe_ultostr.c's vowel-less alphabet, 'z',
// then the inode (12 to 23 characters). Program tag and "[pid]: ":
// src/util/msg_logger.c, msg_logger_print().
var queueLineRe = regexp.MustCompile(`^\S+ ([^\s\[]+)\[\d+\]: ([0-9A-F]{6,24}|[0-9B-DF-HJ-NP-TV-Zb-df-hj-np-tv-z]{10,24}): (.*)`)

// smtpdProgramRe tells an smtpd from the other programs, whatever listener it
// serves: "postfix/smtpd", "postfix/submission/smtpd", "postfix/inbound/smtpd".
// Postfix 3.7.11, src/global/mail_task.c, mail_task(): the tag is
// "<syslog_name>/<process name>"; a master.cf "-o syslog_name=" reaches it
// through mail_params_init() (src/global/mail_params.c) before
// single_server_main() calls maillog_client_init() again.
var smtpdProgramRe = regexp.MustCompile(`^postfix/(?:[a-z0-9-]+/)?smtpd$`)

// port25Program is the smtpd of the inbound relay's listener, which
// build/postfix-config.sh names so that its lines are told from those of the
// submission ports.
const port25Program = "postfix/inbound/smtpd"

const (
	// lineGrace is how old a line must be before the prune asks whether a
	// send-log row carries its queue id. The first lines of a message are
	// logged when it arrives and its row is written at end-of-message; an hour
	// is a message body sent slowly. Postfix sets no such bound: smtpd_timeout
	// limits each read, not the message (postconf(5) smtpd_timeout,
	// smtpd_per_request_deadline, Postfix 3.7.11) - assumptions A-34.
	lineGrace = time.Hour

	// lineBatch is how many lines are held before they are written without
	// waiting for the end of the pass over the log. A pass is a second's worth
	// of lines while following, and the whole backlog when the tailer starts.
	lineBatch = 500

	// port25IDs is how many queue ids opened on port 25 are remembered so that
	// their lines are not kept. Beyond it the oldest knowledge is given up, and
	// those messages' lines are kept until the prune finds them without a row.
	port25IDs = 4096
)

// lineKeeper collects the lines headed by a queue id and writes them in
// batches. It is used from the follow loop only, so it needs no locking.
type lineKeeper struct {
	st StatusStore
	// buf holds the lines not yet written; once says they were read while
	// catching up and may have been read before the last stop.
	buf  []store.SendLogLine
	once bool
	// port25 holds the queue ids the port-25 smtpd opened: mail offered by
	// strangers, which never gets a send-log row. Its lines are not kept - what
	// a stranger can make Postfix log is not something to store on his say-so
	// and prune later. An id leaves when qmgr removes the message or when
	// another listener opens a message under it (Postfix reuses ids).
	// Postfix 3.7.11: smtpd writes "<id>: client=..." when it opens the queue
	// file (src/smtpd/smtpd.c, mail_open_stream()); qmgr writes "<id>: removed"
	// when it deletes it (src/qmgr/qmgr_active.c, qmgr_active_done_3_generic());
	// short ids repeat (postconf(5) enable_long_queue_ids: the long ones are
	// the "non-repeating" kind).
	port25 map[string]struct{}
}

func newLineKeeper(st StatusStore) *lineKeeper {
	return &lineKeeper{st: st, port25: make(map[string]struct{})}
}

// add takes one mail.log line and keeps it if it is headed by the queue id of
// a message that did not come in on port 25. It does not ask whether a
// send-log row exists for the id: for a message's first lines none does yet,
// and the lines of messages that never get one - the bounces Postfix
// generates, port-25 mail met in mid-flight after a restart - are removed by
// pruneLines.
//
// catchingUp is as in inboundJournal.handle: the line may have been read
// before the last stop, and is then written only if it is not there already.
// The line is dated by its own stamp - after the panel was down, that and now
// are hours apart, and a line is matched to its row by time.
func (k *lineKeeper) add(line string, now time.Time, catchingUp bool) {
	at, rest := lineTime(line, now)
	m := queueLineRe.FindStringSubmatch(rest)
	if m == nil {
		return
	}
	program, queueID, text := m[1], m[2], m[3]

	if smtpdProgramRe.MatchString(program) && len(text) >= 7 && text[:7] == "client=" {
		if program == port25Program {
			k.rememberPort25(queueID)
		} else {
			delete(k.port25, queueID)
		}
	}
	if _, inbound := k.port25[queueID]; inbound {
		if program == "postfix/qmgr" && text == "removed" {
			delete(k.port25, queueID)
		}
		return
	}

	if catchingUp != k.once {
		k.flush()
		k.once = catchingUp
	}
	k.buf = append(k.buf, store.SendLogLine{QueueID: queueID, At: at, Line: line})
	if len(k.buf) >= lineBatch {
		k.flush()
	}
}

func (k *lineKeeper) rememberPort25(queueID string) {
	if len(k.port25) >= port25IDs {
		// Which one goes does not matter: the cost of forgetting an id is its
		// remaining lines kept until the prune.
		for id := range k.port25 {
			delete(k.port25, id)
			break
		}
	}
	k.port25[queueID] = struct{}{}
}

// flush writes the lines held. The follow loop calls it after every pass over
// the log and before it saves its place, so the place saved is never ahead of
// what was written. Lines that cannot be written are dropped and said so: the
// tailer goes on, and a page without them falls back to the log file.
func (k *lineKeeper) flush() {
	if len(k.buf) == 0 {
		return
	}
	if _, err := k.st.InsertSendLogLines(k.buf, k.once); err != nil {
		log.Printf("log-tailer: keep %d log lines: %v", len(k.buf), err)
	}
	k.buf = k.buf[:0]
}

// pruneLines removes the stored lines no send-log row carries the queue id of:
// those of rows the retention sweep deleted, and those of messages that never
// had a row.
func pruneLines(st StatusStore) {
	n, err := st.PruneSendLogLines(time.Now().UTC().Add(-lineGrace))
	if err != nil {
		log.Printf("log-tailer: prune stored log lines: %v", err)
		return
	}
	if n > 0 {
		log.Printf("log-tailer: pruned %d stored log lines with no send-log row", n)
	}
}
