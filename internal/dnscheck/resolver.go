package dnscheck

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"
)

// DefaultResolvers are the recursive resolvers the checks query when the
// deployment does not name its own (SELFPOST_DNS_RESOLVERS). Three independent
// operators, so one being unreachable from the host does not blind the checks:
// each is given a time of its own to answer (perResolverTimeout), and the one
// that answered last is asked first (audit 2026-10-12, M14 - before, the first
// had the whole check's five seconds, and one that dropped packets left the
// others an expired context).
var DefaultResolvers = []string{"1.1.1.1:53", "8.8.8.8:53", "9.9.9.9:53"}

// perResolverTimeout is how long one resolver has to answer one question
// before the next is asked. A resolver that is there answers in tens of
// milliseconds; one that drops packets would otherwise take the check's whole
// lookupTimeout.
const perResolverTimeout = 2 * time.Second

// externalResolver talks to a fixed list of recursive resolvers directly
// instead of going through the system resolver.
//
// That detour is deliberate: these checks answer "what does a receiving mail
// server see about us?", and the machine's own stub resolver is the one place
// where the answer differs. systemd-resolved — which a Docker container reaches
// through the embedded 127.0.0.11 forwarder — synthesises a PTR record for the
// host's own addresses out of the local hostname, never asking public DNS. A
// server whose reverse DNS was published correctly therefore had its
// provider-assigned hostname reported back to the panel, and the FCrDNS check
// failed a record that was in fact right.
//
// This bypasses the resolvers in /etc/resolv.conf, not /etc/hosts: Go still
// consults the hosts file first, as any local program would.
type externalResolver struct {
	servers []*net.Resolver
	// timeout is perResolverTimeout; tests shorten it.
	timeout time.Duration

	mu sync.Mutex
	// first is the resolver that answered last, asked first next time.
	first int
}

// newExternalResolver builds a resolver over addrs ("host" or "host:port"). An
// empty list falls back to DefaultResolvers.
func newExternalResolver(addrs []string) *externalResolver {
	if len(addrs) == 0 {
		addrs = DefaultResolvers
	}
	e := &externalResolver{timeout: perResolverTimeout}
	for _, a := range addrs {
		addr := withDefaultPort(a)
		e.servers = append(e.servers, &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		})
	}
	return e
}

func (e *externalResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return queryEach(ctx, e, func(ctx context.Context, i int) ([]string, error) { return e.servers[i].LookupTXT(ctx, name) })
}

func (e *externalResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return queryEach(ctx, e, func(ctx context.Context, i int) ([]net.IPAddr, error) { return e.servers[i].LookupIPAddr(ctx, host) })
}

func (e *externalResolver) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	return queryEach(ctx, e, func(ctx context.Context, i int) ([]string, error) { return e.servers[i].LookupAddr(ctx, addr) })
}

func (e *externalResolver) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	return queryEach(ctx, e, func(ctx context.Context, i int) ([]*net.MX, error) { return e.servers[i].LookupMX(ctx, name) })
}

// queryEach asks the resolvers in turn, the one that answered last first, and
// stops at the first one that answers. "No such name" is an answer - only a
// resolver that cannot be reached, or that times out, moves the query on to
// the next one. Each is asked under a time of its own within ctx, so a
// resolver that drops packets costs e.timeout, not the check's whole budget.
func queryEach[T any](ctx context.Context, e *externalResolver, ask func(ctx context.Context, i int) (T, error)) (T, error) {
	var zero T
	n := len(e.servers)
	if n == 0 {
		return zero, errors.New("no DNS resolver configured")
	}
	e.mu.Lock()
	start := e.first
	e.mu.Unlock()
	answered := func(i int) {
		e.mu.Lock()
		e.first = i
		e.mu.Unlock()
	}
	var lastErr error
	for k := 0; k < n; k++ {
		i := (start + k) % n
		rctx, cancel := context.WithTimeout(ctx, e.timeout)
		v, err := ask(rctx, i)
		cancel()
		if err == nil {
			answered(i)
			return v, nil
		}
		if isNotFound(err) {
			answered(i)
			return zero, err
		}
		lastErr = err
		if ctx.Err() != nil {
			break // the check's own time is up
		}
	}
	return zero, lastErr
}

// isNotFound is an authoritative "no such name": an answer, not a failure.
func isNotFound(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) && dnsErr.IsNotFound
}

// withDefaultPort appends the DNS port to a bare address, so the environment
// variable can name a resolver as plainly as "1.1.1.1".
func withDefaultPort(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(addr, "53")
}

// ParseResolvers reads a comma-separated resolver list, as it arrives from the
// environment. Blank entries are skipped; an empty result means "use
// DefaultResolvers".
func ParseResolvers(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
