package view

// The data of Inbound › Filter lists (templates/in_filter_lists.html, mockup
// in-filter-lists.html): the allow and deny lists that correct the external spam
// filter by hand. The lists are instance-wide, so the page is the global role's.
//
// A page is a typed struct that embeds Meta, built by a constructor and filled
// by With… methods; the handler reads the lists, the view decides what is said
// and how it is worded.

import (
	"net/url"
	"strconv"
	"time"
)

const inboundFilterLists = "/inbound/filter-lists"

// FilterListsHref is the address of the page with the add form filled in: list
// is "allow" or "deny", kind "address", "domain" or "ip". The Inbound log links
// its rows here.
func FilterListsHref(list, kind, value string) string {
	return inboundFilterLists + "?list=" + url.QueryEscape(list) + "&kind=" + url.QueryEscape(kind) + "&value=" + url.QueryEscape(value)
}

// FilterLists is the data of the page: the form that adds an entry, the two
// lists side by side and what happens to them after that.
type FilterLists struct {
	Meta
	Head  Head
	Flash *Flash

	Add         Box
	Action      string
	ListOptions []Option
	KindOptions []Option
	// List, Kind, Value and Note are what the form shows: blank, prefilled from
	// the Inbound log, or what was typed into a form that was refused.
	List  string
	Kind  string
	Value string
	Note  string

	Deny  FilterList
	Allow FilterList

	Where      Box
	WhereFacts []Fact
}

// FilterList is one of the two boxes of entries.
type FilterList struct {
	Box   Box
	Rows  []FilterRow
	Empty EmptyState
}

// FilterRow is one entry of a list: the value with the kind of entry as a tag,
// the operator's note, the day it was added and the way to remove it.
type FilterRow struct {
	Value  string
	Kind   Tag
	Note   string
	Added  string
	Remove RowAction
}

// FilterEntryInput is what the page knows of one entry of a list.
type FilterEntryInput struct {
	ID    int64
	List  string
	Kind  string
	Value string
	Note  string
	Added time.Time
}

// FilterListsInput is the state of the filter the lists are written for: whether
// a filter is attached at all, and when the map files it reads were last written.
type FilterListsInput struct {
	FilterOn    bool
	LastWritten time.Time
}

// NewFilterLists builds the page. The form starts on the Deny list for a sender
// address, the commonest correction.
func NewFilterLists(m Meta, in FilterListsInput) *FilterLists {
	m.Title, m.Section, m.Page = "Filter lists", "inbound", "filter-lists"
	p := &FilterLists{
		Meta: m,
		Head: Head{
			Kicker: "Inbound · global role",
			Title:  "Filter lists",
			Lead:   Plain("Correct the spam filter by hand: senders it must always let through, and senders it must always turn away. One pair of lists for the whole instance."),
		},
		Add:    Box{No: "01", Title: "Add an entry", Help: topicLink(HelpFilterLists, "How the lists are applied")},
		Action: inboundFilterLists,
		ListOptions: []Option{
			{Value: "deny", Label: "Deny — always turn away"},
			{Value: "allow", Label: "Allow — always let through"},
		},
		KindOptions: []Option{
			{Value: "address", Label: "a sender address"},
			{Value: "domain", Label: "a sender domain"},
			{Value: "ip", Label: "a client address or network"},
		},
		List: "deny",
		Kind: "address",

		Deny:  FilterList{Box: Box{No: "02", Title: "Deny"}, Empty: EmptyState{Icon: "ti-filter", Text: Plain("No sender is turned away by hand.")}},
		Allow: FilterList{Box: Box{No: "03", Title: "Allow"}, Empty: EmptyState{Icon: "ti-filter", Text: Plain("No sender is let through by hand.")}},
		Where: Box{No: "04", Title: "Where the lists go"},
	}
	p.Deny.Box.End = Plain(plural(0, "entry", "entries") + " · wins over Allow")
	p.Allow.Box.End = Plain(plural(0, "entry", "entries"))
	p.setWhere(in)
	return p
}

// setWhere writes box 04: the tag that says whether anything reads the lists,
// and the four facts about where they go. With no filter attached the lists are
// still kept and editable, and the box says so instead of promising an effect.
func (p *FilterLists) setWhere(in FilterListsInput) {
	applied := Plain("rspamd, from map files it rereads within seconds of a change")
	p.Where.End = Rich(TagOf("ok", "applied"))
	if !in.FilterOn {
		p.Where.End = Rich(TagOf("off", "not applied"))
		applied = Rich("Nothing — the spam filter is off. The lists are kept and can be edited; nothing reads them until ",
			Code("INBOUND_ANTISPAM_MILTER"), " is set.")
	}
	written := "never"
	if !in.LastWritten.IsZero() {
		written = in.LastWritten.UTC().Format("2006-01-02 15:04") + " UTC"
	}
	p.WhereFacts = []Fact{
		{Label: "Applied by", Value: applied},
		{Label: "Files", Value: Rich(Mono("/data/antispam/"), " — six, one for each list and kind of entry")},
		{Label: "Last written", Value: Plain(written)},
		{Label: "When both lists match", Value: Plain("Deny wins, whichever entries they are.")},
	}
}

// WithEntries sets the two tables, each counted in its box head.
func (p *FilterLists) WithEntries(entries []FilterEntryInput) *FilterLists {
	p.Deny.Rows, p.Allow.Rows = nil, nil
	for _, e := range entries {
		row := FilterRow{
			Value: e.Value, Kind: Tag{Label: e.Kind}, Note: e.Note, Added: e.Added.UTC().Format("2006-01-02"),
			Remove: RowAction{Label: "Remove", Post: inboundFilterLists + "/" + strconv.FormatInt(e.ID, 10) + "/delete", Danger: true},
		}
		switch e.List {
		case "deny":
			p.Deny.Rows = append(p.Deny.Rows, row)
		case "allow":
			p.Allow.Rows = append(p.Allow.Rows, row)
		}
	}
	p.Deny.Box.End = Plain(plural(int64(len(p.Deny.Rows)), "entry", "entries") + " · wins over Allow")
	p.Allow.Box.End = Plain(plural(int64(len(p.Allow.Rows)), "entry", "entries"))
	return p
}

// WithForm sets what the add form shows. The handler passes only what it has
// checked to be a list and a kind the form offers, or what was submitted to a
// form it is showing again.
func (p *FilterLists) WithForm(list, kind, value, note string) *FilterLists {
	p.List, p.Kind, p.Value, p.Note = list, kind, value, note
	return p
}

// WithResult shows the result of the last action, or the refusal of the add
// form, between the head and the first box.
func (p *FilterLists) WithResult(flash, formErr string) *FilterLists {
	p.Flash = resultFlash(flash, formErr)
	return p
}
