// Command panel is the SelfPost control panel. In the finished product this
// single binary combines several roles (spec 7.1): the HTTP panel server,
// the journal-milter, the mail.log tailer and the rate-limit checks.
//
// Phase 1 wires those roles up as a supervised process with a minimal HTTP
// stub, a journal-milter socket stub (so the Postfix start wrapper's readiness
// probe passes) and a log-tailer stub. Real behaviour lands in later phases.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"

	"codeberg.org/mix/selfpost/internal/backup"
	"codeberg.org/mix/selfpost/internal/buildinfo"
	"codeberg.org/mix/selfpost/internal/logtail"
	"codeberg.org/mix/selfpost/internal/store"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(buildinfo.Version)
		return
	}

	log.SetFlags(log.LstdFlags | log.LUTC)
	log.SetPrefix("panel: ")

	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

// config holds the runtime knobs the panel reads from the environment. Every
// value has a safe default so the binary also runs outside the container.
type config struct {
	httpAddr      string
	journalSocket string
	mailLog       string
	retentionDays int

	dataDir        string
	dbPath         string
	manifestPath   string
	setupTokenPath string
	hostname       string
	cookieSecure   bool

	opendkimDir     string
	dkimSelectorDef string

	saslDBPath string
	saslRealm  string
	postfixDir string
}

func loadConfig() config {
	dataDir := envDefault("SELFPOST_DATA_DIR", "/data")
	return config{
		httpAddr:      envDefault("PANEL_HTTP_ADDR", ":8080"),
		journalSocket: envDefault("JOURNAL_MILTER_SOCKET", "/run/selfpost/journal.sock"),
		mailLog:       envDefault("MAIL_LOG", "/var/log/mail.log"),
		// Send-log retention window (spec 7.3). Non-positive/invalid falls back
		// to the 90-day default inside the log-tailer.
		retentionDays: envInt("SEND_LOG_RETENTION_DAYS", 90),

		dataDir:        dataDir,
		dbPath:         envDefault("SELFPOST_DB_PATH", filepath.Join(dataDir, "selfpost.db")),
		manifestPath:   filepath.Join(dataDir, backup.ManifestName),
		setupTokenPath: envDefault("SELFPOST_SETUP_TOKEN_FILE", filepath.Join(dataDir, "setup-token")),
		hostname:       os.Getenv("SELFPOST_HOSTNAME"),
		// Secure cookies by default (spec 7.6.6); PANEL_COOKIE_SECURE=false is a
		// development-only escape hatch for testing over plain HTTP.
		cookieSecure: envDefault("PANEL_COOKIE_SECURE", "true") != "false",

		// Per-domain DKIM state (spec 6). The directory layout matches what
		// entrypoint.sh prepares (setgid, shared `selfpost` group).
		opendkimDir:     envDefault("OPENDKIM_DIR", filepath.Join(dataDir, "opendkim")),
		dkimSelectorDef: envDefault("DKIM_SELECTOR_DEFAULT", "selfpost"),

		// Application SASL accounts and the Postfix sender map (spec 5.1, 9),
		// both under /data so they survive restarts. The SASL realm defaults to
		// the server hostname so account identities line up with Postfix's SASL
		// configuration; it falls back to localhost outside the container.
		saslDBPath: envDefault("SASL_DB_PATH", filepath.Join(dataDir, "sasl", "sasldb2")),
		saslRealm:  saslRealm(),
		postfixDir: envDefault("POSTFIX_DIR", filepath.Join(dataDir, "postfix")),
	}
}

// saslRealm chooses the realm new SASL accounts live under. It mirrors the
// hostname Postfix's SASL layer uses so a client authenticating with a bare
// login resolves to the right account (finalised in Phase 5).
func saslRealm() string {
	if r := os.Getenv("SASL_REALM"); r != "" {
		return r
	}
	if h := os.Getenv("SELFPOST_HOSTNAME"); h != "" {
		return h
	}
	return "localhost"
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envInt reads an integer environment variable, returning def if it is unset or
// not a valid integer.
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		log.Printf("ignoring invalid %s=%q, using %d", key, v, def)
	}
	return def
}

// run starts the panel's three roles and blocks until a shutdown signal or the
// first fatal error from any role. A signal triggers a clean stop of all roles;
// a role error cancels the others and is returned so the process exits non-zero
// (letting supervisord/Docker see the failure — spec 4).
func run() error {
	cfg := loadConfig()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("starting selfpost panel %s", buildinfo.Version)

	// Restore version guard (spec 7.5.A): if a backup was extracted into /data,
	// its manifest version must match this binary before we touch the database,
	// so schema/format skew between versions cannot corrupt the restored state.
	// A match consumes the manifest; its absence is the normal (non-restore) case.
	if err := backup.CheckRestore(cfg.manifestPath, buildinfo.Version); err != nil {
		return err
	}

	// One database handle shared by every role. The store serialises writes
	// (MaxOpenConns(1)), so the HTTP panel, the journal-milter and the tailer
	// can all use it without stepping on each other under WAL.
	st, err := store.Open(cfg.dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	var wg sync.WaitGroup
	errc := make(chan error, 3)

	roles := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"http", func(ctx context.Context) error { return serveHTTP(ctx, cfg, st) }},
		{"journal-milter", func(ctx context.Context) error { return serveJournal(ctx, cfg, st) }},
		{"log-tailer", func(ctx context.Context) error { return logtail.Run(ctx, cfg.mailLog, st, cfg.retentionDays) }},
	}

	for _, r := range roles {
		wg.Add(1)
		go func(name string, fn func(context.Context) error) {
			defer wg.Done()
			if err := fn(ctx); err != nil {
				errc <- fmt.Errorf("%s: %w", name, err)
			}
		}(r.name, r.fn)
	}

	select {
	case <-ctx.Done():
		log.Printf("shutdown signal received, stopping")
		wg.Wait()
		log.Printf("panel stopped cleanly")
		return nil
	case err := <-errc:
		log.Printf("role failed: %v", err)
		stop() // cancel ctx so the other roles wind down
		wg.Wait()
		return err
	}
}
