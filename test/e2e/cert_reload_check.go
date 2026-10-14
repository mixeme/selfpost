package e2e

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// servedSerial is the serial of the certificate Postfix presents on the
// implicit-TLS port.
func servedSerial() (*big.Int, error) {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", smtpsAddr, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         selfpostHostname,
	})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, fmt.Errorf("no certificate presented")
	}
	return certs[0].SerialNumber, nil
}

func fileSerial(path string) (*big.Int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("%s holds no PEM block", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	return cert.SerialNumber, nil
}

// checkCertRenewalPickedUp replaces the certificate in the stand's mount, as
// a renewer would, and waits for port 465 to present it without a restart.
// Postfix reads a copy made at start (postfix-config.sh); until 2.5.0 nothing
// renewed that copy, and a renewed certificate reached Postfix only when the
// container was restarted (audit 2026-10-12, H2). compose.override.yml sets
// TLS_RELOAD_INTERVAL_SECONDS to 2 so this does not wait a day.
func checkCertRenewalPickedUp(s *stack) error {
	certPath := filepath.Join(s.stageDir, "certs", "fullchain.pem")
	before, err := servedSerial()
	if err != nil {
		return fmt.Errorf("read the served certificate: %w", err)
	}
	if err := writeSelfSignedCert(certPath, filepath.Join(s.stageDir, "certs", "privkey.pem")); err != nil {
		return fmt.Errorf("renew the certificate: %w", err)
	}
	want, err := fileSerial(certPath)
	if err != nil {
		return err
	}
	if want.Cmp(before) == 0 {
		return fmt.Errorf("the renewed certificate has the old serial %s", before)
	}
	return waitFor("port 465 to present the renewed certificate", 30*time.Second, 500*time.Millisecond, func() (bool, error) {
		got, err := servedSerial()
		if err != nil {
			return false, err
		}
		if got.Cmp(want) != 0 {
			return false, fmt.Errorf("serial %s served, want %s", got, want)
		}
		return true, nil
	})
}
