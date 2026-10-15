package syslogrelay

import (
	"bytes"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLine(t *testing.T) {
	for in, want := range map[string]string{
		"<22>Oct 15 09:00:00 opendkim[45]: 3A2B: DKIM-Signature field added (s=sp, d=example.com)\n": "syslog: opendkim[45]: 3A2B: DKIM-Signature field added (s=sp, d=example.com)",
		"<19>Oct  5 09:00:00 opendkim[45]: key retrieval failed":                                     "syslog: error: opendkim[45]: key retrieval failed",
		"<20>opendkim[45]: no timestamp":                                                             "syslog: opendkim[45]: no timestamp",
		"not syslog at all":                                                                          "syslog: not syslog at all",
		"<x>Oct 15 09:00:00 odd":                                                                     "syslog: <x>Oct 15 09:00:00 odd",
		// A line of its own cannot be made: a newline inside is written out.
		"<14>opendkim[1]: one\npanel: forged": `syslog: opendkim[1]: one\x0apanel: forged`,
	} {
		if got := Line([]byte(in)); got != want {
			t.Errorf("Line(%q) = %q, want %q", in, got, want)
		}
	}
}

// syncBuffer is a bytes.Buffer the relay and the test may share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// startRelay runs the relay on a socket of its own and returns a connection
// to it.
func startRelay(t *testing.T, w *syncBuffer) net.Conn {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no unixgram sockets on Windows; the container is Linux")
	}
	path := filepath.Join(t.TempDir(), "log")
	go Run(path, w)
	var conn net.Conn
	var err error
	for i := 0; i < 50; i++ {
		if conn, err = net.Dial("unixgram", path); err == nil {
			t.Cleanup(func() { conn.Close() })
			return conn
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(err)
	return nil
}

// What a program sends to the socket comes out as a line.
func TestRunRelaysADatagram(t *testing.T) {
	out := &syncBuffer{}
	conn := startRelay(t, out)
	if _, err := conn.Write([]byte("<19>Oct 15 09:00:00 opendkim[7]: can't load key")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(out.String(), "\n") {
		time.Sleep(10 * time.Millisecond)
	}
	if got := out.String(); got != "syslog: error: opendkim[7]: can't load key\n" {
		t.Errorf("relayed %q", got)
	}
}

// A flood is cut at the rate, and what was dropped is said.
func TestRunDropsAFlood(t *testing.T) {
	out := &syncBuffer{}
	conn := startRelay(t, out)
	for i := 0; i < ratePerSecond+100; i++ {
		if _, err := conn.Write([]byte("<14>flood")); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := conn.Write([]byte("<14>after")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(out.String(), "syslog: after") {
		time.Sleep(10 * time.Millisecond)
	}
	got := out.String()
	if n := strings.Count(got, "syslog: flood\n"); n != ratePerSecond {
		t.Errorf("%d flood lines written, want %d", n, ratePerSecond)
	}
	if !strings.Contains(got, "syslog: relay: dropped 100 lines") {
		t.Errorf("the drop was not said:\n%s", got)
	}
}
