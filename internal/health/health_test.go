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
	"strings"
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
	// Real supervisorctl output: column-aligned, one line per program. Where
	// and when it was seen is not recorded; its columns are those of
	// supervisor 4.2.5 supervisorctl.py _show_statuses() and its FATAL text
	// supervisor/process.py's spawnerr.
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
	sock := filepath.Join(dir, "sign.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix sockets unavailable here: %v", err)
	}
	defer l.Close()

	if got := CheckSocket("dkim-signer", sock, true); got.Status != StatusOK || !got.Present {
		t.Errorf("live socket: status %q present=%v", got.Status, got.Present)
	}

	missing := filepath.Join(dir, "journal.sock")
	if got := CheckSocket("journal", missing, false); got.Status != StatusWarn {
		t.Errorf("missing optional socket: status %q", got.Status)
	}
	if got := CheckSocket("dkim-signer", missing, true); got.Status != StatusError {
		t.Errorf("missing required socket: status %q", got.Status)
	}

	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := CheckSocket("dkim-signer", plain, true); got.Status != StatusError || got.Present {
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

func TestLivenessFromParsedProcesses(t *testing.T) {
	// Composed, in the columns of TestParseProcesses.
	allRunning := `panel                            RUNNING   pid 22, uptime 0:04:09
postfix                          RUNNING   pid 23, uptime 0:04:08
postfix-reload                   STOPPED   Not started
`
	procs := parseProcesses(allRunning)
	for _, p := range procs {
		if mailPathPrograms[p.Name] && p.Status != StatusOK {
			t.Fatalf("%s should be ok for liveness, got %q", p.Name, p.Status)
		}
	}

	postfixDead := `panel                            RUNNING   pid 22, uptime 0:04:09
postfix                          FATAL     Exited too quickly
`
	procs = parseProcesses(postfixDead)
	for _, p := range procs {
		if p.Name == "postfix" && p.Status == StatusOK {
			t.Fatal("postfix FATAL should not grade as OK")
		}
	}
}

// Composed, after supervisor 4.2.5 rpcinterface.py _interpretProcessInfo()
// (str() of a timedelta). "127:00:01" is not a form supervisor prints - it
// only shows that the parser takes hours past 24.
func TestUptime(t *testing.T) {
	for detail, want := range map[string]time.Duration{
		"pid 41, uptime 0:03:12":          3*time.Minute + 12*time.Second,
		"pid 41, uptime 18 days, 3:02:11": 18*24*time.Hour + 3*time.Hour + 2*time.Minute + 11*time.Second,
		"pid 41, uptime 1 day, 0:00:00":   24 * time.Hour,
		"pid 7, uptime 127:00:01":         127*time.Hour + time.Second,
	} {
		if got, ok := Uptime(detail); !ok || got != want {
			t.Errorf("Uptime(%q) = %s, %v; want %s", detail, got, ok, want)
		}
	}
	for _, detail := range []string{"", "Not started", "Oct 12 04:00 PM", "pid 41, uptime soon", "pid 41, uptime x days, 0:00:01", "pid 41, uptime 0:00"} {
		if got, ok := Uptime(detail); ok {
			t.Errorf("Uptime(%q) = %s, want none", detail, got)
		}
	}
}

// The certificate shown is the one Postfix serves; a different mounted file is
// a renewal waiting, a warning once it waited past cert-reload's interval
// (assumptions A-65).
func TestCheckServedCertificate(t *testing.T) {
	dir := t.TempDir()
	served := filepath.Join(dir, "served.pem")
	mounted := filepath.Join(dir, "mounted.pem")
	day := 24 * time.Hour

	// The same file: nothing waits.
	writeCert(t, served, "mail.example.com", 60*day)
	b, _ := os.ReadFile(served)
	if err := os.WriteFile(mounted, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := CheckServedCertificate(served, mounted, day, time.Now()); got.Status != StatusOK || got.Renewal != nil || got.Path != served {
		t.Errorf("same certificate: %+v", got)
	}

	// Postfix's copy nears its end, a renewal was mounted an hour ago: no
	// warning, the renewal named.
	writeCert(t, served, "mail.example.com", 5*day)
	writeCert(t, mounted, "mail.example.com", 90*day)
	now := time.Now()
	if err := os.Chtimes(mounted, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	got := CheckServedCertificate(served, mounted, day, now)
	if got.Status != StatusOK || got.Renewal == nil || got.Renewal.Late || got.DaysLeft > 5 {
		t.Fatalf("renewal waiting: %+v", got)
	}
	if !strings.Contains(got.Detail, "at most a day after it was mounted") {
		t.Errorf("renewal waiting, detail: %q", got.Detail)
	}
	if d := got.Renewal.NotAfter.Sub(now); d < 89*day {
		t.Errorf("the renewal's expiry is the mounted certificate's: %v", d)
	}

	// Waiting past the interval and an hour: a warning, cert-reload named.
	if err := os.Chtimes(mounted, now.Add(-26*time.Hour), now.Add(-26*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got = CheckServedCertificate(served, mounted, day, now)
	if got.Status != StatusWarn || got.Renewal == nil || !got.Renewal.Late || !strings.Contains(got.Detail, "cert-reload") {
		t.Errorf("renewal late: %+v", got)
	}

	// The served one expired: still an error, the renewal said to be coming.
	writeCert(t, served, "mail.example.com", -day)
	if err := os.Chtimes(mounted, now, now); err != nil {
		t.Fatal(err)
	}
	got = CheckServedCertificate(served, mounted, day, now)
	if got.Status != StatusError || got.Renewal == nil || !strings.Contains(got.Detail, "A renewed one is waiting") {
		t.Errorf("expired, renewal waiting: %+v", got)
	}

	// No served path known: the mounted file, as before.
	if got := CheckServedCertificate("", mounted, day, now); got.Path != mounted || got.Renewal != nil {
		t.Errorf("served path unknown: %+v", got)
	}
}

func TestIntervalText(t *testing.T) {
	for d, want := range map[time.Duration]string{
		24 * time.Hour: "a day", 48 * time.Hour: "2 days", time.Hour: "an hour",
		6 * time.Hour: "6 hours", 2 * time.Second: "2 seconds", 90 * time.Minute: "90 minutes",
	} {
		if got := intervalText(d); got != want {
			t.Errorf("intervalText(%v) = %q, want %q", d, got, want)
		}
	}
}

// A certificate for another name is no certificate for this server: a client
// that checks the name refuses it however valid it is. Its DNS names are
// matched as a TLS client matches them, a wildcard for one label; with none,
// its common name, as OpenSSL - and so a sending Postfix - still does.
func TestCertificateForHost(t *testing.T) {
	dir := t.TempDir()
	cnOnly := filepath.Join(dir, "cn.pem")
	writeCert(t, cnOnly, "mail.example.org", 90*24*time.Hour)
	if c := CheckCertificate(cnOnly).ForHost("MAIL.example.org"); c.Status != StatusOK || c.MatchesHost == nil || !*c.MatchesHost {
		t.Errorf("a CN-only certificate for the hostname: %+v", c)
	}
	if c := CheckCertificate(cnOnly).ForHost("other.example.org"); c.Status != StatusError || *c.MatchesHost ||
		!strings.Contains(c.Detail, "None of the certificate's names is other.example.org") {
		t.Errorf("a certificate for another name: %+v", c)
	}

	san := filepath.Join(dir, "san.pem")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "mail.example.org"},
		DNSNames: []string{"*.example.net", "relay.example.org"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(san, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	c := CheckCertificate(san)
	if strings.Join(c.Names, " ") != "*.example.net relay.example.org" {
		t.Errorf("names = %q, want the DNS names", c.Names)
	}
	for host, want := range map[string]bool{"mail.example.net": true, "relay.example.org": true, "a.b.example.net": false,
		"mail.example.org": false /* the CN does not count beside DNS names */} {
		if got := c.ForHost(host); *got.MatchesHost != want {
			t.Errorf("ForHost(%s) matches = %v, want %v", host, *got.MatchesHost, want)
		}
	}
	if c := (Certificate{Status: StatusError}).ForHost("mail.example.org"); c.MatchesHost != nil {
		t.Error("a certificate that was not read was matched")
	}
}
