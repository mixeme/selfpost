package inbound

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mixeme/selfpost/internal/atomicfile"
	"github.com/mixeme/selfpost/internal/configsafe"
	"github.com/mixeme/selfpost/internal/store"
)

// ListStore is the slice of the store the filter lists need. *store.Store
// satisfies it.
type ListStore interface {
	AddSpamListEntry(list, kind, value, note string) (store.SpamListEntry, error)
	ListSpamListEntries() ([]store.SpamListEntry, error)
	DeleteSpamListEntry(id int64) error
	ListSpamListHits(marks ...string) ([]store.SpamListHit, error)
}

// Lists owns the allow and deny lists of the external spam filter: the rows in
// SQLite and the map files the filter reads. SelfPost does not consult the
// lists itself - it writes them out, six files in one directory, one entry a
// line, and the filter (rspamd, deploy/antispam/rspamd/multimap.conf) rereads
// a file when it changes. Which list wins when both match is the filter's
// configuration: deny.
//
// rspamd 3.11.1, src/libserver/maps/map.c: a file map is watched with libev's
// ev_stat (inotify, or a stat poll every map_watch_interval x
// map_file_watch_multiplier = 30 s, conf/options.inc), so a change takes
// effect within that, not at once; docs.rspamd.com/configuration/maps asks
// for the atomic rename atomicfile.Write does.
type Lists struct {
	store ListStore
	dir   string
	// mu makes a change of the table and the rewrite of the files one step.
	// Two requests at once could otherwise write the files in the other order
	// than they changed the table - the first to read the entries being the
	// last to write them - and leave the filter with an entry the page no
	// longer lists. One Lists serves the panel, so a mutex is enough.
	mu sync.Mutex
}

// NewLists builds the filter lists over the store and the map directory
// (/data/antispam).
func NewLists(st ListStore, dir string) *Lists {
	return &Lists{store: st, dir: dir}
}

// noteMaxRunes caps the operator's comment on an entry.
const noteMaxRunes = 200

// mapFiles names the file of every (list, kind). An IP map and a sender map
// are different things to the filter, and a domain entry is matched against
// another part of the sender than an address, so each has its own file.
var mapFiles = map[[2]string]string{
	{store.SpamListAllow, store.SpamEntryIP}:      "allow_ip.map",
	{store.SpamListAllow, store.SpamEntryAddress}: "allow_from.map",
	{store.SpamListAllow, store.SpamEntryDomain}:  "allow_domain.map",
	{store.SpamListDeny, store.SpamEntryIP}:       "deny_ip.map",
	{store.SpamListDeny, store.SpamEntryAddress}:  "deny_from.map",
	{store.SpamListDeny, store.SpamEntryDomain}:   "deny_domain.map",
}

// Entries returns every entry of both lists.
func (l *Lists) Entries() ([]store.SpamListEntry, error) {
	return l.store.ListSpamListEntries()
}

// Add validates an entry, stores it normalised and rewrites the maps. A
// refusal is a *ValidationError; an entry already on that list is
// store.ErrSpamListEntryExists.
func (l *Lists) Add(list, kind, rawValue, rawNote string) (store.SpamListEntry, error) {
	if list != store.SpamListAllow && list != store.SpamListDeny {
		return store.SpamListEntry{}, invalid("choose the allow or the deny list")
	}
	value, err := normalizeListValue(kind, rawValue)
	if err != nil {
		return store.SpamListEntry{}, err
	}
	note, err := normalizeNote(rawNote)
	if err != nil {
		return store.SpamListEntry{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e, err := l.store.AddSpamListEntry(list, kind, value, note)
	if err != nil {
		return store.SpamListEntry{}, err
	}
	if err := l.sync(); err != nil {
		// An entry the filter never got is worse than no entry: the page
		// would show a rule that does nothing. The files are written again
		// after the row is taken back, since some of the six may already
		// hold it; if that fails too, the next change or start puts it right.
		_ = l.store.DeleteSpamListEntry(e.ID)
		_ = l.sync()
		return store.SpamListEntry{}, err
	}
	return e, nil
}

// Delete removes an entry and rewrites the maps. If the maps cannot be
// written the entry is put back: an entry that is gone from the page and
// still in the filter's file could not even be removed a second time.
func (l *Lists) Delete(id int64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, err := l.store.ListSpamListEntries()
	if err != nil {
		return err
	}
	var gone *store.SpamListEntry
	for i := range entries {
		if entries[i].ID == id {
			gone = &entries[i]
		}
	}
	if err := l.store.DeleteSpamListEntry(id); err != nil {
		return err
	}
	if err := l.sync(); err != nil {
		if gone != nil {
			_, _ = l.store.AddSpamListEntry(gone.List, gone.Kind, gone.Value, gone.Note)
		}
		return err
	}
	return nil
}

// Sync writes all six map files from SQLite, each atomically, so the filter
// reads a whole old file or a whole new one. Every file is written even when
// empty: the filter's configuration names all six.
func (l *Lists) Sync() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sync()
}

// sync is Sync for a caller that holds mu.
func (l *Lists) sync() error {
	entries, err := l.store.ListSpamListEntries()
	if err != nil {
		return err
	}
	lines := make(map[string][]string, len(mapFiles))
	for _, name := range mapFiles {
		lines[name] = nil
	}
	for _, e := range entries {
		name, ok := mapFiles[[2]string{e.List, e.Kind}]
		if !ok {
			return fmt.Errorf("filter list entry %d: unknown list %q or kind %q", e.ID, e.List, e.Kind)
		}
		// Values were validated on the way in; this is the writer's own guard
		// against a row that got into the table some other way. In a map file a
		// newline ends the entry, whitespace starts its value and # a comment
		// (rspamd 3.11.1, src/libserver/maps/map_helpers.c,
		// rspamd_parse_kv_list()). An IP map takes bare v4 and v6 addresses and
		// CIDR networks (docs.rspamd.com/configuration/maps, IP maps).
		if err := configsafe.Token("filter list entry", e.Value, " \t\r\n#"); err != nil {
			return err
		}
		lines[name] = append(lines[name], e.Value)
	}
	// The filter runs under another user in another container: the directory
	// and the files are readable by anyone. They hold addresses, not secrets.
	// (The rspamd image runs as uid 11333: rspamd-docker v3.11.1+3,
	// Dockerfile, USER 11333:11333.)
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", l.dir, err)
	}
	for name, vals := range lines {
		sort.Strings(vals)
		var b strings.Builder
		b.WriteString("# Written by SelfPost (Inbound > Filter lists). Do not edit: the next change in the panel replaces this file.\n")
		for _, v := range vals {
			b.WriteString(v)
			b.WriteByte('\n')
		}
		if err := atomicfile.Write(filepath.Join(l.dir, name), []byte(b.String()), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// LastWritten is when the maps were last written: the newest modification
// time of the six files, the zero time while none exists (the filter lists
// have never been touched, or the directory is gone).
func (l *Lists) LastWritten() time.Time {
	var newest time.Time
	for _, name := range mapFiles {
		if fi, err := os.Stat(filepath.Join(l.dir, name)); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return newest
}

// Listable reports whether value could be added to a list as an entry of kind.
// The Inbound log asks before it offers a row's sender to the lists: an
// envelope sender may be spelt in ways the lists do not take.
func Listable(kind, value string) bool {
	_, err := normalizeListValue(kind, value)
	return err == nil
}

// normalizeListValue validates one entry and returns the form it is stored and
// written in: lower case for an address or a domain, the canonical text of an
// address or a network for an IP.
func normalizeListValue(kind, raw string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch kind {
	case store.SpamEntryAddress:
		if v == "" {
			return "", invalid("sender address is required")
		}
		if len(v) > 254 {
			return "", invalid("sender address must be at most 254 characters")
		}
		at := strings.LastIndexByte(v, '@')
		if at <= 0 || at >= len(v)-1 {
			return "", invalid("%q is not a valid email address", v)
		}
		local, domain := v[:at], v[at+1:]
		if local[0] == '.' || local[len(local)-1] == '.' || len(local) > 64 {
			return "", invalid("%q: invalid local part", v)
		}
		for i := 0; i < len(local); i++ {
			c := local[i]
			lower := c >= 'a' && c <= 'z'
			digit := c >= '0' && c <= '9'
			// '=' for the rewritten senders of forwarders (SRS0=…).
			if !lower && !digit && !strings.ContainsRune("._+-=", rune(c)) {
				return "", invalid("%q: local part contains invalid characters", v)
			}
		}
		if err := checkDomain(domain); err != nil {
			return "", err
		}
		return v, nil
	case store.SpamEntryDomain:
		// "@example.com" is how a domain is often written in such a list.
		v = strings.TrimPrefix(v, "@")
		if err := checkDomain(v); err != nil {
			return "", err
		}
		return v, nil
	case store.SpamEntryIP:
		if v == "" {
			return "", invalid("address or network is required")
		}
		if strings.Contains(v, "/") {
			p, err := netip.ParsePrefix(v)
			if err != nil {
				return "", invalid("%q is not a network in CIDR form (e.g. 192.0.2.0/24)", v)
			}
			if p.Addr().Is4In6() {
				return "", invalid("%q: write an IPv4 network as IPv4 (e.g. 192.0.2.0/24)", v)
			}
			if p.Bits() == 0 {
				return "", invalid("%q is every address there is", v)
			}
			// A network of one address is that address: 192.0.2.7/32 and
			// 192.0.2.7 are one entry, not two that look different.
			if p.Bits() == p.Addr().BitLen() {
				return p.Addr().String(), nil
			}
			return p.Masked().String(), nil
		}
		a, err := netip.ParseAddr(v)
		if err != nil || a.Zone() != "" {
			return "", invalid("%q is not an IP address or a network in CIDR form", v)
		}
		return a.Unmap().String(), nil
	default:
		return "", invalid("choose what the entry is: a sender address, a sender domain or a client address")
	}
}

// normalizeNote trims the operator's comment and refuses what a single line of
// text cannot be.
func normalizeNote(raw string) (string, error) {
	note := strings.TrimSpace(raw)
	if utf8.RuneCountInString(note) > noteMaxRunes {
		return "", invalid("note must be at most %d characters", noteMaxRunes)
	}
	for _, r := range note {
		if r < 0x20 || r == 0x7f || r == utf8.RuneError {
			return "", invalid("note must be one line of plain text")
		}
	}
	return note, nil
}

// What a list leaves in the journal when it decides a message - the two ends
// of deploy/antispam/rspamd/multimap.conf this package depends on. A deny rule
// refuses with a message, which the log tailer files as the reply of a
// rejected row. An allow rule adds a symbol named after the kind of entry,
// which the journal milter keeps in the verdict of an accepted row.
//
// rspamd 3.11.1: the deny message reaches Postfix as the milter reply "554
// 5.7.1 <message>" (src/libserver/milter.c, rspamd_milter_send_task_results());
// src/plugins/lua/milter_headers.lua, routine x-spamd-result, writes each
// symbol as "%s(%.2f)[%s]" - name, score, options.
const (
	denyReplyMark = "this server's deny list"
	allowMark     = "SELFPOST_ALLOW_"
)

var allowSymbol = map[string]string{
	store.SpamEntryIP:      "SELFPOST_ALLOW_IP(",
	store.SpamEntryAddress: "SELFPOST_ALLOW_FROM(",
	store.SpamEntryDomain:  "SELFPOST_ALLOW_DOMAIN(",
}

// Usage is how often an entry decided a message, as far back as the journal
// reaches, and when it last did. The zero value is an entry that never did.
type Usage struct {
	Hits int
	Last time.Time
}

// Usage counts, for every entry, the journal rows it decided. Nothing is
// stored for this: the journal already says which list decided a message and
// who the message was from, and an entry is matched against that the way the
// filter matches it - an address whole, a domain as the sender's domain, an
// address or network against the client's. So the count reaches back as far
// as the journal does (its retention, or less when it is full), and a row
// counts for every entry of the deciding list that fits it: the journal does
// not say which of two fitting entries the filter met first.
//
// A counter kept with each entry would not depend on the journal; the plan
// (docs/plans/inbound-antispam-panel.md, Filter list usage) says when that is
// worth a migration.
func (l *Lists) Usage() (map[int64]Usage, error) {
	entries, err := l.store.ListSpamListEntries()
	if err != nil {
		return nil, err
	}
	out := make(map[int64]Usage, len(entries))
	if len(entries) == 0 {
		return out, nil
	}
	hits, err := l.store.ListSpamListHits(denyReplyMark, allowMark)
	if err != nil {
		return nil, err
	}
	type matcher struct {
		id     int64
		list   string
		kind   string
		value  string
		prefix netip.Prefix
	}
	matchers := make([]matcher, 0, len(entries))
	for _, e := range entries {
		m := matcher{id: e.ID, list: e.List, kind: e.Kind, value: e.Value}
		if e.Kind == store.SpamEntryIP {
			if p, err := netip.ParsePrefix(e.Value); err == nil {
				m.prefix = p
			} else if a, err := netip.ParseAddr(e.Value); err == nil {
				m.prefix = netip.PrefixFrom(a, a.BitLen())
			} else {
				continue // not an entry this package wrote
			}
		}
		matchers = append(matchers, m)
	}
	for _, h := range hits {
		from := strings.ToLower(h.From)
		domain := ""
		if i := strings.LastIndexByte(from, '@'); i >= 0 {
			domain = from[i+1:]
		}
		client, clientErr := netip.ParseAddr(h.ClientIP)
		client = client.Unmap()
		denied := h.Decision != store.SpamDecisionAccept && strings.Contains(h.Detail, denyReplyMark)
		for _, m := range matchers {
			switch m.list {
			case store.SpamListDeny:
				if !denied {
					continue
				}
			case store.SpamListAllow:
				if h.Decision != store.SpamDecisionAccept || !strings.Contains(h.Detail, allowSymbol[m.kind]) {
					continue
				}
			}
			fits := false
			switch m.kind {
			case store.SpamEntryAddress:
				fits = from == m.value
			case store.SpamEntryDomain:
				fits = domain == m.value
			case store.SpamEntryIP:
				fits = clientErr == nil && m.prefix.Contains(client)
			}
			if !fits {
				continue
			}
			u := out[m.id]
			u.Hits++
			if h.CreatedAt.After(u.Last) {
				u.Last = h.CreatedAt
			}
			out[m.id] = u
		}
	}
	return out, nil
}

// FilterKeyFile is the file, beside the list files, that holds the key the
// spam filter signs messages with.
const FilterKeyFile = "filter.key"

// FilterKey returns the key the spam filter signs each message with, creating
// it on first use: 32 random bytes as 64 hex characters on one line, in the
// directory the filter's container mounts.
//
// The filter is attached so that mail flows when it is down. Then nothing
// removes verdict headers a sender wrote himself, and the inbound journal
// would record his verdict as the filter's. So the filter signs each message
// with this key and the journal reads a verdict only next to a signature it
// can recompute (internal/milter, FilterSignature).
//
// The file is readable by anyone on the host, like the list files and for the
// same reason: the filter is another user in another container. What the key
// protects is the truth of a monitoring page, against senders on the internet;
// someone with a shell on the host is not who it is kept from.
func FilterKey(dir string) (string, error) {
	path := filepath.Join(dir, FilterKeyFile)
	if raw, err := os.ReadFile(path); err == nil {
		if key := strings.TrimSpace(string(raw)); len(key) >= 32 && !strings.ContainsAny(key, " \t\r\n") {
			return key, nil
		}
		// Empty or cut short: not a key anyone should sign with. Replaced below.
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate the filter key: %w", err)
	}
	key := hex.EncodeToString(buf)
	if err := atomicfile.Write(path, []byte(key+"\n"), 0o644); err != nil {
		return "", err
	}
	return key, nil
}
