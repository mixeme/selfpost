package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/dmarc"
	"github.com/mixeme/selfpost/internal/store"
)

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

	recipient := strings.ToLower(strings.TrimSpace(os.Getenv("RECIPIENT")))
	if recipient == "" {
		recipient = strings.ToLower(strings.TrimSpace(os.Getenv("ORIGINAL_RECIPIENT")))
	}
	if err := dmarc.IngestMessage(st, os.Stdin, recipient, time.Now().UTC()); err != nil {
		if incrErr := st.IncrDMARCParseFailures(); incrErr != nil {
			log.Printf("dmarc ingest: record failure: %v", incrErr)
		}
		return err
	}
	return nil
}

func isDMARCIngestInvocation() bool {
	if len(os.Args) > 1 && os.Args[1] == "-dmarc-ingest" {
		return true
	}
	return filepath.Base(os.Args[0]) == "dmarc-ingest"
}
