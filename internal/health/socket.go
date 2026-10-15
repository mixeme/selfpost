package health

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

// Socket is the state of one milter socket Postfix connects to.
type Socket struct {
	Name string
	// Note qualifies the name where the socket is not one of the two Postfix
	// always has ("inbound"); the Health table shows it beside the name.
	Note    string
	Path    string
	Present bool
	Status  Status
	Detail  string
}

// CheckSocket checks a milter socket. required distinguishes the two milters:
// Postfix attaches OpenDKIM with default_action=tempfail, so a missing socket
// stops mail leaving the server, while the journal-milter is attached with
// default_action=accept and fails open — mail still goes out, only the send
// log stops being written (build/postfix-config.sh smtpd_milters; Postfix
// 3.10.13, src/milter/milter8.c, milter8_comm_error(): tempfail answers "451
// 4.7.1 Service unavailable - try again later", accept answers nothing).
//
// The socket is dialled, as Postfix will dial it; a file is not a listener.
// A unix socket's file outlives the process that bound it unless that process
// unlinks it, and a connect() to it then fails with ECONNREFUSED (unix(7)), so
// a killed OpenDKIM or panel leaves a file that refuses every connection -
// assumptions A-38. The connection is closed before a byte is sent, which
// none of the three milters logs: OpenDKIM's libmilter reads the EOF as
// SMFIC_EOF and ends the session without a line (libmilter 8.18.1,
// libmilter/comm.c, mi_rd_cmd(); engine.c, mi_engine()), and OpenDKIM's
// mlfi_close() has no connection to clean up (opendkim 2.11.0~beta2,
// opendkim/opendkim.c); go-milter v0.4.1 returns quietly on io.EOF
// (session.go, HandleMilterCommands()).
func CheckSocket(name, path string, required bool) Socket {
	s := Socket{Name: name, Path: path}
	if path == "" {
		s.Status = StatusUnknown
		s.Detail = "No socket path is configured."
		return s
	}
	fi, err := os.Stat(path)
	switch {
	case err != nil:
		s.Status = missingStatus(required)
		s.Detail = missingDetail(name, required)
	case fi.Mode()&os.ModeSocket == 0:
		s.Status = missingStatus(required)
		s.Detail = fmt.Sprintf("%s exists but is not a socket.", path)
	default:
		switch err := dialUnix(path); {
		case err == nil:
			s.Present = true
			s.Status = StatusOK
			s.Detail = "Listening"
		case errors.Is(err, syscall.EACCES):
			s.Status = StatusUnknown
			s.Detail = fmt.Sprintf("The panel may not connect to %s, so whether anything listens is not known.", path)
		case errors.Is(err, syscall.ECONNREFUSED):
			s.Status = missingStatus(required)
			s.Detail = fmt.Sprintf("Nothing listens on the %s socket: the program that made it has stopped. %s", name, missingCost(required))
		default:
			s.Status = missingStatus(required)
			s.Detail = fmt.Sprintf("The %s socket does not answer. %s", name, missingCost(required))
		}
	}
	return s
}

// unixDialTimeout bounds a dial of a local socket; a listener that is there
// answers at once, from the kernel's backlog.
const unixDialTimeout = 2 * time.Second

// dialUnix connects to the unix socket at path and closes the connection.
func dialUnix(path string) error {
	c, err := net.DialTimeout("unix", path, unixDialTimeout)
	if err != nil {
		return err
	}
	return c.Close()
}

func missingStatus(required bool) Status {
	if required {
		return StatusError
	}
	return StatusWarn
}

func missingDetail(name string, required bool) string {
	return fmt.Sprintf("The %s socket is missing. %s", name, missingCost(required))
}

// missingCost is what a milter that cannot be reached costs, by its
// default_action (see CheckSocket).
func missingCost(required bool) string {
	if required {
		return "Postfix rejects mail with a temporary error until it is back."
	}
	return "Mail still goes out, but the send log is not being written."
}
