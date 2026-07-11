package main

import (
	"context"
	"log"
)

// tailMailLog is the Phase 1 placeholder for the log-tailer role. In Phase 6 it
// will follow mail.log and reconcile send-log delivery statuses by queue-id;
// for now it just idles until shutdown so the role is present in the process
// tree and its wiring is exercised.
func tailMailLog(ctx context.Context, path string) error {
	log.Printf("log-tailer stub active (will follow %s in Phase 6)", path)
	<-ctx.Done()
	return nil
}
