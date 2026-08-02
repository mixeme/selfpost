package e2e

import (
	"fmt"
	"time"
)

// waitFor polls check every interval until it returns true or timeout elapses.
// Every wait in this suite goes through here — no fixed sleeps standing in for
// a readiness check (plan C.4): a passing check ends the wait immediately, and
// a timeout fails with what was being waited for, not a bare "timed out".
func waitFor(what string, timeout, interval time.Duration, check func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		ok, err := check()
		if ok {
			return nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("timed out after %s waiting for %s: %w", timeout, what, lastErr)
			}
			return fmt.Errorf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(interval)
	}
}
