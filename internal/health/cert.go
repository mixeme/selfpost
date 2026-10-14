package health

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
// CheckServedCertificate reads the copy, and names the mounted file as a
// renewal waiting while the two differ.
type Certificate struct {
	Path     string
	Subject  string
	NotAfter time.Time
	DaysLeft int
	Status   Status
	Detail   string
	// Renewal is the mounted certificate while Postfix serves an older one.
	Renewal *Renewal
}

// Renewal is a renewed certificate in the mounted file that Postfix has not
// taken up yet.
type Renewal struct {
	NotAfter time.Time
	Mounted  time.Time // the mounted file's modification time
	// Late is true once it has waited longer than cert-reload's interval
	// and an hour: the check that should have taken it up has passed.
	Late bool
}

// ServedCertPath is the certificate file Postfix serves, smtpd_tls_cert_file
// as postconf prints it (Postfix 3.7.11, postconf(1): -h, the value without
// its name): build/postfix-config.sh's copy when the mounted files were there
// at the start, the mounted file itself otherwise. "" when postconf fails.
// It is read once, at the panel's start, as the retry policy is: main.cf is
// written before supervisord starts the panel and not after.
func ServedCertPath() string {
	out, err := exec.Command("postconf", "-h", "smtpd_tls_cert_file").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// CheckServedCertificate checks the certificate Postfix serves (served) and,
// when the mounted file differs from it byte for byte - which is how
// build/postfix-cert-reload.sh decides to copy it (cmp -s) - reports the
// mounted one as a renewal waiting. interval is TLS_RELOAD_INTERVAL_SECONDS:
// cert-reload looks for a renewal that often. A renewal that waits is the
// normal state for up to that long; the served certificate's nearing expiry
// is then no warning, its expiry still an error (clients see what is served).
// One waiting longer is a warning: cert-reload refused it (the key in the
// mount does not belong to it) or Postfix's reload failed, and cert-reload's
// lines in the container log say which. With served empty the mounted file is
// checked, as before.
func CheckServedCertificate(served, mounted string, interval time.Duration, now time.Time) Certificate {
	if served == "" {
		return CheckCertificate(mounted)
	}
	c := CheckCertificate(served)
	if mounted == "" || mounted == served {
		return c
	}
	have, err1 := os.ReadFile(served)
	want, err2 := os.ReadFile(mounted)
	fi, err3 := os.Stat(mounted)
	if err1 != nil || err2 != nil || err3 != nil || bytes.Equal(have, want) {
		return c
	}
	leaf, err := parseLeaf(want)
	if err != nil {
		return c
	}
	r := &Renewal{NotAfter: leaf.NotAfter, Mounted: fi.ModTime(), Late: now.Sub(fi.ModTime()) > interval+time.Hour}
	c.Renewal = r
	switch {
	case r.Late:
		c.Status = Worst(c.Status, StatusWarn)
		c.Detail = fmt.Sprintf("A renewed certificate has been in the mounted file since %s UTC, longer than Postfix's check for a renewal allows, and Postfix still serves the one above. cert-reload's lines in the container log say why.",
			r.Mounted.UTC().Format("2006-01-02 15:04"))
	case c.Status == StatusError:
		c.Detail += " A renewed one is waiting in the mounted file; Postfix takes it up at its next check for a renewal, at most " + intervalText(interval) + " after it was mounted."
	default:
		c.Status = StatusOK
		c.Detail = "Postfix still serves the certificate above. A renewed one is waiting in the mounted file; Postfix takes it up at its next check for a renewal, at most " + intervalText(interval) + " after it was mounted."
	}
	return c
}

// intervalText says an interval the way the help says the default: "a day".
func intervalText(d time.Duration) string {
	unit, size := "second", time.Second
	switch {
	case d >= 24*time.Hour && d%(24*time.Hour) == 0:
		unit, size = "day", 24*time.Hour
	case d >= time.Hour && d%time.Hour == 0:
		unit, size = "hour", time.Hour
	case d >= time.Minute && d%time.Minute == 0:
		unit, size = "minute", time.Minute
	}
	switch n := int64(d / size); {
	case n == 1 && unit == "hour":
		return "an hour"
	case n == 1:
		return "a " + unit
	default:
		return fmt.Sprintf("%d %ss", n, unit)
	}
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
