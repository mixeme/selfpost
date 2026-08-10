// Command selfpost-backup produces the full persistent-state archive from inside
// the container, invoked via `docker exec` for scripted/cron backups — the CLI
// equivalent of the panel's backup button (architecture.md § Persistence).
//
// By default the gzip-compressed tar is written to stdout, so the usual form is:
//
//	docker exec <container> selfpost-backup > selfpost-backup.tar.gz
//
// Use -o to write to a file instead. The resulting archive contains DKIM private
// keys, the admin password hash and SASL credentials — treat it as a secret
// (architecture.md § Persistence).
//
// Given a password (SELFPOST_BACKUP_PASSWORD or -password-file, never an
// argument, which would show up in the process list) the archive is written as
// an encrypted .spbk envelope instead. Turn one back into a plain .tar.gz with
// the same password:
//
//	docker exec -i <container> selfpost-backup -decrypt < backup.spbk > backup.tar.gz
//
// Copyright (C) 2026 Mikhail Yenuchenko
// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mixeme/selfpost/internal/backup"
	"github.com/mixeme/selfpost/internal/buildinfo"
	"github.com/mixeme/selfpost/internal/secretfile"
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

	w, closeOut, err := openOutput(outPath)
	if err != nil {
		return err
	}
	defer closeOut()

	sink := w
	var env *secretfile.Writer
	if password != "" {
		env, err = secretfile.NewWriter(w, secretfile.TypeFullBackup, password)
		if err != nil {
			return err
		}
		sink = env
	}

	if err := backup.Create(sink, backup.Params{
		DataDir: dataDir,
		DBPath:  dbPath,
		Version: buildinfo.Version,
	}); err != nil {
		return err
	}
	if env != nil {
		if err := env.Close(); err != nil {
			return err
		}
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
	if env.Type() != secretfile.TypeFullBackup {
		return fmt.Errorf("that file is a %s, not a full backup", env.Type())
	}

	w, closeOut, err := openOutput(outPath)
	if err != nil {
		return err
	}
	defer closeOut()

	if _, err := io.Copy(w, env); err != nil {
		return err
	}
	if outPath != "" {
		fmt.Fprintf(os.Stderr, "selfpost-backup: wrote %s (decrypted)\n", outPath)
	}
	return nil
}

// openOutput returns stdout, or a freshly created owner-only file: both plain
// and encrypted backups are secret enough to keep off other users' eyes.
func openOutput(outPath string) (io.Writer, func(), error) {
	if outPath == "" {
		return os.Stdout, func() {}, nil
	}
	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
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
