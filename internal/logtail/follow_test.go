package logtail

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

// A partial line belongs to neither handle nor the saved offset until its
// newline arrives. When it does, its Place still covers the whole line.
func TestFollowerCompletesAPartialLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.log")
	if err := os.WriteFile(path, []byte("part"), 0o644); err != nil {
		t.Fatal(err)
	}
	type handled struct {
		text  string
		place Place
		start int64
	}
	var got []handled
	tr := &tracker{st: &captureStore{}, path: path, fp: "fp"}
	f := &follower{path: path, tr: tr, flush: func() {}, handle: func(line string) {
		got = append(got, handled{text: line, place: tr.line, start: tr.lineStart})
	}}
	if err := f.openAt(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	defer func() { f.f.Close() }()

	f.drainCurrent()
	if len(got) != 0 || f.pending != "part" || f.readOffset() != 0 {
		t.Fatalf("partial line was consumed: got=%v pending=%q offset=%d", got, f.pending, f.readOffset())
	}
	appendLine(t, path, " two\nnext")
	f.drainCurrent()

	want := []handled{
		{text: "part two", place: Place{FP: "fp", End: 9}, start: 0},
		{text: "next", place: Place{FP: "fp", End: 14}, start: 9},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("handled = %#v, want %#v", got, want)
	}
	if f.pending != "" || f.readOffset() != 14 {
		t.Fatalf("after completion: pending=%q offset=%d", f.pending, f.readOffset())
	}
}

// The buffered line writers must flush before the offset past their lines is
// persisted; otherwise a crash could make those lines unreachable on restart.
func TestFollowerFlushesBeforeSavingTheOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.log")
	if err := os.WriteFile(path, []byte("line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var order []string
	st := &saveOrderStore{order: &order}
	tr := &tracker{st: st, path: path, fp: "fp"}
	f := &follower{
		path: path, tr: tr, caughtUp: make(chan struct{}),
		flush: func() { order = append(order, "flush") }, handle: func(string) {},
		blind: &blindness{add: st.AddTailerBlind},
	}
	if err := f.openAt(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	defer func() { f.f.Close() }()

	f.tick()
	if !reflect.DeepEqual(order, []string{"flush", "save"}) {
		t.Fatalf("order = %v, want flush then save", order)
	}
}

type saveOrderStore struct {
	captureStore
	order *[]string
}

func (s *saveOrderStore) SaveLogtailState(path string, st store.LogtailState) error {
	*s.order = append(*s.order, "save")
	return s.captureStore.SaveLogtailState(path, st)
}

// copytruncate keeps the inode but makes the file shorter than the descriptor
// position. The follower starts the replacement at zero and does not retain a
// rotated tail for it.
func TestFollowerReopensATruncatedLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.log")
	if err := os.WriteFile(path, []byte("old line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := &captureStore{}
	var got []string
	tr := &tracker{st: cs, path: path, fp: "fp"}
	f := &follower{
		path: path, tr: tr, caughtUp: make(chan struct{}),
		flush: func() {}, handle: func(line string) { got = append(got, line) },
		blind: &blindness{add: cs.AddTailerBlind},
	}
	if err := f.openAt(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	defer func() { f.f.Close() }()
	f.drainCurrent()
	got = nil

	if err := os.WriteFile(path, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.tick()
	if f.old != nil || f.readOffset() != 0 {
		t.Fatalf("truncation left old=%v offset=%d", f.old != nil, f.readOffset())
	}
	f.tick()
	if !reflect.DeepEqual(got, []string{"new"}) {
		t.Fatalf("read after truncation = %v, want only the new line", got)
	}
}

// A second rotation within rotatedGrace replaces the previous rotated tail:
// writes to that older file are no longer part of the current log stream.
func TestFollowSecondRotationDropsThePreviousTail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot rename a log file while the tailer has it open")
	}
	old := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = old })

	path := filepath.Join(t.TempDir(), "mail.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cs := &captureStore{}
	stop := startRun(t, path, cs)
	defer stop()

	appendLine(t, path, "host postfix/smtp[1]: Q1: to=<a@example.net>, relay=x, dsn=2.0.0, status=sent (ok)")
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q1|a@example.net|sent") })
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	appendLine(t, path, "host postfix/smtp[1]: Q2: to=<b@example.net>, relay=x, dsn=2.0.0, status=sent (ok)")
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q2|b@example.net|sent") })

	if err := os.Rename(path, path+".2"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	appendLine(t, path, "host postfix/smtp[1]: Q3: to=<c@example.net>, relay=x, dsn=2.0.0, status=sent (ok)")
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q3|c@example.net|sent") })

	appendLine(t, path+".1", "host postfix/smtp[1]: QX: to=<x@example.net>, relay=x, dsn=2.0.0, status=sent (stale)")
	appendLine(t, path, "host postfix/smtp[1]: Q4: to=<d@example.net>, relay=x, dsn=2.0.0, status=sent (ok)")
	waitFor(t, func() bool { return contains(cs.snapshot(), "Q4|d@example.net|sent") })
	if contains(cs.snapshot(), "QX|x@example.net|sent") {
		t.Fatal("the previous rotated tail was still read after a second rotation")
	}
}
