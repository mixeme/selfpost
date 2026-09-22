package domain

import (
	"database/sql"
	"fmt"

	"github.com/mixeme/selfpost/internal/buildinfo"
	"github.com/mixeme/selfpost/internal/store"
)

// FormatDomainExport identifies a single-domain export file (architecture.md §
// Persistence).
const FormatDomainExport = "selfpost-domain-export"

// DomainExport is the serialisable form of one sending domain, for moving it
// between two independently running SelfPost instances (architecture.md §
// Persistence). It carries the DKIM private key (so the published DNS record
// stays valid) and each application's working password (so credentials
// transfer without regeneration). The file is therefore as sensitive as a full
// backup and must be handled as a secret.
type DomainExport struct {
	Format         string      `json:"format"`
	Version        string      `json:"version"`
	Domain         string      `json:"domain"`
	DKIMSelector   string      `json:"dkim_selector"`
	DKIMPrivateKey string      `json:"dkim_private_key"`    // PKCS#1 PEM
	DMARCRua       *string     `json:"dmarc_rua,omitempty"` // nil = inherit profile; set = override ("" = none)
	RateLimit      *RateLimitExport `json:"rate_limit,omitempty"`
	Applications   []AppExport `json:"applications"`
}

// RateLimitExport is the transferable level-2 limit for a domain or application.
type RateLimitExport struct {
	Mode           string   `json:"mode,omitempty"`
	MaxMessages    int      `json:"max_messages,omitempty"`
	WindowSeconds  int      `json:"window_seconds,omitempty"`
	AutoMultiplier float64  `json:"auto_multiplier,omitempty"`
	AllowedIPs     []string `json:"allowed_ips,omitempty"`
}

// AppExport is one application within a DomainExport.
type AppExport struct {
	Login            string   `json:"login"`
	AddressMode      string   `json:"address_mode"`
	Addresses        []string `json:"addresses,omitempty"` // list mode only
	Password         string   `json:"password"`
	AuthIPRestrict   bool     `json:"auth_ip_restrict,omitempty"`
	AuthAllowedIPs   []string `json:"auth_allowed_ips,omitempty"`
	RateLimit        *RateLimitExport `json:"rate_limit,omitempty"`
}

// Export builds the transferable representation of a domain: its DKIM key, its
// selector and every application with its address mode and working password
// (architecture.md § Persistence). The returned struct is marshalled to JSON
// by the caller and offered as a secret download.
func (s *Service) Export(id int64) (DomainExport, error) {
	d, err := s.store.GetDomain(id)
	if err != nil {
		return DomainExport{}, err
	}
	pem, err := s.odk.ExportKey(d.Name, d.DKIMSelector)
	if err != nil {
		return DomainExport{}, fmt.Errorf("export DKIM key for %s: %w", d.Name, err)
	}
	apps, err := s.store.ListApplicationsByDomain(id)
	if err != nil {
		return DomainExport{}, err
	}
	exp := DomainExport{
		Format:         FormatDomainExport,
		Version:        buildinfo.Version,
		Domain:         d.Name,
		DKIMSelector:   d.DKIMSelector,
		DKIMPrivateKey: string(pem),
		Applications:   make([]AppExport, 0, len(apps)),
	}
	if d.DMARCRua.Valid {
		s := d.DMARCRua.String
		exp.DMARCRua = &s
	}
	if rl, ok, err := s.store.GetRateLimit(store.RateLimitScopeDomain, id); err == nil && ok {
		exp.RateLimit = exportRateLimit(rl)
	}
	for _, a := range apps {
		password, err := s.apps.Secret(a.Login)
		if err != nil {
			return DomainExport{}, fmt.Errorf("export credential for %s: %w", a.Login, err)
		}
		appExp := AppExport{
			Login:          a.Login,
			AddressMode:    a.AddressMode,
			Addresses:      a.Addresses,
			Password:       password,
			AuthIPRestrict: a.AuthIPRestrict,
			AuthAllowedIPs: a.AuthAllowedIPs,
		}
		if rl, ok, err := s.store.GetRateLimit(store.RateLimitScopeApp, a.ID); err == nil && ok {
			appExp.RateLimit = exportRateLimit(rl)
		}
		exp.Applications = append(exp.Applications, appExp)
	}
	return exp, nil
}

// Import re-creates a domain from an export file on this instance
// (architecture.md § Persistence): it stores the imported DKIM key (so the
// published DNS record needs no change), registers the domain and rebuilds the
// OpenDKIM tables, then re-creates each application with its working password
// and rebuilds the Postfix sender map.
//
// exp.Domain must already be normalised and validated by the caller
// (security.md); the selector is checked for
// config-injection safety here. A domain or login that already exists is
// rejected (store.ErrDomainExists / store.ErrLoginExists) rather than merged.
// If any step fails, everything the import created is rolled back, so a
// partial import never leaves the instance in an inconsistent state.
func (s *Service) Import(exp DomainExport) (store.Domain, error) {
	if exp.Format != FormatDomainExport {
		return store.Domain{}, fmt.Errorf("not a SelfPost domain export (format %q)", exp.Format)
	}
	if err := assertConfigSafe(exp.Domain, exp.DKIMSelector); err != nil {
		return store.Domain{}, err
	}

	// Registry row first, so its UNIQUE constraint is the sole arbiter of a
	// duplicate domain before we touch the filesystem.
	d, err := s.store.AddDomain(exp.Domain, exp.DKIMSelector)
	if err != nil {
		return store.Domain{}, err // ErrDomainExists surfaces to the caller
	}

	if err := s.odk.ImportKey(d.Name, d.DKIMSelector, []byte(exp.DKIMPrivateKey)); err != nil {
		s.importRollback(d.ID)
		return store.Domain{}, err
	}
	if err := s.resync(); err != nil {
		s.importRollback(d.ID)
		return store.Domain{}, err
	}

	if exp.DMARCRua != nil {
		if err := s.store.UpdateDomainDMARCRua(d.ID, sql.NullString{Valid: true, String: *exp.DMARCRua}); err != nil {
			s.importRollback(d.ID)
			return store.Domain{}, err
		}
		d.DMARCRua = sql.NullString{Valid: true, String: *exp.DMARCRua}
	}
	if exp.RateLimit != nil {
		if err := s.importRateLimit(store.RateLimitScopeDomain, d.ID, *exp.RateLimit); err != nil {
			s.importRollback(d.ID)
			return store.Domain{}, err
		}
	}

	for _, a := range exp.Applications {
		if err := s.apps.ImportApplication(d.ID, a.Login, a.AddressMode, a.Addresses, a.Password); err != nil {
			s.importRollback(d.ID)
			return store.Domain{}, fmt.Errorf("import application %q: %w", a.Login, err)
		}
		restrict, ips := a.AuthIPRestrict, a.AuthAllowedIPs
		// Legacy exports stored client IPs on the rate-limit row as trusted IPs.
		if a.RateLimit != nil && len(a.RateLimit.AllowedIPs) > 0 {
			restrict = true
			ips = a.RateLimit.AllowedIPs
		}
		needAuthIPs := restrict || len(ips) > 0
		needRateLimit := a.RateLimit != nil
		if needAuthIPs || needRateLimit {
			app, err := s.store.GetApplicationByLogin(a.Login)
			if err != nil {
				s.importRollback(d.ID)
				return store.Domain{}, fmt.Errorf("import application %q: %w", a.Login, err)
			}
			if needAuthIPs {
				if err := s.store.UpdateApplicationAuthIPs(app.ID, restrict, ips); err != nil {
					s.importRollback(d.ID)
					return store.Domain{}, fmt.Errorf("import auth IPs for %q: %w", a.Login, err)
				}
			}
			if needRateLimit {
				if err := s.importRateLimit(store.RateLimitScopeApp, app.ID, *a.RateLimit); err != nil {
					s.importRollback(d.ID)
					return store.Domain{}, fmt.Errorf("import rate limit for %q: %w", a.Login, err)
				}
			}
		}
	}
	if err := s.apps.Resync(); err != nil {
		s.importRollback(d.ID)
		return store.Domain{}, err
	}
	return d, nil
}

// importRollback best-effort tears down a partially imported domain by running
// the normal deletion path, which clears the SASL accounts of any applications
// already created, removes the registry rows (cascade), rebuilds both maps and
// removes the DKIM key. Any error here is subordinate to the original failure
// the caller returns.
func (s *Service) importRollback(id int64) {
	_ = s.Delete(id)
}

func exportRateLimit(rl store.RateLimit) *RateLimitExport {
	mode := rl.Mode
	if mode == "" {
		mode = store.RateLimitModeManual
	}
	exp := &RateLimitExport{
		Mode:           mode,
		MaxMessages:    rl.MaxMessages,
		WindowSeconds:  rl.WindowSeconds,
		AutoMultiplier: rl.AutoMultiplier,
	}
	return exp
}

func (s *Service) importRateLimit(scope string, refID int64, exp RateLimitExport) error {
	mode := exp.Mode
	if mode == "" {
		mode = store.RateLimitModeManual
	}
	rl := store.RateLimit{
		Scope:          scope,
		RefID:          refID,
		Mode:           mode,
		MaxMessages:    exp.MaxMessages,
		WindowSeconds:  exp.WindowSeconds,
		AutoMultiplier: exp.AutoMultiplier,
	}
	if mode == store.RateLimitModeManual && rl.MaxMessages <= 0 && rl.WindowSeconds <= 0 {
		return nil
	}
	return s.store.SetRateLimit(rl)
}
