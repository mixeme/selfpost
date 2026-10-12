package main

import (
	"context"
	"log"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/notify"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

// runNotifier writes to the panel's users when something needs them
// (architecture.md § Notifications), until ctx is cancelled. It reads what the
// pages read - a domain's published records against the key this server signs
// with, the certificate file, Postfix's queue - with a DNS checker of its own,
// so that its hourly look is never a page's cached one.
func runNotifier(ctx context.Context, cfg config, st *store.Store) error {
	ms := newMailStack(cfg, st)
	dns := dnscheck.New(cfg.dnsResolvers)

	// OpenDKIM's tables are rebuilt when a domain is added or deleted. On the
	// first start of a version that signs the server's own name they do not
	// have its line yet. A failure here costs the signature, not the message.
	if rebuilt, err := ms.Domains.EnsureServerSigned(); err != nil {
		log.Printf("notifier: sign as %s: %v", cfg.hostname, err)
	} else if rebuilt {
		log.Printf("notifier: OpenDKIM now signs mail of %s", cfg.hostname)
	}

	n := notify.New(st, notify.Checks{
		Domain: func(d store.Domain) (dnscheck.Domain, error) {
			rec, err := ms.Domains.DKIMRecord(d)
			if err != nil {
				return dnscheck.Domain{}, err
			}
			// The server's own addresses are cached for a minute: one lookup
			// for a pass over all the domains.
			srv := dns.Server(cfg.hostname, false)
			return dns.Domain(dnscheck.Query{
				Name:             d.Name,
				Selector:         d.DKIMSelector,
				ExpectedDKIM:     rec.Value,
				Hostname:         srv.Hostname,
				ServerIPs:        srv.IPs,
				DMARCReportEmail: d.DMARCRua,
			}, true), nil
		},
		Certificate: func() health.Certificate { return health.CheckCertificate(cfg.tlsCertFile) },
		Queue:       postfix.QueueMessages,
	}, notify.NewMailer(cfg.hostname), cfg.hostname)
	return n.Run(ctx)
}
