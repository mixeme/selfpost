package app

import (
	"errors"
	"path/filepath"
	"testing"

	"codeberg.org/mix/selfpost/internal/postfix"
	"codeberg.org/mix/selfpost/internal/store"
)

// fakeMaps records the last set of bindings passed to a rebuild and can be told
// to fail, so we can exercise the rollback paths.
type fakeMaps struct {
	last     []postfix.Binding
	calls    int
	failNext bool
}

func (f *fakeMaps) RebuildSenderLoginMaps(b []postfix.Binding) error {
	f.calls++
	if f.failNext {
		f.failNext = false
		return errors.New("boom")
	}
	f.last = b
	return nil
}

// saslRecorder is a fake sasldb2 backend recording set/delete calls.
type saslRecorder struct {
	set      map[string]string // login -> password
	deleted  []string
	failNext bool
}

func newServiceHarness(t *testing.T) (*Service, *store.Store, *saslRecorder, *fakeMaps) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	rec := &saslRecorder{set: map[string]string{}}
	sasl := NewSASLDB("/data/sasl/sasldb2", "mail.example.com")
	sasl.run = func(args []string, stdin []byte) error {
		if rec.failNext {
			rec.failNext = false
			return errors.New("saslpasswd2 failed")
		}
		// args end with the login; a "-d" anywhere means delete.
		login := args[len(args)-1]
		del := false
		for _, a := range args {
			if a == "-d" {
				del = true
			}
		}
		if del {
			rec.deleted = append(rec.deleted, login)
			delete(rec.set, login)
		} else {
			rec.set[login] = string(stdin)
		}
		return nil
	}

	maps := &fakeMaps{}
	return NewService(st, sasl, maps), st, rec, maps
}

func addDomain(t *testing.T, st *store.Store, name string) store.Domain {
	t.Helper()
	d, err := st.AddDomain(name, "selfpost")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	return d
}

func TestServiceCreateWildcard(t *testing.T) {
	svc, st, rec, maps := newServiceHarness(t)
	d := addDomain(t, st, "example.com")

	a, pw, err := svc.Create(d.ID, "alerts", store.AddressModeWildcard, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.Login != "alerts" {
		t.Errorf("login = %q", a.Login)
	}
	if rec.set["alerts"] != pw {
		t.Errorf("sasl password %q != returned %q", rec.set["alerts"], pw)
	}
	if len(maps.last) != 1 || maps.last[0].Address != "@example.com" || maps.last[0].Login != "alerts" {
		t.Errorf("map bindings = %+v", maps.last)
	}
}

func TestServiceCreateListValidatesDomain(t *testing.T) {
	svc, st, _, _ := newServiceHarness(t)
	d := addDomain(t, st, "example.com")

	// A cross-domain address is rejected before anything is written.
	_, _, err := svc.Create(d.ID, "app1", store.AddressModeList, []string{"a@evil.com"})
	if err == nil {
		t.Fatal("Create accepted cross-domain address")
	}
	apps, _ := st.ListApplicationsByDomain(d.ID)
	if len(apps) != 0 {
		t.Errorf("application persisted despite validation failure: %+v", apps)
	}
}

func TestServiceCreateDuplicateLogin(t *testing.T) {
	svc, st, _, _ := newServiceHarness(t)
	d := addDomain(t, st, "example.com")
	if _, _, err := svc.Create(d.ID, "dup", store.AddressModeWildcard, nil); err != nil {
		t.Fatal(err)
	}
	_, _, err := svc.Create(d.ID, "dup", store.AddressModeWildcard, nil)
	if !errors.Is(err, store.ErrLoginExists) {
		t.Fatalf("duplicate create = %v, want ErrLoginExists", err)
	}
}

func TestServiceCreateRollsBackOnSASLFailure(t *testing.T) {
	svc, st, rec, _ := newServiceHarness(t)
	d := addDomain(t, st, "example.com")

	rec.failNext = true // saslpasswd2 fails on the first (set) call
	_, _, err := svc.Create(d.ID, "app1", store.AddressModeWildcard, nil)
	if err == nil {
		t.Fatal("expected Create to fail when SASL set fails")
	}
	apps, _ := st.ListApplicationsByDomain(d.ID)
	if len(apps) != 0 {
		t.Errorf("registry row not rolled back: %+v", apps)
	}
}

func TestServiceCreateRollsBackOnMapFailure(t *testing.T) {
	svc, st, rec, maps := newServiceHarness(t)
	d := addDomain(t, st, "example.com")

	maps.failNext = true
	_, _, err := svc.Create(d.ID, "app1", store.AddressModeWildcard, nil)
	if err == nil {
		t.Fatal("expected Create to fail when map rebuild fails")
	}
	apps, _ := st.ListApplicationsByDomain(d.ID)
	if len(apps) != 0 {
		t.Errorf("registry row not rolled back: %+v", apps)
	}
	if _, ok := rec.set["app1"]; ok {
		t.Errorf("SASL account not rolled back")
	}
}

func TestServiceDelete(t *testing.T) {
	svc, st, rec, _ := newServiceHarness(t)
	d := addDomain(t, st, "example.com")
	a, _, err := svc.Create(d.ID, "app1", store.AddressModeWildcard, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := rec.set["app1"]; ok {
		t.Error("SASL account not deleted")
	}
	if len(rec.deleted) != 1 || rec.deleted[0] != "app1" {
		t.Errorf("deleted logins = %v", rec.deleted)
	}
	apps, _ := st.ListApplicationsByDomain(d.ID)
	if len(apps) != 0 {
		t.Errorf("application not deleted: %+v", apps)
	}
}

func TestServiceUpdateMode(t *testing.T) {
	svc, st, _, maps := newServiceHarness(t)
	d := addDomain(t, st, "example.com")
	a, _, err := svc.Create(d.ID, "app1", store.AddressModeWildcard, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.UpdateMode(a.ID, store.AddressModeList, []string{"alerts@example.com"}); err != nil {
		t.Fatalf("UpdateMode: %v", err)
	}
	if len(maps.last) != 1 || maps.last[0].Address != "alerts@example.com" {
		t.Errorf("map after mode switch = %+v", maps.last)
	}
	got, _ := st.GetApplication(a.ID)
	if got.AddressMode != store.AddressModeList || len(got.Addresses) != 1 {
		t.Errorf("stored app after switch = %+v", got)
	}
}

func TestServiceRegeneratePassword(t *testing.T) {
	svc, st, rec, _ := newServiceHarness(t)
	d := addDomain(t, st, "example.com")
	a, pw1, err := svc.Create(d.ID, "app1", store.AddressModeWildcard, nil)
	if err != nil {
		t.Fatal(err)
	}

	pw2, err := svc.RegeneratePassword(a.ID)
	if err != nil {
		t.Fatalf("RegeneratePassword: %v", err)
	}
	if pw1 == pw2 {
		t.Error("regenerated password equals the old one")
	}
	if rec.set["app1"] != pw2 {
		t.Errorf("sasl password not updated to new value")
	}
}

func TestImportApplicationWritesRowAndSASL(t *testing.T) {
	svc, st, rec, maps := newServiceHarness(t)
	d := addDomain(t, st, "example.com")

	if err := svc.ImportApplication(d.ID, "mailer", store.AddressModeList,
		[]string{"a@example.com"}, "imported-pw"); err != nil {
		t.Fatalf("ImportApplication: %v", err)
	}
	// Registry row and SASL account written with the imported password verbatim.
	apps, _ := st.ListApplicationsByDomain(d.ID)
	if len(apps) != 1 || apps[0].Login != "mailer" {
		t.Fatalf("apps = %+v", apps)
	}
	if rec.set["mailer"] != "imported-pw" {
		t.Errorf("SASL password = %q, want the imported one", rec.set["mailer"])
	}
	// Import does not rebuild the sender map itself (the caller batches that).
	if maps.calls != 0 {
		t.Errorf("ImportApplication rebuilt the map %d times, want 0", maps.calls)
	}
}

func TestImportApplicationRejectsBadInput(t *testing.T) {
	svc, st, rec, _ := newServiceHarness(t)
	d := addDomain(t, st, "example.com")

	// Empty password.
	if err := svc.ImportApplication(d.ID, "mailer", store.AddressModeWildcard, nil, ""); err == nil {
		t.Error("accepted empty imported password")
	}
	// Password with an embedded newline would truncate on the saslpasswd2 stdin.
	if err := svc.ImportApplication(d.ID, "mailer", store.AddressModeWildcard, nil, "line1\nline2"); err == nil {
		t.Error("accepted password with control characters")
	}
	// Cross-domain address.
	if err := svc.ImportApplication(d.ID, "mailer", store.AddressModeList, []string{"x@evil.com"}, "pw"); err == nil {
		t.Error("accepted cross-domain address")
	}
	if apps, _ := st.ListApplicationsByDomain(d.ID); len(apps) != 0 {
		t.Errorf("rows persisted despite validation failure: %+v", apps)
	}
	if len(rec.set) != 0 {
		t.Errorf("SASL accounts written despite validation failure: %v", rec.set)
	}
}

func TestServicePurgeDomainSASL(t *testing.T) {
	svc, st, rec, _ := newServiceHarness(t)
	d := addDomain(t, st, "example.com")
	if _, _, err := svc.Create(d.ID, "app-a", store.AddressModeWildcard, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Create(d.ID, "app-b", store.AddressModeWildcard, nil); err != nil {
		t.Fatal(err)
	}

	if err := svc.PurgeDomainSASL(d.ID); err != nil {
		t.Fatalf("PurgeDomainSASL: %v", err)
	}
	if len(rec.set) != 0 {
		t.Errorf("SASL accounts remain after purge: %v", rec.set)
	}
	if len(rec.deleted) != 2 {
		t.Errorf("deleted %d logins, want 2", len(rec.deleted))
	}
}
