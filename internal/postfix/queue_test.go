package postfix

import "testing"

// The reconcile sweep decides whether a message is still Postfix's problem, so
// the parser must pick queue ids out of a real listing and nothing else out of
// it: not the header, not the byte-count trailer, and above all not a deferred
// entry's reason line, which — unlike the recipient lines — starts at the left
// margin just as an entry does.
func TestParseQueueIDs(t *testing.T) {
	listing := `-Queue ID-  --Size-- ----Arrival Time---- -Sender/Recipient-------
3C5B04E6C1*     446 Fri Aug  8 10:12:31  app@example.com
                                         rcpt@example.net

5B4A2C1D3E      446 Fri Aug  8 10:13:31  app@example.com
(connect to mx.example.net[203.0.113.9]:25: Connection timed out)
                                         deferred@example.net

A1B2C3D4E5F!    891 Fri Aug  8 10:14:31  app@example.com
                                         held@example.net

-- 1 Kbytes in 3 Requests.
`
	ids := parseQueueIDs(listing)
	want := []string{"3C5B04E6C1", "5B4A2C1D3E", "A1B2C3D4E5F"}
	for _, id := range want {
		if _, ok := ids[id]; !ok {
			t.Errorf("queue id %s not found in %v", id, ids)
		}
	}
	if len(ids) != len(want) {
		t.Errorf("got %d ids %v, want exactly %v", len(ids), ids, want)
	}
}

// An empty queue must come back as an empty set, not as a phantom id parsed out
// of Postfix's prose — every stale row would otherwise be compared against a
// listing that claims to hold a message called "Mail".
func TestParseQueueIDsOnAnEmptyQueue(t *testing.T) {
	if ids := parseQueueIDs("Mail queue is empty\n"); len(ids) != 0 {
		t.Errorf("got %v, want no ids", ids)
	}
}
