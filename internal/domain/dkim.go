// Package domain owns SelfPost's sending-domain model: per-domain DKIM key
// generation, the selector table the panel's signer reads, and the
// orchestration that keeps the SQLite registry and the on-disk keys in
// agreement (architecture.md § DKIM signing). Key material lives under /data
// so it survives container restarts.
package domain

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/mixeme/selfpost/internal/atomicfile"
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
// atomically and readable by the panel user alone (0600): the panel's signer
// is the only reader (internal/milter, readKey()). The write is atomic (temp
// file + rename) so the signer never reads a half-written key.
func writePrivateKeyPEM(path string, key *rsa.PrivateKey) error {
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	return writeFileAtomic(path, pem.EncodeToMemory(block), 0o600)
}

// loadPrivateKeyPEM reads and parses a PKCS#1 RSA private key written by
// writePrivateKeyPEM. It is used to recompute the public DNS record on demand,
// keeping the private key file the single source of truth (product.md).
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

// DKIMRecord is the DNS TXT record a user must publish for a domain (product.md).
type DKIMRecord struct {
	// Name is the record's host, e.g. "selfpost._domainkey.example.com".
	Name string
	// Value is the TXT payload, e.g. "v=DKIM1; h=sha256; k=rsa; p=MIIB...".
	Value string
}

// dkimRecord builds the published DKIM DNS record for a public key. The value
// is the one opendkim-genkey emitted: v=DKIM1, sha256, RSA, and the public key
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
// It is the single safe-write primitive for DKIM keys and the selector table
// (security.md).
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	return atomicfile.Write(path, data, perm)
}
