package logtail

import (
	"bufio"
	"context"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

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

// blindMin is a var so the file-integration test can measure a short blind
// stretch without waiting through the production threshold.
var blindMin = 3 * time.Second

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
