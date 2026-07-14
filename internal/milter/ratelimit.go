package milter

import (
	"log"
	"time"

	"codeberg.org/mix/selfpost/internal/store"
)

// overLimit reports whether the message currently being received should be
// refused under a level-2 differentiated limit (spec 7.4). It checks the
// domain-level and application-level limits in turn; either being exceeded is
// enough to refuse.
//
// It is deliberately fail-open: any store error, or the absence of a usable
// limit, is treated as "not over limit" so a malfunction of the level-2 limiter
// can never block mail — Postfix's level-1 anvil limit (spec 5) remains the
// backstop, and it does not depend on this milter at all. Only a clean count at
// or above a configured ceiling returns true.
func (s *session) overLimit() bool {
	if s.clientIP == "" {
		return false // no client IP to key on; level-2 does not apply
	}
	checks := []struct{ scope, ref string }{
		{store.RateLimitScopeDomain, domainOf(s.from)},
		{store.RateLimitScopeApp, s.login},
	}
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
		if n >= int64(rl.MaxMessages) {
			log.Printf("journal-milter: %s %q over limit: %d/%d in %ds from %s — refusing 4xx",
				c.scope, c.ref, n, rl.MaxMessages, rl.WindowSeconds, s.clientIP)
			return true
		}
	}
	return false
}

// recordRejected writes a send-log row for a message refused by a level-2 limit
// (spec 7.4, "опционально фиксирует ... для видимости в UI"), so the rejection
// shows up in the monitoring screen. Only MAIL-stage fields are known; the write
// is best-effort and never affects the response.
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
