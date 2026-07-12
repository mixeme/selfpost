package postfix

import (
	"os"
	"testing"
)

func TestRenderSenderLoginMaps(t *testing.T) {
	// Deliberately unsorted, with two logins sharing one wildcard key
	// (many-to-one, spec 5.1 §4) to exercise merge + sort.
	bindings := []Binding{
		{"@zeta.example", "z1"},
		{"alerts@alpha.example", "a-listed"},
		{"@alpha.example", "a2"},
		{"@alpha.example", "a1"},
	}
	got, err := renderSenderLoginMaps(bindings)
	if err != nil {
		t.Fatalf("renderSenderLoginMaps: %v", err)
	}
	want := "@alpha.example a1,a2\n" +
		"@zeta.example z1\n" +
		"alerts@alpha.example a-listed\n"
	if string(got) != want {
		t.Errorf("map =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderSenderLoginMapsEmpty(t *testing.T) {
	got, err := renderSenderLoginMaps(nil)
	if err != nil {
		t.Fatalf("renderSenderLoginMaps(nil): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %q", got)
	}
}

func TestRenderSenderLoginMapsDedupesLogin(t *testing.T) {
	bindings := []Binding{
		{"@a.example", "dup"},
		{"@a.example", "dup"},
	}
	got, err := renderSenderLoginMaps(bindings)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "@a.example dup\n" {
		t.Errorf("map = %q, want single deduped login", got)
	}
}

func TestAssertMapSafeRejectsInjection(t *testing.T) {
	bad := []struct{ addr, login string }{
		{"@exa mple.com", "log"},
		{"@example.com\nx y z", "log"},
		{"@example.com", "log,evil"},
		{"@example.com", "log in"},
		{"@example.com", "log@realm"}, // '@' would confuse sasldb realm handling
		{"", "log"},
		{"@example.com", ""},
	}
	for _, b := range bad {
		if err := assertMapSafe(b.addr, b.login); err == nil {
			t.Errorf("assertMapSafe(%q,%q) = nil, want error", b.addr, b.login)
		}
	}
	if err := assertMapSafe("alerts@example.com", "app_1-x"); err != nil {
		t.Errorf("assertMapSafe of a clean pair errored: %v", err)
	}
}

func newTestPostfix(t *testing.T) (*Postfix, *int) {
	t.Helper()
	dir := t.TempDir()
	reloads := 0
	p := New(dir)
	p.reload = func() error { reloads++; return nil }
	return p, &reloads
}

func TestRebuildSenderLoginMapsWritesAndReloads(t *testing.T) {
	p, reloads := newTestPostfix(t)
	if err := p.RebuildSenderLoginMaps([]Binding{{"@example.com", "app1"}}); err != nil {
		t.Fatalf("RebuildSenderLoginMaps: %v", err)
	}
	if *reloads != 1 {
		t.Errorf("reload called %d times, want 1", *reloads)
	}
	data, err := os.ReadFile(p.senderLoginMapsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "@example.com app1\n" {
		t.Errorf("map file = %q", data)
	}
}

func TestRebuildRejectsUnsafeWithoutWriting(t *testing.T) {
	p, reloads := newTestPostfix(t)
	// Seed a known-good file so we can prove the failed rebuild left it untouched.
	if err := p.RebuildSenderLoginMaps([]Binding{{"@good.example", "ok"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(p.senderLoginMapsPath)

	err := p.RebuildSenderLoginMaps([]Binding{{"@bad.example", "evil\nlogin"}})
	if err == nil {
		t.Fatal("expected rebuild to reject unsafe login")
	}
	after, _ := os.ReadFile(p.senderLoginMapsPath)
	if string(after) != string(before) {
		t.Errorf("map file changed on failed rebuild: %q", after)
	}
	if *reloads != 1 {
		t.Errorf("reload called %d times, want 1 (no reload on failure)", *reloads)
	}
}
