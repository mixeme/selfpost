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
	"strings"
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
	if err := waitFor("port 465 to present the renewed certificate", 30*time.Second, 500*time.Millisecond, func() (bool, error) {
		got, err := servedSerial()
		if err != nil {
			return false, err
		}
		if got.Cmp(want) != 0 {
			return false, fmt.Errorf("serial %s served, want %s", got, want)
		}
		return true, nil
	}); err != nil {
		return err
	}
	// The path that was broken is the one taken: Postfix reads the copy, and
	// the copy is the renewed file, the key private to root.
	out, err := s.execIn("selfpost", "sh", "-c",
		"postconf -h smtpd_tls_cert_file && cmp /etc/selfpost/tls/fullchain.pem /etc/postfix/tls-internal/fullchain.pem && "+
			"cmp /etc/selfpost/tls/privkey.pem /etc/postfix/tls-internal/privkey.pem && stat -c '%a %U' /etc/postfix/tls-internal/privkey.pem")
	if err != nil {
		return fmt.Errorf("the certificate Postfix reads: %v\n%s", err, out)
	}
	if !strings.Contains(out, "/etc/postfix/tls-internal/fullchain.pem\n") || !strings.Contains(out, "600 root") {
		return fmt.Errorf("the certificate Postfix reads: %q, want Postfix reading it and the key 600 root", out)
	}
	return nil
}

// checkCertArrivingAfterStart starts the container with no certificate in the
// mount, as a proxy that obtains its own (Caddy, Traefik) leaves it, then puts
// the pair there and waits for cert-reload to make Postfix's copy and serve
// it. Until 2.16.1 nothing made the copy: Postfix read the mounted files, the
// panel - which reads what Postfix serves - could not read a key the proxy
// keeps root's alone, and Health called the certificate unreadable until a
// restart (measured on a fresh host, 2026-10-25). The stand's interval of 2 s
// is also how often cert-reload looks while there is no copy.
func checkCertArrivingAfterStart(s *stack) error {
	certs := filepath.Join(s.stageDir, "certs")
	pair := []string{"fullchain.pem", "privkey.pem"}
	for _, f := range pair {
		if err := os.Rename(filepath.Join(certs, f), filepath.Join(s.stageDir, f+".away")); err != nil {
			return fmt.Errorf("take the certificate out of the mount: %w", err)
		}
	}
	putBack := func() error {
		for _, f := range pair {
			if err := os.Rename(filepath.Join(s.stageDir, f+".away"), filepath.Join(certs, f)); err != nil {
				return fmt.Errorf("put the certificate back: %w", err)
			}
		}
		return nil
	}
	// Recreated, not restarted: a restarted container keeps the copy's
	// folder from its earlier start, and one behind Caddy has none - the
	// case where cert-reload made it 0700, out of the panel's reach (2.16.1,
	// before its release).
	if err := s.recreateSelfpost(); err != nil {
		_ = putBack()
		return fmt.Errorf("recreate selfpost: %w", err)
	}
	if err := waitFor("Postfix running without a certificate", 60*time.Second, 500*time.Millisecond, func() (bool, error) {
		out, _ := s.execIn("selfpost", "supervisorctl", "-c", "/etc/supervisor/supervisord.conf", "status")
		if st := parseSupervisorStatus(out)["postfix"]; st != "RUNNING" {
			return false, fmt.Errorf("postfix is %q", st)
		}
		return true, nil
	}); err != nil {
		_ = putBack()
		return fmt.Errorf("%w\n%s", err, s.logs("selfpost"))
	}
	out, err := s.execIn("selfpost", "postconf", "-h", "smtpd_tls_cert_file")
	if err != nil || strings.TrimSpace(out) != "/etc/selfpost/tls/fullchain.pem" {
		_ = putBack()
		return fmt.Errorf("with no certificate at start Postfix reads %q (%v), want the mounted path itself", out, err)
	}
	if _, err := servedSerial(); err == nil {
		_ = putBack()
		return fmt.Errorf("port 465 presented a certificate with none mounted")
	}

	if err := putBack(); err != nil {
		return err
	}
	want, err := fileSerial(filepath.Join(certs, "fullchain.pem"))
	if err != nil {
		return err
	}
	if err := waitFor("port 465 to present the certificate that appeared", 30*time.Second, 500*time.Millisecond, func() (bool, error) {
		got, err := servedSerial()
		if err != nil {
			return false, err
		}
		return got.Cmp(want) == 0, fmt.Errorf("serial %s served, want %s", got, want)
	}); err != nil {
		return fmt.Errorf("%w\n%s", err, s.logs("selfpost"))
	}
	// The panel reads the copy for Health as its own user, through the
	// folder, which is 0755 as postfix-config.sh makes it at the start.
	out, err = s.execIn("selfpost", "sh", "-c",
		"postconf -h smtpd_tls_cert_file smtpd_tls_key_file && stat -c '%a %U' /etc/postfix/tls-internal /etc/postfix/tls-internal/privkey.pem && "+
			"setpriv --reuid=panel --regid=panel --init-groups head -c 27 /etc/postfix/tls-internal/fullchain.pem")
	if err != nil {
		return fmt.Errorf("the certificate Postfix reads: %v\n%s", err, out)
	}
	if out != "/etc/postfix/tls-internal/fullchain.pem\n/etc/postfix/tls-internal/privkey.pem\n755 root\n600 root\n-----BEGIN CERTIFICATE-----" {
		return fmt.Errorf("the certificate Postfix reads: %q, want the copy in a 755 folder, the key 600 root, the certificate readable by the panel", out)
	}
	if !strings.Contains(s.logs("selfpost"), "certificate appeared after start") {
		return fmt.Errorf("cert-reload did not say it took the certificate:\n%s", s.logs("selfpost"))
	}
	return waitForSMTPSReady(30 * time.Second)
}
