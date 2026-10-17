// Package dmarc receives DMARC aggregate reports: Postfix pipes messages here,
// gzip/XML is parsed, and summaries land in SQLite for the panel.
package dmarc

import (
	"sort"
	"strings"
	"sync"

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

	// resyncMu makes a rebuild's read and write one step, as app.Service's
	// (security audit 2026-10-17, W3; the review of stage 1).
	resyncMu sync.Mutex
}

// NewService builds the DMARC ingest service.
func NewService(st *store.Store, maps Maps, hostname string, enabled bool) *Service {
	return &Service{store: st, maps: maps, hostname: strings.ToLower(strings.TrimSpace(hostname)), enabled: enabled}
}

// Enabled reports whether DMARC ingest is active in this deployment.
func (s *Service) Enabled() bool { return s.enabled }

// Resync rebuilds the Postfix allow-list and transport maps from SQLite.
func (s *Service) Resync() error {
	s.resyncMu.Lock()
	defer s.resyncMu.Unlock()
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
	// Only what a domain has chosen is open, and only the one mailbox that is
	// its own: the hosted address tagged with its name. A stored address on
	// this host that is anything else - typed before 2.14.4, when the form
	// still took one - opens nothing: it was an untagged mailbox, or another
	// domain's, open to the internet (security audit 2026-10-17, W1). There
	// is no mailbox for the server as a whole.
	domains, err := s.store.ListDomains()
	if err != nil {
		return nil, err
	}
	for _, d := range domains {
		if hosted := HostedReportAddress(s.hostname, d.Name); strings.EqualFold(strings.TrimSpace(d.DMARCRua), hosted) {
			add(hosted)
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
