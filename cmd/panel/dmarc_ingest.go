package main

import (
	"errors"
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
		if !errors.Is(err, dmarc.ErrRejected) {
			return err
		}
		// Not a report this relay keeps. It is dropped: deferring would have
		// Postfix try it again for days, and a bounce would go to an envelope
		// sender anyone can write - the report addresses are public. Postfix
		// 3.7.11: exit 0 is PIPE_STAT_OK, logged "status=sent (delivered via
		// ... service ...)" (src/global/pipe_command.c, pipe_command();
		// src/pipe/pipe.c, eval_command_status()); a 4.x.x status defers until
		// maximal_queue_lifetime and a 5.x.x one bounces, either way a notice
		// to the sender (src/qmgr/qmgr_active.c, src/global/bounce.c).
		if incrErr := st.IncrDMARCParseFailures(); incrErr != nil {
			log.Printf("dmarc ingest: record failure: %v", incrErr)
		}
		logRejected(err)
	}
	return nil
}

// logRejected leaves the reason in the mail log. Postfix puts the start of
// what a delivery command wrote - about a hundred bytes - into its own line
// for the message ("status=sent (delivered via dmarc-ingest service (...))"),
// so the reason goes out first and bare: no prefix, no date, no recipient,
// all of which that line already has. Postfix 3.7.11, src/pipe/pipe.c,
// eval_command_status(): " (%.100s)" of the output, which
// src/global/pipe_command.c takes from stdout and stderr together. postlog is
// no use here: pipe(8) starts the command without the group that may write to
// Postfix's log socket (src/util/set_ugid.c, set_ugid(): setgroups() to the
// user's one group). Nor does postlog bring the group with it: upstream
// installs it set-gid, Debian's package does not - measured in the image,
// 2026-10-15: /usr/sbin/postlog is -rwxr-xr-x root:root (postfix
// 3.7.11-0+deb12u1), and dpkg-statoverride lists only postdrop and
// postqueue (assumptions A-43).
func logRejected(reason error) {
	fmt.Fprintln(os.Stderr, "dropped: "+logSafe(strings.TrimPrefix(reason.Error(), dmarc.ErrRejected.Error()+": ")))
}

// logSafe makes a reason fit for a log line. Parts of it are the sender's own
// bytes - a header line, a file name inside an archive, a piece of XML - so it
// is cut short and anything that is not printable ASCII becomes "?", whatever
// Postfix would have done with it.
func logSafe(s string) string {
	const max = 200
	if len(s) > max {
		s = s[:max]
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e {
			return '?'
		}
		return r
	}, s)
}

// ingestRecipient is the report address the message was delivered to. Postfix's
// pipe(8) hands it over as an argument (${recipient} in the service's argv,
// build/postfix-config.sh) - it sets no RECIPIENT variable, that is local(8).
// Postfix 3.7.11, src/pipe/pipe.c, deliver_message(): pipe_command() gets only
// CA_PIPE_CMD_EXPORT(export_environment), no extra environment;
// src/local/command.c sets "RECIPIENT".
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
