package auth

import (
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Behind a reverse proxy the panel does not trust, every visitor has the
// proxy's address. A stranger there who guesses one name, or sprays many,
// must not lock out an administrator signing in from behind the same proxy
// (security audit 2026-10-17, A1: the reader's test, two clients behind one
// untrusted peer).
func TestLoginBehindOneUntrustedProxyKeepsOthersIn(t *testing.T) {
	m := moduleWithAdmin(t)
	const proxy = "172.18.0.1:40000"
	for i := 0; i < 15; i++ {
		postLogin(m, proxy, "admin2", "guess")
	}
	if rec := postLogin(m, proxy, "admin", testPassword); rec.Code != http.StatusSeeOther {
		t.Fatalf("the administrator behind the same proxy = %d after a stranger's guesses at another name", rec.Code)
	}
	// Guessing the administrator's own name locks that name from there,
	// not the others.
	for i := 0; i < 10; i++ {
		postLogin(m, proxy, "admin", "guess")
	}
	if rec := postLogin(m, proxy, "admin", testPassword); rec.Code != http.StatusTooManyRequests {
		t.Errorf("the guessed name after ten failures = %d, want 429", rec.Code)
	}
	// Spraying names: the address alone is stopped at a hundred failures.
	m = moduleWithAdmin(t)
	for i := 0; i < 100; i++ {
		postLogin(m, "203.0.113.50:1", "spray"+strings.Repeat("x", i%40)+string(rune('a'+i%26)), "guess")
	}
	if rec := postLogin(m, "203.0.113.50:1", "admin", testPassword); rec.Code != http.StatusTooManyRequests {
		t.Errorf("after a hundred failures from one address = %d, want 429", rec.Code)
	}
}

// A right password is no guess: a day of signing in uses up no limit
// (security audit 2026-10-17, A1 (c)).
func TestLoginSuccessesDoNotCount(t *testing.T) {
	m := moduleWithAdmin(t)
	for i := 0; i < 25; i++ {
		if rec := postLogin(m, "203.0.113.7:1", "admin", testPassword); rec.Code != http.StatusSeeOther {
			t.Fatalf("sign-in %d = %d", i+1, rec.Code)
		}
	}
}

// An IPv6 client is one client per /64: rotating addresses inside it
// gives no fresh limit (security audit 2026-10-17, A9).
func TestIPv6IsLimitedPerSlash64(t *testing.T) {
	m := moduleWithAdmin(t)
	for i := 0; i < 10; i++ {
		postLogin(m, "[2001:db8:1:2::"+strconv.Itoa(100+i)+"]:443", "admin", "guess")
	}
	if rec := postLogin(m, "[2001:db8:1:2:ffff::1]:443", "admin", testPassword); rec.Code != http.StatusTooManyRequests {
		t.Errorf("another address in the same /64 = %d, want 429", rec.Code)
	}
	if rec := postLogin(m, "[2001:db8:1:3::1]:443", "admin", testPassword); rec.Code != http.StatusSeeOther {
		t.Errorf("another /64 = %d, want a sign-in", rec.Code)
	}
	if got := limitAddr(httptest.NewRequest(http.MethodGet, "/", nil), nil); got != "192.0.2.1" {
		t.Errorf("an IPv4 address is keyed as %q", got)
	}
}

// When every request comes from one private peer with X-Forwarded-For and no
// proxy is trusted, the log says so once, with the value to set; a request
// without the header ends the watch (security audit 2026-10-17, A1 (a)).
// Refusals are logged, a few per address (A8).
func TestProxyWatchAndRefusalsAreLogged(t *testing.T) {
	var buf strings.Builder
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	var w proxyWatch
	req := func(xff bool) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/login", nil)
		r.RemoteAddr = "172.18.0.1:5000"
		if xff {
			r.Header.Set("X-Forwarded-For", "198.51.100.4")
		}
		return r
	}
	for i := 0; i < proxyWatchRequests+5; i++ {
		w.see(req(true), nil)
	}
	if n := strings.Count(buf.String(), "set TRUSTED_PROXY_CIDR=172.18.0.1"); n != 1 {
		t.Errorf("the warning was logged %d times:\n%s", n, buf.String())
	}
	buf.Reset()
	var quiet proxyWatch
	quiet.see(req(false), nil)
	for i := 0; i < proxyWatchRequests+5; i++ {
		quiet.see(req(true), nil)
	}
	if buf.Len() != 0 {
		t.Errorf("a watch ended by a request without the header logged:\n%s", buf.String())
	}

	m := moduleWithAdmin(t)
	for i := 0; i < 8; i++ {
		postLogin(m, "203.0.113.9:1", "ad\x1bmin", "guess")
	}
	got := buf.String()
	if n := strings.Count(got, "refused: wrong username or password"); n != 5 || !strings.Contains(got, `sign-in as "ad?min" from 203.0.113.9`) {
		t.Errorf("refusals logged %d times (want 5), or the name unescaped:\n%s", n, got)
	}
}

// A user forces the DNS checks twenty times in fifteen minutes, then sees
// the cached result (security audit 2026-10-17, M9); another user is not
// affected.
func TestDNSRechecksArePerUser(t *testing.T) {
	m := testModule(t, false)
	for i := 0; i < 20; i++ {
		if !m.AllowDNSRecheck(1) {
			t.Fatalf("re-check %d refused", i+1)
		}
	}
	if m.AllowDNSRecheck(1) {
		t.Error("the 21st re-check was allowed")
	}
	if !m.AllowDNSRecheck(2) {
		t.Error("another user's first re-check was refused")
	}
}
