package view

// The data of the Outbound domain pages: the list (templates/out_domains.html,
// mockup out-domains.html), one domain (out_domain.html), its settings
// (out_domain_settings.html) and the confirmation before it is deleted
// (out_domain_delete.html). The application form and the page that shows a
// password once are in outbound_app.go.
//
// A page is a typed struct that embeds Meta, built by a constructor and filled
// by With… methods; the handler reads the stores and the DNS checker, the view
// decides what is said and how it is worded.

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const outboundDomains = "/outbound/domains"

// outboundCrumbs is the trail above a page of one domain.
func outboundCrumbs(id int64, name string) []Crumb {
	return []Crumb{{Href: outboundDomains, Label: "Outbound domains"}, {Href: DomainHref(id), Label: name}}
}

// DomainHref is the address of an outbound domain's page.
func DomainHref(id int64) string { return outboundDomains + "/" + strconv.FormatInt(id, 10) }

// plural writes n and the noun in the right number: "1 message", "2 messages".
func plural(n int64, one, many string) string {
	if n == 1 {
		return FormatCount(n) + " " + one
	}
	return FormatCount(n) + " " + many
}

// StatsLead is the sentence under the title of a domain or an application: how
// much mail it sent in the window, its busiest hour and its average. avg is the
// average per hour as the store formats it.
func StatsLead(total, peak int64, avg string, days int) Text {
	return Plain(plural(total, "message", "messages") + " in " + plural(int64(days), "day", "days") +
		" · peak " + FormatCount(peak) + " msg/h · average " + avg + " msg/h")
}

// FormatActivity is the Domains table's last column: the mail of the window and
// its busiest hour ("1 284 msg · peak 96/h").
func FormatActivity(total, peak int64) string {
	return FormatMessages(total) + " · peak " + FormatCount(peak) + "/h"
}

// FormatAge says how long ago something was, as a box's end slot puts it:
// "just now", "3 min ago", "2 h ago", then the date. A zero time is "".
func FormatAge(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	switch d := now.Sub(t); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d/time.Hour))
	default:
		return t.UTC().Format("2006-01-02")
	}
}

// DNSStatusTag is the tag of one published record, from the status the DNS
// checker reports (ok, warn, error or unknown): green when it is published and
// right, amber when it is published but weak, red when it is missing or wrong.
// found says whether the resolver returned anything at all — an error with a
// record in DNS is a mismatch, without one the record is missing.
func DNSStatusTag(status string, found bool) Tag {
	switch status {
	case "ok":
		return Tag{Status: "published", Label: "published"}
	case "warn":
		return Tag{Status: "warn", Label: "weak"}
	case "error":
		if found {
			return Tag{Status: "mismatch", Label: "mismatch"}
		}
		return Tag{Status: "fail", Label: "missing"}
	}
	return Tag{Status: "unknown", Label: "not checked"}
}

// tagLevel is the level (ok, warn or fail) a tag's status stands for. A status
// that says nothing — unknown, off — is a warning, never a pass.
func tagLevel(status string) string {
	switch status {
	case "ok", "published", "running", "sent":
		return LevelOK
	case "error", "fail", "mismatch", "bounced":
		return LevelFail
	}
	return LevelWarn
}

// appsPhrase counts the applications a domain has, as a sentence needs them:
// "its application", "both applications", "all 3 applications"; "" for none.
func appsPhrase(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return "its application"
	case 2:
		return "both applications"
	}
	return "all " + strconv.Itoa(n) + " applications"
}

// ---- The records of a domain

// DNSCheck is what the DNS checker found for one record: its status (ok, warn,
// error or unknown), the sentence that explains it and the values it found.
type DNSCheck struct {
	Status string
	Detail string
	Found  []string
}

// applyCheck puts a check's verdict on a record: the status tag and, when the
// record is not as it should be, what DNS holds now and why that is wrong. A
// record DNS does not hold at all has nothing to compare, so the explanation
// goes under the value to publish instead. help is the record's standing note
// under that value.
func applyCheck(r *Record, c DNSCheck, standing Text, mismatch Text) {
	found := len(c.Found) > 0
	r.Status = DNSStatusTag(c.Status, found)
	r.ValueHelp = standing
	if c.Status == "ok" {
		return
	}
	if found {
		r.InDNS = strings.Join(c.Found, "  ")
		r.Problem = Rich(c.Detail, mismatch)
		return
	}
	if c.Detail != "" {
		r.ValueHelp = append(Plain(c.Detail+" "), standing...)
	}
}

// DKIMRecord is the DKIM record to publish: the key is not a secret, and the
// selector says which one it is.
func DKIMRecord(selector, host, value string, c DNSCheck) Record {
	r := Record{Name: "DKIM", Note: Rich("Not a secret · selector ", Code(selector)),
		Host: host, Type: "TXT", Value: value, ValueRows: 3}
	applyCheck(&r, c, nil, nil)
	return r
}

// SPFRecord is the SPF record to publish. The check is shallow — it does not
// follow include: — which a mismatch says.
func SPFRecord(domain, value string, c DNSCheck) Record {
	r := Record{Name: "SPF", Note: Plain("Merge into the existing record — never publish a second one"),
		Host: domain, Type: "TXT", Value: value}
	applyCheck(&r, c, nil, Rich(" Shallow check: ", Code("include:"), " is not followed."))
	return r
}

// DMARCRecordInput is what a DMARC record needs besides its check: where the
// reports go in words (Source: the address, or "no reports") and the page that changes it,
// the page of the reports themselves ("" when the panel has none), and whether
// the report address is on the sending domain with nobody to read it there.
type DMARCRecordInput struct {
	Host         string
	Value        string
	Check        DNSCheck
	Source       string
	SettingsHref string
	ReportsHref  string
	SameDomain   bool
}

// DMARCRecord is the DMARC record to publish: p=none changes nothing about
// delivery, so the standing note says how to tighten it.
func DMARCRecord(in DMARCRecordInput) Record {
	r := Record{Name: "DMARC", Host: in.Host, Type: "TXT", Value: in.Value,
		Note: Rich("Report address: ", Link(in.SettingsHref+"#reports", in.Source))}
	reports := Rich("reports")
	if in.ReportsHref != "" {
		reports = Rich(Link(in.ReportsHref, "reports"))
	}
	standing := Rich(Code("p=none"), " does not affect delivery. Tighten to ", Code("quarantine"), ", then ", Code("reject"),
		", once the ", reports, " look clean.")
	if in.SameDomain {
		standing = append(standing, Inline{Text: " The report address is on this sending domain, where SelfPost receives no mail — use a mailbox on another domain."})
	}
	applyCheck(&r, in.Check, standing, nil)
	return r
}

// ReportAuthRecord is the record the domain that receives the reports must
// publish when it is not the sending domain.
func ReportAuthRecord(host, value string, c DNSCheck) Record {
	r := Record{Name: "Report authorization", Note: Plain("Needed because the address is on another domain than the senders"),
		Host: host, Type: "TXT", Value: value}
	applyCheck(&r, c, nil, nil)
	return r
}

// ---- Outbound domains

// OutDomains is the data of the list. The global role adds and deletes domains
// (CanManage); a domain administrator sees the list alone, so the add box is
// absent for them and the table is the first box.
type OutDomains struct {
	Meta
	Head  Head
	Flash *Flash

	CanManage bool
	Add       Box
	AddHelp   Text
	// AddName is what was typed into the form, shown again when it is refused.
	AddName string

	List Box
	// Days is the length of the send window of the "N days" column.
	Days   int
	Rows   []OutDomainRow
	Empty  EmptyState
	Legend Foot
}

// OutDomainRow is one line of the Domains table: the domain, the verdict of
// each DNS check as a tag, the DKIM selector, the applications and the mail of
// the window (FormatActivity). DeleteHref is empty when the viewer may not
// delete.
type OutDomainRow struct {
	Name       string
	Href       string
	DeleteHref string
	DNS        []Tag
	Selector   string
	Apps       int
	Activity   string
}

// NewOutDomains builds the list for a viewer. canManage is the global role;
// days is the send window of the last column.
func NewOutDomains(m Meta, canManage bool, days int) *OutDomains {
	m.Title, m.Section, m.Page = "Outbound domains", "outbound", "domains"
	p := &OutDomains{
		Meta: m,
		Head: Head{
			Kicker: "Outbound",
			Title:  "Outbound domains",
			Lead:   Plain("Domains this relay signs and sends for. Each gets a DKIM key and its own application logins."),
		},
		CanManage: canManage,
		Days:      days,
		List:      Box{No: "01", Title: "Domains"},
		Empty:     EmptyState{Icon: "ti-world-upload", Text: Plain("No outbound domains yet.")},
		Legend: Foot{Bare: true,
			Text: Plain("Each tag is one check. Green — published and correct; amber — published but weak; red — missing or wrong.")},
	}
	if canManage {
		p.Add = Box{No: "01", Title: "Add a domain"}
		p.List.No = "02"
		p.AddHelp = Rich("A DKIM key is generated at once; the next page shows the three DNS records to publish. Moving a domain from another SelfPost? ",
			Link("/server/backup#import", "Import it"), " instead — the key and passwords come across.")
		p.Empty.Text = Plain("No outbound domains yet. Add the first one above.")
	}
	p.List.End = Plain("DNS cached a few minutes")
	return p
}

// WithRows sets the table and counts it in the box head.
func (p *OutDomains) WithRows(rows []OutDomainRow) *OutDomains {
	p.Rows = rows
	p.List.End = Plain(plural(int64(len(rows)), "domain", "domains") + " · DNS cached a few minutes")
	return p
}

// WithResult shows the result of the last action, or the refusal of the add
// form (with what was typed kept in the field), between the head and the first
// box.
func (p *OutDomains) WithResult(flash, formErr, typed string) *OutDomains {
	p.AddName = typed
	p.Flash = resultFlash(flash, formErr)
	return p
}

// resultFlash is the flash of a page that shows either an error or a success.
func resultFlash(flash, formErr string) *Flash {
	switch {
	case formErr != "":
		return &Flash{Error: true, Text: Plain(formErr)}
	case flash != "":
		return &Flash{Text: Plain(flash)}
	}
	return nil
}

// ---- One outbound domain

// OutDomain is the data of a domain's page: the DNS records to publish and
// what they check as, the applications, and how to connect. The side menu leads
// to the boxes, to the pages about the same domain and to its settings.
type OutDomain struct {
	Meta
	Head  Head
	Flash *Flash

	id   int64
	name string

	DNS     Box
	Records []Record

	Apps Box
	// Days is the length of the send window of the "N days" column.
	Days      int
	AppRows   []OutAppRow
	AppsEmpty EmptyState
	AppsFoot  Foot

	Connection Box
	ConnFacts  []Fact

	logHref, dmarcHref string
	limitTag           *Tag
	canDelete          bool
}

// OutAppRow is one line of the Applications table. Senders are the addresses
// the login may send as (*@example.org for the whole domain); Limits are tags,
// the application's own rate limit or "domain" and its client IP allow-list.
// Edit leads to the application's page; New password and Delete are POSTs in
// the row itself (RowAction), each asking before it is sent.
type OutAppRow struct {
	Login    string
	Senders  []string
	Activity string
	Limits   []Tag
	Edit     string
	Actions  []RowAction
}

// NewOutDomain builds the page of a domain. canDelete is the global role: it
// adds the "Delete domain" entry to the menu.
func NewOutDomain(m Meta, id int64, name string, canDelete bool) *OutDomain {
	m.Title, m.Section, m.Page = name+" · Outbound domains", "outbound", "domains"
	href := DomainHref(id)
	return &OutDomain{
		Meta: m,
		Head: Head{
			Crumbs: []Crumb{{Href: outboundDomains, Label: "Outbound domains"}},
			Title:  name,
			Actions: []Action{
				{Label: "Re-check DNS", Icon: "ti-refresh", Post: href + "/dns-recheck"},
				{Label: "Add application", Icon: "ti-plus", Href: href + "/applications/new", Primary: true},
			},
		},
		id: id, name: name, canDelete: canDelete,
		DNS: Box{No: "01", Title: "DNS records", ID: "dns",
			Help: topicLink(HelpDNS, "What to publish and why")},
		Apps: Box{No: "02", Title: "Applications", ID: "apps", End: Plain("SASL logins that may send as this domain"),
			Help: topicLink(HelpApps, "What an application is")},
		AppsEmpty:  EmptyState{Icon: "ti-apps", Text: Plain("No applications yet. Add one to let a program send as this domain.")},
		AppsFoot:   Foot{Bare: true, Text: Plain("New password and Delete ask for confirmation; the old password stops working at once.")},
		Connection: Box{No: "03", Title: "Connection", ID: "connection", End: Plain("The same for every domain")},
	}
}

// WithStats puts the mail of the window under the title.
func (p *OutDomain) WithStats(total, peak int64, avg string, days int) *OutDomain {
	p.Head.Lead = StatsLead(total, peak, avg, days)
	p.Days = days
	return p
}

// WithRecords sets the records to publish, in order, and derives from their
// tags what the page says about them: the stamp (the worst record sets it, and
// it counts the records that are not fine), the count in the side menu and, as
// the box's end slot, how long ago they were checked ("" says nothing).
func (p *OutDomain) WithRecords(records []Record, checked string) *OutDomain {
	p.Records = records
	if checked != "" {
		p.DNS.End = Plain("Checked " + checked)
	}
	level, bad := LevelOK, 0
	for _, r := range records {
		switch l := tagLevel(r.Status.Status); {
		case l == LevelOK:
		case l == LevelFail:
			level, bad = LevelFail, bad+1
		default:
			if level == LevelOK {
				level = LevelWarn
			}
			bad++
		}
	}
	word := map[string]string{LevelOK: "OK", LevelWarn: "WARN", LevelFail: "FAIL"}[level]
	count := bad
	if level == LevelOK {
		count = len(records)
	}
	if len(records) > 0 {
		p.Head.Postmark = &Postmark{Top: "DNS", Word: word,
			Bottom: fmt.Sprintf("%d OF %d RECORDS", count, len(records)), Level: level}
	}
	return p
}

// WithApplications sets the Applications table.
func (p *OutDomain) WithApplications(rows []OutAppRow) *OutDomain {
	p.AppRows = rows
	return p
}

// WithConnection says how to connect: the server's name and whether the
// submission port (587) is on beside the implicit-TLS one (465).
func (p *OutDomain) WithConnection(host string, submission bool) *OutDomain {
	if host == "" {
		host = "(SELFPOST_HOSTNAME is not set)"
	}
	p.ConnFacts = []Fact{
		{Label: "Server", Value: Plain(host), Mono: true},
		{Label: "Port 465", Value: Plain("SSL/TLS (implicit)")},
	}
	if submission {
		p.ConnFacts = append(p.ConnFacts, Fact{Label: "Port 587", Value: Plain("STARTTLS (submission)")})
	}
	p.ConnFacts = append(p.ConnFacts, Fact{Label: "Authentication", Value: Plain("Application login, required on every port")})
	return p
}

// WithSeeAlso adds the pages about the same domain to the menu: its entries in
// the log, and in DMARC reports when the domain has them ("" leaves one out).
func (p *OutDomain) WithSeeAlso(logHref, dmarcHref string) *OutDomain {
	p.logHref, p.dmarcHref = logHref, dmarcHref
	return p
}

// WithRateLimit says in the menu what the domain's rate limit is: "auto",
// "manual", or nothing when it has none of its own.
func (p *OutDomain) WithRateLimit(auto, manual bool) *OutDomain {
	switch {
	case auto:
		p.limitTag = &Tag{Status: "ok", Label: "auto"}
	case manual:
		p.limitTag = &Tag{Status: "ok", Label: "manual"}
	}
	return p
}

// WithResult shows the result of the last action between the head and the boxes.
func (p *OutDomain) WithResult(flash string) *OutDomain {
	p.Flash = resultFlash(flash, "")
	return p
}

// Menu is the side menu: this page's boxes with their counts, the pages about
// the same domain, and the entries of its settings.
func (p *OutDomain) Menu() SideMenu {
	here := MenuGroup{Label: "This domain", Items: []MenuItem{
		{Label: "DNS records", Icon: "ti-world-check", Href: "#dns", Active: true, Tag: p.dnsCount()},
		{Label: "Applications", Icon: "ti-apps", Href: "#apps"},
		{Label: "Connection", Icon: "ti-plug", Href: "#connection"},
	}}
	if n := len(p.AppRows); n > 0 {
		here.Items[1].Tag = &Tag{Label: strconv.Itoa(n)}
	}
	menu := SideMenu{Groups: []MenuGroup{here}}
	var see []MenuItem
	if p.logHref != "" {
		see = append(see, MenuItem{Label: "Log for this domain", Icon: "ti-list-details", Href: p.logHref})
	}
	if p.dmarcHref != "" {
		see = append(see, MenuItem{Label: "DMARC reports", Icon: "ti-report-analytics", Href: p.dmarcHref})
	}
	if len(see) > 0 {
		menu.Groups = append(menu.Groups, MenuGroup{Label: "See also", Items: see})
	}
	settings := DomainHref(p.id) + "/settings"
	rare := MenuGroup{Label: "Rarely changed", Items: []MenuItem{
		{Label: "Report address", Icon: "ti-mail-forward", Href: settings + "#reports"},
		{Label: "Rate limit", Icon: "ti-gauge", Href: settings + "#limit", Tag: p.limitTag},
		{Label: "Export", Icon: "ti-file-export", Href: settings + "#export"},
	}}
	if p.canDelete {
		rare.Items = append(rare.Items, MenuItem{Label: "Delete domain", Icon: "ti-trash", Href: DomainHref(p.id) + "/delete", Danger: true})
	}
	menu.Groups = append(menu.Groups, rare)
	return menu
}

// dnsCount is the tag beside "DNS records" in the menu: how many records are
// not fine, in the colour of the worst. Nothing when all are.
func (p *OutDomain) dnsCount() *Tag {
	bad, level := 0, LevelOK
	for _, r := range p.Records {
		switch l := tagLevel(r.Status.Status); {
		case l == LevelFail:
			bad, level = bad+1, LevelFail
		case l == LevelWarn:
			bad++
			if level == LevelOK {
				level = LevelWarn
			}
		}
	}
	if bad == 0 {
		return nil
	}
	return &Tag{Status: level, Label: strconv.Itoa(bad)}
}

// ---- Settings of an outbound domain

// OutDomainSettings is the data of Domain settings: where DMARC reports go,
// the export of the domain, its rate limit and, for the global role, deleting
// it. Each box is its own form with its own POST.
type OutDomainSettings struct {
	Meta
	Head  Head
	Flash *Flash

	Reports       Box
	ReportsAction string
	RuaOptions    []Option
	RuaSelected   string
	RuaCustom     string
	RuaHelp       Text
	// RuaMine is the signed-in user's profile e-mail, which the button beside
	// the typed address fills the field with; "" leaves the button out.
	RuaMine string

	Export       Box
	ExportAction string
	Encrypt      bool
	PasswordMin  int

	Limit Box
	Rate  DomainRateLimit
	// LimitAction and the two actions beside it are the POST addresses of the
	// rate-limit form: save, recalculate and remove.
	LimitAction  string
	RecalcAction string
	LimitNote    Text

	CanDelete  bool
	Delete     Box
	DeleteText Text
	DeleteLink Action
}

// DomainRateLimit is the domain's rate limit as its form shows it. Active says
// that the domain has a limit of its own in force; Auto that it is worked out
// from the statistics. The numbers are the strings the fields hold.
type DomainRateLimit struct {
	Active      bool
	Auto        bool
	MaxMessages string
	Window      string
	Multiplier  string
	// Computed is the limit auto mode arrived at (messages per window), "" when
	// there was no traffic to compute it from; Peak is the busiest hour of the
	// statistics window and Updated when the limit was last recalculated.
	Computed string
	Peak     int64
	Updated  string

	// Level 1: the per-client-IP backstop nothing below may exceed.
	L1Messages int
	L1Window   int
	// The multiplier's range, as the field states it.
	MinMultiplier     string
	MaxMultiplier     string
	DefaultMultiplier string
}

// AutoHelp is the line under the multiplier: what the limit computes to now.
func (r DomainRateLimit) AutoHelp() Text {
	return autoLimitHelp(r.Computed, r.L1Window, r.Peak, r.Updated)
}

// autoLimitHelp says what an automatic limit works out to: the messages per
// window it computed from the statistics, the busiest hour it was computed
// against and when. computed is "" before there was any traffic to compute it
// from.
func autoLimitHelp(computed string, window int, peak int64, updated string) Text {
	if computed == "" {
		t := Rich("No limit is computed yet: zero traffic keeps auto inactive until messages are sent.")
		if peak > 0 {
			t = append(t, Inline{Text: " Peak was " + FormatCount(peak) + " msg/h."})
		}
		return t
	}
	t := Rich("Computed limit: ", Strong(computed), " messages / "+strconv.Itoa(window)+" s. Peak was "+FormatCount(peak)+" msg/h.")
	if updated != "" {
		t = append(t, Inline{Text: " Last recalculated " + updated + "."})
	}
	return t
}

// NewOutDomainSettings builds the page of a domain's settings. canDelete is the
// global role; apps is how many applications deleting the domain would take.
func NewOutDomainSettings(m Meta, id int64, name string, apps int, canDelete bool) *OutDomainSettings {
	m.Title, m.Section, m.Page = "Settings · "+name, "outbound", "domains"
	href := DomainHref(id)
	p := &OutDomainSettings{
		Meta: m,
		Head: Head{
			Crumbs: outboundCrumbs(id, name),
			Title:  "Domain settings",
			Lead:   Plain("The things set once and rarely touched: where DMARC reports go, the sending ceiling, moving the domain elsewhere."),
		},
		Reports:       Box{No: "01", Title: "DMARC report address", ID: "reports", Help: topicLink(HelpDMARC, "Where DMARC reports go")},
		ReportsAction: href + "/settings/reports",
		Export:        Box{No: "03", Title: "Export domain", ID: "export"},
		ExportAction:  href + "/settings/export",
		Encrypt:       true,
		Limit: Box{No: "02", Title: "Rate limit", ID: "limit",
			Help: topicLink(HelpLimits, "Levels 1 and 2")},
		LimitAction:  href + "/settings/ratelimit",
		RecalcAction: href + "/settings/ratelimit/recalc",
		CanDelete:    canDelete,
	}
	// The topic about exports is in the Server group of Help, which a domain
	// administrator is not shown, so the box links to it for the global role only.
	if m.IsGlobal {
		p.Export.Help = topicLink(HelpBackup, "Moving a domain to another instance")
	}
	if canDelete {
		text := "Removes the DKIM key"
		if n := appsPhrase(apps); n != "" {
			text += " and " + n
		}
		p.Delete = Box{No: "04", Title: "Delete domain", Variant: BoxDanger}
		p.DeleteText = Plain(text + ". Asks once more before doing it.")
		p.DeleteLink = Action{Label: "Delete " + name + "…", Icon: "ti-trash", Href: href + "/delete", Danger: true}
	}
	return p
}

// The three choices of a domain's report address, the values of the select: no
// reports, the address SelfPost hosts for the domain (offered only while hosted
// reports are enabled) and an address typed for the domain.
const (
	ReportNone   = "none"
	ReportHosted = "hosted"
	ReportCustom = "custom"
)

// WithReportAddress sets the report-address form: the choices (the value of an
// option is what the handler reads), the one selected, the address typed for
// the domain, and the profile e-mail of the viewer that the button beside the
// field fills it with ("" for none). The sentence under the select says what
// each choice offered means.
func (p *OutDomainSettings) WithReportAddress(options []Option, selected, custom, mine string) *OutDomainSettings {
	p.RuaOptions, p.RuaSelected, p.RuaCustom, p.RuaMine = options, selected, custom, mine
	help := Rich("No reports leaves ", Code("rua="), " out of the DMARC record. ")
	for _, o := range options {
		if o.Value == ReportHosted {
			help = append(help, Inline{Text: "SelfPost hosted sends them to an address on this server, which parses them for DMARC reports. "})
		}
	}
	help = append(help, Inline{Text: "A specific address is any mailbox, and it receives the raw XML. Changing this changes the DMARC record to publish."})
	p.RuaHelp = help
	return p
}

// WithExport sets the shortest password the export accepts.
func (p *OutDomainSettings) WithExport(passwordMin int) *OutDomainSettings {
	p.PasswordMin = passwordMin
	return p
}

// WithRateLimit sets the rate-limit form and the tag that says whether the
// limit is in force.
func (p *OutDomainSettings) WithRateLimit(r DomainRateLimit) *OutDomainSettings {
	p.Rate = r
	if r.Active {
		p.Limit.End = Rich(TagOf("ok", "active"))
	} else {
		p.Limit.End = Rich(TagOf("off", "inactive"))
	}
	p.LimitNote = Rich("Applies to every sender on this domain and cannot exceed level 1 (",
		strconv.Itoa(r.L1Messages)+" messages / "+strconv.Itoa(r.L1Window)+" s per client IP, set in ", Code(".env"),
		"). An application may carry its own limit above or below this one.")
	return p
}

// WithResult shows the result of the last action, or its refusal, between the
// head and the boxes.
func (p *OutDomainSettings) WithResult(flash, formErr string) *OutDomainSettings {
	p.Flash = resultFlash(flash, formErr)
	return p
}

// ---- Delete an outbound domain

// OutDomainDelete is the data of the confirmation before a domain is deleted:
// what goes with it, the one button that does it, and the way out.
type OutDomainDelete struct {
	Meta
	Head Head

	Danger       Box
	Intro        Text
	Consequences []Text
	Note         Text
	Action       string
	Confirm      string
	KeepHref     string

	Moving     Box
	MovingText Text
}

// NewOutDomainDelete builds the page. logins are the applications the domain
// has: they are named, because they go with it.
func NewOutDomainDelete(m Meta, id int64, name string, logins []string) *OutDomainDelete {
	m.Title, m.Section, m.Page = "Delete "+name, "outbound", "domains"
	href := DomainHref(id)
	p := &OutDomainDelete{
		Meta:         m,
		Head:         Head{Crumbs: outboundCrumbs(id, name), Title: "Delete " + name},
		Danger:       Box{Icon: "ti-alert-triangle", Title: "This cannot be undone", Variant: BoxDanger},
		Intro:        Rich("Deleting ", Strong(name), " will:"),
		Consequences: []Text{Rich("permanently delete its DKIM signing key;")},
		Note:         Plain("The published DKIM record becomes invalid — remove it from DNS unless you plan to re-add the domain. Inbound domains are not touched."),
		Action:       href + "/delete",
		Confirm:      "Delete " + name,
		KeepHref:     href,
		Moving:       Box{Icon: "ti-file-export", Title: "Moving it instead?"},
		MovingText: Rich(Link(href+"/settings#export", "Export the domain"),
			" first. The file carries the key and the application passwords, so the new instance needs no DNS change."),
	}
	if n := appsPhrase(len(logins)); n != "" {
		item := Rich("delete ", Strong(n), " (")
		for i, l := range logins {
			if i > 0 {
				item = append(item, Inline{Text: ", "})
			}
			item = append(item, Mono(l))
		}
		item = append(item, Inline{Text: ") with their credentials and sender bindings;"})
		p.Consequences = append(p.Consequences, item)
	}
	p.Consequences = append(p.Consequences, Rich("reload OpenDKIM so the domain is no longer signed."))
	return p
}
