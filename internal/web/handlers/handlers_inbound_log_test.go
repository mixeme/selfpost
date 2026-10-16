package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/inbound"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
)

// Inbound › Log and Inbound › Filter lists, rendered from the real journal and
// lists by the handlers. Their markup is held to the mockups by the guards in
// internal/web/view; what is checked here is what the handlers read and who may.

// inboundLogStand is an inbound relay with two domains, a journal row for each
// and the lists over a directory the test can read the map files from.
type inboundLogStand struct {
	h    *Handlers
	st   *store.Store
	dir  string
	a, b store.InboundDomain
}

func newInboundLogStand(t *testing.T) inboundLogStand {
	t.Helper()
	h, st := inboundHandlers(t)
	s := inboundLogStand{h: h, st: st, dir: filepath.Join(t.TempDir(), "antispam")}
	h.lists = inbound.NewLists(st, s.dir)
	var err error
	if s.a, err = st.AddInboundDomain("a.example.com"); err != nil {
		t.Fatal(err)
	}
	if s.b, err = st.AddInboundDomain("b.example.com"); err != nil {
		t.Fatal(err)
	}
	s.row("a.example.com", "alice@a.example.com", "Hello A", store.SpamDecisionAccept, store.SpamEngineRspamd)
	s.row("b.example.com", "bob@b.example.com", "Hello B", store.SpamDecisionAccept, store.SpamEngineRspamd)
	return s
}

func (s inboundLogStand) row(domain, to, subject, decision, engine string) {
	s.rowAt(domain, to, subject, decision, engine, time.Time{})
}

func (s inboundLogStand) rowAt(domain, to, subject, decision, engine string, at time.Time) {
	err := s.st.InsertInboundSpam(store.InboundSpamEntry{
		InboundDomain: domain, ClientIP: "203.0.113.9", From: "sender@example.net", To: to, Subject: subject,
		Decision: decision, Engine: engine, Detail: "detail of " + subject, At: at,
	})
	if err != nil {
		panic(err)
	}
}

// get runs a handler over a GET as p.
func (s inboundLogStand) get(h http.HandlerFunc, target string, p auth.Principal) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h(rec, auth.RequestWithPrincipal(httptest.NewRequest(http.MethodGet, target, nil), p))
	return rec
}

func (s inboundLogStand) owner(t *testing.T, username string, domains ...store.InboundDomain) auth.Principal {
	t.Helper()
	var ids []int64
	for _, d := range domains {
		ids = append(ids, d.ID)
	}
	return userWith(t, s.st, username, store.RoleDomain, store.Reach{InboundDomainIDs: ids})
}

// The global role reads the whole journal, is offered every domain, and is given
// the way to correct a decision and to change how long rows are kept.
func TestInboundLogForTheGlobalRole(t *testing.T) {
	s := newInboundLogStand(t)
	s.row("a.example.com", "carol@a.example.com", "Quarantined", store.SpamDecisionQuarantine, store.SpamEngineRspamd)
	if err := s.st.SetInboundSpamLogRetentionDays(30); err != nil {
		t.Fatal(err)
	}
	rec := s.get(s.h.HandleInboundLog, "/inbound/log", globalPrincipal)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /inbound/log = %d:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	has(t, "log", body,
		`<h1 class="title is-3">Inbound log</h1>`,
		`Rows older than 30 days are deleted — change that in <a href="/server/settings">Settings</a>.`,
		`alice@<wbr>a.example.com`, `bob@<wbr>b.example.com`, `Hello A`, `Hello B`,
		`<option value="a.example.com">a.example.com</option>`, `<option value="b.example.com">b.example.com</option>`,
		`class="tag is-success is-light" popovertarget="why-`, `class="tag is-primary is-light" popovertarget="why-`,
		`<span class="sp-mono sp-muted sp-small">203.0.113.9</span>`,
		`<tr><th>Decided by</th><td>rspamd</td></tr><tr><th>Verdict</th><td>detail of Hello A</td></tr>`,
		`<a href="/inbound/filter-lists?list=deny&amp;kind=address&amp;value=sender%40example.net">Deny sender</a>`,
		`<a href="/inbound/filter-lists?list=allow&amp;kind=address&amp;value=sender%40example.net">Allow sender</a>`,
		`Page 1 of 1`, `Allow / Deny sender opens <a href="/inbound/filter-lists">Filter lists</a>`,
	)
	// Newest first: the last row written is the first on the page.
	if strings.Index(body, `Quarantined`) > strings.Index(body, `Hello B`) || strings.Index(body, `Hello B`) > strings.Index(body, `Hello A`) {
		t.Error("the rows are not newest first")
	}
	lacks(t, "log", body, `data-poll`, `hx-get`)
}

// A domain administrator reads the rows of the inbound domains assigned to them,
// is offered those domains alone, and is not given the lists or the setting.
func TestInboundLogForADomainAdministrator(t *testing.T) {
	s := newInboundLogStand(t)
	p := s.owner(t, "owns-a", s.a)
	body := s.get(s.h.HandleInboundLog, "/inbound/log", p).Body.String()
	has(t, "log", body, `Hello A`, `<option value="a.example.com">a.example.com</option>`, `Rows older than 90 days are deleted.`)
	lacks(t, "log", body, `Hello B`, `b.example.com`, `href="/server/settings"`, `/inbound/filter-lists`,
		`Deny sender`, `Allow sender`, `Allow / Deny`)

	// A domain filter outside their reach is dropped, not honoured.
	body = s.get(s.h.HandleInboundLog, "/inbound/log?domain=b.example.com", p).Body.String()
	has(t, "log", body, `Hello A`)
	lacks(t, "log", body, `Hello B`, `b.example.com`)

	// One with All sees every domain, one with none sees no log at all.
	all := userWith(t, s.st, "owns-all", store.RoleDomain, store.Reach{AllInbound: true})
	body = s.get(s.h.HandleInboundLog, "/inbound/log", all).Body.String()
	has(t, "log", body, `Hello A`, `Hello B`)
	lacks(t, "log", body, `Deny sender`)

	outboundOnly := userWith(t, s.st, "outbound-only", store.RoleDomain, store.Reach{AllDomains: true})
	if rec := s.get(s.h.HandleInboundLog, "/inbound/log", outboundOnly); rec.Code != http.StatusNotFound {
		t.Errorf("an outbound-only administrator got %d, want 404", rec.Code)
	}
}

// A domain administrator whose assigned domain has been deleted since has nothing
// to read, not everything.
func TestInboundLogOfADeletedDomainIsEmptyForItsAdministrator(t *testing.T) {
	s := newInboundLogStand(t)
	p := s.owner(t, "owns-a", s.a)
	both := s.owner(t, "owns-both", s.a, s.b)
	if err := s.st.DeleteInboundDomain(s.a.ID); err != nil {
		t.Fatal(err)
	}
	body := s.get(s.h.HandleInboundLog, "/inbound/log", p).Body.String()
	has(t, "log", body, `No messages logged yet.`)
	lacks(t, "log", body, `Hello A`, `Hello B`)
	body = s.get(s.h.HandleInboundLog, "/inbound/log", both).Body.String()
	has(t, "log", body, `Hello B`)
	lacks(t, "log", body, `Hello A`)
	// The global role still reads what the deleted domain left in the journal.
	has(t, "log", s.get(s.h.HandleInboundLog, "/inbound/log", globalPrincipal).Body.String(), `Hello A`, `Hello B`)
}

func TestInboundLogFilters(t *testing.T) {
	s := newInboundLogStand(t)
	day := func(d, h int) time.Time { return time.Date(2026, 9, d, h, 0, 0, 0, time.UTC) }
	s.rowAt("a.example.com", "r1@a.example.com", "Rejected on the 10th", store.SpamDecisionReject, store.SpamEnginePostfix, day(10, 23))
	s.rowAt("a.example.com", "r2@a.example.com", "Rejected on the 11th", store.SpamDecisionReject, store.SpamEnginePostfix, day(11, 0))
	s.rowAt("b.example.com", "r3@b.example.com", "Tempfail on the 11th", store.SpamDecisionTempfail, store.SpamEngineRspamd, day(11, 23))
	s.rowAt("a.example.com", "r4@a.example.com", "Accepted on the 12th", store.SpamDecisionAccept, store.SpamEngineRspamd, day(12, 0))

	rows := func(query string) string {
		t.Helper()
		rec := s.get(s.h.HandleInboundLog, "/inbound/log"+query, globalPrincipal)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /inbound/log%s = %d", query, rec.Code)
		}
		var got []string
		body := rec.Body.String()
		for _, n := range []string{"10th", "11th", "12th"} {
			for _, kind := range []string{"Rejected", "Tempfail", "Accepted"} {
				if strings.Contains(body, kind+" on the "+n) {
					got = append(got, kind[:1]+n[:2])
				}
			}
		}
		return strings.Join(got, " ")
	}
	for query, want := range map[string]string{
		"?decision=reject":               "R10 R11",
		"?domain=b.example.com":          "T11",
		"?from=2026-09-11&to=2026-09-11": "R11 T11",
		"?from=2026-09-12":               "A12",
		"?to=2026-09-10":                 "R10",
		"?domain=a.example.com&decision=reject&from=2026-09-11": "R11",
		// A value that is not one the page offers is ignored.
		"?decision=bounce":                           "R10 R11 T11 A12",
		"?from=yesterday&to=2026-13-45":              "R10 R11 T11 A12",
		"?domain=nobody.example.com":                 "R10 R11 T11 A12",
		"?decision=reject%27+OR+%271%27%3D%271":      "R10 R11 T11 A12",
		"?domain=a.example.com&domain=b.example.com": "R10 R11 A12",
	} {
		if got := rows(query); got != want {
			t.Errorf("GET /inbound/log%s shows %q, want %q", query, got, want)
		}
	}

	// The choice made is the one shown, and the page leads on with it.
	body := s.get(s.h.HandleInboundLog, "/inbound/log?domain=a.example.com&decision=reject&from=2026-09-11&to=2026-09-12", globalPrincipal).Body.String()
	has(t, "log", body, `<option value="a.example.com" selected>`, `<option value="reject" selected>`,
		`name="from" value="2026-09-11"`, `name="to" value="2026-09-12"`)
	body = s.get(s.h.HandleInboundLog, "/inbound/log?decision=quarantine", globalPrincipal).Body.String()
	has(t, "log", body, `No messages match these filters.`)
}

func TestInboundLogPages(t *testing.T) {
	s := newInboundLogStand(t)
	for i := 0; i < inboundLogPageSize; i++ {
		s.row("a.example.com", fmt.Sprintf("u%d@a.example.com", i), fmt.Sprintf("Message %02d", i), store.SpamDecisionAccept, store.SpamEngineRspamd)
	}
	// 51 rows with the one the stand wrote: 50 and 1.
	first := s.get(s.h.HandleInboundLog, "/inbound/log?domain=a.example.com", globalPrincipal).Body.String()
	has(t, "page 1", first, `Page 1 of 2`, `<a href="/inbound/log?domain=a.example.com&amp;p=2">Older →</a>`, `Message 49`, `Message 00`)
	lacks(t, "page 1", first, `← Newer`, `Hello A`)
	if got := strings.Count(first, `<tr><td`); got != inboundLogPageSize {
		t.Errorf("page 1 has %d rows, want %d", got, inboundLogPageSize)
	}

	second := s.get(s.h.HandleInboundLog, "/inbound/log?domain=a.example.com&p=2", globalPrincipal).Body.String()
	has(t, "page 2", second, `Page 2 of 2`, `<a href="/inbound/log?domain=a.example.com">← Newer</a>`, `Hello A`)
	lacks(t, "page 2", second, `Older →`, `Message 49`, `Message 00`)
	if got := strings.Count(second, `<tr><td`); got != 1 {
		t.Errorf("page 2 has %d rows, want 1", got)
	}

	// A page past the end, or nonsense, is the last page, or the first.
	has(t, "page 9", s.get(s.h.HandleInboundLog, "/inbound/log?domain=a.example.com&p=9", globalPrincipal).Body.String(), `Page 2 of 2`, `Hello A`)
	has(t, "page x", s.get(s.h.HandleInboundLog, "/inbound/log?domain=a.example.com&p=x", globalPrincipal).Body.String(), `Page 1 of 2`)
	has(t, "page huge", s.get(s.h.HandleInboundLog, "/inbound/log?domain=a.example.com&p=99999999999999999999", globalPrincipal).Body.String(), `Page 2 of 2`, `Hello A`)
	has(t, "page 0", s.get(s.h.HandleInboundLog, "/inbound/log?domain=a.example.com&p=0", globalPrincipal).Body.String(), `Page 1 of 2`)
}

func TestInboundLogEmptyAndOff(t *testing.T) {
	h, _ := inboundHandlers(t)
	s := inboundLogStand{h: h}
	body := s.get(h.HandleInboundLog, "/inbound/log", globalPrincipal).Body.String()
	has(t, "empty log", body, `No messages logged yet.`)
	lacks(t, "empty log", body, `<table`, `Page 1`)
	body = s.get(h.HandleInboundLog, "/inbound/log?p=5", globalPrincipal).Body.String()
	has(t, "empty log, page 5", body, `No messages logged yet.`)
	lacks(t, "empty log, page 5", body, `<table`, `Page `)

	h.cfg.InboundEnabled = false
	if rec := s.get(h.HandleInboundLog, "/inbound/log", globalPrincipal); rec.Code != http.StatusNotFound {
		t.Errorf("with the inbound relay off GET /inbound/log = %d, want 404", rec.Code)
	}
}

// What a sender or a server wrote is text on the page.
func TestInboundLogEscapesWhatSendersWrote(t *testing.T) {
	s := newInboundLogStand(t)
	err := s.st.InsertInboundSpam(store.InboundSpamEntry{
		InboundDomain: "a.example.com", ClientIP: "203.0.113.9", From: `"><img src=x onerror=alert(1)>@e.example`,
		To: "x@a.example.com", Subject: `<script>alert(3)</script>`, Decision: store.SpamDecisionReject,
		Engine: store.SpamEnginePostfix, Detail: `550 <b>no</b>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := s.get(s.h.HandleInboundLog, "/inbound/log", globalPrincipal).Body.String()
	lacks(t, "log", body, `<script>alert`, `<img src=x`, `<b>no</b>`)
	has(t, "log", body, `&lt;script&gt;alert(3)&lt;/script&gt;`)
}

// ---- Filter lists

func (s inboundLogStand) post(h http.HandlerFunc, target string, form url.Values, p auth.Principal) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rest, ok := strings.CutPrefix(target, "/inbound/filter-lists/"); ok {
		id, _, _ := strings.Cut(rest, "/")
		req.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	h(rec, auth.RequestWithPrincipal(req, p))
	return rec
}

func (s inboundLogStand) mapFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func entry(list, kind, value, note string) url.Values {
	return url.Values{"list_type": {list}, "entry_type": {kind}, "value": {value}, "note": {note}}
}

// The lists are the global role's: everything about them is 404 to a domain
// administrator and while the inbound relay is off, and nothing is written.
func TestFilterListsAreForTheGlobalRoleOnly(t *testing.T) {
	s := newInboundLogStand(t)
	e, err := s.h.lists.Add("deny", "domain", "bulk.example", "")
	if err != nil {
		t.Fatal(err)
	}
	ops := s.owner(t, "owns-a", s.a)
	all := userWith(t, s.st, "owns-all", store.RoleDomain, store.Reach{AllInbound: true, AllDomains: true})
	for _, p := range []auth.Principal{ops, all} {
		if rec := s.get(s.h.HandleFilterLists, "/inbound/filter-lists", p); rec.Code != http.StatusNotFound {
			t.Errorf("GET as %s = %d, want 404", p.Username, rec.Code)
		}
		if rec := s.post(s.h.HandleFilterListAdd, "/inbound/filter-lists", entry("deny", "domain", "evil.example", ""), p); rec.Code != http.StatusNotFound {
			t.Errorf("POST as %s = %d, want 404", p.Username, rec.Code)
		}
		if rec := s.post(s.h.HandleFilterListDelete, "/inbound/filter-lists/"+itoa(e.ID)+"/delete", nil, p); rec.Code != http.StatusNotFound {
			t.Errorf("POST delete as %s = %d, want 404", p.Username, rec.Code)
		}
	}
	if list, _ := s.h.lists.Entries(); len(list) != 1 {
		t.Errorf("a domain administrator changed the lists: %+v", list)
	}
	if got := s.mapFile(t, "deny_domain.map"); strings.Contains(got, "evil.example") || !strings.Contains(got, "bulk.example") {
		t.Errorf("deny_domain.map = %q", got)
	}

	s.h.cfg.InboundEnabled = false
	if rec := s.get(s.h.HandleFilterLists, "/inbound/filter-lists", globalPrincipal); rec.Code != http.StatusNotFound {
		t.Errorf("with the inbound relay off GET = %d, want 404", rec.Code)
	}
	if rec := s.post(s.h.HandleFilterListAdd, "/inbound/filter-lists", entry("deny", "domain", "evil.example", ""), globalPrincipal); rec.Code != http.StatusNotFound {
		t.Errorf("with the inbound relay off POST = %d, want 404", rec.Code)
	}
	if rec := s.post(s.h.HandleFilterListDelete, "/inbound/filter-lists/"+itoa(e.ID)+"/delete", nil, globalPrincipal); rec.Code != http.StatusNotFound {
		t.Errorf("with the inbound relay off POST delete = %d, want 404", rec.Code)
	}

	// Nothing to write the lists to is nothing to show.
	s.h.cfg.InboundEnabled, s.h.lists = true, nil
	if rec := s.get(s.h.HandleFilterLists, "/inbound/filter-lists", globalPrincipal); rec.Code != http.StatusNotFound {
		t.Errorf("without lists GET = %d, want 404", rec.Code)
	}
}

func TestFilterListsAddAndRemove(t *testing.T) {
	s := newInboundLogStand(t)
	empty := s.get(s.h.HandleFilterLists, "/inbound/filter-lists", globalPrincipal).Body.String()
	has(t, "empty lists", empty, `<h1 class="title is-3">Filter lists</h1>`, `<h2>Deny</h2><div class="sp-end">0 entries · wins over Allow</div>`,
		`<h2>Allow</h2><div class="sp-end">0 entries</div>`, `No sender is turned away by hand.`, `No sender is let through by hand.`)
	lacks(t, "empty lists", empty, `<table`)

	// The domain is normalised, written to the file of its list and kind, and the
	// page shows it.
	rec := s.post(s.h.HandleFilterListAdd, "/inbound/filter-lists", entry("deny", "domain", "  @Bulk.Example ", "  newsletter  "), globalPrincipal)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/inbound/filter-lists?added=1" {
		t.Fatalf("add = %d to %q:\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if got := s.mapFile(t, "deny_domain.map"); !strings.Contains(got, "\nbulk.example\n") {
		t.Errorf("deny_domain.map = %q", got)
	}
	if got := s.mapFile(t, "allow_domain.map"); strings.Contains(got, "bulk.example") {
		t.Errorf("allow_domain.map = %q", got)
	}
	rec = s.post(s.h.HandleFilterListAdd, "/inbound/filter-lists", entry("allow", "ip", "198.51.100.77/24", ""), globalPrincipal)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("add ip = %d:\n%s", rec.Code, rec.Body.String())
	}
	if got := s.mapFile(t, "allow_ip.map"); !strings.Contains(got, "\n198.51.100.0/24\n") {
		t.Errorf("allow_ip.map = %q", got)
	}

	page := s.get(s.h.HandleFilterLists, "/inbound/filter-lists?added=1", globalPrincipal).Body.String()
	has(t, "lists", page,
		`<div class="notification is-success is-light">`, `Entry added.`,
		`<h2>Deny</h2><div class="sp-end">1 entry · wins over Allow</div>`, `<h2>Allow</h2><div class="sp-end">1 entry</div>`,
		`<td class="sp-mono sp-small">bulk.example</td><td><button type="button" class="tag is-light" popovertarget="entry-`,
		`<tr><th>Note</th><td>newsletter</td></tr><tr><th>Added</th><td class="sp-mono sp-small">`+time.Now().UTC().Format("2006-01-02")+`</td></tr><tr><th>Matched</th><td>never in the last 90 days</td></tr>`,
		`<td class="sp-mono sp-small">198.51.100.0/24</td>`, `<tr><th>Note</th><td>—</td></tr>`,
		`<td class="sp-muted sp-nowrap">never</td>`,
	)
	lacks(t, "lists", page, `No sender is turned away`)

	// Remove the deny entry: gone from the page and from the file, the allow
	// entry untouched.
	entries, _ := s.h.lists.Entries()
	var denyID int64
	for _, e := range entries {
		if e.List == "deny" {
			denyID = e.ID
		}
	}
	has(t, "lists", page, `action="/inbound/filter-lists/`+itoa(denyID)+`/delete"`)
	rec = s.post(s.h.HandleFilterListDelete, "/inbound/filter-lists/"+itoa(denyID)+"/delete", nil, globalPrincipal)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/inbound/filter-lists?removed=1" {
		t.Fatalf("delete = %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := s.mapFile(t, "deny_domain.map"); strings.Contains(got, "bulk.example") {
		t.Errorf("deny_domain.map still holds the entry: %q", got)
	}
	if got := s.mapFile(t, "allow_ip.map"); !strings.Contains(got, "198.51.100.0/24") {
		t.Errorf("allow_ip.map lost its entry: %q", got)
	}
	page = s.get(s.h.HandleFilterLists, "/inbound/filter-lists?removed=1", globalPrincipal).Body.String()
	has(t, "lists", page, `Entry removed.`, `<h2>Deny</h2><div class="sp-end">0 entries`)
	lacks(t, "lists", page, `bulk.example <span`)

	// An entry that is not there, or an id that is not one.
	for _, id := range []string{itoa(denyID), "9999", "x", "0", "-1"} {
		if rec := s.post(s.h.HandleFilterListDelete, "/inbound/filter-lists/"+id+"/delete", nil, globalPrincipal); rec.Code != http.StatusNotFound {
			t.Errorf("delete %q = %d, want 404", id, rec.Code)
		}
	}
}

// A refusal comes back with 422 on the same page, the message between the head
// and the form and what was typed kept in it; nothing is written.
func TestFilterListsRefusals(t *testing.T) {
	s := newInboundLogStand(t)
	if rec := s.post(s.h.HandleFilterListAdd, "/inbound/filter-lists", entry("deny", "address", "win@lottery.example", ""), globalPrincipal); rec.Code != http.StatusSeeOther {
		t.Fatalf("add = %d", rec.Code)
	}
	before := s.mapFile(t, "deny_from.map")

	for _, c := range []struct {
		name string
		form url.Values
		want string
	}{
		{"duplicate", entry("deny", "address", "WIN@lottery.example", "again"), "That entry is already on the list."},
		{"not an address", entry("deny", "address", "not-an-address", ""), "is not a valid email address"},
		{"not a network", entry("allow", "ip", "192.0.2.0/99", "kept note"), "is not a network in CIDR form"},
		{"no kind", entry("deny", "everything", "x", ""), "choose what the entry is"},
		{"no list", entry("maybe", "domain", "x.example", ""), "choose the allow or the deny list"},
		{"empty", entry("deny", "domain", "  ", ""), "domain is required"},
		{"long note", entry("deny", "domain", "x.example", strings.Repeat("n", 201)), "note must be at most 200 characters"},
	} {
		rec := s.post(s.h.HandleFilterListAdd, "/inbound/filter-lists", c.form, globalPrincipal)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d, want 422:\n%s", c.name, rec.Code, rec.Body.String())
			continue
		}
		body := rec.Body.String()
		has(t, c.name, body, `<div class="notification is-danger is-light">`, c.want, `value="`+c.form.Get("value")+`"`)
		if c.form.Get("list_type") == "allow" || c.form.Get("list_type") == "deny" {
			has(t, c.name, body, `<option value="`+c.form.Get("list_type")+`" selected>`)
		}
		if c.form.Get("note") != "" && len(c.form.Get("note")) < 100 {
			has(t, c.name, body, `value="`+c.form.Get("note")+`"`)
		}
	}
	if got := s.mapFile(t, "deny_from.map"); got != before {
		t.Errorf("a refused entry changed deny_from.map:\n%s", got)
	}
	if e, _ := s.h.lists.Entries(); len(e) != 1 {
		t.Errorf("a refused entry was stored: %+v", e)
	}

	// What was typed is escaped when it comes back.
	rec := s.post(s.h.HandleFilterListAdd, "/inbound/filter-lists", entry("deny", "address", `"><script>1</script>`, `<i>x</i>`), globalPrincipal)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("hostile entry = %d", rec.Code)
	}
	lacks(t, "refused form", rec.Body.String(), `<script>1`, `<i>x</i>`)
}

// The log's Allow / Deny sender links fill the form in. Only a list and a kind
// the form offers are taken; the value is text.
func TestFilterListsPrefillFromTheLog(t *testing.T) {
	s := newInboundLogStand(t)
	get := func(query string) string {
		rec := s.get(s.h.HandleFilterLists, "/inbound/filter-lists"+query, globalPrincipal)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", query, rec.Code)
		}
		return rec.Body.String()
	}
	body := get("?list=allow&kind=address&value=billing%40partner.example")
	has(t, "prefilled", body, `<option value="allow" selected>`, `<option value="address" selected>`,
		`id="value" name="value" value="billing@partner.example"`)
	lacks(t, "prefilled", body, `<option value="deny" selected>`)

	body = get("?list=allow&kind=domain&value=partner.example")
	has(t, "prefilled", body, `<option value="allow" selected>`, `<option value="domain" selected>`, `value="partner.example"`)

	// Unknown choices leave the form on its first; the value is still put in.
	body = get("?list=everything&kind=whatever&value=x%40y.example")
	has(t, "unknown choices", body, `<option value="deny" selected>`, `<option value="address" selected>`, `value="x@y.example"`)

	// The value cannot leave its attribute.
	body = get(`?value=` + url.QueryEscape(`"><script>alert(1)</script>`))
	lacks(t, "hostile value", body, `<script>alert`)
	has(t, "hostile value", body, `value="&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;"`)

	// Nothing in the query is a flash.
	lacks(t, "plain", get(""), `notification`)
}

// The log's links and the form agree: what the log links to is what the form
// accepts, for every kind of sender it can name.
func TestFilterListsAcceptWhatTheLogLinksTo(t *testing.T) {
	s := newInboundLogStand(t)
	log := s.get(s.h.HandleInboundLog, "/inbound/log", globalPrincipal).Body.String()
	has(t, "log", log, `href="/inbound/filter-lists?list=deny&amp;kind=address&amp;value=sender%40example.net"`)
	body := s.get(s.h.HandleFilterLists, "/inbound/filter-lists?list=deny&kind=address&value=sender%40example.net", globalPrincipal).Body.String()
	has(t, "form", body, `value="sender@example.net"`)
	if rec := s.post(s.h.HandleFilterListAdd, "/inbound/filter-lists", entry("deny", "address", "sender@example.net", ""), globalPrincipal); rec.Code != http.StatusSeeOther {
		t.Errorf("the entry the log leads to was refused: %d", rec.Code)
	}
}

// Box 04 says whether anything reads the lists and when they were last written.
func TestFilterListsWhereTheyGo(t *testing.T) {
	s := newInboundLogStand(t)

	// Filter off: the lists are kept, the box says nothing reads them, and the
	// files have never been written.
	body := s.get(s.h.HandleFilterLists, "/inbound/filter-lists", globalPrincipal).Body.String()
	has(t, "filter off", body, `<h2>Where the lists go</h2>`, `<span class="tag is-light">not applied</span>`,
		`Nothing — the spam filter is off. The lists are kept and can be edited; nothing reads them until <code>INBOUND_ANTISPAM_MILTER</code> is set.`,
		`<dt>Last written</dt><dd>never</dd>`, `Deny wins, whichever entries they are.`)
	lacks(t, "filter off", body, `is-success is-light">applied`, `rspamd, from map files`)

	// Filter on, and the files written.
	s.h.cfg.InboundAntispamMilter = "inet:antispam:11332"
	if err := s.h.lists.Sync(); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 9, 11, 8, 40, 0, 0, time.UTC)
	for _, name := range []string{"allow_ip.map", "allow_from.map", "allow_domain.map", "deny_ip.map", "deny_from.map", "deny_domain.map"} {
		if err := os.Chtimes(filepath.Join(s.dir, name), stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	body = s.get(s.h.HandleFilterLists, "/inbound/filter-lists", globalPrincipal).Body.String()
	has(t, "filter on", body, `<span class="tag is-success is-light">applied</span>`,
		`rspamd, from map files it rereads within seconds of a change`, `<dt>Last written</dt><dd>2026-09-11 08:40</dd>`,
		`/data/antispam/`)
	lacks(t, "filter on", body, `not applied`, `Nothing — the spam filter is off`, `INBOUND_ANTISPAM_MILTER`)
}

// ---- Settings and Help

func settingsWithInbound(t *testing.T, on bool) *Handlers {
	t.Helper()
	h, _ := settingsServer(t)
	h.cfg.InboundEnabled = on
	h.cfg.InboundSpamLogRetentionEnvDefault = 90
	return h
}

func TestSettingsShowsAndSavesTheInboundRetention(t *testing.T) {
	h := settingsWithInbound(t, true)
	body := getBody(t, h.HandleServerSettings, "/server/settings")
	has(t, "settings", body, `Keep outbound log rows, days`, `Keep inbound log rows, days`,
		`name="inbound_spam_log_retention_days"`, `value="90"`,
		`The inbound log also never holds more than 10 000 rows`)

	if err := h.store.SetInboundSpamLogRetentionDays(45); err != nil {
		t.Fatal(err)
	}
	has(t, "settings", getBody(t, h.HandleServerSettings, "/server/settings"), `name="inbound_spam_log_retention_days" type="number" min="7" max="365" step="1" value="45"`)

	rec := postFormAs(h.HandleServerSettings, globalPrincipal, "/server/settings", nil,
		url.Values{"send_log_retention_days": {"120"}, "inbound_spam_log_retention_days": {"200"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/server/settings?saved=1" {
		t.Fatalf("POST /server/settings = %d to %q:\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if got, _ := h.store.GetSendLogRetentionDays(90); got != 120 {
		t.Errorf("outbound retention = %d, want 120", got)
	}
	if got, _ := h.store.GetInboundSpamLogRetentionDays(90); got != 200 {
		t.Errorf("inbound retention = %d, want 200", got)
	}
	// The Inbound log's lead reads the same setting.
	if got := h.inboundSpamLogRetentionDays(); got != 200 {
		t.Errorf("inboundSpamLogRetentionDays = %d, want 200", got)
	}
}

func TestSettingsRefuseAnInboundRetentionOutOfRange(t *testing.T) {
	h := settingsWithInbound(t, true)
	for _, bad := range []string{"3", "366", "nine", ""} {
		rec := postFormAs(h.HandleServerSettings, globalPrincipal, "/server/settings", nil,
			url.Values{"send_log_retention_days": {"120"}, "inbound_spam_log_retention_days": {bad}})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("POST (inbound %q) = %d, want 400:\n%s", bad, rec.Code, rec.Body.String())
		}
		if bad == "3" && !strings.Contains(rec.Body.String(), "between 7 and 365") {
			t.Errorf("no range in the refusal:\n%s", rec.Body.String())
		}
		if bad == "nine" && !strings.Contains(rec.Body.String(), "Inbound log retention must be a whole number of days.") {
			t.Errorf("no sentence in the refusal:\n%s", rec.Body.String())
		}
		// The outbound value that was fine is shown as typed, the bad one as it was.
		has(t, "refused form", rec.Body.String(), `name="send_log_retention_days" type="number" min="7" max="365" step="1" value="120"`)
	}
	// Nothing was saved, not even the outbound value that was fine.
	if got, _ := h.store.GetSendLogRetentionDays(90); got != 90 {
		t.Errorf("a refused form changed the outbound retention to %d", got)
	}
	if got, _ := h.store.GetInboundSpamLogRetentionDays(90); got != 90 {
		t.Errorf("a refused form changed the inbound retention to %d", got)
	}
	// A bad outbound value refuses the whole form too.
	rec := postFormAs(h.HandleServerSettings, globalPrincipal, "/server/settings", nil,
		url.Values{"send_log_retention_days": {"3"}, "inbound_spam_log_retention_days": {"200"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST (outbound 3) = %d", rec.Code)
	}
	if got, _ := h.store.GetInboundSpamLogRetentionDays(90); got != 90 {
		t.Errorf("the inbound retention was saved by a refused form: %d", got)
	}
}

// With the relay off there is no inbound journal to keep: the field is not on the
// page and a value posted for it is not read.
func TestSettingsHaveNoInboundRetentionWithTheRelayOff(t *testing.T) {
	h := settingsWithInbound(t, false)
	body := getBody(t, h.HandleServerSettings, "/server/settings")
	has(t, "settings", body, `Keep outbound log rows, days`)
	lacks(t, "settings", body, `inbound_spam_log_retention_days`, `Keep inbound log rows`)

	rec := postFormAs(h.HandleServerSettings, globalPrincipal, "/server/settings", nil,
		url.Values{"send_log_retention_days": {"120"}, "inbound_spam_log_retention_days": {"3"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST = %d:\n%s", rec.Code, rec.Body.String())
	}
	if got, _ := h.store.GetSendLogRetentionDays(90); got != 120 {
		t.Errorf("outbound retention = %d, want 120", got)
	}
	if raw, _ := h.store.GetSetting(store.InboundSpamLogRetentionDaysKey); raw != "" {
		t.Errorf("an inbound retention was saved with the relay off: %q", raw)
	}
}

func TestHelpHasTheInboundLogAndFilterListsTopics(t *testing.T) {
	h, _ := inboundHandlers(t)
	rec := send(h.HandleHelp, &globalPrincipal, "GET", "/help", nil, nil)
	has(t, "help", rec.Body.String(),
		`id="inbound-log"`, `id="filter-lists"`, `href="#inbound-log"`, `href="#filter-lists"`,
		`With the inbound relay on there is a second number, for the Inbound log, with the same range.`)
	// Both pages' help links land on them.
	s := inboundLogStand{h: h, st: h.store, dir: filepath.Join(t.TempDir(), "antispam")}
	h.lists = inbound.NewLists(h.store, s.dir)
	has(t, "log", s.get(h.HandleInboundLog, "/inbound/log", globalPrincipal).Body.String(), `href="/help#inbound-log"`)
	has(t, "lists", s.get(h.HandleFilterLists, "/inbound/filter-lists", globalPrincipal).Body.String(), `href="/help#filter-lists"`)
}

// An entry's count comes from the journal: a row counts for the entry that
// fits it on the list that decided it.
func TestFilterListsCountMatchesFromTheJournal(t *testing.T) {
	s := newInboundLogStand(t)
	for _, e := range [][3]string{{"deny", "domain", "bulk.example"}, {"allow", "address", "billing@vendor.example"}, {"deny", "ip", "203.0.113.0/24"}} {
		if _, err := s.h.lists.Add(e[0], e[1], e[2], ""); err != nil {
			t.Fatalf("Add %v: %v", e, err)
		}
	}
	at := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	for i, row := range []store.InboundSpamEntry{
		{From: "promo@bulk.example", ClientIP: "198.51.100.9", Decision: store.SpamDecisionReject, Engine: store.SpamEngineRspamd, Detail: "5.7.1 Refused by this server's deny list"},
		{From: "other@BULK.example", ClientIP: "198.51.100.9", Decision: store.SpamDecisionReject, Engine: store.SpamEngineRspamd, Detail: "5.7.1 Refused by this server's deny list"},
		{From: "billing@vendor.example", ClientIP: "192.0.2.5", Decision: store.SpamDecisionAccept, Engine: store.SpamEngineRspamd, Detail: "no action, score -99.00 / 15.00; SELFPOST_ALLOW_FROM(-100.00)"},
		// refused by the filter for its own reasons: nobody's match
		{From: "promo@bulk.example", ClientIP: "198.51.100.9", Decision: store.SpamDecisionReject, Engine: store.SpamEngineRspamd, Detail: "5.7.1 Spam message rejected"},
	} {
		row.InboundDomain, row.To, row.At = "a.example.com", "alice@a.example.com", at.Add(time.Duration(i)*time.Hour)
		if err := s.st.InsertInboundSpam(row); err != nil {
			t.Fatal(err)
		}
	}
	page := s.get(s.h.HandleFilterLists, "/inbound/filter-lists", globalPrincipal).Body.String()
	has(t, "lists", page,
		`<tr><th>Matched</th><td>2 in the last 90 days</td></tr><tr><th>Last match</th><td class="sp-mono sp-small">2026-10-01 10:30</td></tr>`,
		`<td class="sp-nowrap">2</td>`,
		`<tr><th>Matched</th><td>1 in the last 90 days</td></tr><tr><th>Last match</th><td class="sp-mono sp-small">2026-10-01 11:30</td></tr>`,
		`<td class="sp-nowrap">1</td>`,
		// the network nobody came from
		`<td class="sp-muted sp-nowrap">never</td>`,
	)
	if got := strings.Count(page, `<td class="sp-muted sp-nowrap">never</td>`); got != 1 {
		t.Errorf("%d entries say never, want the one network", got)
	}
}
