package store

import (
	"errors"
	"testing"
)

func TestUpdateAdmin(t *testing.T) {
	st := openTestStore(t)

	if err := st.CreateAdmin("admin", "hash-one"); err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	if err := st.UpdateAdmin("operator", "hash-two"); err != nil {
		t.Fatalf("UpdateAdmin: %v", err)
	}

	a, err := st.GetAdmin()
	if err != nil {
		t.Fatalf("GetAdmin: %v", err)
	}
	if a.Username != "operator" || a.PasswordHash != "hash-two" {
		t.Fatalf("unexpected admin after update: %+v", a)
	}
	if a.CreatedAt.IsZero() {
		t.Fatal("update dropped created_at")
	}
}

// An update before setup must not create the account: only the one-time setup
// flow may do that (spec 7.6.1).
func TestUpdateAdminWithoutAdmin(t *testing.T) {
	st := openTestStore(t)

	if err := st.UpdateAdmin("operator", "hash"); !errors.Is(err, ErrNoAdmin) {
		t.Fatalf("UpdateAdmin without admin = %v, want ErrNoAdmin", err)
	}
	exists, err := st.AdminExists()
	if err != nil {
		t.Fatalf("AdminExists: %v", err)
	}
	if exists {
		t.Fatal("UpdateAdmin created an administrator")
	}
}
