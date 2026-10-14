package inbound

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

func testLists(t *testing.T) (*Lists, *store.Store, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	dir := filepath.Join(t.TempDir(), "antispam")
	return NewLists(st, dir), st, dir
}

// mapLines reads a map file without its comment line.
func mapLines(t *testing.T, dir, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

func TestNormalizeListValue(t *testing.T) {
	for _, c := range []struct{ kind, raw, want string }{
		{store.SpamEntryAddress, " Billing@Vendor.Example ", "billing@vendor.example"},
		{store.SpamEntryAddress, "srs0=ab12=cd=example.org=user@forwarder.example", "srs0=ab12=cd=example.org=user@forwarder.example"},
		{store.SpamEntryDomain, "@Evil.Example", "evil.example"},
		{store.SpamEntryDomain, "sub.evil.example", "sub.evil.example"},
		{store.SpamEntryIP, "192.0.2.7", "192.0.2.7"},
		{store.SpamEntryIP, "192.0.2.77/24", "192.0.2.0/24"},
		{store.SpamEntryIP, "192.0.2.7/32", "192.0.2.7"},
		{store.SpamEntryIP, "2001:db8::1/128", "2001:db8::1"},
		{store.SpamEntryIP, "::ffff:192.0.2.7", "192.0.2.7"},
		{store.SpamEntryIP, "2001:DB8:0:0::1", "2001:db8::1"},
		{store.SpamEntryIP, "2001:db8:1::5/48", "2001:db8:1::/48"},
	} {
		got, err := normalizeListValue(c.kind, c.raw)
		if err != nil || got != c.want {
			t.Errorf("normalizeListValue(%s, %q) = %q, %v; want %q", c.kind, c.raw, got, err, c.want)
		}
	}

	for _, c := range []struct{ kind, raw string }{
		{store.SpamEntryAddress, ""},
		{store.SpamEntryAddress, "no-at-sign"},
		{store.SpamEntryAddress, "@example.com"},
		{store.SpamEntryAddress, "a@localhost"},
		{store.SpamEntryAddress, "a b@example.com"},
		{store.SpamEntryAddress, "a#b@example.com"},
		{store.SpamEntryAddress, "a\nb@example.com"},
		{store.SpamEntryAddress, ".a@example.com"},
		{store.SpamEntryDomain, ""},
		{store.SpamEntryDomain, "example"},
		{store.SpamEntryDomain, "exa mple.com"},
		{store.SpamEntryDomain, "example.com # all of it"},
		{store.SpamEntryDomain, "user@example.com"},
		{store.SpamEntryIP, ""},
		{store.SpamEntryIP, "example.com"},
		{store.SpamEntryIP, "192.0.2.300"},
		{store.SpamEntryIP, "192.0.2.0/33"},
		{store.SpamEntryIP, "0.0.0.0/0"},
		{store.SpamEntryIP, "::/0"},
		{store.SpamEntryIP, "fe80::1%eth0"},
		{store.SpamEntryIP, "::ffff:192.0.2.0/120"},
		{store.SpamEntryIP, "192.0.2.7 192.0.2.8"},
		{"subject", "pills"},
		{"", "x"},
	} {
		got, err := normalizeListValue(c.kind, c.raw)
		var ve *ValidationError
		if err == nil || !errors.As(err, &ve) {
			t.Errorf("normalizeListValue(%q, %q) = %q, %v; want a ValidationError", c.kind, c.raw, got, err)
		}
	}
}

func TestListsAddWritesTheMaps(t *testing.T) {
	l, _, dir := testLists(t)
	for _, e := range []struct{ list, kind, value string }{
		{store.SpamListAllow, store.SpamEntryAddress, "Billing@Vendor.Example"},
		{store.SpamListAllow, store.SpamEntryAddress, "accounts@vendor.example"},
		{store.SpamListDeny, store.SpamEntryDomain, "@evil.example"},
		{store.SpamListDeny, store.SpamEntryIP, "198.51.100.9/24"},
		// The same value on both lists is two entries: deny wins in the filter.
		{store.SpamListDeny, store.SpamEntryAddress, "billing@vendor.example"},
	} {
		if _, err := l.Add(e.list, e.kind, e.value, " why "); err != nil {
			t.Fatalf("Add(%s, %s, %q): %v", e.list, e.kind, e.value, err)
		}
	}

	want := map[string]string{
		"allow_from.map":   "accounts@vendor.example billing@vendor.example",
		"allow_domain.map": "",
		"allow_ip.map":     "",
		"deny_from.map":    "billing@vendor.example",
		"deny_domain.map":  "evil.example",
		"deny_ip.map":      "198.51.100.0/24",
	}
	for name, lines := range want {
		if got := strings.Join(mapLines(t, dir, name), " "); got != lines {
			t.Errorf("%s = %q, want %q", name, got, lines)
		}
		// The filter is another user in another container.
		if info, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("stat %s: %v", name, err)
		} else if info.Mode().Perm()&0o004 == 0 && os.PathSeparator == '/' {
			t.Errorf("%s is mode %o, not readable by the filter", name, info.Mode().Perm())
		}
	}
	if files, _ := os.ReadDir(dir); len(files) != len(want) {
		t.Errorf("the directory holds %d files, want %d (no temp file left behind)", len(files), len(want))
	}

	entries, err := l.Entries()
	if err != nil || len(entries) != 5 {
		t.Fatalf("Entries = %d, %v", len(entries), err)
	}
	if entries[0].Note != "why" {
		t.Errorf("note = %q, want it trimmed", entries[0].Note)
	}
}

func TestListsRefusals(t *testing.T) {
	l, _, dir := testLists(t)
	if _, err := l.Add(store.SpamListAllow, store.SpamEntryIP, "192.0.2.7", ""); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Written differently, the same entry.
	if _, err := l.Add(store.SpamListAllow, store.SpamEntryIP, "::ffff:192.0.2.7", ""); !errors.Is(err, store.ErrSpamListEntryExists) {
		t.Errorf("the same address again: %v, want ErrSpamListEntryExists", err)
	}
	var ve *ValidationError
	for name, add := range map[string]func() error{
		"an unknown list": func() error { _, err := l.Add("block", store.SpamEntryIP, "192.0.2.8", ""); return err },
		"a bad value":     func() error { _, err := l.Add(store.SpamListDeny, store.SpamEntryIP, "nope", ""); return err },
		"a long note": func() error {
			_, err := l.Add(store.SpamListDeny, store.SpamEntryIP, "192.0.2.8", strings.Repeat("x", noteMaxRunes+1))
			return err
		},
		"a note of two lines": func() error {
			_, err := l.Add(store.SpamListDeny, store.SpamEntryIP, "192.0.2.8", "one\ntwo")
			return err
		},
	} {
		if err := add(); !errors.As(err, &ve) {
			t.Errorf("%s: %v, want a ValidationError", name, err)
		}
	}
	if got := strings.Join(mapLines(t, dir, "deny_ip.map"), " "); got != "" {
		t.Errorf("deny_ip.map = %q after refusals only", got)
	}
	if entries, _ := l.Entries(); len(entries) != 1 {
		t.Errorf("entries = %d, want the first one alone", len(entries))
	}
}

func TestListsDelete(t *testing.T) {
	l, _, dir := testLists(t)
	e, err := l.Add(store.SpamListDeny, store.SpamEntryDomain, "evil.example", "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := l.Delete(e.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := mapLines(t, dir, "deny_domain.map"); len(got) != 0 {
		t.Errorf("deny_domain.map = %v after the delete", got)
	}
	if err := l.Delete(e.ID); !errors.Is(err, store.ErrSpamListEntryNotFound) {
		t.Errorf("a second delete: %v", err)
	}
}

// An entry the filter never got must not stay on the page.
func TestListsAddRollsBackWhenTheMapsCannotBeWritten(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// A file where the directory should be.
	blocked := filepath.Join(t.TempDir(), "antispam")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	l := NewLists(st, blocked)
	if _, err := l.Add(store.SpamListDeny, store.SpamEntryDomain, "evil.example", ""); err == nil {
		t.Fatal("Add succeeded with nowhere to write the maps")
	}
	if entries, _ := l.Entries(); len(entries) != 0 {
		t.Errorf("the entry stayed: %+v", entries)
	}
}

// The writer's own guard: a row that got into the table some other way is not
// written into a map.
func TestListsSyncRefusesAnUnsafeRow(t *testing.T) {
	l, st, dir := testLists(t)
	if _, err := st.AddSpamListEntry(store.SpamListDeny, store.SpamEntryDomain, "evil.example\ngood.example", ""); err != nil {
		t.Fatalf("AddSpamListEntry: %v", err)
	}
	if err := l.Sync(); err == nil {
		t.Fatal("Sync wrote a value with a newline in it")
	}
	if _, err := os.Stat(filepath.Join(dir, "deny_domain.map")); err == nil {
		t.Error("a map file was written")
	}
}

// With nothing on the lists all six files are there, empty.
func TestListsSyncWritesEmptyMaps(t *testing.T) {
	l, _, dir := testLists(t)
	if err := l.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	for _, name := range mapFiles {
		if got := mapLines(t, dir, name); len(got) != 0 {
			t.Errorf("%s = %v", name, got)
		}
	}
}

func TestListsLastWritten(t *testing.T) {
	l, _, dir := testLists(t)
	if got := l.LastWritten(); !got.IsZero() {
		t.Fatalf("LastWritten before anything was written = %v", got)
	}
	if err := l.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := l.LastWritten(); got.IsZero() || time.Since(got) > time.Minute {
		t.Errorf("LastWritten after a sync = %v", got)
	}

	// The newest of the six counts, whichever it is.
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, name := range mapFiles {
		if err := os.Chtimes(filepath.Join(dir, name), old, old); err != nil {
			t.Fatal(err)
		}
	}
	newest := old.Add(48 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "deny_domain.map"), newest, newest); err != nil {
		t.Fatal(err)
	}
	if got := l.LastWritten(); !got.Equal(newest) {
		t.Errorf("LastWritten = %v, want %v", got, newest)
	}

	// A file that is not one of the six does not count.
	other := newest.Add(time.Hour)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "notes.txt"), other, other); err != nil {
		t.Fatal(err)
	}
	if got := l.LastWritten(); !got.Equal(newest) {
		t.Errorf("LastWritten counts a stray file: %v", got)
	}
}

// How often an entry decided a message is read off the journal: the list that
// decided, and whether the entry fits who the message was from.
func TestListsUsage(t *testing.T) {
	l, st, _ := testLists(t)
	add := func(list, kind, value string) int64 {
		t.Helper()
		e, err := l.Add(list, kind, value, "")
		if err != nil {
			t.Fatalf("Add(%s, %s, %s): %v", list, kind, value, err)
		}
		return e.ID
	}
	denyDomain := add(store.SpamListDeny, store.SpamEntryDomain, "evil.example")
	denyAddr := add(store.SpamListDeny, store.SpamEntryAddress, "spammer@evil.example")
	denyNet := add(store.SpamListDeny, store.SpamEntryIP, "198.51.100.0/24")
	denyV6 := add(store.SpamListDeny, store.SpamEntryIP, "2001:db8::/32")
	allowAddr := add(store.SpamListAllow, store.SpamEntryAddress, "billing@vendor.example")
	allowDomain := add(store.SpamListAllow, store.SpamEntryDomain, "vendor.example")
	allowIP := add(store.SpamListAllow, store.SpamEntryIP, "203.0.113.5")
	unused := add(store.SpamListAllow, store.SpamEntryDomain, "quiet.example")

	const denied = "5.7.1 Refused by this server's deny list"
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	hour := 0
	row := func(from, ip, decision, engine, detail string) {
		t.Helper()
		hour++
		err := st.InsertInboundSpam(store.InboundSpamEntry{
			InboundDomain: "in.example.com", To: "a@in.example.com", From: from, ClientIP: ip,
			Decision: decision, Engine: engine, Detail: detail, At: at.Add(time.Duration(hour) * time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// The rows below are composed by hand. Their replies and verdicts follow
	// rspamd 3.11.1's forms: "Spam message rejected" is the proxy's default
	// reject_message (conf/worker-proxy.inc).
	// hour 1: the address and its domain are both on the deny list, and both fit.
	row("Spammer@Evil.Example", "192.0.2.9", store.SpamDecisionReject, store.SpamEngineRspamd, denied)
	// hour 2: another address of the domain.
	row("other@evil.example", "192.0.2.9", store.SpamDecisionReject, store.SpamEngineRspamd, denied)
	// hour 3, 4: the networks, v4 (as Postfix may log it, mapped) and v6.
	row("x@elsewhere.example", "::ffff:198.51.100.77", store.SpamDecisionReject, store.SpamEngineRspamd, denied)
	row("x@elsewhere.example", "2001:db8::1", store.SpamDecisionReject, store.SpamEngineRspamd, denied)
	// hour 5: a subdomain is not the domain.
	row("x@sub.evil.example", "192.0.2.9", store.SpamDecisionReject, store.SpamEngineRspamd, denied)
	// hour 6: allowed by its address and its domain - two symbols, two entries.
	row("billing@vendor.example", "192.0.2.50", store.SpamDecisionAccept, store.SpamEngineRspamd,
		"no action, score -199.00 / 15.00; SELFPOST_ALLOW_DOMAIN(-100.00) SELFPOST_ALLOW_FROM(-100.00)")
	// hour 7: allowed by its domain alone.
	row("accounts@vendor.example", "192.0.2.50", store.SpamDecisionAccept, store.SpamEngineRspamd,
		"no action, score -99.00 / 15.00; SELFPOST_ALLOW_DOMAIN(-100.00)")
	// hour 8: allowed by the client address; the sender's domain is not why.
	row("x@quiet.example", "203.0.113.5", store.SpamDecisionAccept, store.SpamEngineRspamd,
		"no action, score -99.00 / 15.00; SELFPOST_ALLOW_IP(-100.00)")
	// Nobody's: the filter's own refusal, an accept with no list symbol, a
	// refusal by Postfix, and a sender who spells the mark in his own address.
	row("spammer@evil.example", "192.0.2.9", store.SpamDecisionReject, store.SpamEngineRspamd, "5.7.1 Spam message rejected")
	row("billing@vendor.example", "192.0.2.50", store.SpamDecisionAccept, store.SpamEngineRspamd, "no action, score 0.10 / 15.00")
	row("spammer@evil.example", "198.51.100.1", store.SpamDecisionReject, store.SpamEnginePostfix, "550 5.1.1 <nobody@in.example.com>: this server's deny list")
	row("billing@vendor.example", "203.0.113.5", store.SpamDecisionAccept, store.SpamEngineSelfPost, "")

	usage, err := l.Usage()
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	for name, c := range map[string]struct {
		id   int64
		hits int
		last int // hour
	}{
		"deny domain": {denyDomain, 2, 2}, "deny address": {denyAddr, 1, 1},
		"deny network": {denyNet, 1, 3}, "deny v6 network": {denyV6, 1, 4},
		"allow address": {allowAddr, 1, 6}, "allow domain": {allowDomain, 2, 7}, "allow ip": {allowIP, 1, 8},
		"unused": {unused, 0, 0},
	} {
		got := usage[c.id]
		wantLast := time.Time{}
		if c.hits > 0 {
			wantLast = at.Add(time.Duration(c.last) * time.Hour)
		}
		if got.Hits != c.hits || !got.Last.Equal(wantLast) {
			t.Errorf("%s: %d hits, last %v; want %d, %v", name, got.Hits, got.Last, c.hits, wantLast)
		}
	}
}

func TestListsUsageWithNoEntries(t *testing.T) {
	l, _, _ := testLists(t)
	if usage, err := l.Usage(); err != nil || len(usage) != 0 {
		t.Errorf("Usage = %v, %v", usage, err)
	}
}

// The same address written as a network of one is the same entry.
func TestListsOneAddressNetworkIsTheAddress(t *testing.T) {
	l, _, dir := testLists(t)
	if _, err := l.Add(store.SpamListDeny, store.SpamEntryIP, "192.0.2.7", ""); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := l.Add(store.SpamListDeny, store.SpamEntryIP, "192.0.2.7/32", ""); !errors.Is(err, store.ErrSpamListEntryExists) {
		t.Errorf("192.0.2.7/32 after 192.0.2.7: %v, want ErrSpamListEntryExists", err)
	}
	if got := strings.Join(mapLines(t, dir, "deny_ip.map"), " "); got != "192.0.2.7" {
		t.Errorf("deny_ip.map = %q", got)
	}
}

func TestListable(t *testing.T) {
	for value, want := range map[string]bool{
		"billing@vendor.example": true, "srs0=ab=cd=example.org=user@forwarder.example": true,
		"o'brien@example.org": false, "a#b@example.org": false, "": false, "not-an-address": false,
	} {
		if got := Listable(store.SpamEntryAddress, value); got != want {
			t.Errorf("Listable(address, %q) = %v, want %v", value, got, want)
		}
	}
}

// The key the filter signs with is made once and then read, and is readable by
// the filter, which is another user in another container.
func TestFilterKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "antispam")
	key, err := FilterKey(dir)
	if err != nil || len(key) != 64 || strings.Trim(key, "0123456789abcdef") != "" {
		t.Fatalf("FilterKey = %q, %v", key, err)
	}
	again, err := FilterKey(dir)
	if err != nil || again != key {
		t.Errorf("a second call = %q, %v; want the same key", again, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, FilterKeyFile))
	if err != nil || string(raw) != key+"\n" {
		t.Errorf("file = %q, %v", raw, err)
	}
	if info, _ := os.Stat(filepath.Join(dir, FilterKeyFile)); os.PathSeparator == '/' && info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %o", info.Mode().Perm())
	}

	// A file that is not a key is replaced, not signed with.
	if err := os.WriteFile(filepath.Join(dir, FilterKeyFile), []byte("short\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh, err := FilterKey(dir)
	if err != nil || len(fresh) != 64 || fresh == key {
		t.Errorf("after a damaged file: %q, %v", fresh, err)
	}

	// The lists leave it alone.
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := NewLists(st, dir).Sync(); err != nil {
		t.Fatal(err)
	}
	if kept, _ := FilterKey(dir); kept != fresh {
		t.Errorf("the key changed when the lists were written")
	}
}
