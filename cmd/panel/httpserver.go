package main

import (
	"context"
	"errors"
	"fmt"
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

// mailStack is the panel's domain and application services plus the on-disk
// mail-path adapters they write through.
type mailStack struct {
	Domains *domain.Service
	Apps    *app.Service
	pf      *postfix.Postfix
	odk     *domain.OpenDKIM
}

func newMailStack(cfg config, st *store.Store) *mailStack {
	pf := postfix.New(cfg.postfixDir)
	odk := domain.NewOpenDKIM(cfg.opendkimDir)
	apps := app.NewService(st, app.NewSASLDB(cfg.saslDBPath, cfg.saslRealm), pf)
	domains := domain.NewService(st, odk, apps, cfg.dkimSelectorDef)
	return &mailStack{Domains: domains, Apps: apps, pf: pf, odk: odk}
}

// Resync rebuilds OpenDKIM's tables and Postfix's sender map from SQLite and
// reloads both daemons — the same work as the Status page's Reload button.
func (m *mailStack) Resync() error {
	if err := m.Domains.Resync(); err != nil {
		return fmt.Errorf("opendkim resync: %w", err)
	}
	if err := m.Apps.Resync(); err != nil {
		return fmt.Errorf("postfix resync: %w", err)
	}
	return nil
}

func (m *mailStack) skipReloadForTest() {
	m.pf.SetReloadHook(func() error { return nil })
	m.odk.SetReloadHook(func() error { return nil })
}

// resyncAfterRestore runs one mail-path Resync on the first boot after a
// backup restore. testNoReload skips the supervisord reload step so restore
// tests can verify file regeneration without a running mail stack.
func resyncAfterRestore(cfg config, st *store.Store, testNoReload bool) error {
	ms := newMailStack(cfg, st)
	if testNoReload {
		ms.skipReloadForTest()
	}
	return ms.Resync()
}

// newPanel wires the panel's services over the shared database handle and
// builds the HTTP application from cfg. It is the composition of the panel as
// the environment describes it, with nothing bound to a port yet.
func newPanel(cfg config, st *store.Store) (*web.Server, error) {
	ms := newMailStack(cfg, st)
	return web.New(st, ms.Domains, ms.Apps, web.Config{
		Hostname:               cfg.hostname,
		CookieSecure:           cfg.cookieSecure,
		SubmissionEnabled:      cfg.submissionEnabled,
		MailLogPath:            cfg.mailLog,
		DataDir:                cfg.dataDir,
		DBPath:                 cfg.dbPath,
		DeployRoot:             cfg.deployRoot,
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
}

// serveHTTP runs the control-panel HTTP server until ctx is cancelled, using
// the database handle shared by all roles: setup, login and the authenticated
// panel surface (security.md).
func serveHTTP(ctx context.Context, cfg config, st *store.Store) error {
	srvApp, err := newPanel(cfg, st)
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
