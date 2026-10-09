package view

import (
	"strings"
	"testing"
	"time"
)

// ---- The log

// The log is the whole of what the journal holds about the viewer's mail: the
// filters offer what the viewer reaches, the head says how long rows live and
// where to change it, and every row leads to its own page.
func TestOutLogShowsTheRowsAndTheirFilters(t *testing.T) {
	out := renderSignedIn(t, "out-log", outLogFixture())
	pageHas(t, "Outbound log", out,
		`<h1 class="title is-3">Outbound log</h1>`,
		`Rows older than 30 days are deleted — change that in <a href="/server/settings">Settings</a>.`,
		`<form class="sp-end" method="get" action="/outbound/log">`,
		`<select name="domain" aria-label="Domain"><option value="" selected>All domains</option><option value="example.org">example.org</option>`,
		`<select name="app" aria-label="Application"><option value="" selected>All applications</option>`,
		`<th>Time, UTC</th>`, `09-21 14:01:52`,
		`shop@<wbr>shop.example.org`, `m.keller@<wbr>gmx.de`, `title="[FIRING] disk usage above 90 % on db-2"`,
		`<span class="tag is-success is-light">sent</span>`, `<span class="tag is-warning is-light">deferred</span>`,
		`<span class="tag is-danger is-light">bounced</span>`, `<span class="tag is-light">rejected</span>`,
		`href="/outbound/log/184220">Details</a>`,
		`Page 1 of 412`, `<a href="/outbound/log?p=2">Older →</a>`, `New rows appear on their own`,
	)
	pageLacks(t, "Outbound log", out, `← Newer`)

	// Without the global role there is nowhere to change the retention.
	p := NewOutLog(Meta{User: "ops", HasOutbound: true}, 7, false, []string{"example.org"}, nil, "", "")
	out = renderSignedIn(t, "out-log", p.WithRows(nil, 1, 1))
	pageHas(t, "Outbound log", out, `Rows older than 7 days are deleted.`)
	pageLacks(t, "Outbound log", out, `/server/settings`)
	pageLacks(t, "Outbound log", renderSignedIn(t, "out-log", NewOutLog(Meta{User: "ops", HasOutbound: true}, 1, false, nil, nil, "", "").WithRows(nil, 1, 1)),
		`older than 1 days`)
}

// The filter in force is the one selected, and the poll, the paging and the
// links of the rows all carry it — and say nothing for a filter that is not set.
func TestOutLogCarriesItsFilters(t *testing.T) {
	p := NewOutLog(admin(), 30, true, []string{"example.org", "shop.example.org"}, []string{"alerts"}, "shop.example.org", "alerts")
	p.WithRows([]OutLogRow{{Time: "09-21 14:01:52", From: "a@example.org", To: "b@example.org", Subject: "Hi",
		Status: Tag{Status: "sent"}, Href: p.DetailHref(7, 3)}}, 3, 5)
	out := renderSignedIn(t, "out-log", p)
	pageHas(t, "Outbound log", out,
		`<option value="shop.example.org" selected>shop.example.org</option>`, `<option value="alerts" selected>alerts</option>`,
		`hx-get="/outbound/log/fragment?app=alerts&amp;domain=shop.example.org&amp;p=3"`,
		`href="/outbound/log/7?app=alerts&amp;domain=shop.example.org&amp;p=3"`,
		`Page 3 of 5 · <a href="/outbound/log?app=alerts&amp;domain=shop.example.org&amp;p=2">← Newer</a>`,
		`<a href="/outbound/log?app=alerts&amp;domain=shop.example.org&amp;p=4">Older →</a>`,
	)
	if got := LogHref("", "", 1); got != "/outbound/log" {
		t.Errorf("the first unfiltered page is %q", got)
	}
	if got := LogHref("a b&c", "", 2); got != "/outbound/log?domain=a+b%26c&p=2" {
		t.Errorf("a filter is not encoded: %q", got)
	}
}

// The last page has nothing older, and a page with no rows says so and still
// polls: a log that is empty now is the one that fills while the page is open.
func TestOutLogLastPageAndEmptyPage(t *testing.T) {
	p := NewOutLog(admin(), 30, true, nil, nil, "", "")
	out := renderSignedIn(t, "out-log", p.WithRows([]OutLogRow{{Subject: "x", Status: Tag{Status: "sent"}, Href: "/outbound/log/1"}}, 2, 2))
	pageHas(t, "Outbound log", out, `Page 2 of 2`, `← Newer</a>`)
	pageLacks(t, "Outbound log", out, `Older →`)

	out = renderSignedIn(t, "out-log", NewOutLog(admin(), 30, true, nil, nil, "", "").WithRows(nil, 1, 1))
	pageHas(t, "Outbound log", out, `No messages logged yet.`, `id="out-log-rows"`, `data-poll`, `id="out-log-foot"`, `New rows appear on their own`)
	pageLacks(t, "Outbound log", out, `<table`, `Page 1`)
}

// The fragment is the table and, marked to be swapped in by its id, the foot;
// the page's own foot carries the id and no mark.
func TestOutLogFragmentMarksOnlyTheFoot(t *testing.T) {
	page := renderSignedIn(t, "out-log", outLogFixture())
	frag := renderFragment(t, "out_log_rows", outLogFixture().Fragment())
	pageHas(t, "fragment", frag, `<div class="table-container" id="out-log-rows" data-poll`, `<div class="sp-box-foot" id="out-log-foot" hx-swap-oob="true">`)
	pageLacks(t, "fragment", frag, `<form`, `<select`, `sp-head`)
	pageLacks(t, "page", page, `hx-swap-oob`)
	pageHas(t, "page", page, `<div class="sp-box-foot" id="out-log-foot">`)
	if strings.Count(frag, `<table`) != 1 {
		t.Errorf("the fragment holds %d tables", strings.Count(frag, `<table`))
	}
}

func TestOutLogEscapesWhatSendersWrote(t *testing.T) {
	p := NewOutLog(admin(), 30, true, []string{`<b>d</b>`}, []string{`"><i>`}, "", "")
	p.WithRows([]OutLogRow{{Time: "t", From: `"><img src=x>@a`, To: `<u>@b`, Subject: `<script>alert(1)</script>`,
		Status: Tag{Status: `<i>`}, Href: "/outbound/log/1"}}, 1, 1)
	out := renderSignedIn(t, "out-log", p)
	pageLacks(t, "Outbound log", out, `<script>alert`, `<img src=x>`, `<b>d</b>`, `<u>@b`, `<i>`)
	pageHas(t, "Outbound log", out, `&lt;script&gt;alert(1)&lt;/script&gt;`)
}

// ---- One message

func TestOutMessageShowsWhatTheJournalRecorded(t *testing.T) {
	out := renderSignedIn(t, "out-message", outMessageFixture())
	pageHas(t, "Message", out,
		`<h1 class="title is-4">[FIRING] disk usage above 90 % on db-2</h1>`,
		`<a href="/outbound/log">Outbound log</a> /`,
		`<p class="sp-route"><span>alerts@example.org</span><i class="ti ti-arrow-right sp-muted"></i><span>noc@example.org</span><span class="tag is-warning is-light">deferred</span></p>`,
		`<a href="/outbound/domains/1">example.org</a>`, `<dd class="sp-mono"><a href="/outbound/domains/1/applications/2">alerts</a></dd>`,
		`2026-09-21 13:58:07 UTC`, `2026-09-21 13:58:09 UTC`, `<dd class="sp-mono">4XcB7k2Jm9z1</dd>`, `<dd class="sp-mono">184220</dd>`,
		`<li><time>2026-09-21 13:58:07 UTC</time><strong>Accepted and queued</strong>`,
		`<li class="sp-warn"><time>2026-09-21 13:58:09 UTC</time><strong>Deferred, will be retried</strong>`,
		`<li class="sp-pending"><time>not yet</time>`,
		`Lines from <code>mail.log</code> with this queue id`, `<pre class="sp-log">`, `<span class="sp-d">13:58:07</span> postfix/smtpd[2114]`,
		`<span class="sp-w">postfix/smtp[2123]: 4XcB7k2Jm9z1: to=&lt;noc@example.org&gt;`,
	)
}

// A message without a subject is named for it, and a row with nothing to show
// for a field shows a dash.
func TestOutMessageWithoutSubjectOrRecipient(t *testing.T) {
	at := time.Date(2026, 9, 21, 13, 58, 7, 0, time.UTC)
	p := NewOutMessage(admin(), MessageInput{ID: 9, Domain: "gone.example", App: "old", Status: "rejected", Accepted: at, Reported: at,
		BackHref: "/outbound/log?domain=gone.example&p=2"}).
		WithDeliveryLog(nil, "This message never reached the queue, so Postfix wrote no delivery lines for it.")
	out := renderSignedIn(t, "out-message", p)
	pageHas(t, "Message", out, `<h1 class="title is-4">(no subject)</h1>`, `<span>—</span><i class="ti ti-arrow-right sp-muted"></i><span>—</span>`,
		`<span class="tag is-light">rejected</span>`, `href="/outbound/log?domain=gone.example&amp;p=2"`,
		`<dd>gone.example</dd>`, `<dd class="sp-mono">old</dd>`, `<dd class="sp-mono">—</dd>`,
		`never reached the queue`)
	pageLacks(t, "Message", out, `<pre`, `<a href="/outbound/domains/`)
}

func TestOutMessageEscapesWhatSendersWrote(t *testing.T) {
	at := time.Now()
	p := NewOutMessage(admin(), MessageInput{ID: 1, QueueID: `"><i>`, Domain: `<b>d</b>`, App: `<u>a</u>`, From: `<s>f</s>`, To: `"><img src=x>`,
		Subject: `<script>alert(1)</script>`, Status: "sent", Accepted: at, Reported: at, BackHref: "/outbound/log"}).
		WithHistory([]Step{{Time: "t", Strong: `<b>s</b>`, Text: Plain(`<i>x</i>`)}}).
		WithDeliveryLog([]LogLine{{Time: `<u>`, Text: `<script>y</script>`}}, "")
	out := renderSignedIn(t, "out-message", p)
	pageLacks(t, "Message", out, `<script>alert`, `<script>y`, `<b>d</b>`, `<u>a</u>`, `<s>f</s>`, `<img src=x>`, `<b>s</b>`, `<i>x</i>`, `<u>`)
}

func TestStepLevelAndLogLineLevel(t *testing.T) {
	for _, c := range []struct {
		outcome  string
		happened bool
		want     string
	}{
		{"ok", true, ""}, {"", true, ""}, {"warn", true, LevelWarn}, {"error", true, LevelFail},
		{"", false, LevelPending}, {"error", false, LevelPending},
	} {
		if got := StepLevel(c.outcome, c.happened); got != c.want {
			t.Errorf("StepLevel(%q, %v) = %q, want %q", c.outcome, c.happened, got, c.want)
		}
	}
	for text, want := range map[string]string{
		"postfix/smtp[1]: Q1: to=<a@b>, status=sent (250 OK)":          "",
		"postfix/smtp[1]: Q1: to=<a@b>, status=deferred (try later)":   "warn",
		"postfix/smtp[1]: Q1: to=<a@b>, status=bounced (no such user)": "error",
		"postfix/qmgr[1]: Q1: status=expired, returned to sender":      "error",
		"postfix/smtp[1]: warning: TLS library problem":                "warn",
		"opendkim[1]: Q1: DKIM-Signature field added":                  "",
	} {
		if got := LogLineLevel(text); got != want {
			t.Errorf("LogLineLevel(%q) = %q, want %q", text, got, want)
		}
	}
	if got := StepTime(time.Time{}); got != "not yet" {
		t.Errorf("a step that has not happened is at %q", got)
	}
}
