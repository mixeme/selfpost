package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// serveSecured runs one request through the security middleware and reports
// what came out. The wrapped handler answers 200, so any other status is the
// middleware's doing.
func serveSecured(cookieSecure bool, r *http.Request) *httptest.ResponseRecorder {
	s := &Server{cfg: Config{CookieSecure: cookieSecure}}
	h := s.secure(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// post builds a request as a browser would send it to the panel's own host.
func post(secFetchSite, origin string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://panel.example.com/domains", nil)
	r.Host = "panel.example.com"
	if secFetchSite != "" {
		r.Header.Set("Sec-Fetch-Site", secFetchSite)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

// The full matrix the origin check has to get right. The row that
// matters most is "same-site": a neighbouring host on example.com is same-site
// as far as the session cookie's SameSite=Lax is concerned, so this check is
// the only thing standing between it and a forged POST.
func TestOriginCheck(t *testing.T) {
	tests := []struct {
		name         string
		req          *http.Request
		wantRejected bool
	}{
		{"no headers at all is let through (accepted risk)", post("", ""), false},
		{"same-origin", post("same-origin", "https://panel.example.com"), false},
		{"same-site, i.e. a neighbouring subdomain", post("same-site", ""), true},
		{"cross-site", post("cross-site", "https://evil.example.net"), true},
		{"none, a navigation with no initiator", post("none", ""), true},
		{"Sec-Fetch-Site outranks a matching Origin", post("cross-site", "https://panel.example.com"), true},
		{"Origin host matches", post("", "https://panel.example.com"), false},
		{"Origin host differs", post("", "https://evil.example.net"), true},
		{"Origin differs from a neighbour only in host", post("", "https://cms.example.com"), true},
		{"opaque null Origin", post("", "null"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveSecured(true, tc.req)
			rejected := rec.Code == http.StatusForbidden
			if rejected != tc.wantRejected {
				t.Fatalf("status = %d, rejected = %t, want rejected = %t", rec.Code, rejected, tc.wantRejected)
			}
		})
	}
}

// The proxy terminates TLS and forwards plain HTTP, so the panel never learns
// its own external scheme: comparing it against the https in Origin would
// reject every legitimate form submission in the documented deployment.
func TestOriginCheckIgnoresScheme(t *testing.T) {
	rec := serveSecured(true, post("", "https://panel.example.com"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: an https Origin against a plain-HTTP panel must pass", rec.Code)
	}
}

// Every GET route in the panel is a read, so cross-origin reads have nothing
// to change and must not be blocked — the HTMX polling fragments included.
func TestOriginCheckExemptsReads(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		r := httptest.NewRequest(method, "http://panel.example.com/status/fragment", nil)
		r.Host = "panel.example.com"
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		if rec := serveSecured(true, r); rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", method, rec.Code)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := serveSecured(true, httptest.NewRequest(http.MethodGet, "http://panel.example.com/status", nil))
	want := map[string]string{
		"Content-Security-Policy":   contentSecurityPolicy,
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "no-referrer",
		"Strict-Transport-Security": strictTransportSecurity,
	}
	for name, value := range want {
		if got := rec.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
}

// HSTS is the one header that must not be sent unconditionally: on the
// plain-HTTP development instance it would pin the browser to a scheme that
// instance does not speak.
func TestHSTSOnlyWhenSecure(t *testing.T) {
	rec := serveSecured(false, httptest.NewRequest(http.MethodGet, "http://panel.example.com/status", nil))
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q on a non-secure deployment, want none", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got == "" {
		t.Error("the other security headers must still be sent without TLS")
	}
}

// A rejected request is still a response the browser renders, so it carries
// the same headers as any other.
func TestRejectedRequestKeepsSecurityHeaders(t *testing.T) {
	rec := serveSecured(true, post("cross-site", ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Errorf("Content-Security-Policy = %q on the 403 response, want the policy", got)
	}
}
