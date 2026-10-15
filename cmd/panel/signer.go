package main

import (
	"context"
	"log"
	"path/filepath"
	"strings"

	"github.com/mixeme/selfpost/internal/milter"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

// serveSigner runs the DKIM signer (internal/milter/sign.go) on its two
// sockets: signSocket for what applications send on 465/587, signed with the
// key of the domain the login belongs to - the sender_login_maps Postfix and
// the sender check read (Bindings.DomainOf); signLocalSocket for what the
// panel itself submits with sendmail(1), signed with the key of the server's
// name. Postfix attaches both strictly (default_action=tempfail,
// build/postfix-config.sh), so while this role is not serving, mail waits.
//
// The keys and selectors come from the key tree and the selector table under
// dkimDir (internal/domain, Keys). Before it serves, the table is made to
// hold the server's own name - and written at all on the first start of
// 2.8.0, whose table replaced OpenDKIM's: Postfix waits for these sockets
// (build/postfix-wrapper.sh), so no message meets the table before it.
func serveSigner(ctx context.Context, cfg config, st *store.Store) error {
	if rebuilt, err := newMailStack(cfg, st).Domains.EnsureServerSigned(); err != nil {
		// Mail waits: what the signer cannot find a key for it defers.
		log.Printf("dkim-signer: the selector table: %v", err)
	} else if rebuilt {
		log.Printf("dkim-signer: the selector table was rewritten from the registry")
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
