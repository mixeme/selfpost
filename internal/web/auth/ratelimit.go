package auth

import (
	"sync"
	"time"
)

const defaultMaxBuckets = 4096

// rateLimiter is a simple fixed-window per-key counter used to throttle the
// setup and login routes (security.md). Keys are client IPs.
type rateLimiter struct {
	max        int
	window     time.Duration
	maxBuckets int

	mu      sync.Mutex
	buckets map[string]*rlBucket
}

type rlBucket struct {
	count      int
	windowEnds time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		max:        max,
		window:     window,
		maxBuckets: defaultMaxBuckets,
		buckets:    make(map[string]*rlBucket),
	}
}

func (r *rateLimiter) startSweeper() {
	go func() {
		ticker := time.NewTicker(r.window)
		defer ticker.Stop()
		for range ticker.C {
			r.mu.Lock()
			r.sweep(time.Now())
			r.mu.Unlock()
		}
	}()
}

func (r *rateLimiter) Allow(key string) bool {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()

	b := r.buckets[key]
	if b == nil || now.After(b.windowEnds) {
		r.makeRoom(now)
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

func (r *rateLimiter) makeRoom(now time.Time) {
	if r.maxBuckets <= 0 || len(r.buckets) < r.maxBuckets {
		return
	}
	r.sweep(now)
	for len(r.buckets) >= r.maxBuckets {
		r.evictOldest()
	}
}

func (r *rateLimiter) evictOldest() {
	var oldestKey string
	var oldestEnds time.Time
	first := true
	for k, b := range r.buckets {
		if first || b.windowEnds.Before(oldestEnds) {
			oldestKey = k
			oldestEnds = b.windowEnds
			first = false
		}
	}
	if oldestKey != "" {
		delete(r.buckets, oldestKey)
	}
}

func (r *rateLimiter) sweep(now time.Time) {
	for k, b := range r.buckets {
		if now.After(b.windowEnds) {
			delete(r.buckets, k)
		}
	}
}
