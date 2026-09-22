package milter

import (
	"errors"
	"log"

	"github.com/mixeme/selfpost/internal/store"
)

// authIPAllowed reports whether the authenticated application may submit from
// the connecting client IP. When the application has no IP restriction, or the
// client IP is not known, the check passes. Store errors are fail-open (see
// overLimit).
func (s *session) authIPAllowed() bool {
	if s.login == "" || s.clientIP == "" {
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
	log.Printf("journal-milter: application %q refused from %s — client IP not allowed", s.login, s.clientIP)
	return false
}
