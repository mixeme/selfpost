package store

import (
	"errors"
	"testing"
)

const hostedForTest = "dmarc+example.com@mail.example.org"

// A domain either has a report address of its own or follows a user's default;
// the two never mix, and a fresh domain has neither.
func TestDomainDMARCAddressAndUser(t *testing.T) {
	st := openTestStore(t)
	d, err := st.AddDomain("example.com", "sel")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	got, err := st.GetDomain(d.ID)
	if err != nil {
		t.Fatalf("GetDomain: %v", err)
	}
	if got.DMARCRua != "" || got.DMARCRuaUserID.Valid {
		t.Fatalf("a new domain = %q / follows %v, want no reports", got.DMARCRua, got.DMARCRuaUserID)
	}

	if err := st.SetDomainDMARCAddress(d.ID, "reports@hub.com"); err != nil {
		t.Fatalf("SetDomainDMARCAddress: %v", err)
	}
	got, _ = st.GetDomain(d.ID)
	if rua, err := st.DomainDMARCRua(got, hostedForTest); err != nil || rua != "reports@hub.com" {
		t.Fatalf("own address resolves to %q, %v", rua, err)
	}

	if err := st.CreateGlobalUser("admin", "hash"); err != nil {
		t.Fatalf("CreateGlobalUser: %v", err)
	}
	u, _ := st.GetUserByUsername("admin")
	if err := st.SetDomainDMARCUser(d.ID, u.ID); err != nil {
		t.Fatalf("SetDomainDMARCUser: %v", err)
	}
	got, _ = st.GetDomain(d.ID)
	if got.DMARCRua != "" || !got.DMARCRuaUserID.Valid || got.DMARCRuaUserID.Int64 != u.ID {
		t.Fatalf("following a user left %q / %v; the own address must be dropped", got.DMARCRua, got.DMARCRuaUserID)
	}

	if err := st.SetDomainDMARCAddress(d.ID, ""); err != nil {
		t.Fatalf("SetDomainDMARCAddress none: %v", err)
	}
	got, _ = st.GetDomain(d.ID)
	if got.DMARCRua != "" || got.DMARCRuaUserID.Valid {
		t.Fatalf("none = %q / %v", got.DMARCRua, got.DMARCRuaUserID)
	}

	if err := st.SetDomainDMARCAddress(d.ID+100, "x@y.com"); !errors.Is(err, ErrDomainNotFound) {
		t.Fatalf("unknown domain = %v, want ErrDomainNotFound", err)
	}
}

// A domain that follows a user resolves to whatever that user's default says
// today — every mode — and changes with it.
func TestDomainFollowsUserDefault(t *testing.T) {
	st := openTestStore(t)
	if err := st.CreateGlobalUser("admin", "hash"); err != nil {
		t.Fatalf("CreateGlobalUser: %v", err)
	}
	u, _ := st.GetUserByUsername("admin")
	if err := st.UpdateUser(u.ID, "admin", "hash", "mix@example.org"); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	d, _ := st.AddDomain("example.com", "sel")
	if err := st.SetDomainDMARCUser(d.ID, u.ID); err != nil {
		t.Fatalf("SetDomainDMARCUser: %v", err)
	}
	d, _ = st.GetDomain(d.ID)

	for _, tc := range []struct {
		mode, address string
		hosted        string
		want          string
	}{
		{DMARCDefaultNone, "", hostedForTest, ""},
		{DMARCDefaultAccount, "", hostedForTest, "mix@example.org"},
		{DMARCDefaultCustom, "dmarc@hub.example", hostedForTest, "dmarc@hub.example"},
		{DMARCDefaultHosted, "", hostedForTest, hostedForTest},
		// Ingest switched off: a hosted default has no mailbox to point at.
		{DMARCDefaultHosted, "", "", ""},
		// The custom address belongs to the custom mode only.
		{DMARCDefaultAccount, "ignored@hub.example", hostedForTest, "mix@example.org"},
	} {
		if err := st.SetDMARCDefault(u.ID, tc.mode, tc.address); err != nil {
			t.Fatalf("SetDMARCDefault(%s): %v", tc.mode, err)
		}
		got, err := st.DomainDMARCRua(d, tc.hosted)
		if err != nil || got != tc.want {
			t.Errorf("mode %s, hosted %q: resolved %q, %v; want %q", tc.mode, tc.hosted, got, err, tc.want)
		}
	}
	def, err := st.GetDMARCDefault(u.ID)
	if err != nil || def.Address != "" || def.Username != "admin" {
		t.Errorf("default after a non-custom mode = %+v, %v; the custom address must be cleared", def, err)
	}
	if err := st.SetDMARCDefault(u.ID, "profile", ""); err == nil {
		t.Error("an unknown mode was stored")
	}
	if err := st.SetDMARCDefault(u.ID+100, DMARCDefaultNone, ""); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown user = %v, want ErrUserNotFound", err)
	}
}

// Deleting the user a domain follows must leave the domain with no report
// address — not with another user's default, and not with a dangling id.
func TestDeletingTheFollowedUserLeavesNoReports(t *testing.T) {
	st := openTestStore(t)
	if err := st.CreateGlobalUser("admin", "hash"); err != nil {
		t.Fatalf("CreateGlobalUser: %v", err)
	}
	d, _ := st.AddDomain("example.com", "sel")
	id, err := st.CreateUser("second", "hash", RoleGlobal, Reach{})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := st.SetDMARCDefault(id, DMARCDefaultCustom, "dmarc@hub.example"); err != nil {
		t.Fatalf("SetDMARCDefault: %v", err)
	}
	if err := st.SetDomainDMARCUser(d.ID, id); err != nil {
		t.Fatalf("SetDomainDMARCUser: %v", err)
	}
	if err := st.DeleteUser(id); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	got, err := st.GetDomain(d.ID)
	if err != nil {
		t.Fatalf("GetDomain: %v", err)
	}
	if got.DMARCRuaUserID.Valid || got.DMARCRua != "" {
		t.Fatalf("after deleting the followed user: %q / %v, want no reports", got.DMARCRua, got.DMARCRuaUserID)
	}
	if rua, err := st.DomainDMARCRua(got, hostedForTest); err != nil || rua != "" {
		t.Fatalf("resolved %q, %v; want no reports", rua, err)
	}
}
