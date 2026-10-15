package inbound

import (
	"errors"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

type nullMaps struct{}

func (nullMaps) RebuildInboundMaps([]postfix.InboundRoute) error { return nil }

// An upstream has one TLS mode, since Postfix keys the policy by the next
// hop: a second domain saved to the same [host]:port with another mode is
// refused with the first domain's name and mode; the same mode is fine
// (audit 2026-10-12, L5).
func TestOneTLSModePerUpstream(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := NewService(st, nullMaps{})
	a, err := s.Add("a.example")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Add("b.example")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTransport(a.ID, "10.0.0.8", "25", "encrypt"); err != nil {
		t.Fatal(err)
	}
	err = s.SetTransport(b.ID, "[10.0.0.8]", "25", "may")
	var verr *ValidationError
	if !errors.As(err, &verr) || !strings.Contains(err.Error(), "[10.0.0.8]:25 is already the upstream of a.example, with TLS mode encrypt") {
		t.Fatalf("a second mode for one upstream = %v", err)
	}
	if err := s.SetTransport(b.ID, "10.0.0.8", "25", "encrypt"); err != nil {
		t.Errorf("the same mode refused: %v", err)
	}
	if err := s.SetTransport(b.ID, "10.0.0.8", "2525", "may"); err != nil {
		t.Errorf("another port refused: %v", err)
	}
	// The domain itself may change its own mode.
	if err := s.SetTransport(a.ID, "10.0.0.8", "25", "may"); err != nil {
		t.Errorf("a domain changing its own mode refused: %v", err)
	}
}
