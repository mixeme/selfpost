package postfix

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const postqueueTimeout = 5 * time.Second

// maxQueueOutput bounds how much of `postqueue -p` the panel keeps. A deferred
// queue with a very large backlog prints one paragraph per message, and the
// whole listing is held in memory and then rendered into a page — so the read
// is capped and the remainder reported as truncated rather than being allowed
// to size the panel's memory by the size of the queue.
const maxQueueOutput = 4 << 20

// limitWriter keeps the first remaining bytes written to it and drops the rest,
// recording that it did. Writes always report full consumption so postqueue is
// never killed by a short write; the extra output is simply discarded.
type limitWriter struct {
	buf       bytes.Buffer
	remaining int
	truncated bool
}

func (l *limitWriter) Write(p []byte) (int, error) {
	keep := len(p)
	if keep > l.remaining {
		keep = l.remaining
		l.truncated = true
	}
	if keep > 0 {
		l.buf.Write(p[:keep])
		l.remaining -= keep
	}
	return len(p), nil
}

// Queue returns Postfix's own human-readable mail-queue listing
// (architecture.md § Panel HTTP surface): active, deferred and held messages,
// exactly as an administrator would see via the CLI. The command takes a
// single fixed flag and no user input, so it never goes through a shell
// (security.md). The panel is responsible for escaping the output before
// display (security.md); this function returns it as-is.
func Queue() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), postqueueTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "postqueue", "-p")
	out := &limitWriter{remaining: maxQueueOutput}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("postqueue -p timed out after %s", postqueueTimeout)
	}
	if err != nil {
		return "", fmt.Errorf("postqueue -p: %w: %s", err, strings.TrimSpace(out.buf.String()))
	}
	text := out.buf.String()
	if out.truncated {
		text += fmt.Sprintf("\n[listing truncated at %d bytes]\n", maxQueueOutput)
	}
	return text, nil
}

// QueueIDs returns the set of queue ids Postfix is still holding — everything
// in the maildrop, incoming, active, deferred and hold queues. It answers the
// one question the log-tailer's reconcile sweep asks about a send-log row stuck
// at "queued": is Postfix still working on this message, or has it left the
// queue without the panel ever seeing a delivery line for it (architecture.md §
// Log tailer)?
//
// An error means the queue could not be listed and therefore says nothing about
// any message; the caller must treat it as "no information", never as an empty
// queue.
func QueueIDs() (map[string]struct{}, error) {
	out, err := Queue()
	if err != nil {
		return nil, err
	}
	return parseQueueIDs(out), nil
}

// queueEntryRe matches the first line of a `postqueue -p` entry, e.g.
//
//	3C5B04E6C1*     446 Thu Aug  7 10:12:31  app@example.com
//
// The id is at the start of the line, optionally flagged '*' (in the active
// queue) or '!' (on hold), and is followed by the message size. Requiring the
// size is what separates an entry from the listing's other left-margin lines:
// the '-Queue ID-' header, the '-- 5 Kbytes in 2 Requests.' trailer, a deferred
// entry's '(connect timed out)' reason, and 'Mail queue is empty'. Recipient
// lines are indented and never match.
var queueEntryRe = regexp.MustCompile(`^([0-9A-Za-z]+)[*!]?\s+\d+\s`)

func parseQueueIDs(listing string) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, line := range strings.Split(listing, "\n") {
		if m := queueEntryRe.FindStringSubmatch(line); m != nil {
			ids[m[1]] = struct{}{}
		}
	}
	return ids
}
