// Package domain owns SelfPost's sending-domain model: per-domain DKIM key
// generation, the OpenDKIM KeyTable/SigningTable that drive signing, and the
// orchestration that keeps the SQLite registry, the on-disk keys and OpenDKIM
// in agreement (spec 4.1, 6). Key material lives under /data so it survives
// container restarts (spec 6.1, 9).
package domain

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// dkimKeyBits is the RSA key size for DKIM signing keys. 2048 is the DKIM
// interoperability sweet spot: strong, and short enough that the published
// public key still fits comfortably in a DNS TXT record.
const dkimKeyBits = 2048

// generateDKIMKey creates a fresh RSA private key for signing a domain.
func generateDKIMKey() (*rsa.PrivateKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, dkimKeyBits)
	if err != nil {
		return nil, fmt.Errorf("generate dkim key: %w", err)
	}
	return key, nil
}

// writePrivateKeyPEM writes key to path as a PKCS#1 "RSA PRIVATE KEY" PEM,
// atomically and group-readable (0640). The file is owned by the panel user and
// read by OpenDKIM through the shared `selfpost` group (see build/opendkim.conf
// and entrypoint.sh); the parent directory carries setgid so the group is
// inherited. The write is atomic (temp file + rename) so OpenDKIM never observes
// a half-written key.
func writePrivateKeyPEM(path string, key *rsa.PrivateKey) error {
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	return writeFileAtomic(path, pem.EncodeToMemory(block), 0o640)
}

// loadPrivateKeyPEM reads and parses a PKCS#1 RSA private key written by
// writePrivateKeyPEM. It is used to recompute the public DNS record on demand,
// keeping the private key file the single source of truth (spec 7.2.10).
func loadPrivateKeyPEM(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return nil, fmt.Errorf("dkim key %s: not a PKCS#1 RSA private key", path)
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse dkim key %s: %w", path, err)
	}
	return key, nil
}

// DKIMRecord is the DNS TXT record a user must publish for a domain (spec 7.2.10).
type DKIMRecord struct {
	// Name is the record's host, e.g. "selfpost._domainkey.example.com".
	Name string
	// Value is the TXT payload, e.g. "v=DKIM1; h=sha256; k=rsa; p=MIIB...".
	Value string
}

// dkimRecord builds the published DKIM DNS record for a public key. The value
// mirrors what opendkim-genkey emits: v=DKIM1, sha256, RSA, and the public key
// as base64-encoded SubjectPublicKeyInfo (PKIX) DER.
func dkimRecord(selector, domainName string, pub *rsa.PublicKey) (DKIMRecord, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return DKIMRecord{}, fmt.Errorf("marshal dkim public key: %w", err)
	}
	p := base64.StdEncoding.EncodeToString(der)
	return DKIMRecord{
		Name:  fmt.Sprintf("%s._domainkey.%s", selector, domainName),
		Value: fmt.Sprintf("v=DKIM1; h=sha256; k=rsa; p=%s", p),
	}, nil
}

// writeFileAtomic writes data to path via a temp file in the same directory
// followed by a rename, so readers only ever see the complete old or new file.
// It is the single safe-write primitive for DKIM keys and OpenDKIM tables
// (spec 7.6.4).
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	cleanup = false
	return nil
}
