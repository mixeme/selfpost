package inbound

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

type fakeMaps struct {
	routes  []postfix.InboundRoute
	err     error
	rebuild int
}

func (f *fakeMaps) RebuildInboundMaps(routes []postfix.InboundRoute) error {
	f.rebuild++
	f.routes = append([]postfix.InboundRoute(nil), routes...)
	return f.err
}

func testService(t *testing.T) (*Service, *store.Store, *fakeMaps) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m := &fakeMaps{}
	return NewService(st, m), st, m
}

func TestAddAndSetTransport(t *testing.T) {
	s, _, m := testService(t)
	d, err := s.Add("Lists.Example.COM")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "lists.example.com" {
		t.Fatalf("name = %q", d.Name)
	}
	if m.rebuild != 1 {
		t.Fatalf("rebuild after add = %d", m.rebuild)
	}
	// Empty host is omitted from maps.
	if len(m.routes) != 1 || m.routes[0].Host != "" {
		t.Fatalf("routes after add: %+v", m.routes)
	}

	if err := s.SetTransport(d.ID, "10.0.0.8", "25", store.TLSModeEncrypt); err != nil {
		t.Fatal(err)
	}
	if m.routes[0].Host != "10.0.0.8" || m.routes[0].TLSMode != store.TLSModeEncrypt {
		t.Fatalf("routes after transport: %+v", m.routes[0])
	}
}

func TestSetRecipientsValidatesDomain(t *testing.T) {
	s, _, _ := testService(t)
	d, err := s.Add("lists.example.com")
	if err != nil {
		t.Fatal(err)
	}
	err = s.SetRecipients(d.ID, store.RecipientModeList, []string{"staff@other.com"})
	if err == nil {
		t.Fatal("expected foreign-domain error")
	}
	err = s.SetRecipients(d.ID, store.RecipientModeList, nil)
	if err == nil {
		t.Fatal("expected empty-list error")
	}
	if err := s.SetRecipients(d.ID, store.RecipientModeList, []string{"staff@lists.example.com"}); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsUnsafeHost(t *testing.T) {
	s, _, _ := testService(t)
	d, err := s.Add("lists.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTransport(d.ID, "10.0.0.8; rm", "25", store.TLSModeMay); err == nil {
		t.Fatal("expected unsafe host to be rejected")
	}
}

func TestDeleteResyncs(t *testing.T) {
	s, _, m := testService(t)
	d, err := s.Add("lists.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(d.ID); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(mustGet(t, s, d.ID), store.ErrInboundDomainNotFound) {
		t.Fatal("domain still present")
	}
	if len(m.routes) != 0 {
		t.Fatalf("maps after delete: %+v", m.routes)
	}
}

func mustGet(t *testing.T, s *Service, id int64) error {
	t.Helper()
	_, err := s.Get(id)
	return err
}
