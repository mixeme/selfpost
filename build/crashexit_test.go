// Package build holds the image's scripts; this file tests crashexit.py, the
// supervisor event listener that takes the container down.
package build

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// event is one event as supervisord writes it to a listener's stdin
// (supervisor 4.2.5, docs/events.rst: a header line whose len is the
// payload's length, then the payload).
func event(name, payload string) string {
	return fmt.Sprintf("ver:3.0 server:supervisor serial:1 pool:crashexit poolserial:1 eventname:%s len:%d\n%s", name, len(payload), payload)
}

// runListener feeds events to crashexit.py in dry run and returns what it
// wrote to stderr.
func runListener(t *testing.T, events ...string) string {
	t.Helper()
	// python3 on Linux; on Windows "python3" may be the Store's stub, which
	// runs nothing.
	names := []string{"python3", "python"}
	if runtime.GOOS == "windows" {
		names = []string{"python", "python3"}
	}
	python := ""
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			python = p
			break
		}
	}
	if python == "" {
		t.Skip("no python")
	}
	cmd := exec.Command(python, "crashexit.py")
	cmd.Env = append(os.Environ(), "CRASHEXIT_DRYRUN=1")
	cmd.Stdin = strings.NewReader(strings.Join(events, ""))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("crashexit.py: %v\n%s", err, stderr.String())
	}
	// Python on Windows writes its text stdout with \r\n.
	if n := strings.Count(strings.ReplaceAll(string(out), "\r\n", "\n"), "RESULT 2\nOK"); n != len(events) {
		t.Errorf("answered %d of %d events:\n%q", n, len(events), out)
	}
	return stderr.String()
}

// A program on the mail path that keeps crashing after its startsecs - never
// FATAL - takes the container down at the fifth unexpected exit (audit
// 2026-10-12, L32); expected exits, spared programs and fewer crashes do not.
func TestCrashexitCountsCrashes(t *testing.T) {
	exit := func(name, expected string) string {
		return event("PROCESS_STATE_EXITED", "processname:"+name+" groupname:"+name+" from_state:RUNNING expected:"+expected+" pid:7")
	}
	var four []string
	for i := 0; i < 4; i++ {
		four = append(four, exit("panel", "0"))
	}
	if got := runListener(t, four...); strings.Contains(got, "would shut down") {
		t.Errorf("four crashes shut down:\n%s", got)
	}
	if got := runListener(t, append(four, exit("panel", "0"))...); !strings.Contains(got, "panel exited unexpectedly 5 times") || !strings.Contains(got, "would shut down") {
		t.Errorf("five crashes did not shut down:\n%s", got)
	}
	var calm []string
	for i := 0; i < 6; i++ {
		calm = append(calm, exit("panel", "1"), exit("logrotate", "0"))
	}
	calm = append(calm, exit("postfix", "0"), exit("postfix", "0"), exit("postfix", "0"))
	if got := runListener(t, calm...); strings.Contains(got, "would shut down") {
		t.Errorf("expected exits, spared programs and three crashes of another shut down:\n%s", got)
	}
}

// FATAL of a mail-path program shuts down at once; of a spared one, not.
func TestCrashexitFatal(t *testing.T) {
	fatal := func(name string) string {
		return event("PROCESS_STATE_FATAL", "processname:"+name+" groupname:"+name+" from_state:BACKOFF")
	}
	if got := runListener(t, fatal("postfix")); !strings.Contains(got, "postfix entered FATAL; shutting down") || !strings.Contains(got, "would shut down") {
		t.Errorf("postfix FATAL:\n%s", got)
	}
	if got := runListener(t, fatal("logrotate"), fatal("cert-reload")); strings.Contains(got, "would shut down") {
		t.Errorf("a spared program's FATAL shut down:\n%s", got)
	}
}
