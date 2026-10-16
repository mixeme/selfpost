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
	"github.com/mixeme/selfpost/internal/dmarc"
	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/inbound"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web"
)

// mailStack is the panel's domain and application services plus the on-disk
// mail-path adapters they write through.
type mailStack struct {
	Domains        *domain.Service
	Apps           *app.Service
	Inbound        *inbound.Service
	Lists          *inbound.Lists
	DMARC          *dmarc.Service
	pf             *postfix.Postfix
	inboundEnabled bool
	dmarcEnabled   bool
}

func newMailStack(cfg config, st *store.Store) *mailStack {
	pf := postfix.New(cfg.postfixDir)
	apps := app.NewService(st, app.NewSASLDB(cfg.saslDBPath, cfg.saslRealm), pf)
	domains := domain.NewService(st, domain.NewKeys(cfg.dkimDir), apps, cfg.dkimSelectorDef)
	// The panel's notifications are sent as postmaster@ the server's own name,
	// and signed as it.
	domains.SetServerName(cfg.hostname)
	inb := inbound.NewService(st, pf)
	dmarcSvc := dmarc.NewService(st, pf, cfg.hostname, cfg.dmarcEnabled)
	return &mailStack{
		Domains: domains, Apps: apps, Inbound: inb, DMARC: dmarcSvc,
		Lists: inbound.NewLists(st, cfg.antispamDir),
		pf:    pf, inboundEnabled: cfg.inboundEnabled, dmarcEnabled: cfg.dmarcEnabled,
	}
}

// Resync rebuilds the selector table and Postfix's maps from SQLite and
// reloads Postfix — the same work as the Health page's Reload button.
func (m *mailStack) Resync() error {
	if err := m.Domains.Resync(); err != nil {
		return fmt.Errorf("dkim resync: %w", err)
	}
	if err := m.Apps.Resync(); err != nil {
		return fmt.Errorf("postfix resync: %w", err)
	}
	if m.inboundEnabled {
		if err := m.Inbound.Resync(); err != nil {
			return fmt.Errorf("inbound maps resync: %w", err)
		}
		// The filter's configuration names all six list files; they exist from
		// the first start, empty until an entry is added.
		if err := m.Lists.Sync(); err != nil {
			return fmt.Errorf("filter lists resync: %w", err)
		}
	}
	if m.dmarcEnabled {
		if err := m.DMARC.Resync(); err != nil {
			return fmt.Errorf("dmarc maps resync: %w", err)
		}
	}
	return nil
}

func (m *mailStack) skipReloadForTest() {
	m.pf.SetReloadHook(func() error { return nil })
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
	// postfix-config.sh has already run (entrypoint). postconf -h is the
	// effective config, including a manual override; the panel keeps this
	// snapshot for the process lifetime (architecture.md). Postfix 3.10.13,
	// postconf(1): without -d or -n it shows main.cf's value or the default,
	// and -h drops the "name = " label; values are not $-expanded without -x.
	retryPolicy := postfix.LoadRetryPolicy()
	return web.New(st, ms.Domains, ms.Apps, ms.Inbound, ms.Lists, ms.DMARC, web.Config{
		Hostname:                   cfg.hostname,
		CookieSecure:               cfg.cookieSecure,
		SubmissionEnabled:          cfg.submissionEnabled,
		MailLogPath:                cfg.mailLog,
		DataDir:                    cfg.dataDir,
		DBPath:                     cfg.dbPath,
		DeployRoot:                 cfg.deployRoot,
		Version:                    buildinfo.Version,
		TrustedProxyCIDRs:          cfg.trustedProxies,
		TLSCertFile:                cfg.tlsCertFile,
		TLSServedCertFile:          cfg.tlsServedCertFile,
		SMTPBanner:                 cfg.smtpBanner,
		TimeZone:                   cfg.timeZone,
		TLSReloadInterval:          cfg.tlsReloadInterval,
		SignSocket:                 cfg.signSocket,
		SignLocalSocket:            cfg.signLocalSocket,
		JournalSocket:              cfg.journalSocket,
		SenderSocket:               cfg.senderSocket,
		SessionIdleDays:            cfg.sessionIdleDays,
		DNSResolvers:               cfg.dnsResolvers,
		RateLimitMessagesPerIP:     cfg.rateLimitMessagesPerIP,
		RateLimitWindowSeconds:     cfg.rateLimitWindowSeconds,
		RetryPolicy:                retryPolicy,
		InboundEnabled:             cfg.inboundEnabled,
		InboundAntispamMilter:      cfg.inboundAntispamMilter,
		InboundAntispamAction:      cfg.inboundAntispamAction,
		DMARCEnabled:               cfg.dmarcEnabled,
		SendLogRetentionEnvDefault: cfg.retentionDays,

		InboundSpamLogRetentionEnvDefault: cfg.inboundSpamLogRetentionDays,
	}, cfg.setupTokenPath)
}

// newHTTPServer bounds what a client may hold the panel's server for (audit
// 2026-10-12, L14): the request with its body - a form, or a domain export
// of 1 MiB at most - in 30 s; the answer in 60 s, the slowest page being one
// that asks DNS; an idle kept-alive connection 2 min. The full backup moves
// its own write deadline to two hours: it streams for as long as the archive
// takes (handlers.HandleBackup). net/http, Go 1.26: ReadTimeout covers the
// header and the body, WriteTimeout runs from the end of the header's read
// to the end of the response's write, IdleTimeout is the wait for the next
// request on a kept-alive connection.
func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
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

	srv := newHTTPServer(cfg.httpAddr, srvApp.Handler())

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
