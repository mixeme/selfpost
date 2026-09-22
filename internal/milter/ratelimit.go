package milter

import (
	"log"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

// overLimit reports whether the message currently being received should be
// refused under a level-2 differentiated limit (guide § Rate limiting).
//
// Trusted application IPs (app limit active and client IP listed) use only the
// app ceiling and skip the domain check. Everyone else is under the domain
// ceiling when one is configured; otherwise only level 1 applies.
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

	if s.login != "" {
		rl, ok, err := s.rec.RateLimit(store.RateLimitScopeApp, s.login)
		if err != nil {
			log.Printf("journal-milter: rate-limit lookup application %q: %v (fail-open)", s.login, err)
		} else if ok && rl.Active() && rl.AllowsIP(s.clientIP) {
			return s.enforceLimit(store.RateLimitScopeApp, s.login, rl)
		}
	}

	domain := domainOf(s.from)
	if domain == "" {
		return false
	}
	rl, ok, err := s.rec.RateLimit(store.RateLimitScopeDomain, domain)
	if err != nil {
		log.Printf("journal-milter: rate-limit lookup domain %q: %v (fail-open)", domain, err)
		return false
	}
	if !ok || !rl.Active() {
		return false
	}
	return s.enforceLimit(store.RateLimitScopeDomain, domain, rl)
}

// enforceLimit counts recent messages for scope/ref and refuses when at or
// above the ceiling. The stored count and the in-flight slots are weighed and
// the admitted message's own slot is taken in one atomic step (tryAdmit), so
// two sessions racing at MAIL FROM cannot both claim the last free slot.
func (s *session) enforceLimit(scope, ref string, rl store.RateLimit) bool {
	since := time.Now().Add(-time.Duration(rl.WindowSeconds) * time.Second)
	stored, err := s.rec.CountMessages(scope, ref, since)
	if err != nil {
		log.Printf("journal-milter: rate-limit count %s %q: %v (fail-open)", scope, ref, err)
		return false
	}
	r, n, ok := s.flight.tryAdmit(scope+"|"+ref, since, stored, int64(rl.MaxMessages))
	if !ok {
		log.Printf("journal-milter: %s %q over limit: %d/%d in %ds from %s — refusing 4xx",
			scope, ref, n, rl.MaxMessages, rl.WindowSeconds, s.clientIP)
		return true
	}
	s.reserved = append(s.reserved, r)
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
// limit (guide § Rate limiting — refusals are recorded too), so the rejection
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
