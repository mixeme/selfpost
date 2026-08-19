package postfix

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const postqueueTimeout = 5 * time.Second

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
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("postqueue -p timed out after %s", postqueueTimeout)
	}
	if err != nil {
		return "", fmt.Errorf("postqueue -p: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
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
