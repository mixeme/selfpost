package auth

import (
	"sync"
	"time"
)

// rateLimiter is a simple fixed-window per-key counter used to throttle the
// setup and login routes (security.md). Keys are client IPs.
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

func (r *rateLimiter) sweep(now time.Time) {
	for k, b := range r.buckets {
		if now.After(b.windowEnds) {
			delete(r.buckets, k)
		}
	}
}
