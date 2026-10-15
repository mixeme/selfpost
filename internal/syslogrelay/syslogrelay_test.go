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
		"<22>Oct 15 09:00:00 opendkim[45]: 3A2B: DKIM-Signature field added (s=sp, d=example.com)\n": "opendkim[45]: 3A2B: DKIM-Signature field added (s=sp, d=example.com)",
		"<19>Oct  5 09:00:00 opendkim[45]: key retrieval failed":                                     "error: opendkim[45]: key retrieval failed",
		"<20>opendkim[45]: no timestamp":                                                             "opendkim[45]: no timestamp",
		"not syslog at all":                                                                          "not syslog at all",
		"<x>Oct 15 09:00:00 odd":                                                                     "<x>Oct 15 09:00:00 odd",
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

// What a program sends to the socket comes out as a line.
func TestRunRelaysADatagram(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unixgram sockets on Windows; the container is Linux")
	}
	path := filepath.Join(t.TempDir(), "log")
	out := &syncBuffer{}
	go Run(path, out)
	var conn net.Conn
	var err error
	for i := 0; i < 50; i++ {
		if conn, err = net.Dial("unixgram", path); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("<19>Oct 15 09:00:00 opendkim[7]: can't load key")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(out.String(), "\n") {
		time.Sleep(10 * time.Millisecond)
	}
	if got := out.String(); got != "error: opendkim[7]: can't load key\n" {
		t.Errorf("relayed %q", got)
	}
}
