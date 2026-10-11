package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/dmarc"
	"github.com/mixeme/selfpost/internal/store"
)

const maxIngestStdin = 10 << 20 // 10 MiB

func runDMARCIngestMode() error {
	cfg := loadConfig()
	if !cfg.dmarcEnabled {
		return fmt.Errorf("dmarc ingest invoked but DMARC_REPORTS_ENABLE is not true")
	}
	st, err := store.Open(cfg.dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	recipient := ingestRecipient(os.Args[1:])
	if err := dmarc.IngestMessage(st, io.LimitReader(os.Stdin, maxIngestStdin), recipient, time.Now().UTC()); err != nil {
		if incrErr := st.IncrDMARCParseFailures(); incrErr != nil {
			log.Printf("dmarc ingest: record failure: %v", incrErr)
		}
		return err
	}
	return nil
}

// ingestRecipient is the report address the message was delivered to. Postfix's
// pipe(8) hands it over as an argument (${recipient} in the service's argv,
// build/postfix-config.sh) - it sets no RECIPIENT variable, that is local(8).
// Without it the check that a report came to its own domain's address is
// skipped.
func ingestRecipient(args []string) string {
	for _, a := range args {
		if a == "-dmarc-ingest" {
			continue
		}
		return strings.ToLower(strings.TrimSpace(a))
	}
	return ""
}

func isDMARCIngestInvocation() bool {
	if len(os.Args) > 1 && os.Args[1] == "-dmarc-ingest" {
		return true
	}
	return filepath.Base(os.Args[0]) == "dmarc-ingest"
}
