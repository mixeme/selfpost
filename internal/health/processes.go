package health

import (
	"fmt"
	"os/exec"
	"strings"
)

// supervisorConf is the supervisord configuration the panel's other control
// calls already address (see internal/postfix and internal/domain).
const supervisorConf = "/etc/supervisor/supervisord.conf"

// oneShotPrograms are supervisord entries that are meant to sit idle: they are
// started on demand and exit immediately, so STOPPED/EXITED is their healthy
// state rather than a fault (see build/supervisord.conf).
var oneShotPrograms = map[string]bool{
	"postfix-reload": true,
}

// Process is one supervised program as supervisord reports it.
type Process struct {
	Name   string
	State  string // supervisord's own state word, e.g. RUNNING
	Detail string // the rest of the line: pid/uptime, or exit information
	Status Status
}

// Processes returns the state of every supervised program (spec 4's three
// processes plus the reload/cert/logrotate helpers).
//
// The command takes fixed arguments and no user input, so it never goes through
// a shell (spec 7.6.3). `supervisorctl status` deliberately exits non-zero when
// some program is not running, so the output is parsed first and the exit status
// only matters when nothing could be parsed from it.
func Processes() ([]Process, error) {
	cmd := exec.Command("supervisorctl", "-c", supervisorConf, "status")
	out, err := cmd.CombinedOutput()
	procs := parseProcesses(string(out))
	if len(procs) == 0 {
		if err != nil {
			return nil, fmt.Errorf("supervisorctl status: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil, fmt.Errorf("supervisorctl status: no programs reported")
	}
	return procs, nil
}

// supervisorStates are the state words supervisord prints. Lines whose second
// field is not one of them are not status lines (banners, error text) and are
// skipped, so unexpected output cannot masquerade as a process.
var supervisorStates = map[string]bool{
	"STOPPED":  true,
	"STARTING": true,
	"RUNNING":  true,
	"BACKOFF":  true,
	"STOPPING": true,
	"EXITED":   true,
	"FATAL":    true,
	"UNKNOWN":  true,
}

// parseProcesses turns supervisorctl's tabular output into Process values. Each
// status line is "<name> <STATE> <detail...>", column-aligned with spaces.
func parseProcesses(out string) []Process {
	var procs []Process
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !supervisorStates[fields[1]] {
			continue
		}
		name, state := fields[0], fields[1]
		procs = append(procs, Process{
			Name:   name,
			State:  state,
			Detail: strings.Join(fields[2:], " "),
			Status: processStatus(name, state),
		})
	}
	return procs
}

// processStatus grades a supervisord state. A one-shot program that is not
// running is healthy; anything else that is not RUNNING means a component of
// the mail path is down or flapping.
func processStatus(name, state string) Status {
	switch state {
	case "RUNNING":
		return StatusOK
	case "STARTING", "STOPPING":
		return StatusWarn
	case "STOPPED", "EXITED":
		if oneShotPrograms[name] {
			return StatusOK
		}
		return StatusError
	default: // BACKOFF, FATAL, UNKNOWN
		return StatusError
	}
}
