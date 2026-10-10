package view

// The data of the Inbound domain pages: the list (templates/in_domains.html,
// mockup in-domains.html), one domain (in_domain.html, in-domain.html) and the
// confirmation before a domain is deleted (in_domain_delete.html,
// in-domain-delete.html).
//
// A page is a typed struct that embeds Meta, built by a constructor and filled
// by With… methods; the handler reads the stores and the DNS checker, the view
// decides what is said and how it is worded.

import (
	"strconv"
	"strings"
)

const inboundDomains = "/inbound/domains"

// InboundDomainHref is the address of an inbound domain's page.
func InboundDomainHref(id int64) string { return inboundDomains + "/" + strconv.FormatInt(id, 10) }

// The values of an inbound domain's recipient mode and TLS mode, as the store
// keeps them.
const (
	recipientModeAny = "any"
	tlsModeEncrypt   = "encrypt"
	tlsModeNone      = "none"
)

// ---- Inbound domains

// InDomains is the data of the list. The global role adds and deletes domains
// (CanManage); a domain administrator sees the list alone, so the add box is
// absent for them and the table is the first box.
type InDomains struct {
	Meta
	Head  Head
	Flash *Flash

	CanManage bool
	Add       Box
	AddHelp   Text
	// AddName is what was typed into the form, shown again when it is refused.
	AddName string

	List   Box
	Rows   []InDomainRow
	Empty  EmptyState
	Legend Foot

	filter *bool
}

// InDomainRow is one line of the Domains table: the verdict of the MX check as
// a tag, where accepted mail is handed on, who is accepted and the TLS the hand
// over needs. DeleteHref is empty when the viewer may not delete.
type InDomainRow struct {
	Name       string
	Href       string
	DeleteHref string
	DNS        Tag
	Upstream   string
	Recipients string
	TLS        Tag
}

// InDomainInput is what the list knows of one inbound domain: the store's
// fields and the status the MX check returned (ok, warn, error or unknown).
// Host is empty while no upstream is saved.
type InDomainInput struct {
	ID             int64
	Name           string
	DNSStatus      string
	Host           string
	Port           int
	TLSMode        string
	RecipientMode  string
	RecipientCount int
	// Addresses is the listed recipients, which only the page of one domain
	// shows.
	Addresses []string
}

// NewInDomainRow words one domain of the list. canDelete is the global role.
func NewInDomainRow(in InDomainInput, canDelete bool) InDomainRow {
	href := InboundDomainHref(in.ID)
	r := InDomainRow{
		Name: in.Name, Href: href,
		DNS:        Tag{Status: in.DNSStatus, Label: "MX"},
		Upstream:   upstreamLabel(in.Host, in.Port),
		Recipients: recipientsLabel(in.RecipientMode, in.RecipientCount),
		TLS:        tlsTag(in.TLSMode),
	}
	if canDelete {
		r.DeleteHref = href + "/delete"
	}
	return r
}

// upstreamLabel is host:port, or a dash while no upstream is saved.
func upstreamLabel(host string, port int) string {
	if host == "" {
		return "—"
	}
	return host + ":" + strconv.Itoa(port)
}

// recipientsLabel says who a domain accepts: "any address" or the number of
// listed ones.
func recipientsLabel(mode string, n int) string {
	if mode == recipientModeAny {
		return "any address"
	}
	return plural(int64(n), "address", "addresses")
}

// tlsTag is the TLS a domain's upstream needs: green when it is required,
// neutral for the two modes that let mail through in the clear.
func tlsTag(mode string) Tag {
	switch mode {
	case tlsModeEncrypt:
		return Tag{Status: "ok", Label: "required"}
	case tlsModeNone:
		return Tag{Status: "off", Label: "off"}
	}
	return Tag{Status: "off", Label: "opportunistic"}
}

// NewInDomains builds the list for a viewer. canManage is the global role.
func NewInDomains(m Meta, canManage bool) *InDomains {
	m.Title, m.Section, m.Page = "Inbound domains", "inbound", "domains"
	p := &InDomains{
		Meta: m,
		Head: Head{
			Kicker: "Inbound",
			Title:  "Inbound domains",
			Lead:   Plain("Backup-MX / forwarder. Port 25 accepts mail only for the domains listed here and hands it to their upstream. No mailboxes."),
		},
		CanManage: canManage,
		List:      Box{No: "01", Title: "Domains", Help: topicLink(HelpInbound, "Backup-MX and forwarding")},
		Empty:     EmptyState{Icon: "ti-world-download", Text: Plain("No inbound domains yet.")},
		Legend:    Foot{Bare: true, Text: Plain("The MX tag is green when at least one MX of the domain points at this server.")},
	}
	if canManage {
		p.Add = Box{No: "01", Title: "Add a domain"}
		p.List.No = "02"
		p.AddHelp = Plain("Mail is not accepted until an upstream host is saved on the domain's page. Adding and deleting domains is for a global administrator; a domain administrator sees the list without this form and without Delete.")
		p.Empty.Text = Plain("No inbound domains yet. Add the first one above, then set its upstream on its page.")
	}
	return p
}

// WithRows sets the table and counts it in the box head.
func (p *InDomains) WithRows(rows []InDomainRow) *InDomains {
	p.Rows = rows
	p.setEnd()
	return p
}

// WithFilter says in the box head whether the instance filters inbound mail.
func (p *InDomains) WithFilter(on bool) *InDomains {
	p.filter = &on
	p.setEnd()
	return p
}

// setEnd writes the box head's end slot: the spam filter's state, when it is
// known, and how many domains the table lists.
func (p *InDomains) setEnd() {
	count := plural(int64(len(p.Rows)), "domain", "domains")
	if p.filter == nil {
		p.List.End = Plain(count)
		return
	}
	p.List.End = Rich("Spam filter ", filterTag(*p.filter), " · "+count)
}

// filterTag is the on / off tag of the spam filter.
func filterTag(on bool) Inline {
	if on {
		return TagOf("ok", "on")
	}
	return TagOf("off", "off")
}

// WithResult shows the result of the last action, or the refusal of the add
// form (with what was typed kept in the field), between the head and the first
// box.
func (p *InDomains) WithResult(flash, formErr, typed string) *InDomains {
	p.AddName = typed
	p.Flash = resultFlash(flash, formErr)
	return p
}

// ---- Delete an inbound domain

// InDomainDelete is the data of the confirmation before an inbound domain is
// deleted: what goes with it, the one button that does it, and the way out.
type InDomainDelete struct {
	Meta
	Head Head

	Danger       Box
	Intro        Text
	Consequences []Text
	Note         Text
	Action       string
	Confirm      string
	KeepHref     string
}

// NewInDomainDelete builds the page. hasUpstream says that an upstream host is
// saved on the domain; listed is how many recipient addresses it lists (0 when
// it accepts any address): both go with the domain, so both are named.
func NewInDomainDelete(m Meta, id int64, name string, hasUpstream bool, listed int) *InDomainDelete {
	m.Title, m.Section, m.Page = "Delete "+name, "inbound", "domains"
	href := InboundDomainHref(id)
	crumbs := []Crumb{{Href: inboundDomains, Label: "Inbound domains"}, {Href: href, Label: name}}
	p := &InDomainDelete{
		Meta:   m,
		Head:   Head{Crumbs: crumbs, Title: "Delete " + name},
		Danger: Box{Icon: "ti-alert-triangle", Title: "This cannot be undone", Variant: BoxDanger},
		Intro:  Rich("Deleting the inbound domain ", Strong(name), " will:"),
		Consequences: []Text{
			Plain("stop accepting mail for it on port 25 — senders get a relay-denied reply;"),
		},
		Note:     Plain("Remove or repoint the MX record first, or mail for the domain will bounce. Outbound domains are not touched."),
		Action:   href + "/delete",
		Confirm:  "Delete " + name,
		KeepHref: href,
	}
	switch {
	case hasUpstream && listed > 0:
		p.Consequences = append(p.Consequences, Plain("remove its upstream and its "+plural(int64(listed), "listed recipient", "listed recipients")+"."))
	case hasUpstream:
		p.Consequences = append(p.Consequences, Plain("remove its upstream."))
	case listed > 0:
		p.Consequences = append(p.Consequences, Plain("remove its "+plural(int64(listed), "listed recipient", "listed recipients")+"."))
	}
	return p
}

// ---- One inbound domain

// InDomain is the data of a domain's page: the MX record to publish and what
// DNS says of it, the upstream and the recipients — each its own form with its
// own POST — and what the spam filter does for the domain. The side menu leads
// to the boxes and, for the global role, to deleting the domain.
type InDomain struct {
	Meta
	Head  Head
	Flash *Flash

	id             int64
	name           string
	dnsStatus      string
	recipientCount int
	recipientMode  string
	filterOn       bool

	DNS    Box
	Record Record

	Upstream       Box
	UpstreamAction string
	Host           string
	Port           string
	TLSOptions     []Option
	TLSSelected    string

	Recipients       Box
	RecipientsAction string
	ModeOptions      []Option
	ModeSelected     string
	Addresses        string
	RecipientsHelp   Text

	Filter      Box
	FilterText  Text
	FilterFacts []Fact
}

// NewInDomain builds the page of a domain. The MX record is filled by WithMX
// and the filter box by WithFilter.
func NewInDomain(m Meta, in InDomainInput) *InDomain {
	m.Title, m.Section, m.Page = in.Name+" · Inbound domains", "inbound", "domains"
	href := InboundDomainHref(in.ID)
	return &InDomain{
		Meta: m,
		Head: Head{
			Crumbs:  []Crumb{{Href: inboundDomains, Label: "Inbound domains"}},
			Title:   in.Name,
			Lead:    inDomainLead(in),
			Actions: []Action{{Label: "Re-check DNS", Icon: "ti-refresh", Post: href + "/dns-recheck"}},
		},
		id: in.ID, name: in.Name, dnsStatus: in.DNSStatus, recipientCount: in.RecipientCount, recipientMode: in.RecipientMode,

		DNS: Box{No: "01", Title: "MX record", ID: "dns", Help: topicLink(HelpInbound, "Backup-MX and forwarding")},

		Upstream:       Box{No: "02", Title: "Upstream", ID: "upstream", End: Plain("Not a mailbox"), Help: topicLink(HelpUpstream, "Host, port and TLS")},
		UpstreamAction: href + "/upstream",
		Host:           in.Host,
		Port:           strconv.Itoa(in.Port),
		TLSOptions: []Option{{Value: "may", Label: "Opportunistic"}, {Value: tlsModeEncrypt, Label: "Required"},
			{Value: tlsModeNone, Label: "Off"}},
		TLSSelected: in.TLSMode,

		Recipients:       Box{No: "03", Title: "Valid recipients", ID: "recipients", Help: topicLink(HelpRecipients, "Listed addresses or any address")},
		RecipientsAction: href + "/recipients",
		ModeOptions: []Option{{Value: "list", Label: "Listed addresses only"},
			{Value: recipientModeAny, Label: "Any recipient at this domain"}},
		ModeSelected:   in.RecipientMode,
		Addresses:      strings.Join(in.Addresses, "\n"),
		RecipientsHelp: Plain("One per line or comma-separated. Unknown recipients are rejected at RCPT, so this relay generates no backscatter. Prefer a list unless the upstream itself rejects unknowns."),

		Filter: Box{No: "04", Title: "Spam filter", ID: "filter", Help: topicLink(HelpFilter, "A server setting")},
	}
}

// inDomainLead is the sentence under the title: what port 25 does with mail
// for the domain and who it accepts.
func inDomainLead(in InDomainInput) Text {
	who := "no recipients listed"
	switch {
	case in.RecipientMode == recipientModeAny:
		who = "any recipient"
	case in.RecipientCount > 0:
		who = plural(int64(in.RecipientCount), "recipient", "recipients")
	}
	if in.Host == "" {
		return Plain("Not accepting mail yet: no upstream is saved · " + who)
	}
	tls := "over opportunistic TLS"
	switch in.TLSMode {
	case tlsModeEncrypt:
		tls = "over required TLS"
	case tlsModeNone:
		tls = "without TLS"
	}
	return Rich("Accepted on port 25, handed to ", Mono(in.Host+":"+strconv.Itoa(in.Port)), " "+tls+" · "+who)
}

// MXRecord is the MX record to publish: it points the domain at this server
// (hostname is SELFPOST_HOSTNAME) and keeps any primary MX of its own.
func MXRecord(domain, hostname string, c DNSCheck) Record {
	r := Record{Name: "MX", Note: Plain("Keep any existing primary MX if this is a backup-MX"),
		Host: domain, Type: "MX", Value: "10 " + strings.TrimSuffix(hostname, ".") + "."}
	applyCheck(&r, c, nil, nil)
	return r
}

// WithMX sets the MX record and, from its check, the stamp under the title.
func (p *InDomain) WithMX(hostname string, c DNSCheck) *InDomain {
	p.Record = MXRecord(p.name, hostname, c)
	p.dnsStatus = c.Status
	level := tagLevel(c.Status)
	pm := &Postmark{Top: "DNS", Word: "OK", Bottom: "MX POINTS HERE", Level: level}
	switch level {
	case LevelFail:
		pm.Word, pm.Bottom = "FAIL", "NO MX HERE"
	case LevelWarn:
		pm.Word, pm.Bottom = "WARN", "NOT CHECKED"
	}
	p.Head.Postmark = pm
	return p
}

// WithFilter fills the Spam filter box from what the server is set up with:
// on says a filter is attached to port 25, milter is its address
// (INBOUND_ANTISPAM_MILTER) and action what Postfix does while it is down
// ("accept", "tempfail" or "" when that is not known). The box says only that;
// the address is the global role's, and so is the name of the variable.
func (p *InDomain) WithFilter(on bool, milter, action string) *InDomain {
	p.filterOn = on
	p.Filter.End = Rich(filterTag(on))
	if !on {
		if p.IsGlobal {
			p.FilterText = Rich("Mail for this domain is passed to the upstream unfiltered; turning the filter on is a server setting (",
				Code("INBOUND_ANTISPAM_MILTER"), ").")
		} else {
			p.FilterText = Plain("Mail for this domain is passed to the upstream unfiltered; turning the filter on is a server setting, set by the server's administrator.")
		}
		return p
	}
	p.FilterFacts = []Fact{{Label: "Applies to", Value: Plain("Every inbound domain of this server")}}
	if p.IsGlobal && milter != "" {
		p.FilterFacts = append(p.FilterFacts, Fact{Label: "Filter", Value: Plain(milter), Mono: true})
	}
	switch action {
	case "accept":
		p.FilterFacts = append(p.FilterFacts, Fact{Label: "If the filter is down", Value: Plain("Mail is accepted unfiltered")})
	case "tempfail":
		p.FilterFacts = append(p.FilterFacts, Fact{Label: "If the filter is down", Value: Plain("Mail is deferred until it answers")})
	}
	return p
}

// WithResult shows the result of the last action, or its refusal, between the
// head and the boxes.
func (p *InDomain) WithResult(flash, formErr string) *InDomain {
	p.Flash = resultFlash(flash, formErr)
	return p
}

// Menu is the side menu: this page's boxes with their state, and the entry that
// deletes the domain for the global role.
func (p *InDomain) Menu() SideMenu {
	mx := Tag{Status: p.dnsStatus, Label: p.dnsStatus}
	if p.dnsStatus == "error" {
		mx = Tag{Status: "fail", Label: "fail"}
	}
	recipients := &Tag{Label: strconv.Itoa(p.recipientCount)}
	if p.recipientMode == recipientModeAny {
		recipients.Label = "any"
	}
	here := MenuGroup{Label: "This domain", Items: []MenuItem{
		{Label: "MX record", Icon: "ti-world-check", Href: "#dns", Active: true, Tag: &mx},
		{Label: "Upstream", Icon: "ti-arrow-forward-up", Href: "#upstream"},
		{Label: "Recipients", Icon: "ti-address-book", Href: "#recipients", Tag: recipients},
		{Label: "Spam filter", Icon: "ti-shield-check", Href: "#filter", Tag: filterTag(p.filterOn).Tag},
	}}
	menu := SideMenu{Groups: []MenuGroup{here}}
	if p.IsGlobal {
		menu.Groups = append(menu.Groups, MenuGroup{Label: "Rarely changed", Items: []MenuItem{
			{Label: "Delete domain", Icon: "ti-trash", Href: InboundDomainHref(p.id) + "/delete", Danger: true},
		}})
	}
	return menu
}
