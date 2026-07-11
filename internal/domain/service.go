package domain

import (
	"fmt"

	"codeberg.org/mix/selfpost/internal/store"
)

// Service coordinates the three places a sending domain lives: the SQLite
// registry, the on-disk DKIM keys and OpenDKIM's tables. Callers (the web
// handlers) validate user input first; Service keeps the three stores in
// agreement and drives the OpenDKIM reload (spec 6, 7.2.2-4, 7.2.10).
type Service struct {
	store    *store.Store
	odk      *OpenDKIM
	selector string
}

// NewService builds the domain service. selectorDefault is the DKIM selector
// assigned to new domains (spec 8: DKIM_SELECTOR_DEFAULT); it is
// operator-configured, not user input.
func NewService(st *store.Store, odk *OpenDKIM, selectorDefault string) *Service {
	return &Service{store: st, odk: odk, selector: selectorDefault}
}

// List returns all domains with application counts (spec 7.2.2).
func (s *Service) List() ([]store.Domain, error) {
	return s.store.ListDomains()
}

// Get returns one domain by id (store.ErrDomainNotFound if absent).
func (s *Service) Get(id int64) (store.Domain, error) {
	return s.store.GetDomain(id)
}

// Add registers a new sending domain: it records the row, ensures a DKIM key
// exists on disk, and regenerates + reloads the OpenDKIM tables (spec 7.2.3).
// name must already be normalised and validated by the caller. A duplicate
// returns store.ErrDomainExists.
//
// The registry row is written first so its UNIQUE constraint is the single
// arbiter of "already exists" (avoiding a check-then-act race). An existing
// on-disk key is reused rather than overwritten, so re-adding a domain whose DB
// row was lost keeps its published DNS record valid. If key generation or the
// OpenDKIM rebuild fails, the row is rolled back so we never leave a registered
// domain that OpenDKIM cannot sign.
func (s *Service) Add(name string) (store.Domain, error) {
	d, err := s.store.AddDomain(name, s.selector)
	if err != nil {
		return store.Domain{}, err
	}

	if _, err := s.odk.EnsureKey(d.Name, d.DKIMSelector); err != nil {
		s.rollbackAdd(d.ID)
		return store.Domain{}, err
	}
	if err := s.resync(); err != nil {
		s.rollbackAdd(d.ID)
		return store.Domain{}, err
	}
	return d, nil
}

// rollbackAdd best-effort removes a half-created domain after a downstream
// failure. Errors here are logged by the caller's returned error path; the key
// (if freshly generated) is left in place harmlessly and reused on retry.
func (s *Service) rollbackAdd(id int64) {
	_ = s.store.DeleteDomain(id)
}

// Delete removes a domain and everything bound to it — applications and their
// SASL/binding rows go via the DB cascade, and the DKIM key and table entries
// are removed here (spec 7.2.4, 6.5). The registry row and tables are updated
// (so OpenDKIM stops signing for the domain) before the key is deleted.
func (s *Service) Delete(id int64) error {
	d, err := s.store.GetDomain(id)
	if err != nil {
		return err
	}
	if err := s.store.DeleteDomain(id); err != nil {
		return err
	}
	if err := s.resync(); err != nil {
		return err
	}
	if err := s.odk.RemoveKey(d.Name); err != nil {
		// The domain is gone from the registry and tables; a leftover key
		// directory is harmless. Surface it so it is not silently ignored.
		return fmt.Errorf("domain deleted but key cleanup failed: %w", err)
	}
	return nil
}

// DKIMRecord returns the DNS TXT record to publish for a domain (spec 7.2.10).
func (s *Service) DKIMRecord(d store.Domain) (DKIMRecord, error) {
	return s.odk.Record(d.Name, d.DKIMSelector)
}

// Resync regenerates the OpenDKIM tables from the registry and reloads OpenDKIM.
// It backs the manual reload button (spec 7.2.12) and doubles as a recovery path
// if the tables ever drift from the database.
func (s *Service) Resync() error {
	return s.resync()
}

// resync rebuilds KeyTable/SigningTable from the current domain set and reloads.
func (s *Service) resync() error {
	domains, err := s.store.ListDomains()
	if err != nil {
		return err
	}
	signing := make([]SigningDomain, 0, len(domains))
	for _, d := range domains {
		signing = append(signing, SigningDomain{Name: d.Name, Selector: d.DKIMSelector})
	}
	return s.odk.Rebuild(signing)
}
