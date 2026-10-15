package main

import (
	"testing"
	"time"
)

// The panel's server bounds every phase a client could hold it in (audit
// 2026-10-12, L14); before, only the header's read was.
func TestHTTPServerHasItsTimeouts(t *testing.T) {
	s := newHTTPServer(":0", nil)
	for name, d := range map[string]time.Duration{
		"ReadHeaderTimeout": s.ReadHeaderTimeout,
		"ReadTimeout":       s.ReadTimeout,
		"WriteTimeout":      s.WriteTimeout,
		"IdleTimeout":       s.IdleTimeout,
	} {
		if d <= 0 {
			t.Errorf("%s is %v", name, d)
		}
	}
	if s.ReadTimeout < s.ReadHeaderTimeout || s.WriteTimeout < 30*time.Second {
		t.Errorf("read %v (header %v), write %v: too short for a form or a DNS page", s.ReadTimeout, s.ReadHeaderTimeout, s.WriteTimeout)
	}
}
