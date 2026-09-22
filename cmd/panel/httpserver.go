package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/mixeme/selfpost/internal/app"
	"github.com/mixeme/selfpost/internal/buildinfo"
	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web"
)

// serveHTTP runs the control-panel HTTP server until ctx is cancelled, using
// the database handle shared by all roles: setup, login and the authenticated
// panel surface (security.md).
func serveHTTP(ctx context.Context, cfg config, st *store.Store) error {
	// Applications own the SASL accounts and the Postfix sender map; the domain
	// service delegates to them when a domain (and its applications) is deleted.
	pf := postfix.New(cfg.postfixDir)
	apps := app.NewService(st, app.NewSASLDB(cfg.saslDBPath, cfg.saslRealm), pf)
	domains := domain.NewService(st, domain.NewOpenDKIM(cfg.opendkimDir), apps, cfg.dkimSelectorDef)

	srvApp, err := web.New(st, domains, apps, web.Config{
		Hostname:               cfg.hostname,
		CookieSecure:           cfg.cookieSecure,
		SubmissionEnabled:      cfg.submissionEnabled,
		MailLogPath:            cfg.mailLog,
		DataDir:                cfg.dataDir,
		DBPath:                 cfg.dbPath,
		Version:                buildinfo.Version,
		TrustedProxyCIDRs:      cfg.trustedProxies,
		TLSCertFile:            cfg.tlsCertFile,
		OpenDKIMSocket:         cfg.opendkimSocket,
		JournalSocket:          cfg.journalSocket,
		SessionIdleDays:        cfg.sessionIdleDays,
		DNSResolvers:           cfg.dnsResolvers,
		RateLimitMessagesPerIP: cfg.rateLimitMessagesPerIP,
		RateLimitWindowSeconds: cfg.rateLimitWindowSeconds,
	}, cfg.setupTokenPath)
	if err != nil {
		return err
	}
	if err := srvApp.Start(); err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.httpAddr,
		Handler:           srvApp.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Shut the server down cleanly when the process is asked to stop.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("http panel listening on %s", cfg.httpAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
