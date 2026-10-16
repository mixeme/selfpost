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
	HelpDNS  = "dns"
	HelpApps = "apps"
	// HelpSenderCheck is what an application may write in From: and what the
	// strict check costs; Health and a refused message's page link to it.
	HelpSenderCheck = "sender-check"
	HelpLimits      = "limits"
	HelpLog         = "log"
	HelpQueue       = "queue"
	HelpDMARC       = "dmarc"
	// Inbound
	HelpInbound    = "inbound"
	HelpUpstream   = "upstream"
	HelpRecipients = "recipients"
	HelpFilter     = "filter"
	// HelpInboundLog and HelpFilterLists: the plain "log" is the Outbound log's.
	HelpInboundLog  = "inbound-log"
	HelpFilterLists = "filter-lists"
	// Server
	HelpBackup   = "backup"
	HelpUsers    = "users"
	HelpSettings = "settings"
	// HelpNotifications is what the panel writes to its users and what makes
	// that mail arrive; Account, Settings and Health link to it.
	HelpNotifications = "notifications"
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
			{Label: "Sender check", Icon: "ti-shield-check", Href: "#" + HelpSenderCheck},
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
			{Label: "Inbound log", Icon: "ti-list-details", Href: "#" + HelpInboundLog},
			{Label: "Filter lists", Icon: "ti-filter", Href: "#" + HelpFilterLists},
		})
	}
	if m.IsGlobal {
		add("Server", serverTopics, []MenuItem{
			{Label: "Backup and moving a domain", Icon: "ti-archive", Href: "#" + HelpBackup},
			{Label: "Users and roles", Icon: "ti-users", Href: "#" + HelpUsers},
			{Label: "Settings", Icon: "ti-settings", Href: "#" + HelpSettings},
			{Label: "Notifications", Icon: "ti-mail", Href: "#" + HelpNotifications},
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
			" shows the readings behind them: CPU and memory are the container's own, not the host's spare capacity, and network is a short window, not a daily total. ",
			Em("Your address"), ", in the Configuration card, is the address the panel sees your browser at: behind a reverse proxy it must be your own, taken from ", Code("X-Forwarded-For"),
			"; when it is the proxy's, every visitor shares one sign-in limit, and Health warns and names the address to put in ", Code("TRUSTED_PROXY_CIDR"), "."),
	}},
	{ID: HelpCertificate, Title: "TLS certificate", Body: []Text{
		Rich("The certificate Postfix presents on 465 and 587. The reverse proxy or the image mounts it, the panel does not issue it. Postfix serves its own copy of the mounted file and looks for a renewed one once a day by default; until it takes one up, the card shows it as a renewal waiting, and warns once it has waited longer than that check allows - the container log's cert-reload lines say why. It warns 14 days before expiry unless a renewal is already waiting, and fails once expired, or when the file is missing or unreadable — clients that verify certificates then refuse to connect. It also fails when none of its names is the hostname (",
			Code("SELFPOST_HOSTNAME"), ") - the tag beside ", Em("Names"), " says which: a client that verifies the name refuses it however valid it is."),
	}},
	{ID: HelpRDNS, Title: "Hostname and reverse DNS", Body: []Text{
		Rich("The hostname (", Code("SELFPOST_HOSTNAME"), ") must resolve to this server's address, and that address must resolve back to the hostname. Large receivers reject or spam-folder mail from hosts where the two disagree. The PTR record is set at your hosting provider, not in your DNS zone. ",
			Em("Greeting"), " is the line Postfix opens every connection with, ", Code("220"), " and its banner; it must name the hostname too - a receiver compares it with the name it looked up - and a container id or ",
			Code("localhost"), " there is a warning."),
	}},
}

var outboundTopics = []HelpTopic{
	{ID: HelpDNS, Title: "DNS records", Body: []Text{
		Rich("DKIM proves the message was signed by this relay, SPF lists the addresses allowed to send for the domain, DMARC tells receivers what to do when both fail and where to report. Publish all three exactly as shown; merge SPF into an existing record rather than adding a second one. A domain's status is the worst of its three records, and results are cached for a few minutes, so use Re-check DNS after publishing. A record DNS already holds as shown is folded to its name and status; Show values opens it to copy them again. The SPF check is shallow: it looks for this server's address in the record itself and does not follow ",
			Code("include:"), " or ", Code("redirect="), ", so a record that relies on them is reported as a warning, not a failure. ",
			Code("p=none"), " does not affect delivery; tighten to ", Code("quarantine"), ", then ", Code("reject"),
			", once the reports look clean. A domain's report address is set in its Domain settings."),
	}},
	{ID: HelpApps, Title: "Applications", Body: []Text{
		Plain("An application is one SASL login, unique across all domains. It may send as any address of its domain or only as listed addresses, and, with a list of client IPs set, only from those addresses. The password is shown once at creation or regeneration and is not stored in readable form."),
	}},
	{ID: HelpSenderCheck, Title: "Sender check", Body: []Text{
		Rich("An application sends only as itself, and that holds for every sender a message names. The envelope sender is held against the login by Postfix. The ",
			Code("From:"), " line, the one a mail program shows and the one DMARC holds the DKIM signature to, is held against it by the panel, under the same rule: an application set to any address of its domain may write any address of that domain, and one with a list of addresses only those. A message with any other ",
			Code("From:"), " - another of your domains, the server's own name, an address somewhere else, or none at all - is refused for good with ",
			Code("550 5.7.1"), ". The line must hold one address, read as a mail program reads it: what a display name around it says is not judged, and a line holding more than one address is refused. A ", Code("Sender:"), " line, which a mail program shows beside ", Code("From:"), " as \"on behalf of\", is held to the same rule when the message has one. A system that writes for people outside your domains writes its own address in ", Code("From:"), " with the person's name, and the person's address in ", Code("Reply-To:"), ": their domain in ", Code("From:"), " would fail DMARC at the receiver anyway. It is a rejected row of the Outbound log, and its page says which address was refused. This is what keeps one application from sending, under this server's signature, as another domain or another application.",
			Br(), Br(),
			"The check is strict. While the panel does not answer, Postfix does not accept mail unchecked: it replies with a temporary error and the application sends again later. Server › Health counts what that costs: for how long the panel did not answer while Postfix itself was running; how many connections Postfix turned away because of it - it refuses from the connection on, before a message is offered, so a client that tries three times is three and a monitor that only connects is one; and how many messages were refused with a temporary error in the middle, when the panel stopped answering after a message had begun. A time marked ≈ includes a start at which supervisord could not say since when Postfix was running: the time since the panel's last sign of life is counted, which may be more than Postfix was up. The card warns when a connection was turned away or a message refused in the last 24 hours. If the numbers stay at nothing, the check is where it should be; if they do not, it belongs in a process of its own."),
	}},
	{ID: HelpLimits, Title: "Rate limits", Body: []Text{
		Rich("Level 1 is a per-IP ceiling set in ", Code(".env"), ". Level 2 is optional, per domain and per application, entered by hand or computed from the last 30 days of sending, and can never exceed level 1. An application's own limit replaces its domain's."),
	}},
	{ID: HelpLog, Title: "Outbound log", Body: []Text{
		Rich("One row per message and recipient, with the status Postfix last reported: queued (accepted, nothing reported yet), sent (the receiving server took it; what the mailbox does next is not reported back), deferred (not taken yet, Postfix tries again), bounced (failed for good, or the queue lifetime ran out; or closed by the panel when Postfix no longer held the message and ", Code("mail.log"), " never said what became of it) and rejected (refused before queueing, so it never reached the queue: under a level-2 rate limit, from a client address the application does not allow, or for a ", Code("From:"), " or ", Code("Sender:"), " line that is not the application's - the message's page says which). A message's page lists these steps and the lines Postfix wrote about it in ",
			Code("mail.log"), ", found by queue id - and for a message refused before it had one, Postfix's one line about the refusal. The lines are kept with the row: they stay on the page after that file has rotated, for as long as the row does. A message accepted before version 2.2.0 shows them only while they are still in the current file. Rows are deleted after the retention set in Server › Settings, 90 days by default, and their lines with them. A domain administrator sees only the rows of their own domains."),
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
		Plain("The upstream is the server that actually holds the mailboxes. Give its host and port — used exactly as written, with no MX lookup — and choose the TLS for the hop to it: Opportunistic uses TLS when the upstream offers it and otherwise sends in the clear; Required sends only over TLS, and the mail waits in the queue while the upstream does not offer it or the TLS handshake fails - an upstream that answers STARTTLS with a permanent error bounces it; Off never uses TLS. While the upstream does not answer, Postfix keeps the mail and retries as for any other mail."),
	}},
	{ID: HelpRecipients, Title: "Valid recipients", Body: []Text{
		Plain("Choose whom this domain accepts. Listed addresses only: the relay accepts exactly the addresses you list, one per line or comma-separated, and refuses any other while the sender is still connected, so it never has to send a bounce for them. Any recipient: everything at the domain is accepted and passed on, and an address the upstream then rejects comes back as a bounce from this server to a sender who is often forged — backscatter. Prefer a list, and keep it in step with the mailboxes upstream."),
	}},
	{ID: HelpFilter, Title: "Spam filter", Body: []Text{
		Rich("The spam filter belongs to the server, not to a domain. The server's administrator attaches it in the server's ", Code(".env"),
			", and from then on every inbound domain is screened by it before mail is handed on; the box shows on or off and has nothing to switch. Off means mail goes to the upstream unfiltered. While the filter is down, mail is either accepted unfiltered (the default) or refused with a temporary error, so the sending server tries again later, as the server's setting says; the box states which."),
	}},
	{ID: HelpInboundLog, Title: "Inbound log", Body: []Text{
		Rich("One row per recipient of a message offered on port 25 for an inbound domain, with what was decided: ",
			TagOf("accept", ""), " — taken for the upstream; ", TagOf("reject", ""), " — refused for good; ",
			TagOf("tempfail", ""), " — the sender was asked to come back later; ", TagOf("quarantine", ""), " — held by the filter. ",
			Em("Why"), " names who decided. ", Code("rspamd"), " is the spam filter, with its action, the score against the reject threshold and the heaviest symbols; ",
			Code("postfix"), " is the relay's own rule — an unknown recipient, a sender domain that does not exist — with its reply; ",
			Code("selfpost"), " means nobody judged the message: it was relayed with no filter attached. ",
			Em("Upstream"), " says what became of an accepted message: ", TagOf("queued", ""), " — not tried yet; ",
			TagOf("held", ""), " — held in the queue by the filter, not tried until it is released; ",
			TagOf("sent", ""), " — the upstream took it; ", TagOf("deferred", ""),
			" — the upstream put it off or could not be reached, and Postfix tries again; ", TagOf("bounced", ""),
			" — the upstream refused it, or it waited in the queue too long, and the sender was sent a bounce, if it had a return address. It opens the last reply and the host that gave it. A refused message has no upstream result, and neither has a row logged before 2.10.0.",
			Br(), Br(),
			"Accepted mail is recorded as it passes; refused mail is read from ", Code("mail.log"),
			", so its row has no subject, and a message refused after its text was sent is one row however many recipients it had. A refusal to relay for a domain that is not an inbound one is every scanner on the internet and is not recorded. Rows are kept for the number of days set in Settings, and never more than 10 000."),
	}},
	{ID: HelpFilterLists, Title: "Filter lists", Body: []Text{
		Rich("Two lists for the whole server that correct the spam filter by hand. An entry is a sender address, a sender domain — that domain, not its subdomains — or a client address or network in CIDR form. A sender on the Deny list is refused outright. A sender on the Allow list is given a weight no spam score outweighs, so the filter lets it through. The sender's address is whatever the sending server says it is - anyone can claim to be a sender you allow - so allow a client address or network where you can, and a sender address only for mail you would rather receive forged than lose. When entries of both lists match one message, ",
			Strong("Deny wins"), ".",
			Br(), Br(),
			"SelfPost does not apply the lists itself: it writes them as files under ", Code("/data/antispam/"),
			", and rspamd rereads a file within seconds of a change. With the spam filter off the lists are kept and can be edited, but nothing reads them. The sender matched is the envelope sender — the one in the log — not the From line a mail program shows. Matched counts the messages in the Inbound log that the entry decided, so it reaches back as far as the log does; never means not once in that time, and such an entry is one to consider removing."),
	}},
}

var serverTopics = []HelpTopic{
	{ID: HelpBackup, Title: "Backup and moving a domain", Body: []Text{
		Rich("Full backup downloads one archive of the whole instance: ", Code("data/"), " (the database, DKIM keys and application credentials), ",
			Code("docker-compose.yml"), ", ", Code(".env"), " and ", Code("certs/"),
			". Both it and a domain's export ask for your account password - the one you sign in with - at every download, so a session left open is not enough to take them; wrong guesses count against the same limit as a password change."+
				" The Postfix queue is not in it. The backup is taken while the server is running, and Postfix keeps moving messages through the queue, "+
				"so a copy could hold a message twice or miss it; mail still waiting for delivery is therefore not carried over, and a restored server "+
				"starts with an empty queue. To keep the queue, stop the container and archive the project directory instead. The private key in ", Code("certs/"),
			" has to be readable by the panel: give it to group 10998 on the host (", Code("chgrp 10998"), ", ", Code("chmod 640"),
			"). Until then the panel refuses to make a backup and names the file, rather than hand over an archive without the key. It holds secrets, so leave Encrypt with a password ticked: the file is sealed as ", Code(".spbk"),
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
			Code("/data"), " grows. With the inbound relay on there is a second number, for the Inbound log, with the same range. It does not change the rotation of ", Code("mail.log"),
			", which is daily with about two weeks kept. The sending rate limit shown under it is level 1, the per-client-IP ceiling set in ",
			Code(".env"), ": changing it needs a restart, and no level-2 limit can exceed it. Notifications are turned on or off here for everyone."),
	}},
	{ID: HelpNotifications, Title: "Notifications", Body: []Text{
		Rich("The panel writes to its users when something needs them, from ", Code("postmaster@"), " the server's hostname. Four things are reported. A sending domain's SPF, DKIM or DMARC record stops matching: sent once two hourly checks in a row have found it so, and once more when the record matches again. A level-2 rate limit refuses mail: sent at the first refusal, then at most once a day for the same limit. The TLS certificate is 14 days from expiry, and again at 3 days if it is still the same certificate. A message has waited in the queue for more than three days, with one more message when none is left. A global user is told all of it; a domain administrator is told about the DNS records and limits of their own domains.",
			Br(), Br(),
			"Each user turns notifications on or off under Account and needs an e-mail there; Server › Settings turns them off for everyone. They are the panel's own mail: not rows of the Outbound log, and not counted against any limit.",
			Br(), Br(),
			"For that mail to arrive, the server's own name needs two DNS records of its own, as each of your domains does: a DKIM key, which the panel generates and signs notifications with, and an SPF record, ", Code("v=spf1 a -all"), ". Server › Settings gives both with the values to publish and whether they are in place, and its Re-check DNS checks them again once they are published; Server › Health shows them too. Without them notifications are still sent, and a receiver may file them as spam. Mail of your domains does not depend on either."),
	}},
}

var accountTopics = []HelpTopic{
	{ID: HelpAccount, Title: "Your account", Body: []Text{
		Plain("Account is your own, whatever your role: your username and e-mail, whether the panel sends you notifications, and your password. Changing the password signs out every other session of this account and keeps the one you are using; applications have their own logins and are not affected."),
	}},
}
