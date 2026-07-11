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
	"sync"
	"syscall"

	"codeberg.org/mix/selfpost/internal/buildinfo"
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

	dataDir        string
	dbPath         string
	setupTokenPath string
	hostname       string
	cookieSecure   bool
}

func loadConfig() config {
	dataDir := envDefault("SELFPOST_DATA_DIR", "/data")
	return config{
		httpAddr:      envDefault("PANEL_HTTP_ADDR", ":8080"),
		journalSocket: envDefault("JOURNAL_MILTER_SOCKET", "/run/selfpost/journal.sock"),
		mailLog:       envDefault("MAIL_LOG", "/var/log/mail.log"),

		dataDir:        dataDir,
		dbPath:         envDefault("SELFPOST_DB_PATH", filepath.Join(dataDir, "selfpost.db")),
		setupTokenPath: envDefault("SELFPOST_SETUP_TOKEN_FILE", filepath.Join(dataDir, "setup-token")),
		hostname:       os.Getenv("SELFPOST_HOSTNAME"),
		// Secure cookies by default (spec 7.6.6); PANEL_COOKIE_SECURE=false is a
		// development-only escape hatch for testing over plain HTTP.
		cookieSecure: envDefault("PANEL_COOKIE_SECURE", "true") != "false",
	}
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
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

	var wg sync.WaitGroup
	errc := make(chan error, 3)

	roles := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"http", func(ctx context.Context) error { return serveHTTP(ctx, cfg) }},
		{"journal-milter", func(ctx context.Context) error { return serveJournalStub(ctx, cfg.journalSocket) }},
		{"log-tailer", func(ctx context.Context) error { return tailMailLog(ctx, cfg.mailLog) }},
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
