package domain

import (
	"errors"
	"fmt"

	"github.com/mixeme/selfpost/internal/store"
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
	// (architecture.md § Persistence).
	Secret(login string) (string, error)
	// ImportApplication re-creates an application (registry row + SASL account)
	// from a domain-export file, without rebuilding the sender map
	// (architecture.md § Persistence).
	ImportApplication(domainID int64, login, mode string, addresses []string, password string) error
}

// Service coordinates the places a sending domain lives: the SQLite registry,
// the on-disk DKIM keys and the selector table the signer reads, plus — on
// deletion — the SASL database and Postfix sender map its applications touch.
// Callers (the web handlers) validate user input first; Service keeps the
// stores in agreement and drives the Postfix reloads (architecture.md § DKIM
// signing, product.md).
type Service struct {
	store    *store.Store
	keys     *Keys
	apps     Applications
	selector string
	// serverName is the server's own hostname, which the panel's notifications
	// are sent from and signed as (SetServerName). Empty: nothing is signed
	// for it.
	serverName string
}

// SetServerName gives the service the server's own hostname. The panel sends
// its notifications from postmaster@ that name, and for a receiver to believe
// them the name is signed like a sending domain: it gets a DKIM key and a line
// in the selector table, without being a row of the registry - no application
// can send as it. The name comes from SELFPOST_HOSTNAME, which the entrypoint
// has validated; it is not user input.
func (s *Service) SetServerName(hostname string) {
	s.serverName = hostname
}

// ServerMail is how the server's own name is signed.
type ServerMail struct {
	Name     string
	Selector string
	Record   DKIMRecord
}

// ServerMail returns the name notifications are sent as, its DKIM selector and
// the record to publish for it, creating the key the first time. When the
// hostname is itself a sending domain, it is that domain's key and selector:
// one name has one key.
func (s *Service) ServerMail() (ServerMail, error) {
	if s.serverName == "" {
		return ServerMail{}, fmt.Errorf("server mail: no hostname")
	}
	selector, err := s.serverSelector()
	if err != nil {
		return ServerMail{}, err
	}
	if _, err := s.keys.EnsureKey(s.serverName, selector); err != nil {
		return ServerMail{}, err
	}
	rec, err := s.keys.Record(s.serverName, selector)
	if err != nil {
		return ServerMail{}, err
	}
	return ServerMail{Name: s.serverName, Selector: selector, Record: rec}, nil
}

// EnsureServerSigned makes sure the selector table has the line for the
// server's own name, rebuilding it if it does not - or if there is no table
// yet, as on the first start of 2.8.0, whose table replaced OpenDKIM's. The
// table is otherwise rebuilt only when a domain is added or deleted, so an
// installation whose DKIM_SELECTOR_DEFAULT was changed would go on signing
// with the key of the old selector while the panel shows the record of the
// new. It reports whether it had to rebuild.
func (s *Service) EnsureServerSigned() (bool, error) {
	if s.serverName == "" {
		return false, nil
	}
	selector, err := s.serverSelector()
	if err != nil {
		return false, err
	}
	signed, err := s.keys.signs(s.serverName, selector)
	if err != nil {
		return false, err
	}
	if signed {
		return false, nil
	}
	return true, s.resync()
}

// serverSelector is the selector the server's own name is signed with: that of
// the sending domain of the same name if there is one, the default otherwise.
func (s *Service) serverSelector() (string, error) {
	domains, err := s.store.ListDomains()
	if err != nil {
		return "", err
	}
	for _, d := range domains {
		if d.Name == s.serverName {
			return d.DKIMSelector, nil
		}
	}
	return s.selector, nil
}

// NewService builds the domain service. selectorDefault is the DKIM selector
// assigned to new domains (guide § Environment variables:
// DKIM_SELECTOR_DEFAULT); it is operator-configured, not user input. apps is
// used only on deletion, to clear the SASL accounts and sender-map bindings of
// the domain's applications.
func NewService(st *store.Store, keys *Keys, apps Applications, selectorDefault string) *Service {
	return &Service{store: st, keys: keys, apps: apps, selector: selectorDefault}
}

// List returns all domains with application counts (product.md).
func (s *Service) List() ([]store.Domain, error) {
	return s.store.ListDomains()
}

// Get returns one domain by id (store.ErrDomainNotFound if absent).
func (s *Service) Get(id int64) (store.Domain, error) {
	return s.store.GetDomain(id)
}

// Add registers a new sending domain: it records the row, ensures a DKIM key
// exists on disk, and regenerates the selector table (product.md).
// name must already be normalised and validated by the caller. A duplicate
// returns store.ErrDomainExists.
//
// The registry row is written first so its UNIQUE constraint is the single
// arbiter of "already exists" (avoiding a check-then-act race). An existing
// on-disk key is reused rather than overwritten, so re-adding a domain whose DB
// row was lost keeps its published DNS record valid. If key generation or the
// table's rebuild fails, the row is rolled back so we never leave a registered
// domain the signer cannot sign for.
func (s *Service) Add(name string) (store.Domain, error) {
	d, err := s.store.AddDomain(name, s.selector)
	if err != nil {
		return store.Domain{}, err
	}

	if _, err := s.keys.EnsureKey(d.Name, d.DKIMSelector); err != nil {
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

// Delete removes a domain and everything bound to it (product.md). The
// order matters: the applications' SASL accounts are cleared first, while their
// logins are still in the registry; then the registry rows (applications and
// their addresses) go via the DB cascade; then the selector table and the
// Postfix sender map are rebuilt from what remains — so the signer stops
// signing and Postfix stops authorising the domain's senders — before the
// DKIM key is deleted.
func (s *Service) Delete(id int64) error {
	d, err := s.store.GetDomain(id)
	if err != nil {
		return err
	}
	if err := s.apps.PurgeDomainSASL(id); err != nil {
		return fmt.Errorf("clear SASL accounts for %s: %w", d.Name, err)
	}
	// Drop the domain's own level-2 limit and those of its applications while the
	// application rows still exist (the cleanup query joins them). rate_limits
	// has no cascade of its own (ref_id is a plain integer, guide § Rate
	// limiting; architecture.md § Persistence).
	if err := s.store.DeleteRateLimitsForDomain(id); err != nil {
		return fmt.Errorf("clear rate limits for %s: %w", d.Name, err)
	}
	if err := s.store.DeleteDomain(id); err != nil {
		return err
	}
	// From here the domain is deleted, whatever fails after: each rebuild
	// runs - the sender map was left with the purged logins when the
	// signing tables failed first - and a failure says the domain is gone
	// (audit 2026-10-12, L17).
	var errs []error
	tablesRebuilt := true
	if err := s.resync(); err != nil {
		tablesRebuilt = false
		errs = append(errs, fmt.Errorf("rebuild the selector table: %w", err))
	}
	if err := s.apps.Resync(); err != nil {
		errs = append(errs, fmt.Errorf("rebuild the sender map: %w", err))
	}
	// A sending domain named like the server shared its key with the server's
	// own mail. The resync above kept the name in the table under the default
	// selector: the same key if that was the domain's selector, a new one -
	// with a new record to publish, which Settings shows - if it was not.
	// Either way the directory holds the key now in use. And while the table
	// may still name the deleted domain's key, the key stays.
	if d.Name != s.serverName && tablesRebuilt {
		if err := s.keys.RemoveKey(d.Name); err != nil {
			errs = append(errs, fmt.Errorf("remove its DKIM key: %w", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%w (%s): %w", ErrDeletedIncomplete, d.Name, errors.Join(errs...))
	}
	return nil
}

// ErrDeletedIncomplete is a domain deleted from the registry, with a step
// after it that failed: the mail configuration still names it until a
// rebuild succeeds (Reload configuration).
var ErrDeletedIncomplete = errors.New("domain deleted, but the mail configuration was not fully rewritten after it")

// DKIMRecord returns the DNS TXT record to publish for a domain (product.md).
func (s *Service) DKIMRecord(d store.Domain) (DKIMRecord, error) {
	return s.keys.Record(d.Name, d.DKIMSelector)
}

// RateLimit returns the domain-level differentiated rate limit (guide § Rate
// limiting), and whether one is configured, for the domain's edit form.
func (s *Service) RateLimit(domainID int64) (store.RateLimit, bool, error) {
	return s.store.GetRateLimit(store.RateLimitScopeDomain, domainID)
}

// SaveRateLimit stores the domain-level rate limit. The caller has validated the
// numbers (security.md); the milter reads the row live, so no reload is needed.
func (s *Service) SaveRateLimit(domainID int64, rl store.RateLimit) error {
	rl.Scope = store.RateLimitScopeDomain
	rl.RefID = domainID
	return s.store.SetRateLimit(rl)
}

// ClearRateLimit removes the domain-level rate limit, falling back to level 1
// only (guide § Rate limiting).
func (s *Service) ClearRateLimit(domainID int64) error {
	return s.store.DeleteRateLimit(store.RateLimitScopeDomain, domainID)
}

// Resync regenerates the selector table from the registry. It backs the
// manual reload button (architecture.md § Panel HTTP surface) and doubles as
// a recovery path if the table ever drifts from the database.
//
// It also removes the key directories of names no domain has and that are
// not the server's own: what a deletion left when the table could not be
// rebuilt (Delete keeps the key then). The "Reload configuration" the page
// points to after such a deletion finishes it here (review of audit stage 4).
func (s *Service) Resync() error {
	if err := s.resync(); err != nil {
		return err
	}
	domains, err := s.store.ListDomains()
	if err != nil {
		return err
	}
	keep := map[string]bool{s.serverName: true}
	for _, d := range domains {
		keep[d.Name] = true
	}
	names, err := s.keys.KeyDomains()
	if err != nil {
		return err
	}
	for _, name := range names {
		if !keep[name] {
			if err := s.keys.RemoveKey(name); err != nil {
				return fmt.Errorf("remove the key of %s, a domain that is gone: %w", name, err)
			}
		}
	}
	return nil
}

// resync rebuilds the selector table from the current domain set.
func (s *Service) resync() error {
	domains, err := s.store.ListDomains()
	if err != nil {
		return err
	}
	signing := make([]SigningDomain, 0, len(domains)+1)
	registered := false
	for _, d := range domains {
		signing = append(signing, SigningDomain{Name: d.Name, Selector: d.DKIMSelector})
		if d.Name == s.serverName {
			registered = true
		}
	}
	// The server's own name, for the panel's notifications - unless it is a
	// sending domain already and so in the table above.
	if s.serverName != "" && !registered {
		if _, err := s.keys.EnsureKey(s.serverName, s.selector); err != nil {
			return fmt.Errorf("dkim key for %s: %w", s.serverName, err)
		}
		signing = append(signing, SigningDomain{Name: s.serverName, Selector: s.selector})
	}
	return s.keys.Rebuild(signing)
}
