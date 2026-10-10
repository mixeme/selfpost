package health

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// milterDialTimeout caps the TCP probe. The Health page and Overview run every
// check on each poll, one after another, so this is the most a dead address can
// add to a poll.
const milterDialTimeout = time.Second

// CheckMilter probes a milter address in the two forms Postfix takes:
// unix:/path is stat-ed like any milter socket, and inet:host:port is dialled
// and the connection closed at once, with nothing written. required says what
// a failure costs, as for CheckSocket: an error when mail stops without the
// milter, a warning when it only goes through unscreened. An address in neither
// form is reported as not checkable (StatusUnknown), never as fine. Details say
// what failed in a sentence; they never carry the Go error.
func CheckMilter(name, addr string, required bool) Socket {
	return checkMilter(name, addr, required, milterDialTimeout)
}

func checkMilter(name, addr string, required bool, timeout time.Duration) Socket {
	s := Socket{Name: name, Path: addr}
	addr = strings.TrimSpace(addr)
	if addr == "" {
		s.Status = StatusUnknown
		s.Detail = "No address is configured."
		return s
	}

	fail := func(detail string) Socket {
		s.Status = missingStatus(required)
		s.Detail = detail
		return s
	}

	switch {
	case strings.HasPrefix(addr, "unix:") && len(addr) > len("unix:"):
		path := strings.TrimPrefix(addr, "unix:")
		fi, err := os.Stat(path)
		switch {
		case err != nil:
			return fail("The socket file does not exist.")
		case fi.Mode()&os.ModeSocket == 0:
			return fail("The path exists but is not a socket.")
		}
	case strings.HasPrefix(addr, "inet:"):
		host, port, ok := splitInet(strings.TrimPrefix(addr, "inet:"))
		if !ok {
			return notCheckable(s)
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
		if err != nil {
			return fail(milterDialDetail(err, host, port))
		}
		_ = conn.Close()
	default:
		return notCheckable(s)
	}

	s.Present = true
	s.Status = StatusOK
	s.Detail = "Listening"
	return s
}

func notCheckable(s Socket) Socket {
	s.Status = StatusUnknown
	s.Detail = "The address is not unix:/path or inet:host:port, so it cannot be checked."
	return s
}

// splitInet reads host:port (or [v6]:port) with a numeric port.
func splitInet(hp string) (host, port string, ok bool) {
	host, port, err := net.SplitHostPort(hp)
	if err != nil || host == "" {
		return "", "", false
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", "", false
	}
	return host, port, true
}

// milterDialDetail turns a failed dial into a sentence that names only host and
// port.
func milterDialDetail(err error, host, port string) string {
	hp := net.JoinHostPort(host, port)
	var dns *net.DNSError
	var nerr net.Error
	switch {
	case errors.As(err, &dns):
		return fmt.Sprintf("The name %s could not be resolved.", host)
	case errors.As(err, &nerr) && nerr.Timeout():
		return fmt.Sprintf("No answer from %s within %d s.", hp, int(milterDialTimeout/time.Second))
	case errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(strings.ToLower(err.Error()), "refused"):
		return fmt.Sprintf("Connection to %s refused.", hp)
	}
	return fmt.Sprintf("Could not connect to %s.", hp)
}
