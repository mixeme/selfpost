package main

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"

	"codeberg.org/mix/selfpost/internal/milter"
	"codeberg.org/mix/selfpost/internal/store"
)

// serveJournal opens the journal-milter Unix socket and runs the real milter
// (spec 7.3), recording accepted messages into the send log. Socket lifecycle
// (creation, stale cleanup, group permissions) lives here; the protocol handler
// lives in internal/milter.
func serveJournal(ctx context.Context, cfg config, st *store.Store) error {
	socketPath := cfg.journalSocket
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
		ln.Close()
		return err
	}

	log.Printf("journal-milter listening on %s", socketPath)
	return milter.Serve(ctx, ln, st)
}
