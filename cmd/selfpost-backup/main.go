// Command selfpost-backup produces the full persistent-state archive from inside
// the container, invoked via `docker exec` for scripted/cron backups — the CLI
// equivalent of the panel's backup button (spec 7.5.A, 11.6).
//
// By default the gzip-compressed tar is written to stdout, so the usual form is:
//
//	docker exec <container> selfpost-backup > selfpost-backup.tar.gz
//
// Use -o to write to a file instead. The resulting archive contains DKIM private
// keys, the admin password hash and SASL credentials — treat it as a secret
// (spec 7.5.A).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"codeberg.org/mix/selfpost/internal/backup"
	"codeberg.org/mix/selfpost/internal/buildinfo"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	out := flag.String("o", "", "write the archive to this file instead of stdout")
	flag.Parse()

	if *showVersion {
		fmt.Println(buildinfo.Version)
		return
	}

	if err := run(*out); err != nil {
		fmt.Fprintf(os.Stderr, "selfpost-backup: %v\n", err)
		os.Exit(1)
	}
}

func run(outPath string) error {
	dataDir := envDefault("SELFPOST_DATA_DIR", "/data")
	dbPath := envDefault("SELFPOST_DB_PATH", filepath.Join(dataDir, "selfpost.db"))

	w := os.Stdout
	if outPath != "" {
		// Backups are secret; create them owner-only.
		f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}

	if err := backup.Create(w, backup.Params{
		DataDir: dataDir,
		DBPath:  dbPath,
		Version: buildinfo.Version,
	}); err != nil {
		return err
	}
	if outPath != "" {
		fmt.Fprintf(os.Stderr, "selfpost-backup: wrote %s (SelfPost %s)\n", outPath, buildinfo.Version)
	}
	return nil
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
