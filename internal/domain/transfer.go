package domain

import (
	"fmt"

	"codeberg.org/mix/selfpost/internal/buildinfo"
	"codeberg.org/mix/selfpost/internal/store"
)

// FormatDomainExport identifies a single-domain export file (spec 7.5.B).
const FormatDomainExport = "selfpost-domain-export"

// DomainExport is the serialisable form of one sending domain, for moving it
// between two independently running SelfPost instances (spec 7.5.B). It carries
// the DKIM private key (so the published DNS record stays valid) and each
// application's working password (so credentials transfer without regeneration).
// The file is therefore as sensitive as a full backup and must be handled as a
// secret.
type DomainExport struct {
	Format         string      `json:"format"`
	Version        string      `json:"version"`
	Domain         string      `json:"domain"`
	DKIMSelector   string      `json:"dkim_selector"`
	DKIMPrivateKey string      `json:"dkim_private_key"` // PKCS#1 PEM
	Applications   []AppExport `json:"applications"`
}

// AppExport is one application within a DomainExport.
type AppExport struct {
	Login       string   `json:"login"`
	AddressMode string   `json:"address_mode"`
	Addresses   []string `json:"addresses,omitempty"` // list mode only
	Password    string   `json:"password"`
}

// Export builds the transferable representation of a domain: its DKIM key, its
// selector and every application with its address mode and working password
// (spec 7.5.B). The returned struct is marshalled to JSON by the caller and
// offered as a secret download.
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
	for _, a := range apps {
		password, err := s.apps.Secret(a.Login)
		if err != nil {
			return DomainExport{}, fmt.Errorf("export credential for %s: %w", a.Login, err)
		}
		exp.Applications = append(exp.Applications, AppExport{
			Login:       a.Login,
			AddressMode: a.AddressMode,
			Addresses:   a.Addresses,
			Password:    password,
		})
	}
	return exp, nil
}

// Import re-creates a domain from an export file on this instance (spec 7.5.B):
// it stores the imported DKIM key (so the published DNS record needs no change),
// registers the domain and rebuilds the OpenDKIM tables, then re-creates each
// application with its working password and rebuilds the Postfix sender map.
//
// exp.Domain must already be normalised and validated by the caller (spec
// 7.6.2); the selector is checked for config-injection safety here. A domain or
// login that already exists is rejected (store.ErrDomainExists /
// store.ErrLoginExists) rather than merged. If any step fails, everything the
// import created is rolled back, so a partial import never leaves the instance
// in an inconsistent state.
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

	for _, a := range exp.Applications {
		if err := s.apps.ImportApplication(d.ID, a.Login, a.AddressMode, a.Addresses, a.Password); err != nil {
			s.importRollback(d.ID)
			return store.Domain{}, fmt.Errorf("import application %q: %w", a.Login, err)
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
