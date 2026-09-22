package auth

import (
	"sync"
	"time"
)

const defaultMaxBuckets = 4096

// blockedBucketCeiling is how far past maxBuckets the map may grow while every
// bucket in it is blocked. Blocked buckets are normally protected from
// eviction (see evictNearest), but protecting them unconditionally would let
// an attacker with many source addresses grow the map without bound — each
// address only has to fail max times. Past the ceiling blocked buckets are
// evicted too, nearest expiry first: bounded memory wins over holding every
// block, and the addresses that lose their bucket have to spend max attempts
// again to get back to blocked.
const blockedBucketCeiling = 4

// rateLimiter is a simple fixed-window per-key counter used to throttle the
// setup and login routes (security.md). Keys are client IPs.
type rateLimiter struct {
	max        int
	window     time.Duration
	maxBuckets int

	mu           sync.Mutex
	buckets      map[string]*rlBucket
	lastOverflow time.Time
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
	hardCap := r.maxBuckets * blockedBucketCeiling
	for len(r.buckets) >= r.maxBuckets {
		if r.evictNearest(false) {
			continue
		}
		// Nothing evictable is left: every remaining bucket is blocked. Hold
		// them until the map reaches the hard ceiling, then start evicting
		// blocked buckets as well so memory stays bounded.
		if len(r.buckets) < hardCap {
			break
		}
		if !r.evictNearest(true) {
			break
		}
		if now.Sub(r.lastOverflow) >= r.window {
			r.lastOverflow = now
			logf("panel: login rate limiter holds %d blocked buckets (cap %d) — evicting blocked entries; "+
				"this means many distinct client addresses are failing authentication", len(r.buckets), r.maxBuckets)
		}
	}
}

// evictNearest removes the bucket whose window ends soonest and reports whether
// it removed one. Blocked buckets (count >= max) are skipped unless evictBlocked
// is set, so an attacker cannot flush active rate-limit state by filling the map
// with new keys.
func (r *rateLimiter) evictNearest(evictBlocked bool) bool {
	var bestKey string
	var bestEnds time.Time
	first := true
	for k, b := range r.buckets {
		if !evictBlocked && b.count >= r.max {
			continue
		}
		if first || b.windowEnds.Before(bestEnds) {
			bestKey = k
			bestEnds = b.windowEnds
			first = false
		}
	}
	if bestKey == "" {
		return false
	}
	delete(r.buckets, bestKey)
	return true
}

func (r *rateLimiter) sweep(now time.Time) {
	for k, b := range r.buckets {
		if now.After(b.windowEnds) {
			delete(r.buckets, k)
		}
	}
}
