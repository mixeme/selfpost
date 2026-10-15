package app

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mixeme/selfpost/internal/atomicfile"
)

// SASLDB manages the Cyrus SASL account database (sasldb2) the panel maintains
// for application credentials (architecture.md § Mail path). The panel is the
// only writer; Postfix reads it to authenticate SMTP clients. Accounts are
// created and removed with the standard saslpasswd2 tool.
//
// saslpasswd2 is never pointed at the live file. Cyrus SASL 2.1.28 opens
// sasldb2 with no Berkeley DB environment, so without locks
// (sasldb/db_berkeley.c, berkeleydb_open(): db_create(mbdb, NULL, 0)), and a
// write puts and deletes in the file itself (_sasldb_putdata()); smtpd opens
// and closes it for every lookup (_sasldb_getdata()), and the backup reads it
// whenever it runs. Either could read a page half written: a spurious 535 for
// an application, or a torn sasldb2 in a backup whose restore breaks every
// application's AUTH (assumptions C-S3, C-Q3). So each change is made to a
// copy, which is then renamed over the file (replaceSASLDB): a reader opens
// the old file or the new one, whole, and needs no lock. mu keeps the
// panel's own changes from copying the same old file at once.
type SASLDB struct {
	path  string // sasldb2 file, under /data so it survives restarts
	realm string // SASL realm, so lookups match what Postfix's SASL uses

	mu sync.Mutex

	// run executes saslpasswd2. It is a field so tests can substitute a fake;
	// the default shells out to the real binary via runSaslpasswd2.
	run func(args []string, stdin []byte) error

	// replace makes a change to the database through a copy: edit gets the
	// path to give saslpasswd2. The default is replaceSASLDB.
	replace func(path string, edit func(path string) error) error

	// dump reads the raw sasldb2 as db_dump key/value pairs (Berkeley DB). It is
	// a field so tests can substitute a fake; the default runs db_dump.
	dump func(path string) ([]byte, error)
}

// NewSASLDB builds a manager for the sasldb2 at path with the given realm. The
// realm should match SELFPOST_HOSTNAME so the account identity lines up with
// Postfix's SASL configuration.
//
// Postfix 3.10.13, src/xsasl/xsasl_cyrus_server.c, xsasl_cyrus_server_create():
// sasl_server_new() gets $myhostname and, with smtpd_sasl_local_domain empty,
// no user realm; Cyrus SASL 2.1.28, plugins/sasldb.c,
// sasldb_auxprop_lookup(), then looks a login without "@" up under the
// server's name (common/plugin_common.c, _plug_parseuser()).
func NewSASLDB(path, realm string) *SASLDB {
	return &SASLDB{path: path, realm: realm, run: runSaslpasswd2, dump: dumpSASLDB, replace: replaceSASLDB}
}

// change runs saslpasswd2 with args on a copy of the database and puts the
// copy in its place; at in args stands for the path of the copy.
func (s *SASLDB) change(args []string, at int, stdin []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replace(s.path, func(path string) error {
		a := append([]string(nil), args...)
		a[at] = path
		return s.run(a, stdin)
	})
}

// replaceSASLDB copies the database at path to path+".new", lets edit change
// the copy, and renames it over path. With no database yet, edit gets a path
// where none exists, and saslpasswd2 creates one there (db_berkeley.c,
// berkeleydb_open(): DB_CREATE for a writer). The copy is a byte copy of a
// file no one else writes, which is a whole Berkeley DB file: smtpd and
// db_dump only read it. The new file is 0640 in the setgid directory, group
// selfpost, as build/entrypoint.sh sets the old one, so Postfix reads it.
// A copy left by a panel killed mid-change is removed first.
func replaceSASLDB(path string, edit func(path string) error) error {
	tmp := path + ".new"
	if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", tmp, err)
	}
	if err := copySASLDB(path, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := edit(tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Read-write: fsync of a file opened read-only is refused on Windows.
	f, err := os.OpenFile(tmp, os.O_RDWR, 0)
	if err == nil {
		err = f.Chmod(0o640)
		if err == nil {
			err = f.Sync()
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("finish %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	// The rename is the directory's change; without its own sync a power
	// loss may bring the old database back, out of step with the panel's
	// records (review of stage 2c, F10). A directory that cannot be synced
	// - not on every system - leaves the change as durable as before.
	_ = atomicfile.SyncDir(filepath.Dir(path))
	return nil
}

// copySASLDB copies path to tmp; with no file at path it makes no copy.
func copySASLDB(path, tmp string) error {
	src, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer src.Close()
	dst, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return fmt.Errorf("copy %s: %w", path, err)
	}
	return dst.Close()
}

// Set creates or updates an application's SASL account with the given password
// (architecture.md § Mail path). Used both at creation and when a password is
// regenerated; saslpasswd2 overwrites an existing entry (in the copy, see
// SASLDB).
//
// The password is passed to saslpasswd2 on stdin (never as an argument, so it
// cannot leak through the process table or logs). The login is passed as a
// separate argv element after being whitelisted by validateLogin — it never
// goes through a shell and is never interpolated into a command string
// (security.md).
func (s *SASLDB) Set(login, password string) error {
	if err := validateLogin(login); err != nil {
		return err
	}
	// Cyrus SASL 2.1.28, utils/saslpasswd.c, main(): getopt(), which ends the
	// options at "--"; -p reads the password with one read() and no prompt
	// (read_password()); -c is SASL_SET_CREATE, which lib/server.c,
	// sasl_setpass(), checks against nothing - an existing entry is written
	// over (sasldb/db_berkeley.c, _sasldb_putdata(), put with no flags).
	// -p: read the passphrase from stdin (pipe mode, no tty prompt).
	// -c: create the account / set the password.
	// -f: operate on our sasldb2 rather than the system default path.
	// -u: the realm the account lives under.
	// --: end of options, so a login can never be parsed as a flag (the
	//     whitelist already forbids nothing that getopt would eat, but a login
	//     starting with '-' is legal there — this keeps it an operand).
	args := []string{"-p", "-c", "-f", s.path, "-u", s.realm, "--", login}
	if err := s.change(args, 3, []byte(password)); err != nil {
		return fmt.Errorf("saslpasswd2 set %q: %w", login, err)
	}
	return nil
}

// Delete removes an application's SASL account (product.md). A missing account
// is not treated as an error, so deletion is idempotent and safe to retry.
//
// Cyrus SASL 2.1.28, plugins/sasldb.c, sasldb_auxprop_store(): "Deleting
// something which is not there is not an error" - the SASL_NOUSER of
// sasldb/db_berkeley.c, _sasldb_putdata(), becomes SASL_OK, so saslpasswd2 -d
// on a missing login exits 0 (utils/saslpasswd.c, main()).
func (s *SASLDB) Delete(login string) error {
	if err := validateLogin(login); err != nil {
		return err
	}
	// -d: delete the account. "--" as in Set: the login is always an operand.
	args := []string{"-d", "-f", s.path, "-u", s.realm, "--", login}
	if err := s.change(args, 2, nil); err != nil {
		return fmt.Errorf("saslpasswd2 delete %q: %w", login, err)
	}
	return nil
}

// ErrSecretNotFound is returned by Secret when the sasldb2 has no password entry
// for the login under this realm.
var ErrSecretNotFound = fmt.Errorf("sasl secret not found")

// Secret returns an application's stored password so it can be carried in a
// domain export and re-created verbatim on another instance (architecture.md §
// Persistence). This is possible because sasldb2 keeps the SASL secret in a
// password-equivalent form (the plaintext userPassword property, to serve
// challenge-response mechanisms) — unlike the admin's one-way bcrypt hash
// (security.md). The value is realm-independent, so the importer can re-key it
// under its own realm.
//
// It reads the database with db_dump (Berkeley DB), passing only our own file
// path as a fixed argument (no shell, no user input — security.md), and
// returns ErrSecretNotFound if the login has no entry.
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
//
// Cyrus SASL 2.1.28: sasldb/allockey.c, _sasldb_alloc_key(), builds the key
// with no NUL at its end; plugins/sasldb.c, sasldb_auxprop_store(), stores the
// value with strlen(); lib/server.c, sasl_setpass(), stores the plaintext
// userPassword unless SASL_SET_NOPLAIN (saslpasswd2 -n, which Set does not
// pass). Built against Berkeley DB (m4/sasldb.m4 auto_detect; debian/rules
// records BDB_VERSION). Berkeley DB 5.3 Programmer's Reference, "Dump output
// formats" (docs.oracle.com/cd/E17076_02/html/programmer_reference/
// dumpload_format.html): header lines up to HEADER=END, then for a hash or
// btree database key and data lines in turn, each a single space and hex,
// and DATA=END.
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
// ever interpreted as a command (security.md).
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
// with no shell (security.md).
//
// Only standard output is the dump: a message db_dump writes to standard
// error is not data and is left out of what is parsed (the same rule as
// postconf's values in internal/postfix, retry.go; review of base-trixie).
func dumpSASLDB(path string) ([]byte, error) {
	cmd := exec.Command("db_dump", path)
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("db_dump: %w: %s", err, strings.TrimSpace(errOut.String()))
	}
	return out, nil
}

// WithRunner replaces the command that runs saslpasswd2 and returns the same
// manager. It exists for tests outside this package that need an application
// service without the real binary; the panel never calls it. With no real
// database either, the runner is given the database's own path, not a copy.
func (s *SASLDB) WithRunner(run func(args []string, stdin []byte) error) *SASLDB {
	s.run = run
	s.replace = inPlace
	return s
}

// inPlace hands edit the path itself: for tests whose runner touches no file.
func inPlace(path string, edit func(path string) error) error { return edit(path) }
