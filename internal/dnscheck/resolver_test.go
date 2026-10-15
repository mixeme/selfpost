package dnscheck

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestWithDefaultPort(t *testing.T) {
	cases := map[string]string{
		"1.1.1.1":          "1.1.1.1:53",
		"1.1.1.1:5353":     "1.1.1.1:5353",
		"dns.example.com":  "dns.example.com:53",
		"2606:4700:4700::": "[2606:4700:4700::]:53",
		"[::1]:5353":       "[::1]:5353",
	}
	for in, want := range cases {
		if got := withDefaultPort(in); got != want {
			t.Errorf("withDefaultPort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseResolvers(t *testing.T) {
	got := ParseResolvers(" 1.1.1.1 , ,8.8.8.8:53,")
	want := []string{"1.1.1.1", "8.8.8.8:53"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
	if r := ParseResolvers(""); len(r) != 0 {
		t.Errorf("empty string parsed to %q, want nothing", r)
	}
}

func TestNewExternalResolverFallsBackToDefaults(t *testing.T) {
	if n := len(newExternalResolver(nil).servers); n != len(DefaultResolvers) {
		t.Errorf("resolver count = %d, want %d (the defaults)", n, len(DefaultResolvers))
	}
}

// The checks must reach the second resolver when the first is unreachable, but
// must not second-guess an authoritative "no such name" — otherwise a domain
// that genuinely lacks a record costs one timeout per configured resolver.
func TestQueryEachTriesTheNextResolverOnlyOnFailure(t *testing.T) {
	e := &externalResolver{servers: []*net.Resolver{{}, {}, {}}, timeout: time.Second}

	asked := 0
	got, err := queryEach(context.Background(), e, func(context.Context, int) (string, error) {
		asked++
		if asked < 3 {
			return "", &net.DNSError{Err: "timed out", IsTimeout: true}
		}
		return "answer", nil
	})
	if err != nil || got != "answer" {
		t.Fatalf("got (%q, %v), want (\"answer\", nil)", got, err)
	}
	if asked != 3 {
		t.Errorf("asked %d resolvers, want 3", asked)
	}

	asked = 0
	_, err = queryEach(context.Background(), e, func(context.Context, int) (string, error) {
		asked++
		return "", notFound("absent.example")
	})
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
		t.Fatalf("err = %v, want a not-found DNSError", err)
	}
	if asked != 1 {
		t.Errorf("NXDOMAIN asked %d resolvers, want 1 — it is an answer, not a failure", asked)
	}
}

// A first resolver that drops packets costs its own time, not the check's:
// the second is asked with time left, and answers; and the next question goes
// to the one that answered (audit 2026-10-12, M14).
func TestADroppingResolverLeavesTheNextItsTime(t *testing.T) {
	e := &externalResolver{servers: []*net.Resolver{{}, {}, {}}, timeout: 50 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var order []int
	drops := func(ctx context.Context, i int) (string, error) {
		order = append(order, i)
		if i == 0 {
			<-ctx.Done() // a resolver that never answers
			return "", &net.DNSError{Err: "i/o timeout", IsTimeout: true}
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "answer", nil
	}
	start := time.Now()
	got, err := queryEach(ctx, e, drops)
	if err != nil || got != "answer" {
		t.Fatalf("got (%q, %v): the second resolver was not given its time", got, err)
	}
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Errorf("the dropping resolver took %v of the check's second", took)
	}
	order = nil
	if _, err := queryEach(ctx, e, drops); err != nil || len(order) != 1 || order[0] != 1 {
		t.Errorf("the next question went to %v (%v), want the resolver that answered", order, err)
	}
}

// The reason this package does not use net.DefaultResolver: systemd-resolved
// answers PTR queries for the machine's own addresses out of the local
// hostname, which is not what the rest of the internet sees. The dial hook must
// therefore ignore the address the standard resolver picked from
// /etc/resolv.conf and connect to the configured one.
func TestExternalResolverDialsOnlyTheConfiguredAddress(t *testing.T) {
	e := newExternalResolver([]string{"192.0.2.53"})
	if len(e.servers) != 1 {
		t.Fatalf("servers = %d, want 1", len(e.servers))
	}
	conn, err := e.servers[0].Dial(context.Background(), "udp", "127.0.0.53:53")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if got := conn.RemoteAddr().String(); got != "192.0.2.53:53" {
		t.Errorf("connected to %q, want 192.0.2.53:53 (the system resolver won)", got)
	}
}
