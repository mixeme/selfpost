package logtail

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

const (
	// fingerprintSize is how many bytes from the head of the log identify it.
	// Postfix writes a timestamped line per event (Postfix 3.7.11,
	// src/util/msg_logger.c, msg_logger_print(): "%b %d %H:%M:%S" on every
	// record), so the first 512 bytes are
	// effectively unique per log generation — enough to tell "the file we were
	// reading" from "a fresh one created by logrotate while we were down",
	// which os.SameFile cannot answer across a restart.
	fingerprintSize = 512
	// persistInterval throttles the offset write. Losing up to this much
	// progress on a crash means re-parsing a few lines, which is much cheaper
	// than a database write per poll tick. For a delivery status that changes
	// nothing (UpdateStatus is idempotent), and a refusal among them is not
	// written to the inbound journal a second time (inboundJournal.handle).
	persistInterval = 5 * time.Second
)

// tracker persists the tailer's read position so a restart resumes where the
// previous run stopped instead of jumping to end-of-file — the "send-log rows
// stay queued forever" gap (architecture.md § Log tailer). It is used from the
// follow loop only, so it needs no locking.
type tracker struct {
	st   StatusStore
	path string

	fp       string // fingerprint of the file currently open ("" if too short)
	saved    int64  // last offset written to the store
	lastSave time.Time

	// behind is set by resume when the saved place belongs to a file that is
	// no longer the log - rotated while the panel was down; prev is that place.
	behind bool
	prev   store.LogtailState

	// line is the place of the line being handled, lineStart the offset it
	// starts at: set by follow and readRotated before each call of handle.
	line      Place
	lineStart int64

	// hold, when set, names the earliest place in a file that something read
	// from the log still waits on (a refusal's line, a refusal to recover):
	// no offset past it is saved, so a stop - clean or not - before the wait
	// is over reads it again (review of stage 2b).
	hold func(fp string) (Place, bool)
}

// Place is where a line of the log ends: the fingerprint of its file and the
// byte offset just past it. A rotated file keeps its fingerprint (the inode
// is part of it), so a place stays true across a rotation.
type Place struct {
	FP  string
	End int64
}

// String is the place as it is saved: "<fingerprint>@<offset>".
func (p Place) String() string { return fmt.Sprintf("%s@%d", p.FP, p.End) }

// parsePlace reads a saved place; the zero Place for anything else.
func parsePlace(s string) Place {
	i := strings.LastIndexByte(s, '@')
	if i < 0 {
		return Place{}
	}
	var end int64
	if _, err := fmt.Sscan(s[i+1:], &end); err != nil {
		return Place{}
	}
	return Place{FP: s[:i], End: end}
}

// resume returns the byte offset the tailer should start reading f at, having
// recorded f's fingerprint for later saves.
//
// The rules, in order: no stored state at all (first ever start) means start at
// the end, so installing the panel does not replay a pre-existing log; a stored
// state whose fingerprint still matches means continue from it, parsing the
// tail written while the panel was down; anything else means the file is not
// the one the offset referred to (rotated, recreated or truncated in the
// meantime), so read it from the start. Re-parsing lines already seen writes
// the same status onto the same send-log row, and no second inbound journal
// row for a refusal among them.
func (t *tracker) resume(f *os.File) int64 {
	size, err := fileSize(f)
	if err != nil {
		log.Printf("log-tailer: stat %s: %v (reading from the start)", t.path, err)
		return 0
	}
	t.fp = fingerprintOf(f)

	prev, ok, err := t.st.LogtailState(t.path)
	if err != nil {
		log.Printf("log-tailer: read stored offset: %v (starting at end)", err)
		return size
	}
	switch {
	case !ok:
		t.saved = size
		return size
	case prev.Fingerprint != "" && sameLog(prev.Fingerprint, t.fp) && prev.Offset <= size:
		t.saved = prev.Offset
		if prev.Offset < size {
			log.Printf("log-tailer: resuming %s at offset %d (%d bytes to catch up)",
				t.path, prev.Offset, size-prev.Offset)
		}
		return prev.Offset
	default:
		log.Printf("log-tailer: %s changed while the panel was down; reading from the start", t.path)
		t.saved = 0
		t.behind, t.prev = prev.Fingerprint != "", prev
		return 0
	}
}

// readRotated reads, when the log was rotated while the panel was down, the
// rest of the file the saved place belongs to: logrotate renamed it to
// mail.log.1 (delaycompress leaves it plain - logrotate 3.21.0, logrotate.c,
// prerotateSingleLog()), and what Postfix wrote there
// between the panel's stop and the rotation - deliveries, refusals, deferrals -
// was otherwise never read (audit 2026-10-12, stage 2b). It is read from the
// saved offset to its end, line by line through handle, before the new file
// is read from its start. A mail.log.1 that is not that file - the log
// rotated more than once, or the file is gone - is said in the log, with the
// time from which nothing was read. It reports whether it read anything.
func (t *tracker) readRotated(handle func(string)) bool {
	if !t.behind {
		return false
	}
	t.behind = false
	rotated := t.path + ".1"
	lost := func(why string) {
		log.Printf("log-tailer: %s; what was written to %s from %s until it was rotated was not read",
			why, t.path, t.prev.UpdatedAt.UTC().Format(time.RFC3339))
	}
	f, err := os.Open(rotated)
	if err != nil {
		lost(fmt.Sprintf("%s cannot be opened (%v)", rotated, err))
		return false
	}
	defer f.Close()
	size, err := fileSize(f)
	if err != nil || !sameLog(t.prev.Fingerprint, fingerprintOf(f)) || t.prev.Offset > size {
		lost(fmt.Sprintf("%s is not the file the saved place belongs to (rotated more than once while the panel was down?)", rotated))
		return false
	}
	if _, err := f.Seek(t.prev.Offset, io.SeekStart); err != nil {
		lost(fmt.Sprintf("seek %s: %v", rotated, err))
		return false
	}
	fp := fingerprintOf(f)
	end := t.prev.Offset
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		t.lineStart = end
		end += int64(len(line))
		if line != "" {
			t.line = Place{FP: fp, End: end}
			handle(strings.TrimRight(line, "\r\n"))
		}
		if err != nil {
			break
		}
	}
	log.Printf("log-tailer: read %s from offset %d to its end (%d bytes) before the new %s",
		rotated, t.prev.Offset, size-t.prev.Offset, t.path)
	return true
}

// sameLog says whether a saved fingerprint is the current file's. One saved
// before the fingerprint carried the file's identity is its hash alone, and is
// compared with the hash.
func sameLog(saved, current string) bool {
	if saved == current {
		return true
	}
	if !strings.Contains(saved, ":") {
		hash, _, _ := strings.Cut(current, ":")
		return saved != "" && saved == hash
	}
	return false
}

// adopt re-fingerprints after the follow loop switched to a rotated-in file and
// persists the fresh start immediately, so a restart right after a rotation
// does not resume at the old file's offset.
func (t *tracker) adopt(f *os.File) {
	t.fp = fingerprintOf(f)
	t.saved = -1 // force the write below even if the old offset happened to be 0
	t.record(f, 0, true)
}

// record persists offset, at most once per persistInterval unless force is set
// (rotation and shutdown, where the write must not be skipped).
func (t *tracker) record(f *os.File, offset int64, force bool) {
	if t.hold != nil {
		if p, ok := t.hold(t.fp); ok && p.End < offset {
			offset = p.End
		}
	}
	if offset == t.saved {
		return
	}
	if !force && time.Since(t.lastSave) < persistInterval {
		return
	}
	if t.fp == "" {
		// The log was shorter than a fingerprint when we opened it; now that it
		// has grown, an identifiable one may be available.
		t.fp = fingerprintOf(f)
	}
	if err := t.st.SaveLogtailState(t.path, store.LogtailState{Fingerprint: t.fp, Offset: offset}); err != nil {
		log.Printf("log-tailer: save offset: %v", err)
		return
	}
	t.saved = offset
	t.lastSave = time.Now()
}

// fingerprintOf hashes the head of the file and adds the file's identity
// (device and inode, where there is one): two log files whose first bytes
// match are still two files. It returns "" for a file too short to identify —
// the head would still change as Postfix appends, so such a fingerprint could
// not be compared meaningfully on the next start.
func fingerprintOf(f *os.File) string {
	buf := make([]byte, fingerprintSize)
	n, err := f.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		log.Printf("log-tailer: fingerprint read: %v", err)
		return ""
	}
	if n < fingerprintSize {
		return ""
	}
	sum := sha256.Sum256(buf)
	fp := hex.EncodeToString(sum[:])
	if id := fileID(f); id != "" {
		fp += ":" + id
	}
	return fp
}

func fileSize(f *os.File) (int64, error) {
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
