package milter

import (
	"log"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

// overLimit reports whether the message currently being received should be
// refused under a level-2 differentiated limit (README § Rate limiting). It
// checks the domain-level and application-level limits in turn; either being
// exceeded is enough to refuse.
//
// It is deliberately fail-open: any store error, or the absence of a usable
// limit, is treated as "not over limit" so a malfunction of the level-2
// limiter can never block mail — Postfix's level-1 anvil limit
// (architecture.md § Mail path) remains the backstop, and it does not depend
// on this milter at all. Only a clean count at or above a configured ceiling
// returns true.
//
// A message that passes reserves a slot per applicable limit, released once it
// reaches the send log (or is abandoned) — see inflight for why the stored
// count alone is not enough.
func (s *session) overLimit() bool {
	if s.clientIP == "" {
		return false // no client IP to key on; level-2 does not apply
	}
	checks := []struct{ scope, ref string }{
		{store.RateLimitScopeDomain, domainOf(s.from)},
		{store.RateLimitScopeApp, s.login},
	}
	var taken []*reservation
	for _, c := range checks {
		if c.ref == "" {
			continue
		}
		rl, ok, err := s.rec.RateLimit(c.scope, c.ref)
		if err != nil {
			log.Printf("journal-milter: rate-limit lookup %s %q: %v (fail-open)", c.scope, c.ref, err)
			continue
		}
		// No limit configured, an inert draft, or a client IP outside the
		// registered set: the differentiated limit does not apply here.
		if !ok || !rl.Active() || !rl.AllowsIP(s.clientIP) {
			continue
		}
		since := time.Now().Add(-time.Duration(rl.WindowSeconds) * time.Second)
		n, err := s.rec.CountMessages(c.scope, c.ref, since)
		if err != nil {
			log.Printf("journal-milter: rate-limit count %s %q: %v (fail-open)", c.scope, c.ref, err)
			continue
		}
		key := c.scope + "|" + c.ref
		n += s.flight.count(key, since)
		if n >= int64(rl.MaxMessages) {
			log.Printf("journal-milter: %s %q over limit: %d/%d in %ds from %s — refusing 4xx",
				c.scope, c.ref, n, rl.MaxMessages, rl.WindowSeconds, s.clientIP)
			// The message is refused, so the slots claimed for the limits
			// checked before this one must not stay claimed.
			for _, r := range taken {
				s.flight.release(r)
			}
			return true
		}
		taken = append(taken, s.flight.reserve(key))
	}
	s.reserved = append(s.reserved, taken...)
	return false
}

// releaseReservations gives back every slot this message holds. It runs once
// the message is in the send log (where the stored count sees it), and whenever
// the transaction ends without getting there.
func (s *session) releaseReservations() {
	for _, r := range s.reserved {
		s.flight.release(r)
	}
	s.reserved = nil
}

// recordRejected writes a send-log row for a message refused by a level-2
// limit (README § Rate limiting — refusals are recorded too), so the rejection
// shows up in the monitoring screen. Only MAIL-stage fields are known; the
// write is best-effort and never affects the response.
func (s *session) recordRejected() {
	err := s.rec.InsertRejected(store.SendLogEntry{
		Domain:   domainOf(s.from),
		AppLogin: s.login,
		From:     s.from,
	})
	if err != nil {
		log.Printf("journal-milter: record rejected %s: %v", s.from, err)
	}
}
