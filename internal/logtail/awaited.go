package logtail

import (
	"regexp"
	"sync"
	"time"
)

// The panel's own test message (Server › Health, test-email) is handed to
// Postfix through sendmail, which says nothing of the queue id it gets; the
// journal-milter does not see local mail (non_smtpd_milters is the signer
// alone). Its row is written first, with no queue id, and its Message-ID is
// awaited here: cleanup logs each message's Message-ID with its queue id,
//
//	postfix/cleanup[52]: 4j5qN53nwRz791P4: message-id=<9f3c....1760600000@mail.example.org>
//
// (Postfix 3.10.13, src/cleanup/cleanup_message.c:639, msg_info("%s:
// message-id=%s")), and the tailer puts that queue id on the row. From then on
// the row is any row: the delivery lines of its queue id give it its status.
// The awaited ids live in memory: a restart between the send and cleanup's
// line, a matter of a second, leaves the row without a queue id, and the
// reconcile sweep closes it once awaitedTTL has passed
// (StatusStore.CloseUnmatchedTestMessages).

// messageIDRe matches cleanup's Message-ID line from the head, after the time
// stamp: the id is the client's text, so only a whole line of this shape counts.
var messageIDRe = regexp.MustCompile(`^\S+ \S*/cleanup\[\d+\]: ([0-9A-Za-z]+): message-id=(<[^<>\s]+>)\s*$`)

// awaitedTTL is how long an awaited Message-ID is kept: far longer than
// sendmail's hand-over takes, and short enough that a lost one goes.
const awaitedTTL = 10 * time.Minute

type awaitedIDs struct {
	mu  sync.Mutex
	ids map[string]awaitedRow
}

type awaitedRow struct {
	row   int64
	since time.Time
}

var awaited = &awaitedIDs{ids: make(map[string]awaitedRow)}

// AwaitMessageID registers the send-log row of a message about to be handed
// to Postfix under its Message-ID ("<...>"), for the tailer to give it its
// queue id. Call it before the message is sent.
func AwaitMessageID(id string, row int64) {
	awaited.mu.Lock()
	defer awaited.mu.Unlock()
	now := time.Now()
	for k, v := range awaited.ids {
		if now.Sub(v.since) > awaitedTTL {
			delete(awaited.ids, k)
		}
	}
	awaited.ids[id] = awaitedRow{row: row, since: now}
}

// ForgetMessageID drops an awaited id: the message was not handed over.
func ForgetMessageID(id string) {
	awaited.mu.Lock()
	defer awaited.mu.Unlock()
	delete(awaited.ids, id)
}

// takeAwaited returns the row awaiting a Message-ID line's id, if any, and
// stops awaiting it.
func takeAwaited(rest string) (queueID string, row int64, ok bool) {
	m := messageIDRe.FindStringSubmatch(rest)
	if m == nil {
		return "", 0, false
	}
	awaited.mu.Lock()
	defer awaited.mu.Unlock()
	a, ok := awaited.ids[m[2]]
	if !ok {
		return "", 0, false
	}
	delete(awaited.ids, m[2])
	// An id awaited too long belongs to a row the sweep has closed by now;
	// whoever received the message knows the id, and a line of his with it
	// must not reach that row.
	if time.Since(a.since) > awaitedTTL {
		return "", 0, false
	}
	return m[1], a.row, true
}
