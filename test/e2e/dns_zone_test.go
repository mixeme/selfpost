package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// Two zones written within one second still get two serials, the second
// larger: CoreDNS reloads a zone only when its serial grows (C-E2).
func TestZoneSerialGrows(t *testing.T) {
	stage := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stage, "dns-stage"), 0o755); err != nil {
		t.Fatal(err)
	}
	soa := regexp.MustCompile(`IN SOA  ns\.e2e\.test\. admin\.e2e\.test\. \( (\d+) `)
	serial := func() int64 {
		t.Helper()
		if err := writeZone(stage, nil); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(stage, "dns-stage", "db.zone"))
		if err != nil {
			t.Fatal(err)
		}
		m := soa.FindSubmatch(b)
		if m == nil {
			t.Fatalf("no SOA serial in\n%s", b)
		}
		n, _ := strconv.ParseInt(string(m[1]), 10, 64)
		return n
	}
	first, second := serial(), serial()
	if second <= first {
		t.Fatalf("serials %d then %d: the second write would not be served", first, second)
	}
}
