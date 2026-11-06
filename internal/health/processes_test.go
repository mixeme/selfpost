package health

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// A supervisorctl that never answers is not waited on for ever: the command
// has a deadline, the refusal reaches the caller within the bound, and the
// Health card that gets an error grades Unknown — which is what the /healthz
// probe and the polled fragment render (review 2026-11-05, FT1; the card's
// Unknown-on-error path is covered by the handlers' page tests).
func TestProcessesDoesNotWaitForAnUnansweredSupervisorctl(t *testing.T) {
	oldP, oldTimeout := supervisorctlP, supervisorctlTimeout
	supervisorctlTimeout = 250 * time.Millisecond
	supervisorctlP = func(ctx context.Context) *exec.Cmd {
		// Re-exec the test binary as a fake that never writes, the way
		// internal/postfix re-execs its fake postqueue: CommandContext
		// kills it when the deadline passes.
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSupervisorctlNeverWrites$")
		cmd.Env = append(os.Environ(), "SP_SUPERVISORCTL_NEVER=1")
		return cmd
	}
	t.Cleanup(func() { supervisorctlP, supervisorctlTimeout = oldP, oldTimeout })

	start := time.Now()
	_, err := Processes()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Processes answered for a supervisorctl that never wrote anything")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("the error does not say the bound was hit: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("Processes took %v, want it bounded near the timeout", elapsed)
	}
	// The error reaches liveness too: the probe reports unhealthy rather
	// than waiting.
	if lerr := Liveness(); lerr == nil {
		t.Error("Liveness reported healthy on an unanswered supervisorctl")
	}
}

// TestSupervisorctlNeverWrites is the fake command TestProcesses above runs:
// it blocks until the parent's context kills its process. The re-exec pattern
// is internal/postfix's (queue_test.go, fakePostqueue).
func TestSupervisorctlNeverWrites(t *testing.T) {
	if os.Getenv("SP_SUPERVISORCTL_NEVER") == "" {
		t.Skip("helper process, run by TestProcessesDoesNotWaitForAnUnansweredSupervisorctl")
	}
	// A pending timer keeps the standalone helper's runtime from reporting a
	// deadlock when this is the only test selected; CommandContext kills it.
	time.Sleep(time.Hour)
}
