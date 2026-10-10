package store

import (
	"errors"
	"testing"
)

// A domain has one report address and nothing else: a fresh domain has none,
// the address is whatever was set last, and "" is no reports.
func TestDomainDMARCAddress(t *testing.T) {
	st := openTestStore(t)
	d, err := st.AddDomain("example.com", "sel")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	got, err := st.GetDomain(d.ID)
	if err != nil {
		t.Fatalf("GetDomain: %v", err)
	}
	if got.DMARCRua != "" {
		t.Fatalf("a new domain reports to %q, want no reports", got.DMARCRua)
	}

	for _, rua := range []string{"reports@hub.com", "dmarc+example.com@mail.example.org", ""} {
		if err := st.SetDomainDMARCAddress(d.ID, rua); err != nil {
			t.Fatalf("SetDomainDMARCAddress(%q): %v", rua, err)
		}
		if got, _ = st.GetDomain(d.ID); got.DMARCRua != rua {
			t.Fatalf("set %q, read %q", rua, got.DMARCRua)
		}
	}

	if err := st.SetDomainDMARCAddress(d.ID+100, "x@y.com"); !errors.Is(err, ErrDomainNotFound) {
		t.Errorf("unknown domain = %v, want ErrDomainNotFound", err)
	}
}

// Deleting a user changes no domain's report address: the address belongs to
// the domain, not to whoever set it.
func TestDeletingAUserLeavesEveryDomainAddressAlone(t *testing.T) {
	st := openTestStore(t)
	if err := st.CreateGlobalUser("admin", "hash"); err != nil {
		t.Fatalf("CreateGlobalUser: %v", err)
	}
	id, err := st.CreateUser("second", "hash", RoleGlobal, Reach{})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := st.UpdateUser(id, "second", "hash", "second@hub.example"); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	typed, _ := st.AddDomain("typed.example", "sel")
	none, _ := st.AddDomain("none.example", "sel")
	if err := st.SetDomainDMARCAddress(typed.ID, "second@hub.example"); err != nil {
		t.Fatalf("SetDomainDMARCAddress: %v", err)
	}
	if err := st.DeleteUser(id); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if got, _ := st.GetDomain(typed.ID); got.DMARCRua != "second@hub.example" {
		t.Errorf("typed address after deleting the user = %q", got.DMARCRua)
	}
	if got, _ := st.GetDomain(none.ID); got.DMARCRua != "" {
		t.Errorf("no-reports domain after deleting the user = %q", got.DMARCRua)
	}
}
