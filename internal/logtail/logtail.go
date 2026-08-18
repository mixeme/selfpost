// Package logtail follows Postfix's mail.log and reconciles the send-log
// delivery statuses the journal-milter could not know at receive time
// (architecture.md § Persistence). A milter row starts life as "queued";
// Postfix only decides sent / deferred / bounced later, per recipient, and
// reports it in mail.log. This package parses those lines by queue-id +
// recipient and advances the matching rows, and prunes rows past the retention
// window.
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
// retention window, and remembering how far into mail.log it has read.
// *store.Store satisfies it.
type StatusStore interface {
	UpdateStatus(queueID, recipient, status string) (int64, error)
	ListQueuedOlderThan(cutoff time.Time) ([]store.QueuedDelivery, error)
	DeleteSendLogBefore(cutoff time.Time) (int64, error)
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
// It is a var so tests can answer without a running Postfix.
var queueIDs = postfix.QueueIDs

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

// deliveryRe matches a Postfix delivery line and captures queue-id, recipient
// and status, e.g.
//
//	postfix/smtp[26]: 41E862C00D9E: to=<a@example.net>, relay=…, dsn=2.0.0, status=sent (250 OK)
//
// The "<queue-id>: to=<addr>, …, status=<word>" shape is specific to the
// delivery agents; qmgr/smtpd/cleanup lines do not match.
//
// The run before status= is lazy on purpose. Postfix appends the remote
// server's reply verbatim, so a greedy match would take the *last* status= on
// the line — and that one can come from the reply text, which the far end
// controls. A bounce whose reply quoted "status=sent" would then be filed as a
// success. The real field is always the first one after to=<…>.
var deliveryRe = regexp.MustCompile(`\b([0-9A-Za-z]+): to=<([^>]*)>,.*?\bstatus=(\w+)`)

// parseDelivery extracts (queue-id, recipient, status) from a mail.log line.
// ok is false for lines that are not recognised delivery results.
func parseDelivery(line string) (queueID, recipient, status string, ok bool) {
	m := deliveryRe.FindStringSubmatch(line)
	if m == nil {
		return "", "", "", false
	}
	switch m[3] {
	case "sent":
		status = store.StatusSent
	case "deferred":
		status = store.StatusDeferred
	case "bounced":
		status = store.StatusBounced
	case "expired":
		// Postfix gave up after the queue lifetime; a final failure for us.
		status = store.StatusBounced
	default:
		return "", "", "", false
	}
	return m[1], m[2], status, true
}

// Run follows path and updates send-log statuses until ctx is cancelled, while
// a background sweep prunes rows older than retention(). Reading resumes at
// the offset the previous run persisted, so a restart parses the delivery lines
// written while the panel was down. It returns nil on a clean shutdown.
func Run(ctx context.Context, path string, st StatusStore, retention RetentionDays) error {
	go retentionLoop(ctx, st, retention)

	// The reconcile sweep must not run against a backlog the tailer has not
	// read yet: on a restart the log holds the very lines that resolve the rows
	// the sweep would otherwise close. follow() closes this once it has read to
	// end-of-file for the first time.
	caughtUp := make(chan struct{})
	go reconcileLoop(ctx, st, caughtUp)

	return follow(ctx, path, &tracker{st: st, path: path}, caughtUp, func(line string) {
		queueID, recipient, status, ok := parseDelivery(line)
		if !ok {
			return
		}
		if _, err := st.UpdateStatus(queueID, recipient, status); err != nil {
			log.Printf("log-tailer: update %s/%s -> %s: %v", queueID, recipient, status, err)
		}
	})
}

// reconcileLoop periodically closes send-log rows Postfix has stopped working
// on (architecture.md § Log tailer). It starts only once the tailer has caught
// up with the log, and then leaves the first sweep a full interval away, so a
// restart resolves rows from the log — the accurate source — before the sweep
// gets to guess at whatever the log could not explain.
func reconcileLoop(ctx context.Context, st StatusStore, caughtUp <-chan struct{}) {
	select {
	case <-ctx.Done():
		return
	case <-caughtUp:
	}

	t := time.NewTicker(reconcileInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			reconcile(st, time.Now().UTC().Add(-reconcileGrace))
		}
	}
}

// reconcile marks as bounced every row still "queued" from before cutoff whose
// message Postfix no longer holds.
//
// A row reaches this state only when its delivery lines are gone for good — the
// log rotated past its fourteen files while the panel was down, or was deleted
// — since the log itself now outlives the container. Postfix having dropped the
// message means it will never report anything more about it, so the row can
// only be closed on an assumption; it is closed as a failure rather than a
// success because a delivery the panel cannot evidence must not be shown as
// one. Rows whose message is still in the queue, and every row when the queue
// cannot be listed at all, are left exactly as they are.
func reconcile(st StatusStore, cutoff time.Time) {
	rows, err := st.ListQueuedOlderThan(cutoff)
	if err != nil {
		log.Printf("log-tailer: reconcile: list queued rows: %v", err)
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

	var closed int
	for _, row := range rows {
		if _, still := held[row.QueueID]; still {
			continue
		}
		if _, err := st.UpdateStatus(row.QueueID, row.To, store.StatusBounced); err != nil {
			log.Printf("log-tailer: reconcile: close %s/%s: %v", row.QueueID, row.To, err)
			continue
		}
		closed++
	}
	if closed > 0 {
		log.Printf("log-tailer: reconcile: closed %d row(s) Postfix no longer holds and never reported", closed)
	}
}

// retentionLoop prunes expired send-log rows immediately and then periodically.
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
// writes the id followed by a colon ("… postfix/smtp[26]: 41E862C00D9E: to=…"),
// and the match is anchored on the character before it so a shorter id is not
// found inside a longer one — queue ids are hexadecimal, and one being the tail
// of another is ordinary, not unlikely.
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

// Timestamps at the head of a mail.log line, in the two formats postlogd
// writes (maillog_file in build/postfix-config.sh).
//
// syslogStampRe is the one that matches in practice today: the format is
// controlled by maillog_file_format, which arrived in Postfix 3.9, and the
// image is built on Debian's 3.7 — where the parameter does not exist and the
// only format is syslog's traditional one. It carries no year and no zone, so
// the stamp shown is a wall clock and nothing more, which is all this column
// claims to be.
//
// isoStampRe is for the RFC 3339 format that same parameter selects once the
// base image carries a Postfix new enough to offer it. Matching it first costs
// one failed anchor per line and means the upgrade needs no change here.
var (
	isoStampRe    = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}:\d{2})(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?\s`)
	syslogStampRe = regexp.MustCompile(`^([A-Z][a-z]{2}\s+\d{1,2} \d{2}:\d{2}:\d{2})\s`)
)

// SplitTimestamp separates the timestamp at the head of a mail.log line from
// the rest of it, so a page can put the two in their own columns. The stamp
// comes back without its fractional seconds and zone offset — five decimal
// places of microsecond are the widest part of the column and the least worth
// reading — but is otherwise the log's own wall clock, not converted: what is
// on the page is what is in the file.
//
// A line whose head is not a timestamp this recognises comes back whole, as
// rest, with an empty stamp. Nothing is ever dropped: the point of showing the
// log is that it says what it says.
func SplitTimestamp(line string) (stamp, rest string) {
	if m := isoStampRe.FindStringSubmatch(line); m != nil {
		return m[1] + " " + m[2], strings.TrimSpace(line[len(m[0]):])
	}
	if m := syslogStampRe.FindStringSubmatch(line); m != nil {
		return m[1], strings.TrimSpace(line[len(m[0]):])
	}
	return "", line
}

// follow tails path line by line, calling handle for each complete line, until
// ctx is cancelled. Where it starts is tr's decision (a persisted offset, the
// start of a file that changed while the panel was down, or end-of-file on a
// first ever run); it reopens the file when it is rotated (inode change from
// logrotate's create, or truncation from copytruncate) so nothing is missed.
//
// caughtUp is closed after the first read that reaches end-of-file, which is
// the point where every line the panel missed while it was down has been
// handled.
func follow(ctx context.Context, path string, tr *tracker, caughtUp chan struct{}, handle func(string)) error {
	var (
		f       *os.File
		r       *bufio.Reader
		info    os.FileInfo
		pending string
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
	if _, err := f.Seek(tr.resume(f), io.SeekStart); err != nil {
		log.Printf("log-tailer: seek %s: %v", path, err)
	}
	r.Reset(f) // the reader buffered from the pre-seek position
	defer func() {
		if f != nil {
			f.Close()
		}
	}()

	drain := func() {
		for {
			line, err := r.ReadString('\n')
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

	var once sync.Once

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			tr.record(f, read(), true) // shutdown: the next start resumes here
			return nil
		case <-ticker.C:
			drain()
			once.Do(func() { close(caughtUp) })
			ni, err := os.Stat(path)
			if err != nil {
				continue // file briefly gone mid-rotation; try again next tick
			}
			pos, _ := f.Seek(0, io.SeekCurrent)
			if !os.SameFile(info, ni) || ni.Size() < pos {
				// Rotated away or truncated: the old (renamed) inode may have
				// gained lines between the drain() above and this check, since
				// Postfix keeps writing to it until it reloads. Drain it once
				// more before switching so nothing in that gap is lost.
				drain()
				if err := openAt(0, io.SeekStart); err != nil {
					log.Printf("log-tailer: reopen %s: %v", path, err)
					continue
				}
				tr.adopt(f)
				continue
			}
			tr.record(f, read(), false)
		}
	}
}
