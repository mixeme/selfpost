package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
)

// The keys the public routes are limited by (security audit 2026-10-17, A1,
// A8, A9).
//
// A client's address is the one clientIP reads, an IPv6 address cut to its
// /64: a client with a /64 to itself rotates addresses inside it at will
// and never met a per-address limit (A9). Behind a reverse proxy the panel
// does not trust, every visitor has the proxy's address; so no limit that a
// stranger can fill is keyed on the address alone at a size that stops the
// others: sign-in counts per address and username, the address alone only
// at a ceiling ten times higher; a link page counts per link; and a right
// password gives its attempt back (A1). proxyWatch says in the log when the
// shared-address case is what the panel sees.

// limitAddr is the client's address as the limiters key it.
func limitAddr(r *http.Request, trusted []*net.IPNet) string {
	host := clientIP(r, trusted)
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() != nil {
		return host
	}
	return (&net.IPNet{IP: ip.Mask(net.CIDRMask(64, 128)), Mask: net.CIDRMask(64, 128)}).String()
}

// userKey is a limiter key for one username tried from one address. The
// name is folded as the store folds it (usernames are matched without
// regard to case).
func userKey(addr, username string) string {
	return addr + "|" + strings.ToLower(strings.TrimSpace(username))
}

// tokenKey is a limiter key for one link: its hash, so a limiter's map holds
// no token.
func tokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "link:" + hex.EncodeToString(sum[:8])
}

// logName is a typed username as a log line may hold it: printable ASCII,
// cut. It is the stranger's text.
func logName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) {
			return '?'
		}
		return r
	}, s)
	if len(s) > 64 {
		s = s[:64] + "…"
	}
	return s
}

// proxyWatchRequests is how many requests in a row must come from one
// private peer with X-Forwarded-For, while no proxy is trusted, before the
// panel says so.
const proxyWatchRequests = 20

// proxyWatch looks at the first requests after the start. When they all
// come from one private (or loopback) peer that carries X-Forwarded-For and
// TRUSTED_PROXY_CIDR is empty, every visitor shares that peer's sign-in
// limits: it says so once, with the value to set. Any other request - no
// header, another peer, a public one - ends the watch silently.
type proxyWatch struct {
	mu   sync.Mutex
	peer string
	n    int
	done bool
}

func (w *proxyWatch) see(r *http.Request, trusted []*net.IPNet) {
	if len(trusted) > 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done {
		return
	}
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	ip := net.ParseIP(peer)
	if ip == nil || !(ip.IsPrivate() || ip.IsLoopback()) || r.Header.Get("X-Forwarded-For") == "" ||
		(w.peer != "" && w.peer != peer) {
		w.done = true
		return
	}
	w.peer = peer
	if w.n++; w.n >= proxyWatchRequests {
		w.done = true
		logf("panel: the last %d requests all came from %s with X-Forwarded-For, which is ignored while TRUSTED_PROXY_CIDR is empty: "+
			"every visitor shares one sign-in limit. If %s is your reverse proxy, set TRUSTED_PROXY_CIDR=%s (guide § Environment variables)",
			w.n, peer, peer, peer)
	}
}

// refusalLog lets a refused sign-in or a lockout be logged a few times per
// address in a window, so the log shows a brute force without becoming one.
type refusalLog struct{ limiter *rateLimiter }

func newRefusalLog() *refusalLog {
	return &refusalLog{limiter: newRateLimiter(5, 15*time.Minute)}
}

func (l *refusalLog) printf(addr, format string, args ...any) {
	if l.limiter.Allow(addr) {
		logf(format, args...)
	}
}
