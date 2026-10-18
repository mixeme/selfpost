package postfix

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// QueuedMessage is one message Postfix is holding, as `postqueue -j` reports
// it: where it is, since when, and what became of each recipient's last try.
//
// Postfix 3.10.13, src/postqueue/showq_json.c, format_json(): one object and a
// newline per message - "queue_name", "queue_id", "arrival_time" (seconds,
// %ld), "message_size", "forced_expire", "sender", "recipients" [{"address",
// "delay_reason"}]; the queue names are those of src/global/mail_queue.h that
// src/showq/showq.c, showq_service(), scans.
type QueuedMessage struct {
	ID         string
	Queue      string // "deferred", "active", "hold", "incoming", "maildrop"
	Sender     string
	Recipients []QueuedRecipient
	Arrival    time.Time
	Size       int64 // bytes, "message_size"
}

// QueuedRecipient is one recipient of a queued message. DelayReason is why its
// last delivery attempt did not finish - the remote server's reply, or why no
// connection was made - and empty when there was none yet (showq_json.c writes
// "delay_reason" only for a recipient that has one).
type QueuedRecipient struct {
	Address     string
	DelayReason string
}

// queueJSONTimeout is longer than the listing for a page gets (postqueueTimeout):
// the notifier asks once an hour and can wait, and a queue large enough to be
// slow to list is the one that must not go unreported.
const queueJSONTimeout = 30 * time.Second

// readQueueJSON runs `postqueue -j` with a deadline. It is a var so tests can
// answer without a running Postfix, the way readRetryConf is.
var readQueueJSON = postqueueJSON

func postqueueJSON(timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// A fixed flag and no user input, never through a shell (security.md).
	cmd := exec.CommandContext(ctx, "postqueue", "-j")
	out := &limitWriter{remaining: maxQueueOutput}
	errOut := &limitWriter{remaining: 4096}
	cmd.Stdout = out
	cmd.Stderr = errOut
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("postqueue -j timed out after %s", timeout)
	}
	if err != nil {
		return nil, fmt.Errorf("postqueue -j: %w: %s", err, strings.TrimSpace(errOut.buf.String()))
	}
	if out.truncated {
		// A listing cut at the size limit says nothing about the messages past
		// the cut - the oldest may be among them. It is no listing.
		return nil, fmt.Errorf("postqueue -j: listing is larger than %d bytes", maxQueueOutput)
	}
	return out.buf.Bytes(), nil
}

// QueueMessages returns the messages Postfix holds with the time each arrived,
// for the notification about mail that has waited too long. An error means the
// queue could not be listed and says nothing about any message.
func QueueMessages() ([]QueuedMessage, error) {
	out, err := readQueueJSON(queueJSONTimeout)
	if err != nil {
		return nil, err
	}
	return parseQueueJSON(out)
}

// QueueListing is QueueMessages for a page: the Outbound and Inbound queues
// (architecture.md § Panel HTTP surface). A page waits as long as the
// `postqueue -p` listing did, not the notifier's half minute.
func QueueListing() ([]QueuedMessage, error) {
	out, err := readQueueJSON(postqueueTimeout)
	if err != nil {
		return nil, err
	}
	return parseQueueJSON(out)
}

// parseQueueJSON reads `postqueue -j` output: one JSON object per line. A line
// that is not one is skipped; a line too long to read ends the listing with an
// error, since what follows it is unknown.
func parseQueueJSON(out []byte) ([]QueuedMessage, error) {
	var msgs []QueuedMessage
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e struct {
			QueueName   string `json:"queue_name"`
			QueueID     string `json:"queue_id"`
			ArrivalTime int64  `json:"arrival_time"`
			MessageSize int64  `json:"message_size"`
			Sender      string `json:"sender"`
			Recipients  []struct {
				Address     string `json:"address"`
				DelayReason string `json:"delay_reason"`
			} `json:"recipients"`
		}
		if err := json.Unmarshal([]byte(line), &e); err != nil || e.QueueID == "" || e.ArrivalTime <= 0 {
			continue
		}
		m := QueuedMessage{
			ID:      e.QueueID,
			Queue:   e.QueueName,
			Sender:  e.Sender,
			Arrival: time.Unix(e.ArrivalTime, 0).UTC(),
			Size:    e.MessageSize,
		}
		for _, r := range e.Recipients {
			m.Recipients = append(m.Recipients, QueuedRecipient{Address: r.Address, DelayReason: r.DelayReason})
		}
		msgs = append(msgs, m)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read postqueue -j: %w", err)
	}
	return msgs, nil
}
