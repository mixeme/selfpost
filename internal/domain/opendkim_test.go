package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderTables(t *testing.T) {
	keysDir := "/data/opendkim/keys"
	// Deliberately out of order to exercise the deterministic sort.
	domains := []SigningDomain{
		{Name: "zeta.example", Selector: "selfpost"},
		{Name: "alpha.example", Selector: "sel2"},
	}
	kt, st, err := renderTables(keysDir, domains)
	if err != nil {
		t.Fatalf("renderTables: %v", err)
	}

	wantKT := "alpha.example alpha.example:sel2:/data/opendkim/keys/alpha.example/sel2.private\n" +
		"zeta.example zeta.example:selfpost:/data/opendkim/keys/zeta.example/selfpost.private\n"
	if string(kt) != wantKT {
		t.Errorf("KeyTable =\n%q\nwant\n%q", kt, wantKT)
	}

	wantST := "*@alpha.example alpha.example\n*@zeta.example zeta.example\n"
	if string(st) != wantST {
		t.Errorf("SigningTable =\n%q\nwant\n%q", st, wantST)
	}
}

func TestRenderTablesEmpty(t *testing.T) {
	kt, st, err := renderTables("/keys", nil)
	if err != nil {
		t.Fatalf("renderTables(nil): %v", err)
	}
	if len(kt) != 0 || len(st) != 0 {
		t.Errorf("expected empty tables, got kt=%q st=%q", kt, st)
	}
}

func TestAssertConfigSafeRejectsInjection(t *testing.T) {
	bad := []struct{ name, sel string }{
		{"exa mple.com", "selfpost"},
		{"example.com\nInject yes", "selfpost"},
		{"example.com", "sel:evil"},
		{"../etc", "selfpost"},
		{"", "selfpost"},
		{"example.com", ""},
	}
	for _, b := range bad {
		if err := assertConfigSafe(b.name, b.sel); err == nil {
			t.Errorf("assertConfigSafe(%q,%q) = nil, want error", b.name, b.sel)
		}
	}
	if err := assertConfigSafe("example.com", "selfpost"); err != nil {
		t.Errorf("assertConfigSafe of a clean pair errored: %v", err)
	}
}

// newTestOpenDKIM builds a manager rooted at a temp dir with reload stubbed out.
func newTestOpenDKIM(t *testing.T) (*OpenDKIM, *int) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "keys"), 0o750); err != nil {
		t.Fatal(err)
	}
	reloads := 0
	o := NewOpenDKIM(dir)
	o.reload = func() error { reloads++; return nil }
	return o, &reloads
}

func TestEnsureKeyReusesExisting(t *testing.T) {
	o, _ := newTestOpenDKIM(t)

	created, err := o.EnsureKey("example.com", "selfpost")
	if err != nil {
		t.Fatalf("EnsureKey: %v", err)
	}
	if !created {
		t.Fatal("expected a new key to be created")
	}
	first, err := os.ReadFile(o.keyPath("example.com", "selfpost"))
	if err != nil {
		t.Fatal(err)
	}

	created, err = o.EnsureKey("example.com", "selfpost")
	if err != nil {
		t.Fatalf("EnsureKey (second): %v", err)
	}
	if created {
		t.Error("expected existing key to be reused, not regenerated")
	}
	second, _ := os.ReadFile(o.keyPath("example.com", "selfpost"))
	if string(first) != string(second) {
		t.Error("key file changed on reuse — published DNS record would break")
	}
}

func TestRebuildWritesTablesAndReloads(t *testing.T) {
	o, reloads := newTestOpenDKIM(t)
	if _, err := o.EnsureKey("example.com", "selfpost"); err != nil {
		t.Fatal(err)
	}
	if err := o.Rebuild([]SigningDomain{{Name: "example.com", Selector: "selfpost"}}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if *reloads != 1 {
		t.Errorf("reload called %d times, want 1", *reloads)
	}
	kt, err := os.ReadFile(o.keyTablePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(kt), "example.com:selfpost:") {
		t.Errorf("KeyTable missing entry: %q", kt)
	}
}

func TestRemoveKey(t *testing.T) {
	o, _ := newTestOpenDKIM(t)
	if _, err := o.EnsureKey("example.com", "selfpost"); err != nil {
		t.Fatal(err)
	}
	if err := o.RemoveKey("example.com"); err != nil {
		t.Fatalf("RemoveKey: %v", err)
	}
	if _, err := os.Stat(filepath.Join(o.keysDir, "example.com")); !os.IsNotExist(err) {
		t.Error("key directory still present after RemoveKey")
	}
	// Removing a non-existent key is not an error.
	if err := o.RemoveKey("example.com"); err != nil {
		t.Errorf("RemoveKey on missing dir errored: %v", err)
	}
}

func TestRecordFromWrittenKey(t *testing.T) {
	o, _ := newTestOpenDKIM(t)
	if _, err := o.EnsureKey("example.com", "selfpost"); err != nil {
		t.Fatal(err)
	}
	rec, err := o.Record("example.com", "selfpost")
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if rec.Name != "selfpost._domainkey.example.com" {
		t.Errorf("record name = %q", rec.Name)
	}
	if !strings.HasPrefix(rec.Value, "v=DKIM1;") {
		t.Errorf("record value = %q", rec.Value)
	}
}
