package app

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
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

// If sasldb2 cannot be updated the application must stay in the registry: an
// account that still authenticates but has no panel row is invisible to the
// operator and cannot be deleted again.
func TestServiceDeleteKeepsRowWhenSASLFails(t *testing.T) {
	svc, st, rec, _ := newServiceHarness(t)
	d := addDomain(t, st, "example.com")
	a, _, err := svc.Create(d.ID, "app1", store.AddressModeWildcard, nil)
	if err != nil {
		t.Fatal(err)
	}

	rec.failNext = true // saslpasswd2 -d fails
	if err := svc.Delete(a.ID); err == nil {
		t.Fatal("Delete reported success although the SASL account was not removed")
	}
	apps, _ := st.ListApplicationsByDomain(d.ID)
	if len(apps) != 1 {
		t.Fatalf("registry row dropped while the SASL account can still authenticate: %+v", apps)
	}
	if _, ok := rec.set["app1"]; !ok {
		t.Fatal("SASL account gone despite the failure — the harness no longer proves the ordering")
	}
	// The delete is retryable now that the row is still there.
	if err := svc.Delete(a.ID); err != nil {
		t.Fatalf("retried Delete: %v", err)
	}
	if _, ok := rec.set["app1"]; ok {
		t.Error("SASL account not deleted on retry")
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

// The application form is one POST: sender, client IPs and rate limit arrive
// together and are saved together.
func TestCreateWithSettings(t *testing.T) {
	svc, st, rec, _ := newServiceHarness(t)
	d := addDomain(t, st, "example.com")
	recalced := 0

	a, pw, err := svc.CreateWithSettings(d.ID, "prod", Settings{
		Mode: store.AddressModeList, Addresses: []string{"alerts@example.com"},
		AuthIPRestrict: true, AuthAllowedIPs: []string{"203.0.113.10"},
		Limit: Limit{Mode: store.RateLimitModeManual, MaxMessages: 200, WindowSeconds: 3600},
	}, func(int64) error { recalced++; return nil })
	if err != nil {
		t.Fatalf("CreateWithSettings: %v", err)
	}
	if rec.set["prod"] != pw || pw == "" {
		t.Errorf("sasl password %q != returned %q", rec.set["prod"], pw)
	}
	if a.AddressMode != store.AddressModeList || len(a.Addresses) != 1 || !a.AuthIPRestrict || len(a.AuthAllowedIPs) != 1 {
		t.Errorf("application = %+v", a)
	}
	rl, ok, err := svc.RateLimit(a.ID)
	if err != nil || !ok || rl.MaxMessages != 200 || rl.WindowSeconds != 3600 || rl.IsAuto() {
		t.Errorf("rate limit = %+v, %v, %v", rl, ok, err)
	}
	if recalced != 0 {
		t.Error("a manual limit was recalculated")
	}

	// Auto: the ceiling comes from the caller's recalculation.
	b, _, err := svc.CreateWithSettings(d.ID, "auto", Settings{
		Mode: store.AddressModeWildcard, Limit: Limit{Mode: store.RateLimitModeAuto, AutoMultiplier: 2.5, WindowSeconds: 3600},
	}, func(int64) error { recalced++; return nil })
	if err != nil {
		t.Fatalf("CreateWithSettings auto: %v", err)
	}
	if rl, ok, _ := svc.RateLimit(b.ID); !ok || !rl.IsAuto() || rl.AutoMultiplier != 2.5 || recalced != 1 {
		t.Errorf("auto limit = %+v, %v; recalculated %d time(s)", rl, ok, recalced)
	}

	// No limit of its own: the domain limit applies.
	c, _, err := svc.CreateWithSettings(d.ID, "plain", Settings{Mode: store.AddressModeWildcard}, nil)
	if err != nil {
		t.Fatalf("CreateWithSettings plain: %v", err)
	}
	if _, ok, _ := svc.RateLimit(c.ID); ok {
		t.Error("an application created without a limit has one")
	}
}

// A failure after the application row exists must leave no application: not in
// the registry, not in sasldb2, and no orphaned limit.
func TestCreateWithSettingsIsAllOrNothing(t *testing.T) {
	svc, st, rec, _ := newServiceHarness(t)
	d := addDomain(t, st, "example.com")

	_, _, err := svc.CreateWithSettings(d.ID, "prod", Settings{
		Mode: store.AddressModeWildcard, AuthIPRestrict: true, AuthAllowedIPs: []string{"203.0.113.10"},
		Limit: Limit{Mode: store.RateLimitModeAuto, AutoMultiplier: 2, WindowSeconds: 3600},
	}, func(int64) error { return errors.New("no statistics") })
	if err == nil {
		t.Fatal("CreateWithSettings succeeded although the recalculation failed")
	}
	if apps, _ := st.ListApplicationsByDomain(d.ID); len(apps) != 0 {
		t.Errorf("%d application(s) left behind", len(apps))
	}
	if _, ok := rec.set["prod"]; ok {
		t.Error("the SASL account was left behind")
	}
	// The id is free again and carries nothing over.
	a, _, err := svc.CreateWithSettings(d.ID, "prod", Settings{Mode: store.AddressModeWildcard}, nil)
	if err != nil {
		t.Fatalf("create again: %v", err)
	}
	if _, ok, _ := svc.RateLimit(a.ID); ok || a.AuthIPRestrict {
		t.Errorf("the new application inherited settings of the failed one: %+v", a)
	}

	// A sender rule that does not validate creates nothing either.
	if _, _, err := svc.CreateWithSettings(d.ID, "bad", Settings{Mode: store.AddressModeList, Addresses: []string{"a@evil.com"}}, nil); err == nil {
		t.Error("a cross-domain address was accepted")
	}
	if _, ok := rec.set["bad"]; ok {
		t.Error("a SASL account was written for a refused application")
	}
}

func TestSaveSettings(t *testing.T) {
	svc, st, rec, maps := newServiceHarness(t)
	d := addDomain(t, st, "example.com")
	a, pw, err := svc.CreateWithSettings(d.ID, "prod", Settings{
		Mode: store.AddressModeWildcard, Limit: Limit{Mode: store.RateLimitModeManual, MaxMessages: 100, WindowSeconds: 3600},
	}, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = svc.SaveSettings(a.ID, Settings{
		Mode: store.AddressModeList, Addresses: []string{"alerts@example.com", "noreply@example.com"},
		AuthIPRestrict: true, AuthAllowedIPs: []string{"203.0.113.10", "203.0.113.11"},
	}, nil)
	if err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	got, _ := st.GetApplication(a.ID)
	if got.AddressMode != store.AddressModeList || len(got.Addresses) != 2 || !got.AuthIPRestrict || len(got.AuthAllowedIPs) != 2 {
		t.Errorf("application = %+v", got)
	}
	if _, ok, _ := svc.RateLimit(a.ID); ok {
		t.Error("choosing the domain limit left the application's own limit in place")
	}
	if len(maps.last) != 2 {
		t.Errorf("sender map has %d binding(s), want the two listed addresses", len(maps.last))
	}
	if rec.set["prod"] != pw {
		t.Error("saving the settings touched the password")
	}
}

// Nothing is half-saved: a sender rule that does not validate changes nothing,
// and a failure in a later write puts the earlier ones back.
func TestSaveSettingsIsAllOrNothing(t *testing.T) {
	svc, st, _, maps := newServiceHarness(t)
	d := addDomain(t, st, "example.com")
	a, _, err := svc.CreateWithSettings(d.ID, "prod", Settings{
		Mode: store.AddressModeList, Addresses: []string{"alerts@example.com"},
		AuthIPRestrict: true, AuthAllowedIPs: []string{"203.0.113.10"},
		Limit: Limit{Mode: store.RateLimitModeManual, MaxMessages: 100, WindowSeconds: 3600},
	}, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	unchanged := func(when string) {
		t.Helper()
		got, _ := st.GetApplication(a.ID)
		rl, ok, _ := svc.RateLimit(a.ID)
		if got.AddressMode != store.AddressModeList || len(got.Addresses) != 1 || got.Addresses[0] != "alerts@example.com" ||
			!got.AuthIPRestrict || len(got.AuthAllowedIPs) != 1 || !ok || rl.MaxMessages != 100 || rl.IsAuto() {
			t.Errorf("%s: the application changed: %+v, limit %+v (%v)", when, got, rl, ok)
		}
	}

	change := Settings{
		Mode: store.AddressModeWildcard, AuthIPRestrict: false,
		Limit: Limit{Mode: store.RateLimitModeAuto, AutoMultiplier: 3, WindowSeconds: 3600},
	}
	bad := change
	bad.Mode, bad.Addresses = store.AddressModeList, []string{"a@evil.com"}
	if err := svc.SaveSettings(a.ID, bad, nil); err == nil {
		t.Fatal("a cross-domain address was accepted")
	}
	unchanged("after a refused sender rule")

	if err := svc.SaveSettings(a.ID, change, func(int64) error { return errors.New("no statistics") }); err == nil {
		t.Fatal("SaveSettings succeeded although the recalculation failed")
	}
	unchanged("after a failed recalculation")

	maps.failNext = true
	if err := svc.SaveSettings(a.ID, change, func(int64) error { return nil }); err == nil {
		t.Fatal("SaveSettings succeeded although the sender map could not be rebuilt")
	}
	unchanged("after a failed sender-map rebuild")
	if len(maps.last) != 1 || maps.last[0].Address != "alerts@example.com" {
		t.Errorf("the sender map was left at %+v", maps.last)
	}

	if err := svc.SaveSettings(a.ID+100, change, nil); !errors.Is(err, store.ErrApplicationNotFound) {
		t.Errorf("unknown application = %v, want ErrApplicationNotFound", err)
	}
}
