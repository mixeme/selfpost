package health

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorstPicksMostSevere(t *testing.T) {
	cases := []struct {
		in   []Status
		want Status
	}{
		{nil, StatusUnknown},
		{[]Status{StatusOK, StatusOK}, StatusOK},
		{[]Status{StatusOK, StatusWarn}, StatusWarn},
		{[]Status{StatusWarn, StatusError, StatusOK}, StatusError},
		{[]Status{StatusUnknown, StatusOK}, StatusOK},
	}
	for _, c := range cases {
		if got := Worst(c.in...); got != c.want {
			t.Errorf("Worst(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseProcesses(t *testing.T) {
	// Real supervisorctl output: column-aligned, one line per program.
	out := `opendkim                         RUNNING   pid 21, uptime 0:04:10
panel                            RUNNING   pid 22, uptime 0:04:09
postfix                          FATAL     Exited too quickly (process log may have details)
postfix-reload                   STOPPED   Not started
logrotate                        RUNNING   pid 25, uptime 0:04:08
`
	procs := parseProcesses(out)
	if len(procs) != 5 {
		t.Fatalf("parsed %d processes, want 5: %+v", len(procs), procs)
	}
	want := map[string]Status{
		"opendkim":       StatusOK,
		"panel":          StatusOK,
		"postfix":        StatusError,
		"postfix-reload": StatusOK, // one-shot: idle is its healthy state
		"logrotate":      StatusOK,
	}
	for _, p := range procs {
		if want[p.Name] != p.Status {
			t.Errorf("%s (%s): status %q, want %q", p.Name, p.State, p.Status, want[p.Name])
		}
	}
	if procs[0].Detail != "pid 21, uptime 0:04:10" {
		t.Errorf("detail = %q", procs[0].Detail)
	}
}

func TestParseProcessesSkipsNonStatusLines(t *testing.T) {
	out := `error: <class 'socket.error'>, [Errno 2] No such file or directory
unix:///run/supervisor.sock refused connection
`
	if procs := parseProcesses(out); len(procs) != 0 {
		t.Errorf("error output parsed as processes: %+v", procs)
	}
}

func TestCheckCertificate(t *testing.T) {
	dir := t.TempDir()

	valid := filepath.Join(dir, "valid.pem")
	writeCert(t, valid, "mail.example.com", 90*24*time.Hour)
	if got := CheckCertificate(valid); got.Status != StatusOK {
		t.Errorf("valid certificate: status %q (%s)", got.Status, got.Detail)
	} else if got.Subject != "mail.example.com" {
		t.Errorf("subject = %q", got.Subject)
	}

	soon := filepath.Join(dir, "soon.pem")
	writeCert(t, soon, "mail.example.com", 3*24*time.Hour)
	if got := CheckCertificate(soon); got.Status != StatusWarn {
		t.Errorf("nearly expired certificate: status %q (%s)", got.Status, got.Detail)
	}

	expired := filepath.Join(dir, "expired.pem")
	writeCert(t, expired, "mail.example.com", -24*time.Hour)
	if got := CheckCertificate(expired); got.Status != StatusError {
		t.Errorf("expired certificate: status %q (%s)", got.Status, got.Detail)
	}

	if got := CheckCertificate(filepath.Join(dir, "absent.pem")); got.Status != StatusError {
		t.Errorf("missing certificate: status %q", got.Status)
	}

	junk := filepath.Join(dir, "junk.pem")
	if err := os.WriteFile(junk, []byte("not a certificate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := CheckCertificate(junk); got.Status != StatusError {
		t.Errorf("unparsable certificate: status %q", got.Status)
	}

	if got := CheckCertificate(""); got.Status != StatusUnknown {
		t.Errorf("unconfigured certificate: status %q", got.Status)
	}
}

func TestCheckSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "opendkim.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix sockets unavailable here: %v", err)
	}
	defer l.Close()

	if got := CheckSocket("OpenDKIM", sock, true); got.Status != StatusOK || !got.Present {
		t.Errorf("live socket: status %q present=%v", got.Status, got.Present)
	}

	missing := filepath.Join(dir, "journal.sock")
	if got := CheckSocket("journal", missing, false); got.Status != StatusWarn {
		t.Errorf("missing optional socket: status %q", got.Status)
	}
	if got := CheckSocket("OpenDKIM", missing, true); got.Status != StatusError {
		t.Errorf("missing required socket: status %q", got.Status)
	}

	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := CheckSocket("OpenDKIM", plain, true); got.Status != StatusError || got.Present {
		t.Errorf("regular file in place of a socket: status %q present=%v", got.Status, got.Present)
	}
}

// writeCert writes a self-signed certificate expiring after validFor (negative
// for an already-expired one).
func writeCert(t *testing.T, path, cn string, validFor time.Duration) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(validFor),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	body := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}
