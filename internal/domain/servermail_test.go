package domain

import (
	"os"
	"strings"
	"testing"
)

func readSelectors(t *testing.T, keys *Keys) string {
	t.Helper()
	data, err := os.ReadFile(keys.selectorsPath)
	if err != nil {
		t.Fatalf("read the selector table: %v", err)
	}
	return string(data)
}

// The server's own name is signed like a sending domain - a key, a line in
// the selector table - without being one: no row in the registry, so no
// application can send as it.
func TestServerNameIsSignedWithoutBeingADomain(t *testing.T) {
	svc, keys := newTestService(t, &fakeApps{})
	svc.SetServerName("mail.example.org")

	if _, err := svc.Add("example.com"); err != nil {
		t.Fatalf("add domain: %v", err)
	}
	if got, want := readSelectors(t, keys), "example.com selfpost\nmail.example.org selfpost\n"; got != want {
		t.Errorf("selector table =\n%q\nwant\n%q", got, want)
	}
	domains, err := svc.List()
	if err != nil || len(domains) != 1 {
		t.Fatalf("registry holds %d domains, %v; want the one that was added", len(domains), err)
	}

	mail, err := svc.ServerMail()
	if err != nil {
		t.Fatalf("ServerMail: %v", err)
	}
	if mail.Name != "mail.example.org" || mail.Selector != "selfpost" ||
		mail.Record.Name != "selfpost._domainkey.mail.example.org" || !strings.HasPrefix(mail.Record.Value, "v=DKIM1;") {
		t.Errorf("server mail = %+v", mail)
	}
	// Asked again, it is the same key: the record published in DNS stays valid.
	again, err := svc.ServerMail()
	if err != nil || again.Record.Value != mail.Record.Value {
		t.Errorf("the server's key changed between two readings (%v)", err)
	}
}

// When the hostname is itself a sending domain, there is one key and one line,
// the domain's; deleting the domain leaves the server's mail signed with it.
func TestServerNameThatIsAlsoADomain(t *testing.T) {
	svc, keys := newTestService(t, &fakeApps{})
	svc.SetServerName("example.com")

	d, err := svc.Add("example.com")
	if err != nil {
		t.Fatalf("add domain: %v", err)
	}
	if got := readSelectors(t, keys); got != "example.com selfpost\n" {
		t.Errorf("selector table = %q, want the name once", got)
	}
	asDomain, err := svc.DKIMRecord(d)
	if err != nil {
		t.Fatal(err)
	}
	mail, err := svc.ServerMail()
	if err != nil || mail.Record.Value != asDomain.Value {
		t.Fatalf("the server's record is not the domain's (%v)", err)
	}

	if err := svc.Delete(d.ID); err != nil {
		t.Fatalf("delete domain: %v", err)
	}
	if got := readSelectors(t, keys); got != "example.com selfpost\n" {
		t.Errorf("deleting the domain stopped the server's own mail being signed: %q", got)
	}
	after, err := svc.ServerMail()
	if err != nil || after.Record.Value != asDomain.Value {
		t.Errorf("deleting the domain changed the server's key (%v)", err)
	}
}

// The first start of 2.8.0 finds no selector table - OpenDKIM's tables were
// the earlier versions' - and an installation may lack the server's line.
// The start rebuilds, once.
func TestEnsureServerSignedRebuildsOnce(t *testing.T) {
	svc, keys := newTestService(t, &fakeApps{})
	if _, err := svc.Add("example.com"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(keys.selectorsPath); err != nil {
		t.Fatal(err)
	}

	svc.SetServerName("mail.example.org")
	if rebuilt, err := svc.EnsureServerSigned(); err != nil || !rebuilt {
		t.Fatalf("first start: rebuilt = %v, %v; want a rebuild", rebuilt, err)
	}
	if got, want := readSelectors(t, keys), "example.com selfpost\nmail.example.org selfpost\n"; got != want {
		t.Errorf("selector table after the rebuild = %q, want %q", got, want)
	}
	if rebuilt, err := svc.EnsureServerSigned(); err != nil || rebuilt {
		t.Errorf("second start: rebuilt = %v, %v; want nothing to do", rebuilt, err)
	}

	// A name that only begins like the server's is not it.
	other, otherKeys := newTestService(t, &fakeApps{})
	other.SetServerName("mail.example.org.evil.example")
	if _, err := other.EnsureServerSigned(); err != nil {
		t.Fatal(err)
	}
	if signed, err := otherKeys.signs("mail.example.org", "selfpost"); err != nil || signed {
		t.Errorf("a longer name was taken for the server's: %v, %v", signed, err)
	}
}

// A table that signs the server's name with the key of another selector - the
// default selector was changed - is rebuilt: the signer must sign with the key
// whose record the panel shows.
func TestEnsureServerSignedFollowsTheSelector(t *testing.T) {
	svc, keys := newTestService(t, &fakeApps{})
	svc.SetServerName("mail.example.org")
	if _, err := svc.EnsureServerSigned(); err != nil {
		t.Fatal(err)
	}

	svc.selector = "sp2027"
	if rebuilt, err := svc.EnsureServerSigned(); err != nil || !rebuilt {
		t.Fatalf("after the selector changed: rebuilt = %v, %v; want a rebuild", rebuilt, err)
	}
	if got := readSelectors(t, keys); got != "mail.example.org sp2027\n" {
		t.Errorf("selector table after the rebuild = %q", got)
	}
	mail, err := svc.ServerMail()
	if err != nil || mail.Selector != "sp2027" {
		t.Errorf("the panel shows selector %q (%v), the table signs with sp2027", mail.Selector, err)
	}
}

// Without a hostname nothing is added and nothing is signed for it.
func TestNoServerName(t *testing.T) {
	svc, keys := newTestService(t, &fakeApps{})
	if _, err := svc.Add("example.com"); err != nil {
		t.Fatal(err)
	}
	if got := readSelectors(t, keys); got != "example.com selfpost\n" {
		t.Errorf("selector table has more than the one domain: %q", got)
	}
	if _, err := svc.ServerMail(); err == nil {
		t.Error("ServerMail without a hostname returned no error")
	}
}
