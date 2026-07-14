package app

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
)

// SASLDB manages the Cyrus SASL account database (sasldb2) the panel maintains
// for application credentials (spec 5.1). The panel is the only writer; Postfix
// reads it to authenticate SMTP clients. Accounts are created and removed with
// the standard saslpasswd2 tool ("эквивалент saslpasswd2", per the plan).
type SASLDB struct {
	path  string // sasldb2 file, under /data so it survives restarts (spec 9)
	realm string // SASL realm, so lookups match what Postfix's SASL uses

	// run executes saslpasswd2. It is a field so tests can substitute a fake;
	// the default shells out to the real binary via runSaslpasswd2.
	run func(args []string, stdin []byte) error

	// dump reads the raw sasldb2 as db_dump key/value pairs (Berkeley DB). It is
	// a field so tests can substitute a fake; the default runs db_dump.
	dump func(path string) ([]byte, error)
}

// NewSASLDB builds a manager for the sasldb2 at path with the given realm. The
// realm should match SELFPOST_HOSTNAME so the account identity lines up with
// Postfix's SASL configuration in Phase 5.
func NewSASLDB(path, realm string) *SASLDB {
	return &SASLDB{path: path, realm: realm, run: runSaslpasswd2, dump: dumpSASLDB}
}

// Set creates or updates an application's SASL account with the given password
// (spec 5.1, 7.2.9). Used both at creation and when a password is regenerated;
// saslpasswd2 overwrites an existing entry in place.
//
// The password is passed to saslpasswd2 on stdin (never as an argument, so it
// cannot leak through the process table or logs). The login is passed as a
// separate argv element after being whitelisted by validateLogin — it never
// goes through a shell and is never interpolated into a command string (spec
// 7.6.3).
func (s *SASLDB) Set(login, password string) error {
	if err := validateLogin(login); err != nil {
		return err
	}
	// -p: read the passphrase from stdin (pipe mode, no tty prompt).
	// -c: create the account / set the password.
	// -f: operate on our sasldb2 rather than the system default path.
	// -u: the realm the account lives under.
	args := []string{"-p", "-c", "-f", s.path, "-u", s.realm, login}
	if err := s.run(args, []byte(password)); err != nil {
		return fmt.Errorf("saslpasswd2 set %q: %w", login, err)
	}
	return nil
}

// Delete removes an application's SASL account (spec 7.2.8). A missing account
// is not treated as an error, so deletion is idempotent and safe to retry.
func (s *SASLDB) Delete(login string) error {
	if err := validateLogin(login); err != nil {
		return err
	}
	// -d: delete the account.
	args := []string{"-d", "-f", s.path, "-u", s.realm, login}
	if err := s.run(args, nil); err != nil {
		return fmt.Errorf("saslpasswd2 delete %q: %w", login, err)
	}
	return nil
}

// ErrSecretNotFound is returned by Secret when the sasldb2 has no password entry
// for the login under this realm.
var ErrSecretNotFound = fmt.Errorf("sasl secret not found")

// Secret returns an application's stored password so it can be carried in a
// domain export and re-created verbatim on another instance (spec 7.5.B). This
// is possible because sasldb2 keeps the SASL secret in a password-equivalent
// form (the plaintext userPassword property, to serve challenge-response
// mechanisms) — unlike the admin's one-way bcrypt hash (spec 7.6). The value is
// realm-independent, so the importer can re-key it under its own realm.
//
// It reads the database with db_dump (Berkeley DB), passing only our own file
// path as a fixed argument (no shell, no user input — spec 7.6.3), and returns
// ErrSecretNotFound if the login has no entry.
func (s *SASLDB) Secret(login string) (string, error) {
	if err := validateLogin(login); err != nil {
		return "", err
	}
	out, err := s.dump(s.path)
	if err != nil {
		return "", fmt.Errorf("read sasldb2 for %q: %w", login, err)
	}
	secret, ok, err := parseSASLSecret(out, login, s.realm)
	if err != nil {
		return "", fmt.Errorf("parse sasldb2 for %q: %w", login, err)
	}
	if !ok {
		return "", fmt.Errorf("login %q: %w", login, ErrSecretNotFound)
	}
	return secret, nil
}

// parseSASLSecret scans db_dump's byte-value output for the userPassword entry
// keyed by (login, realm). sasldb2 keys are NUL-separated tuples
// "<login>\0<realm>\0<property>"; the matching value is the stored password.
func parseSASLSecret(dump []byte, login, realm string) (string, bool, error) {
	sc := bufio.NewScanner(bytes.NewReader(dump))
	// sasldb2 records are tiny, but raise the line cap so a long hex line is
	// never silently truncated.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	inData := false
	var keyBytes []byte
	haveKey := false
	for sc.Scan() {
		line := sc.Text()
		if !inData {
			if line == "HEADER=END" {
				inData = true
			}
			continue
		}
		if line == "DATA=END" {
			break
		}
		// Each data line is a single leading space followed by hex.
		hexStr := strings.TrimPrefix(line, " ")
		raw, err := hex.DecodeString(hexStr)
		if err != nil {
			return "", false, fmt.Errorf("bad db_dump hex line: %w", err)
		}
		if !haveKey {
			keyBytes = raw
			haveKey = true
			continue
		}
		// raw is the value for keyBytes.
		haveKey = false
		parts := bytes.Split(keyBytes, []byte{0})
		if len(parts) != 3 {
			continue
		}
		if string(parts[0]) == login && string(parts[1]) == realm && string(parts[2]) == "userPassword" {
			return string(raw), true, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", false, err
	}
	return "", false, nil
}

// runSaslpasswd2 executes the real saslpasswd2 with the given arguments and
// stdin. Arguments are passed as a fixed argv (no shell), so no user input is
// ever interpreted as a command (spec 7.6.3).
func runSaslpasswd2(args []string, stdin []byte) error {
	cmd := exec.Command("saslpasswd2", args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// dumpSASLDB runs db_dump to export the sasldb2 as key/value hex pairs. The path
// is our own sasldb2 file (never user input) and is passed as a fixed argument
// with no shell (spec 7.6.3).
func dumpSASLDB(path string) ([]byte, error) {
	cmd := exec.Command("db_dump", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("db_dump: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
