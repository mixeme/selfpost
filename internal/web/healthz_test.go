package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// /healthz answers a probe that did not come through a proxy, from loopback
// or a private address, and is not found for anyone else (security audit
// 2026-10-17, I6). Liveness itself is not under test here: a probe that is
// answered gets 200 or 503, never 404.
func TestHealthzAnswersOnlyLocalProbes(t *testing.T) {
	for _, c := range []struct {
		name, remote, xff string
		answered          bool
	}{
		{"the container's own probe", "127.0.0.1:40000", "", true},
		{"the host through the published port", "172.18.0.1:40000", "", true},
		{"through the reverse proxy", "172.18.0.1:40000", "198.51.100.7", false},
		{"a public address", "198.51.100.7:40000", "", false},
	} {
		r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		rec := httptest.NewRecorder()
		handleHealth(rec, r)
		if got := rec.Code != http.StatusNotFound; got != c.answered {
			t.Errorf("%s: %d, answered %v, want %v", c.name, rec.Code, got, c.answered)
		}
	}
}
