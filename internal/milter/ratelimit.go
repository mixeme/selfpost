package milter

import (
	"errors"
	"log"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

// overLimit reports whether the message currently being received should be
// refused under a level-2 differentiated limit (guide § Rate limiting).
//
// When an application has an active limit, it overrides the domain limit for
// that login (the ceiling may be higher or lower than the domain). Otherwise
// the domain ceiling applies when configured; if neither is set, only level 1
// applies.
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
	// A ceiling is on a domain or an application, whatever address the mail
	// comes from, so the client address is not asked for here. It used to be,
	// and a message that arrived without one was let through uncounted.
	if s.login != "" {
		rl, ok, err := s.rec.RateLimit(store.RateLimitScopeApp, s.login)
		if err != nil {
			log.Printf("journal-milter: rate-limit lookup application %q: %v (fail-open)", s.login, err)
		} else if ok && rl.Active() {
			return s.enforceLimit(store.RateLimitScopeApp, s.login, rl)
		}
	}

	// The domain is the login's, never the envelope's. MAIL FROM reaches
	// this milter before Postfix has held it to the login - with
	// smtpd_delay_reject at its default no sender restriction runs at MAIL
	// (Postfix 3.10.13, src/smtpd/smtpd.c, mail_cmd()) - so a domain read
	// from it let a stranger on 465/587, with no login at all, or another
	// tenant's application reserve a victim domain's slots (security audit
	// 2026-10-17, M1). A session without a login can deliver nothing on
	// those ports and reserves nothing; the send log counts a domain's
	// messages by the login's domain too (store.CountMessages).
	if s.login == "" {
		return false
	}
	domain := s.loginDomain()
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
// limit (guide § Rate limiting — refusals are recorded too) or for its client
// address, so the rejection shows up in the monitoring screen with its reason
// (store.Reason*). Only MAIL-stage fields are known; the write is best-effort
// and never affects the response.
func (s *session) recordRejected(reason string) {
	err := s.rec.InsertRejected(store.SendLogEntry{
		Domain:   s.loginDomain(),
		AppLogin: s.login,
		From:     s.from,
		Reason:   reason,
		ClientIP: s.clientIP, // Postfix's line about the refusal names it
	})
	if err != nil {
		log.Printf("journal-milter: record rejected %s: %v", s.from, err)
	}
}

// loginDomain is the domain of the session's application, "" without a login
// or when the store cannot say (fail-open, as overLimit is).
func (s *session) loginDomain() string {
	if s.login == "" {
		return ""
	}
	a, err := s.rec.ApplicationByLogin(s.login)
	if err != nil {
		if !errors.Is(err, store.ErrApplicationNotFound) {
			log.Printf("journal-milter: domain of application %q: %v (fail-open)", s.login, err)
		}
		return ""
	}
	return strings.ToLower(a.DomainName)
}
