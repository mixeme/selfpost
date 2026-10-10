package view

// The data of the kit page (GET /server/components): every partial of
// components.html in every state it has, filled the way the mockup components
// page (docs/assets/panel-redesign/panel/src/components.html) is filled, same
// boxes, same order, same words. It is production code, not a test fixture,
// because the handler serves it; the guard tests render the same value
// (fixtures_test.go).

// Kit is the data of the kit page, rendered by templates/kit.html. The fields
// are named after the boxes of the page they fill.
type Kit struct {
	Meta
	Head       Head
	Flash      Flash
	FlashError Flash

	Layouts       Box
	LayoutFacts   []Fact
	Postmark      Box
	Postmarks     []Postmark
	StatusTag     Box
	StatusRows    []KitStatusRow
	BoxHead       Box
	BoxFoot       Foot
	DangerZone    Box
	Credential    Box
	HealthCard    Box
	HealthCards   []HealthCard
	DNSRecord     Box
	Records       []Record
	Tables        Box
	TablesFoot    Foot
	RowAction     RowAction
	LogPane       Box
	LogLines      []LogLine
	Facts         Box
	FactList      []Fact
	Timeline      Box
	Steps         []Step
	EmptyState    Box
	Empty         EmptyState
	ConfirmList   Box
	Confirm       []Text
	SideMenu      SideMenu
	FormRows      Box
	Route         Route
	HelpTopic     HelpTopic
	Shell         Box
	ShellFacts    []Fact
	TypeScale     Box
	TypeScaleFoot Foot
	KickerSample  string
}

// KitStatusRow is one row of the status_tag table: the words that map to one
// tag, and the tag.
type KitStatusRow struct {
	Words string
	Tag   Tag
}

// KitPage returns the kit page's data. The page is shown to the global role, so
// the shell is theirs; it sits under Server in the address and in the menu.
func KitPage() *Kit {
	return &Kit{
		Meta: Meta{Title: "Components", User: "admin", IsGlobal: true, Section: "server"},
		Head: Head{
			Postmark: &Postmark{Top: "SELFPOST", Word: "REF", Bottom: "DESIGN CONTRACT", Level: LevelOK},
			Kicker:   "Reference · page_head",
			Title:    "Components",
			Lead:     Plain("Every building block of the panel, in every state it has. A screen is assembled from these and from plain Bulma — nothing else. The title of each box is the name of its template partial."),
			Actions:  []Action{{Label: "Help", Icon: "ti-help-circle", Href: "/help"}},
		},
		Flash:      Flash{Text: Rich(Strong("flash"), " — the result of the last action, between page_head and the first box. Success, or…")},
		FlashError: Flash{Error: true, Text: Rich("…", Strong("flash, error"), " — a failure that belongs to the page, not to one field. A field's error stays under the field in Bulma's ", Code("help is-danger"), ".")},

		Layouts: Box{No: "00", Title: "Page layouts", End: Plain("A page is exactly one of these")},
		LayoutFacts: []Fact{
			{Label: "list", Value: Rich("page_head, then full-width boxes: the add form first, the table second. ", Link("/outbound/domains", "Example"))},
			{Label: "detail", Value: Rich("page_head, then ", Code("columns"), ": ", Code("is-3"), " side menu + ", Code("is-9"), " boxes.")},
			{Label: "form", Value: Rich("page_head, then boxes in equal ", Code("columns"), ", one submit row under them.")},
			{Label: "signed out", Value: Rich("The split brand / form screen, no navbar. ", Link("/login", "Example"))},
		},

		Postmark: Box{No: "01", Title: "postmark", End: Plain("Worst status of the page's checks")},
		Postmarks: []Postmark{
			{Top: "SELFPOST", Word: "OK", Bottom: "6 OF 6 CHECKS", Level: LevelOK},
			{Top: "SELFPOST", Word: "WARN", Bottom: "1 OF 6 CHECKS", Level: LevelWarn},
			{Top: "DNS", Word: "FAIL", Bottom: "1 OF 3 RECORDS", Level: LevelFail},
		},

		StatusTag: Box{No: "03", Title: "status_tag", End: Plain("One mapping, one function")},
		StatusRows: []KitStatusRow{
			{"ok, published, running, sent", Tag{Status: "ok"}},
			{"warn, deferred, quiet", Tag{Status: "warn"}},
			{"error, fail, mismatch, bounced", Tag{Status: "fail"}},
			{"unknown, off, neutral counts", Tag{Status: "off"}},
			{"held: quarantine · role: global", Tag{Status: "quarantine"}},
		},

		BoxHead: Box{No: "02", Title: "box · box_head",
			End:  Plain("The end slot: a note, a tag, a link or a filter form"),
			Help: &HelpLink{Href: "/help#checks", Title: "Optional: the section's Help topic"}},
		BoxFoot:    Foot{Text: Plain("box_foot — a legend, paging"), End: Plain("end slot")},
		DangerZone: Box{Icon: "ti-alert-triangle", Title: "box · sp-danger-zone", Variant: BoxDanger},
		Credential: Box{Icon: "ti-eye", Title: "box · sp-credential", End: Plain("Shown once"), Variant: BoxCredential},

		HealthCard: Box{No: "04", Title: "health_card", End: Plain("Overview only · each card links to its detail")},
		HealthCards: []HealthCard{
			{Name: "Name", Value: "Verdict in words", Sub: "ok · one supporting fact", Icon: "ti-server-cog", Href: "#"},
			{Name: "Name", Value: "Expires in 12 days", Sub: "sp-warn", Icon: "ti-certificate", Href: "#", Level: LevelWarn},
			{Name: "Name", Value: "1 of 2 answering", Sub: "sp-fail", Icon: "ti-plug-connected-x", Href: "#", Level: LevelFail},
		},

		DNSRecord: Box{No: "05", Title: "dns_record · copy_field", End: Plain("One block per record")},
		Records: []Record{
			{Name: "Record", Status: Tag{Status: "published"}, Note: Plain("A one-line note"),
				Host: "copy_field — single line", Type: "TXT",
				Value: "copy_field — multi-line, for a DKIM key", ValueRows: 2},
			{Name: "Record", Status: Tag{Status: "mismatch"}, Note: Plain(`"In DNS now" appears only when it differs`),
				InDNS: "what the resolver returned", Problem: Plain("What is wrong and the exact fix.")},
		},

		Tables:     Box{No: "07", Title: "Table conventions", End: Rich("Always inside ", Code("table-container"), " · an address breaks after its @")},
		TablesFoot: Foot{Text: Plain("Page 1 of 9"), Link: &Anchor{Href: "#", Label: "Older →"}},
		RowAction:  RowAction{Label: "Delete", Post: "#", Danger: true},

		LogPane: Box{No: "09", Title: "log_pane", End: Plain("sp-d time · sp-w warning · sp-e error")},
		LogLines: []LogLine{
			{Time: "13:58:07", Text: "an ordinary line"},
			{Time: "13:58:09", Text: "a deferred delivery — sp-w", Level: "warn"},
			{Time: "13:57:41", Text: "a reject or a bounce — sp-e", Level: "error"},
		},

		Facts: Box{No: "06", Title: "facts"},
		FactList: []Fact{
			{Label: "Label", Value: Plain("Value")},
			{Label: "Mono value", Value: Plain("4XcB7k2Jm9z1"), Mono: true},
			{Label: "sp-big", Value: Plain("5 days"), Big: true, Note: Plain("a supporting line")},
		},

		Timeline: Box{No: "08", Title: "timeline"},
		Steps: []Step{
			{Time: "13:58:07 UTC", Strong: "Done", Text: Plain(" — default")},
			{Time: "13:58:09 UTC", Strong: "Deferred", Text: Plain(" — sp-warn"), Level: LevelWarn},
			{Time: "14:40:00 UTC", Strong: "Bounced", Text: Plain(" — sp-fail"), Level: LevelFail},
			{Time: "not yet", Text: Plain("Expected next — sp-pending"), Level: LevelPending},
		},

		EmptyState: Box{No: "10", Title: "empty_state"},
		Empty:      EmptyState{Icon: "ti-stack-2", Text: Plain("One sentence: what is absent and why that is fine.")},

		ConfirmList: Box{No: "11", Title: "confirm_list"},
		Confirm: []Text{
			Plain("each thing that will be lost, one per line;"),
			Plain("inside an sp-danger-zone box, above the danger button."),
		},

		SideMenu: SideMenu{Groups: []MenuGroup{
			{Label: "side_menu", Items: []MenuItem{
				{Label: "Current", Icon: "ti-world-check", Href: "#", Active: true, Tag: &Tag{Status: "fail", Label: "1"}},
				{Label: "Section", Icon: "ti-apps", Href: "#", Tag: &Tag{Status: "off", Label: "2"}},
			}},
			{Label: "Rarely changed", Items: []MenuItem{
				{Label: "Delete", Icon: "ti-trash", Href: "#", Danger: true},
			}},
		}},

		FormRows: Box{No: "12", Title: "Form rows · route · help_topic"},
		Route: Route{
			{Text: "from@example.org"}, {Arrow: true}, {Text: "to@example.net"}, {Tag: &Tag{Status: "deferred"}},
		},
		HelpTopic: HelpTopic{Title: "help_topic", Body: []Text{Plain("A heading and a paragraph; topics stack inside one box on the Help page.")}},

		Shell: Box{No: "13", Title: "shell", End: Plain("Owned by the layout — a screen never writes it")},
		ShellFacts: []Fact{
			{Label: "Name", Value: Plain("The wordmark, not the stamp: at nav size the stamp's perforation is under a pixel and its brick field dissolves into the bar. The stamp lives on sign-in, setup and the favicon.")},
			{Label: "Edge", Value: Rich("Built like the mark: the brick field, then the stamp's paper margin, and the perforation along the outer edge of that margin — round holes, flat teeth, the ink hairline. One tile, ", Code("perf-edge.svg"), ".")},
			{Label: "Sibling strip", Value: Plain("The pages of the current group, under the edge. Five or six entries; past that the group is split.")},
			{Label: "Menus", Value: Plain("Bulma hover dropdowns, no JavaScript. The user menu holds Account, Help and Sign out.")},
		},

		TypeScale:     Box{No: "14", Title: "Type scale", End: Plain("Three sizes · nothing to be read is under 13px")},
		TypeScaleFoot: Foot{Bare: true, Text: Rich("Bulma's ", Code("is-small"), " and ", Code("is-size-7"), " are not used: a control is never small, and ", Code("sp-small"), " replaces the helper. The postmark's fine print is an imprint, not text.")},
		KickerSample:  "Upper-case mono label",
	}
}
