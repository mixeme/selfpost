package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// wantRunning are the supervisord programs that must be RUNNING once the
// container is up (build/supervisord.conf). postfix-reload is deliberately
// excluded: autostart=false, so it rests STOPPED until its first run and
// EXITED after it (supervisor 4.2.5, supervisor/process.py,
// Subprocess.finish(); assumptions C-V10).
var wantRunning = []string{"panel", "postfix", "cert-reload", "logrotate"}

// checkSupervisorProcesses shells into the container directly (not through the
// panel's /overview page) so it works before an administrator account even
// exists — this is the first thing the harness checks after `docker compose
// up` (plan C.4). Programs take a moment to leave STARTING right after the
// container starts - postfix its startsecs, MILTER_WAIT_TIMEOUT plus ten
// seconds, 20 on this stand - so this polls rather than checking once.
func checkSupervisorProcesses(s *stack) error {
	var lastErr error
	err := waitFor("all supervised programs to reach their steady state", 60*time.Second, 500*time.Millisecond, func() (bool, error) {
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
		if state, ok := states["postfix-reload"]; ok && state != "STOPPED" && state != "EXITED" {
			lastErr = fmt.Errorf("program postfix-reload is %s, want STOPPED or EXITED (autostart=false):\n%s", state, out)
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
// removed when INBOUND_RELAY_ENABLE is not true). For an absent service
// postconf -M prints nothing on stdout and "warning: unmatched request" on
// stderr (Postfix 3.10.13, src/postconf/postconf_master.c,
// pcf_show_master_entries()).
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

// checkCitedPackageVersions compares the Debian packages the image runs with
// the versions their sources were read at (docs/development.md, the table
// between the cited-package-versions markers). The image installs unpinned
// from a moving trixie, so a point release changes them on a rebuild
// without anyone reading what changed (assumptions C-D1). The citations are
// of the source, so the source version is compared: a binary-only rebuild
// changes the binary version on one architecture and nothing that was read
// (logrotate is 3.22.0-1+b2 on arm64 only - "Binary-only non-maintainer
// upload for arm64; no source changes", 2025-04-07).
func checkCitedPackageVersions(s *stack) error {
	doc, err := os.ReadFile(filepath.Join(s.repoRoot, "docs", "development.md"))
	if err != nil {
		return err
	}
	text := string(doc)
	start := strings.Index(text, "<!-- cited-package-versions -->")
	end := strings.Index(text, "<!-- /cited-package-versions -->")
	if start < 0 || end < start {
		return fmt.Errorf("docs/development.md has no cited-package-versions table")
	}
	row := regexp.MustCompile("(?m)^[|] `([^`]+)` [|] `([^`]+)` [|]$")
	want := map[string]string{}
	var names []string
	for _, m := range row.FindAllStringSubmatch(text[start:end], -1) {
		want[m[1]] = m[2]
		names = append(names, m[1])
	}
	if len(names) == 0 {
		return fmt.Errorf("the cited-package-versions table lists no package")
	}
	out, err := s.execIn("selfpost", append([]string{"dpkg-query", "-W", "-f", "${Package} ${source:Version}\n"}, names...)...)
	if err != nil {
		return fmt.Errorf("dpkg-query: %v\n%s", err, out)
	}
	var diffs []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && want[f[0]] != f[1] {
			diffs = append(diffs, fmt.Sprintf("%s: the image has %s, the citations were read at %s", f[0], f[1], want[f[0]]))
		}
	}
	if len(diffs) > 0 {
		return fmt.Errorf("packages changed under the citations - re-read them (docs/development.md):\n%s", strings.Join(diffs, "\n"))
	}
	return nil
}

// checkDKIMTree: the keys and the selector table are the panel's alone,
// directories 0700 and files 0600 (build/entrypoint.sh; internal/domain,
// Keys) - no other program reads them since OpenDKIM left - and the table
// is there, written before the signer serves (cmd/panel/signer.go).
func checkDKIMTree(s *stack) error {
	out, err := s.execIn("selfpost", "sh", "-c",
		`test -f /data/dkim/selectors && find /data/dkim ! \( -user panel -group panel \( -type d -perm 0700 -o -type f -perm 0600 \) \)`)
	if err != nil {
		return fmt.Errorf("the DKIM tree: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("not the panel's alone under /data/dkim:\n%s", out)
	}
	return nil
}

// checkPidsLimit reads the container's process ceiling back from the kernel:
// the shipped compose sets pids_limit (security audit 2026-10-17, I3).
func checkPidsLimit(s *stack) error {
	out, err := s.execIn("selfpost", "cat", "/sys/fs/cgroup/pids.max")
	if err != nil {
		return fmt.Errorf("read pids.max: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(out); got != "1024" {
		return fmt.Errorf("the container's pids.max is %q, want the compose file's 1024", got)
	}
	return nil
}

// checkPostfixHardening reads back what postfix-config.sh sets against
// downgrade and enumeration (audit 2026-10-12, L31).
func checkPostfixHardening(s *stack) error {
	out, err := s.execIn("selfpost", "postconf", "-h", "smtpd_tls_protocols", "smtpd_tls_mandatory_protocols", "disable_vrfy_command", "smtpd_banner",
		"smtpd_etrn_restrictions", "smtpd_discard_ehlo_keywords")
	if err != nil {
		return fmt.Errorf("postconf: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSpace(out), "\n")
	want := []string{">=TLSv1.2", ">=TLSv1.2", "yes", "$myhostname ESMTP", "reject", "silent-discard,etrn"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		return fmt.Errorf("postconf -h of the hardened settings = %q, want %q", got, want)
	}
	return nil
}
