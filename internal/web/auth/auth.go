// Package auth implements the panel's login sessions, one-time setup flow,
// and authentication middleware.
package auth

import (
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
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

	// loginLimiter counts failed sign-ins per address and username,
	// loginAddrLimiter per address alone at ten times that; setupLimiter
	// counts setup requests per address. Neither address bucket is one a
	// stranger behind the same proxy fills for everyone at the per-user size
	// (limitkeys.go).
	loginLimiter     *rateLimiter
	loginAddrLimiter *rateLimiter
	setupLimiter     *rateLimiter
	passwordLimiter  *rateLimiter
	// resetLimiter counts the requests for a reset link per address and
	// username, resetAddrLimiter per address alone; linkLimiter the requests
	// a link's page gets per link, linkAddrLimiter per address at a high
	// ceiling; mailLimiter the links mailed to one user's address (links.go).
	resetLimiter     *rateLimiter
	resetAddrLimiter *rateLimiter
	linkLimiter      *rateLimiter
	linkAddrLimiter  *rateLimiter
	mailLimiter      *rateLimiter

	proxyWatch proxyWatch
	refusals   *refusalLog
	mailer     Mailer
	// mailing counts the links being mailed after their answer, so a stop
	// waits for them (Wait) before the database closes.
	mailing sync.WaitGroup

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
		store:            st,
		cfg:              cfg,
		view:             v,
		sessions:         newSessionStore(st, time.Duration(idleDays)*24*time.Hour),
		setupLimiter:     newRateLimiter(10, time.Minute),
		loginLimiter:     newRateLimiter(10, 15*time.Minute),
		loginAddrLimiter: newRateLimiter(100, 15*time.Minute),
		// The Account form's current-password check has a bucket of its
		// own, per user: ten wrong guesses there used to lock the address
		// out of signing in (audit 2026-10-12, L15), and the one guessing
		// is whoever holds that user's session, wherever from.
		passwordLimiter:  newRateLimiter(10, 15*time.Minute),
		resetLimiter:     newRateLimiter(5, 15*time.Minute),
		resetAddrLimiter: newRateLimiter(50, 15*time.Minute),
		linkLimiter:      newRateLimiter(30, 15*time.Minute),
		linkAddrLimiter:  newRateLimiter(300, 15*time.Minute),
		refusals:         newRefusalLog(),
		// Five links an hour to one address, of either kind: three resets
		// asked for and a couple of confirmations is ordinary use, a mailbox
		// full of them is not.
		mailLimiter:    newRateLimiter(5, time.Hour),
		trustedProxies: cfg.TrustedProxyCIDRs,
	}
	m.setupLimiter.startSweeper()
	m.loginLimiter.startSweeper()
	m.loginAddrLimiter.startSweeper()
	m.resetAddrLimiter.startSweeper()
	m.linkAddrLimiter.startSweeper()
	m.refusals.limiter.startSweeper()
	m.passwordLimiter.startSweeper()
	m.resetLimiter.startSweeper()
	m.linkLimiter.startSweeper()
	m.mailLimiter.startSweeper()
	// Made now rather than at the first unknown name, which would otherwise
	// pay for two bcrypt runs (review of audit stage 4).
	go noSuchUserHash()
	m.setup = newSetupManager(st, cfg.Hostname, setupTokenPath)
	return m
}

// Bootstrap runs once at startup. If setup is not complete it mints and
// announces the first setup token (security.md).
func (m *Module) Bootstrap() error {
	return m.setup.bootstrap()
}

// AllowPasswordChange reports whether another current-password check on the
// Account form is within the user's rate limit (security.md).
func (m *Module) AllowPasswordChange(userID int64) bool {
	return m.passwordLimiter.Allow("user:" + strconv.FormatInt(userID, 10))
}

// PasswordCheckPassed gives back the attempt AllowPasswordChange took for a
// check that found the right password: only wrong guesses count, so an
// administrator who adds ten users in a quarter of an hour is not locked out
// of the next backup (review of audit 2026-10-17 stage 0).
func (m *Module) PasswordCheckPassed(userID int64) {
	m.passwordLimiter.Refund("user:" + strconv.FormatInt(userID, 10))
}

// SessionToken returns the session token the request carries, if exactly one
// cookie of that name is present.
func (m *Module) SessionToken(r *http.Request) (string, bool) {
	return m.sessionToken(r)
}

// DestroyOtherSessions invalidates every session of the user except keep.
func (m *Module) DestroyOtherSessions(userID int64, keep string) {
	m.sessions.DestroyOthers(userID, keep)
}

// DestroyUserSessions invalidates every session of the user.
func (m *Module) DestroyUserSessions(userID int64) {
	m.sessions.DestroyAll(userID)
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

// SeenAddress is how the panel sees a request's address, for Health: the
// address it counts sign-ins against (Client), the transport peer (Peer),
// and whether Client came from X-Forwarded-For (FromHeader) or the header
// was there and left unread because the peer is not a trusted proxy
// (HeaderIgnored) - the case where every visitor shares the proxy's address
// and one sign-in limit.
type SeenAddress struct {
	Client        string
	Peer          string
	FromHeader    bool
	HeaderIgnored bool
}

// Seen reads a request's address as clientIP does.
func (m *Module) Seen(r *http.Request) SeenAddress {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	s := SeenAddress{Client: clientIP(r, m.trustedProxies), Peer: peer}
	hasHeader := r.Header.Get("X-Forwarded-For") != ""
	s.FromHeader = hasHeader && s.Client != peer
	s.HeaderIgnored = hasHeader && !s.FromHeader
	return s
}

func ipInAny(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
