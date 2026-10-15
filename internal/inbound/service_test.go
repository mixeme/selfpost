package inbound

import (
	"errors"
	"testing"

	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

type nullMaps struct{}

func (nullMaps) RebuildInboundMaps([]postfix.InboundRoute) error { return nil }

// An upstream has one TLS mode, since Postfix keys the policy by the next
// hop (audit 2026-10-12, L5): the other domains forwarded to it take the mode
// saved for one of them, when the one saving may change them; one they may
// not is refused, and nothing is saved. Two domains on one upstream can
// change its mode (review of stage 4: they could not, each refused for the
// other).
func TestOneTLSModePerUpstream(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := NewService(st, nullMaps{})
	a, _ := s.Add("a.example")
	b, _ := s.Add("b.example")
	c, _ := s.Add("c.example")
	mode := func(id int64) string {
		d, err := st.GetInboundDomain(id)
		if err != nil {
			t.Fatal(err)
		}
		return d.TLSMode
	}
	all := func(store.InboundDomain) bool { return true }

	if _, err := s.SetTransportAs(a.ID, "10.0.0.8", "25", "encrypt", all); err != nil {
		t.Fatal(err)
	}
	if n, err := s.SetTransportAs(b.ID, "[10.0.0.8]", "25", "encrypt", all); err != nil || n != 0 {
		t.Fatalf("the same mode: %d, %v", n, err)
	}
	// Either domain changes the mode, for both.
	if n, err := s.SetTransportAs(a.ID, "10.0.0.8", "25", "may", all); err != nil || n != 1 {
		t.Fatalf("a changes the shared mode: %d, %v", n, err)
	}
	if mode(a.ID) != "may" || mode(b.ID) != "may" {
		t.Errorf("modes after a's change: %s, %s", mode(a.ID), mode(b.ID))
	}
	// Another port is another next hop.
	if n, err := s.SetTransportAs(c.ID, "10.0.0.8", "2525", "none", all); err != nil || n != 0 {
		t.Errorf("another port: %d, %v", n, err)
	}

	// One that may not change b is refused, with b's mode, and saves nothing.
	notB := func(d store.InboundDomain) bool { return d.ID != b.ID }
	_, err = s.SetTransportAs(c.ID, "10.0.0.8", "25", "encrypt", notB)
	var shared *UpstreamShared
	if !errors.As(err, &shared) || shared.Mode != "may" || shared.Domain != "b.example" || shared.Upstream != "[10.0.0.8]:25" {
		t.Fatalf("a mode for another's upstream = %v", err)
	}
	if d, _ := st.GetInboundDomain(c.ID); d.Port != 2525 || mode(a.ID) != "may" {
		t.Errorf("the refused save changed something: c on %d, a %s", d.Port, mode(a.ID))
	}
}
