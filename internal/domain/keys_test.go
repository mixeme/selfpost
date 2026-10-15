package domain

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRenderSelectors(t *testing.T) {
	// Deliberately out of order to exercise the deterministic sort.
	domains := []SigningDomain{
		{Name: "zeta.example", Selector: "selfpost"},
		{Name: "alpha.example", Selector: "sel2"},
	}
	got, err := renderSelectors(domains)
	if err != nil {
		t.Fatalf("renderSelectors: %v", err)
	}
	if want := "alpha.example sel2\nzeta.example selfpost\n"; string(got) != want {
		t.Errorf("selector table =\n%q\nwant\n%q", got, want)
	}
	if empty, err := renderSelectors(nil); err != nil || len(empty) != 0 {
		t.Errorf("renderSelectors(nil) = %q, %v; want an empty table", empty, err)
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
		{"..", "selfpost"},
		{".", "selfpost"},
		{"example.com", ".."},
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

// newTestKeys builds a manager rooted at a temp dir.
func newTestKeys(t *testing.T) *Keys {
	t.Helper()
	return NewKeys(t.TempDir())
}

func TestEnsureKeyReusesExisting(t *testing.T) {
	o := newTestKeys(t)

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

// The key and the table are the panel's alone: no other program reads them.
func TestRebuildWritesThePanelsOwnFiles(t *testing.T) {
	o := newTestKeys(t)
	if _, err := o.EnsureKey("example.com", "selfpost"); err != nil {
		t.Fatal(err)
	}
	if err := o.Rebuild([]SigningDomain{{Name: "example.com", Selector: "selfpost"}}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if signed, err := o.signs("example.com", "selfpost"); err != nil || !signed {
		t.Errorf("signs after Rebuild = %v, %v", signed, err)
	}
	if signed, err := o.signs("example.com", "other"); err != nil || signed {
		t.Errorf("signs with another selector = %v, %v", signed, err)
	}
	if runtime.GOOS == "windows" {
		return // no Unix modes
	}
	for path, want := range map[string]os.FileMode{
		o.selectorsPath:                         0o600,
		o.keyPath("example.com", "selfpost"):    0o600,
		filepath.Join(o.keysDir, "example.com"): 0o700,
	} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s: mode %o, want %o", path, got, want)
		}
	}
}

func TestRemoveKey(t *testing.T) {
	o := newTestKeys(t)
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
	o := newTestKeys(t)
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
