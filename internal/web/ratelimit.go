package web

import (
	"sync"
	"time"
)

// rateLimiter is a simple fixed-window per-key counter used to throttle the
// setup and login routes (spec 7.6.1, 7.6.5). Keys are client IPs. It is not a
// precise sliding window — a coarse backstop against brute-force and log noise
// is all these routes need.
type rateLimiter struct {
	max    int
	window time.Duration

	mu      sync.Mutex
	buckets map[string]*rlBucket
}

type rlBucket struct {
	count      int
	windowEnds time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		max:     max,
		window:  window,
		buckets: make(map[string]*rlBucket),
	}
}

// Allow records an attempt for key and reports whether it is within the limit.
// The current window is reset lazily once it elapses.
func (r *rateLimiter) Allow(key string) bool {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()

	b := r.buckets[key]
	if b == nil || now.After(b.windowEnds) {
		r.buckets[key] = &rlBucket{count: 1, windowEnds: now.Add(r.window)}
		r.sweep(now)
		return true
	}
	if b.count >= r.max {
		return false
	}
	b.count++
	return true
}

// sweep drops expired buckets so the map cannot grow without bound. Called
// under the lock while a window is being reset, which is often enough given the
// low request volume of these routes.
func (r *rateLimiter) sweep(now time.Time) {
	for k, b := range r.buckets {
		if now.After(b.windowEnds) {
			delete(r.buckets, k)
		}
	}
}
