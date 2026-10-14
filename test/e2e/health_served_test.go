package e2e

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testHealthShowsWhatIsServed: Server › Health dials the milter sockets as
// Postfix does, rather than finding their files (assumptions A-38), and shows
// the certificate Postfix serves, naming a different mounted one as a renewal
// waiting (A-65).
func testHealthShowsWhatIsServed(t *testing.T, sc *scenario) {
	health := func() string {
		t.Helper()
		resp, body, err := sc.panel.get("/server/health")
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("GET /server/health: %v, %v", resp, err)
		}
		return body
	}
	box := func(body, id string) string {
		t.Helper()
		i := strings.Index(body, `id="`+id+`"`)
		if i < 0 {
			t.Fatalf("no box %s on Health:\n%.2000s", id, body)
		}
		rest := body[i:]
		if j := strings.Index(rest[1:], `<div class="box`); j > 0 {
			rest = rest[:j+1]
		}
		return rest
	}

	// The panel's user reaches all three sockets through the selfpost group,
	// as Postfix does: three ok rows and no detail under any.
	sockets := box(health(), "sockets")
	if n := strings.Count(sockets, `<tr><td>`); n != 3 {
		t.Fatalf("Milter sockets has %d rows, want 3:\n%s", n, sockets)
	}
	if strings.Contains(sockets, `sp-muted sp-small">`) {
		t.Fatalf("a milter socket is not answering the panel's dial:\n%s", sockets)
	}

	// A certificate mounted with a key it does not belong to: cert-reload
	// refuses to copy it (build/postfix-cert-reload.sh, same_pair), so
	// Postfix keeps serving the old one and the mounted one waits.
	certPath := filepath.Join(h.stageDir, "certs", "fullchain.pem")
	keyPath := filepath.Join(h.stageDir, "certs", "privkey.pem")
	served, err := servedNotAfter()
	if err != nil {
		t.Fatal(err)
	}
	renewalNotAfter := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Minute)
	if err := writeCertWithoutItsKey(certPath, renewalNotAfter); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// A pair that belongs together, picked up as any renewal is.
		_ = writeSelfSignedCert(certPath, keyPath)
		_ = waitFor("the mended certificate served", 30*time.Second, 500*time.Millisecond, func() (bool, error) {
			want, err := fileSerial(certPath)
			if err != nil {
				return false, err
			}
			got, err := servedSerial()
			return err == nil && got.Cmp(want) == 0, err
		})
	})
	if err := waitFor("cert-reload to refuse the mismatched pair", 20*time.Second, 500*time.Millisecond, func() (bool, error) {
		return strings.Contains(h.logs("selfpost"), "do not belong together"), nil
	}); err != nil {
		t.Fatalf("%v\n%s", err, h.logs("selfpost"))
	}
	cert := box(health(), "certificate")
	for _, want := range []string{
		"<dt>Expires</dt><dd class=\"sp-mono\">" + served.UTC().Format("2006-01-02 15:04 UTC"),
		"<dt>Renewal expires</dt><dd class=\"sp-mono\">" + renewalNotAfter.Format("2006-01-02 15:04 UTC"),
		"<dt>Renewal mounted</dt>",
		"Postfix still serves the certificate above.",
	} {
		if !strings.Contains(cert, want) {
			t.Fatalf("the TLS certificate card lacks %q:\n%s", want, cert)
		}
	}
}

// servedNotAfter is the expiry of the certificate port 465 presents.
func servedNotAfter() (time.Time, error) {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", smtpsAddr, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         selfpostHostname,
	})
	if err != nil {
		return time.Time{}, err
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return time.Time{}, fmt.Errorf("no certificate presented")
	}
	return certs[0].NotAfter, nil
}

// writeCertWithoutItsKey writes a certificate for a key that is thrown away,
// leaving the mounted key as it was.
func writeCertWithoutItsKey(certPath string, notAfter time.Time) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: selfpostHostname},
		DNSNames:     []string{selfpostHostname},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	return os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

// testFormPostWithTheBrowsersOrigin: a browser on a plain-HTTP panel at a
// name sends no Sec-Fetch-Site (Fetch Metadata § 3: only for a potentially
// trustworthy URL), and its form POST - a navigation - carries Origin by the
// page's Referrer-Policy (Fetch Standard § 3.2): its own origin under the
// panel's same-origin, which passes; "null" under no-referrer, which the
// panel sent until 2.5.2 and which the origin check refuses (assumptions
// D-B3). The client here is not a browser: it sends what one would.
func testFormPostWithTheBrowsersOrigin(t *testing.T, sc *scenario) {
	resp, _, err := sc.panel.get("/server/health")
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Header.Get("Referrer-Policy"); got != "same-origin" {
		t.Fatalf("Referrer-Policy %q, want same-origin", got)
	}
	post := func(origin string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, panelBaseURL+"/server/health/recheck", strings.NewReader(""))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin)
		resp, err := sc.panel.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := post(panelBaseURL); code != http.StatusOK {
		t.Fatalf("POST with the page's own Origin: status %d, want 200 after the redirect", code)
	}
	if code := post("null"); code != http.StatusForbidden {
		t.Fatalf("POST with Origin: null: status %d, want 403", code)
	}
}
