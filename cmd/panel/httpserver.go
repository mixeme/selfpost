package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"codeberg.org/mix/selfpost/internal/app"
	"codeberg.org/mix/selfpost/internal/domain"
	"codeberg.org/mix/selfpost/internal/postfix"
	"codeberg.org/mix/selfpost/internal/store"
	"codeberg.org/mix/selfpost/internal/web"
)

// serveHTTP opens the panel database and runs the control-panel HTTP server
// until ctx is cancelled. From Phase 2 this serves the real setup, login and
// authenticated panel surface (spec 7.6).
func serveHTTP(ctx context.Context, cfg config) error {
	st, err := store.Open(cfg.dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	// Applications own the SASL accounts and the Postfix sender map; the domain
	// service delegates to them when a domain (and its applications) is deleted.
	pf := postfix.New(cfg.postfixDir)
	apps := app.NewService(st, app.NewSASLDB(cfg.saslDBPath, cfg.saslRealm), pf)
	domains := domain.NewService(st, domain.NewOpenDKIM(cfg.opendkimDir), apps, cfg.dkimSelectorDef)

	srvApp, err := web.New(st, domains, apps, web.Config{
		Hostname:     cfg.hostname,
		CookieSecure: cfg.cookieSecure,
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
