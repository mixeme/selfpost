package view

import (
	"strings"
	"testing"
	"time"
)

// ---- The log

// The log is the whole of what the journal holds about the viewer's inbound mail:
// the filters offer what the viewer reaches, the head says how long rows live and
// where to change it, and the global role is given the way to correct a decision.
func TestInLogShowsTheRowsAndTheirFilters(t *testing.T) {
	out := renderSignedIn(t, "in-log", inLogFixture())
	pageHas(t, "Inbound log", out,
		`<h1 class="title is-3">Inbound log</h1>`,
		`refused mail is read from <code>mail.log</code>; the filter&#39;s verdict appears when the spam filter is on. Rows older than 90 days are deleted — change that in <a href="/server/settings">Settings</a>.`,
		`<form class="sp-end" method="get" action="/inbound/log">`,
		`<select name="domain" aria-label="Domain"><option value="" selected>All domains</option><option value="lists.example.org">lists.example.org</option><option value="acme.io">acme.io</option></select>`,
		`<select name="decision" aria-label="Decision"><option value="" selected>Any decision</option><option value="accept">accept</option><option value="reject">reject</option><option value="tempfail">tempfail</option><option value="quarantine">quarantine</option></select>`,
		`<input class="input" type="date" name="from" value="2026-09-14" aria-label="From date">`,
		`<input class="input" type="date" name="to" value="2026-09-21" aria-label="To date">`,
		`href="/help#inbound-log" title="What the decisions mean"`,
		`<th>Time</th><th>From</th><th>To</th><th>Subject</th><th>Decision</th><th>Upstream</th><th></th></tr>`,
		// what the upstream answered: a tag that opens its last reply, a
		// plain tag while not tried, "—" for refused mail
		`<button type="button" class="tag is-success is-light" popovertarget="up-1">sent</button><div class="sp-sheet" id="up-1" popover>`,
		`<span class="tag is-success is-light">sent</span><h2>Upstream</h2>`,
		`<tr><th>Host</th><td class="sp-mono sp-small">mx.upstream.example[198.51.100.5]:25</td></tr><tr><th>Reply</th><td class="sp-mono sp-small">250 2.0.0 Ok: queued as CE05D10261D</td></tr><tr><th>At</th><td class="sp-mono sp-small">09-21 14:00:19</td></tr></tbody>`,
		`<button type="button" class="tag is-danger is-light" popovertarget="up-2">bounced</button>`,
		`<tr><th>Then</th><td>The sender was sent a bounce</td></tr>`,
		`<button type="button" class="tag is-warning is-light" popovertarget="up-6">deferred</button>`,
		`<tbody><tr><th>Reply</th><td class="sp-mono sp-small">connect to mx.upstream.example[198.51.100.5]:25: Connection refused</td></tr><tr><th>At</th><td class="sp-mono sp-small">09-21 13:45:12</td></tr><tr><th>Then</th><td>Postfix tries again; the message is in the queue</td></tr></tbody>`,
		`<td><span class="tag is-light">queued</span></td>`,
		`</div></div></div></div></td><td class="sp-muted">—</td><td class="sp-actions">`,
		`09-21 14:00:18`,
		`t.berg@<wbr>uni.example<br><span class="sp-mono sp-muted sp-small">192.0.2.77</span>`,
		`dev@<wbr>lists.example.org`, `title="Re: [dev] patch series v3"`,
		`<td class="sp-clip sp-muted">—</td>`,
		// the decision is a tag that opens the sheet with who decided and why
		`<button type="button" class="tag is-success is-light" popovertarget="why-1">accept</button><div class="sp-sheet" id="why-1" popover>`,
		`<div class="sp-sheet-head"><span class="tag is-success is-light">accept</span><h2>Why</h2><button type="button" popovertarget="why-1" popovertargetaction="hide" aria-label="Close">`,
		`<button type="button" class="tag is-danger is-light" popovertarget="why-3">reject</button>`,
		`<button type="button" class="tag is-warning is-light" popovertarget="why-5">tempfail</button>`,
		`<tr><th>Decided by</th><td>rspamd</td></tr><tr><th>Verdict</th><td>no action, score 0.40 / 15.00</td></tr><tr><th>Subject</th><td>Re: [dev] patch series v3</td></tr>`,
		`<tr><th>Verdict</th><td>add header, score 9.80 / 15.00</td></tr><tr><th>Symbols</th><td class="sp-mono sp-small">PHISHING(5.00)<br>FORGED_SENDER(3.00)<br>R_SPF_FAIL(1.00)</td></tr>`,
		`<tr><th>Decided by</th><td>postfix</td></tr><tr><th>Reply</th><td class="sp-mono sp-small">550 5.1.1 &lt;sales@lists.example.org&gt;: Recipient address rejected: User unknown in relay recipient table</td></tr></tbody>`,
		`Page 1 of 38`, `<a href="/inbound/log?from=2026-09-14&amp;p=2&amp;to=2026-09-21">Older →</a>`,
		`A decision opens its reason, an upstream result its last reply · Allow / Deny sender opens <a href="/inbound/filter-lists">Filter lists</a> with the entry filled in`,
	)
	pageLacks(t, "Inbound log", out, `← Newer`, `data-poll`, `hx-get`, `<th>Why</th>`)
	// A message Postfix itself refused offers no way into the lists: one Deny
	// for each of the four accepted messages, one Allow for each the filter
	// refused or put off.
	if deny, allow := strings.Count(out, `>Deny sender</a>`), strings.Count(out, `>Allow sender</a>`); deny != 4 || allow != 2 {
		t.Errorf("the fixture's seven rows offer Deny %d times and Allow %d times, want 4 and 2", deny, allow)
	}
	if n := strings.Count(out, `<td class="sp-muted">—</td>`); n != 3 {
		t.Errorf("%d rows have no upstream result, want the three refused", n)
	}

	// Without the global role there is nowhere to change the retention, and
	// nowhere to send a sender.
	domain := NewInLog(Meta{User: "ops", HasInbound: true}, 7, []string{"acme.io"}, InLogFilter{})
	out = renderSignedIn(t, "in-log", domain.WithRows([]InLogRow{domain.NewRow(InLogRowInput{
		At: time.Date(2026, 9, 21, 14, 0, 0, 0, time.UTC), From: "a@example.net", To: "b@acme.io",
		Decision: "accept", Engine: "rspamd"})}, 1, 1))
	pageHas(t, "Inbound log", out, `Rows older than 7 days are deleted.`, `rspamd</td>`)
	pageLacks(t, "Inbound log", out, `/server/settings`, `/inbound/filter-lists`, `Deny sender`, `Allow sender`, `Allow / Deny`)
}

// The lead's day count reads right for one day too, though the setting's range
// starts at seven: the word follows the number, not the other way round.
func TestInLogLeadCountsDays(t *testing.T) {
	out := renderSignedIn(t, "in-log", NewInLog(Meta{User: "ops", HasInbound: true}, 1, nil, InLogFilter{}))
	pageHas(t, "Inbound log", out, `Rows older than 1 day are deleted.`)
}

// The filter in force is the one selected, and the paging and the empty state
// carry and name it.
func TestInLogCarriesItsFilters(t *testing.T) {
	f := InLogFilter{Domain: "acme.io", Decision: "reject", From: "2026-09-01", To: "2026-09-30"}
	p := NewInLog(admin(), 90, []string{"lists.example.org", "acme.io"}, f)
	p.WithRows([]InLogRow{p.NewRow(InLogRowInput{At: time.Date(2026, 9, 2, 1, 2, 3, 0, time.UTC),
		From: "a@example.net", To: "b@acme.io", Decision: "reject", Engine: "postfix"})}, 3, 5)
	out := renderSignedIn(t, "in-log", p)
	pageHas(t, "Inbound log", out,
		`<option value="acme.io" selected>acme.io</option>`, `<option value="reject" selected>reject</option>`,
		`name="from" value="2026-09-01"`, `name="to" value="2026-09-30"`,
		`Page 3 of 5 · <a href="/inbound/log?decision=reject&amp;domain=acme.io&amp;from=2026-09-01&amp;p=2&amp;to=2026-09-30">← Newer</a>`,
		`<a href="/inbound/log?decision=reject&amp;domain=acme.io&amp;from=2026-09-01&amp;p=4&amp;to=2026-09-30">Older →</a>`,
	)
	if got := InLogHref(InLogFilter{}, 1); got != "/inbound/log" {
		t.Errorf("the first unfiltered page is %q", got)
	}
	if got := InLogHref(InLogFilter{Domain: "a b&c"}, 2); got != "/inbound/log?domain=a+b%26c&p=2" {
		t.Errorf("a filter is not encoded: %q", got)
	}

	out = renderSignedIn(t, "in-log", NewInLog(admin(), 90, nil, f).WithRows(nil, 1, 1))
	pageHas(t, "Inbound log", out, `No messages match these filters.`)
	out = renderSignedIn(t, "in-log", NewInLog(admin(), 90, nil, InLogFilter{}).WithRows(nil, 1, 1))
	pageHas(t, "Inbound log", out, `No messages logged yet.`)
	pageLacks(t, "Inbound log", out, `<table`, `Page 1`, `sp-box-foot`)
}

// The last page has nothing older.
func TestInLogLastPage(t *testing.T) {
	p := NewInLog(admin(), 90, nil, InLogFilter{})
	row := p.NewRow(InLogRowInput{At: time.Now(), From: "a@example.net", To: "b@acme.io", Decision: "accept", Engine: "selfpost"})
	out := renderSignedIn(t, "in-log", p.WithRows([]InLogRow{row}, 2, 2))
	pageHas(t, "Inbound log", out, `Page 2 of 2`, `← Newer</a>`)
	pageLacks(t, "Inbound log", out, `Older →`)
}

// A row says who decided, and what they said when they said something; a message
// refused before its sender was known has no sender to show or to list.
func TestInLogRowWording(t *testing.T) {
	p := NewInLog(admin(), 90, nil, InLogFilter{})
	at := time.Date(2026, 9, 21, 14, 0, 18, 0, time.UTC)
	r := p.NewRow(InLogRowInput{At: at, From: "a@example.net", To: "b@acme.io", Decision: "accept", Engine: "selfpost"})
	if len(r.Why.Rows) != 1 || r.Why.Rows[0].Lines[0] != "selfpost" || r.Time != "09-21 14:00:18" || r.Client != "" {
		t.Errorf("row = %+v", r)
	}
	r = p.NewRow(InLogRowInput{ID: 7, At: at, To: "b@acme.io", Decision: "reject", Engine: "postfix", Detail: "550 5.1.1 no"})
	if r.Why.ID != "why-7" || r.Why.Tag.Status != "reject" || len(r.Why.Rows) != 2 || r.Why.Rows[1].Label != "Reply" || r.From != "—" || r.Action != nil {
		t.Errorf("row = %+v", r)
	}
	out := renderSignedIn(t, "in-log", p.WithRows([]InLogRow{r}, 1, 1))
	pageHas(t, "Inbound log", out, `<td>—</td>`)
	pageLacks(t, "Inbound log", out, `<br><span class="sp-mono sp-muted sp-small"></span>`)
}

// Which decisions lead to which list, for the global role: the sender of mail
// that got through can be denied, the sender of mail the filter turned away or
// put off can be allowed, and a refusal that is Postfix's own offers neither.
func TestInLogSenderAction(t *testing.T) {
	for _, c := range []struct {
		decision, engine, from, label, href string
	}{
		{"accept", "rspamd", "a@example.net", "Deny sender", "/inbound/filter-lists?list=deny&kind=address&value=a%40example.net"},
		{"accept", "selfpost", "a@example.net", "Deny sender", "/inbound/filter-lists?list=deny&kind=address&value=a%40example.net"},
		{"reject", "rspamd", "a+b@example.net", "Allow sender", "/inbound/filter-lists?list=allow&kind=address&value=a%2Bb%40example.net"},
		{"tempfail", "rspamd", "a@example.net", "Allow sender", "/inbound/filter-lists?list=allow&kind=address&value=a%40example.net"},
		{"quarantine", "rspamd", "a@example.net", "Allow sender", "/inbound/filter-lists?list=allow&kind=address&value=a%40example.net"},
		{"reject", "postfix", "a@example.net", "", ""},
		{"tempfail", "postfix", "a@example.net", "", ""},
		{"reject", "selfpost", "a@example.net", "", ""},
		{"accept", "rspamd", "", "", ""},
		{"reject", "rspamd", "", "", ""},
	} {
		got := senderAction(c.decision, c.engine, c.from)
		switch {
		case c.label == "" && got != nil:
			t.Errorf("%s by %s from %q is offered %+v", c.decision, c.engine, c.from, *got)
		case c.label != "" && (got == nil || got.Label != c.label || got.Href != c.href):
			t.Errorf("%s by %s from %q is offered %+v, want %s -> %s", c.decision, c.engine, c.from, got, c.label, c.href)
		}
	}

	// The address in the link is data: it cannot leave the attribute.
	p := NewInLog(admin(), 90, nil, InLogFilter{})
	// (The handler would not call such a sender listable; the view must not
	// depend on that.)
	row := p.NewRow(InLogRowInput{Listable: true, At: time.Now(), From: `"><script>1</script>@x`, To: "b@acme.io", Decision: "accept", Engine: "rspamd"})
	out := renderSignedIn(t, "in-log", p.WithRows([]InLogRow{row}, 1, 1))
	pageLacks(t, "Inbound log", out, `<script>1`)
	pageHas(t, "Inbound log", out, `value=%22%3E%3Cscript%3E1%3C%2Fscript%3E%40x`)

	// A sender the lists would refuse is offered no way into them.
	if row := p.NewRow(InLogRowInput{At: time.Now(), From: "o'brien@example.org", To: "b@acme.io", Decision: "accept", Engine: "rspamd"}); row.Action != nil {
		t.Errorf("a sender that is not listable is offered %+v", *row.Action)
	}
}

// What a sender wrote is text on the page: an address, a subject or a reply from
// a server is never markup.
func TestInLogEscapesWhatSendersWrote(t *testing.T) {
	p := NewInLog(admin(), 90, nil, InLogFilter{})
	row := p.NewRow(InLogRowInput{At: time.Now(), From: `<img src=x onerror=alert(1)>@e.example`, Client: `<b>1</b>`,
		To: `<script>alert(2)</script>@acme.io`, Subject: `<script>alert(3)</script> "quoted"`,
		Decision: "reject", Engine: "rspamd", Detail: `<i>5.7.1</i>`})
	out := renderSignedIn(t, "in-log", p.WithRows([]InLogRow{row}, 1, 1))
	pageLacks(t, "Inbound log", out, `<script>alert`, `<img src=x`, `<b>1</b>`, `<i>5.7.1</i>`)
	pageHas(t, "Inbound log", out, `&lt;script&gt;alert(3)&lt;/script&gt;`, `&lt;i&gt;5.7.1&lt;/i&gt;`)
}

// ---- Filter lists

// The page is the add form, the two lists with their counts, and where the lists
// go - drawn as the mockup draws it.
func TestFilterListsShowsTheFormTheListsAndWhereTheyGo(t *testing.T) {
	out := renderSignedIn(t, "in-filter-lists", inFilterListsFixture())
	pageHas(t, "Filter lists", out,
		`<p class="sp-kicker">Inbound · global role</p>`, `<h1 class="title is-3">Filter lists</h1>`,
		`<form class="sp-form" method="post" action="/inbound/filter-lists">`,
		`<select id="list" name="list_type">`, `<option value="deny" selected>Deny — always turn away</option>`,
		`<option value="allow">Allow — always let through</option>`,
		`<select id="etype" name="entry_type">`, `<option value="address" selected>a sender address</option>`,
		`<option value="domain">a sender domain</option>`, `<option value="ip">a client address or network</option>`,
		`<input class="input sp-mono" id="value" name="value" value="" placeholder="promo@bulk.example"`,
		`<input class="input" id="note" name="note" value=""`, `Add entry`,
		`href="/help#filter-lists" title="How the lists are applied"`,
		`<h2>Deny</h2><div class="sp-end">3 entries · wins over Allow</div>`,
		`<h2>Allow</h2><div class="sp-end">2 entries</div>`,
		`<thead><tr><th>Value</th><th>Kind</th><th>Matched</th><th></th></tr></thead>`,
		// the kind is a tag that opens the entry's sheet; the count is beside it
		`<td class="sp-mono sp-small">bulk.example</td><td><button type="button" class="tag is-light" popovertarget="entry-1">domain</button><div class="sp-sheet" id="entry-1" popover>`,
		`<div class="sp-sheet-head"><span class="tag is-light">domain</span><h2>bulk.example</h2>`,
		`<tr><th>Note</th><td>newsletter nobody subscribed to</td></tr><tr><th>Added</th><td class="sp-mono sp-small">2026-09-02</td></tr><tr><th>Matched</th><td>41 in the last 90 days</td></tr><tr><th>Last match</th><td class="sp-mono sp-small">2026-09-21 13:57</td></tr></tbody>`,
		`</div></td><td class="sp-nowrap">41</td><td class="sp-actions">`,
		`<td class="sp-nowrap">1203</td>`,
		// an entry that never decided anything says so, in the row and on its sheet
		`<td class="sp-mono sp-small">win@lottery.example</td><td><button type="button" class="tag is-light" popovertarget="entry-3">address</button>`,
		`<tr><th>Note</th><td>—</td></tr><tr><th>Added</th><td class="sp-mono sp-small">2026-07-30</td></tr><tr><th>Matched</th><td>never in the last 90 days</td></tr></tbody>`,
		`</div></td><td class="sp-muted sp-nowrap">never</td><td class="sp-actions">`,
		`<form method="post" action="/inbound/filter-lists/3/delete"><button type="submit" class="has-text-danger">Remove</button></form>`,
		`<h2>Where the lists go</h2><div class="sp-end"><span class="tag is-success is-light">applied</span></div>`,
		`rspamd, from map files it rereads within seconds of a change`,
		`<span class="sp-mono">/data/antispam/</span> — six, one for each list and kind of entry`,
		`2026-09-11 08:40`, `Deny wins, whichever entries they are.`,
	)
	if got := strings.Count(out, `<table class="table is-fullwidth is-hoverable">`); got != 2 {
		t.Errorf("%d list tables, want the two lists", got)
	}
	pageLacks(t, "Filter lists", out, `<th>Note</th><th>`, `<th>Added</th><th>`)
}

// With no filter attached the lists are still the operator's to edit, and the
// box says nothing reads them; a list with no entries says so in its box.
func TestFilterListsOffAndEmpty(t *testing.T) {
	p := NewFilterLists(admin(), FilterListsInput{}).WithEntries([]FilterEntryInput{
		{ID: 9, List: "allow", Kind: "ip", Value: "192.0.2.1", Added: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
	})
	out := renderSignedIn(t, "in-filter-lists", p)
	pageHas(t, "Filter lists", out,
		`<span class="tag is-light">not applied</span>`,
		`Nothing — the spam filter is off. The lists are kept and can be edited; nothing reads them until <code>INBOUND_ANTISPAM_MILTER</code> is set.`,
		`<dt>Last written</dt><dd>never</dd>`,
		`<h2>Deny</h2><div class="sp-end">0 entries · wins over Allow</div>`,
		`<h2>Allow</h2><div class="sp-end">1 entry</div>`,
		`No sender is turned away by hand.`,
	)
	pageLacks(t, "Filter lists", out, `rspamd, from map files`, `is-success is-light">applied`, `No sender is let through by hand.`)
	if got := strings.Count(out, `<table class="table is-fullwidth is-hoverable">`); got != 1 {
		t.Errorf("%d list tables, want the one list that has entries", got)
	}
}

// The form shows what it is told to: prefilled from the log, or what was typed
// into a form that was refused, with the refusal between the head and the form.
func TestFilterListsFormAndRefusal(t *testing.T) {
	p := inFilterListsFixture().
		WithForm("allow", "domain", `x"><script>1</script>`, `a "note"`).
		WithResult("", "That entry is already on the list.")
	out := renderSignedIn(t, "in-filter-lists", p)
	pageHas(t, "Filter lists", out,
		`<option value="allow" selected>Allow — always let through</option>`, `<option value="domain" selected>a sender domain</option>`,
		`value="x&#34;&gt;&lt;script&gt;1&lt;/script&gt;"`, `value="a &#34;note&#34;"`,
		`<div class="notification is-danger is-light">`, `That entry is already on the list.`)
	pageLacks(t, "Filter lists", out, `<script>1`, `<option value="deny" selected>`)
	if strings.Index(out, `class="notification`) < strings.Index(out, `class="sp-head"`) ||
		strings.Index(out, `class="notification`) > strings.Index(out, `<form class="sp-form"`) {
		t.Error("the refusal is not between the head and the form")
	}

	out = renderSignedIn(t, "in-filter-lists", inFilterListsFixture().WithResult("Entry added.", ""))
	pageHas(t, "Filter lists", out, `<div class="notification is-success is-light">`, `Entry added.`)
}

// What a person typed into a list is text on the page.
func TestFilterListsEscapesTheEntries(t *testing.T) {
	p := NewFilterLists(admin(), FilterListsInput{FilterOn: true}).WithEntries([]FilterEntryInput{
		{ID: 1, List: "deny", Kind: "address", Value: `<img src=x>@a.example`, Note: `<script>alert(1)</script>`, Added: time.Now()},
	})
	out := renderSignedIn(t, "in-filter-lists", p)
	pageLacks(t, "Filter lists", out, `<img src=x>`, `<script>alert`)
}

// The menu leads to both pages, the lists for the global role only.
func TestInboundMenuHasLogAndFilterLists(t *testing.T) {
	out := renderSignedIn(t, "in-log", inLogFixture())
	pageHas(t, "Inbound log", out,
		`<a class="navbar-item is-active" href="/inbound/log"><i class="ti ti-list-details"></i>Log</a>`,
		`<a class="navbar-item" href="/inbound/filter-lists"><i class="ti ti-filter"></i>Filter lists</a>`,
		`<a href="/inbound/log" aria-current="page"><i class="ti ti-list-details"></i>Log</a><a href="/inbound/filter-lists"><i class="ti ti-filter"></i>Filter lists</a>`)
	out = renderSignedIn(t, "in-filter-lists", inFilterListsFixture())
	pageHas(t, "Filter lists", out,
		`<a class="navbar-item is-active" href="/inbound/filter-lists">`, `<a href="/inbound/filter-lists" aria-current="page">`)

	domain := NewInLog(Meta{User: "ops", HasInbound: true}, 90, nil, InLogFilter{}).WithRows(nil, 1, 1)
	out = renderSignedIn(t, "in-log", domain)
	pageHas(t, "Inbound log", out, `<a class="navbar-item is-active" href="/inbound/log">`, `<a href="/inbound/log" aria-current="page">`)
	pageLacks(t, "Inbound log", out, `/inbound/filter-lists`)
}

// What a sheet says, from the two shapes a row's detail has: the filter's
// verdict on mail that passed, and the reply a refusal was given.
func TestWhyRows(t *testing.T) {
	flat := func(rows []SheetRow) string {
		var out []string
		for _, r := range rows {
			out = append(out, r.Label+"="+strings.Join(r.Lines, "|"))
		}
		return strings.Join(out, "; ")
	}
	for _, c := range []struct{ decision, engine, detail, subject, want string }{
		{"accept", "selfpost", "", "", "Decided by=selfpost"},
		{"accept", "selfpost", "", "Hello", "Decided by=selfpost; Subject=Hello"},
		{"accept", "rspamd", "no action, score 0.40 / 15.00", "Hi", "Decided by=rspamd; Verdict=no action, score 0.40 / 15.00; Subject=Hi"},
		{"accept", "rspamd", "add header, score 9.80 / 15.00; A(5.00) B(-0.10)", "", "Decided by=rspamd; Verdict=add header, score 9.80 / 15.00; Symbols=A(5.00)|B(-0.10)"},
		// a verdict with symbols and no action or score
		{"quarantine", "rspamd", "A(5.00)", "", "Decided by=rspamd; Verdict=A(5.00)"},
		{"reject", "rspamd", "5.7.1 Spam message rejected", "", "Decided by=rspamd; Reply=5.7.1 Spam message rejected"},
		{"tempfail", "postfix", "450 4.1.8 <a@nx.example>: Sender address rejected; see x", "", "Decided by=postfix; Reply=450 4.1.8 <a@nx.example>: Sender address rejected; see x"},
		// a refusal's text is a reply whatever it starts with
		{"reject", "rspamd", "milter triggers DISCARD action", "", "Decided by=rspamd; Reply=milter triggers DISCARD action"},
	} {
		if got := flat(whyRows(c.decision, c.engine, c.detail, c.subject)); got != c.want {
			t.Errorf("whyRows(%q, %q, %q, %q)\n got: %s\nwant: %s", c.decision, c.engine, c.detail, c.subject, got, c.want)
		}
	}
}

// A message the filter had held is a plain tag until it is released, and a
// bounce of mail from the null sender says none went back.
func TestInLogHeldAndABounceWithNoReturnAddress(t *testing.T) {
	p := NewInLog(admin(), 90, nil, InLogFilter{})
	at := time.Date(2026, 9, 21, 14, 0, 0, 0, time.UTC)
	out := renderSignedIn(t, "in-log", p.WithRows([]InLogRow{
		p.NewRow(InLogRowInput{ID: 1, At: at, From: "a@example.net", To: "b@acme.io", Decision: "quarantine", Engine: "rspamd", Upstream: "held"}),
		p.NewRow(InLogRowInput{ID: 2, At: at, To: "b@acme.io", Decision: "accept", Engine: "selfpost",
			Upstream: "bounced", UpstreamReply: "550 5.1.1 no such user", UpstreamAt: at}),
	}, 1, 1))
	pageHas(t, "Inbound log", out, `<td><span class="tag is-primary is-light">held</span></td>`,
		`<tr><th>Then</th><td>No bounce: the message had no return address</td></tr>`)
	pageLacks(t, "Inbound log", out, `The sender was sent a bounce`, `<th>Host</th>`)
}
