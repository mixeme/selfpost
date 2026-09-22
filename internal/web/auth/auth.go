// Package auth implements the panel's login sessions, one-time setup flow,
// and authentication middleware.
package auth

import (
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/view"
)

// Config holds auth-specific panel configuration.
type Config struct {
	CookieSecure      bool
	Hostname          string
	SessionIdleDays   int
	TrustedProxyCIDRs []*net.IPNet
}

// Module handles login, logout, setup, and session middleware.
type Module struct {
	store    *store.Store
	cfg      Config
	view     *view.Engine
	sessions *sessionStore
	setup    *setupManager

	loginLimiter *rateLimiter
	setupLimiter *rateLimiter

	trustedProxies []*net.IPNet
}

// New builds the auth module. setupTokenPath is where the current setup token
// is mirrored on disk (security.md).
func New(st *store.Store, cfg Config, v *view.Engine, setupTokenPath string) *Module {
	idleDays := cfg.SessionIdleDays
	if idleDays <= 0 {
		idleDays = 7
	}
	m := &Module{
		store:          st,
		cfg:            cfg,
		view:           v,
		sessions:       newSessionStore(st, time.Duration(idleDays)*24*time.Hour),
		setupLimiter:   newRateLimiter(10, time.Minute),
		loginLimiter:   newRateLimiter(10, 15*time.Minute),
		trustedProxies: cfg.TrustedProxyCIDRs,
	}
	m.setup = newSetupManager(st, cfg.Hostname, setupTokenPath)
	return m
}

// Bootstrap runs once at startup. If setup is not complete it mints and
// announces the first setup token (security.md).
func (m *Module) Bootstrap() error {
	return m.setup.bootstrap()
}

// AllowLoginAttempt reports whether a login or account-password change attempt
// from r is within the rate limit (security.md).
func (m *Module) AllowLoginAttempt(r *http.Request) bool {
	return m.loginLimiter.Allow(clientIP(r, m.trustedProxies))
}

// SessionToken returns the session token the request carries, if exactly one
// cookie of that name is present.
func (m *Module) SessionToken(r *http.Request) (string, bool) {
	return m.sessionToken(r)
}

// RenameSession updates the username carried by a session.
func (m *Module) RenameSession(token, username string) {
	m.sessions.Rename(token, username)
}

// DestroyOtherSessions invalidates every session except keep.
func (m *Module) DestroyOtherSessions(keep string) {
	m.sessions.DestroyOthers(keep)
}

func logf(format string, args ...any) {
	log.Printf(format, args...)
}

// clientIP extracts the peer IP for rate-limiting. By default it is the
// transport peer (RemoteAddr), which cannot be spoofed. If RemoteAddr matches
// one of trustedProxies, the last entry of X-Forwarded-For is used instead —
// that is the address the trusted proxy itself appended, so a client can't
// forge it by sending its own XFF header.
func clientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	if len(trustedProxies) > 0 {
		if peer := net.ParseIP(host); peer != nil && ipInAny(peer, trustedProxies) {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				parts := strings.Split(xff, ",")
				if ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); ip != nil {
					return ip.String()
				}
			}
		}
	}

	return host
}

func ipInAny(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
