// Package dnscheck performs the read-only DNS lookups behind the panel's
// deliverability checks: forward-confirmed reverse DNS (FCrDNS) for the
// server's own hostname, and the DKIM/SPF/DMARC records published for each
// sending domain.
//
// Every lookup is bounded by a timeout and results are cached, because DNS is
// the one part of the status page that talks to the network: a slow or dead
// resolver must degrade a single card to "could not check", never hang the
// page. Nothing here changes state — the panel only reports what the world can
// see about this server.
package dnscheck

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"codeberg.org/mix/selfpost/internal/health"
)

const (
	// lookupTimeout bounds all the lookups of a single check together, so a
	// dead resolver costs one wait and not one per record type.
	lookupTimeout = 5 * time.Second
	// serverTTL/domainTTL are how long a cached result stays fresh. The
	// server's own hostname/PTR is cheap and rarely changes; a domain's
	// records are three lookups, and the operator has just published them, so
	// a few minutes plus an explicit Re-check button is the right trade.
	serverTTL = time.Minute
	domainTTL = 5 * time.Minute
)

// Result is the outcome of one published-record check.
type Result struct {
	Status health.Status
	// Detail is a full sentence for the operator: what was found and, when
	// something is wrong, what to do about it.
	Detail string
	// Records is what was actually found in DNS, shown verbatim so the
	// operator can compare it with what they published.
	Records []string
}

// Server is the state of the server's own name in DNS: the addresses
// SELFPOST_HOSTNAME resolves to, and whether their PTR records point back at
// it. Receiving servers weigh this heavily, so a mismatch is an error.
type Server struct {
	Hostname  string
	IPs       []string // forward-resolved addresses, reused for the SPF check
	PTR       Result
	CheckedAt time.Time
}

// Domain is the published-DNS state of one sending domain.
type Domain struct {
	Name      string
	DKIM      Result
	SPF       Result
	DMARC     Result
	Overall   health.Status
	CheckedAt time.Time
}

// Query describes the domain to check. ExpectedDKIM is the TXT value the panel
// tells the operator to publish (domain.DKIMRecord.Value), so the check
// compares DNS against the key this server actually signs with. Hostname and
// ServerIPs identify this server and come from a preceding Server check.
type Query struct {
	Name         string
	Selector     string
	ExpectedDKIM string
	Hostname     string
	ServerIPs    []string
}

// resolver is the slice of *net.Resolver this package uses, as an interface so
// tests can drive the checks without touching the network.
type resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
	LookupAddr(ctx context.Context, addr string) ([]string, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
}

// Checker runs the checks and caches their results. The zero value is not
// usable; call New.
type Checker struct {
	resolver  resolver
	timeout   time.Duration
	serverTTL time.Duration
	domainTTL time.Duration

	mu      sync.Mutex
	servers map[string]cached[Server]
	domains map[string]cached[Domain]
}

type cached[T any] struct {
	value   T
	expires time.Time
}

// New returns a Checker using the process resolver and the package's default
// timeout and cache lifetimes.
func New() *Checker {
	return newChecker(net.DefaultResolver, lookupTimeout, serverTTL, domainTTL)
}

func newChecker(r resolver, timeout, srvTTL, domTTL time.Duration) *Checker {
	return &Checker{
		resolver:  r,
		timeout:   timeout,
		serverTTL: srvTTL,
		domainTTL: domTTL,
		servers:   make(map[string]cached[Server]),
		domains:   make(map[string]cached[Domain]),
	}
}

// Server checks the server's own hostname. force skips the cache, for the
// Re-check button.
func (c *Checker) Server(hostname string, force bool) Server {
	if !force {
		c.mu.Lock()
		entry, ok := c.servers[hostname]
		c.mu.Unlock()
		if ok && time.Now().Before(entry.expires) {
			return entry.value
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	srv := c.checkServer(ctx, hostname)

	c.mu.Lock()
	c.servers[hostname] = cached[Server]{value: srv, expires: srv.CheckedAt.Add(c.serverTTL)}
	c.mu.Unlock()
	return srv
}

// Domain checks one sending domain's published records. force skips the cache,
// for the Re-check button on the domain page.
func (c *Checker) Domain(q Query, force bool) Domain {
	if !force {
		c.mu.Lock()
		entry, ok := c.domains[q.Name]
		c.mu.Unlock()
		if ok && time.Now().Before(entry.expires) {
			return entry.value
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	d := c.checkDomain(ctx, q)

	c.mu.Lock()
	c.domains[q.Name] = cached[Domain]{value: d, expires: d.CheckedAt.Add(c.domainTTL)}
	c.mu.Unlock()
	return d
}

// Forget drops a domain's cached result, so the next page view re-checks it.
// Used when a domain is removed or re-imported.
func (c *Checker) Forget(domainName string) {
	c.mu.Lock()
	delete(c.domains, domainName)
	c.mu.Unlock()
}

// checkDomain runs the three record checks concurrently: they are independent,
// and in series three timeouts would stack up into a page that looks hung.
func (c *Checker) checkDomain(ctx context.Context, q Query) Domain {
	d := Domain{Name: q.Name, CheckedAt: time.Now()}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); d.DKIM = c.checkDKIM(ctx, q) }()
	go func() { defer wg.Done(); d.SPF = c.checkSPF(ctx, q) }()
	go func() { defer wg.Done(); d.DMARC = c.checkDMARC(ctx, q.Name) }()
	wg.Wait()
	d.Overall = health.Worst(d.DKIM.Status, d.SPF.Status, d.DMARC.Status)
	return d
}

// lookupTXT wraps the resolver's TXT lookup, separating "the name does not
// exist / has no TXT records" (a finding to report) from "the lookup failed"
// (nothing was learned).
func (c *Checker) lookupTXT(ctx context.Context, name string) (records []string, found bool, err error) {
	txt, err := c.resolver.LookupTXT(ctx, name)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	if len(txt) == 0 {
		return nil, false, nil
	}
	return txt, true, nil
}

// lookupFailed is the shared shape for "the resolver did not answer": unknown,
// not an accusation against the domain's configuration.
func lookupFailed(what string, err error) Result {
	return Result{
		Status: health.StatusUnknown,
		Detail: "Could not check " + what + ": the DNS lookup failed (" + dnsErrorText(err) + "). Try Re-check in a moment.",
	}
}

// dnsErrorText reduces a resolver error to its message, without the internals
// (Go wraps the name and server into the string form).
func dnsErrorText(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsTimeout {
			return "timed out"
		}
		return dnsErr.Err
	}
	return err.Error()
}

// normalizeName lowercases a DNS name and drops the root label, so a PTR answer
// ("mail.example.com.") compares equal to a configured hostname.
func normalizeName(name string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
}
