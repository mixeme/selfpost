package view

// The data of the application pages: the form that adds or edits an application
// (templates/out_app.html, mockup out-app.html) and the page that shows its
// password once (out_app_created.html, mockup out-app-created.html).
//
// The form is one POST: who the application may send as, which client IPs may
// use it and its rate limit are checked and saved together or not at all.

import (
	"strconv"
	"strings"
)

// Address modes of an application, the values of the sender select
// (store.AddressMode*).
const (
	AddressWildcard = "wildcard"
	AddressList     = "list"
)

// Rate-limit choices of an application, the values of the limit select.
const (
	LimitDomain = "domain"
	LimitManual = "manual"
	LimitAuto   = "auto"
)

// OutApp is the data of the application form. It adds an application when
// Login is not fixed (the empty form) and edits one when it is. The form fields
// hold what was stored, or what was just submitted when the form is shown again
// with a refusal.
type OutApp struct {
	Meta
	Head  Head
	Flash *Flash

	// Action is where the form is posted; Submit is the main button's label.
	// Cancel leads back to the domain. Recalc, set only while the limit is
	// automatic, is the POST address of its "Recalculate" button.
	Action string
	Submit string
	Cancel string
	Recalc string

	Sender      Box
	Login       string
	LoginFixed  bool
	LoginHelp   Text
	Modes       []Option
	Mode        string
	Addresses   string
	Placeholder string

	IPs        Box
	IPRestrict bool
	AllowedIPs string

	Limit       Box
	LimitModes  []Option
	LimitMode   string
	MaxMessages string
	Window      string
	Multiplier  string
	Rate        AppRateLimit

	recalc string
}

// AppRateLimit is the context of an application's rate-limit fields: the
// ceiling nothing may exceed (level 1), the range of the multiplier, what the
// domain's own limit is, and what the automatic limit has computed.
type AppRateLimit struct {
	L1Messages        int
	L1Window          int
	MinMultiplier     string
	MaxMultiplier     string
	DefaultMultiplier string
	// DomainLimit is the limit the application falls back to ("288 / h"), ""
	// when the domain has none of its own.
	DomainLimit string
	// Computed, Peak and Updated are the automatic limit as DomainRateLimit has
	// them; Computed is "" for an application that has none yet.
	Computed string
	Peak     int64
	Updated  string
}

// AutoHelp is the line under the multiplier: what the limit computes to now.
func (r AppRateLimit) AutoHelp() Text {
	return autoLimitHelp(r.Computed, r.L1Window, r.Peak, r.Updated)
}

// OutAppState is what is stored about the application, which the tags in the
// box heads and the Recalculate button report (the fields may hold something
// that was not saved): whether its client IP allow-list is on, whether it has a
// rate limit of its own, and whether that limit is automatic.
type OutAppState struct {
	IPs   bool
	Limit bool
	Auto  bool
}

// OutAppForm is the values of the application form.
type OutAppForm struct {
	Login       string
	Mode        string
	Addresses   string
	IPRestrict  bool
	AllowedIPs  string
	LimitMode   string
	MaxMessages string
	Window      string
	Multiplier  string
}

// NewOutApp builds the form of a domain's application. login is the stored
// application's login, or "" for the form that adds one.
func NewOutApp(m Meta, domainID int64, domain, login string) *OutApp {
	href := DomainHref(domainID)
	crumbs := append(outboundCrumbs(domainID, domain), Crumb{Label: "Applications"})
	m.Section, m.Page = "outbound", "domains"
	p := &OutApp{
		Cancel: href,
		Sender: Box{No: "01", Title: "Sender"},
		IPs:    Box{No: "02", Title: "Client IP allow-list", Help: topicLink(HelpApps, "What an application is")},
		Limit:  Box{No: "03", Title: "Rate limit", Help: topicLink(HelpLimits, "Levels 1 and 2")},
		Modes: []Option{
			{Value: AddressWildcard, Label: "Any address of the domain"},
			{Value: AddressList, Label: "Specific addresses (list)"},
		},
		LimitModes: []Option{
			{Value: LimitDomain, Label: "Use the domain limit"},
			{Value: LimitManual, Label: "Manual"},
			{Value: LimitAuto, Label: "Auto (from statistics)"},
		},
		Placeholder: "alerts@" + domain,
	}
	if login == "" {
		m.Title = "New application · " + domain
		p.Head = Head{Crumbs: crumbs, Title: "New application",
			Lead: Plain("A password is generated when it is created and shown once.")}
		p.Action = href + "/applications/new"
		p.Submit = "Create application"
		p.LoginHelp = Rich("Unique across domains; letters, digits, ", Code("."), " ", Code("-"), " ", Code("_"), ".")
	} else {
		m.Title = login + " · " + domain
		p.Head = Head{Crumbs: crumbs, Title: login, Mono: true}
		p.Submit = "Save application"
		p.LoginFixed = true
		p.LoginHelp = Plain("Fixed: add a new application to use another login.")
	}
	p.Meta = m
	return p
}

// WithApplication points an edit form at its application: the POST address, the
// head's buttons (a new password and delete, each asking first) and the mail of
// the statistics window under the title.
func (p *OutApp) WithApplication(domainID, id int64, total, peak int64, avg string, days int) *OutApp {
	own := DomainHref(domainID) + "/applications/" + strconv.FormatInt(id, 10)
	p.Action = own
	p.Head.Lead = StatsLead(total, peak, avg, days)
	p.Head.Actions = []Action{
		{Label: "New password", Icon: "ti-key", Post: own + "/password",
			Confirm: "Generate a new password for " + p.Head.Title + "? The current password stops working immediately."},
		{Label: "Delete", Icon: "ti-trash", Post: own + "/delete", Danger: true,
			Confirm: "Delete application " + p.Head.Title + "? Its credentials stop working immediately."},
	}
	p.recalc = own + "/ratelimit/recalc"
	return p
}

// WithForm fills the fields; state is what is stored.
func (p *OutApp) WithForm(f OutAppForm, state OutAppState, rate AppRateLimit) *OutApp {
	p.Login, p.Mode, p.Addresses = f.Login, f.Mode, f.Addresses
	p.IPRestrict, p.AllowedIPs = f.IPRestrict, f.AllowedIPs
	p.LimitMode, p.MaxMessages, p.Window, p.Multiplier = f.LimitMode, f.MaxMessages, f.Window, f.Multiplier
	p.Rate = rate

	if state.IPs {
		p.IPs.End = Rich(TagOf("ok", "active"))
	} else {
		p.IPs.End = Rich(TagOf("off", "off"))
	}
	if state.Limit {
		p.Limit.End = Rich(TagOf("ok", "active"))
	} else {
		p.Limit.End = Rich(TagOf("off", "inactive"))
	}
	if state.Auto && p.recalc != "" {
		p.Recalc = p.recalc
	}
	return p
}

// LimitHelp is the line under the rate-limit fields: what the limit overrides.
func (p *OutApp) LimitHelp() Text {
	fall := "the domain limit, if it has one, or level 1"
	if p.Rate.DomainLimit != "" {
		fall = "the domain limit (" + p.Rate.DomainLimit + ")"
	}
	return Rich("Overrides ", fall, " for this application; may be higher or lower, never above level 1.")
}

// WithResult shows the refusal of the form, or the result of an action, between
// the head and the form.
func (p *OutApp) WithResult(flash, formErr string) *OutApp {
	p.Flash = resultFlash(flash, formErr)
	return p
}

// ---- The password, shown once

// OutAppCreated is the page that follows creating an application or issuing it
// a new password: the login and the password, which are not stored and are not
// shown again, and how to connect with them. It is the response to a POST and
// has no address of its own.
type OutAppCreated struct {
	Meta
	Head Head

	Credential Box
	Login      CopyField
	Password   CopyField
	Done       Foot

	Connect Box
	Facts   []Fact
}

// NewOutAppCreated builds the page. fresh says the application was created just
// now (it had no password before). host is the server's name, submission whether
// port 587 is on, and senders what the login may send as.
func NewOutAppCreated(m Meta, domainID int64, domain, login, password string, fresh bool, host string, submission bool, senders []string) *OutAppCreated {
	m.Title, m.Section, m.Page = "New password · "+login, "outbound", "domains"
	help := Plain("The previous password stopped working when this one was generated. If this one is lost, generate another.")
	if fresh {
		help = Plain("If this one is lost, generate another from the application's page.")
	}
	if host == "" {
		host = "(SELFPOST_HOSTNAME is not set)"
	}
	port := "465 SSL/TLS"
	if submission {
		port += " · 587 STARTTLS"
	}
	return &OutAppCreated{
		Meta: m,
		Head: Head{Crumbs: append(outboundCrumbs(domainID, domain), Crumb{Label: "Applications"}),
			Title: "New password for", Code: login},
		Credential: Box{Icon: "ti-eye", Title: "Shown once", End: Plain("Not stored — copy it now"), Variant: BoxCredential},
		Login:      CopyField{Label: "Login", Value: login},
		Password:   CopyField{Label: "Password", Value: password, Help: help},
		Done:       Foot{Button: &Action{Label: "Done — back to " + domain, Href: DomainHref(domainID), Primary: true}},
		Connect:    Box{Icon: "ti-plug", Title: "Connect with"},
		Facts: []Fact{
			{Label: "Server", Value: Plain(host), Mono: true},
			{Label: "Port", Value: Plain(port)},
			{Label: "May send as", Value: Plain(strings.Join(senders, ", ")), Mono: true},
		},
	}
}
