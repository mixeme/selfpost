package postfix

import (
	"errors"
	"testing"
	"time"
)

func TestParseQueueJSON(t *testing.T) {
	out := []byte(`{"queue_name": "deferred", "queue_id": "41E862C00D9E", "arrival_time": 1791000000, "message_size": 446, "forced_expire": false, "sender": "app@example.com", "recipients": [{"address": "a@example.net", "delay_reason": "connect to mx.example.net[203.0.113.9]:25: Connection timed out"}]}
{"queue_name": "active", "queue_id": "5F00AA12", "arrival_time": 1791250000, "message_size": 900, "sender": "", "recipients": [{"address": "b@example.net"}]}

not json at all
{"queue_name": "deferred", "queue_id": "", "arrival_time": 1791250000}
{"queue_name": "deferred", "queue_id": "6C00BB34", "arrival_time": 17912`)

	got := parseQueueJSON(out)
	if len(got) != 2 {
		t.Fatalf("parsed %d messages, want 2: %+v", len(got), got)
	}
	if got[0].ID != "41E862C00D9E" || got[0].Queue != "deferred" || got[0].Sender != "app@example.com" ||
		!got[0].Arrival.Equal(time.Unix(1791000000, 0)) {
		t.Errorf("first message = %+v", got[0])
	}
	if got[1].ID != "5F00AA12" || got[1].Queue != "active" {
		t.Errorf("second message = %+v", got[1])
	}
	if len(parseQueueJSON(nil)) != 0 {
		t.Error("an empty queue is not empty")
	}
}

// A queue that cannot be listed is an error, not an empty queue: the caller
// must not conclude that nothing is waiting.
func TestQueueMessagesReportsAFailedListing(t *testing.T) {
	old := readQueueJSON
	t.Cleanup(func() { readQueueJSON = old })
	readQueueJSON = func() ([]byte, error) { return nil, errors.New("postqueue: Permission denied") }

	if msgs, err := QueueMessages(); err == nil {
		t.Fatalf("no error for a failed listing (got %v)", msgs)
	}
}
