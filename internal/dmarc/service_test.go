package dmarc

import (
	"reflect"
	"testing"

	"github.com/mixeme/selfpost/internal/store"
)

// The ingest allow-list is the report address of each domain that points at
// this host, and nothing for the server as a whole: a new domain (no reports) and a domain
// reporting to another host open nothing of their own.
func TestAllowedRecipientsFollowTheDomainAddresses(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()
	hosted, _ := st.AddDomain("hosted.example", "sel")
	typed, _ := st.AddDomain("typed.example", "sel")
	if _, err := st.AddDomain("silent.example", "sel"); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	local, _ := st.AddDomain("local.example", "sel")
	if err := st.SetDomainDMARCAddress(hosted.ID, HostedReportAddress("mail.example.com", "hosted.example")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetDomainDMARCAddress(typed.ID, "reports@elsewhere.example"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetDomainDMARCAddress(local.ID, "ops@mail.example.com"); err != nil {
		t.Fatal(err)
	}

	// Another domain's hosted address, stored for this one: no mailbox
	// either (W1's second shape).
	stolen, _ := st.AddDomain("stolen.example", "sel")
	if err := st.SetDomainDMARCAddress(stolen.ID, HostedReportAddress("mail.example.com", "victim.example")); err != nil {
		t.Fatal(err)
	}

	got, err := NewService(st, nil, "mail.example.com", true).AllowedRecipients()
	if err != nil {
		t.Fatalf("AllowedRecipients: %v", err)
	}
	// Only the hosted address of the domain that chose it: an address typed
	// on the server's name (ops@) opens nothing (security audit 2026-10-17,
	// W1; this test asserted the opposite until then).
	want := []string{"dmarc-reports+hosted.example@mail.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("allowed recipients = %v, want %v", got, want)
	}
}
