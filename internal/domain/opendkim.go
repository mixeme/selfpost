package domain

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// OpenDKIM manages the on-disk OpenDKIM state the panel is responsible for
// (spec 6): per-domain signing keys under keysDir and the KeyTable/SigningTable
// that map domains to those keys. After rewriting the tables it asks OpenDKIM to
// reload them.
type OpenDKIM struct {
	keysDir          string
	keyTablePath     string
	signingTablePath string

	// reload sends the running OpenDKIM a reload signal. It is a field so tests
	// can substitute a no-op; the default drives supervisord (see reloadViaSupervisor).
	reload func() error
}

// NewOpenDKIM builds a manager rooted at dir (typically /data/opendkim), the
// same layout entrypoint.sh prepares. The default reload path signals OpenDKIM
// through supervisord.
func NewOpenDKIM(dir string) *OpenDKIM {
	return &OpenDKIM{
		keysDir:          filepath.Join(dir, "keys"),
		keyTablePath:     filepath.Join(dir, "KeyTable"),
		signingTablePath: filepath.Join(dir, "SigningTable"),
		reload:           reloadViaSupervisor,
	}
}

// SigningDomain is one row's worth of signing configuration.
type SigningDomain struct {
	Name     string
	Selector string
}

// keyPath is the private-key path for a domain/selector, matching the KeyTable.
func (o *OpenDKIM) keyPath(domainName, selector string) string {
	return filepath.Join(o.keysDir, domainName, selector+".private")
}

// EnsureKey makes sure a signing key exists for the domain. An existing key is
// reused untouched — critical because overwriting it would silently invalidate
// the DKIM record already published in DNS (spec 6.1). Returns whether a new key
// was generated.
func (o *OpenDKIM) EnsureKey(domainName, selector string) (bool, error) {
	if err := assertConfigSafe(domainName, selector); err != nil {
		return false, err
	}
	path := o.keyPath(domainName, selector)
	if _, err := os.Stat(path); err == nil {
		return false, nil // reuse existing key
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat dkim key: %w", err)
	}
	// setgid on keysDir (entrypoint.sh) makes the per-domain dir inherit the
	// shared `selfpost` group so OpenDKIM can traverse into it.
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
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

// RemoveKey deletes a domain's key directory (spec 6.5). A missing directory is
// not an error.
func (o *OpenDKIM) RemoveKey(domainName string) error {
	if err := assertConfigSafe(domainName, "x"); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(o.keysDir, domainName)); err != nil {
		return fmt.Errorf("remove key dir for %s: %w", domainName, err)
	}
	return nil
}

// Record returns the published DKIM DNS record for a domain, recomputed from the
// private key on disk (spec 7.2.10).
func (o *OpenDKIM) Record(domainName, selector string) (DKIMRecord, error) {
	key, err := loadPrivateKeyPEM(o.keyPath(domainName, selector))
	if err != nil {
		return DKIMRecord{}, err
	}
	return dkimRecord(selector, domainName, &key.PublicKey)
}

// Rebuild regenerates KeyTable and SigningTable from the full domain set and
// reloads OpenDKIM (spec 6.2). Full regeneration (rather than incremental
// edits) keeps the files a pure function of the registry, so add and delete
// share one idempotent path. Both files are written atomically before the
// reload signal is sent.
func (o *OpenDKIM) Rebuild(domains []SigningDomain) error {
	keyTable, signingTable, err := renderTables(o.keysDir, domains)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(o.keyTablePath, keyTable, 0o640); err != nil {
		return err
	}
	if err := writeFileAtomic(o.signingTablePath, signingTable, 0o640); err != nil {
		return err
	}
	return o.reload()
}

// Reload asks OpenDKIM to re-read its tables without regenerating them. It backs
// the panel's manual reload button (spec 7.2.12).
func (o *OpenDKIM) Reload() error {
	return o.reload()
}

// renderTables builds the KeyTable and SigningTable byte contents for a domain
// set, sorted by name so the output is deterministic. Every domain is
// re-checked for shell/config-injection safety before being written (spec
// 7.6.4) — validation upstream already guarantees this, but the table writer
// refuses to emit anything unsafe as a hard backstop.
func renderTables(keysDir string, domains []SigningDomain) (keyTable, signingTable []byte, err error) {
	sorted := append([]SigningDomain(nil), domains...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var kt, st strings.Builder
	for _, d := range sorted {
		if err := assertConfigSafe(d.Name, d.Selector); err != nil {
			return nil, nil, err
		}
		keyName := d.Name // one key per domain; the domain name is a fine handle
		// Absolute key path so OpenDKIM resolves it independently of its CWD.
		keyFile := filepath.Join(keysDir, d.Name, d.Selector+".private")
		// KeyTable:     <key-name>  <domain>:<selector>:<key-path>
		fmt.Fprintf(&kt, "%s %s:%s:%s\n", keyName, d.Name, d.Selector, keyFile)
		// SigningTable (refile): <address-pattern>  <key-name>
		fmt.Fprintf(&st, "*@%s %s\n", d.Name, keyName)
	}
	return []byte(kt.String()), []byte(st.String()), nil
}

// assertConfigSafe rejects any domain/selector value that could break out of a
// single table line. Domains are already whitelisted to [a-z0-9.-] and selectors
// to a similar set before they reach here (spec 7.6.2); this is defence in depth
// against a validation gap ever letting whitespace, a newline or a field
// separator through into a config file (spec 7.6.4).
func assertConfigSafe(domainName, selector string) error {
	for _, v := range []string{domainName, selector} {
		if v == "" {
			return fmt.Errorf("opendkim: empty domain or selector")
		}
		if strings.ContainsAny(v, " \t\r\n:/\\") {
			return fmt.Errorf("opendkim: unsafe character in %q", v)
		}
	}
	return nil
}

// reloadViaSupervisor asks supervisord (PID 1, running as root) to send the
// OpenDKIM process SIGUSR1, which makes it re-read KeyTable/SigningTable
// (opendkim's documented reload signal). The panel runs unprivileged and cannot
// signal another user's process directly, so it goes through the supervisor
// control socket, reachable via the shared `selfpost` group (spec 7.6.3, 7.6.8).
//
// Arguments are fixed literals — no user input is interpolated into the command,
// and it never goes through a shell (spec 7.6.3).
func reloadViaSupervisor() error {
	cmd := exec.Command("supervisorctl",
		"-c", "/etc/supervisor/supervisord.conf",
		"signal", "USR1", "opendkim")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("reload opendkim via supervisor: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
