package view

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// The tests of the Help page that depend on who is reading it. The one that
// every link of every page lands on a topic is TestEveryHelpLinkLandsOnATopic
// (server_test.go).

// helpViewers are the readers of the Help page and the sections each is shown.
var helpViewers = []struct {
	name    string
	meta    Meta
	inbound bool
	groups  []string
}{
	{"global", Meta{User: "admin", IsGlobal: true}, true, []string{"Overview", "Outbound", "Inbound", "Server", "Account"}},
	{"global, inbound feature off", Meta{User: "admin", IsGlobal: true}, false, []string{"Overview", "Outbound", "Server", "Account"}},
	{"domain, outbound only", Meta{User: "ops", HasOutbound: true}, true, []string{"Outbound", "Account"}},
	{"domain, inbound only", Meta{User: "ops", HasInbound: true}, true, []string{"Inbound", "Account"}},
	{"domain, inbound only, feature off", Meta{User: "ops", HasInbound: true}, false, []string{"Account"}},
	{"domain, both", Meta{User: "ops", HasOutbound: true, HasInbound: true}, true, []string{"Outbound", "Inbound", "Account"}},
	{"domain, both, feature off", Meta{User: "ops", HasOutbound: true, HasInbound: true}, false, []string{"Outbound", "Account"}},
}

// Each reader is shown the sections of the pages they can open and no other,
// numbered one after the other, and the menu lists the same sections.
func TestHelpShowsEachRoleOnlyItsSections(t *testing.T) {
	groupTitle := regexp.MustCompile(`<span class="sp-no">(\d+)</span><h2>([^<]+)</h2>`)
	menuLabel := regexp.MustCompile(`<p class="menu-label">([^<]+)</p>`)
	for _, v := range helpViewers {
		out := renderSignedIn(t, "help", NewHelp(v.meta, v.inbound))
		var got, numbers, menu []string
		for _, m := range groupTitle.FindAllStringSubmatch(out, -1) {
			numbers, got = append(numbers, m[1]), append(got, m[2])
		}
		for _, m := range menuLabel.FindAllStringSubmatch(out, -1) {
			menu = append(menu, m[1])
		}
		if strings.Join(got, ",") != strings.Join(v.groups, ",") {
			t.Errorf("%s: sections %v, want %v", v.name, got, v.groups)
		}
		if strings.Join(menu, ",") != strings.Join(v.groups, ",") {
			t.Errorf("%s: the menu lists %v, the page shows %v", v.name, menu, v.groups)
		}
		for i, n := range numbers {
			if want := fmt.Sprintf("%02d", i+1); n != want {
				t.Errorf("%s: section %d is numbered %s, want %s", v.name, i+1, n, want)
			}
		}
	}
}

// The menu of the Help page jumps to topics that exist, in the order they are
// shown, and every topic shown is in it; no id is used twice on the page.
func TestHelpMenuAndTopicsAgree(t *testing.T) {
	menuLink := regexp.MustCompile(`<a (?:class="is-active" )?href="#([a-z-]+)">`)
	for _, v := range helpViewers {
		out := renderSignedIn(t, "help", NewHelp(v.meta, v.inbound))
		topics := helpTopic.FindAllStringSubmatch(out, -1)
		menu := menuLink.FindAllStringSubmatch(out, -1)
		if len(topics) != len(menu) || len(topics) == 0 {
			t.Fatalf("%s: %d topics, %d menu entries", v.name, len(topics), len(menu))
		}
		for i := range topics {
			if topics[i][1] != menu[i][1] {
				t.Errorf("%s: topic %d is %q, its menu entry points at %q", v.name, i, topics[i][1], menu[i][1])
			}
		}
		unique(t, "Help ("+v.name+")", ids(out))
	}
}

// The topics about Overview and Health, and about the Server pages, are for the
// role that sees those pages; a domain administrator is not shown them, nor a
// link to them.
func TestHelpLeavesOutWhatADomainAdministratorCannotOpen(t *testing.T) {
	out := renderSignedIn(t, "help", NewHelp(Meta{User: "ops", HasOutbound: true, HasInbound: true}, true))
	pageHas(t, "Help", out, `id="dns"`, `id="apps"`, `id="limits"`, `id="log"`, `id="queue"`, `id="dmarc"`,
		`id="inbound"`, `id="upstream"`, `id="recipients"`, `id="filter"`, `id="account"`,
		`<h2>Outbound</h2>`, `<h2>Inbound</h2>`, `<h2>Account</h2>`)
	pageLacks(t, "Help", out, `id="checks"`, `id="certificate"`, `id="rdns"`, `id="backup"`, `id="users"`, `id="settings"`,
		`<h2>Overview</h2>`, `<h2>Server</h2>`, `href="/server/health"`)
}

// Help text is data, not markup: a topic is one paragraph of Plain, Rich and
// Code runs with no raw HTML in it, and the page renders each topic as a
// heading and exactly one paragraph.
func TestHelpTopicsAreOneParagraphOfPlainText(t *testing.T) {
	p := NewHelp(admin(), true)
	seen := map[string]bool{}
	for _, g := range p.Groups {
		for _, topic := range g.Topics {
			if seen[topic.ID] {
				t.Errorf("topic id %q is used twice", topic.ID)
			}
			seen[topic.ID] = true
			if len(topic.Body) != 1 {
				t.Errorf("topic %q has %d paragraphs, want one: paragraph margins are reset, so several would run together", topic.ID, len(topic.Body))
			}
			texts := []string{topic.Title}
			for _, text := range topic.Body {
				for _, in := range text {
					texts = append(texts, in.Text)
				}
			}
			for _, s := range texts {
				if strings.ContainsAny(s, "<>") || strings.Contains(s, "&amp;") || strings.Contains(s, "&lt;") {
					t.Errorf("topic %q holds markup: %q", topic.ID, s)
				}
			}
		}
	}
	out := renderSignedIn(t, "help", p)
	blocks := regexp.MustCompile(`(?s)<div class="sp-help-topic" id="([a-z-]+)">(.*?)</div>`).FindAllStringSubmatch(out, -1)
	if len(blocks) != len(seen) {
		t.Errorf("%d topics rendered, %d in the data", len(blocks), len(seen))
	}
	for _, m := range blocks {
		if n, h := strings.Count(m[2], "<p>"), strings.Count(m[2], "<h3>"); n != 1 || h != 1 {
			t.Errorf("topic %q renders %d paragraphs and %d headings, want one of each", m[1], n, h)
		}
	}
	pageLacks(t, "Help", out, `&amp;lt;`, `&lt;a `)
}

// A link leads only to a topic its reader is shown. Boxes on pages that more
// than one role opens have their topic in a section only some are shown, so what
// they link to depends on the viewer.
func TestHelpLinksStayInsideWhatTheirViewerIsShown(t *testing.T) {
	for _, v := range helpViewers {
		shown := map[string]bool{}
		for _, m := range helpTopic.FindAllStringSubmatch(renderSignedIn(t, "help", NewHelp(v.meta, v.inbound)), -1) {
			shown[m[1]] = true
		}
		landsOn := func(where string, l *HelpLink) {
			t.Helper()
			if l == nil {
				return
			}
			if anchor := strings.TrimPrefix(l.Href, "/help#"); !shown[anchor] {
				t.Errorf("%s, %s: a box links to /help#%s, a topic that reader is not shown", v.name, where, anchor)
			}
		}

		if !v.meta.reachesOutbound() {
			continue
		}
		settings := NewOutDomainSettings(v.meta, 1, "example.org", 2, false)
		landsOn("Domain settings", settings.Reports.Help)
		landsOn("Domain settings", settings.Limit.Help)
		landsOn("Domain settings", settings.Export.Help)
		if has := settings.Export.Help != nil; has != v.meta.IsGlobal {
			t.Errorf("%s: the Export box has a Help link = %v, want %v (its topic is in Server)", v.name, has, v.meta.IsGlobal)
		}
		hub := NewDMARCHub(v.meta, IngestInput{}).WithoutIngest()
		if hub.Recent.Help == nil || hub.Recent.No != "01" {
			t.Errorf("%s: without the Ingest box the first box of the DMARC hub is %+v, want it numbered 01 with the Help link", v.name, hub.Recent)
		}
		landsOn("DMARC hub", hub.Recent.Help)
	}
}
