package health

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
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
	// OneShot marks a program that is started on demand and exits at once
	// (oneShotPrograms): idle is its healthy state, and it is not one of the
	// programs that are expected to be running.
	OneShot bool
}

// Processes returns the state of every supervised program (architecture.md §
// Image and processes — the three processes plus the reload/cert/logrotate
// helpers).
//
// The command takes fixed arguments and no user input, so it never goes through
// a shell (security.md). `supervisorctl status` deliberately exits non-zero when
// some program is not running, so the output is parsed first and the exit status
// only matters when nothing could be parsed from it. supervisor 4.2.5,
// supervisor/supervisorctl.py, do_status(): exit status 3 (NOT_RUNNING) when
// any program is in STOPPED_STATES, 4 when supervisord does not answer.
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
// skipped, so unexpected output cannot masquerade as a process. supervisor
// 4.2.5, supervisor/states.py, ProcessStates.
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
// supervisor 4.2.5, supervisor/supervisorctl.py, _show_statuses(): the
// template is "%(namespec)-<maxlen+3>s%(state)-10s%(desc)s" (maxlen at least
// 30, no state word longer than 8), and an unknown name prints
// "<name>: ERROR (no such process)".
func parseProcesses(out string) []Process {
	var procs []Process
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !supervisorStates[fields[1]] {
			continue
		}
		name, state := fields[0], fields[1]
		procs = append(procs, Process{
			Name:    name,
			State:   state,
			Detail:  strings.Join(fields[2:], " "),
			Status:  processStatus(name, state),
			OneShot: oneShotPrograms[name],
		})
	}
	return procs
}

// Uptime reads for how long a running program has been up from the detail
// supervisord prints for it: "pid 41, uptime 0:03:12", or with days in front,
// "pid 41, uptime 18 days, 3:02:11". supervisor 4.2.5,
// supervisor/rpcinterface.py, _interpretProcessInfo(): 'pid %s, uptime %s'
// with Python's str() of a timedelta from the spawn time ("1 day, ...").
func Uptime(detail string) (time.Duration, bool) {
	_, rest, found := strings.Cut(detail, "uptime ")
	if !found {
		return 0, false
	}
	var days int
	if d, clock, hasDays := strings.Cut(rest, ", "); hasDays {
		n, _, _ := strings.Cut(d, " ")
		v, err := strconv.Atoi(n)
		if err != nil || v < 0 {
			return 0, false
		}
		days, rest = v, clock
	}
	parts := strings.Split(strings.TrimSpace(rest), ":")
	if len(parts) != 3 {
		return 0, false
	}
	var hms [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return 0, false
		}
		hms[i] = v
	}
	return time.Duration(days)*24*time.Hour + time.Duration(hms[0])*time.Hour +
		time.Duration(hms[1])*time.Minute + time.Duration(hms[2])*time.Second, true
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
