package dnscheck

import (
	"context"
	"errors"
	"net"
	"strings"
)

// DefaultResolvers are the recursive resolvers the checks query when the
// deployment does not name its own (SELFPOST_DNS_RESOLVERS). Three independent
// operators, so one being unreachable from the host does not blind the checks.
var DefaultResolvers = []string{"1.1.1.1:53", "8.8.8.8:53", "9.9.9.9:53"}

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
}

// newExternalResolver builds a resolver over addrs ("host" or "host:port"). An
// empty list falls back to DefaultResolvers.
func newExternalResolver(addrs []string) *externalResolver {
	if len(addrs) == 0 {
		addrs = DefaultResolvers
	}
	e := &externalResolver{}
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
	return queryEach(e.servers, func(r *net.Resolver) ([]string, error) { return r.LookupTXT(ctx, name) })
}

func (e *externalResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return queryEach(e.servers, func(r *net.Resolver) ([]net.IPAddr, error) { return r.LookupIPAddr(ctx, host) })
}

func (e *externalResolver) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	return queryEach(e.servers, func(r *net.Resolver) ([]string, error) { return r.LookupAddr(ctx, addr) })
}

func (e *externalResolver) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	return queryEach(e.servers, func(r *net.Resolver) ([]*net.MX, error) { return r.LookupMX(ctx, name) })
}

// queryEach asks each resolver in turn and stops at the first one that
// answers. "No such name" is an answer — only a resolver that cannot be
// reached, or that times out, moves the query on to the next one.
func queryEach[T any](servers []*net.Resolver, ask func(*net.Resolver) (T, error)) (T, error) {
	var zero T
	var lastErr error
	for _, r := range servers {
		v, err := ask(r)
		if err == nil {
			return v, nil
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return zero, err
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no DNS resolver configured")
	}
	return zero, lastErr
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
