package view

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Tests of the component kit's own code: the template functions, each partial
// in each of its states, the shell's visibility rules and the agreement between
// kitPages and legacy_pages.txt. The design contract itself (vocabulary, outlines,
// the stylesheet) is held by the guard tests, which this file does not repeat.

func kitEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := New("9.9.9-test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	e.SetInboundEnabled(true)
	e.SetDMARCEnabled(true)
	return e
}

// partial executes one named partial of components.html with data.
func partial(t *testing.T, name string, data any) string {
	t.Helper()
	var buf bytes.Buffer
	if err := kitEngine(t).Page("components").ExecuteTemplate(&buf, name, data); err != nil {
		t.Fatalf("partial %s: %v", name, err)
	}
	return buf.String()
}

// squash removes the layout whitespace between tags, so a test can look for the
// markup without caring about the line breaks the template source carries.
func squash(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "")
}

func mustContain(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
}

func mustNotContain(t *testing.T, out string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(out, u) {
			t.Errorf("unexpected %q in:\n%s", u, out)
		}
	}
}

// status_tag is the one mapping of a status to Bulma's classes: the table of
// box 03 of the kit page. A word it does not know is neutral, never an error.
func TestStatusTagMapsEveryStatusOfTheKitTable(t *testing.T) {
	for status, want := range map[string]string{
		"ok": "tag is-success is-light", "published": "tag is-success is-light",
		"running": "tag is-success is-light", "sent": "tag is-success is-light",
		"warn": "tag is-warning is-light", "deferred": "tag is-warning is-light",
		"quiet": "tag is-warning is-light",
		"error": "tag is-danger is-light", "fail": "tag is-danger is-light",
		"mismatch": "tag is-danger is-light", "bounced": "tag is-danger is-light",
		"quarantine": "tag is-primary is-light", "global": "tag is-primary is-light",
		"off": "tag is-light", "unknown": "tag is-light",
	} {
		if got := StatusTag(status); got != want {
			t.Errorf("StatusTag(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestStatusTagTreatsAnUnknownStatusAsNeutral(t *testing.T) {
	for _, status := range []string{"", "rate-limited", "3", "DKIM", "something new"} {
		if got := StatusTag(status); got != "tag is-light" {
			t.Errorf("StatusTag(%q) = %q, want the neutral tag", status, got)
		}
	}
	// Case and surrounding space are not part of the word.
	if got := StatusTag("  Deferred "); got != "tag is-warning is-light" {
		t.Errorf("StatusTag(\"  Deferred \") = %q", got)
	}
}

// The kit page's status table is drawn from the function, so the words shown
// there cannot drift from what the function does.
func TestKitStatusTableAgreesWithStatusTag(t *testing.T) {
	for _, row := range KitPage().StatusRows {
		out := partial(t, "tag", row.Tag)
		mustContain(t, out, `class="`+StatusTag(row.Tag.Status)+`"`)
	}
}

func TestWbrAtBreaksAfterTheAtSignAndEscapesTheRest(t *testing.T) {
	if got, want := string(wbrAt("shop@shop.example.org")), "shop@<wbr>shop.example.org"; got != want {
		t.Errorf("wbrAt = %q, want %q", got, want)
	}
	got := string(wbrAt(`<b>"a"</b>@x&y`))
	if strings.Contains(got, "<b>") || !strings.Contains(got, "@<wbr>x&amp;y") {
		t.Errorf("wbrAt did not escape the address: %q", got)
	}
	if got := string(wbrAt("no-address")); got != "no-address" {
		t.Errorf("wbrAt changed text without an @: %q", got)
	}
}

// Both functions are reachable from a page template, which is how a table cell
// uses them.
func TestStatusTagAndWbrAtAreTemplateFunctions(t *testing.T) {
	funcs := templateFuncs()
	for _, name := range []string{"status_tag", "wbr_at"} {
		if _, ok := funcs[name]; !ok {
			t.Errorf("template function %s is missing", name)
		}
	}
}

func TestRichBuildsATextFromMixedParts(t *testing.T) {
	got := Rich("a ", Code("b"), Text{{Text: " c"}}, 4)
	if len(got) != 4 || got[0].Text != "a " || !got[1].Code || got[2].Text != " c" || got[3].Text != "4" {
		t.Errorf("Rich = %#v", got)
	}
}

func TestInlineRendersEveryKindAndEscapesText(t *testing.T) {
	out := partial(t, "inline", Rich(
		"1<2 ", Code("c"), Mono("m"), Strong("s"), Em("e"), Br(), Link("/x?a=1&b=2", "go"), TagOf("ok", "fine"),
		Inline{Text: "both", Href: "/y", Strong: true},
	))
	mustContain(t, out,
		"1&lt;2 ", "<code>c</code>", `<span class="sp-mono">m</span>`, "<strong>s</strong>", "<em>e</em>", "<br>",
		`<a href="/x?a=1&amp;b=2">go</a>`, `<span class="tag is-success is-light">fine</span>`,
		`<a href="/y"><strong>both</strong></a>`)
}

func TestTagLabelDefaultsToTheStatus(t *testing.T) {
	mustContain(t, partial(t, "tag", Tag{Status: "deferred"}), `<span class="tag is-warning is-light">deferred</span>`)
	mustContain(t, partial(t, "tag", Tag{Status: "ok", Label: "DKIM"}), `<span class="tag is-success is-light">DKIM</span>`)
}

func TestPostmarkLevels(t *testing.T) {
	for level, class := range map[string]string{
		LevelOK: `class="sp-postmark"`, "": `class="sp-postmark"`,
		LevelWarn: `class="sp-postmark sp-warn"`, LevelFail: `class="sp-postmark sp-fail"`,
	} {
		out := partial(t, "postmark", Postmark{Top: "DNS", Word: "FAIL", Bottom: "1 OF 3", Level: level})
		mustContain(t, out, class, "<span>DNS</span><b>FAIL</b><span>1 OF 3</span>")
	}
}

func TestPageHeadStates(t *testing.T) {
	// Everything a head can carry.
	out := squash(partial(t, "page_head", Head{
		Postmark: &Postmark{Top: "DNS", Word: "FAIL", Bottom: "1 OF 3 RECORDS", Level: LevelFail},
		Crumbs:   []Crumb{{Href: "/outbound/domains", Label: "Outbound domains"}, {Label: "Applications"}},
		Title:    "New password for", Code: "prod-server",
		Lead:    Rich("Lead with ", Code("code")),
		Note:    []string{"Updated 14:02:31 UTC", "refreshes every 10 s"},
		Actions: []Action{{Label: "Re-check DNS", Icon: "ti-refresh", Post: "/x/dns-recheck"}, {Label: "Add", Icon: "ti-plus", Href: "/x/new", Primary: true}, {Label: "Delete", Href: "/x/delete", Danger: true}},
	}))
	mustContain(t, out,
		`<div class="sp-head"><div class="sp-postmark sp-fail">`,
		`<p class="sp-kicker"><a href="/outbound/domains">Outbound domains</a> / Applications /</p>`,
		`<h1 class="title is-3">New password for <span class="sp-mono">prod-server</span></h1>`,
		`<p class="sp-lead">Lead with <code>code</code></p>`,
		`<div class="sp-muted sp-small">Updated 14:02:31 UTC<br>refreshes every 10 s</div>`,
		`<form method="post" action="/x/dns-recheck"><button type="submit" class="button"><i class="ti ti-refresh"></i>Re-check DNS</button></form>`,
		`<a class="button is-primary" href="/x/new"><i class="ti ti-plus"></i>Add</a>`,
		`<a class="button is-danger is-light" href="/x/delete">Delete</a>`)

	// The plainest head: a kicker, a title, a lead — and nothing else.
	out = squash(partial(t, "page_head", Head{Kicker: "Outbound", Title: "Outbound domains", Lead: Plain("Lead.")}))
	mustContain(t, out, `<p class="sp-kicker">Outbound</p>`, `<h1 class="title is-3">Outbound domains</h1>`)
	mustNotContain(t, out, "sp-postmark", `class="buttons"`, "sp-muted sp-small")

	// A message: a compact mono title and a route in the place of the lead.
	out = squash(partial(t, "page_head", Head{
		Title: "[FIRING] disk", Compact: true, Mono: true,
		Route: Route{{Text: "a@example.org"}, {Arrow: true}, {Text: "b@example.org"}, {Tag: &Tag{Status: "deferred"}}},
		Lead:  Plain("never shown beside a route"),
	}))
	mustContain(t, out, `<h1 class="title is-4 sp-mono">[FIRING] disk</h1>`,
		`<p class="sp-route"><span>a@example.org</span><i class="ti ti-arrow-right sp-muted"></i><span>b@example.org</span><span class="tag is-warning is-light">deferred</span></p>`)
	mustNotContain(t, out, "sp-lead")
}

func TestFlashSuccessAndError(t *testing.T) {
	mustContain(t, partial(t, "flash", Flash{Text: Rich(Strong("Saved"), " — done")}),
		`class="notification is-success is-light"`, `<i class="ti ti-circle-check"></i><strong>Saved</strong> — done`)
	mustContain(t, partial(t, "flash", Flash{Error: true, Text: Plain("It failed")}),
		`class="notification is-danger is-light"`, `<i class="ti ti-alert-circle"></i>It failed`)
}

func TestBoxOpensWithItsHeadInEveryVariant(t *testing.T) {
	out := squash(partial(t, "box_open", Box{No: "02", Title: "Applications", ID: "apps", End: Plain("note"),
		Help: &HelpLink{Href: "/help#apps", Title: "What an application is"}}))
	mustContain(t, out,
		`<div class="box" id="apps"><div class="sp-box-head"><span class="sp-no">02</span><h2>Applications</h2><div class="sp-end">note</div>`,
		`<a class="sp-help" href="/help#apps" title="What an application is"><i class="ti ti-help-circle"></i></a></div>`)

	mustContain(t, partial(t, "box_open", Box{No: "01", Title: "x", Variant: BoxDanger}), `<div class="box sp-danger-zone">`)
	out = partial(t, "box_open", Box{Icon: "ti-eye", Title: "Shown once", Variant: BoxCredential})
	mustContain(t, out, `<div class="box sp-credential">`, `<span class="sp-no"><i class="ti ti-eye"></i></span>`)
	mustNotContain(t, out, "id=")

	if got := partial(t, "box_close", nil); got != "</div>" {
		t.Errorf("box_close = %q", got)
	}
	if got := partial(t, "body_open", nil); got != `<div class="sp-box-body">` {
		t.Errorf("body_open = %q", got)
	}
}

func TestBoxHeadFilterFormReplacesTheEndSlot(t *testing.T) {
	out := squash(partial(t, "box_head", Box{No: "01", Title: "Messages", End: Plain("ignored"), Filter: &Filter{
		Action: "/outbound/log",
		Selects: []Select{{Name: "status", Label: "Status", Selected: "sent",
			Options: []Option{{Value: "", Label: "Any status"}, {Value: "sent", Label: "sent"}}}},
		Dates: []DateInput{{Name: "from", Label: "From date", Value: "2026-09-14"}},
	}}))
	mustContain(t, out,
		`<form class="sp-end" method="get" action="/outbound/log">`,
		`<div class="select"><select name="status" aria-label="Status"><option value="">Any status</option><option value="sent" selected>sent</option></select></div>`,
		`<input class="input" type="date" name="from" value="2026-09-14" aria-label="From date">`,
		`<button type="submit" class="button"><i class="ti ti-filter"></i>Filter</button></form>`)
	mustNotContain(t, out, "ignored")
}

func TestBoxFootVariants(t *testing.T) {
	mustContain(t, partial(t, "box_foot", Foot{Text: Plain("Page 1 of 9"), Link: &Anchor{Href: "/p?n=2", Label: "Older →"}, End: Plain("live")}),
		`<div class="sp-box-foot"><span>Page 1 of 9</span><a href="/p?n=2">Older →</a><span class="sp-end">live</span></div>`)
	mustContain(t, partial(t, "box_foot", Foot{Bare: true, Text: Plain("A legend.")}),
		`<div class="sp-box-foot">A legend.</div>`)
	mustContain(t, partial(t, "box_foot", Foot{Text: Plain("ignored"), Button: &Action{Label: "Done", Href: "/x", Primary: true}}),
		`<div class="sp-box-foot"><a class="button is-primary" href="/x">Done</a></div>`)
}

func TestHealthCards(t *testing.T) {
	out := squash(partial(t, "health_cards", []HealthCard{
		{Name: "Machine", Value: "CPU 12 %", Sub: "RAM 41 %", Icon: "ti-cpu", Href: "/server/health#machine"},
		{Name: "TLS certificate", Value: "Expires in 12 days", Sub: "renewal has not run", Icon: "ti-certificate", Href: "/server/health#certificate", Level: LevelWarn},
		{Name: "Sockets", Value: "1 of 2", Sub: "x", Icon: "ti-plug-connected-x", Href: "/server/health#sockets", Level: LevelFail},
	}))
	mustContain(t, out, `<div class="sp-health"><a href="/server/health#machine"><span class="sp-h-icon"><i class="ti ti-cpu"></i></span><span><span class="sp-h-name">Machine</span><span class="sp-h-value">CPU 12 %</span><span class="sp-h-sub">RAM 41 %</span></span></a>`,
		`<a href="/server/health#certificate" class="sp-warn">`, `<a href="/server/health#sockets" class="sp-fail">`)
}

func TestDNSRecordShowsInDNSNowOnlyOnAMismatch(t *testing.T) {
	ok := squash(partial(t, "dns_record", Record{
		Name: "DKIM", Status: Tag{Status: "published"}, Note: Plain("selector"),
		Host: "sp2026._domainkey.example.org", Type: "TXT", Value: "v=DKIM1; p=AAA", ValueRows: 3,
	}))
	mustContain(t, ok,
		`<div class="sp-record"><div class="sp-record-head"><h3>DKIM</h3><span class="tag is-success is-light">published</span><small>selector</small></div>`,
		`<div class="sp-row">`, `<input class="input" readonly value="sp2026._domainkey.example.org">`,
		`<label class="label">Type</label><div class="control"><input class="input" readonly value="TXT">`,
		`<textarea class="textarea" rows="3" readonly>v=DKIM1; p=AAA</textarea>`)
	mustNotContain(t, ok, "In DNS now", "is-danger")

	bad := squash(partial(t, "dns_record", Record{
		Name: "SPF", Status: Tag{Status: "mismatch"}, Host: "example.org", Type: "TXT", Value: "v=spf1 ip4:203.0.113.25 ~all",
		InDNS: "v=spf1 include:_spf.google.com ~all", Problem: Rich("Add ", Code("ip4:203.0.113.25"), " before ~all."),
	}))
	mustContain(t, bad, `<label class="label">In DNS now</label>`,
		`<input class="input is-danger" readonly value="v=spf1 include:_spf.google.com ~all">`,
		`<p class="help is-danger">Add <code>ip4:203.0.113.25</code> before ~all.</p>`,
		`<input class="input" readonly value="v=spf1 ip4:203.0.113.25 ~all">`)
}

func TestCopyFieldSingleLineAndMultiLine(t *testing.T) {
	one := squash(partial(t, "copy_field", CopyField{Label: "Login", Value: "prod-server", Help: Plain("Keep it.")}))
	mustContain(t, one,
		`<label class="label">Login</label>`,
		`<div class="field has-addons"><div class="control is-expanded"><input class="input" readonly value="prod-server"></div>`,
		`<button type="button" class="button" data-copy><i class="ti ti-copy"></i>Copy</button>`,
		`<p class="help">Keep it.</p>`)
	many := squash(partial(t, "copy_field", CopyField{Label: "Key", Value: "line <1>\nline 2", Rows: 2}))
	mustContain(t, many, `<textarea class="textarea" rows="2" readonly>line &lt;1&gt;`, "data-copy")
	mustNotContain(t, many, "<input", `class="help"`)
}

func TestRecordBlockWrapsASecretShownOnce(t *testing.T) {
	out := squash(partial(t, "record_open", nil) + partial(t, "copy_field", CopyField{Label: "Password", Value: "x"}) + partial(t, "record_close", nil))
	mustContain(t, out, `<div class="sp-record"><div class="field"><label class="label">Password</label>`)
	if !strings.HasSuffix(out, "</div></div>") {
		t.Errorf("record_close does not end the block: %s", out)
	}
}

func TestFactsBigMonoAndNote(t *testing.T) {
	out := squash(partial(t, "facts", []Fact{
		{Label: "Domain", Value: Rich(Link("/outbound/domains/1", "example.org"))},
		{Label: "Queue id", Value: Plain("4XcB7k2Jm9z1"), Mono: true},
		{Label: "Peak", Value: Plain("5 days"), Big: true, Note: Plain("a supporting line")},
	}))
	mustContain(t, out,
		`<dl class="sp-facts"><div><dt>Domain</dt><dd><a href="/outbound/domains/1">example.org</a></dd></div>`,
		`<dd class="sp-mono">4XcB7k2Jm9z1</dd>`,
		`<dd class="sp-big">5 days</dd><dd class="sp-muted">a supporting line</dd>`)
	if got := Facts("A", "1", "B", "2", "odd"); len(got) != 2 || got[1].Label != "B" {
		t.Errorf("Facts(pairs) = %#v", got)
	}
}

func TestSideMenu(t *testing.T) {
	out := squash(partial(t, "side_menu", SideMenu{Groups: []MenuGroup{
		{Label: "This domain", Items: []MenuItem{
			{Label: "DNS records", Icon: "ti-world-check", Href: "#dns", Active: true, Tag: &Tag{Status: "fail", Label: "1"}},
			{Label: "Connection", Icon: "ti-plug", Href: "#connection"},
		}},
		{Label: "Rarely changed", Items: []MenuItem{{Label: "Delete domain", Icon: "ti-trash", Href: "/outbound/domains/1/delete", Danger: true}}},
	}}))
	mustContain(t, out,
		`<aside class="menu sp-side"><p class="menu-label">This domain</p><ul class="menu-list">`,
		`<li><a class="is-active" href="#dns"><i class="ti ti-world-check"></i>DNS records <span class="tag is-danger is-light">1</span></a></li>`,
		`<li><a href="#connection"><i class="ti ti-plug"></i>Connection</a></li>`,
		`<li><a class="has-text-danger" href="/outbound/domains/1/delete"><i class="ti ti-trash has-text-danger"></i>Delete domain</a></li>`)
}

func TestTimelineLevels(t *testing.T) {
	out := squash(partial(t, "timeline", []Step{
		{Time: "13:58:07 UTC", Strong: "Accepted", Text: Rich(" from ", Mono("alerts"))},
		{Time: "13:58:09 UTC", Strong: "Deferred", Text: Plain(" — later"), Level: LevelWarn},
		{Time: "14:40:00 UTC", Strong: "Bounced", Level: LevelFail},
		{Time: "next attempt", Text: Plain("Still queued"), Level: LevelPending},
	}))
	mustContain(t, out,
		`<ol class="sp-timeline"><li><time>13:58:07 UTC</time><strong>Accepted</strong> from <span class="sp-mono">alerts</span></li>`,
		`<li class="sp-warn"><time>13:58:09 UTC</time><strong>Deferred</strong> — later</li>`,
		`<li class="sp-fail"><time>14:40:00 UTC</time><strong>Bounced</strong></li>`,
		`<li class="sp-pending"><time>next attempt</time>Still queued</li>`)
}

// The log is a pre: its lines are separated by a newline of their own, not by
// the template's layout whitespace, and a line is escaped.
func TestLogPaneIsAPreWithEscapedLines(t *testing.T) {
	out := partial(t, "log_pane", []LogLine{
		{Time: "13:58:07", Text: "message-id=<a81f0c@alertmanager>"},
		{Time: "13:58:09", Text: "status=deferred", Level: "warn"},
		{Text: "no time on this one", Level: "error"},
	})
	want := `<pre class="sp-log"><span class="sp-d">13:58:07</span> message-id=&lt;a81f0c@alertmanager&gt;` + "\n" +
		`<span class="sp-d">13:58:09</span> <span class="sp-w">status=deferred</span>` + "\n" +
		`<span class="sp-e">no time on this one</span></pre>`
	if out != want {
		t.Errorf("log_pane =\n%s\nwant\n%s", out, want)
	}
}

func TestEmptyStateConfirmListAndHelpTopic(t *testing.T) {
	mustContain(t, partial(t, "empty_state", EmptyState{Icon: "ti-stack-2", Text: Plain("Nothing waiting.")}),
		`<div class="sp-empty"><i class="ti ti-stack-2"></i>Nothing waiting.</div>`)
	mustContain(t, squash(partial(t, "confirm_list", []Text{Plain("the key"), Rich("every ", Code("login"))})),
		`<ul class="sp-list-check"><li><i class="ti ti-x"></i>the key</li><li><i class="ti ti-x"></i>every <code>login</code></li></ul>`)
	mustContain(t, partial(t, "help_topic", HelpTopic{ID: "dns", Title: "DNS records", Body: []Text{Plain("One."), Rich("Two ", Code("x"))}}),
		`<div class="sp-help-topic" id="dns"><h3>DNS records</h3><p>One.</p><p>Two <code>x</code></p></div>`)
	mustNotContain(t, partial(t, "help_topic", HelpTopic{Title: "t", Body: []Text{Plain("b")}}), "id=")
	mustContain(t, partial(t, "kicker", "Upper-case"), `<span class="sp-kicker">Upper-case</span>`)
}

// The kit page shows every partial: if one stops rendering, or a state is lost
// from kit.go, its marker disappears here.
func TestKitPageRendersEveryPartial(t *testing.T) {
	rec := httptest.NewRecorder()
	kitEngine(t).Render(rec, http.StatusOK, "components", KitPage())
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	for partial, marker := range map[string]string{
		"page_head": `class="sp-head"`, "postmark": `class="sp-postmark sp-warn"`, "box_head": `class="sp-box-head"`,
		"box_foot": `class="sp-box-foot"`, "flash": `class="notification is-danger is-light"`,
		"health_card": `class="sp-fail"><span class="sp-h-icon">`, "dns_record": `class="sp-record-head"`,
		"copy_field": `data-copy`, "facts": `class="sp-facts"`, "side_menu": `class="menu sp-side"`,
		"timeline": `class="sp-pending"`, "log_pane": `class="sp-log"`, "empty_state": `class="sp-empty"`,
		"confirm_list": `class="sp-list-check"`, "help_topic": `class="sp-help-topic"`, "route": `class="sp-route"`,
		"danger box": `class="box sp-danger-zone"`, "credential box": `class="box sp-credential"`,
		"in DNS now": `In DNS now`, "status_tag": `tag is-primary is-light`,
	} {
		if !strings.Contains(out, marker) {
			t.Errorf("the kit page does not show %s (looked for %s)", partial, marker)
		}
	}
	if n := strings.Count(out, `class="box`); n != 17 {
		t.Errorf("the kit page has %d boxes, the mockup components page has 17", n)
	}
}

func TestShellVisibilityFollowsRoleAndFlags(t *testing.T) {
	on := kitEngine(t)
	off, err := New("t")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name   string
		e      *Engine
		m      Meta
		want   Shell // only the flags and Home are compared
		wantIn bool
	}{
		{"global, everything on", on, Meta{IsGlobal: true},
			Shell{Home: "/overview", ShowOverview: true, ShowOutbound: true, ShowQueue: true, ShowDMARC: true, ShowInbound: true, ShowServer: true}, true},
		{"global, inbound and DMARC off", off, Meta{IsGlobal: true},
			Shell{Home: "/overview", ShowOverview: true, ShowOutbound: true, ShowQueue: true, ShowServer: true}, true},
		{"domain admin with both reaches", on, Meta{HasOutbound: true, HasInbound: true},
			Shell{Home: "/outbound/domains", ShowOutbound: true, ShowDMARC: true, ShowInbound: true}, true},
		{"domain admin, outbound only", on, Meta{HasOutbound: true},
			Shell{Home: "/outbound/domains", ShowOutbound: true, ShowDMARC: true}, true},
		{"domain admin, inbound only", on, Meta{HasInbound: true},
			Shell{Home: "/inbound/domains", ShowInbound: true}, true},
		{"domain admin, inbound only, feature off", off, Meta{HasInbound: true},
			Shell{Home: "/account"}, true},
		{"domain admin with nothing", on, Meta{}, Shell{Home: "/account"}, true},
	} {
		got := c.e.shell(c.m, "c", "s")
		got.Title, got.User, got.Section, got.Page, got.Version, got.Copyright, got.SourceURL = "", "", "", "", "", "", ""
		if got != c.want {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
	}
}

func renderShell(t *testing.T, e *Engine, m Meta) string {
	t.Helper()
	rec := httptest.NewRecorder()
	page := KitPage()
	page.Meta = m
	e.Render(rec, http.StatusOK, "components", page)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// The menu has the 2.0 entries and none of the ones that are not in 2.0 (plan
// § Routes): no Inbound Log, Queue, Quarantine or Filter lists, no Preflight.
func TestMenuHoldsTheTwoDotZeroEntries(t *testing.T) {
	out := renderShell(t, kitEngine(t), Meta{Title: "t", User: "admin", IsGlobal: true})
	for _, href := range []string{
		"/overview", "/outbound/domains", "/outbound/log", "/outbound/queue", "/outbound/dmarc",
		"/inbound/domains", "/server/health", "/server/log", "/server/backup", "/server/users", "/server/settings",
		"/account", "/help", "/license",
	} {
		mustContain(t, out, `href="`+href+`"`)
	}
	for _, gone := range []string{
		"/inbound/log", "/inbound/queue", "/inbound/quarantine", "/inbound/filter-lists", "/server/preflight",
		`href="/status"`, `href="/domains"`, `href="/users"`, `href="/settings"`,
	} {
		mustNotContain(t, out, gone)
	}
}

func TestMenuHighlightsTheCurrentGroupEntryAndSibling(t *testing.T) {
	out := renderShell(t, kitEngine(t), Meta{Title: "t", User: "admin", IsGlobal: true, Section: "outbound", Page: "log"})
	mustContain(t, out,
		`<div class="navbar-item has-dropdown is-hoverable sp-current">`+"\n"+`          <a class="navbar-link" href="/outbound/domains">`,
		`<a class="navbar-item is-active" href="/outbound/log">`,
		`<a href="/outbound/log" aria-current="page">`)
	if n := strings.Count(out, "sp-current"); n != 1 {
		t.Errorf("%d current groups, want 1", n)
	}
	if n := strings.Count(out, "aria-current"); n != 1 {
		t.Errorf("%d current siblings, want 1", n)
	}

	// The user menu is a group of its own and has no sibling strip.
	out = renderShell(t, kitEngine(t), Meta{Title: "t", User: "admin", IsGlobal: true, Section: "user", Page: "account"})
	mustContain(t, out, `<div class="navbar-item has-dropdown is-hoverable sp-current">`+"\n"+`          <a class="navbar-link" href="/account">`)
	mustNotContain(t, out, "sp-subnav")

	// Overview is a single link, not a dropdown.
	out = renderShell(t, kitEngine(t), Meta{Title: "t", User: "admin", IsGlobal: true, Section: "overview"})
	mustContain(t, out, `<a class="navbar-item sp-current" href="/overview">Overview</a>`)
	mustNotContain(t, out, "sp-subnav")
}

func TestMenuOfADomainAdministratorLeavesNoGap(t *testing.T) {
	out := renderShell(t, kitEngine(t), Meta{Title: "t", User: "ops", HasOutbound: true, Section: "outbound", Page: "domains"})
	mustContain(t, out, `href="/outbound/domains"`, `href="/outbound/log"`, `href="/outbound/dmarc"`, `href="/account"`)
	mustNotContain(t, out, `href="/overview"`, `href="/outbound/queue"`, `href="/inbound/domains"`, `href="/server/`)
	// The wordmark leads to what they can open.
	mustContain(t, out, `<a class="navbar-item" href="/outbound/domains"><img src="/static/wordmark.svg"`)

	// With DMARC off the entry goes too.
	e, _ := New("t")
	out = renderShell(t, e, Meta{Title: "t", User: "ops", HasOutbound: true})
	mustNotContain(t, out, "/outbound/dmarc")
}

// Signing out is a POST, as it is in the old layout and as the handler demands;
// it has to work without scripts.
func TestSignOutStaysAPostForm(t *testing.T) {
	out := renderShell(t, kitEngine(t), Meta{Title: "t", User: "admin", IsGlobal: true})
	mustContain(t, out, `<form method="post" action="/logout"><button type="submit" class="navbar-item"><i class="ti ti-logout"></i>Sign out</button></form>`)
	mustNotContain(t, out, `href="/logout"`)
}

func TestFooterShowsTheVersionOnlyWhenSignedIn(t *testing.T) {
	signedIn := renderShell(t, kitEngine(t), Meta{Title: "t", User: "admin", IsGlobal: true})
	mustContain(t, signedIn,
		`<footer class="sp-foot">SelfPost 9.9.9-test · Copyright © 2026 Mikhail Yenuchenko · <a href="/license">License (AGPL-3.0)</a> · <a href="https://github.com/mixeme/selfpost">Source</a> · No warranty</footer>`)

	// Signed out: the split screen, the notice, and no version, no navigation.
	out := renderShell(t, kitEngine(t), Meta{Title: "Sign in"})
	mustContain(t, out, `class="columns is-gapless sp-signin"`, `<img src="/static/logo.svg" alt="SelfPost">`,
		"Copyright © 2026 Mikhail Yenuchenko", `href="/license"`, `href="https://github.com/mixeme/selfpost"`, "No warranty")
	mustNotContain(t, out, "9.9.9-test", `class="navbar`, "sp-perf", "sp-subnav", `class="container sp-page"`)
}

// Every page of the kit loads exactly the three stylesheets and two scripts.
func TestKitLayoutLoadsOnlyTheKit(t *testing.T) {
	for _, m := range []Meta{{Title: "t", User: "admin", IsGlobal: true}, {Title: "Sign in"}} {
		out := renderShell(t, kitEngine(t), m)
		mustContain(t, out,
			`<link rel="stylesheet" href="/static/bulma.min.css">`+"\n"+`<link rel="stylesheet" href="/static/tabler-icons.css">`+"\n"+`<link rel="stylesheet" href="/static/panel.css">`,
			`<script src="/static/htmx.min.js" defer></script>`, `<script src="/static/panel.js" defer></script>`,
			`<meta name="htmx-config" content='{"includeIndicatorStyles":false}'>`)
		mustNotContain(t, out, "legacy.css", "http://", "https://cdn")
	}
}

// Meta can come from a struct that embeds it or from the map a handler builds.
func TestMetaOfReadsAStructAndAMap(t *testing.T) {
	want := Meta{Title: "t", User: "u", IsGlobal: true, HasOutbound: true, HasInbound: true, Section: "server", Page: "users"}
	if got := metaOf(&Kit{Meta: want}); got != want {
		t.Errorf("metaOf(struct) = %+v", got)
	}
	got := metaOf(map[string]any{"Title": "t", "User": "u", "IsGlobal": true, "HasOutbound": true, "HasInbound": true, "Section": "server", "Page": "users", "Other": 1})
	if got != want {
		t.Errorf("metaOf(map) = %+v", got)
	}
	if got := metaOf(42); got != (Meta{}) {
		t.Errorf("metaOf(other) = %+v", got)
	}
}

// A page is on the new layout exactly when legacy_pages.txt does not list it.
func TestKitPagesAreTheOnesOffTheRatchet(t *testing.T) {
	b, err := os.ReadFile("legacy_pages.txt")
	if err != nil {
		t.Fatal(err)
	}
	legacy := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "@") {
			legacy[line] = true
		}
	}
	for name := range pageFiles {
		if kitPages[name] == legacy[name] {
			t.Errorf("page %q: kitPages=%v and legacy_pages.txt lists it=%v; it must be in exactly one", name, kitPages[name], legacy[name])
		}
	}
	for name := range kitPages {
		if _, ok := pageFiles[name]; !ok {
			t.Errorf("kitPages names %q, which is not a page", name)
		}
	}
}

// The old pages keep rendering through the old layout and stylesheet.
func TestLegacyPagesStayOnTheOldLayout(t *testing.T) {
	e := kitEngine(t)
	rec := httptest.NewRecorder()
	e.Render(rec, http.StatusOK, "mail_queue", map[string]any{"Title": "t", "User": "admin", "IsGlobal": true})
	out := rec.Body.String()
	mustContain(t, out, `href="/static/legacy.css"`, `class="shell"`)
	mustNotContain(t, out, "bulma.min.css", "tabler-icons.css", "/static/panel.css", "navbar")
}

// Poll, ID and OOB are optional attributes: empty, box_open and page_head write
// exactly the markup they wrote before they existed.
func TestPollAndOOBAttributesAreAbsentWhenEmpty(t *testing.T) {
	if got := partial(t, "box_open", Box{No: "01", Title: "x", ID: "apps"}); !strings.HasPrefix(got, `<div class="box" id="apps">`+"\n") {
		t.Errorf("box_open without Poll starts %q", got[:40])
	}
	if got := partial(t, "box_open", Box{No: "01", Title: "x"}); !strings.HasPrefix(got, `<div class="box">`+"\n") {
		t.Errorf("box_open without ID or Poll starts %q", got[:40])
	}
	if got := partial(t, "page_head", Head{Title: "T"}); !strings.HasPrefix(got, `<div class="sp-head">`+"\n") {
		t.Errorf("page_head without ID starts %q", got[:40])
	}
	mustNotContain(t, partial(t, "box_open", Box{No: "01", Title: "x", ID: "a"}), "data-poll", "hx-")
	mustNotContain(t, partial(t, "page_head", Head{Title: "T", ID: "h"}), "hx-swap-oob")
}

// With them, the box is the element that polls and the head is the element a
// fragment swaps into place — attributes on the element, nothing else changed.
func TestPollAndOOBAttributes(t *testing.T) {
	got := partial(t, "box_open", Box{No: "01", Title: "x", ID: "health", Poll: "/overview/fragment"})
	mustContain(t, got, `<div class="box" id="health" data-poll aria-live="polite" hx-get="/overview/fragment" hx-trigger="load" hx-swap="outerHTML">`)
	got = partial(t, "page_head", Head{Title: "T", ID: "h", OOB: true})
	mustContain(t, got, `<div class="sp-head" id="h" hx-swap-oob="true">`)
}
