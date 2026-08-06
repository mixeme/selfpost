package health

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"
)

// certWarnDays is how close to expiry the certificate has to be before the
// status page complains. Let's Encrypt renews at 30 days left, so two weeks is
// comfortably past the point where automatic renewal should have happened.
const certWarnDays = 14

// Certificate is the state of the TLS certificate Postfix serves on 465/587
// (README § Environment variables: TLS_CERT_FILE). The panel only reads it —
// the file is supplied by the reverse proxy through a read-only mount.
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
