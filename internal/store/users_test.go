package store

import (
	"errors"
	"testing"
)

func TestUpdateUser(t *testing.T) {
	st := openTestStore(t)

	if err := st.CreateGlobalUser("admin", "hash-one"); err != nil {
		t.Fatalf("CreateGlobalUser: %v", err)
	}
	u, err := st.GetUserByUsername("admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if err := st.UpdateUser(u.ID, "operator", "hash-two", "reports@hub.example"); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	got, err := st.GetUser(u.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Email != "reports@hub.example" {
		t.Fatalf("email = %q", got.Email)
	}
	// The account e-mail is the user's own: saving it does not make it anyone's
	// report address, and nothing is mirrored into the instance settings.
	if got.DMARCDefaultMode != DMARCDefaultNone {
		t.Fatalf("dmarc default mode = %q after setting the e-mail, want %q", got.DMARCDefaultMode, DMARCDefaultNone)
	}
	if v, err := st.GetSetting("dmarc_report_email"); err == nil && v != "" {
		t.Fatalf("the e-mail leaked into the instance settings: %q", v)
	}

	if err := st.UpdateUser(u.ID, "operator", "hash-three", ""); err != nil {
		t.Fatalf("clear e-mail: %v", err)
	}
	got, err = st.GetUser(u.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Username != "operator" || got.PasswordHash != "hash-three" {
		t.Fatalf("unexpected user after update: %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("update dropped created_at")
	}
}

func TestUpdateUserWithoutUser(t *testing.T) {
	st := openTestStore(t)

	if err := st.UpdateUser(1, "operator", "hash", ""); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("UpdateUser without user = %v, want ErrUserNotFound", err)
	}
	exists, err := st.UserExists()
	if err != nil {
		t.Fatalf("UserExists: %v", err)
	}
	if exists {
		t.Fatal("UpdateUser created a user")
	}
}

func TestCreateDomainAdminUser(t *testing.T) {
	st := openTestStore(t)
	if err := st.CreateGlobalUser("admin", "hash"); err != nil {
		t.Fatalf("CreateGlobalUser: %v", err)
	}
	d, err := st.AddDomain("example.com", "s1")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	id, err := st.CreateUser("domainop", "hash2", RoleDomain, []int64{d.ID})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u, err := st.GetUser(id)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if len(u.DomainIDs) != 1 || u.DomainIDs[0] != d.ID {
		t.Fatalf("domain ids = %v, want [%d]", u.DomainIDs, d.ID)
	}
}

func TestDeleteLastGlobalUser(t *testing.T) {
	st := openTestStore(t)
	if err := st.CreateGlobalUser("admin", "hash"); err != nil {
		t.Fatalf("CreateGlobalUser: %v", err)
	}
	u, err := st.GetUserByUsername("admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if err := st.DeleteUser(u.ID); !errors.Is(err, ErrLastGlobal) {
		t.Fatalf("DeleteUser = %v, want ErrLastGlobal", err)
	}
}
