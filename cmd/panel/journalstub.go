package main

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"
)

// serveJournalStub opens the journal-milter Unix socket so the Postfix start
// wrapper's readiness probe (test -S) succeeds and the cold-start ordering
// (spec 4) can be exercised end to end. The real milter protocol handler is
// implemented in Phase 6; here connections are simply accepted and closed.
func serveJournalStub(ctx context.Context, socketPath string) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return err
	}
	// Clear a stale socket left behind by an unclean shutdown, otherwise the
	// listen below fails with "address already in use".
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	// Postfix (user `postfix`) connects to this socket as a milter and needs
	// group access. The socket inherits group `selfpost` from the setgid parent
	// dir entrypoint.sh prepares; make it group read/write so postfix can reach
	// it (connecting to a Unix socket needs write permission on the node).
	if err := os.Chmod(socketPath, 0o660); err != nil {
		return err
	}

	// Closing the listener unblocks Accept and unlinks the socket file.
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	log.Printf("journal-milter stub listening on %s", socketPath)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil // expected during shutdown
			}
			return err
		}
		_ = conn.Close()
	}
}
