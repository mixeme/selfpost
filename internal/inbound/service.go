// Package inbound owns backup-MX / relay-forwarder domains: the SQLite
// registry and the Postfix lookup tables (relay_domains, transport_maps,
// relay_recipient_maps, smtp_tls_policy_maps). It does not listen on port 25
// itself — postfix-config.sh does that when INBOUND_RELAY_ENABLE is true.
package inbound

import (
	"fmt"
	"strings"

	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

// Maps is the slice of the Postfix manager the inbound service needs.
type Maps interface {
	RebuildInboundMaps(routes []postfix.InboundRoute) error
}

// Service coordinates inbound-domain state across SQLite and the Postfix maps.
type Service struct {
	store *store.Store
	maps  Maps
}

// NewService builds the inbound service over the shared store and Postfix maps.
func NewService(st *store.Store, maps Maps) *Service {
	return &Service{store: st, maps: maps}
}

// List returns every inbound domain.
func (s *Service) List() ([]store.InboundDomain, error) {
	return s.store.ListInboundDomains()
}

// Get returns one inbound domain by id.
func (s *Service) Get(id int64) (store.InboundDomain, error) {
	return s.store.GetInboundDomain(id)
}

// Add validates the domain name, inserts it, and rebuilds the maps.
func (s *Service) Add(name string) (store.InboundDomain, error) {
	name = normalizeDomain(name)
	if err := checkDomain(name); err != nil {
		return store.InboundDomain{}, err
	}
	d, err := s.store.AddInboundDomain(name)
	if err != nil {
		return store.InboundDomain{}, err
	}
	if err := s.Resync(); err != nil {
		_ = s.store.DeleteInboundDomain(d.ID)
		return store.InboundDomain{}, err
	}
	return d, nil
}

// SetTransport validates and saves the upstream, then rebuilds the maps.
func (s *Service) SetTransport(id int64, host, portRaw, tlsMode string) error {
	if _, err := s.store.GetInboundDomain(id); err != nil {
		return err
	}
	host = normalizeHost(host)
	if err := checkHost(host); err != nil {
		return err
	}
	port, err := parsePort(portRaw)
	if err != nil {
		return err
	}
	if err := checkTLSMode(tlsMode); err != nil {
		return err
	}
	if err := s.store.UpdateInboundTransport(id, host, port, tlsMode); err != nil {
		return err
	}
	return s.Resync()
}

// SetRecipients validates the mode and, in list mode, every address, then
// rebuilds the maps.
func (s *Service) SetRecipients(id int64, mode string, rawAddresses []string) error {
	d, err := s.store.GetInboundDomain(id)
	if err != nil {
		return err
	}
	if err := checkRecipientMode(mode); err != nil {
		return err
	}
	var addrs []string
	if mode == store.RecipientModeList {
		addrs, err = parseRecipientAddresses(rawAddresses, d.Name)
		if err != nil {
			return err
		}
	}
	if err := s.store.UpdateInboundRecipients(id, mode, addrs); err != nil {
		return err
	}
	return s.Resync()
}

// Delete removes the domain and rebuilds the maps.
func (s *Service) Delete(id int64) error {
	if err := s.store.DeleteInboundDomain(id); err != nil {
		return err
	}
	return s.Resync()
}

// Resync rebuilds the inbound Postfix maps from SQLite.
func (s *Service) Resync() error {
	list, err := s.store.ListInboundDomains()
	if err != nil {
		return err
	}
	routes := make([]postfix.InboundRoute, 0, len(list))
	for _, d := range list {
		full, err := s.store.GetInboundDomain(d.ID)
		if err != nil {
			return err
		}
		routes = append(routes, postfix.InboundRoute{
			Domain:        full.Name,
			Host:          full.Host,
			Port:          full.Port,
			TLSMode:       full.TLSMode,
			RecipientMode: full.RecipientMode,
			Recipients:    full.Recipients,
		})
	}
	return s.maps.RebuildInboundMaps(routes)
}

func parseRecipientAddresses(raw []string, domain string) ([]string, error) {
	seen := make(map[string]bool)
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		addr := strings.ToLower(strings.TrimSpace(r))
		if addr == "" {
			continue
		}
		if err := checkMailbox(addr, domain); err != nil {
			return nil, err
		}
		if seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, addr)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("listed-recipients mode requires at least one address")
	}
	return out, nil
}
