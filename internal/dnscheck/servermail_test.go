package dnscheck

import (
	"net"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/health"
)

const serverDKIM = "v=DKIM1; k=rsa; p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAserverkey"

func serverMailQuery() Query {
	return Query{
		Name:         "mail.example.org",
		Selector:     "sp",
		ExpectedDKIM: serverDKIM,
		Hostname:     "mail.example.org",
		ServerIPs:    []string{"203.0.113.25"},
	}
}

// The server's own name passes with its key published and "v=spf1 a -all":
// the name's address record is this server.
func TestServerMailPublished(t *testing.T) {
	f := &fakeResolver{
		txt: map[string][]string{
			"sp._domainkey.mail.example.org": {serverDKIM},
			"mail.example.org":               {ServerMailSPF},
		},
		addr: map[string][]net.IPAddr{"mail.example.org": ipAddrs("203.0.113.25")},
	}
	c := newTestChecker(f)

	m := c.ServerMail(serverMailQuery(), false)
	if m.DKIM.Status != health.StatusOK || m.SPF.Status != health.StatusOK || m.Overall != health.StatusOK {
		t.Fatalf("DKIM %s (%s), SPF %s (%s), overall %s", m.DKIM.Status, m.DKIM.Detail, m.SPF.Status, m.SPF.Detail, m.Overall)
	}

	// Cached like a domain, and re-read when forced.
	before := f.lookups
	c.ServerMail(serverMailQuery(), false)
	if f.lookups != before {
		t.Errorf("a second reading looked DNS up again (%d lookups)", f.lookups-before)
	}
	c.ServerMail(serverMailQuery(), true)
	if f.lookups == before {
		t.Error("a forced reading used the cache")
	}
}

// With nothing published both records are errors, and the SPF advice names the
// record for a server's own name, not the one for a sending domain.
func TestServerMailMissing(t *testing.T) {
	c := newTestChecker(&fakeResolver{})

	m := c.ServerMail(serverMailQuery(), false)
	if m.DKIM.Status != health.StatusError || m.SPF.Status != health.StatusError || m.Overall != health.StatusError {
		t.Fatalf("DKIM %s, SPF %s, overall %s; want errors", m.DKIM.Status, m.SPF.Status, m.Overall)
	}
	if !strings.Contains(m.SPF.Detail, ServerMailSPF) || strings.Contains(m.SPF.Detail, "a:mail.example.org") {
		t.Errorf("SPF advice: %s", m.SPF.Detail)
	}
}

// A sending domain of the same name does not share the cache entry.
func TestServerMailCacheIsItsOwn(t *testing.T) {
	f := &fakeResolver{
		txt: map[string][]string{
			"sp._domainkey.mail.example.org": {serverDKIM},
			"mail.example.org":               {ServerMailSPF},
		},
		addr: map[string][]net.IPAddr{"mail.example.org": ipAddrs("203.0.113.25")},
	}
	c := newTestChecker(f)
	c.Domain(serverMailQuery(), false)
	before := f.lookups
	c.ServerMail(serverMailQuery(), false)
	if f.lookups == before {
		t.Error("the server's mail check was answered from the domain's cache entry")
	}
}

// The address lookup behind an "a" mechanism did not answer: nothing was
// learned, and the record is not called wrong for it. A name that answers
// "no such host" is a finding, and the record is wrong.
func TestSPFLookupThatDidNotAnswerIsNotAMismatch(t *testing.T) {
	// The fake fails by name, so the mechanism names a host of its own: the
	// TXT lookup of the server's name answers, the address lookup does not.
	f := &fakeResolver{
		txt:  map[string][]string{"mail.example.org": {"v=spf1 a:addr.example.org -all"}},
		fail: map[string]bool{"addr.example.org": true},
	}
	c := newTestChecker(f)
	q := serverMailQuery()
	if got := c.ServerMail(q, true).SPF; got.Status != health.StatusWarn {
		t.Errorf("the lookup did not answer: SPF is %s (%s), want a warning", got.Status, got.Detail)
	}

	f.fail = nil
	if got := c.ServerMail(q, true).SPF; got.Status != health.StatusError {
		t.Errorf("the name does not exist: SPF is %s (%s), want an error", got.Status, got.Detail)
	}
}
