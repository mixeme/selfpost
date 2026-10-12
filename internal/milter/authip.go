package milter

import (
	"errors"
	"log"

	"github.com/mixeme/selfpost/internal/store"
)

// authIPAllowed reports whether the authenticated application may submit from
// the connecting client IP. When the application has no IP restriction the
// check passes. When it has one and the client's address is not known, the
// check fails: an address list that an unknown address satisfies is no list.
// Store errors are fail-open (see overLimit).
func (s *session) authIPAllowed() bool {
	if s.login == "" {
		return true
	}
	a, err := s.rec.ApplicationByLogin(s.login)
	if err != nil {
		if errors.Is(err, store.ErrApplicationNotFound) {
			return true
		}
		log.Printf("journal-milter: auth IP lookup application %q: %v (fail-open)", s.login, err)
		return true
	}
	if a.AllowsAuthFromIP(s.clientIP) {
		return true
	}
	if s.clientIP == "" {
		log.Printf("journal-milter: application %q refused — its client IPs are restricted and Postfix gave no client address", s.login)
		return false
	}
	log.Printf("journal-milter: application %q refused from %s — client IP not allowed", s.login, s.clientIP)
	return false
}
