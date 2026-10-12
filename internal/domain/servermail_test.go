package domain

import (
	"os"
	"strings"
	"testing"
)

func readTables(t *testing.T, odk *OpenDKIM) (keyTable, signingTable string) {
	t.Helper()
	kt, err := os.ReadFile(odk.keyTablePath)
	if err != nil {
		t.Fatalf("read KeyTable: %v", err)
	}
	st, err := os.ReadFile(odk.signingTablePath)
	if err != nil {
		t.Fatalf("read SigningTable: %v", err)
	}
	return string(kt), string(st)
}

// The server's own name is signed like a sending domain - a key, a line in
// each table - without being one: no row in the registry, so no application
// can send as it.
func TestServerNameIsSignedWithoutBeingADomain(t *testing.T) {
	svc, odk := newTestService(t, &fakeApps{})
	svc.SetServerName("mail.example.org")

	if _, err := svc.Add("example.com"); err != nil {
		t.Fatalf("add domain: %v", err)
	}
	kt, st := readTables(t, odk)
	for _, want := range []string{"mail.example.org mail.example.org:selfpost:", "example.com example.com:selfpost:"} {
		if !strings.Contains(kt, want) {
			t.Errorf("KeyTable lacks %q:\n%s", want, kt)
		}
	}
	if !strings.Contains(st, "*@mail.example.org mail.example.org\n") {
		t.Errorf("SigningTable does not sign the server's own name:\n%s", st)
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
	svc, odk := newTestService(t, &fakeApps{})
	svc.SetServerName("example.com")

	d, err := svc.Add("example.com")
	if err != nil {
		t.Fatalf("add domain: %v", err)
	}
	_, st := readTables(t, odk)
	if strings.Count(st, "*@example.com ") != 1 {
		t.Errorf("the name is in the SigningTable more than once:\n%s", st)
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
	_, st = readTables(t, odk)
	if !strings.Contains(st, "*@example.com example.com\n") {
		t.Errorf("deleting the domain stopped the server's own mail being signed:\n%s", st)
	}
	after, err := svc.ServerMail()
	if err != nil || after.Record.Value != asDomain.Value {
		t.Errorf("deleting the domain changed the server's key (%v)", err)
	}
}

// An installation upgraded to a version that signs the server's own name has
// tables without its line until something rebuilds them. The start does, once.
func TestEnsureServerSignedRebuildsOnce(t *testing.T) {
	svc, odk := newTestService(t, &fakeApps{})
	if _, err := svc.Add("example.com"); err != nil {
		t.Fatal(err)
	}
	reloads := 0
	odk.reload = func() error { reloads++; return nil }

	// The tables as the earlier version left them; now the name is known.
	svc.SetServerName("mail.example.org")
	if rebuilt, err := svc.EnsureServerSigned(); err != nil || !rebuilt {
		t.Fatalf("first start: rebuilt = %v, %v; want a rebuild", rebuilt, err)
	}
	if _, st := readTables(t, odk); !strings.Contains(st, "*@mail.example.org mail.example.org\n") || !strings.Contains(st, "*@example.com ") {
		t.Errorf("SigningTable after the rebuild:\n%s", st)
	}
	if rebuilt, err := svc.EnsureServerSigned(); err != nil || rebuilt {
		t.Errorf("second start: rebuilt = %v, %v; want nothing to do", rebuilt, err)
	}
	if reloads != 1 {
		t.Errorf("OpenDKIM was reloaded %d times, want once", reloads)
	}

	// A name that only begins like the server's is not it.
	other, otherOdk := newTestService(t, &fakeApps{})
	other.SetServerName("mail.example.org.evil.example")
	if _, err := other.EnsureServerSigned(); err != nil {
		t.Fatal(err)
	}
	other.SetServerName("mail.example.org")
	if signed, err := otherOdk.signs("mail.example.org", "selfpost"); err != nil || signed {
		t.Errorf("a longer name was taken for the server's: %v, %v", signed, err)
	}
}

// Tables that sign the server's name with the key of another selector - the
// default selector was changed - are rebuilt: OpenDKIM must sign with the key
// whose record the panel shows.
func TestEnsureServerSignedFollowsTheSelector(t *testing.T) {
	svc, odk := newTestService(t, &fakeApps{})
	svc.SetServerName("mail.example.org")
	if _, err := svc.EnsureServerSigned(); err != nil {
		t.Fatal(err)
	}

	svc.selector = "sp2027"
	if rebuilt, err := svc.EnsureServerSigned(); err != nil || !rebuilt {
		t.Fatalf("after the selector changed: rebuilt = %v, %v; want a rebuild", rebuilt, err)
	}
	kt, _ := readTables(t, odk)
	if !strings.Contains(kt, "mail.example.org mail.example.org:sp2027:") || strings.Contains(kt, ":selfpost:") {
		t.Errorf("KeyTable after the rebuild:\n%s", kt)
	}
	mail, err := svc.ServerMail()
	if err != nil || mail.Selector != "sp2027" {
		t.Errorf("the panel shows selector %q (%v), the tables sign with sp2027", mail.Selector, err)
	}
}

// Without a hostname nothing is added and nothing is signed for it.
func TestNoServerName(t *testing.T) {
	svc, odk := newTestService(t, &fakeApps{})
	if _, err := svc.Add("example.com"); err != nil {
		t.Fatal(err)
	}
	if _, st := readTables(t, odk); strings.Count(st, "\n") != 1 {
		t.Errorf("SigningTable has more than the one domain:\n%s", st)
	}
	if _, err := svc.ServerMail(); err == nil {
		t.Error("ServerMail without a hostname returned no error")
	}
}
