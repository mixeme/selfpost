package health

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"
)

// certWarnDays is how close to expiry the certificate has to be before the
// status page complains. certbot renews a 90-day certificate at 30 days left
// - a fixed 30 days before 4.0, a third of the lifetime from 4.0 (certbot
// docs, certbot/docs/using.rst, "Renewing certificates") - so two weeks is
// comfortably past the point where automatic renewal should have happened.
// For a 45-day certificate a third is 15 days, one day ahead of this warning
// (assumptions A-64).
const certWarnDays = 14

// Certificate is the state of the TLS certificate Postfix serves on 465/587
// (guide § Environment variables: TLS_CERT_FILE). The panel only reads it —
// the file is supplied by the reverse proxy through a read-only mount.
// Postfix itself serves the copy build/postfix-config.sh makes in
// /etc/postfix/tls-internal, refreshed by build/postfix-cert-reload.sh up to
// TLS_RELOAD_INTERVAL_SECONDS later, and each smtpd loads it when it starts
// (Postfix 3.7.11, src/smtpd/smtpd.c, pre_jail_init(), TLS_SERVER_INIT) - so
// the mounted file can be newer than what is served (assumptions A-65).
type Certificate struct {
	Path     string
	Subject  string
	NotAfter time.Time
	DaysLeft int
	Status   Status
	Detail   string
}

// CheckCertificate parses the leaf certificate at path and reports how much
// validity is left. A missing or unparsable file is an error status rather than
// an error return: the status page reports it in place, like every other check.
func CheckCertificate(path string) Certificate {
	c := Certificate{Path: path}
	if path == "" {
		c.Status = StatusUnknown
		c.Detail = "No certificate path is configured (TLS_CERT_FILE)."
		return c
	}
	data, err := os.ReadFile(path)
	if err != nil {
		c.Status = StatusError
		c.Detail = fmt.Sprintf("Could not read the certificate at %s.", path)
		return c
	}
	leaf, err := parseLeaf(data)
	if err != nil {
		c.Status = StatusError
		c.Detail = fmt.Sprintf("%s does not contain a readable certificate.", path)
		return c
	}

	c.Subject = leaf.Subject.CommonName
	c.NotAfter = leaf.NotAfter
	c.DaysLeft = int(time.Until(leaf.NotAfter).Hours() / 24)
	switch {
	case !time.Now().Before(leaf.NotAfter):
		c.Status = StatusError
		c.Detail = "The certificate has expired. Senders will refuse the TLS connection."
	case c.DaysLeft < certWarnDays:
		c.Status = StatusWarn
		c.Detail = fmt.Sprintf("Expires in %d day(s). Check that renewal on the host still works.", c.DaysLeft)
	default:
		c.Status = StatusOK
		c.Detail = fmt.Sprintf("Valid for another %d day(s).", c.DaysLeft)
	}
	return c
}

// parseLeaf returns the first certificate in a PEM chain — the leaf, which is
// the one whose validity clients see.
func parseLeaf(data []byte) (*x509.Certificate, error) {
	for rest := data; len(rest) > 0; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		return x509.ParseCertificate(block.Bytes)
	}
	return nil, fmt.Errorf("no CERTIFICATE block found")
}
