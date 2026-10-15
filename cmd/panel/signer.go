package main

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/milter"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

// rewriteRetry is how often the signer tries again to write the selector
// table when the write at its start failed.
const rewriteRetry = 10 * time.Second

// serveSigner runs the DKIM signer (internal/milter/sign.go) on its two
// sockets: signSocket for what applications send on 465/587, signed with the
// key of the domain the login belongs to - the sender_login_maps Postfix and
// the sender check read (Bindings.DomainOf); signLocalSocket for what the
// panel itself submits with sendmail(1), signed with the key of the server's
// name. Postfix attaches both strictly (default_action=tempfail,
// build/postfix-config.sh), so while this role is not serving, mail waits.
//
// The keys and selectors come from the key tree and the selector table under
// dkimDir (internal/domain, Keys). Before it serves, the table is rewritten
// from the registry (WriteSelectors): Postfix waits for these sockets
// (build/postfix-wrapper.sh), so no message meets a table from before. A
// rewrite that fails is tried again every rewriteRetry until it succeeds;
// meanwhile what the signer finds no key for it defers, and the panel's
// pages - Reload configuration among them - stay up.
func serveSigner(ctx context.Context, cfg config, st *store.Store) error {
	domains := newMailStack(cfg, st).Domains
	if err := domains.WriteSelectors(); err != nil {
		log.Printf("dkim-signer: the selector table: %v - trying again every %s", err, rewriteRetry)
		go func() {
			tick := time.NewTicker(rewriteRetry)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					if err := domains.WriteSelectors(); err != nil {
						log.Printf("dkim-signer: the selector table: %v", err)
						continue
					}
					log.Printf("dkim-signer: the selector table is written")
					return
				}
			}
		}()
	}
	keys := milter.NewSignKeys(filepath.Join(cfg.dkimDir, "selectors"), filepath.Join(cfg.dkimDir, "keys"))
	bind := milter.NewBindings(postfix.New(cfg.postfixDir).SenderLoginMapsPath())
	server := strings.ToLower(cfg.hostname)

	apps, err := listenMilter(cfg.signSocket)
	if err != nil {
		return err
	}
	local, err := listenMilter(cfg.signLocalSocket)
	if err != nil {
		apps.Close()
		return err
	}
	log.Printf("dkim-signer listening on %s (applications) and %s (the panel's own mail, as %s)", cfg.signSocket, cfg.signLocalSocket, server)

	errc := make(chan error, 2)
	go func() { errc <- milter.ServeSign(ctx, apps, keys, bind.DomainOf) }()
	go func() {
		errc <- milter.ServeSign(ctx, local, keys, func(string) string { return server })
	}()
	// The first to stop decides: an error stops the panel (run()), and on a
	// cancelled ctx both return nil.
	if err := <-errc; err != nil {
		return err
	}
	return <-errc
}
