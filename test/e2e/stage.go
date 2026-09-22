package e2e

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// selfpostHostname is SELFPOST_HOSTNAME for the whole e2e stand: the
// certificate's CN/SAN, the panel's SASL realm and Postfix's myhostname/HELO
// all have to agree on it (plan B.3), so it is defined once here.
const selfpostHostname = "mail.e2e.test"

// prepareStage (re)creates the scratch directory compose.override.yml mounts
// everything from: /data, the TLS cert Postfix serves on 465, the DNS zone
// CoreDNS is authoritative for, and the sink-MX's dump directory. Called once
// per run before `docker compose up`, so every run starts from a clean slate.
func prepareStage(s *stack) error {
	if err := os.RemoveAll(s.stageDir); err != nil {
		return fmt.Errorf("clean stage dir: %w", err)
	}
	dirs := []string{"data", "certs", "dns-stage", "mail-stage"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(s.stageDir, d), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	if err := writeSelfSignedCert(
		filepath.Join(s.stageDir, "certs", "fullchain.pem"),
		filepath.Join(s.stageDir, "certs", "privkey.pem"),
	); err != nil {
		return err
	}
	corefile, err := os.ReadFile(filepath.Join("dns", "Corefile"))
	if err != nil {
		return fmt.Errorf("read Corefile: %w", err)
	}
	if err := os.WriteFile(filepath.Join(s.stageDir, "dns-stage", "Corefile"), corefile, 0o644); err != nil {
		return fmt.Errorf("write Corefile: %w", err)
	}
	return writeZone(s.stageDir, nil)
}

// writeSelfSignedCert generates a throwaway RSA key + self-signed certificate
// for selfpostHostname, valid for a day — this stand never outlives that.
// Postfix's smtpd_tls_security_level is "may" (opportunistic), not enforced,
// so the e2e SMTP client simply skips verification of it (plan C.4: "test
// dependencies... zero new dependencies" — no need for a real CA here).
func writeSelfSignedCert(certPath, keyPath string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate TLS key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: selfpostHostname},
		DNSNames:              []string{selfpostHostname},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create certificate: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return os.WriteFile(keyPath, keyPEM, 0o600)
}
