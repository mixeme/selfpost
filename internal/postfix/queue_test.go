package postfix

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestFakePostqueue is not a test: it is the fake `postqueue -p` the tests
// below run in place of the real one, writing SP_FAKE_POSTQUEUE_BYTES bytes of
// queue entries composed by hand after the layout of Postfix 3.10.13's
// src/postqueue/showq_compat.c. The listing is composed once — one entry
// repeated — and written in one pass, so a loaded test machine cannot make
// composing it the slow part (review 2026-11-05, FT6). With
// SP_FAKE_POSTQUEUE_LINGER_MS set it then sleeps that long, so a test can let
// the deadline fire after the cap was reached, deterministically.
func TestFakePostqueue(t *testing.T) {
	if os.Getenv("SP_FAKE_POSTQUEUE") != "1" {
		t.Skip("helper process")
	}
	var size int
	fmt.Sscan(os.Getenv("SP_FAKE_POSTQUEUE_BYTES"), &size)
	out := bufio.NewWriter(os.Stdout)
	fmt.Fprintln(out, "-Queue ID-  --Size-- ----Arrival Time---- -Sender/Recipient-------")
	// A batch of distinct ids, composed once and then repeated to the size
	// asked: composing in the write loop was the slow part on a loaded
	// machine, and the id is what the under-cap tests read out (review
	// 2026-11-05, FT6).
	var batch strings.Builder
	for n, i := 0, 0; n < 4096; i++ {
		entry := fmt.Sprintf("%010X      446 Fri Aug  8 10:13:31  app@example.com\n%41s rcpt@example.net\n\n", i, "")
		batch.WriteString(entry)
		n += len(entry)
	}
	listing := strings.Repeat(batch.String(), size/4096+1)
	if size > 0 {
		if _, err := out.WriteString(listing[:size]); err != nil {
			os.Exit(1)
		}
	}
	out.Flush()
	if linger := os.Getenv("SP_FAKE_POSTQUEUE_LINGER_MS"); linger != "" {
		var ms int
		fmt.Sscan(linger, &ms)
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
	os.Exit(0)
}

// fakePostqueue puts a postqueue that lists about size bytes of entries in
// place of the real one for the rest of the test.
func fakePostqueue(t *testing.T, size int) {
	t.Helper()
	fakePostqueueLingering(t, size, 0)
}

// fakePostqueueLingering is fakePostqueue whose process then sleeps lingerMs,
// so a test can let the reader's deadline fire after the output was written.
func fakePostqueueLingering(t *testing.T, size, lingerMs int) {
	t.Helper()
	old := postqueueP
	postqueueP = func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFakePostqueue$")
		cmd.Env = append(os.Environ(), "SP_FAKE_POSTQUEUE=1",
			fmt.Sprintf("SP_FAKE_POSTQUEUE_BYTES=%d", size),
			fmt.Sprintf("SP_FAKE_POSTQUEUE_LINGER_MS=%d", lingerMs))
		return cmd
	}
	t.Cleanup(func() { postqueueP = old })
}

// A listing over the cap is no listing for the reconcile sweep: the messages
// past the cut are still queued, and taking the first 4 MiB of ids for the
// whole queue closed every row past it as bounced (audit 2026-10-12, H3).
// The queue page still shows the cut listing, with the note.
func TestQueueIDsRefusesATruncatedListing(t *testing.T) {
	fakePostqueue(t, 5<<20)
	if ids, err := QueueIDs(); err == nil || !strings.Contains(err.Error(), "listing is larger than") {
		t.Fatalf("QueueIDs on a 5 MiB listing = %d ids, %v; want the listing refused for its size", len(ids), err)
	}
	text, err := Queue()
	if err != nil {
		t.Fatalf("Queue on a 5 MiB listing: %v", err)
	}
	if !strings.HasSuffix(text, fmt.Sprintf("[listing truncated at %d bytes]\n", maxQueueOutput)) {
		t.Fatalf("Queue on a 5 MiB listing does not end with the truncation note: ...%q", text[len(text)-80:])
	}
}

// When postqueue passes the cap and then lingers past the deadline, the
// refusal names the size, not the clock: a merely big queue stopped being
// reported as a command that did not answer (review 2026-11-05, FT6). The
// cap is reached well inside the shortened deadline and the linger carries
// the process past it, so both facts are true at once, deterministically.
func TestListQueueNamesTheCapWhenTheDeadlineFiresToo(t *testing.T) {
	fakePostqueueLingering(t, 5<<20, 3000)
	old := postqueueTimeout
	postqueueTimeout = time.Second
	t.Cleanup(func() { postqueueTimeout = old })

	_, err := QueueIDs()
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("QueueIDs on a capped listing that lingered past its deadline = %v; want the size cap named", err)
	}
	if _, err := Queue(); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("Queue on a capped listing that lingered past its deadline = %v; want the size cap named", err)
	}
}

// A postqueue that answers nothing is bounded, and the refusal is the
// timeout's, not the cap's — the other half of the deadline's honesty
// (review 2026-11-05, FT6). The deadline is the test's, not the product's 5
// s: the var the fix made of it is what lets this run in milliseconds.
func TestListQueueTimesOutOnAnUnansweredPostqueue(t *testing.T) {
	fakePostqueueLingering(t, 0, 2000)
	old := postqueueTimeout
	postqueueTimeout = 250 * time.Millisecond
	t.Cleanup(func() { postqueueTimeout = old })

	start := time.Now()
	_, err := QueueIDs()
	if err == nil || !strings.Contains(err.Error(), "timed out after 250ms") {
		t.Fatalf("QueueIDs on a postqueue that never answered = %v; want the timeout named", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("the refusal took %v; want it bounded near the timeout", elapsed)
	}
}

func TestQueueIDsReadsAListingUnderTheCap(t *testing.T) {
	fakePostqueue(t, 1000)
	ids, err := QueueIDs()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ids["0000000000"]; !ok || len(ids) < 5 {
		t.Fatalf("QueueIDs on a small listing = %v", ids)
	}
}

// The reconcile sweep decides whether a message is still Postfix's problem, so
// the parser must pick queue ids out of a listing and nothing else out of it:
// not the header, not the byte-count trailer, and above all not a deferred
// entry's reason line. The listing is composed by hand. Postfix 3.10.13
// (src/postqueue/showq_compat.c, showq_message()) right-aligns a reason to
// column 76, so it reaches the left margin only when it is 76 characters or
// longer; the one here is put at the margin as the hard case.
func TestParseQueueIDs(t *testing.T) {
	listing := `-Queue ID-  --Size-- ----Arrival Time---- -Sender/Recipient-------
3C5B04E6C1*     446 Fri Aug  8 10:12:31  app@example.com
                                         rcpt@example.net

5B4A2C1D3E      446 Fri Aug  8 10:13:31  app@example.com
(connect to mx.example.net[203.0.113.9]:25: Connection timed out)
                                         deferred@example.net

A1B2C3D4E5F!    891 Fri Aug  8 10:14:31  app@example.com
                                         held@example.net

6D7E8F9A0B*#    512 Fri Aug  8 10:15:31  app@example.com
                                         expiring@example.net

7E8F9A0B1C#     512 Fri Aug  8 10:16:31  app@example.com
                                         expiring2@example.net

-- 2 Kbytes in 5 Requests.
`
	ids := parseQueueIDs(listing)
	want := []string{"3C5B04E6C1", "5B4A2C1D3E", "A1B2C3D4E5F", "6D7E8F9A0B", "7E8F9A0B1C"}
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
