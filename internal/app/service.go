// Package app owns application accounts (spec 4.1, 5.1): the SASL credentials in
// sasldb2, the per-application sender address mode, and the
// smtpd_sender_login_maps bindings that tie each login to the addresses it may
// send from. It keeps those three stores — the SQLite registry, sasldb2 and the
// Postfix map — in agreement and drives the Postfix reload.
package app

import (
	"codeberg.org/mix/selfpost/internal/postfix"
	"codeberg.org/mix/selfpost/internal/store"
)

// SenderMaps is the slice of the Postfix manager the application service needs:
// rebuilding the sender_login_maps from the current bindings and reloading.
// *postfix.Postfix satisfies it; tests substitute a fake.
type SenderMaps interface {
	RebuildSenderLoginMaps(bindings []postfix.Binding) error
}

// Service coordinates application state across SQLite, sasldb2 and the Postfix
// sender_login_maps. Web handlers validate raw input first; the Service performs
// the domain-ownership checks that must not be skipped (spec 7.6.2) and keeps
// the stores consistent.
type Service struct {
	store *store.Store
	sasl  *SASLDB
	pf    SenderMaps
}

// NewService builds the application service over the shared store, the sasldb2
// manager and the Postfix manager.
func NewService(st *store.Store, sasl *SASLDB, pf SenderMaps) *Service {
	return &Service{store: st, sasl: sasl, pf: pf}
}

// List returns a domain's applications with their address lists (spec 7.2.6).
func (s *Service) List(domainID int64) ([]store.Application, error) {
	return s.store.ListApplicationsByDomain(domainID)
}

// Get returns one application by id (store.ErrApplicationNotFound if absent).
func (s *Service) Get(id int64) (store.Application, error) {
	return s.store.GetApplication(id)
}

// Create adds an application to a domain: it validates the login and (in list
// mode) that every address belongs to the domain (spec 7.6.2), generates a
// strong password, writes the SASL account and rebuilds the sender map (spec
// 7.2.5). The generated password is returned so the caller can show it exactly
// once (spec 7.6.1) — it is never persisted in plaintext.
//
// The registry row is written first so its UNIQUE constraint is the sole arbiter
// of a duplicate login (avoiding a check-then-act race and, crucially, avoiding
// clobbering an existing account's password in sasldb2). If the SASL write or
// the map rebuild fails, everything is rolled back so we never leave an
// application the panel cannot fully account for.
func (s *Service) Create(domainID int64, login, mode string, rawAddresses []string) (store.Application, string, error) {
	addresses, err := s.validateForDomain(domainID, login, mode, rawAddresses)
	if err != nil {
		return store.Application{}, "", err
	}

	password, err := generatePassword()
	if err != nil {
		return store.Application{}, "", err
	}

	a, err := s.store.AddApplication(domainID, login, mode, addresses)
	if err != nil {
		return store.Application{}, "", err
	}

	if err := s.sasl.Set(login, password); err != nil {
		s.rollbackCreate(a.ID, "") // login has no SASL account yet; nothing to unset
		return store.Application{}, "", err
	}
	if err := s.Resync(); err != nil {
		s.rollbackCreate(a.ID, login)
		return store.Application{}, "", err
	}
	return a, password, nil
}

// rollbackCreate best-effort undoes a partially created application after a
// downstream failure: it removes the SASL account (if one was written) and the
// registry row. Errors here are subordinate to the original failure the caller
// returns.
func (s *Service) rollbackCreate(id int64, login string) {
	if login != "" {
		_ = s.sasl.Delete(login)
	}
	_, _ = s.store.DeleteApplication(id)
}

// UpdateMode switches an application's address mode / list and rebuilds the
// sender map (spec 7.2.7). The login and password are untouched. Addresses are
// re-validated against the application's domain.
func (s *Service) UpdateMode(id int64, mode string, rawAddresses []string) error {
	a, err := s.store.GetApplication(id)
	if err != nil {
		return err
	}
	addresses, err := s.validateForDomain(a.DomainID, a.Login, mode, rawAddresses)
	if err != nil {
		return err
	}
	if err := s.store.UpdateApplicationMode(id, mode, addresses); err != nil {
		return err
	}
	return s.Resync()
}

// RegeneratePassword issues a fresh password for an existing application (spec
// 7.2.9). The old password is invalidated by overwriting the SASL account; the
// address mode and bindings are unchanged, so no map rebuild is needed. The new
// password is returned to be shown once.
func (s *Service) RegeneratePassword(id int64) (string, error) {
	a, err := s.store.GetApplication(id)
	if err != nil {
		return "", err
	}
	password, err := generatePassword()
	if err != nil {
		return "", err
	}
	if err := s.sasl.Set(a.Login, password); err != nil {
		return "", err
	}
	return password, nil
}

// Delete removes an application: its SASL account, its registry row (and address
// rows via cascade) and its sender-map bindings, then reloads Postfix (spec
// 7.2.8). The domain and other applications are untouched.
func (s *Service) Delete(id int64) error {
	a, err := s.store.DeleteApplication(id)
	if err != nil {
		return err
	}
	if err := s.sasl.Delete(a.Login); err != nil {
		return err
	}
	// Drop the application's level-2 limit, if any (spec 7.4); rate_limits has no
	// cascade of its own.
	if err := s.store.DeleteRateLimit(store.RateLimitScopeApp, id); err != nil {
		return err
	}
	return s.Resync()
}

// RateLimit returns the application-level differentiated rate limit (spec 7.4),
// and whether one is configured, for the application's edit form.
func (s *Service) RateLimit(appID int64) (store.RateLimit, bool, error) {
	return s.store.GetRateLimit(store.RateLimitScopeApp, appID)
}

// SaveRateLimit stores the application-level rate limit. The caller has validated
// the IPs and numbers (spec 7.6.2); the milter reads the row live, so no reload
// is needed.
func (s *Service) SaveRateLimit(appID int64, ips []string, maxMessages, windowSeconds int) error {
	return s.store.SetRateLimit(store.RateLimit{
		Scope:         store.RateLimitScopeApp,
		RefID:         appID,
		AllowedIPs:    ips,
		MaxMessages:   maxMessages,
		WindowSeconds: windowSeconds,
	})
}

// ClearRateLimit removes the application-level rate limit (spec 7.4).
func (s *Service) ClearRateLimit(appID int64) error {
	return s.store.DeleteRateLimit(store.RateLimitScopeApp, appID)
}

// PurgeDomainSASL removes the SASL accounts of every application bound to a
// domain. It must be called before the domain's registry rows are cascade-
// deleted, while the logins are still known (spec 7.2.4). The registry rows and
// the sender map are handled by the domain deletion path; this only clears
// sasldb2, which has no cascade of its own.
func (s *Service) PurgeDomainSASL(domainID int64) error {
	logins, err := s.store.ListLoginsByDomain(domainID)
	if err != nil {
		return err
	}
	for _, login := range logins {
		if err := s.sasl.Delete(login); err != nil {
			return err
		}
	}
	return nil
}

// Resync rebuilds smtpd_sender_login_maps from the full set of application
// bindings and reloads Postfix (spec 5.1). It is the single idempotent apply
// path shared by create/edit/delete and is also reachable from the manual
// reload button; it doubles as recovery if the map ever drifts from the
// database.
func (s *Service) Resync() error {
	bindings, err := s.store.ListBindings()
	if err != nil {
		return err
	}
	pfBindings := make([]postfix.Binding, 0, len(bindings))
	for _, b := range bindings {
		pfBindings = append(pfBindings, postfix.Binding{Address: b.Address, Login: b.Login})
	}
	return s.pf.RebuildSenderLoginMaps(pfBindings)
}

// validateForDomain resolves the domain, validates the login and address mode,
// and — in list mode — validates that every address belongs to the domain
// (spec 7.6.2). It returns the cleaned address list, which is empty in wildcard
// mode. Resolving the domain here also confirms it exists before any write.
func (s *Service) validateForDomain(domainID int64, login, mode string, rawAddresses []string) ([]string, error) {
	d, err := s.store.GetDomain(domainID)
	if err != nil {
		return nil, err
	}
	if err := validateLogin(login); err != nil {
		return nil, err
	}
	if err := validateAddressMode(mode); err != nil {
		return nil, err
	}
	if mode == store.AddressModeWildcard {
		return nil, nil
	}
	return parseAddresses(rawAddresses, d.Name)
}
