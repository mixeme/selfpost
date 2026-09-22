package domain

import (
	"fmt"

	"codeberg.org/mix/selfpost/internal/store"
)

// Applications is the slice of the application service the domain service needs
// to keep the SASL database and sender map consistent when a domain (and its
// applications, via cascade) is deleted. *app.Service satisfies it; it is an
// interface here to avoid a package import cycle and to keep domain deletion
// testable in isolation.
type Applications interface {
	// PurgeDomainSASL removes the SASL accounts of the domain's applications.
	// It must run before the registry cascade so the logins are still known.
	PurgeDomainSASL(domainID int64) error
	// Resync rebuilds smtpd_sender_login_maps from the remaining applications
	// and reloads Postfix.
	Resync() error
	// Secret returns an application's stored password, for a domain export
	// (spec 7.5.B).
	Secret(login string) (string, error)
	// ImportApplication re-creates an application (registry row + SASL account)
	// from a domain-export file, without rebuilding the sender map (spec 7.5.B).
	ImportApplication(domainID int64, login, mode string, addresses []string, password string) error
}

// Service coordinates the places a sending domain lives: the SQLite registry,
// the on-disk DKIM keys and OpenDKIM's tables, plus — on deletion — the SASL
// database and Postfix sender map its applications touch. Callers (the web
// handlers) validate user input first; Service keeps the stores in agreement and
// drives the OpenDKIM/Postfix reloads (spec 6, 7.2.2-4, 7.2.10).
type Service struct {
	store    *store.Store
	odk      *OpenDKIM
	apps     Applications
	selector string
}

// NewService builds the domain service. selectorDefault is the DKIM selector
// assigned to new domains (spec 8: DKIM_SELECTOR_DEFAULT); it is
// operator-configured, not user input. apps is used only on deletion, to clear
// the SASL accounts and sender-map bindings of the domain's applications.
func NewService(st *store.Store, odk *OpenDKIM, apps Applications, selectorDefault string) *Service {
	return &Service{store: st, odk: odk, apps: apps, selector: selectorDefault}
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

// Delete removes a domain and everything bound to it (spec 7.2.4, 6.5). The
// order matters: the applications' SASL accounts are cleared first, while their
// logins are still in the registry; then the registry rows (applications and
// their addresses) go via the DB cascade; then the OpenDKIM tables and the
// Postfix sender map are rebuilt from what remains — so OpenDKIM stops signing
// and Postfix stops authorising the domain's senders — before the DKIM key is
// deleted.
func (s *Service) Delete(id int64) error {
	d, err := s.store.GetDomain(id)
	if err != nil {
		return err
	}
	if err := s.apps.PurgeDomainSASL(id); err != nil {
		return fmt.Errorf("clear SASL accounts for %s: %w", d.Name, err)
	}
	// Drop the domain's own level-2 limit and those of its applications while the
	// application rows still exist (the cleanup query joins them). rate_limits has
	// no cascade of its own (ref_id is a plain integer, spec 7.4/9).
	if err := s.store.DeleteRateLimitsForDomain(id); err != nil {
		return fmt.Errorf("clear rate limits for %s: %w", d.Name, err)
	}
	if err := s.store.DeleteDomain(id); err != nil {
		return err
	}
	if err := s.resync(); err != nil {
		return err
	}
	if err := s.apps.Resync(); err != nil {
		return fmt.Errorf("rebuild sender map after deleting %s: %w", d.Name, err)
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

// RateLimit returns the domain-level differentiated rate limit (spec 7.4), and
// whether one is configured, for the domain's edit form.
func (s *Service) RateLimit(domainID int64) (store.RateLimit, bool, error) {
	return s.store.GetRateLimit(store.RateLimitScopeDomain, domainID)
}

// SaveRateLimit stores the domain-level rate limit. The caller has validated the
// IPs and numbers (spec 7.6.2); the milter reads the row live, so no reload is
// needed.
func (s *Service) SaveRateLimit(domainID int64, ips []string, maxMessages, windowSeconds int) error {
	return s.store.SetRateLimit(store.RateLimit{
		Scope:         store.RateLimitScopeDomain,
		RefID:         domainID,
		AllowedIPs:    ips,
		MaxMessages:   maxMessages,
		WindowSeconds: windowSeconds,
	})
}

// ClearRateLimit removes the domain-level rate limit, falling back to level 1
// only (spec 7.4).
func (s *Service) ClearRateLimit(domainID int64) error {
	return s.store.DeleteRateLimit(store.RateLimitScopeDomain, domainID)
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
