package health

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCheckMilterInetAnswering(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("tcp unavailable here: %v", err)
	}
	defer l.Close()

	got := CheckMilter("Spam filter", "inet:"+l.Addr().String(), true)
	if got.Status != StatusOK || !got.Present || got.Detail != "Listening" {
		t.Errorf("listening address: %+v", got)
	}
	if got.Name != "Spam filter" || got.Path != "inet:"+l.Addr().String() {
		t.Errorf("name and path are not carried through: %+v", got)
	}
}

func TestCheckMilterInetRefused(t *testing.T) {
	// A port that was just free and is closed again.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("tcp unavailable here: %v", err)
	}
	addr := l.Addr().String()
	l.Close()

	got := CheckMilter("Spam filter", "inet:"+addr, true)
	if got.Present || got.Status != StatusError {
		t.Errorf("closed port, required: %+v", got)
	}
	if want := "Connection to " + addr + " refused."; got.Detail != want {
		t.Errorf("detail = %q, want %q", got.Detail, want)
	}
	if got := CheckMilter("Spam filter", "inet:"+addr, false); got.Status != StatusWarn {
		t.Errorf("closed port, optional: status %q", got.Status)
	}
}

// What a failed dial says is a sentence about host:port, never the Go error.
func TestMilterDialDetail(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"timeout", timeoutErr{}, "No answer from antispam:11332 within 1 s."},
		{"refused", &net.OpError{Op: "dial", Err: errors.New("connectex: No connection could be made because the target machine actively refused it.")}, "Connection to antispam:11332 refused."},
		{"unix-style refused", &net.OpError{Op: "dial", Err: errors.New("connect: connection refused")}, "Connection to antispam:11332 refused."},
		{"no such host", &net.DNSError{Err: "no such host", Name: "antispam", IsNotFound: true}, "The name antispam could not be resolved."},
		{"other", errors.New("dial tcp 10.0.0.1:11332: some internal thing 0xc000"), "Could not connect to antispam:11332."},
	} {
		got := milterDialDetail(c.err, "antispam", "11332")
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
		if strings.Contains(got, "dial tcp") || strings.Contains(got, "0xc0") {
			t.Errorf("%s: raw error leaked: %q", c.name, got)
		}
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestCheckMilterUnix(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "filter.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix sockets unavailable here: %v", err)
	}
	defer l.Close()

	if got := CheckMilter("Spam filter", "unix:"+sock, true); got.Status != StatusOK || !got.Present {
		t.Errorf("live socket: %+v", got)
	}

	missing := filepath.Join(dir, "gone.sock")
	got := CheckMilter("Spam filter", "unix:"+missing, true)
	if got.Present || got.Status != StatusError || got.Detail != "The socket file does not exist." {
		t.Errorf("missing socket, required: %+v", got)
	}
	if got := CheckMilter("Spam filter", "unix:"+missing, false); got.Status != StatusWarn {
		t.Errorf("missing socket, optional: status %q", got.Status)
	}
}

func TestCheckMilterUnixNotASocket(t *testing.T) {
	plain := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(plain, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got := CheckMilter("Spam filter", "unix:"+plain, true)
	if got.Present || got.Status != StatusError || got.Detail != "The path exists but is not a socket." {
		t.Errorf("regular file in place of a socket: %+v", got)
	}
}

func TestCheckMilterNotCheckable(t *testing.T) {
	for _, addr := range []string{
		"", "antispam:11332", "tcp:antispam:11332", "inet:antispam", "inet::11332", "inet:antispam:", "inet:antispam:http",
		"inet:antispam:0", "inet:antispam:70000", "unix:", "unix", "inet:", "  ",
	} {
		got := CheckMilter("Spam filter", addr, true)
		if got.Status != StatusUnknown || got.Present {
			t.Errorf("%q: status %q present=%v, want unknown and not present", addr, got.Status, got.Present)
		}
		if got.Detail == "" {
			t.Errorf("%q: no detail", addr)
		}
	}
	got := CheckMilter("Spam filter", "antispam:11332", true)
	if want := "The address is not unix:/path or inet:host:port, so it cannot be checked."; got.Detail != want {
		t.Errorf("detail = %q, want %q", got.Detail, want)
	}
}

// inet:[::1]:port is the form Postfix takes for an IPv6 address.
func TestCheckMilterInet6(t *testing.T) {
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable here: %v", err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	if _, err := strconv.Atoi(port); err != nil {
		t.Fatal(err)
	}
	if got := CheckMilter("Spam filter", "inet:[::1]:"+port, true); got.Status != StatusOK {
		t.Errorf("IPv6 listener: %+v", got)
	}
}
