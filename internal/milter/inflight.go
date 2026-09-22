package milter

import (
	"sync"
	"time"
)

// reservationTTL bounds how long a message may stay reserved. A reservation is
// released at end-of-message or on ABORT, but a client that simply drops the
// connection after MAIL FROM produces neither callback (go-milter has no
// connection-close hook), and a reservation that never expired would count
// against the limit forever — a fail-closed drift this milter must not have.
// The TTL is generously longer than any realistic DATA transfer, so a message
// still being received is never dropped from the count.
const reservationTTL = 10 * time.Minute

// reservation is one message that passed the level-2 check and has not been
// written to the send log yet.
type reservation struct {
	key string
	at  time.Time
}

// inflight counts messages that are between the limit check (MAIL FROM) and the
// send-log insert (end-of-message). The stored count alone cannot see them, so
// without this several concurrent SMTP sessions each read the same pre-insert
// count, each conclude they are under the ceiling, and the limit is overshot by
// however many were in flight. Counting reservations closes that window without
// writing placeholder rows the operator would see in the UI.
//
// One instance is shared by every session of the process, hence the mutex.
// Methods tolerate a nil receiver so a session built without one (tests) simply
// behaves as it did before.
type inflight struct {
	mu sync.Mutex
	m  map[string]map[*reservation]struct{}
}

// tryAdmit decides whether one more message may be sent for key and, if so,
// claims its slot — both under a single lock. Counting and reserving in two
// separate critical sections would let two SMTP sessions that reach MAIL FROM
// at the same moment observe the same total and each take the last free slot,
// which is exactly the overshoot the in-flight registry exists to prevent.
//
// stored is the count the send log already holds for the limit's window and max
// is the ceiling; the caller supplies both because only it can query the store.
// The returned total is what was measured, for the refusal log line.
func (f *inflight) tryAdmit(key string, since time.Time, stored, max int64) (*reservation, int64, bool) {
	if f == nil {
		return nil, stored, stored < max // no in-flight accounting (tests)
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	total := stored + f.countLocked(key, since)
	if total >= max {
		return nil, total, false
	}
	return f.reserveLocked(key), total, true
}

// count returns how many reservations for key were taken within the limit's
// window (at or after since), pruning any that outlived reservationTTL.
func (f *inflight) count(key string, since time.Time) int64 {
	if f == nil {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.countLocked(key, since)
}

// countLocked is count's body; the caller holds mu.
func (f *inflight) countLocked(key string, since time.Time) int64 {
	set := f.m[key]
	cutoff := time.Now().Add(-reservationTTL)
	var n int64
	for r := range set {
		if r.at.Before(cutoff) {
			delete(set, r)
			continue
		}
		if !r.at.Before(since) {
			n++
		}
	}
	if len(set) == 0 {
		delete(f.m, key)
	}
	return n
}

// reserveLocked is reserve's body; the caller holds mu.
func (f *inflight) reserveLocked(key string) *reservation {
	if f.m == nil {
		f.m = make(map[string]map[*reservation]struct{})
	}
	if f.m[key] == nil {
		f.m[key] = make(map[*reservation]struct{})
	}
	r := &reservation{key: key, at: time.Now()}
	f.m[key][r] = struct{}{}
	return r
}

// release drops a reservation, either because the message reached the send log
// (where the stored count takes over) or because it never will.
func (f *inflight) release(r *reservation) {
	if f == nil || r == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	set := f.m[r.key]
	delete(set, r)
	if len(set) == 0 {
		delete(f.m, r.key)
	}
}
