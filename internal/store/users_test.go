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
	// The account e-mail is the user's own: nothing is mirrored into the
	// instance settings.
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

// The Account profile is one statement (review 2026-11-05, L4): username,
// e-mail and the notifications switch together, the password hash untouched,
// the address's confirmation kept for the address that stays and cleared for
// a new one, a taken username refused whole.
func TestUpdateUserProfile(t *testing.T) {
	st := openTestStore(t)

	if err := st.CreateGlobalUser("admin", "hash-one"); err != nil {
		t.Fatalf("CreateGlobalUser: %v", err)
	}
	u, err := st.GetUserByUsername("admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if _, err := st.db.Exec("UPDATE users SET email_confirmed_at = ? WHERE id = ?",
		"2026-11-06T09:00:00Z", u.ID); err != nil {
		t.Fatalf("confirm address: %v", err)
	}

	if err := st.UpdateUserProfile(u.ID, "operator", "reports@hub.example", false); err != nil {
		t.Fatalf("UpdateUserProfile: %v", err)
	}
	got, err := st.GetUser(u.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Username != "operator" || got.Email != "reports@hub.example" || got.Notify {
		t.Fatalf("profile after save: %+v", got)
	}
	if got.PasswordHash != "hash-one" {
		t.Fatalf("the profile save rewrote the password hash: %q", got.PasswordHash)
	}
	if !got.EmailConfirmed.IsZero() {
		t.Fatalf("a new address stayed confirmed: %v", got.EmailConfirmed)
	}

	// The same address, the switch back on: the confirmation stays.
	if _, err := st.db.Exec("UPDATE users SET email_confirmed_at = ? WHERE id = ?",
		"2026-11-06T09:30:00Z", u.ID); err != nil {
		t.Fatalf("confirm address again: %v", err)
	}
	if err := st.UpdateUserProfile(u.ID, "operator", "reports@hub.example", true); err != nil {
		t.Fatalf("UpdateUserProfile again: %v", err)
	}
	got, _ = st.GetUser(u.ID)
	if got.EmailConfirmed.IsZero() || !got.Notify {
		t.Fatalf("an unchanged address lost its confirmation, or the switch: %+v", got)
	}

	// A username that is taken is refused and nothing is saved.
	if _, err := st.CreateUser("taken", "hash", RoleGlobal, Reach{}); err != nil {
		t.Fatalf("create taken: %v", err)
	}
	if err := st.UpdateUserProfile(u.ID, "taken", "reports@hub.example", true); !errors.Is(err, ErrUserExists) {
		t.Fatalf("a taken username = %v, want ErrUserExists", err)
	}
	got, _ = st.GetUser(u.ID)
	if got.Username != "operator" {
		t.Fatalf("a refused save changed the username: %q", got.Username)
	}

	if err := st.UpdateUserProfile(u.ID+1000, "ghost", "ghost@example.org", true); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("a user that does not exist = %v, want ErrUserNotFound", err)
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
	id, err := st.CreateUser("domainop", "hash2", RoleDomain, Reach{DomainIDs: []int64{d.ID}})
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

// A domain user's reach covers two directions that are granted separately,
// each with an All flag; the store writes and reads all four parts together.
func TestUserReach(t *testing.T) {
	st := openTestStore(t)
	if err := st.CreateGlobalUser("admin", "hash"); err != nil {
		t.Fatalf("CreateGlobalUser: %v", err)
	}
	out1, _ := st.AddDomain("one.example.com", "s1")
	out2, _ := st.AddDomain("two.example.com", "s1")
	in1, _ := st.AddInboundDomain("in-one.example.com")
	in2, _ := st.AddInboundDomain("in-two.example.com")

	if _, err := st.CreateUser("nothing", "h", RoleDomain, Reach{}); !errors.Is(err, ErrEmptyReach) {
		t.Fatalf("a domain user with nothing assigned = %v, want ErrEmptyReach", err)
	}
	if exists, _ := st.GetUserByUsername("nothing"); exists.ID != 0 {
		t.Fatal("the refused user was created")
	}

	// Inbound alone is enough: the role needs something on either list.
	id, err := st.CreateUser("ops", "h", RoleDomain, Reach{InboundDomainIDs: []int64{in1.ID}})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u, _ := st.GetUser(id)
	if len(u.DomainIDs) != 0 || len(u.InboundDomainIDs) != 1 || u.InboundDomainIDs[0] != in1.ID || u.AllDomains || u.AllInboundDomains {
		t.Fatalf("reach = %+v", u.Reach())
	}
	if got, _ := st.ListDomainsForUser(id); len(got) != 0 {
		t.Errorf("an inbound assignment gave %d sending domains", len(got))
	}
	if got, _ := st.ListInboundDomainsForUser(id); len(got) != 1 || got[0].ID != in1.ID {
		t.Errorf("inbound domains for the user = %+v", got)
	}

	// All replaces the rows of its direction and leaves the other alone.
	if err := st.SetUserReach(id, Reach{AllDomains: true, DomainIDs: []int64{out1.ID}, InboundDomainIDs: []int64{in1.ID, in2.ID}}); err != nil {
		t.Fatalf("SetUserReach: %v", err)
	}
	u, _ = st.GetUser(id)
	if !u.AllDomains || len(u.DomainIDs) != 0 || len(u.InboundDomainIDs) != 2 {
		t.Fatalf("reach after All = %+v; rows under All must not be stored", u.Reach())
	}
	// ...and All includes a domain added afterwards.
	out3, _ := st.AddDomain("three.example.com", "s1")
	got, err := st.ListDomainsForUser(id)
	if err != nil || len(got) != 3 {
		t.Fatalf("All lists %d sending domains (%v), want 3 including the one added later", len(got), err)
	}
	_ = out2
	_ = out3

	// Clearing All does not bring an old row back.
	if err := st.SetUserReach(id, Reach{DomainIDs: []int64{out2.ID}, AllInbound: true}); err != nil {
		t.Fatalf("SetUserReach: %v", err)
	}
	u, _ = st.GetUser(id)
	if u.AllDomains || len(u.DomainIDs) != 1 || u.DomainIDs[0] != out2.ID || !u.AllInboundDomains || len(u.InboundDomainIDs) != 0 {
		t.Fatalf("reach = %+v", u.Reach())
	}
	if got, _ := st.ListDomainsForUser(id); len(got) != 1 || got[0].ID != out2.ID {
		t.Errorf("sending domains for the user = %+v", got)
	}
	if got, _ := st.ListInboundDomainsForUser(id); len(got) != 2 {
		t.Errorf("All inbound lists %d domains, want 2", len(got))
	}

	rows, err := st.ListUserRows()
	if err != nil {
		t.Fatalf("ListUserRows: %v", err)
	}
	for _, r := range rows {
		if r.User.ID != id {
			continue
		}
		if len(r.DomainNames) != 1 || r.DomainNames[0] != "two.example.com" || len(r.InboundDomainNames) != 0 || !r.User.AllInboundDomains {
			t.Errorf("list row = %+v", r)
		}
	}

	if err := st.SetUserReach(id, Reach{}); !errors.Is(err, ErrEmptyReach) {
		t.Errorf("emptying a domain user's reach = %v, want ErrEmptyReach", err)
	}
	admin, _ := st.GetUserByUsername("admin")
	if err := st.SetUserReach(admin.ID, Reach{AllDomains: true}); err == nil {
		t.Error("a reach was set on a global user")
	}
	if err := st.ClearUserReach(id); err != nil {
		t.Fatalf("ClearUserReach: %v", err)
	}
	if u, _ = st.GetUser(id); !u.Reach().Empty() {
		t.Errorf("reach after clearing = %+v", u.Reach())
	}
}
