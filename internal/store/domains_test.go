package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestAddAndListDomains(t *testing.T) {
	st := openTestStore(t)

	d, err := st.AddDomain("example.com", "selfpost")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	if d.ID == 0 || d.Name != "example.com" || d.DKIMSelector != "selfpost" {
		t.Fatalf("unexpected domain: %+v", d)
	}

	list, err := st.ListDomains()
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	if len(list) != 1 || list[0].Name != "example.com" || list[0].AppCount != 0 {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestAddDomainDuplicate(t *testing.T) {
	st := openTestStore(t)
	if _, err := st.AddDomain("example.com", "selfpost"); err != nil {
		t.Fatal(err)
	}
	_, err := st.AddDomain("example.com", "other")
	if !errors.Is(err, ErrDomainExists) {
		t.Fatalf("duplicate AddDomain error = %v, want ErrDomainExists", err)
	}
}

func TestGetDomainNotFound(t *testing.T) {
	st := openTestStore(t)
	if _, err := st.GetDomain(999); !errors.Is(err, ErrDomainNotFound) {
		t.Fatalf("GetDomain(missing) = %v, want ErrDomainNotFound", err)
	}
}

func TestDeleteDomainCascadesApplications(t *testing.T) {
	st := openTestStore(t)
	d, err := st.AddDomain("example.com", "selfpost")
	if err != nil {
		t.Fatal(err)
	}

	// Insert an application + address directly (the AddApplication API lands in
	// Phase 4); this verifies the ON DELETE CASCADE wiring now.
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := st.db.Exec(
		"INSERT INTO applications (domain_id, login, address_mode, created_at) VALUES (?, ?, 'wildcard', ?)",
		d.ID, "app1", now)
	if err != nil {
		t.Fatal(err)
	}
	appID, _ := res.LastInsertId()
	if _, err := st.db.Exec(
		"INSERT INTO application_addresses (application_id, address) VALUES (?, ?)",
		appID, "noreply@example.com"); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetDomain(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AppCount != 1 {
		t.Fatalf("AppCount = %d, want 1", got.AppCount)
	}

	if err := st.DeleteDomain(d.ID); err != nil {
		t.Fatalf("DeleteDomain: %v", err)
	}

	assertCount(t, st, "applications", 0)
	assertCount(t, st, "application_addresses", 0)
	if _, err := st.GetDomain(d.ID); !errors.Is(err, ErrDomainNotFound) {
		t.Errorf("domain still present after delete: %v", err)
	}
}

func TestDeleteDomainNotFound(t *testing.T) {
	st := openTestStore(t)
	if err := st.DeleteDomain(123); !errors.Is(err, ErrDomainNotFound) {
		t.Fatalf("DeleteDomain(missing) = %v, want ErrDomainNotFound", err)
	}
}

func assertCount(t *testing.T, st *Store, table string, want int) {
	t.Helper()
	var n int
	// table is a trusted literal from the test, not user input.
	if err := st.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if n != want {
		t.Errorf("%s count = %d, want %d", table, n, want)
	}
}
