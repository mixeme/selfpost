package e2e

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Supervision as supervisord does it (audit 2026-10-12, stage 2c; assumptions
// C-P42, C-V5, C-V6, C-V10). The stand runs with MILTER_WAIT_TIMEOUT=10
// (compose.override.yml), so program postfix has startsecs 20.

const supervisorConfPath = "/etc/supervisor/supervisord.conf"

// supervisorctl runs supervisorctl in the container; its output comes back
// whatever its exit status, which is non-zero whenever something is not
// RUNNING (supervisor 4.2.5, supervisor/supervisorctl.py, do_status()).
func supervisorctl(args ...string) (string, error) {
	return h.execIn("selfpost", append([]string{"supervisorctl", "-c", supervisorConfPath}, args...)...)
}

// programState is the state supervisorctl status gives one program.
func programState(name string) (string, string) {
	out, _ := supervisorctl("status", name)
	f := strings.Fields(out)
	if len(f) < 2 || f[0] != name {
		return "", out
	}
	return f[1], out
}

func waitProgramState(t *testing.T, name, want string, within time.Duration) {
	t.Helper()
	var last string
	if err := waitFor(fmt.Sprintf("program %s to be %s", name, want), within, 500*time.Millisecond, func() (bool, error) {
		state, out := programState(name)
		last = out
		return state == want, nil
	}); err != nil {
		t.Fatalf("%v; last status: %s\n%s", err, last, h.logs("selfpost"))
	}
}

// postfixStops counts the lines "postfix stop" writes to mail.log before it
// sends master TERM (Postfix 3.10.13, conf/postfix-script, "stop": $INFO
// stopping the Postfix mail system; "... with force" when master had not
// gone after five seconds, which this does not count).
func postfixStops(t *testing.T) int {
	return countMailLog(t, `postfix-script\[[0-9]*\]: stopping the Postfix mail system$`)
}

// mastersTerminated counts master's own line for SIGTERM (src/master/
// master_sig.c, master_sigdeath(): "terminating on signal %d"). master sends
// TERM to its process group, postlogd included, before it writes the line,
// so whether the line reaches mail.log is measured, not asserted.
func mastersTerminated(t *testing.T) int {
	return countMailLog(t, `postfix/master\[[0-9]*\]: terminating on signal 15`)
}

func countMailLog(t *testing.T, re string) int {
	t.Helper()
	out, _ := h.execIn("selfpost", "sh", "-c", "grep -c '"+re+"' /data/log/mail.log || true")
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("count %q in mail.log: %q", re, out)
	}
	return n
}

// masters counts the processes named master in the container.
func masters(t *testing.T) int {
	t.Helper()
	out, err := h.execIn("selfpost", "sh", "-c",
		`n=0; for c in /proc/[0-9]*/comm; do [ "$(cat "$c" 2>/dev/null)" = master ] && n=$((n+1)); done; echo $n`)
	n, cerr := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || cerr != nil {
		t.Fatalf("count master processes: %q, %v", out, err)
	}
	return n
}

func testSupervision(t *testing.T, sc *scenario) {
	// A restart of the program stops Postfix and starts one master. Before,
	// the stop reached the wrapper's shell alone; master lived on, and the
	// new start ended "the Postfix mail system is already running" until
	// the program was FATAL and the container went down.
	stops, terminated := postfixStops(t), mastersTerminated(t)
	if out, err := supervisorctl("restart", "postfix"); err != nil {
		t.Fatalf("supervisorctl restart postfix: %v\n%s", err, out)
	}
	waitProgramState(t, "postfix", "RUNNING", 60*time.Second)
	if got := postfixStops(t); got != stops+1 {
		t.Fatalf("mail.log has %d lines of postfix stop after the restart, want %d\n%s", got, stops+1, mailLogTail(h, "postfix"))
	}
	t.Logf("measured: master's \"terminating on signal 15\" in mail.log after the stop: %d new", mastersTerminated(t)-terminated)
	if n := masters(t); n != 1 {
		t.Fatalf("%d master processes after restarting the program, want 1", n)
	}
	if err := waitForSMTPSReady(30 * time.Second); err != nil {
		t.Fatal(err)
	}

	// The wrapper's give-up on the milter sockets is an exit supervisord
	// counts as too quick: BACKOFF, on its way to FATAL, not a restart for
	// ever. The panel's sockets are taken away for one attempt, then
	// given back, and Postfix comes up on the next one. Postfix is stopped
	// before the panel, so the panel's start counts no downtime of the check.
	if out, err := supervisorctl("stop", "postfix"); err != nil {
		t.Fatalf("stop postfix: %v\n%s", err, out)
	}
	if out, err := supervisorctl("stop", "panel"); err != nil {
		t.Fatalf("stop panel: %v\n%s", err, out)
	}
	panelUp := false
	t.Cleanup(func() {
		if !panelUp {
			_, _ = supervisorctl("start", "panel")
		}
	})
	// As build/entrypoint.sh does for a killed container's leftovers.
	if _, err := h.execIn("selfpost", "sh", "-c", "rm -f /run/selfpost/*.sock"); err != nil {
		t.Fatal(err)
	}
	// start waits for RUNNING. An exit before startsecs leaves spawnerr
	// "Exited too quickly" (process.py, Subprocess.finish()), which onwait()
	// looks at before the state: "spawn error" (supervisor 4.2.5,
	// rpcinterface.py, startProcess(); supervisorctl.py, _startresult()).
	out, err := supervisorctl("start", "postfix")
	if err == nil || !strings.Contains(out, "postfix: ERROR (spawn error)") {
		t.Fatalf("start postfix without the panel's sockets: %v\n%s; want a spawn error", err, out)
	}
	if state, status := programState("postfix"); state != "BACKOFF" && state != "STARTING" {
		t.Fatalf("postfix after the wrapper gave up: %s", status)
	}
	if !strings.Contains(h.logs("selfpost"), "postfix-wrapper: timed out after 10s waiting for milter socket") {
		t.Fatalf("the wrapper did not say it gave up:\n%s", h.logs("selfpost"))
	}
	if out, err := supervisorctl("start", "panel"); err != nil {
		t.Fatalf("start panel: %v\n%s", err, out)
	}
	panelUp = true
	waitProgramState(t, "postfix", "RUNNING", 90*time.Second)
	if err := waitForPanelReady(); err != nil {
		t.Fatal(err)
	}
	if err := waitForSMTPSReady(30 * time.Second); err != nil {
		t.Fatal(err)
	}

	// logrotate refusing a config it would ignore ends FATAL, and the
	// container stays up: it is not on the mail path. With startsecs=0 it
	// was restarted every second for ever.
	if _, err := h.execIn("selfpost", "chmod", "664", "/etc/logrotate.d/mail"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = h.execIn("selfpost", "chmod", "644", "/etc/logrotate.d/mail")
		_, _ = supervisorctl("start", "logrotate")
	})
	_, _ = supervisorctl("restart", "logrotate")
	waitProgramState(t, "logrotate", "FATAL", 30*time.Second)
	if !strings.Contains(h.logs("selfpost"), "crashexit: logrotate entered FATAL; it is not on the mail path, the container stays up") {
		t.Fatalf("crashexit did not spare logrotate:\n%s", h.logs("selfpost"))
	}
	for _, p := range []string{"panel", "postfix"} {
		if state, status := programState(p); state != "RUNNING" {
			t.Fatalf("program %s with logrotate FATAL: %s", p, status)
		}
	}
	if _, err := h.execIn("selfpost", "chmod", "644", "/etc/logrotate.d/mail"); err != nil {
		t.Fatal(err)
	}
	if out, err := supervisorctl("start", "logrotate"); err != nil {
		t.Fatalf("start logrotate with its config mended: %v\n%s", err, out)
	}
	waitProgramState(t, "logrotate", "RUNNING", 15*time.Second)

	// The panel's Reload configuration runs postfix-reload to its end and
	// reads its exit status (internal/supervisor, RunOnce): Postfix
	// refreshed in mail.log by the time the page answers, and the helper
	// resting EXITED after it, which the check of the processes takes as
	// healthy.
	refreshes := countMailLog(t, `postfix-script\[[0-9]*\]: refreshing the Postfix mail system$`)
	resp, body, err := sc.panel.postForm("/server/health/reload", url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || resp.Request.URL.Path != "/server/health" {
		t.Fatalf("Reload configuration: status %d, landed on %s:\n%.800s", resp.StatusCode, resp.Request.URL.Path, body)
	}
	if got := countMailLog(t, `postfix-script\[[0-9]*\]: refreshing the Postfix mail system$`); got <= refreshes {
		t.Fatalf("Reload configuration answered before Postfix was refreshed (%d refresh lines, %d before)", got, refreshes)
	}
	if state, status := programState("postfix-reload"); state != "EXITED" {
		t.Fatalf("postfix-reload after the panel's reload: %s, want EXITED", status)
	}
	if err := checkSupervisorProcesses(h); err != nil {
		t.Fatal(err)
	}
}
