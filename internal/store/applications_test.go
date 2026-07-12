package store

import (
	"errors"
	"testing"
)

func addTestDomain(t *testing.T, st *Store, name string) Domain {
	t.Helper()
	d, err := st.AddDomain(name, "selfpost")
	if err != nil {
		t.Fatalf("AddDomain(%q): %v", name, err)
	}
	return d
}

func TestAddApplicationWildcard(t *testing.T) {
	st := openTestStore(t)
	d := addTestDomain(t, st, "example.com")

	a, err := st.AddApplication(d.ID, "alerts", AddressModeWildcard, nil)
	if err != nil {
		t.Fatalf("AddApplication: %v", err)
	}
	if a.ID == 0 || a.Login != "alerts" || a.AddressMode != AddressModeWildcard {
		t.Fatalf("unexpected application: %+v", a)
	}
	if len(a.Addresses) != 0 {
		t.Errorf("wildcard app should have no addresses, got %v", a.Addresses)
	}

	got, err := st.GetApplication(a.ID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if got.Login != "alerts" || len(got.Addresses) != 0 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestAddApplicationListStoresAddresses(t *testing.T) {
	st := openTestStore(t)
	d := addTestDomain(t, st, "example.com")

	addrs := []string{"noreply@example.com", "alerts@example.com"}
	a, err := st.AddApplication(d.ID, "app1", AddressModeList, addrs)
	if err != nil {
		t.Fatalf("AddApplication: %v", err)
	}
	got, err := st.GetApplication(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Addresses come back sorted.
	if len(got.Addresses) != 2 || got.Addresses[0] != "alerts@example.com" || got.Addresses[1] != "noreply@example.com" {
		t.Fatalf("addresses = %v", got.Addresses)
	}
}

func TestAddApplicationDuplicateLogin(t *testing.T) {
	st := openTestStore(t)
	d := addTestDomain(t, st, "example.com")
	d2 := addTestDomain(t, st, "other.com")

	if _, err := st.AddApplication(d.ID, "shared", AddressModeWildcard, nil); err != nil {
		t.Fatal(err)
	}
	// Same login under a different domain must still collide (global uniqueness).
	_, err := st.AddApplication(d2.ID, "shared", AddressModeWildcard, nil)
	if !errors.Is(err, ErrLoginExists) {
		t.Fatalf("duplicate login error = %v, want ErrLoginExists", err)
	}
}

func TestUpdateApplicationMode(t *testing.T) {
	st := openTestStore(t)
	d := addTestDomain(t, st, "example.com")
	a, err := st.AddApplication(d.ID, "app1", AddressModeList, []string{"a@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	// list -> wildcard drops the addresses.
	if err := st.UpdateApplicationMode(a.ID, AddressModeWildcard, nil); err != nil {
		t.Fatalf("UpdateApplicationMode: %v", err)
	}
	got, _ := st.GetApplication(a.ID)
	if got.AddressMode != AddressModeWildcard || len(got.Addresses) != 0 {
		t.Fatalf("after wildcard switch: %+v", got)
	}

	// wildcard -> list adds a fresh set.
	if err := st.UpdateApplicationMode(a.ID, AddressModeList, []string{"b@example.com", "c@example.com"}); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetApplication(a.ID)
	if got.AddressMode != AddressModeList || len(got.Addresses) != 2 {
		t.Fatalf("after list switch: %+v", got)
	}
}

func TestUpdateApplicationModeNotFound(t *testing.T) {
	st := openTestStore(t)
	if err := st.UpdateApplicationMode(999, AddressModeWildcard, nil); !errors.Is(err, ErrApplicationNotFound) {
		t.Fatalf("UpdateApplicationMode(missing) = %v, want ErrApplicationNotFound", err)
	}
}

func TestDeleteApplication(t *testing.T) {
	st := openTestStore(t)
	d := addTestDomain(t, st, "example.com")
	a, err := st.AddApplication(d.ID, "app1", AddressModeList, []string{"a@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := st.DeleteApplication(a.ID)
	if err != nil {
		t.Fatalf("DeleteApplication: %v", err)
	}
	if deleted.Login != "app1" {
		t.Errorf("deleted login = %q, want app1", deleted.Login)
	}
	assertCount(t, st, "applications", 0)
	assertCount(t, st, "application_addresses", 0)

	if _, err := st.DeleteApplication(a.ID); !errors.Is(err, ErrApplicationNotFound) {
		t.Fatalf("second delete = %v, want ErrApplicationNotFound", err)
	}
}

func TestListBindingsMixedModes(t *testing.T) {
	st := openTestStore(t)
	d1 := addTestDomain(t, st, "example.com")
	d2 := addTestDomain(t, st, "other.com")

	if _, err := st.AddApplication(d1.ID, "wild", AddressModeWildcard, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddApplication(d1.ID, "listed", AddressModeList,
		[]string{"alerts@example.com", "noreply@example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddApplication(d2.ID, "wild2", AddressModeWildcard, nil); err != nil {
		t.Fatal(err)
	}

	bindings, err := st.ListBindings()
	if err != nil {
		t.Fatalf("ListBindings: %v", err)
	}
	want := []Binding{
		{"@example.com", "wild"},
		{"@other.com", "wild2"},
		{"alerts@example.com", "listed"},
		{"noreply@example.com", "listed"},
	}
	if len(bindings) != len(want) {
		t.Fatalf("bindings = %+v, want %+v", bindings, want)
	}
	for i := range want {
		if bindings[i] != want[i] {
			t.Errorf("binding[%d] = %+v, want %+v", i, bindings[i], want[i])
		}
	}
}

func TestListLoginsByDomain(t *testing.T) {
	st := openTestStore(t)
	d := addTestDomain(t, st, "example.com")
	other := addTestDomain(t, st, "other.com")
	if _, err := st.AddApplication(d.ID, "a", AddressModeWildcard, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddApplication(d.ID, "b", AddressModeWildcard, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddApplication(other.ID, "c", AddressModeWildcard, nil); err != nil {
		t.Fatal(err)
	}

	logins, err := st.ListLoginsByDomain(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logins) != 2 || logins[0] != "a" || logins[1] != "b" {
		t.Fatalf("logins = %v, want [a b]", logins)
	}
}
