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

// postqueueP builds the `postqueue -p` command. It is a var so tests can put
// a fake postqueue in its place, the way readQueueJSON is.
var postqueueP = func(ctx context.Context) *exec.Cmd {
	// A single fixed flag and no user input, never through a shell (security.md).
	return exec.CommandContext(ctx, "postqueue", "-p")
}

// listQueue runs `postqueue -p` and keeps at most maxQueueOutput bytes of it;
// truncated says whether there was more.
//
// Postfix 3.7.11, src/postqueue/postqueue.c, show_queue(): any user passes
// authorized_mailq_users (DEF_SHOWQ_ACL static:anyone), and a failure is
// msg_fatal_status() - a non-zero exit with the reason on stderr;
// src/postqueue/showq_compat.c, showq_compat(): "Mail queue is empty".
func listQueue() (text string, truncated bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), postqueueTimeout)
	defer cancel()
	cmd := postqueueP(ctx)
	out := &limitWriter{remaining: maxQueueOutput}
	cmd.Stdout = out
	cmd.Stderr = out
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", false, fmt.Errorf("postqueue -p timed out after %s", postqueueTimeout)
	}
	if err != nil {
		// stdout and stderr share the buffer: the end of it says why, and up
		// to 4 MiB of listing does not belong in an error.
		why := strings.TrimSpace(out.buf.String())
		if len(why) > 512 {
			why = "..." + why[len(why)-512:]
		}
		return "", false, fmt.Errorf("postqueue -p: %w: %s", err, why)
	}
	return out.buf.String(), out.truncated, nil
}

// Queue returns Postfix's own human-readable mail-queue listing
// (architecture.md § Panel HTTP surface): active, deferred and held messages,
// exactly as an administrator would see via the CLI. The panel is responsible
// for escaping the output before display (security.md); this function returns
// it as-is. A listing over the size cap is shown cut, with a note saying so.
func Queue() (string, error) {
	text, truncated, err := listQueue()
	if err != nil {
		return "", err
	}
	if truncated {
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
// queue. A listing cut at the size cap is such an error: the messages past the
// cut are still in the queue, and a backlog that large is the outage the sweep
// exists for (audit 2026-10-12, H3). QueueMessages treats its cap the same way.
//
// Postfix 3.7.11, src/showq/showq.c, showq_service(): maildrop, active,
// incoming, deferred and hold are the queues it lists.
func QueueIDs() (map[string]struct{}, error) {
	out, truncated, err := listQueue()
	if err != nil {
		return nil, err
	}
	if truncated {
		return nil, fmt.Errorf("postqueue -p: listing is larger than %d bytes", maxQueueOutput)
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
