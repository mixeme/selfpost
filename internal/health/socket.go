package health

import (
	"fmt"
	"os"
)

// Socket is the state of one milter socket Postfix connects to.
type Socket struct {
	Name    string
	Path    string
	Present bool
	Status  Status
	Detail  string
}

// CheckSocket stats a milter socket. required distinguishes the two milters:
// OpenDKIM runs with default_action=tempfail, so a missing socket stops mail
// leaving the server, while the journal-milter fails open — mail still goes out,
// only the send log stops being written.
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
		s.Present = true
		s.Status = StatusOK
		s.Detail = "Listening."
	}
	return s
}

func missingStatus(required bool) Status {
	if required {
		return StatusError
	}
	return StatusWarn
}

func missingDetail(name string, required bool) string {
	if required {
		return fmt.Sprintf("The %s socket is missing. Postfix rejects mail with a temporary error until it is back.", name)
	}
	return fmt.Sprintf("The %s socket is missing. Mail still goes out, but the send log is not being written.", name)
}
