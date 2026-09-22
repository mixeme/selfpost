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

// The queue listing is held in memory and rendered into a page, so a very large
// queue must not size the panel's allocation: the writer keeps the cap and
// reports the rest as dropped, while still accepting every write so postqueue
// itself is never cut short.
func TestLimitWriterCapsOutput(t *testing.T) {
	w := &limitWriter{remaining: 8}
	n, err := w.Write([]byte("12345"))
	if n != 5 || err != nil {
		t.Fatalf("first write = %d, %v", n, err)
	}
	if w.truncated {
		t.Fatal("truncated before the cap was reached")
	}
	n, err = w.Write([]byte("67890"))
	if n != 5 || err != nil {
		t.Fatalf("second write = %d, %v", n, err)
	}
	if !w.truncated {
		t.Fatal("write past the cap was not recorded as truncated")
	}
	if got := w.buf.String(); got != "12345678" {
		t.Fatalf("kept %q, want the first 8 bytes", got)
	}
	if n, err := w.Write([]byte("more")); n != 4 || err != nil {
		t.Fatalf("write after the cap = %d, %v (must still accept)", n, err)
	}
	if got := w.buf.String(); got != "12345678" {
		t.Fatalf("buffer grew past the cap: %q", got)
	}
}
