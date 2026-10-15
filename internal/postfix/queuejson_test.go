package postfix

import (
	"errors"
	"testing"
	"time"
)

// The listing is composed by hand: its first line in the field order of
// Postfix 3.10.13's src/postqueue/showq_json.c, format_json(); the others
// short of fields or broken on purpose.
func TestParseQueueJSON(t *testing.T) {
	out := []byte(`{"queue_name": "deferred", "queue_id": "41E862C00D9E", "arrival_time": 1791000000, "message_size": 446, "forced_expire": false, "sender": "app@example.com", "recipients": [{"address": "a@example.net", "delay_reason": "connect to mx.example.net[203.0.113.9]:25: Connection timed out"}]}
{"queue_name": "active", "queue_id": "5F00AA12", "arrival_time": 1791250000, "message_size": 900, "sender": "", "recipients": [{"address": "b@example.net"}]}

not json at all
{"queue_name": "deferred", "queue_id": "", "arrival_time": 1791250000}
{"queue_name": "deferred", "queue_id": "6C00BB34", "arrival_time": 17912`)

	got, err := parseQueueJSON(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("parsed %d messages, want 2: %+v", len(got), got)
	}
	if got[0].ID != "41E862C00D9E" || got[0].Queue != "deferred" || got[0].Sender != "app@example.com" ||
		!got[0].Arrival.Equal(time.Unix(1791000000, 0)) {
		t.Errorf("first message = %+v", got[0])
	}
	if len(got[0].Recipients) != 1 || got[0].Recipients[0] != "a@example.net" {
		t.Errorf("first message's recipients = %v", got[0].Recipients)
	}
	if got[1].ID != "5F00AA12" || got[1].Queue != "active" {
		t.Errorf("second message = %+v", got[1])
	}
	if empty, err := parseQueueJSON(nil); err != nil || len(empty) != 0 {
		t.Errorf("an empty queue: %v, %v", empty, err)
	}
	// A line the reader cannot hold ends the listing with an error: what
	// follows it is unknown, and "nothing old is left" must not be concluded.
	long := append([]byte(`{"queue_id": "A", "arrival_time": 1, "sender": "`), make([]byte, 2<<20)...)
	if _, err := parseQueueJSON(long); err == nil {
		t.Error("a line longer than the reader's limit was not an error")
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
