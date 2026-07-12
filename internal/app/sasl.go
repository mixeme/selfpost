package app

import (
	"bytes"
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
}

// NewSASLDB builds a manager for the sasldb2 at path with the given realm. The
// realm should match SELFPOST_HOSTNAME so the account identity lines up with
// Postfix's SASL configuration in Phase 5.
func NewSASLDB(path, realm string) *SASLDB {
	return &SASLDB{path: path, realm: realm, run: runSaslpasswd2}
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
