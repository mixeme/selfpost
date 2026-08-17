package e2e

import (
	"fmt"
	"strings"
	"time"
)

// wantRunning are the supervisord programs that must be RUNNING once the
// container is up (build/supervisord.conf). postfix-reload is deliberately
// excluded: autostart=false, its healthy resting state is STOPPED.
var wantRunning = []string{"opendkim", "panel", "postfix", "cert-reload", "logrotate"}

// checkSupervisorProcesses shells into the container directly (not through the
// panel's /status page) so it works before an administrator account even
// exists — this is the first thing the harness checks after `docker compose
// up` (plan C.4). Programs take a moment to leave STARTING right after the
// container starts, so this polls rather than checking once.
func checkSupervisorProcesses(s *stack) error {
	var lastErr error
	err := waitFor("all supervised programs to reach their steady state", 20*time.Second, 500*time.Millisecond, func() (bool, error) {
		out, execErr := s.execIn("selfpost", "supervisorctl", "-c", "/etc/supervisor/supervisord.conf", "status")
		// supervisorctl exits non-zero when any program is not RUNNING, which
		// is expected for postfix-reload — parse the output regardless of exit
		// status.
		states := parseSupervisorStatus(out)
		if len(states) == 0 {
			lastErr = fmt.Errorf("supervisorctl status produced nothing to parse: %v\n%s", execErr, out)
			return false, lastErr
		}
		for _, name := range wantRunning {
			state, ok := states[name]
			if !ok {
				lastErr = fmt.Errorf("program %q not reported by supervisorctl:\n%s", name, out)
				return false, lastErr
			}
			if state != "RUNNING" {
				lastErr = fmt.Errorf("program %q is %s, want RUNNING:\n%s", name, state, out)
				return false, lastErr
			}
		}
		if state, ok := states["postfix-reload"]; ok && state != "STOPPED" {
			lastErr = fmt.Errorf("program postfix-reload is %s, want STOPPED (autostart=false):\n%s", state, out)
			return false, lastErr
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("%w\n==== selfpost logs ====\n%s", err, s.logs("selfpost"))
	}
	return nil
}

func parseSupervisorStatus(out string) map[string]string {
	states := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		states[fields[0]] = fields[1]
	}
	return states
}

// checkInboundRelayOff asserts the default image does not accept mail on
// port 25: smtp/inet is absent from master.cf (Debian's stock listener is
// removed when INBOUND_RELAY_ENABLE is not true).
func checkInboundRelayOff(s *stack) error {
	out, err := s.execIn("selfpost", "postconf", "-M", "smtp/inet")
	combined := out
	if err != nil {
		combined += err.Error()
	}
	if strings.Contains(combined, "smtpd") && !strings.Contains(combined, "warning:") && !strings.Contains(combined, "fatal:") {
		return fmt.Errorf("inbound smtp/inet is present while INBOUND_RELAY_ENABLE is off:\n%s", out)
	}
	return nil
}
