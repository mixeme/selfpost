package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// project isolates this stand's containers/network from anything else on the
// host (in particular a prod SelfPost stack in an unrelated compose project —
// plan C.4). stageDir holds everything a single e2e run writes: the generated
// TLS cert, the /data bind mount, the DNS zone CoreDNS serves, and the mail
// sink's dump directory. It is never reused across runs.
const project = "selfpost-e2e"

// stack drives the compose lifecycle. baseCompose is the shipped
// deploy/docker-compose.yml — deliberately the real one, not a parallel test
// compose, so cap_drop/cap_add/no-new-privileges are exercised as documented
// rather than a laxer stand-in (plan C.4).
type stack struct {
	repoRoot        string
	baseComposeFile string
	overrideFile    string
	stageDir        string
}

func newStack() (*stack, error) {
	wd, err := os.Getwd() // test/e2e, where go.mod lives
	if err != nil {
		return nil, err
	}
	repoRoot, err := filepath.Abs(filepath.Join(wd, "..", ".."))
	if err != nil {
		return nil, err
	}
	stageDir, err := filepath.Abs(filepath.Join(wd, ".stage"))
	if err != nil {
		return nil, err
	}
	return &stack{
		repoRoot:        repoRoot,
		baseComposeFile: filepath.Join(repoRoot, "deploy", "docker-compose.yml"),
		overrideFile:    filepath.Join(wd, "compose.override.yml"),
		stageDir:        stageDir,
	}, nil
}

// compose runs `docker compose <args...>` against this stand's two files and
// project directory/name, streaming output to the test log on failure.
func (s *stack) compose(args ...string) ([]byte, error) {
	full := append([]string{
		"compose",
		"-p", project,
		"-f", s.baseComposeFile,
		"-f", s.overrideFile,
		"--project-directory", s.stageDir,
	}, args...)
	cmd := exec.Command("docker", full...)
	// deploy/docker-compose.yml interpolates ${SELFPOST_HOSTNAME:?...} at
	// compose-file parse time, straight from the process environment — that
	// happens before the override file's `environment:` mapping is even
	// looked at (that mapping only reaches the container, not compose's own
	// variable interpolation), so it has to be set here too.
	cmd.Env = append(os.Environ(), "SELFPOST_HOSTNAME="+selfpostHostname)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		return out.Bytes(), fmt.Errorf("docker %v: %w\n%s", full, err, out.String())
	}
	return out.Bytes(), nil
}

// execIn runs a command inside the running service container via `docker
// compose exec -T`, the same way the harness checks supervisord/mail.log
// state without going through the panel (so those checks work even before an
// administrator account exists).
func (s *stack) execIn(service string, args ...string) (string, error) {
	full := append([]string{"exec", "-T", service}, args...)
	out, err := s.compose(full...)
	return string(out), err
}

func (s *stack) build() error {
	_, err := s.compose("build")
	return err
}

func (s *stack) up() error {
	_, err := s.compose("up", "-d", "--force-recreate")
	return err
}

// restartSelfpost restarts just the selfpost container the way `docker
// restart` would (plan C.4 check 8: a session must survive that), without
// tearing down the DNS/sink sidecars or their state.
func (s *stack) restartSelfpost() error {
	_, err := s.compose("restart", "selfpost")
	return err
}

func (s *stack) down() {
	_, _ = s.compose("down", "-v", "--remove-orphans")
}

// reclaimData chowns/chmods the /data bind mount from inside the still-running
// selfpost container so the host test user can delete it afterwards. Files
// written as panel/postfix (setup-token 0600, opendkim tree, sqlite) otherwise
// leave EACCES on TempDir/stage cleanup (CI hostname-gate + prepareStage).
func (s *stack) reclaimData() {
	_, _ = s.execIn("selfpost", "sh", "-c", "chown -R root:root /data && chmod -R a+rwX /data")
}

// logs returns a service's combined stdout/stderr, for failure diagnostics.
func (s *stack) logs(service string) string {
	out, _ := s.compose("logs", "--no-color", service)
	return string(out)
}
