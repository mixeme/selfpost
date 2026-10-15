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

// The signer rewrites the table at every start: the first start of 2.8.0
// finds none (OpenDKIM's tables were the earlier versions'), and a rewrite
// cut short leaves a domain without its line while the server's is there.
func TestWriteSelectorsAtTheStart(t *testing.T) {
	svc, keys := newTestService(t, &fakeApps{})
	if _, err := svc.Add("example.com"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(keys.selectorsPath); err != nil {
		t.Fatal(err)
	}

	svc.SetServerName("mail.example.org")
	if err := svc.WriteSelectors(); err != nil {
		t.Fatalf("first start: %v", err)
	}
	want := "example.com selfpost" + "\n" + "mail.example.org selfpost" + "\n"
	if got := readSelectors(t, keys); got != want {
		t.Errorf("selector table after the first start = %q, want %q", got, want)
	}

	// A domain's line lost (a rewrite cut short): the next start puts it back.
	if err := os.WriteFile(keys.selectorsPath, []byte("mail.example.org selfpost"+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := svc.WriteSelectors(); err != nil {
		t.Fatal(err)
	}
	if got := readSelectors(t, keys); got != want {
		t.Errorf("selector table after the next start = %q, want %q", got, want)
	}

	// A name that only begins like the server's is not it.
	other, otherKeys := newTestService(t, &fakeApps{})
	other.SetServerName("mail.example.org.evil.example")
	if err := other.WriteSelectors(); err != nil {
		t.Fatal(err)
	}
	if got := readSelectors(t, otherKeys); got != "mail.example.org.evil.example selfpost"+"\n" {
		t.Errorf("selector table = %q", got)
	}
}

// A changed default selector: the server's name is signed with the key of the
// new one, whose record the panel shows.
func TestWriteSelectorsFollowsTheSelector(t *testing.T) {
	svc, keys := newTestService(t, &fakeApps{})
	svc.SetServerName("mail.example.org")
	if err := svc.WriteSelectors(); err != nil {
		t.Fatal(err)
	}

	svc.selector = "sp2027"
	if err := svc.WriteSelectors(); err != nil {
		t.Fatal(err)
	}
	if got := readSelectors(t, keys); got != "mail.example.org sp2027"+"\n" {
		t.Errorf("selector table after the change = %q", got)
	}
	mail, err := svc.ServerMail()
	if err != nil || mail.Selector != "sp2027" {
		t.Errorf("the panel shows selector %q (%v), the table signs with sp2027", mail.Selector, err)
	}
}

// A hostname written with capitals is the server's name in lower case: the
// signer looks a key up by the name in lower case, and a key written under the
// capitals would never be found.
func TestServerNameInCapitals(t *testing.T) {
	svc, keys := newTestService(t, &fakeApps{})
	svc.SetServerName("Mail.Example.ORG")
	if err := svc.WriteSelectors(); err != nil {
		t.Fatal(err)
	}
	if got := readSelectors(t, keys); got != "mail.example.org selfpost"+"\n" {
		t.Errorf("selector table = %q", got)
	}
	if _, err := os.Stat(KeyPath(keys.keysDir, "mail.example.org", "selfpost")); err != nil {
		t.Errorf("the key is not under the name in lower case: %v", err)
	}
	if mail, err := svc.ServerMail(); err != nil || mail.Name != "mail.example.org" {
		t.Errorf("server mail = %+v, %v", mail, err)
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
