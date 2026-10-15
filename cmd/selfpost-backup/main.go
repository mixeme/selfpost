// Command selfpost-backup produces the full persistent-state archive from inside
// the container, invoked via `docker exec` for scripted/cron backups — the CLI
// equivalent of the panel's backup button (architecture.md § Persistence).
//
// By default the gzip-compressed tar is written to stdout, so the usual form is:
//
//	docker exec <container> selfpost-backup > selfpost-backup.tar.gz
//
// Use -o to write to a file instead. The resulting archive is self-contained:
// data/, docker-compose.yml, .env, and certs/ when present — treat it as a
// secret (architecture.md § Persistence).
//
// Given a password (SELFPOST_BACKUP_PASSWORD or -password-file, never an
// argument, which would show up in the process list) the archive is written as
// an encrypted .spbk envelope instead. Turn one back into a plain .tar.gz with
// the same password:
//
//	docker exec -i <container> selfpost-backup -decrypt < backup.spbk > backup.tar.gz
//
// Copyright (C) 2026 Mikhail Yenuchenko
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/mixeme/selfpost/internal/atomicfile"
	"github.com/mixeme/selfpost/internal/backup"
	"github.com/mixeme/selfpost/internal/buildinfo"
	"github.com/mixeme/selfpost/internal/secretfile"
	"github.com/mixeme/selfpost/internal/web/validate"
)

// passwordEnv names the environment variable holding the encryption password.
// A password must never be a command-line argument: the process list is
// readable by every process in the container.
const passwordEnv = "SELFPOST_BACKUP_PASSWORD"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	out := flag.String("o", "", "write the output to this file instead of stdout")
	in := flag.String("i", "", "read the encrypted archive from this file instead of stdin (-decrypt only)")
	decrypt := flag.Bool("decrypt", false, "decrypt an encrypted backup (.spbk) back to a plain .tar.gz")
	pwFile := flag.String("password-file", "", "read the encryption password from this file (first line); "+passwordEnv+" is used when unset")
	flag.Parse()

	if *showVersion {
		fmt.Println(buildinfo.Version)
		return
	}

	password, err := readPassword(*pwFile)
	if err == nil {
		if *decrypt {
			err = runDecrypt(*in, *out, password)
		} else {
			err = run(*out, password)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "selfpost-backup: %v\n", err)
		os.Exit(1)
	}
}

// run writes a backup, encrypting it when a password was supplied.
func run(outPath, password string) error {
	dataDir := envDefault("SELFPOST_DATA_DIR", "/data")
	dbPath := envDefault("SELFPOST_DB_PATH", filepath.Join(dataDir, "selfpost.db"))

	// The floor the panel holds the same files to: an encrypted backup is
	// attacked offline, at leisure (security.md; audit 2026-10-12, M5). An
	// empty password is no encryption, by design.
	if password != "" && utf8.RuneCountInString(password) < validate.MinSecretFilePasswordLen {
		return fmt.Errorf("the encryption password must be at least %d characters", validate.MinSecretFilePasswordLen)
	}

	w, err := openOutput(outPath)
	if err != nil {
		return err
	}
	defer w.abort()

	var sink io.Writer = w
	var env *secretfile.Writer
	if password != "" {
		env, err = secretfile.NewWriter(w, secretfile.TypeFullBackup, password)
		if err != nil {
			return err
		}
		sink = env
	}

	if err := backup.Create(sink, backup.Params{
		DataDir:    dataDir,
		DBPath:     dbPath,
		Version:    buildinfo.Version,
		DeployRoot: envDefault("SELFPOST_DEPLOY_ROOT", "/selfpost-deploy"),
		OnWarn: func(msg string) {
			fmt.Fprintf(os.Stderr, "selfpost-backup: %s\n", msg)
		},
	}); err != nil {
		return err
	}
	if env != nil {
		if err := env.Close(); err != nil {
			return err
		}
	}
	if err := w.commit(); err != nil {
		return err
	}
	if outPath != "" {
		kind := "plain"
		if password != "" {
			kind = "encrypted"
		}
		fmt.Fprintf(os.Stderr, "selfpost-backup: wrote %s (%s, SelfPost %s)\n", outPath, kind, buildinfo.Version)
	}
	return nil
}

// runDecrypt turns a .spbk envelope back into the plain gzip tar, so an
// encrypted backup can be extracted with ordinary tools during a restore.
func runDecrypt(inPath, outPath, password string) error {
	if password == "" {
		return fmt.Errorf("-decrypt needs the password (set %s or use -password-file)", passwordEnv)
	}

	r := io.Reader(os.Stdin)
	if inPath != "" {
		f, err := os.Open(inPath)
		if err != nil {
			return err
		}
		defer f.Close()
		r = f
	}

	env, err := secretfile.NewReader(r, password)
	if err != nil {
		return err
	}
	// The first chunk is opened before anything is written: a wrong password
	// is told before the output is touched, and the envelope's type is
	// authenticated only once a chunk opens (secretfile.Reader.Type; audit
	// 2026-10-12, L1).
	plain := bufio.NewReaderSize(env, 64<<10)
	if _, err := plain.Peek(1); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if env.Type() != secretfile.TypeFullBackup {
		return fmt.Errorf("that file is a %s, not a full backup", env.Type())
	}

	w, err := openOutput(outPath)
	if err != nil {
		return err
	}
	defer w.abort()

	if _, err := io.Copy(w, plain); err != nil {
		return err
	}
	if err := w.commit(); err != nil {
		return err
	}
	if outPath != "" {
		fmt.Fprintf(os.Stderr, "selfpost-backup: wrote %s (decrypted)\n", outPath)
	}
	return nil
}

// output is where the CLI writes: stdout, or a file that takes its name only
// once it is whole. A backup or a decryption that fails half way - a wrong
// password, an envelope cut short - leaves whatever had the name before, and
// no half-written file under it (audit 2026-10-12, L1 and stage 3). The file
// is the owner's alone (os.CreateTemp makes it 0600): plain and encrypted
// backups are secret enough to keep off other users' eyes.
type output struct {
	io.Writer
	tmp  *os.File // nil for stdout
	path string
}

func openOutput(outPath string) (*output, error) {
	if outPath == "" {
		return &output{Writer: os.Stdout}, nil
	}
	f, err := os.CreateTemp(filepath.Dir(outPath), "."+filepath.Base(outPath)+".tmp-*")
	if err != nil {
		return nil, err
	}
	return &output{Writer: f, tmp: f, path: outPath}, nil
}

// commit puts the whole file under its name.
func (o *output) commit() error {
	if o.tmp == nil {
		return nil
	}
	f := o.tmp
	o.tmp = nil
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	if err := os.Rename(f.Name(), o.path); err != nil {
		os.Remove(f.Name())
		return err
	}
	_ = atomicfile.SyncDir(filepath.Dir(o.path))
	return nil
}

// abort removes a file that was not committed; after commit it does nothing.
func (o *output) abort() {
	if o.tmp == nil {
		return
	}
	o.tmp.Close()
	os.Remove(o.tmp.Name())
	o.tmp = nil
}

// readPassword takes the password from the given file (first line) or, when no
// file is named, from the environment. An empty result means "no encryption".
func readPassword(pwFile string) (string, error) {
	if pwFile == "" {
		return os.Getenv(passwordEnv), nil
	}
	data, err := os.ReadFile(pwFile)
	if err != nil {
		return "", fmt.Errorf("read password file: %w", err)
	}
	// A password file is usually written with a trailing newline; take the first
	// line and strip the line ending, but keep any other whitespace, which may
	// well be part of the password.
	line, _, _ := strings.Cut(string(data), "\n")
	line = strings.TrimSuffix(line, "\r")
	if line == "" {
		return "", fmt.Errorf("password file %s is empty", pwFile)
	}
	return line, nil
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
