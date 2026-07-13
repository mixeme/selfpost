// Package logtail follows Postfix's mail.log and reconciles the send-log
// delivery statuses the journal-milter could not know at receive time (spec
// 7.3). A milter row starts life as "queued"; Postfix only decides sent /
// deferred / bounced later, per recipient, and reports it in mail.log. This
// package parses those lines by queue-id + recipient and advances the matching
// rows, and prunes rows past the retention window.
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
	"time"

	"codeberg.org/mix/selfpost/internal/store"
)

// StatusStore is the slice of the store the log-tailer needs. *store.Store
// satisfies it.
type StatusStore interface {
	UpdateStatus(queueID, recipient, status string) (int64, error)
	DeleteSendLogBefore(cutoff time.Time) (int64, error)
}

// pollInterval is how often the tail loop checks for new bytes / rotation. It
// is a var so tests can shorten it.
var pollInterval = time.Second

const (
	// retentionInterval is how often the retention sweep runs (also once at
	// startup). The window itself is configurable; the cadence need not be.
	retentionInterval = 6 * time.Hour
	// defaultRetentionDays applies when the configured value is unset/invalid
	// (spec 7.3).
	defaultRetentionDays = 90
)

// deliveryRe matches a Postfix delivery line and captures queue-id, recipient
// and status, e.g.
//
//	postfix/smtp[26]: 41E862C00D9E: to=<a@example.net>, relay=…, dsn=2.0.0, status=sent (250 OK)
//
// The "<queue-id>: to=<addr>, …, status=<word>" shape is specific to the
// delivery agents; qmgr/smtpd/cleanup lines do not match.
var deliveryRe = regexp.MustCompile(`\b([0-9A-Za-z]+): to=<([^>]*)>,.*\bstatus=(\w+)`)

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
// a background sweep prunes rows older than retentionDays. It returns nil on a
// clean shutdown.
func Run(ctx context.Context, path string, st StatusStore, retentionDays int) error {
	go retentionLoop(ctx, st, retentionDays)

	return follow(ctx, path, func(line string) {
		queueID, recipient, status, ok := parseDelivery(line)
		if !ok {
			return
		}
		if _, err := st.UpdateStatus(queueID, recipient, status); err != nil {
			log.Printf("log-tailer: update %s/%s -> %s: %v", queueID, recipient, status, err)
		}
	})
}

// retentionLoop prunes expired send-log rows immediately and then periodically.
func retentionLoop(ctx context.Context, st StatusStore, retentionDays int) {
	if retentionDays <= 0 {
		retentionDays = defaultRetentionDays
	}
	prune := func() {
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
// panel's mail.log monitoring view (spec 7.2.13). It is a one-shot,
// point-in-time read on request — unrelated to the background follow loop
// above — that reads backwards in chunks so it stays cheap against a
// multi-megabyte log rather than reading the whole file every poll.
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

// follow tails path line by line, calling handle for each complete line, until
// ctx is cancelled. It starts at end-of-file (so a restart does not reprocess
// history) and reopens the file when it is rotated (inode change from
// logrotate's create, or truncation from copytruncate) so nothing is missed.
func follow(ctx context.Context, path string, handle func(string)) error {
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
		if err := openAt(0, io.SeekEnd); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(pollInterval):
		}
	}
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

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			drain()
			ni, err := os.Stat(path)
			if err != nil {
				continue // file briefly gone mid-rotation; try again next tick
			}
			pos, _ := f.Seek(0, io.SeekCurrent)
			if !os.SameFile(info, ni) || ni.Size() < pos {
				// Rotated away or truncated: reopen from the start of the new
				// file. Any tail of the old file was already drained above.
				if err := openAt(0, io.SeekStart); err != nil {
					log.Printf("log-tailer: reopen %s: %v", path, err)
				}
			}
		}
	}
}
