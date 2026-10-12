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
var queueLineRe = regexp.MustCompile(`^\S+ [^\s\[]+\[\d+\]: ([0-9A-F]{6,24}|[0-9B-DF-HJ-NP-TV-Zb-df-hj-np-tv-z]{10,24}): `)

// lineGrace is how old a line must be before the prune asks whether a send-log
// row carries its queue id. The first lines of a message are logged when it
// arrives and its row is written at end-of-message; an hour is a message body
// sent slowly.
const lineGrace = time.Hour

// linePruneInterval is how often lines without a row are removed between two
// send-log sweeps. With the inbound relay on, most of what Postfix logs under
// a queue id is mail that never gets a send-log row; it is not left to pile up
// for six hours. A var so tests can shorten it.
var linePruneInterval = time.Hour

// storeLine keeps line if it is headed by a queue id. It does not ask whether
// a send-log row exists for the id: for a message's first lines none does yet,
// and the lines of messages that never get one are removed by pruneLines.
//
// catchingUp is as in inboundJournal.handle: the line may have been read
// before the last stop, and is then written only if it is not there already.
// The line is dated by its own stamp - after the panel was down, that and now
// are hours apart, and a line is matched to its row by time.
func storeLine(st StatusStore, line string, now time.Time, catchingUp bool) {
	at, rest := lineTime(line, now)
	m := queueLineRe.FindStringSubmatch(rest)
	if m == nil {
		return
	}
	l := store.SendLogLine{QueueID: m[1], At: at, Line: line}
	var err error
	if catchingUp {
		_, err = st.InsertSendLogLineOnce(l)
	} else {
		err = st.InsertSendLogLine(l)
	}
	if err != nil {
		log.Printf("log-tailer: keep line of %s: %v", m[1], err)
	}
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
