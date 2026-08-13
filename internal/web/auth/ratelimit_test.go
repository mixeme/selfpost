package auth

import (
	"testing"
	"time"
)

// The limiter is what stands between the public login form and an unlimited
// guessing rate (security.md), so the ceiling has to be exact: the configured
// number of attempts go through and the next one does not, however often it is
// repeated.
func TestRateLimiterStopsAtTheCeiling(t *testing.T) {
	r := newRateLimiter(3, time.Minute)

	for i := 1; i <= 3; i++ {
		if !r.Allow("203.0.113.7") {
			t.Fatalf("attempt %d of 3 was refused before the ceiling", i)
		}
	}
	for i := 4; i <= 6; i++ {
		if r.Allow("203.0.113.7") {
			t.Fatalf("attempt %d passed after the ceiling of 3", i)
		}
	}
}

// Buckets are per key, so one locked-out address must not lock out the rest of
// the internet — a shared counter would turn a single guesser into a denial of
// service against every operator.
func TestRateLimiterKeepsKeysApart(t *testing.T) {
	r := newRateLimiter(1, time.Minute)

	if !r.Allow("203.0.113.7") || r.Allow("203.0.113.7") {
		t.Fatal("the first key did not use up its single attempt")
	}
	if !r.Allow("198.51.100.9") {
		t.Fatal("a second address was refused because another one was locked out")
	}
}

// The window is fixed, not sliding: once it has elapsed the count starts again
// from zero rather than being carried over. Time is moved by ageing the bucket
// instead of sleeping, so the test states the boundary rather than approaching
// it.
func TestRateLimiterReopensAfterTheWindow(t *testing.T) {
	r := newRateLimiter(2, time.Minute)
	r.Allow("203.0.113.7")
	r.Allow("203.0.113.7")
	if r.Allow("203.0.113.7") {
		t.Fatal("the ceiling was not reached")
	}

	expire(r, "203.0.113.7")

	if !r.Allow("203.0.113.7") {
		t.Fatal("the key is still locked out after its window ended")
	}
	if !r.Allow("203.0.113.7") {
		t.Fatal("the new window did not start from an empty count")
	}
	if r.Allow("203.0.113.7") {
		t.Fatal("the new window allowed more than the ceiling")
	}
}

// Every address that ever tried to sign in gets a bucket, and the only thing
// that removes the finished ones is the sweep on a new window. It runs on the
// key that triggered it as well as on the others, so a long-running panel does
// not accumulate a bucket per source address for ever.
func TestRateLimiterSweepsFinishedBuckets(t *testing.T) {
	r := newRateLimiter(2, time.Minute)
	for _, key := range []string{"203.0.113.7", "198.51.100.9"} {
		r.Allow(key)
		expire(r, key)
	}
	r.Allow("192.0.2.5") // still inside its window

	r.Allow("203.0.113.7") // new window for this key: sweeps the rest

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.buckets["198.51.100.9"]; ok {
		t.Error("a bucket whose window ended is still held")
	}
	for _, key := range []string{"203.0.113.7", "192.0.2.5"} {
		if _, ok := r.buckets[key]; !ok {
			t.Errorf("the sweep dropped %s, whose window is still open", key)
		}
	}
}

// expire moves a key's window into the past, the same state it would reach by
// waiting for the window to elapse.
func expire(r *rateLimiter, key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b := r.buckets[key]; b != nil {
		b.windowEnds = time.Now().Add(-time.Second)
	}
}
