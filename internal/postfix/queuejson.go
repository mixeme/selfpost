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
// it: where it is, and since when.
type QueuedMessage struct {
	ID      string
	Queue   string // "deferred", "active", "hold", "incoming", "maildrop"
	Sender  string
	Arrival time.Time
}

// readQueueJSON runs `postqueue -j`. It is a var so tests can answer without a
// running Postfix, the way readRetryConf is.
var readQueueJSON = postqueueJSON

func postqueueJSON() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), postqueueTimeout)
	defer cancel()
	// A fixed flag and no user input, never through a shell (security.md).
	cmd := exec.CommandContext(ctx, "postqueue", "-j")
	out := &limitWriter{remaining: maxQueueOutput}
	errOut := &limitWriter{remaining: 4096}
	cmd.Stdout = out
	cmd.Stderr = errOut
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("postqueue -j timed out after %s", postqueueTimeout)
	}
	if err != nil {
		return nil, fmt.Errorf("postqueue -j: %w: %s", err, strings.TrimSpace(errOut.buf.String()))
	}
	// A listing cut at the size limit is still a list of whole lines up to the
	// cut; the last, broken one is dropped by the parser.
	return out.buf.Bytes(), nil
}

// QueueMessages returns the messages Postfix holds with the time each arrived,
// for the notification about mail that has waited too long. An error means the
// queue could not be listed and says nothing about any message.
func QueueMessages() ([]QueuedMessage, error) {
	out, err := readQueueJSON()
	if err != nil {
		return nil, err
	}
	return parseQueueJSON(out), nil
}

// parseQueueJSON reads `postqueue -j` output: one JSON object per line. A line
// that is not one - the tail of a listing cut at its size limit - is skipped.
func parseQueueJSON(out []byte) []QueuedMessage {
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
			Sender      string `json:"sender"`
		}
		if err := json.Unmarshal([]byte(line), &e); err != nil || e.QueueID == "" || e.ArrivalTime <= 0 {
			continue
		}
		msgs = append(msgs, QueuedMessage{
			ID:      e.QueueID,
			Queue:   e.QueueName,
			Sender:  e.Sender,
			Arrival: time.Unix(e.ArrivalTime, 0).UTC(),
		})
	}
	return msgs
}
