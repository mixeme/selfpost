package main

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"

	"github.com/mixeme/selfpost/internal/inbound"
	"github.com/mixeme/selfpost/internal/milter"
	"github.com/mixeme/selfpost/internal/store"
)

// serveJournal opens the journal-milter Unix socket and runs the real milter
// (architecture.md § Mail path), recording accepted messages into the send
// log. Socket lifecycle (creation, stale cleanup, group permissions) lives
// in listenMilter; the protocol handler lives in internal/milter.
func serveJournal(ctx context.Context, cfg config, st *store.Store) error {
	ln, err := listenMilter(cfg.journalSocket)
	if err != nil {
		return err
	}
	log.Printf("journal-milter listening on %s", cfg.journalSocket)
	return milter.Serve(ctx, ln, st)
}

// serveInboundJournal runs the inbound journal milter, the last milter on
// port 25 (internal/milter/inbound.go). It exists only while the inbound relay
// is on: build/postfix-config.sh attaches the socket under the same condition.
func serveInboundJournal(ctx context.Context, cfg config, st *store.Store) error {
	ln, err := listenMilter(cfg.inboundJournalSocket)
	if err != nil {
		return err
	}
	// With a filter attached, the key it signs with. Without the key no
	// verdict can be told from a sender's own, so none is read: the journal
	// still records the mail, and the log says why it shows no verdicts.
	key := ""
	if cfg.inboundAntispamMilter != "" {
		if key, err = inbound.FilterKey(cfg.antispamDir); err != nil {
			log.Printf("inbound-journal-milter: no key for the filter's signature, verdicts will not be recorded: %v", err)
			key = ""
		}
	}
	log.Printf("inbound-journal-milter listening on %s", cfg.inboundJournalSocket)
	return milter.ServeInbound(ctx, ln, st, key)
}

// listenMilter opens a milter's Unix socket where Postfix can reach it.
func listenMilter(socketPath string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return nil, err
	}
	// Clear a stale socket left behind by an unclean shutdown, otherwise the
	// listen below fails with "address already in use".
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	// Postfix (user `postfix`) connects to this socket as a milter and needs
	// group access. The socket inherits group `selfpost` from the setgid parent
	// dir entrypoint.sh prepares; make it group read/write so postfix can reach
	// it (connecting to a Unix socket needs write permission on the node).
	if err := os.Chmod(socketPath, 0o660); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}
