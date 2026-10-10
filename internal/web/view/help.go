package view

// The data of the Help page (templates/help.html, mockup help.html): what each
// screen checks or does and what to do when it is not green, as topics that
// stack inside one box per section of the panel. The head of a box on another
// page links to its topic with the anchor of that topic's ID (Box.Help); nothing
// is shown in a drawer.
//
// The topics are plain data written here once, one paragraph each. A topic is
// only worded for something this build has: Help explains the checks, records
// and settings the panel shows, not the features of a later release. Text goes
// through Plain, Rich and Code, never raw HTML.

import "fmt"

// Help is the data of the Help page: the menu, and one group of topics per
// section of the panel the viewer can open. A domain administrator is not shown
// Overview and Server (those pages are 404 for them), nor Inbound without an
// inbound reach or with the feature off, nor Outbound without an outbound reach;
// the menu lists exactly the topics that are shown.
type Help struct {
	Meta
	Head Head

	Menu   SideMenu
	Groups []HelpGroup
}

// HelpGroup is one box of the Help page: the section of the panel it explains,
// its topics, and the menu entries that jump to them.
type HelpGroup struct {
	Box    Box
	Topics []HelpTopic
}

// Anchors of the Help page. A link to one of these is a Box.Help of another
// page; the page must carry exactly the topics its links point at.
const (
	// Overview
	HelpChecks      = "checks"
	HelpCertificate = "certificate"
	HelpRDNS        = "rdns"
	// Outbound
	HelpDNS    = "dns"
	HelpApps   = "apps"
	HelpLimits = "limits"
	HelpLog    = "log"
	HelpQueue  = "queue"
	HelpDMARC  = "dmarc"
	// Inbound
	HelpInbound    = "inbound"
	HelpUpstream   = "upstream"
	HelpRecipients = "recipients"
	HelpFilter     = "filter"
	// Server
	HelpBackup   = "backup"
	HelpUsers    = "users"
	HelpSettings = "settings"
	// Account
	HelpAccount = "account"
)

// topicLink is a box's link to a topic of the Help page.
func topicLink(topic, title string) *HelpLink {
	return &HelpLink{Href: "/help#" + topic, Title: title}
}

// reachesOutbound is whether the viewer has anything in Outbound: the global
// role, or a domain administrator with an outbound domain.
func (m Meta) reachesOutbound() bool { return m.IsGlobal || m.HasOutbound }

// NewHelp builds the page for a viewer. inbound is whether the inbound feature is
// on; with the viewer's own reach it decides, as the shell does for the menu,
// whether Inbound is shown.
func NewHelp(m Meta, inbound bool) *Help {
	m.Title, m.Section, m.Page = "Help", "user", "help"
	p := &Help{
		Meta: m,
		Head: Head{
			Kicker: "Reference",
			Title:  "Help",
			Lead:   Plain("What each screen checks or does, and what to do when it is not green. The ? in a box's head opens its topic here."),
		},
	}
	add := func(title string, topics []HelpTopic, menu []MenuItem) {
		p.Groups = append(p.Groups, HelpGroup{Box: Box{Title: title}, Topics: topics})
		p.Menu.Groups = append(p.Menu.Groups, MenuGroup{Label: title, Items: menu})
	}
	if m.IsGlobal {
		add("Overview", overviewTopics, []MenuItem{
			{Label: "Health checks", Icon: "ti-activity-heartbeat", Href: "#" + HelpChecks},
			{Label: "TLS certificate", Icon: "ti-certificate", Href: "#" + HelpCertificate},
			{Label: "Reverse DNS", Icon: "ti-arrows-exchange", Href: "#" + HelpRDNS},
		})
	}
	if m.reachesOutbound() {
		add("Outbound", outboundTopics, []MenuItem{
			{Label: "DNS records", Icon: "ti-world-check", Href: "#" + HelpDNS},
			{Label: "Applications", Icon: "ti-apps", Href: "#" + HelpApps},
			{Label: "Rate limits", Icon: "ti-gauge", Href: "#" + HelpLimits},
			{Label: "Outbound log", Icon: "ti-list-details", Href: "#" + HelpLog},
			{Label: "Queue and retries", Icon: "ti-stack-2", Href: "#" + HelpQueue},
			{Label: "DMARC reports", Icon: "ti-report-analytics", Href: "#" + HelpDMARC},
		})
	}
	if inbound && (m.IsGlobal || m.HasInbound) {
		add("Inbound", inboundTopics, []MenuItem{
			{Label: "Backup-MX and forwarding", Icon: "ti-inbox", Href: "#" + HelpInbound},
			{Label: "Upstream", Icon: "ti-arrow-forward-up", Href: "#" + HelpUpstream},
			{Label: "Valid recipients", Icon: "ti-address-book", Href: "#" + HelpRecipients},
			{Label: "Spam filter", Icon: "ti-shield-check", Href: "#" + HelpFilter},
		})
	}
	if m.IsGlobal {
		add("Server", serverTopics, []MenuItem{
			{Label: "Backup and moving a domain", Icon: "ti-archive", Href: "#" + HelpBackup},
			{Label: "Users and roles", Icon: "ti-users", Href: "#" + HelpUsers},
			{Label: "Settings", Icon: "ti-settings", Href: "#" + HelpSettings},
		})
	}
	add("Account", accountTopics, []MenuItem{
		{Label: "Your account", Icon: "ti-user-circle", Href: "#" + HelpAccount},
	})
	for i := range p.Groups {
		p.Groups[i].Box.No = fmt.Sprintf("%02d", i+1)
	}
	return p
}

var overviewTopics = []HelpTopic{
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
}

var outboundTopics = []HelpTopic{
	{ID: HelpDNS, Title: "DNS records", Body: []Text{
		Rich("DKIM proves the message was signed by this relay, SPF lists the addresses allowed to send for the domain, DMARC tells receivers what to do when both fail and where to report. Publish all three exactly as shown; merge SPF into an existing record rather than adding a second one. A domain's status is the worst of its three records, and results are cached for a few minutes, so use Re-check DNS after publishing. The SPF check is shallow: it looks for this server's address in the record itself and does not follow ",
			Code("include:"), " or ", Code("redirect="), ", so a record that relies on them is reported as a warning, not a failure. ",
			Code("p=none"), " does not affect delivery; tighten to ", Code("quarantine"), ", then ", Code("reject"),
			", once the reports look clean. A domain's report address is set in its Domain settings."),
	}},
	{ID: HelpApps, Title: "Applications", Body: []Text{
		Plain("An application is one SASL login, unique across all domains. It may send as any address of its domain or only as listed addresses, and, with a list of client IPs set, only from those addresses. The password is shown once at creation or regeneration and is not stored in readable form."),
	}},
	{ID: HelpLimits, Title: "Rate limits", Body: []Text{
		Rich("Level 1 is a per-IP ceiling set in ", Code(".env"), ". Level 2 is optional, per domain and per application, entered by hand or computed from the last 30 days of sending, and can never exceed level 1. An application's own limit replaces its domain's."),
	}},
	{ID: HelpLog, Title: "Outbound log", Body: []Text{
		Rich("One row per message and recipient, with the status Postfix last reported: queued (accepted, nothing reported yet), sent (the receiving server took it; what the mailbox does next is not reported back), deferred (not taken yet, Postfix tries again), bounced (failed for good, or the queue lifetime ran out) and rejected (refused before queueing under a level-2 rate limit, so it never reached Postfix). A message's page lists these steps and the lines Postfix wrote about it in ",
			Code("mail.log"), ", found by queue id; that file is rotated daily and about two weeks are kept, so an old message may show none. Rows are deleted after the retention set in Server › Settings, 90 days by default. A domain administrator sees only the rows of their own domains."),
	}},
	{ID: HelpQueue, Title: "Queue and retries", Body: []Text{
		Rich("The page lists, as ", Code("postqueue -p"), " prints it, the messages Postfix has accepted and not yet delivered: mail being sent right now and mail deferred for a later attempt. Only the global role can open it, and it only shows; nothing here deletes or forces a message. A deferred message is tried again after the first retry delay, then at gaps that double up to a cap, until the receiving server takes it or the queue lifetime runs out and it bounces; there is no attempt counter, and the box on the page shows this server's values. A queue that keeps growing means a receiver is refusing or not answering: open the message from the Outbound log to read the reason Postfix logged."),
	}},
	{ID: HelpDMARC, Title: "DMARC reports", Body: []Text{
		Rich("An aggregate report is the summary a receiving provider sends, usually daily, to the ", Code("rua="),
			" address of your DMARC record: which servers sent mail as your domain and whether SPF or DKIM passed for it. A domain's report address is one of three choices, set in its Domain settings: no reports (the default for a new domain), the SelfPost hosted address (an address on this server whose reports are parsed and shown under DMARC reports; offered only while hosted reports are enabled on the server), or an address typed for the domain (an ordinary mailbox that receives the raw XML; a button fills in your own profile e-mail). When the typed address is on another domain than the one sending, that domain must publish a report-authorization record, or providers will not send; the domain's page shows it with the other DNS records and checks it. In the tables pass means SPF or DKIM passed and aligned, fail means neither did, and your own server shows as ",
			Em("this relay"), ". A fail from any other source is someone else sending as your domain — a forwarder, an application you forgot, or abuse — so do not tighten ",
			Code("p="), " until that sender is fixed or removed. A domain administrator reads the reports of their own domains."),
	}},
}

var inboundTopics = []HelpTopic{
	{ID: HelpInbound, Title: "Backup-MX and forwarding", Body: []Text{
		Rich("An optional backup-MX or forwarder, switched on in the server's ", Code(".env"), "; the Inbound menu exists only then. Postfix accepts mail on port 25 for the listed domains only and hands each message on to that domain's upstream; there are no mailboxes. For each domain publish the MX record its page shows, and keep a primary MX the domain already has if this is a backup. The MX tag is green when at least one MX of the domain points at this server, and a domain accepts no mail until an upstream is saved on its page. Adding and deleting domains is for the global role."),
	}},
	{ID: HelpUpstream, Title: "Upstream", Body: []Text{
		Plain("The upstream is the server that actually holds the mailboxes. Give its host and port — used exactly as written, with no MX lookup — and choose the TLS for the hop to it: Opportunistic uses TLS when the upstream offers it and otherwise sends in the clear, Required holds the mail until a TLS session is possible, Off never uses TLS. While the upstream does not answer, Postfix keeps the mail and retries as for any other mail."),
	}},
	{ID: HelpRecipients, Title: "Valid recipients", Body: []Text{
		Plain("Choose whom this domain accepts. Listed addresses only: the relay accepts exactly the addresses you list, one per line or comma-separated, and refuses any other while the sender is still connected, so it never has to send a bounce for them. Any recipient: everything at the domain is accepted and passed on, and an address the upstream then rejects comes back as a bounce from this server to a sender who is often forged — backscatter. Prefer a list, and keep it in step with the mailboxes upstream."),
	}},
	{ID: HelpFilter, Title: "Spam filter", Body: []Text{
		Rich("The spam filter belongs to the server, not to a domain. The server's administrator attaches it in the server's ", Code(".env"),
			", and from then on every inbound domain is screened by it before mail is handed on; the box shows on or off and has nothing to switch. Off means mail goes to the upstream unfiltered. While the filter is down, mail is either accepted unfiltered (the default) or deferred, so the sending server tries again later, as the server's setting says; the box states which."),
	}},
}

var serverTopics = []HelpTopic{
	{ID: HelpBackup, Title: "Backup and moving a domain", Body: []Text{
		Rich("Full backup downloads one archive of the whole instance: ", Code("data/"), " (the database, DKIM keys, application credentials and the queue), ",
			Code("docker-compose.yml"), ", ", Code(".env"), " and ", Code("certs/"),
			". It holds secrets, so leave Encrypt with a password ticked: the file is sealed as ", Code(".spbk"),
			", the password is not stored, and without it nothing can be recovered. To restore, unpack it into an empty project directory (decrypt a ",
			Code(".spbk"), " first with ", Code("selfpost-backup -decrypt"),
			") and start the same SelfPost version that made it; the panel refuses data from another version. The reverse-proxy configuration and ",
			Code("mail.log"), " are not included. One domain moves on its own: Export domain on its settings page writes a ", Code(".json"),
			" (or encrypted ", Code(".spde"), ") with its DKIM key and its applications' passwords, and Import a domain here reads it into another instance, so the published DNS needs no change."),
	}},
	{ID: HelpUsers, Title: "Users and roles", Body: []Text{
		Rich("The two roles are a reach, not a rank: every user is an administrator and signs in with a username and password. Global reaches the whole server — Overview, Server, the queue and every domain — and is the only role that adds or deletes domains and users. Domain reaches only the domains assigned to it, outbound and inbound separately (the same name on both lists is two assignments); ",
			Em("All"), " includes domains added later, and a domain user needs at least one domain or ", Em("All"),
			" on either list. The only global user cannot be demoted or deleted, and you cannot delete the account you are signed in with. A new password set here signs that user out of their other sessions."),
	}},
	{ID: HelpSettings, Title: "Settings", Body: []Text{
		Rich("Settings holds what is true of the whole instance. Log retention is how many days the Outbound log keeps its rows, from 7 to 365 and 90 by default; a sweep every six hours deletes older ones, and it is the main driver of how fast ",
			Code("/data"), " grows. It does not change the rotation of ", Code("mail.log"),
			", which is daily with about two weeks kept. The sending rate limit shown under it is level 1, the per-client-IP ceiling set in ",
			Code(".env"), ": changing it needs a restart, and no level-2 limit can exceed it."),
	}},
}

var accountTopics = []HelpTopic{
	{ID: HelpAccount, Title: "Your account", Body: []Text{
		Plain("Account is your own, whatever your role: your username and e-mail, and your password. Changing the password signs out every other session of this account and keeps the one you are using; applications have their own logins and are not affected."),
	}},
}
