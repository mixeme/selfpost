package view

// The data of the Help page (templates/help.html, mockup help.html): what each
// screen checks and what to do when it is not green, as topics that stack inside
// two boxes. The head of a numbered box on another page links to its topic with
// the anchor of that topic's ID (Box.Help); nothing is shown in a drawer.
//
// The topics are plain data written here once. A topic is only worded for
// something this build has: Help explains the checks and records the panel
// shows, not the features of a later release.

// Help is the data of the Help page. The Overview box explains the checks of
// Overview and Health, which only the global role sees, so it is absent for a
// domain administrator along with its entries in the menu.
type Help struct {
	Meta
	Head Head

	Menu SideMenu

	Overview       Box
	OverviewTopics []HelpTopic
	// ShowOverview is whether the Overview box is present.
	ShowOverview bool

	Outbound       Box
	OutboundTopics []HelpTopic
}

// Anchors of the Help page. A link to one of these is a Box.Help of another
// page; the page must carry exactly the topics its links point at.
const (
	HelpChecks      = "checks"
	HelpCertificate = "certificate"
	HelpRDNS        = "rdns"
	HelpDNS         = "dns"
	HelpApps        = "apps"
	HelpLimits      = "limits"
)

// NewHelp builds the page for a viewer. global is the role that sees Overview
// and Server, and so the topics about their checks.
func NewHelp(m Meta, global bool) *Help {
	m.Title, m.Section, m.Page = "Help", "user", "help"
	p := &Help{
		Meta: m,
		Head: Head{
			Kicker: "Reference",
			Title:  "Help",
			Lead:   Plain("What each screen checks and what to do when it is not green. The ? in a box's head opens its topic here."),
		},
		ShowOverview: global,
		Overview:     Box{No: "01", Title: "Overview"},
		OverviewTopics: []HelpTopic{
			{ID: HelpChecks, Title: "Health checks", Body: []Text{
				Rich("Six checks, and the worst of them is the postmark. ", TagOf("ok", ""), " needs nothing; ",
					TagOf("warn", ""), " still delivers mail but will stop doing so if ignored; ",
					TagOf("fail", ""), " means mail is being lost or refused now. ", Link("/server/health", "Health"),
					" shows the readings behind them: CPU and memory are the container's own, not the host's spare capacity, and network is a short window, not a daily total."),
			}},
			{ID: HelpCertificate, Title: "TLS certificate", Body: []Text{
				Plain("The certificate Postfix presents on 465 and 587. The reverse proxy or the image mounts it, the panel does not issue it. It warns 14 days before expiry and fails once expired, or when the file is missing or unreadable — clients that verify certificates then refuse to connect."),
			}},
			{ID: HelpRDNS, Title: "Hostname and reverse DNS", Body: []Text{
				Rich("The hostname (", Code("SELFPOST_HOSTNAME"), ") must resolve to this server's address, and that address must resolve back to the hostname. Large receivers reject or spam-folder mail from hosts where the two disagree. The PTR record is set at your hosting provider, not in your DNS zone."),
			}},
		},
		Outbound: Box{No: "02", Title: "Outbound"},
		OutboundTopics: []HelpTopic{
			{ID: HelpDNS, Title: "DNS records", Body: []Text{
				Rich("DKIM proves the message was signed by this relay, SPF lists the addresses allowed to send for the domain, DMARC tells receivers what to do when both fail and where to report. Publish all three exactly as shown; merge SPF into an existing record rather than adding a second one. A domain's status is the worst of its three records, and results are cached for a few minutes, so use Re-check DNS after publishing. The SPF check is shallow: it looks for this server's address in the record itself and does not follow ",
					Code("include:"), " or ", Code("redirect="), ", so a record that relies on them is reported as a warning, not a failure. ",
					Code("p=none"), " does not affect delivery; tighten to ", Code("quarantine"), ", then ", Code("reject"),
					", once the reports look clean. A domain's report address is set in its settings; the default it follows belongs to a user and is set under ",
					Link("/account#dmarc", "Account"), "."),
			}},
			{ID: HelpApps, Title: "Applications", Body: []Text{
				Plain("An application is one SASL login, unique across all domains. It may send as any address of its domain or only as listed addresses, and, with a list of client IPs set, only from those addresses. The password is shown once at creation or regeneration and is not stored in readable form."),
			}},
			{ID: HelpLimits, Title: "Rate limits", Body: []Text{
				Rich("Level 1 is a per-IP ceiling set in ", Code(".env"), ". Level 2 is optional, per domain and per application, entered by hand or computed from the last 30 days of sending, and can never exceed level 1. An application's own limit replaces its domain's."),
			}},
		},
	}
	p.Menu = SideMenu{}
	if global {
		p.Menu.Groups = append(p.Menu.Groups, MenuGroup{Label: "Overview", Items: []MenuItem{
			{Label: "Health checks", Icon: "ti-activity-heartbeat", Href: "#" + HelpChecks},
			{Label: "TLS certificate", Icon: "ti-certificate", Href: "#" + HelpCertificate},
			{Label: "Reverse DNS", Icon: "ti-arrows-exchange", Href: "#" + HelpRDNS},
		}})
	}
	p.Menu.Groups = append(p.Menu.Groups, MenuGroup{Label: "Outbound", Items: []MenuItem{
		{Label: "DNS records", Icon: "ti-world-check", Href: "#" + HelpDNS},
		{Label: "Applications", Icon: "ti-apps", Href: "#" + HelpApps},
		{Label: "Rate limits", Icon: "ti-gauge", Href: "#" + HelpLimits},
	}})
	return p
}
