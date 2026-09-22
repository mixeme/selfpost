// Package dmarc receives DMARC aggregate reports: Postfix pipes messages here,
// gzip/XML is parsed, and summaries land in SQLite for the panel.
package dmarc

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

// Maps is the Postfix manager surface the DMARC service needs.
type Maps interface {
	RebuildDMARCMaps(cfg postfix.DMARCMapsConfig) error
}

// Service coordinates DMARC ingest allow-lists across SQLite and Postfix.
type Service struct {
	store    *store.Store
	maps     Maps
	hostname string
	enabled  bool
}

// NewService builds the DMARC ingest service.
func NewService(st *store.Store, maps Maps, hostname string, enabled bool) *Service {
	return &Service{store: st, maps: maps, hostname: strings.ToLower(strings.TrimSpace(hostname)), enabled: enabled}
}

// Enabled reports whether DMARC ingest is active in this deployment.
func (s *Service) Enabled() bool { return s.enabled }

// Resync rebuilds the Postfix allow-list and transport maps from SQLite.
func (s *Service) Resync() error {
	if !s.enabled {
		return s.maps.RebuildDMARCMaps(postfix.DMARCMapsConfig{})
	}
	addrs, err := s.AllowedRecipients()
	if err != nil {
		return err
	}
	domains := recipientDomains(addrs)
	return s.maps.RebuildDMARCMaps(postfix.DMARCMapsConfig{
		Recipients: addrs,
		Domains:    domains,
	})
}

// AllowedRecipients returns every address Postfix may accept for DMARC ingest.
func (s *Service) AllowedRecipients() ([]string, error) {
	if !s.enabled || s.hostname == "" {
		return nil, nil
	}
	profile, err := s.store.GlobalDMARCReportEmail()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var out []string
	add := func(addr string) {
		addr = strings.ToLower(strings.TrimSpace(addr))
		if addr == "" || !IsHostedOnHostname(addr, s.hostname) || seen[addr] {
			return
		}
		seen[addr] = true
		out = append(out, addr)
	}
	add(profile)
	if profile == "" {
		add(DefaultHostedReportAddress(s.hostname))
	}
	domains, err := s.store.ListDomains()
	if err != nil {
		return nil, err
	}
	for _, d := range domains {
		rua := dnscheck.ResolveDMARCRua(d.DMARCRua, profile)
		if rua == "" {
			continue
		}
		if IsHostedOnHostname(rua, s.hostname) {
			add(rua)
			continue
		}
		if d.DMARCRua.Valid && d.DMARCRua.String == "" {
			continue
		}
		hosted := HostedReportAddress(s.hostname, d.Name)
		if strings.EqualFold(rua, hosted) {
			add(rua)
		}
	}
	sort.Strings(out)
	return out, nil
}

func recipientDomains(addrs []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, addr := range addrs {
		d := dnscheck.EmailDomain(addr)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// HostedSuggestion returns the address the panel should suggest for a domain.
func (s *Service) HostedSuggestion(domain string) string {
	return HostedReportAddress(s.hostname, domain)
}

// DefaultHostedSuggestion is the settings-level hosted address.
func (s *Service) DefaultHostedSuggestion() string {
	return DefaultHostedReportAddress(s.hostname)
}

// ValidateHostedAddress ensures addr is on this hostname before saving.
func (s *Service) ValidateHostedAddress(addr string) error {
	if !IsHostedOnHostname(addr, s.hostname) {
		return fmt.Errorf("hosted report address must be on %s", s.hostname)
	}
	return nil
}
