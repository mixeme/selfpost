package domain

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mixeme/selfpost/internal/configsafe"
)

// Keys manages the DKIM state on disk (architecture.md § DKIM signing): a
// signing key per name under keysDir, keysDir/<name>/<selector>.private, and
// the selector table that says, for every name the server signs, which of its
// keys signs. The panel's signer (internal/milter, SignKeys) reads both; it
// reads no database, so a page's long query never holds up a message.
type Keys struct {
	keysDir       string
	selectorsPath string
}

// NewKeys builds a manager rooted at dir (typically /data/dkim), the layout
// entrypoint.sh prepares.
func NewKeys(dir string) *Keys {
	return &Keys{
		keysDir:       filepath.Join(dir, "keys"),
		selectorsPath: filepath.Join(dir, "selectors"),
	}
}

// SigningDomain is one row's worth of signing configuration.
type SigningDomain struct {
	Name     string
	Selector string
}

// KeyPath is the private-key file of a name and selector.
func KeyPath(keysDir, domainName, selector string) string {
	return filepath.Join(keysDir, domainName, selector+".private")
}

func (o *Keys) keyPath(domainName, selector string) string {
	return KeyPath(o.keysDir, domainName, selector)
}

// EnsureKey makes sure a signing key exists for the domain. An existing key is
// reused untouched — critical because overwriting it would silently invalidate
// the DKIM record already published in DNS (architecture.md § DKIM signing).
// Returns whether a new key was generated.
func (o *Keys) EnsureKey(domainName, selector string) (bool, error) {
	if err := assertConfigSafe(domainName, selector); err != nil {
		return false, err
	}
	path := o.keyPath(domainName, selector)
	if _, err := os.Stat(path); err == nil {
		return false, nil // reuse existing key
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat dkim key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, fmt.Errorf("create key dir: %w", err)
	}
	key, err := generateDKIMKey()
	if err != nil {
		return false, err
	}
	if err := writePrivateKeyPEM(path, key); err != nil {
		return false, err
	}
	return true, nil
}

// RemoveKey deletes a domain's key directory (architecture.md § DKIM
// signing). A missing directory is not an error.
func (o *Keys) RemoveKey(domainName string) error {
	if err := assertConfigSafe(domainName, "x"); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(o.keysDir, domainName)); err != nil {
		return fmt.Errorf("remove key dir for %s: %w", domainName, err)
	}
	return nil
}

// KeyDomains lists the names that have a key directory.
func (o *Keys) KeyDomains() ([]string, error) {
	entries, err := os.ReadDir(o.keysDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list key dirs: %w", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// ExportKey returns a domain's DKIM private key as PKCS#1 PEM, for carrying in
// a domain export so the receiving instance signs with the same key and the
// DNS TXT record never has to change (architecture.md § Persistence). It
// re-marshals the parsed key rather than returning the raw file, so a
// malformed on-disk key is caught here.
func (o *Keys) ExportKey(domainName, selector string) ([]byte, error) {
	if err := assertConfigSafe(domainName, selector); err != nil {
		return nil, err
	}
	key, err := loadPrivateKeyPEM(o.keyPath(domainName, selector))
	if err != nil {
		return nil, err
	}
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	return pem.EncodeToMemory(block), nil
}

// ImportKey writes an imported DKIM private key to disk for a domain
// (architecture.md § Persistence). The PEM is parsed and re-marshalled through
// the same writer used for generated keys, so only a well-formed PKCS#1 RSA
// key is ever stored. Unlike EnsureKey it overwrites: an import (re-)creates
// the domain with exactly this key, which is the whole point of keeping the
// published DNS record valid.
func (o *Keys) ImportKey(domainName, selector string, pemKey []byte) error {
	if err := assertConfigSafe(domainName, selector); err != nil {
		return err
	}
	block, _ := pem.Decode(pemKey)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return fmt.Errorf("import dkim key for %s: not a PKCS#1 RSA private key", domainName)
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("import dkim key for %s: %w", domainName, err)
	}
	path := o.keyPath(domainName, selector)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create key dir: %w", err)
	}
	return writePrivateKeyPEM(path, key)
}

// Record returns the published DKIM DNS record for a domain, recomputed from the
// private key on disk (product.md).
func (o *Keys) Record(domainName, selector string) (DKIMRecord, error) {
	key, err := loadPrivateKeyPEM(o.keyPath(domainName, selector))
	if err != nil {
		return DKIMRecord{}, err
	}
	return dkimRecord(selector, domainName, &key.PublicKey)
}

// Rebuild rewrites the selector table from the full domain set. Full
// regeneration (rather than incremental edits) keeps the file a pure function
// of the registry, so add and delete share one idempotent path. The signer
// reads the file again when it changes; nothing is reloaded.
func (o *Keys) Rebuild(domains []SigningDomain) error {
	table, err := renderSelectors(domains)
	if err != nil {
		return err
	}
	return writeFileAtomic(o.selectorsPath, table, 0o600)
}

// renderSelectors builds the selector table for a domain set, a line
// "<name> <selector>" each, sorted by name so the output is deterministic.
// Every domain is re-checked for config-injection safety before being written
// (security.md) — validation upstream already guarantees this, but the table
// writer refuses to emit anything unsafe as a hard backstop.
func renderSelectors(domains []SigningDomain) ([]byte, error) {
	sorted := append([]SigningDomain(nil), domains...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	var sb strings.Builder
	for _, d := range sorted {
		if err := assertConfigSafe(d.Name, d.Selector); err != nil {
			return nil, err
		}
		fmt.Fprintf(&sb, "%s %s\n", d.Name, d.Selector)
	}
	return []byte(sb.String()), nil
}

// keyForbidden is what may never appear in a name or a selector: white space
// and the newline that would end the selector table's field or line, and the
// path separators - the name and the selector are also the key's path - with
// the colon, which no DNS name holds either.
const keyForbidden = " \t\r\n:/\\"

// assertConfigSafe rejects any domain/selector value that could break out of a
// single table line or out of the key directory. Domains are already
// whitelisted to [a-z0-9.-] and selectors to a similar set before they reach
// here (security.md); this is defence in depth against a validation gap ever
// letting whitespace, a newline or a path separator through. A name of dots
// alone - ".." - would also climb out of keysDir.
func assertConfigSafe(domainName, selector string) error {
	for _, f := range []struct{ what, value string }{
		{"domain", domainName},
		{"selector", selector},
	} {
		if err := configsafe.Token(f.what, f.value, keyForbidden); err != nil {
			return fmt.Errorf("dkim: %w", err)
		}
		if strings.Trim(f.value, ".") == "" {
			return fmt.Errorf("dkim: unsafe %s %q", f.what, f.value)
		}
	}
	return nil
}
