// Package logtail follows Postfix's mail.log and reconciles the send-log
// delivery statuses the journal-milter could not know at receive time
// (architecture.md § Persistence). A milter row starts life as "queued";
// Postfix only decides sent / deferred / bounced later, per recipient, and
// reports it in mail.log. This package parses those lines by queue-id +
// recipient and advances the matching rows, and prunes rows past the retention
// window. It also keeps every line Postfix wrote under a queue id, so that a
// message's page can show them after the log has rotated (lines.go).
package logtail

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

// StatusStore is the slice of the store the log-tailer needs: advancing
// delivery statuses, finding the rows still waiting for one, pruning the
// retention window, keeping the lines of queued messages, and remembering how
// far into mail.log it has read. *store.Store satisfies it.
type StatusStore interface {
	UpdateStatus(queueID, recipient, status string) (int64, error)
	// SetSendLogQueueID gives the panel's test message its queue id
	// (awaited.go); CloseUnmatchedTestMessages closes the test messages
	// that never got one.
	SetSendLogQueueID(id int64, queueID string) error
	CloseUnmatchedTestMessages(before time.Time) (int64, error)
	ExpireMessage(queueID string) (int64, error)
	ListOpenOlderThan(cutoff time.Time) ([]store.QueuedDelivery, error)
	CloseUnreported(id int64) (int64, error)
	// The counters of delivery_log_days (store/deliverylog.go).
	AddReconcileClosed(at time.Time, n int64) error
	AddReconcileMissed(at time.Time, n int64) error
	AddTailerBlind(from time.Time, d time.Duration) error
	CountOpenOlderThan(cutoff time.Time) (int64, error)
	SetQueuedOverLifetime(at time.Time, n int64) error
	PruneDeliveryLogDays(now time.Time) error
	RefusalStore
	DeleteSendLogBefore(cutoff time.Time) (int64, error)
	InsertSendLogLines(lines []store.SendLogLine, once bool) (int, error)
	PruneSendLogLines(cutoff time.Time) (int64, error)
	LogtailState(path string) (store.LogtailState, bool, error)
	SaveLogtailState(path string, st store.LogtailState) error
}

// pollInterval is how often the tail loop checks for new bytes / rotation. It
// is a var so tests can shorten it.
var pollInterval = time.Second

// retentionInterval is how often the retention sweep runs (also once at
// startup). It is a var so tests can shorten it.
var retentionInterval = 6 * time.Hour

// RetentionInterval returns how often background send-log pruning runs.
func RetentionInterval() time.Duration {
	return retentionInterval
}

// queueIDs lists the messages Postfix currently holds, for the reconcile sweep.
// It is a var so tests can answer without a running Postfix. Postfix 3.10.13,
// src/showq/showq.c, showq_service(): the listing covers the maildrop,
// active, incoming, deferred and hold queues.
var queueIDs = postfix.QueueIDs

// retryPolicy reads Postfix's queue lifetimes, for counting the rows left
// queued past them. A var for the same reason.
var retryPolicy = postfix.LoadRetryPolicy

const (
	// reconcileInterval is how often the sweep compares stuck rows against the
	// Postfix queue, and reconcileGrace how long a row is left alone first.
	// The grace covers the ordinary lag between the milter writing the row and
	// Postfix logging the result — seconds, generously rounded up — so a
	// message merely in flight is never touched.
	reconcileInterval = 5 * time.Minute
	reconcileGrace    = 2 * time.Minute
)

// RetentionDays returns the send-log retention window in days. The log-tailer
// calls it on every prune cycle so a panel change takes effect without restart.
type RetentionDays func() int

// deliveryRe matches a Postfix delivery line and captures queue-id, recipient,
// orig_to, relay and status, e.g.
//
//	postfix/smtp[26]: 41E862C00D9E: to=<a@example.net>, relay=…, dsn=2.0.0, status=sent (250 OK)
//
// The "<queue-id>: to=<addr>, …, status=<word>" shape is the per-recipient
// result line: Postfix 3.10.13, src/global/log_adhoc.c, log_adhoc() writes
// "%s: to=<%s>", ", orig_to=<%s>" (only when the address was rewritten),
// ", relay=%s", then ", dsn=%s, status=%s (%s)". It is called from sent(),
// defer_append() and bounce_append() in whichever process decides - the
// delivery agents, and qmgr too for recipients it defers or bounces itself
// (src/qmgr/qmgr_defer.c, qmgr_bounce.c); smtpd's and cleanup's own lines
// have no "to=<…>, relay=" after the id.
//
// The recipient runs to the ">, " that comes before relay= (and orig_to=,
// when there is one): a quoted local part may hold a ">", and a recipient cut
// there would match no row and leave it for the sweep (review of stage 1).
//
// It is matched at the head of the line - the host name, "program[pid]:",
// then the queue id - after SplitTimestamp has taken the stamp off, like
// queueLineRe and the inbound patterns. Anywhere else the text is a
// stranger's: a port-25 reject line ends with the client's HELO name, which
// once parsed as a delivery result when it read "<id>: to=<…>, status=sent"
// (audit 2026-10-12, M12).
//
// The run before status= is lazy on purpose. Postfix appends the remote
// server's reply verbatim, so a greedy match would take the *last* status= on
// the line — and that one can come from the reply text, which the far end
// controls. A bounce whose reply quoted "status=sent" would then be filed as a
// success. The real field is always the first one after to=<…>: in
// log_adhoc() the reply is the "(%s)" after status=.
var deliveryRe = regexp.MustCompile(`^\S+ [^\s\[]+\[\d+\]: ([0-9A-Za-z]+): to=<(` + loggedAddress + `)>, (?:orig_to=<(` + loggedAddress + `)>, )?relay=([^,\s]*).*?\bstatus=(\w+)`)

// loggedAddress is an address as Postfix logs it, external form: a local part
// in quotes when it is not a plain dot-string - and then it may hold "<", ">",
// "," and "=", with only '"' and '\' escaped - or one with none of '"' and
// '>' (Postfix 3.10.13, src/global/quote_822_local.c,
// src/global/info_log_addr_form.c). Read by that grammar, a stranger's quoted
// RCPT TO cannot end the address early and pass "orig_to=<…>, relay=…,
// status=…" of his own as the line's fields (security review of
// inbound-upstream-result, 2026-10-16; a local part with "@" is a routed
// address, which port 25 refuses as relaying anyway: src/trivial-rewrite/
// resolve.c:467, src/smtpd/smtpd_check.c:1724, allow_untrusted_routing=no).
const loggedAddress = `"(?:[^"\\]|\\.)*"@[^>]*|[^>"]*`

// parseDelivery extracts (queue-id, recipient, status) from a mail.log line.
// ok is false for lines that are not recognised delivery results. The
// recipient is the one the application gave: orig_to= when Postfix rewrote
// the address (log_adhoc() writes it only then), to= otherwise - the row
// holds the RCPT TO address (assumptions A-9).
func parseDelivery(line string) (queueID, recipient, status string, ok bool) {
	_, rest := SplitTimestamp(line)
	m := deliveryRe.FindStringSubmatch(rest)
	if m == nil {
		return "", "", "", false
	}
	recipient = m[2]
	if m[3] != "" {
		recipient = m[3]
	}
	switch m[5] {
	case "sent":
		status = store.StatusSent
	case "deferred":
		status = store.StatusDeferred
	case "bounced":
		status = store.StatusBounced
	default:
		return "", "", "", false
	}
	return m[1], recipient, status, true
}

// deliveryReply returns the host and the reply of a delivery line (the line
// after its time stamp): relay= - which log_adhoc() writes right after to=
// and orig_to=, followed by a comma - and the "(%s)" after the first status=,
// to the end of the line (Postfix 3.10.13, src/global/log_adhoc.c). Taking the
// first status= keeps the reply whole when the far end's text holds one of
// its own. A line that is not a delivery line gives "", "".
func deliveryReply(rest string) (relay, reply string) {
	loc := deliveryRe.FindStringSubmatchIndex(rest)
	if loc == nil {
		return "", ""
	}
	relay = rest[loc[8]:loc[9]]
	tail := strings.TrimSpace(rest[loc[1]:])
	if strings.HasPrefix(tail, "(") && strings.HasSuffix(tail, ")") {
		reply = tail[1 : len(tail)-1]
	}
	return relay, reply
}

// expiryRe matches qmgr's line for a message it gave up on:
//
//	postfix/qmgr[93]: 41E862C00D9E: from=<app@example.org>, status=expired, returned to sender
//
// Postfix 3.10.13, src/qmgr/qmgr_active.c, qmgr_active_done_25_generic():
// msg_info("%s: from=<%s>, status=%s, returned to sender") with "expired"
// once the queue lifetime has passed, "force-expired" after postsuper -e.
// It is the only line about the expiry, one per message, and no recipient is
// named (assumptions A-8). The line must end there: the sender is the
// client's text, and only qmgr writes this shape.
var expiryRe = regexp.MustCompile(`^\S+ \S*/qmgr\[\d+\]: ([0-9A-Za-z]+): from=<.*>, status=(?:force-)?expired, returned to sender\s*$`)

// parseExpiry returns the queue id of a message Postfix gave up on, from its
// expiry line; ok is false for any other line.
func parseExpiry(line string) (queueID string, ok bool) {
	_, rest := SplitTimestamp(line)
	m := expiryRe.FindStringSubmatch(rest)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// Run follows path and updates send-log statuses until ctx is cancelled, while
// a background sweep prunes rows older than retention(). Reading resumes at
// the offset the previous run persisted, so a restart parses the delivery lines
// written while the panel was down. The lines headed by a queue id are kept
// for the message's page, except those of mail that came in on port 25
// (lines.go). With inbound set it also writes the
// inbound journal's rows for refused mail and prunes that journal
// (inbound.go), and with sender set it counts the mail Postfix deferred
// because the sender check did not answer (sendercheck.go). It returns nil on
// a clean shutdown.
func Run(ctx context.Context, path string, st StatusStore, retention RetentionDays, inbound *Inbound, sender *SenderCheck) error {
	go retentionLoop(ctx, st, retention)
	var journal *inboundJournal
	if inbound != nil {
		go inboundRetentionLoop(ctx, inbound)
		journal = newInboundJournal(inbound.Store)
	}

	// The reconcile sweep must not run against a backlog the tailer has not
	// read yet: on a restart the log holds the very lines that resolve the rows
	// the sweep would otherwise close. follow() closes this once it has read to
	// end-of-file for the first time.
	caughtUp := make(chan struct{})
	go reconcileLoop(ctx, st, caughtUp, path)

	var deferred *deferredCounter
	if sender != nil {
		deferred = newDeferredCounter(sender)
	}

	keeper := newLineKeeper(st)
	refusals := &refusalLines{st: st}
	flush := func() {
		keeper.flush()
		refusals.retry(time.Now().UTC())
	}
	tr := &tracker{st: st, path: path, hold: refusals.oldest}
	return follow(ctx, path, tr, caughtUp, flush, func(line string) {
		// Until the first read reaches end-of-file the lines are those written
		// while the panel was down - and, after an unclean stop, some it had
		// read already.
		catchingUp := true
		select {
		case <-caughtUp:
			catchingUp = false
		default:
		}
		now := time.Now().UTC()
		if journal != nil {
			journal.handle(line, now, catchingUp)
		}
		if deferred != nil {
			deferred.handle(line, now, catchingUp, tr.line)
		}
		keeper.add(line, now, catchingUp)
		refusals.handle(line, now, Place{FP: tr.line.FP, End: tr.lineStart})
		// The panel's test message learns its queue id from cleanup's line.
		if _, rest := SplitTimestamp(line); rest != "" {
			if qid, row, ok := takeAwaited(rest); ok {
				if err := st.SetSendLogQueueID(row, qid); err != nil {
					log.Printf("log-tailer: test message %d -> %s: %v", row, qid, err)
				}
			}
		}
		// A delivery or expiry line no send-log row takes may be of mail
		// accepted on port 25: the inbound journal's rows take its result.
		queueID, recipient, status, ok := parseDelivery(line)
		if !ok {
			if queueID, ok := parseExpiry(line); ok {
				n, err := st.ExpireMessage(queueID)
				if err != nil {
					log.Printf("log-tailer: expire %s: %v", queueID, err)
				}
				if n == 0 && journal != nil {
					journal.expired(line, now, queueID)
				}
			}
			return
		}
		n, err := st.UpdateStatus(queueID, recipient, status)
		if err != nil {
			log.Printf("log-tailer: update %s/%s -> %s: %v", queueID, recipient, status, err)
		}
		if n == 0 && journal != nil {
			journal.upstream(line, now, queueID, recipient, status)
		}
	})
}

// reconcileLoop periodically closes send-log rows Postfix has stopped working
// on (architecture.md § Log tailer). It starts only once the tailer has caught
// up with the log, and then leaves the first sweep a full interval away, so a
// restart resolves rows from the log — the accurate source — before the sweep
// gets to guess at whatever the log could not explain.
func reconcileLoop(ctx context.Context, st StatusStore, caughtUp <-chan struct{}, path string) {
	select {
	case <-ctx.Done():
		return
	case <-caughtUp:
	}

	// A message Postfix still works on stays queued until its first retry after
	// the queue lifetime, which can be a whole backoff later. Postfix 3.10.13,
	// src/qmgr/qmgr_active.c: expiry is decided when an attempt ends
	// (qmgr_active_done_25_generic()), and a deferred message waits at most
	// maximal_backoff_time (qmgr_active_done_3_generic()). The deferred queue
	// is scanned only every queue_run_delay (src/qmgr/qmgr.c), which this
	// bound leaves out - assumptions A-30.
	policy := retryPolicy()
	overLifetime := policy.LongestLifetime() + policy.MaximalBackoff

	t := time.NewTicker(reconcileInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := time.Now().UTC()
			reconcile(st, now.Add(-reconcileGrace), path)
			countStuck(st, now, overLifetime)
			if _, err := st.CloseUnmatchedTestMessages(now.Add(-awaitedTTL)); err != nil {
				log.Printf("log-tailer: close test messages with no queue id: %v", err)
			}
		}
	}
}

// countStuck records how many rows are still open past Postfix's queue
// lifetime - what the log would leave unresolved if there were no sweep. Each
// sweep overwrites the day's value, so a past day keeps its last sweep's.
func countStuck(st StatusStore, now time.Time, overLifetime time.Duration) {
	n, err := st.CountOpenOlderThan(now.Add(-overLifetime))
	if err == nil {
		err = st.SetQueuedOverLifetime(now, n)
	}
	if err != nil {
		log.Printf("log-tailer: count rows queued past the queue lifetime: %v", err)
	}
}

// reconcile marks as bounced every row still open ("queued" or "deferred")
// from before cutoff whose message Postfix no longer holds, with reason
// "reconcile". The close itself is conditional on the row still being open,
// so a final delivery line read between the listing and the close wins.
//
// A row reaches this state only when its delivery lines are gone for good — the
// log rotated past its fourteen files while the panel was down, or was deleted
// — since the log itself now outlives the container. Postfix having dropped the
// message means it will never report anything more about it, so the row can
// only be closed on an assumption; it is closed as a failure rather than a
// success because a delivery the panel cannot evidence must not be shown as
// one. Postfix does leave a recipient without a result line in a few cases of
// its own - postsuper -d (src/postsuper/postsuper.c logs only "removed"), a
// recipient cleanup dropped as a duplicate (src/cleanup/
// cleanup_out_recipient.c, been_here()), one a milter deleted - assumptions
// A-31. Rows whose message is still in the queue, and every row when the queue
// cannot be listed at all, are left exactly as they are.
//
// A closed row whose delivery line is in mail.log or mail.log.1 after all is
// counted as missed: the line was there and the tailer did not read it. That
// is what tells a tailer that loses lines from a log that lost them, which
// reconcile_contradicted cannot - a lost line is never read late.
func reconcile(st StatusStore, cutoff time.Time, path string) {
	rows, err := st.ListOpenOlderThan(cutoff)
	if err != nil {
		log.Printf("log-tailer: reconcile: list open rows: %v", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	held, err := queueIDs()
	if err != nil {
		// No listing is no information: closing rows now would be a guess made
		// against nothing.
		log.Printf("log-tailer: reconcile: read postfix queue: %v", err)
		return
	}

	var closed, missed int64
	for _, row := range rows {
		if _, still := held[row.QueueID]; still {
			continue
		}
		n, err := st.CloseUnreported(row.ID)
		if err != nil {
			log.Printf("log-tailer: reconcile: close %s/%s: %v", row.QueueID, row.To, err)
			continue
		}
		closed += n
		if n > 0 && resultInLog(path, row) {
			log.Printf("log-tailer: reconcile: %s/%s had a delivery line in the log the tailer did not read", row.QueueID, row.To)
			missed++
		}
	}
	if closed > 0 {
		log.Printf("log-tailer: reconcile: closed %d row(s) Postfix no longer holds and never reported", closed)
		if err := st.AddReconcileClosed(time.Now(), closed); err != nil {
			log.Printf("log-tailer: reconcile: count: %v", err)
		}
		if err := st.AddReconcileMissed(time.Now(), missed); err != nil {
			log.Printf("log-tailer: reconcile: count: %v", err)
		}
	}
}

// resultInLog says whether the current log or the one rotated last holds a
// final result for row - a delivery line that says sent or bounced, or the
// message's expiry line. A deferred line is not one: a deferred row has one
// by definition. Rows reach the sweep minutes after they were written, so
// the end of those two files is where such a line would be.
// logrotate 3.22.0, logrotate.c, prerotateSingleLog(): the first rotated
// name is "<log>.1", without the compression suffix under delaycompress
// (build/logrotate-mail.conf).
func resultInLog(path string, row store.QueuedDelivery) bool {
	if path == "" {
		return false
	}
	for _, p := range []string{path, path + ".1"} {
		lines, err := QueueLines(p, row.QueueID, 256)
		if err != nil {
			continue
		}
		for _, line := range lines {
			if q, r, s, ok := parseDelivery(line); ok && q == row.QueueID && strings.EqualFold(r, row.To) && s != store.StatusDeferred {
				return true
			}
			if q, ok := parseExpiry(line); ok && q == row.QueueID {
				return true
			}
		}
	}
	return false
}

// retentionLoop prunes expired send-log rows immediately and then periodically,
// and after each sweep the stored log lines no row carries the queue id of:
// those the deleted rows leave behind, and those that never had a row
// (lines.go).
func retentionLoop(ctx context.Context, st StatusStore, retention RetentionDays) {
	prune := func() {
		retentionDays := retention()
		if retentionDays <= 0 {
			retentionDays = store.SendLogRetentionDaysDefault
		}
		cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
		n, err := st.DeleteSendLogBefore(cutoff)
		if err != nil {
			log.Printf("log-tailer: retention prune: %v", err)
			return
		}
		if n > 0 {
			log.Printf("log-tailer: pruned %d send-log rows older than %d days", n, retentionDays)
		}
	}

	pruneDays := func() {
		if err := st.PruneDeliveryLogDays(time.Now()); err != nil {
			log.Printf("log-tailer: %v", err)
		}
	}

	prune()
	pruneLines(st)
	pruneDays()
	t := time.NewTicker(retentionInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			prune()
			pruneLines(st)
			pruneDays()
		}
	}
}

// TailLines returns up to n of the most recent lines from path, for the
// panel's mail.log monitoring view (architecture.md § Panel HTTP surface). It
// is a one-shot, point-in-time read on request — unrelated to the background
// follow loop above — that reads backwards in chunks so it stays cheap against
// a multi-megabyte log rather than reading the whole file every poll.
func TailLines(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	const chunkSize = 8192
	var (
		buf    []byte
		offset = info.Size()
	)
	for offset > 0 && bytes.Count(buf, []byte("\n")) <= n {
		size := int64(chunkSize)
		if size > offset {
			size = offset
		}
		offset -= size
		chunk := make([]byte, size)
		if _, err := f.ReadAt(chunk, offset); err != nil {
			return nil, err
		}
		buf = append(chunk, buf...)
	}

	text := strings.TrimRight(string(buf), "\n")
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// queueScanBytes bounds how far back QueueLines reads. A message's own lines
// are a handful, but they are scattered through everything else the mail path
// logged around them, so finding them means reading rather than seeking. The
// budget is what keeps that read bounded on a log that has grown for a day:
// beyond it the answer is "not in the part of the log still on disk", which is
// the same answer rotation gives and is reported the same way. It is a var so
// tests can shrink it, the way pollInterval above is.
var queueScanBytes int64 = 4 << 20

// QueueLines returns the mail.log lines Postfix wrote about one queue id,
// oldest first and at most n of them, for a single delivery's page
// (architecture.md § Panel HTTP surface). It reads the tail of the log the way
// TailLines does — one-shot, on request, unrelated to the follow loop above —
// but keeps only the lines belonging to this message instead of the last n of
// everything.
//
// A message older than the tail scanned, or older than the current log file,
// comes back empty rather than as an error: send-log rows outlive mail.log
// (retention is ninety days by default, rotation keeps fourteen files), so a
// row with nothing left to show for it is expected.
func QueueLines(path, queueID string, n int) ([]string, error) {
	// No queue-id, no lines: a message the milter refused was never queued, so
	// there is nothing to match on and every line would be someone else's.
	if queueID == "" || n <= 0 {
		return nil, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	// Forward from the start of the budget rather than backwards in chunks:
	// the lines are wanted oldest first, and the whole budget is read either
	// way, so reading it in order costs nothing and keeps them in order.
	start := info.Size() - queueScanBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}

	sc := bufio.NewScanner(f)
	// A Postfix line carrying a long remote reply can pass the scanner's default
	// 64KB token; without a bigger ceiling that one line would end the scan and
	// silently truncate the answer.
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	// Reading into the middle of the file lands mid-line; that fragment is
	// dropped rather than reported as a line of its own.
	if start > 0 && sc.Scan() {
		_ = sc.Text()
	}

	var lines []string
	for sc.Scan() {
		line := sc.Text()
		if !mentionsQueueID(line, queueID) {
			continue
		}
		lines = append(lines, line)
		// Keep the latest n rather than stopping at the first n: what a message
		// did last is what its page is opened for.
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// mentionsQueueID reports whether a mail.log line is about queueID. Postfix
// writes the id followed by a colon ("… postfix/smtp[26]: 41E862C00D9E: to=…";
// Postfix 3.10.13 "%s: ..." in log_adhoc(), smtpd's "%s: client=", qmgr's
// "%s: removed", cleanup's "%s: removed (%s)"), and the match is anchored on
// the character before it so a shorter id is not found inside a longer one —
// queue ids are hexadecimal, and one being the tail of another is ordinary,
// not unlikely.
func mentionsQueueID(line, queueID string) bool {
	needle := queueID + ":"
	for i := 0; i <= len(line)-len(needle); {
		j := strings.Index(line[i:], needle)
		if j < 0 {
			return false
		}
		j += i
		if j == 0 || !isQueueIDByte(line[j-1]) {
			return true
		}
		i = j + 1
	}
	return false
}

func isQueueIDByte(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

// syslogStampRe is the timestamp at the head of a mail.log line (maillog_file
// in build/postfix-config.sh), the one format Postfix 3.10 writes. Postfix
// 3.10.13, src/util/msg_logger.c, msg_logger_print(): every record is stamped
// strftime("%b %d %H:%M:%S ") in the logging process's local time (the day
// zero-padded), then the first label of the host name, and postlogd writes it
// as received (src/postlogd/postlogd.c, postlogd_service()). It carries no
// year and no zone, so the stamp shown is a wall clock and nothing more,
// which is all this column claims to be. An RFC 3339 stamp once matched
// here too, for a maillog_file_format parameter said to arrive in Postfix
// 3.9; there is none in 3.7.11, nor in 3.10.13, nor in the 3.12 snapshot of
// 2026-09-25 (proto/postconf.proto; msg_logger.c keeps the strftime) -
// assumptions A-12.
var syslogStampRe = regexp.MustCompile(`^([A-Z][a-z]{2}\s+\d{1,2} \d{2}:\d{2}:\d{2})\s`)

// SplitTimestamp separates the timestamp at the head of a mail.log line from
// the rest of it, so a page can put the two in their own columns. The stamp
// is the log's own wall clock, not converted: what is on the page is what is
// in the file.
//
// A line whose head is not a timestamp this recognises comes back whole, as
// rest, with an empty stamp. Nothing is ever dropped: the point of showing the
// log is that it says what it says.
func SplitTimestamp(line string) (stamp, rest string) {
	if m := syslogStampRe.FindStringSubmatch(line); m != nil {
		return m[1], strings.TrimSpace(line[len(m[0]):])
	}
	return "", line
}

// follow tails path line by line, calling handle for each complete line, until
// ctx is cancelled. Where it starts is tr's decision (a persisted offset, the
// start of a file that changed while the panel was down, or end-of-file on a
// first ever run); it reopens the file when it is rotated, so nothing is missed.
// The inode changes under logrotate's create, which build/logrotate-mail.conf
// uses; a file shorter than the read position is taken as truncation.
// logrotate 3.22.0, logrotate.c, rotateSingleLog(): without copytruncate the
// file is rename()d and, with create, a new one made by createOutputFile();
// with copytruncate it is copied and truncated, which that config does not use.
//
// caughtUp is closed after the first read that reaches end-of-file, which is
// the point where every line the panel missed while it was down has been
// handled.
//
// flush is called at the end of every pass over the file, before the read
// position is saved: a handler that gathers lines to write them together
// writes them there, so the saved position is never ahead of what was written.
func follow(ctx context.Context, path string, tr *tracker, caughtUp chan struct{}, flush func(), handle func(string)) error {
	var (
		f       *os.File
		r       *bufio.Reader
		info    os.FileInfo
		pending string
		// consumed is the offset just past what the reader has handed over,
		// pending included: where a complete line ends.
		consumed int64
		// old is the file rotated away, still read for rotatedGrace.
		old *rotatedTail
	)
	openAt := func(offset int64, whence int) error {
		nf, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := nf.Seek(offset, whence); err != nil {
			nf.Close()
			return err
		}
		ni, err := nf.Stat()
		if err != nil {
			nf.Close()
			return err
		}
		if f != nil {
			f.Close()
		}
		f, r, info, pending = nf, bufio.NewReader(nf), ni, ""
		consumed = offset
		return nil
	}

	// The container may start before Postfix has created mail.log; wait for it.
	for {
		if err := openAt(0, io.SeekStart); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(pollInterval):
		}
	}
	start := tr.resume(f)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		log.Printf("log-tailer: seek %s: %v", path, err)
		start = 0
	}
	consumed = start
	r.Reset(f) // the reader buffered from the pre-seek position
	// Rotated while the panel was down: the end of the old file first.
	if tr.readRotated(handle) {
		flush()
	}
	defer func() {
		if f != nil {
			f.Close()
		}
	}()

	drain := func() {
		defer flush()
		for {
			line, err := r.ReadString('\n')
			consumed += int64(len(line))
			if err == io.EOF {
				pending += line // hold the partial line until it completes
				return
			}
			if err != nil {
				log.Printf("log-tailer: read %s: %v", path, err)
				return
			}
			full := pending + line
			pending = ""
			tr.line = Place{FP: tr.fp, End: consumed}
			tr.lineStart = consumed - int64(len(full))
			handle(strings.TrimRight(full, "\r\n"))
		}
	}

	// read returns how many bytes of the open file have actually been consumed:
	// the descriptor position less the partial line bufio handed back at EOF,
	// which is re-read (and completed) on the next drain or the next start.
	read := func() int64 {
		pos, _ := f.Seek(0, io.SeekCurrent)
		return pos - int64(len(pending))
	}

	// drainOld reads what was written to the rotated file since the switch.
	// Its lines take places in that file, which the tracker never saves as
	// the place to resume from: on a restart the rotated file is read from
	// the saved place in the new one onward only (readRotated).
	drainOld := func(now time.Time) {
		if old == nil {
			return
		}
		defer flush()
		for {
			line, err := old.r.ReadString('\n')
			old.consumed += int64(len(line))
			if err != nil {
				old.pending += line
				break
			}
			full := old.pending + line
			old.pending = ""
			tr.line = Place{FP: old.fp, End: old.consumed}
			tr.lineStart = old.consumed - int64(len(full))
			handle(strings.TrimRight(full, "\r\n"))
		}
		if now.After(old.until) {
			old.f.Close()
			old = nil
		}
	}
	defer func() {
		if old != nil {
			old.f.Close()
		}
	}()

	var once sync.Once
	blind := &blindness{add: tr.st.AddTailerBlind}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			blind.see(time.Now(), false)
			// What postlogd wrote to the rotated file since the last tick:
			// the next start resumes in the new file only (review of stage
			// 2c, F8).
			drainOld(time.Now())
			tr.record(f, read(), true) // shutdown: the next start resumes here
			return nil
		case <-ticker.C:
			drainOld(time.Now())
			drain()
			once.Do(func() { close(caughtUp) })
			ni, err := os.Stat(path)
			if err != nil {
				blind.see(time.Now(), true)
				continue // file briefly gone mid-rotation; try again next tick
			}
			pos, _ := f.Seek(0, io.SeekCurrent)
			if !os.SameFile(info, ni) || ni.Size() < pos {
				// Rotated away or truncated: the old (renamed) inode may have
				// gained lines between the drain() above and this check, since
				// Postfix keeps writing to it until it reloads. Drain it once
				// more before switching, and - renamed, not truncated - go on
				// reading it for rotatedGrace after the switch, for what
				// postlogd writes there until the reload (assumptions A-33).
				drain()
				renamed := !os.SameFile(info, ni)
				if old != nil {
					drainOld(time.Time{}) // a second rotation within the grace
					if old != nil {
						old.f.Close()
						old = nil
					}
				}
				prev := &rotatedTail{f: f, r: r, pending: pending, consumed: consumed, fp: tr.fp, until: time.Now().Add(rotatedGrace)}
				if renamed {
					f = nil // openAt closes the file it replaces; this one is kept
				}
				if err := openAt(0, io.SeekStart); err != nil {
					if renamed {
						f, r = prev.f, prev.r
					}
					log.Printf("log-tailer: reopen %s: %v", path, err)
					blind.see(time.Now(), true)
					continue
				}
				if renamed {
					old = prev
				}
				blind.see(time.Now(), false)
				tr.adopt(f)
				continue
			}
			blind.see(time.Now(), false)
			tr.record(f, read(), false)
		}
	}
}

// rotatedTail is mail.log just rotated away, still open: postlogd writes to
// it until "postfix reload" makes it exit. Postfix 3.10.13,
// src/postlogd/postlogd.c, pre_jail_init(): maillog_file is opened once per
// postlogd process; src/master/master_service.c, master_restart_service(),
// closes the service's status pipe at a reload (master_status_cleanup()),
// and src/master/dgram_server.c, dgram_server_abort(), exits when it reads
// its end - the next postlogd opens the new file. logrotate runs the reload
// right after the rename and create (build/logrotate-mail.conf, postrotate;
// logrotate 3.22.0, logrotate.c, rotateLogSet(): the postrotate script runs
// right after rotateSingleLog() has renamed and created), so the gap is short;
// rotatedGrace is far longer than it.
type rotatedTail struct {
	f        *os.File
	r        *bufio.Reader
	pending  string
	consumed int64
	fp       string
	until    time.Time
}

// rotatedGrace is how long the rotated file is still read after the switch.
const rotatedGrace = time.Minute

// blindness measures the stretches in which follow cannot read mail.log - the
// file gone, reopening it failing - for delivery_log_days. A stretch is
// stored when it ends, and every minute while it lasts, so a long one is not
// lost to a restart and lands on the days it covered. A stretch shorter than
// blindMin is not counted: a rotation leaves the name missing for a tick
// between rename and create, and that loses nothing - the new file is read
// from its start. logrotate 3.22.0, logrotate.c, rotateSingleLog(): the
// rename() and createOutputFile() follow each other in one pass, with no
// script or wait between them (postrotate runs after).
type blindness struct {
	add   func(from time.Time, d time.Duration) error
	since time.Time // start of the current stretch; zero when reading
	long  bool      // the current stretch has passed blindMin
}

const blindMin = 3 * time.Second

func (b *blindness) see(now time.Time, blind bool) {
	if blind && b.since.IsZero() {
		b.since = now
		return
	}
	if b.since.IsZero() {
		return
	}
	d := now.Sub(b.since)
	if d >= blindMin {
		b.long = true
	}
	if blind && d < time.Minute {
		return
	}
	if b.long {
		if err := b.add(b.since, d); err != nil {
			log.Printf("log-tailer: count the time mail.log could not be read: %v", err)
		}
	}
	if blind {
		b.since = now // the stretch goes on; the next part starts here
		return
	}
	b.since, b.long = time.Time{}, false
}
