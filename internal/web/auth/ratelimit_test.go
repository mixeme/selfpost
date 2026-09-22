package auth

import (
	"fmt"
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

// A long-running panel can see many unique client addresses. Finished buckets
// are swept on every new window, and a hard cap evicts the oldest when the map
// would otherwise grow without bound.
func TestRateLimiterCapsBucketCount(t *testing.T) {
	r := newRateLimiter(1, time.Minute)
	r.maxBuckets = 3

	for i, key := range []string{"203.0.113.7", "198.51.100.9", "192.0.2.5"} {
		if !r.Allow(key) {
			t.Fatalf("attempt %d for %s was refused under the cap", i+1, key)
		}
		expire(r, key)
	}

	if !r.Allow("203.0.113.8") {
		t.Fatal("a fourth address was refused even though room was made")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buckets) > 3 {
		t.Fatalf("bucket count = %d, want at most 3", len(r.buckets))
	}
}

// A flood of new addresses must not be able to reset the limiter for an address
// that is already blocked: below the hard ceiling, blocked buckets survive
// eviction even though the map is over its soft cap.
func TestRateLimiterKeepsBlockedBucketsUnderPressure(t *testing.T) {
	r := newRateLimiter(2, time.Minute)
	r.maxBuckets = 3

	blocked := []string{"203.0.113.7", "198.51.100.9", "192.0.2.5"}
	for _, key := range blocked {
		for i := 0; i < 2; i++ {
			if !r.Allow(key) {
				t.Fatalf("%s refused before reaching the limit", key)
			}
		}
		if r.Allow(key) {
			t.Fatalf("%s was not blocked after 2 attempts", key)
		}
	}

	// Fresh addresses arrive and find nothing evictable.
	for i := 0; i < 10; i++ {
		r.Allow(fmt.Sprintf("198.51.100.%d", 100+i))
	}

	for _, key := range blocked {
		if r.Allow(key) {
			t.Fatalf("%s lost its block after new addresses arrived", key)
		}
	}
}

// Protecting blocked buckets is bounded: past maxBuckets * blockedBucketCeiling
// they are evicted too, so an attacker cannot grow the map without limit by
// burning through source addresses.
func TestRateLimiterBoundsBlockedBuckets(t *testing.T) {
	r := newRateLimiter(1, time.Minute)
	r.maxBuckets = 2
	hardCap := r.maxBuckets * blockedBucketCeiling

	for i := 0; i < 100; i++ {
		r.Allow(fmt.Sprintf("192.0.2.%d", i))
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buckets) > hardCap {
		t.Fatalf("bucket count = %d, want at most the hard cap %d", len(r.buckets), hardCap)
	}
}
