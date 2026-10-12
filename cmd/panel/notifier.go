package main

import (
	"context"

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

	n := notify.New(st, notify.Checks{
		Domain: func(d store.Domain) (dnscheck.Domain, error) {
			rec, err := ms.Domains.DKIMRecord(d)
			if err != nil {
				return dnscheck.Domain{}, err
			}
			srv := dns.Server(cfg.hostname, true)
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
