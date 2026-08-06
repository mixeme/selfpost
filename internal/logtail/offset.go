package logtail

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"os"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

const (
	// fingerprintSize is how many bytes from the head of the log identify it.
	// Postfix writes a timestamped line per event, so the first 512 bytes are
	// effectively unique per log generation — enough to tell "the file we were
	// reading" from "a fresh one created by logrotate while we were down",
	// which os.SameFile cannot answer across a restart.
	fingerprintSize = 512
	// persistInterval throttles the offset write. Losing up to this much
	// progress on a crash only means re-parsing a few lines (UpdateStatus is
	// idempotent), which is much cheaper than a database write per poll tick.
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
}

// resume returns the byte offset the tailer should start reading f at, having
// recorded f's fingerprint for later saves.
//
// The rules, in order: no stored state at all (first ever start) means start at
// the end, so installing the panel does not replay a pre-existing log; a stored
// state whose fingerprint still matches means continue from it, parsing the
// tail written while the panel was down; anything else means the file is not
// the one the offset referred to (rotated, recreated or truncated in the
// meantime), so read it from the start. Re-parsing lines already seen is
// harmless: UpdateStatus writes the same status onto the same row.
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
	case prev.Fingerprint != "" && prev.Fingerprint == t.fp && prev.Offset <= size:
		t.saved = prev.Offset
		if prev.Offset < size {
			log.Printf("log-tailer: resuming %s at offset %d (%d bytes to catch up)",
				t.path, prev.Offset, size-prev.Offset)
		}
		return prev.Offset
	default:
		log.Printf("log-tailer: %s changed while the panel was down; reading from the start", t.path)
		t.saved = 0
		return 0
	}
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

// fingerprintOf hashes the head of the file. It returns "" for a file too short
// to identify — the head would still change as Postfix appends, so such a
// fingerprint could not be compared meaningfully on the next start.
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
	return hex.EncodeToString(sum[:])
}

func fileSize(f *os.File) (int64, error) {
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
