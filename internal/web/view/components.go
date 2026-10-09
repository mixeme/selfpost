package view

// The typed input of every partial in templates/components.html, and of the
// shell in templates/layout.html (docs/plans/panel-redesign.md § Component kit).
// A partial's input is part of its definition: components.html documents the
// look, this file the data. A page template passes one of these structs to its
// partial — it never assembles the input with dict — and the handler (or a
// fixture) fills it. Anything a person could type is a plain string and is
// escaped; markup a field can carry — a link, a code span — is a Text, never
// trusted HTML.

import (
	"fmt"
	"html/template"
	"strings"
)

// Levels a postmark, a health card or a timeline step can have. The empty
// string is the default look (ok); "pending" is a timeline step that has not
// happened yet.
const (
	LevelOK      = "ok"
	LevelWarn    = "warn"
	LevelFail    = "fail"
	LevelPending = "pending"
)

// Box variants.
const (
	// BoxDanger is the sp-danger-zone box: a destructive confirmation or the
	// "delete" entry of a settings page.
	BoxDanger = "danger"
	// BoxCredential is the sp-credential box: a secret the panel will never
	// show again.
	BoxCredential = "credential"
)

// Inline is one piece of a Text. Exactly one kind applies, in this order: a
// line break, a tag, then text — which a link wraps when Href is set, and which
// is code, a mono value, strong or emphasised when the matching flag is.
type Inline struct {
	Text   string
	Href   string
	Code   bool
	Mono   bool
	Strong bool
	Em     bool
	Tag    *Tag
	Break  bool
}

// Text is a run of inline content: what a lead, a box's end slot, a note, a
// fact's value or a help paragraph holds when it is more than one plain string.
// It is rendered by the "inline" partial. Build it with Rich.
type Text []Inline

// Rich builds a Text from strings (plain text), Inlines (Code, Link, …) and
// other Texts. Anything else is printed as text, so a number can be passed.
func Rich(parts ...any) Text {
	var t Text
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			t = append(t, Inline{Text: v})
		case Inline:
			t = append(t, v)
		case Text:
			t = append(t, v...)
		default:
			t = append(t, Inline{Text: fmt.Sprint(v)})
		}
	}
	return t
}

// Plain is a Text of one plain string — the common case.
func Plain(s string) Text { return Text{{Text: s}} }

// Code is a <code> span.
func Code(s string) Inline { return Inline{Text: s, Code: true} }

// Mono is a value set in the mono face.
func Mono(s string) Inline { return Inline{Text: s, Mono: true} }

// Strong is a <strong> span.
func Strong(s string) Inline { return Inline{Text: s, Strong: true} }

// Em is an <em> span.
func Em(s string) Inline { return Inline{Text: s, Em: true} }

// Link is a link with plain text.
func Link(href, label string) Inline { return Inline{Text: label, Href: href} }

// Br is a line break.
func Br() Inline { return Inline{Break: true} }

// TagOf is a status tag inside a Text.
func TagOf(status, label string) Inline { return Inline{Tag: &Tag{Status: status, Label: label}} }

// Tag is a status tag (partial "tag", function status_tag): the colour comes
// from Status, the text from Label — the status itself when Label is empty. It
// is the small coloured word in a table cell, a box's end slot or a menu entry.
type Tag struct {
	Status string
	Label  string
}

// statusClass is the one mapping of status to Bulma tag classes — box 03 of the
// kit page. A page passes the status it knows (a DKIM check says ok, a message
// says deferred) and never picks a colour.
var statusClass = map[string]string{
	"ok": "tag is-success is-light", "published": "tag is-success is-light",
	"running": "tag is-success is-light", "sent": "tag is-success is-light",

	"warn": "tag is-warning is-light", "deferred": "tag is-warning is-light",
	"quiet": "tag is-warning is-light",

	"error": "tag is-danger is-light", "fail": "tag is-danger is-light",
	"mismatch": "tag is-danger is-light", "bounced": "tag is-danger is-light",

	"quarantine": "tag is-primary is-light", "held": "tag is-primary is-light",
	"global": "tag is-primary is-light",
}

// neutralTag is a status without a colour: unknown, off, a plain count.
const neutralTag = "tag is-light"

// StatusTag returns the Bulma classes of a status tag (template function
// status_tag). A status the table does not know — "off", "unknown", a count, a
// word a page invented — is neutral rather than an error: a tag that is merely
// grey is a visible prompt to add the word to statusClass (and to the kit page),
// where a failing render would take the whole page down for it. Case and
// surrounding space do not matter.
func StatusTag(status string) string {
	if c, ok := statusClass[strings.ToLower(strings.TrimSpace(status))]; ok {
		return c
	}
	return neutralTag
}

// wbrAt escapes s and puts a <wbr> after every @ (template function wbr_at):
// an address in a table cell breaks where it reads naturally, so the column can
// shrink without splitting a name mid-word.
func wbrAt(s string) template.HTML {
	return template.HTML(strings.ReplaceAll(template.HTMLEscapeString(s), "@", "@<wbr>"))
}

// Head is the input of page_head, which opens every page: the line above the
// title (a section name or the path to this page), the title, a lead or a route,
// the postmark when the page gives a verdict, a note on the right (when it was
// updated) and the page's buttons.
type Head struct {
	Postmark *Postmark
	// Kicker is a plain word or two above the title ("Outbound"); Crumbs
	// replace it with the trail to this page.
	Kicker string
	Crumbs []Crumb
	Title  string
	// Code follows the title in the mono face ("New password for prod-server");
	// Mono sets the whole title in it; Compact is the smaller h1 of a page
	// whose title is data (a subject line).
	Code    string
	Mono    bool
	Compact bool
	// Lead is the sentence under the title. A Route takes its place on the page
	// of one message.
	Lead    Text
	Route   Route
	Note    []string // right-hand note, one line each
	Actions []Action
}

// Crumb is one step of a Head's trail; a step without Href is plain text (the
// level that has no page of its own).
type Crumb struct {
	Href  string
	Label string
}

// Action is a button in a Head or a Foot: a link to Href, or a POST form
// submitting to Post. Icon is a Tabler class ("ti-refresh").
type Action struct {
	Label   string
	Icon    string
	Href    string
	Post    string
	Primary bool
	Danger  bool
}

// Postmark is the round verdict stamp inside a Head: three lines of imprint
// and the Level (ok, warn or fail) that colours it.
type Postmark struct {
	Top    string
	Word   string
	Bottom string
	Level  string
}

// RoutePart is one piece of a Route: a plain value, an arrow, or a tag.
type RoutePart struct {
	Text  string
	Arrow bool
	Tag   *Tag
}

// Route is the from → to line under the title of a message, with its status;
// partial "route", used in a Head and in the body of a box.
type Route []RoutePart

// Flash is the input of flash: the result of the last action, between the page
// head and the first box. Error makes it the red one; Text may open with a Strong.
type Flash struct {
	Error bool
	Text  Text
}

// Box is the input of box_open and box_head: a box and its numbered mono head.
// No is "01"; Icon replaces it when the box is not part of a sequence. End is
// the slot at the right of the head (a note, a tag, a link) and Filter takes it
// instead as a filter form. Help links the section's topic on the Help page.
// ID is the anchor a side menu points at. Variant is BoxDanger or BoxCredential.
type Box struct {
	No      string
	Icon    string
	Title   string
	ID      string
	End     Text
	Filter  *Filter
	Help    *HelpLink
	Variant string
}

// HelpLink is a Box's link to its Help topic.
type HelpLink struct {
	Href  string
	Title string
}

// Filter is the input of filter_form, the filter of a list in a box head: it is
// sent with GET to Action.
type Filter struct {
	Action  string
	Selects []Select
	Dates   []DateInput
	Button  string // "Filter" when empty
}

// Select is one select of a Filter. Label is its accessible name.
type Select struct {
	Name     string
	Label    string
	Options  []Option
	Selected string
}

// Option is one choice of a Select; an empty Value means "any".
type Option struct {
	Value string
	Label string
}

// DateInput is one date field of a Filter.
type DateInput struct {
	Name  string
	Label string
	Value string
}

// Foot is the input of box_foot, the line under a box's content: Text (a legend
// or "Page 1 of 9"), a Link beside it ("Older →"), an End slot at the right, or
// instead of all of it one Button that finishes the page. Bare puts Text
// directly in the foot instead of in a span, as the legends under tables do.
type Foot struct {
	Text   Text
	Bare   bool
	Link   *Anchor
	End    Text
	Button *Action
}

// Anchor is a plain link: where it goes and what it says.
type Anchor struct {
	Href  string
	Label string
}

// HealthCard is the input of health_card and, as a slice, of health_cards: one
// tile of the Overview grid. Name is the check, Value its verdict in words, Sub
// one supporting fact; Level is ok, warn or fail; Href is its detail.
type HealthCard struct {
	Name  string
	Value string
	Sub   string
	Icon  string
	Href  string
	Level string
}

// Record is the input of dns_record: one DNS record. Host and Type say where
// and what, Value is what to publish (a textarea of ValueRows lines when it is
// long, as a DKIM key is), InDNS what the resolver returned — set only on a
// mismatch, with Problem saying what is wrong and the fix.
type Record struct {
	Name      string
	Status    Tag
	Note      Text
	Host      string
	Type      string
	Value     string
	ValueRows int
	ValueHelp Text
	InDNS     string
	Problem   Text
}

// HostField is the Copy field of the record's host.
func (r Record) HostField() CopyField { return CopyField{Label: "Host / name", Value: r.Host} }

// ValueField is the Copy field of what to publish.
func (r Record) ValueField() CopyField {
	return CopyField{Label: "Value to publish", Value: r.Value, Rows: r.ValueRows, Help: r.ValueHelp}
}

// CopyField is the input of copy_field: a read-only value with its Copy button.
// Rows makes it a textarea; Help sits under it. Used in a dns_record and, in a
// record_open block, for a secret shown once.
type CopyField struct {
	Label string
	Value string
	Rows  int
	Help  Text
}

// Fact is the input of facts (as a slice): a label and its value. Mono sets the
// value in the mono face; Big is a headline number, with Note as the line under it.
type Fact struct {
	Label string
	Value Text
	Mono  bool
	Big   bool
	Note  Text
}

// Facts is a shorthand for plain label / value pairs: Facts("Domain",
// "example.org", "Port", "587").
func Facts(pairs ...string) []Fact {
	var out []Fact
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, Fact{Label: pairs[i], Value: Plain(pairs[i+1])})
	}
	return out
}

// SideMenu is the input of side_menu, the left column of a detail page: groups
// of entries with icons and count tags.
type SideMenu struct {
	Groups []MenuGroup
}

// MenuGroup is a labelled list of a SideMenu.
type MenuGroup struct {
	Label string
	Items []MenuItem
}

// MenuItem is one entry of a SideMenu. Active marks the section on screen;
// Danger is the entry that deletes; Tag is a count.
type MenuItem struct {
	Label  string
	Icon   string
	Href   string
	Active bool
	Danger bool
	Tag    *Tag
}

// Step is one entry of a timeline (as a slice): a time, a strong word and the
// rest of the sentence. Level is "", warn, fail or pending.
type Step struct {
	Time   string
	Strong string
	Text   Text
	Level  string
}

// LogLine is one line of a log_pane (as a slice). Level is "", warn or error;
// Time is the dimmed prefix when the line has one.
type LogLine struct {
	Time  string
	Text  string
	Level string
}

// EmptyState is the input of empty_state: what is absent and why that is fine.
type EmptyState struct {
	Icon string
	Text Text
}

// HelpTopic is the input of help_topic: one topic of the Help page, a heading
// and its paragraphs. ID is the anchor the boxes' help links point at.
type HelpTopic struct {
	ID    string
	Title string
	Body  []Text
}

// Meta is what a page says about itself to the shell. A page's data embeds it
// (Engine.Render reads it through ShellMeta); a handler that still passes a map
// gives the same names as keys: Title, User, IsGlobal, HasOutbound, HasInbound,
// Section and Page.
//
// Section is the group the page belongs to — overview, outbound, inbound,
// server, or user for Account and Help — and Page the entry within it
// (domains, log, queue, dmarc, health, backup, users, settings…): the first
// highlights the menu, the second the entry and the sibling strip. A domain
// administrator's reach is HasOutbound and HasInbound: whether anything of that
// direction is assigned to them (ignored for the global role, who has it all).
type Meta struct {
	Title       string
	User        string
	IsGlobal    bool
	HasOutbound bool
	HasInbound  bool
	Section     string
	Page        string
}

// ShellMeta lets a page's data that embeds Meta be read by the engine.
func (m Meta) ShellMeta() Meta { return m }

// shellMeta is implemented by Meta, and so by every page that embeds it.
type shellMeta interface{ ShellMeta() Meta }

// metaOf reads a page's Meta from its data: a struct that embeds Meta, or the
// map a handler builds.
func metaOf(data any) Meta {
	switch d := data.(type) {
	case shellMeta:
		return d.ShellMeta()
	case map[string]any:
		str := func(k string) string { s, _ := d[k].(string); return s }
		flag := func(k string) bool { b, _ := d[k].(bool); return b }
		return Meta{
			Title: str("Title"), User: str("User"), IsGlobal: flag("IsGlobal"),
			HasOutbound: flag("HasOutbound"), HasInbound: flag("HasInbound"),
			Section: str("Section"), Page: str("Page"),
		}
	}
	return Meta{}
}

// Shell is the input of layout.html: what the navbar, the sibling strip and the
// footer show. The menu's paths are in the template; whether an entry is there
// is decided here, once, from the role and the feature flags (plan § Who sees
// what): Overview and Server only for the global role, Queue likewise (a queue
// is not filtered by domain), Inbound only when the feature is on and there is
// something in it for this user, DMARC only when it is on.
type Shell struct {
	Title     string
	User      string // empty: signed out, no navigation
	Section   string
	Page      string
	Home      string // where the wordmark leads
	Version   string
	Copyright string
	SourceURL string

	ShowOverview bool
	ShowOutbound bool
	ShowQueue    bool
	ShowDMARC    bool
	ShowInbound  bool
	ShowServer   bool
}

// Frame is what layout.html is executed with: the shell and the page's own
// data, which the page's "content" template receives.
type Frame struct {
	Shell Shell
	Page  any
}

// shell derives the Shell for a page from its Meta and the engine's flags.
func (e *Engine) shell(m Meta, copyright, sourceURL string) Shell {
	s := Shell{
		Title: m.Title, User: m.User, Section: m.Section, Page: m.Page,
		Version: e.version, Copyright: copyright, SourceURL: sourceURL,

		ShowOverview: m.IsGlobal,
		ShowServer:   m.IsGlobal,
		ShowQueue:    m.IsGlobal,
		ShowOutbound: m.IsGlobal || m.HasOutbound,
		ShowInbound:  e.inboundEnabled && (m.IsGlobal || m.HasInbound),
	}
	s.ShowDMARC = e.dmarcEnabled && s.ShowOutbound
	switch {
	case s.ShowOverview:
		s.Home = "/overview"
	case s.ShowOutbound:
		s.Home = "/outbound/domains"
	case s.ShowInbound:
		s.Home = "/inbound/domains"
	default:
		s.Home = "/account"
	}
	return s
}
