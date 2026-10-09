// Package app owns application accounts (product.md § Multi-domain model): the
// SASL credentials in sasldb2, the per-application sender address mode, and
// the smtpd_sender_login_maps bindings that tie each login to the addresses it
// may send from. It keeps those three stores — the SQLite registry, sasldb2
// and the Postfix map — in agreement and drives the Postfix reload.
package app

import (
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

// SenderMaps is the slice of the Postfix manager the application service needs:
// rebuilding the sender_login_maps from the current bindings and reloading.
// *postfix.Postfix satisfies it; tests substitute a fake.
type SenderMaps interface {
	RebuildSenderLoginMaps(bindings []postfix.Binding) error
}

// Service coordinates application state across SQLite, sasldb2 and the Postfix
// sender_login_maps. Web handlers validate raw input first; the Service performs
// the domain-ownership checks that must not be skipped (security.md) and keeps
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

// List returns a domain's applications with their address lists (product.md).
func (s *Service) List(domainID int64) ([]store.Application, error) {
	return s.store.ListApplicationsByDomain(domainID)
}

// Get returns one application by id (store.ErrApplicationNotFound if absent).
func (s *Service) Get(id int64) (store.Application, error) {
	return s.store.GetApplication(id)
}

// Create adds an application to a domain: it validates the login and (in list
// mode) that every address belongs to the domain (security.md), generates a
// strong password, writes the SASL account and rebuilds the sender map (spec
// 7.2.5). The generated password is returned so the caller can show it exactly
// once (security.md) — it is never persisted in plaintext.
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

// ImportApplication re-creates an application from a domain-export file
// (architecture.md § Persistence): it validates the login and (in list mode)
// that every address belongs to the domain, inserts the registry row and
// writes the SASL account with the imported password verbatim, re-keyed under
// this instance's realm so the credential keeps working without regeneration.
// It deliberately does not rebuild the sender map — the caller (domain import)
// does that once after all applications are in — and returns
// store.ErrLoginExists if the login collides.
func (s *Service) ImportApplication(domainID int64, login, mode string, rawAddresses []string, password string) error {
	addresses, err := s.validateForDomain(domainID, login, mode, rawAddresses)
	if err != nil {
		return err
	}
	if err := validateImportedPassword(password); err != nil {
		return err
	}
	a, err := s.store.AddApplication(domainID, login, mode, addresses)
	if err != nil {
		return err // ErrLoginExists surfaces to the caller as a friendly message
	}
	if err := s.sasl.Set(login, password); err != nil {
		_, _ = s.store.DeleteApplication(a.ID)
		return err
	}
	return nil
}

// Secret returns an application's stored password for a domain export
// (architecture.md § Persistence). See SASLDB.Secret for why this is possible
// and safe.
func (s *Service) Secret(login string) (string, error) {
	return s.sasl.Secret(login)
}

// UpdateMode switches an application's address mode / list and rebuilds the
// sender map (product.md). The login and password are untouched. Addresses are
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
//
// The order matches domain deletion: the SASL account goes first, while the
// login is still in the registry. Dropping the row first would, on a
// saslpasswd2 failure, leave an account that can still authenticate to Postfix
// but that the panel no longer knows about — an orphan no operator can see or
// remove. Failing before the row is deleted is recoverable: the application is
// still listed and the delete can be retried.
func (s *Service) Delete(id int64) error {
	a, err := s.store.GetApplication(id)
	if err != nil {
		return err
	}
	if err := s.sasl.Delete(a.Login); err != nil {
		return err
	}
	if _, err := s.store.DeleteApplication(id); err != nil {
		return err
	}
	// Drop the application's level-2 limit, if any (guide § Rate limiting);
	// rate_limits has no cascade of its own.
	if err := s.store.DeleteRateLimit(store.RateLimitScopeApp, id); err != nil {
		return err
	}
	return s.Resync()
}

// RateLimit returns the application-level differentiated rate limit (guide §
// Rate limiting), and whether one is configured, for the application's edit
// form.
func (s *Service) RateLimit(appID int64) (store.RateLimit, bool, error) {
	return s.store.GetRateLimit(store.RateLimitScopeApp, appID)
}

// SaveRateLimit stores the application-level rate limit (guide § Rate
// limiting). The caller has validated the numbers (security.md); the milter
// reads the row live, so no reload is needed.
func (s *Service) SaveRateLimit(appID int64, rl store.RateLimit) error {
	rl.Scope = store.RateLimitScopeApp
	rl.RefID = appID
	return s.store.SetRateLimit(rl)
}

// ClearRateLimit removes the application-level rate limit (guide § Rate
// limiting).
func (s *Service) ClearRateLimit(appID int64) error {
	return s.store.DeleteRateLimit(store.RateLimitScopeApp, appID)
}

// UpdateAuthIPs sets client IP restriction for an application (guide § Rate
// limiting — authorization). The caller has validated the IPs (security.md).
func (s *Service) UpdateAuthIPs(appID int64, restrict bool, ips []string) error {
	return s.store.UpdateApplicationAuthIPs(appID, restrict, ips)
}

// PurgeDomainSASL removes the SASL accounts of every application bound to a
// domain. It must be called before the domain's registry rows are cascade-
// deleted, while the logins are still known (product.md). The registry rows and
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
// bindings and reloads Postfix (architecture.md § Mail path). It is the single
// idempotent apply path shared by create/edit/delete and is also reachable
// from the manual reload button; it doubles as recovery if the map ever drifts
// from the database.
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
// (security.md). It returns the cleaned address list, which is empty in wildcard
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

// Limit is an application's level-2 rate-limit choice on the application form.
// An empty Mode means "use the domain limit": the application has no limit of
// its own.
type Limit struct {
	Mode           string // "", store.RateLimitModeManual or store.RateLimitModeAuto
	MaxMessages    int
	WindowSeconds  int
	AutoMultiplier float64
}

// Settings is everything the application form holds besides the login: who the
// application may send as, which client IPs may use it, and its rate limit.
// The form is one POST, so the three are validated and saved together.
type Settings struct {
	Mode           string
	Addresses      []string // raw; list mode only
	AuthIPRestrict bool
	AuthAllowedIPs []string
	Limit          Limit
}

// Recalc derives an auto limit's ceiling from send statistics. The numbers it
// needs (retention, the level-1 backstop) are the panel's configuration, so
// the caller supplies it.
type Recalc func(appID int64) error

// CreateWithSettings adds an application with all of its settings in one step
// and returns the password to show once. If anything after the creation fails
// the application is removed again: the form either produces a complete
// application or none.
func (s *Service) CreateWithSettings(domainID int64, login string, set Settings, recalc Recalc) (store.Application, string, error) {
	a, password, err := s.Create(domainID, login, set.Mode, set.Addresses)
	if err != nil {
		return store.Application{}, "", err
	}
	if err := s.applyAccess(a.ID, set, recalc); err != nil {
		_ = s.ClearRateLimit(a.ID)
		s.rollbackCreate(a.ID, login)
		_ = s.Resync()
		return store.Application{}, "", err
	}
	a, err = s.store.GetApplication(a.ID)
	if err != nil {
		return store.Application{}, "", err
	}
	return a, password, nil
}

// SaveSettings replaces an application's settings in one step. The sender
// rules are validated before anything is written; if a later write fails, what
// was already changed is put back, so the application is left as it was rather
// than half-saved. The login and the password are untouched.
func (s *Service) SaveSettings(id int64, set Settings, recalc Recalc) error {
	before, err := s.store.GetApplication(id)
	if err != nil {
		return err
	}
	addresses, err := s.validateForDomain(before.DomainID, before.Login, set.Mode, set.Addresses)
	if err != nil {
		return err
	}
	limitBefore, hadLimit, err := s.RateLimit(id)
	if err != nil {
		return err
	}
	restore := func() {
		_ = s.store.UpdateApplicationMode(id, before.AddressMode, before.Addresses)
		_ = s.store.UpdateApplicationAuthIPs(id, before.AuthIPRestrict, before.AuthAllowedIPs)
		if hadLimit {
			_ = s.SaveRateLimit(id, limitBefore)
		} else {
			_ = s.ClearRateLimit(id)
		}
		_ = s.Resync()
	}
	if err := s.store.UpdateApplicationMode(id, set.Mode, addresses); err != nil {
		restore()
		return err
	}
	if err := s.applyAccess(id, set, recalc); err != nil {
		restore()
		return err
	}
	if err := s.Resync(); err != nil {
		restore()
		return err
	}
	return nil
}

// applyAccess writes the client-IP restriction and the rate limit.
func (s *Service) applyAccess(id int64, set Settings, recalc Recalc) error {
	if err := s.UpdateAuthIPs(id, set.AuthIPRestrict, set.AuthAllowedIPs); err != nil {
		return err
	}
	switch set.Limit.Mode {
	case "":
		return s.ClearRateLimit(id)
	case store.RateLimitModeAuto:
		rl := store.RateLimit{Mode: store.RateLimitModeAuto, AutoMultiplier: set.Limit.AutoMultiplier, WindowSeconds: set.Limit.WindowSeconds}
		if err := s.SaveRateLimit(id, rl); err != nil {
			return err
		}
		if recalc == nil {
			return nil
		}
		return recalc(id)
	default:
		return s.SaveRateLimit(id, store.RateLimit{
			Mode: store.RateLimitModeManual, MaxMessages: set.Limit.MaxMessages, WindowSeconds: set.Limit.WindowSeconds,
		})
	}
}
