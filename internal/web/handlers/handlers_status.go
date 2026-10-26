package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/logtail"
	"github.com/mixeme/selfpost/internal/notify"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/validate"
	"github.com/mixeme/selfpost/internal/web/view"
)

// The old Status page is two pages in 2.0 (plan § Information architecture):
//
//   - Overview answers "is mail flowing?" with one verdict per check, each a
//     card that leads to its detail, and lists the two kinds of domain;
//   - Server › Health is everything behind those cards — the tables — and the
//     home of the two actions, Re-check DNS and Reload configuration.
//
// Both read the same checks (healthChecks), so a card and its table can never
// disagree.

// HandleOverview shows the verdicts and the domains.
func (h *Handlers) HandleOverview(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	domains, err := h.store.ListDomains()
	if err != nil {
		logf("panel: overview: list domains: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	retention := h.sendLogRetentionDays()
	_, _, days := store.StatsWindow(retention, time.Now())

	inboundOn := h.cfg.InboundEnabled && h.inbound != nil
	page := view.NewOverview(h.shellMeta(r), h.cfg.Hostname, healthCards(h.healthChecks()), time.Now(), days, inboundOn)
	page.OutboundRows = h.overviewDomains(domains, retention)
	if inboundOn {
		list, err := h.inbound.List()
		if err != nil {
			logf("panel: overview: list inbound domains: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		for _, row := range h.inboundRows(list, false) {
			page.InboundRows = append(page.InboundRows, view.OverviewInbound{
				Name: row.Name, Href: row.Href, MX: row.DNS, Upstream: row.Upstream,
			})
		}
	}
	h.view.Render(w, http.StatusOK, "overview", page)
}

// HandleOverviewFragment is the polled part of Overview: the Server health box
// read again, and the page head — stamp, sentence, time — which follows the
// cards. The domain tables are not part of it; they are not a live reading.
func (h *Handlers) HandleOverviewFragment(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	_, _, days := store.StatsWindow(h.sendLogRetentionDays(), time.Now())
	page := view.NewOverview(h.shellMeta(r), h.cfg.Hostname, healthCards(h.healthChecks()), time.Now(), days, false)
	h.view.RenderFragment(w, http.StatusOK, "overview_poll", page)
}

// overviewDomains is one row per sending domain: the verdict of each DNS check
// (cached for a few minutes, as on the domain list), the applications, and the
// mail it sent in the stats window.
func (h *Handlers) overviewDomains(domains []store.Domain, retention int) []view.OverviewDomain {
	rows := make([]view.OverviewDomain, len(domains))
	var wg sync.WaitGroup
	for i, d := range domains {
		rows[i] = view.OverviewDomain{
			Name: d.Name, Href: fmt.Sprintf("/outbound/domains/%d", d.ID), Apps: d.AppCount,
			DNS: dnsTags(health.StatusUnknown, health.StatusUnknown, health.StatusUnknown), Messages: "—",
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if stats, err := h.store.DomainSendStats(d.Name, retention, d.CreatedAt); err != nil {
				logf("panel: overview: domain %d: send stats: %v", d.ID, err)
			} else {
				rows[i].Messages = view.FormatMessages(stats.Total)
			}
			record, err := h.domains.DKIMRecord(d)
			if err != nil {
				logf("panel: overview: domain %d: dkim record: %v", d.ID, err)
				return
			}
			dns := h.domainDNS(d, record, d.DMARCRua, false)
			rows[i].DNS = dnsTags(dns.DKIM.Status, dns.SPF.Status, dns.DMARC.Status)
		}()
	}
	wg.Wait()
	return rows
}

func dnsTags(dkim, spf, dmarc health.Status) []view.Tag {
	return []view.Tag{
		{Status: string(dkim), Label: "DKIM"},
		{Status: string(spf), Label: "SPF"},
		{Status: string(dmarc), Label: "DMARC"},
	}
}

// HandleHealth shows every check in full, with the two server actions.
func (h *Handlers) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	h.view.Render(w, http.StatusOK, "health", h.healthView(r, statusFlash(r)))
}

// HandleHealthFragment is the polled part of Health: the boxes the page holds,
// all but Configuration, read again.
func (h *Handlers) HandleHealthFragment(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	h.view.RenderFragment(w, http.StatusOK, "health_body", h.healthView(r, "").AsFragment())
}

// HandleHealthRecheck re-runs the DNS checks of the server ignoring the cache.
func (h *Handlers) HandleHealthRecheck(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	h.dns.Server(h.cfg.Hostname, true)
	h.serverMail(true)
	http.Redirect(w, r, "/server/health?rechecked=1", http.StatusSeeOther)
}

// testMessagesPerHour is how many test messages the panel sends in an hour,
// counted from the send log, whoever asked: a form that sends mail must not
// become a way to send a lot of it.
const testMessagesPerHour = 10

// HandleTestMessage sends the panel's test message (test-email; owner
// 2026-10-13, its sender his choice of 2026-10-16): from postmaster@ the
// hostname to the one address typed, as notifications go - through
// sendmail, signed by the signer's local socket with the key of the
// server's own name. The row is written first, with the Message-ID the
// tailer awaits to give it its queue id, and the browser goes to it: its
// page follows the message to the receiving server. Global role only; one
// address (validate.AccountEmail refuses a list, as it does a name that
// reads as an option); ten an hour.
func (h *Handlers) HandleTestMessage(w http.ResponseWriter, r *http.Request) {
	p, ok := h.requireGlobal(w, r)
	if !ok {
		return
	}
	from := notify.From(h.cfg.Hostname)
	to := ""
	fail := func(status int, msg string) {
		page := h.healthView(r, "")
		page.WithTestMessage(from, to, msg)
		h.view.Render(w, status, "health", page)
	}
	if err := r.ParseForm(); err != nil {
		fail(http.StatusBadRequest, "Invalid form submission.")
		return
	}
	to = strings.TrimSpace(r.PostFormValue("recipient"))
	if to == "" || validate.AccountEmail(to) != nil {
		fail(http.StatusBadRequest, "Enter one e-mail address.")
		return
	}
	n, err := h.store.CountTestMessagesSince(from, time.Now().Add(-time.Hour))
	if err != nil {
		logf("panel: test message: %v", err)
		fail(http.StatusInternalServerError, "Internal error. Please try again.")
		return
	}
	if n >= testMessagesPerHour {
		fail(http.StatusTooManyRequests, "Ten test messages have gone in the last hour. Please try again later.")
		return
	}
	id, err := notify.NewMessageID(h.cfg.Hostname)
	if err != nil {
		logf("panel: test message: %v", err)
		fail(http.StatusInternalServerError, "Internal error. Please try again.")
		return
	}
	const subject = "SelfPost test message"
	now := time.Now()
	body := "A test message from SelfPost at " + h.cfg.Hostname + ", sent from Server › Health on " +
		now.Format("2006-01-02 at 15:04 MST") + ".\n\nIt came from this server, signed with the DKIM key of its own name. " +
		"Its page in the panel's Outbound log shows what this mail server answered.\n"
	row, err := h.store.InsertTestMessage(h.cfg.Hostname, from, to, subject)
	if err != nil {
		logf("panel: test message: %v", err)
		fail(http.StatusInternalServerError, "Internal error. Please try again.")
		return
	}
	logtail.AwaitMessageID(id, row)
	if err := h.testMail.SendWithID(to, subject, body, id); errors.Is(err, notify.ErrTimeout) {
		// postdrop may have queued it after all: the row and the awaited id
		// stay, and the row's page shows what became of it - or the sweep
		// closes the row once the id is no longer awaited.
		logf("panel: test message to %s (send log %d): %v", to, row, err)
	} else if err != nil {
		logtail.ForgetMessageID(id)
		if derr := h.store.DeleteSendLogRow(row); derr != nil {
			logf("panel: test message: %v", derr)
		}
		logf("panel: test message to %s: %v", to, err)
		fail(http.StatusBadGateway, "Postfix did not take the message; the container log says why.")
		return
	}
	logf("panel: user %d sent a test message to %s (send log %d)", p.ID, to, row)
	http.Redirect(w, r, "/outbound/log/"+strconv.FormatInt(row, 10), http.StatusSeeOther)
}

// healthView is the Health page for the request: the checks behind the cards
// of Overview and the Notification mail card, which only Health shows.
func (h *Handlers) healthView(r *http.Request, flash string) *view.Health {
	page := healthPage(h.shellMeta(r), h.healthChecks(), flash)
	seen := h.auth.Seen(r)
	page.WithClientAddress(seen.Client, seen.Peer, seen.FromHeader, seen.HeaderIgnored)
	page.WithTestMessage(notify.From(h.cfg.Hostname), "", "")
	mail := h.serverMail(false)
	page.WithNotificationMail(notify.From(h.cfg.Hostname), mail.rows, mail.status)
	stats, err := h.store.SenderCheckStats(time.Now())
	if err != nil {
		logf("panel: health: sender check statistics: %v", err)
	}
	rows, since, status := senderCheckCard(stats, time.Now())
	if err != nil {
		// Zeros that were not read are not zeros.
		for i := range rows {
			rows[i].Down, rows[i].Deferred, rows[i].Messages = "—", "—", "—"
		}
		status = string(health.StatusUnknown)
	}
	return page.WithSenderCheck(rows, since, status)
}

// senderCheckCard is the Sender check card from its statistics. It warns when
// a connection was turned away or a message deferred in the last 24 hours:
// that is mail an application had to send again. Downtime that deferred
// nothing is only shown.
func senderCheckCard(s store.SenderCheckStats, now time.Time) (rows []view.SenderCheckRow, since, status string) {
	row := func(period string, t store.SenderCheckTotals) view.SenderCheckRow {
		down := view.Downtime(t.Down)
		if t.Estimated {
			down = "≈ " + down
		}
		return view.SenderCheckRow{Period: period, Down: down,
			Deferred: strconv.FormatInt(t.Deferred, 10), Messages: strconv.FormatInt(t.Messages, 10)}
	}
	rows = []view.SenderCheckRow{row("Today", s.Today), row("Last 30 days", s.Month), row("Last 12 months", s.Year)}
	if !s.Since.IsZero() {
		since = s.Since.Format("2006-01-02")
	}
	status = string(health.StatusOK)
	if !s.DeferredAt.IsZero() && now.Sub(s.DeferredAt) < 24*time.Hour {
		status = string(health.StatusWarn)
	}
	return rows, since, status
}

// healthPage turns one reading of the checks (healthChecks) into the Health
// page: a table row per resource, process and socket, and the verdict of each
// box in its head.
func healthPage(m view.Meta, c healthReadings, flash string) *view.Health {
	page := view.NewHealth(m, flash)

	machine := c.Machine
	page.Machine.End = view.Verdict(string(machine.Status))
	page.MachineRows = machineRows(machine)

	page.Processes.End = view.Verdict(string(c.ProcessStatus))
	page.ProcessError = c.ProcessError
	for _, p := range c.Processes {
		page.ProcessRows = append(page.ProcessRows, processRow(p))
	}

	cert := c.Cert
	page.Certificate.End = view.Verdict(string(cert.Status))
	if !cert.NotAfter.IsZero() {
		page.CertFacts = append(page.CertFacts, view.Fact{Label: "Expires", Value: view.Plain(cert.NotAfter.Local().Format("2006-01-02 15:04")), Mono: true})
	}
	if len(cert.Names) > 0 {
		names := view.Fact{Label: "Names", Value: view.Plain(strings.Join(cert.Names, ", ")), Mono: true}
		if m := cert.MatchesHost; m != nil {
			names.LabelTag = &view.Tag{Status: "ok", Label: "matches"}
			if !*m {
				names.LabelTag = &view.Tag{Status: "fail", Label: "not for the hostname"}
			}
		}
		page.CertFacts = append(page.CertFacts, names)
	}
	if r := cert.Renewal; r != nil {
		page.CertFacts = append(page.CertFacts,
			view.Fact{Label: "Renewal expires", Value: view.Plain(r.NotAfter.Local().Format("2006-01-02 15:04")), Mono: true},
			view.Fact{Label: "Renewal mounted", Value: view.Plain(r.Mounted.Local().Format("2006-01-02 15:04")), Mono: true})
	}
	page.CertDetail, page.CertProblem = cert.Detail, cert.Status != health.StatusOK

	page.Sockets.End = view.Verdict(string(c.SocketStatus))
	for _, s := range c.Sockets {
		row := view.SocketRow{Name: s.Name, Note: s.Note, Path: s.Path, State: view.Tag{Status: string(s.Status)}}
		if s.Status != health.StatusOK {
			row.Detail = s.Detail
		}
		page.SocketRows = append(page.SocketRows, row)
	}

	ptr := c.PTR
	hostStatus := ptr.Status
	host := c.Hostname
	if host == "" {
		host = "(SELFPOST_HOSTNAME is not set)"
	}
	page.HostFacts = []view.Fact{{Label: "Hostname", Value: view.Plain(host), Mono: true}}
	if len(ptr.Records) > 0 {
		var lookup view.Text
		for i, rec := range ptr.Records {
			if i > 0 {
				lookup = append(lookup, view.Br())
			}
			lookup = append(lookup, view.Inline{Text: rec})
		}
		page.HostFacts = append(page.HostFacts, view.Fact{Label: "Lookup", Value: lookup, Mono: true})
	}
	if ptr.Status != health.StatusOK {
		page.HostDetail, page.HostProblem = ptr.Detail, ptr.Status == health.StatusError
	}
	// The greeting must name the hostname: a receiver compares it with the
	// name it looked up, and a container id or localhost there reads as a
	// misconfigured host. A banner that could not be read says nothing.
	if c.Banner != "" {
		fact := view.Fact{Label: "Greeting", Value: view.Plain("220 " + c.Banner), Mono: true, Wide: true}
		if f := strings.Fields(c.Banner); c.Hostname != "" && (len(f) == 0 || !strings.EqualFold(f[0], c.Hostname)) {
			hostStatus = health.Worst(hostStatus, health.StatusWarn)
			fact.Note = view.Plain("Postfix greets with another name than " + c.Hostname + ": set myhostname to it (build/postfix-config.sh does, from SELFPOST_HOSTNAME).")
		}
		page.HostFacts = append(page.HostFacts, fact)
	}
	page.Hostname.End = view.Verdict(string(hostStatus))
	return page
}

// processRow is one line of the Processes table. A one-shot helper that sits
// idle is not "stopped" the way a daemon is: it runs when asked and exits, so
// its row says "on demand" in a neutral tag and when it last ran, instead of a
// word that reads as a fault beside the running programs.
func processRow(p health.Process) view.ProcessRow {
	if p.OneShot && p.Status == health.StatusOK && p.State != "RUNNING" {
		detail := "Runs when the configuration is reloaded"
		switch {
		case p.Detail == "" || strings.EqualFold(p.Detail, "Not started"):
			detail += "; not run since the container started"
		default:
			detail += "; last run: " + p.Detail
		}
		return view.ProcessRow{Name: p.Name, State: view.Tag{Status: "off", Label: "on demand"}, Detail: detail}
	}
	return view.ProcessRow{Name: p.Name, State: view.Tag{Status: string(p.Status), Label: strings.ToLower(p.State)}, Detail: p.Detail}
}

// machineRows is the Machine table: processor and memory as bars with their
// figure, the network as rates, and for each the sentence the sampler gave. A
// reading that could not be taken keeps its row, without a figure.
func machineRows(m health.Machine) []view.MachineRow {
	cpu := view.MachineRow{Resource: "CPU", Detail: lines(m.CPU.Detail)}
	if m.CPU.Measured {
		cpu.Gauge = &view.Gauge{Percent: m.CPU.Percent(), Text: m.CPU.BusyText(), Level: cardLevel(m.CPU.Status)}
	}
	mem := view.MachineRow{Resource: "Memory", Detail: lines(m.Memory.Detail)}
	if m.Memory.Measured {
		mem.Gauge = &view.Gauge{Percent: m.Memory.Percent(), Text: m.Memory.PctText(), Level: cardLevel(m.Memory.Status)}
	}
	net := view.MachineRow{Resource: "Network"}
	if m.Network.Measured {
		net.Usage = "↓ " + m.Network.InRateText() + " · ↑ " + m.Network.OutRateText()
	}
	for _, i := range m.Network.Interfaces {
		net.Detail = append(net.Detail, fmt.Sprintf("%s: %s in, %s out", i.Name, i.InText(), i.OutText()))
	}
	net.Detail = append(net.Detail, lines(m.Network.Detail)...)
	return []view.MachineRow{cpu, mem, net}
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// healthCards turns the checks into the six cards of Overview, in the order of
// the tables on Health. Each card links to the table behind it.
func healthCards(c healthReadings) []view.HealthCard {
	machine := c.Machine
	procs := c.Processes
	cert := c.Cert
	sockets := c.Sockets
	ptr := c.PTR

	card := func(name, icon, href string, status health.Status, value, sub string) view.HealthCard {
		return view.HealthCard{Name: name, Icon: icon, Href: href, Level: cardLevel(status), Value: value, Sub: sub}
	}

	cpu, ram := "No reading", ""
	if machine.CPU.Measured {
		cpu = "CPU " + machine.CPU.BusyText()
	}
	if machine.Memory.Measured {
		ram = "RAM " + machine.Memory.PctText()
	}

	// The count is of the programs that are meant to stay up. A one-shot helper
	// (postfix-reload) idles between runs: it is not "running" and not missing,
	// so it is left out of both numbers and named only when it is in trouble.
	running, expected, notRunning := 0, 0, []string{}
	for _, p := range procs {
		if !p.OneShot {
			expected++
			if p.Status == health.StatusOK {
				running++
			}
		}
		if p.Status != health.StatusOK {
			notRunning = append(notRunning, p.Name)
		}
	}
	procValue, procSub := fmt.Sprintf("%d of %d running", running, expected), "all programs"
	if len(notRunning) > 0 {
		procSub = strings.Join(notRunning, ", ")
	}
	if c.ProcessError {
		procValue, procSub = "Could not be read", "supervisord"
	}

	certValue := cert.Detail
	if cert.Status == health.StatusOK || cert.Status == health.StatusWarn {
		certValue = fmt.Sprintf("Expires in %d days", cert.DaysLeft)
	}

	// One Postfix queue, split into two pages when the inbound relay is on
	// (mockup overview: "Queues · Both empty · outbound and inbound").
	queueName, queueValue, queueSub := "Queue", queueCardValue(c.QueueSummary), "outbound"
	if c.Inbound {
		queueName, queueSub = "Queues", "outbound and inbound"
		if queueValue == "Empty" {
			queueValue = "Both empty"
		}
	}
	if e := c.QueueError; e != "" {
		queueValue = "Could not be read"
	}

	// The line under the count has to fit a card: the sockets that do not
	// answer when some do not; otherwise their names - the signer's two once -
	// or, with the inbound spam filter among them, when three names no longer
	// fit, that it is included.
	answering, socketNames, silent, withFilter := 0, make([]string, 0, len(sockets)), []string{}, false
	for _, s := range sockets {
		if s.Present {
			answering++
		} else if s.Note != "" && s.Name != filterName {
			silent = append(silent, s.Name+" · "+s.Note)
		} else {
			silent = append(silent, s.Name)
		}
		if s.Name == filterName {
			withFilter = true
		}
		if !slices.Contains(socketNames, s.Name) {
			socketNames = append(socketNames, s.Name)
		}
	}
	socketSub := strings.Join(socketNames, ", ")
	switch {
	case len(silent) > 0:
		socketSub = strings.Join(silent, ", ")
	case withFilter:
		socketSub = "incl. the spam filter"
	}

	ptrValue := "Forward = reverse"
	if ptr.Status != health.StatusOK {
		ptrValue = "Needs attention"
		if ptr.Status == health.StatusUnknown {
			ptrValue = "Not checked"
		}
	}

	return []view.HealthCard{
		card("Machine", "ti-cpu", "/server/health", machine.Status, cpu, ram),
		card("Processes", "ti-server-cog", "/server/health", c.ProcessStatus, procValue, procSub),
		card("TLS certificate", "ti-certificate", "/server/health", cert.Status, certValue, cert.Subject),
		card(queueName, "ti-stack-2", "/outbound/queue", c.QueueStatus, queueValue, queueSub),
		card("Milter sockets", "ti-plug-connected", "/server/health", c.SocketStatus,
			fmt.Sprintf("%d of %d answering", answering, len(sockets)), socketSub),
		card("Reverse DNS", "ti-arrows-exchange", "/server/health", ptr.Status, ptrValue, c.Hostname),
	}
}

// queuedRequests reads the count out of postqueue's last line, "-- 3 Kbytes in 2
// Requests.".
var queuedRequests = regexp.MustCompile(`(\d+) Requests?`)

// queueCardValue is what the Queue card says in words short enough for a card:
// "Empty", "2 queued", or — for a line it does not recognise — the line itself.
func queueCardValue(summary string) string {
	switch m := queuedRequests.FindStringSubmatch(summary); {
	case summary == "" || strings.Contains(summary, "queue is empty"):
		return "Empty"
	case m != nil:
		return m[1] + " queued"
	}
	return summary
}

// cardLevel maps the status of a check onto the three states of a card. A
// check that could not run is shown as a warning: the card must not look fine
// when nothing was verified.
func cardLevel(s health.Status) string {
	switch s {
	case health.StatusOK:
		return ""
	case health.StatusError:
		return "fail"
	default:
		return "warn"
	}
}

// healthReadings are what Health and Overview are drawn from, read once per
// page. A struct, not a map: a mistyped name is a compile error and not an
// empty card (owner, 2026-10-13; the last of template-data-typing).
type healthReadings struct {
	Processes     []health.Process
	ProcessError  bool
	ProcessStatus health.Status
	QueueSummary  string
	QueueError    string
	QueueStatus   health.Status
	// Inbound is the relay on port 25: Postfix's one queue then holds mail of
	// both directions, and the Queues card says so.
	Inbound      bool
	Machine      health.Machine
	Cert         health.Certificate
	Sockets      []health.Socket
	SocketStatus health.Status
	Hostname     string
	PTR          dnscheck.Result
	// Banner is Postfix's smtpd_banner, expanded: the greeting after "220".
	Banner string
}

// healthChecks runs every server check once.
func (h *Handlers) healthChecks() healthReadings {
	procs, procErr := health.Processes()
	procStatus := health.StatusUnknown
	if procErr != nil {
		logf("panel: status: supervisorctl: %v", procErr)
	} else {
		for _, p := range procs {
			procStatus = health.Worst(procStatus, p.Status)
		}
	}

	queueText, queueErr := readQueue()
	queueStatus := health.StatusOK
	if queueErr != "" {
		queueStatus = health.StatusWarn
	}

	served := ""
	if h.cfg.TLSServedCertFile != nil {
		served = h.cfg.TLSServedCertFile()
	}
	cert := health.CheckServedCertificate(served, h.cfg.TLSCertFile, h.cfg.TLSReloadInterval, time.Now()).ForHost(h.cfg.Hostname)
	sockets := []health.Socket{
		signerSocket(h.cfg.SignSocket, "applications"),
		signerSocket(h.cfg.SignLocalSocket, "notifications"),
		health.CheckSocket("sender-check", h.cfg.SenderSocket, true),
		health.CheckSocket("journal-milter", h.cfg.JournalSocket, false),
	}
	// The inbound journal writes Inbound › Log, fail-open as the outbound
	// one: dialled while there is an inbound relay to journal.
	if h.cfg.InboundEnabled {
		sockets = append(sockets, health.CheckSocket("inbound-journal-milter", h.cfg.InboundJournalSocket, false))
	}
	if on, milter, action := h.inboundFilter(); on {
		sockets = append(sockets, filterSocket(milter, action))
	}
	socketStatus := health.StatusUnknown
	for _, sock := range sockets {
		socketStatus = health.Worst(socketStatus, sock.Status)
	}

	machine := h.machine.Sample()
	srv := h.dns.Server(h.cfg.Hostname, false)

	return healthReadings{
		Processes:     procs,
		ProcessError:  procErr != nil,
		ProcessStatus: procStatus,
		QueueSummary:  queueSummary(queueText),
		QueueError:    queueErr,
		QueueStatus:   queueStatus,
		Inbound:       h.cfg.InboundEnabled,
		Machine:       machine,
		Cert:          cert,
		Sockets:       sockets,
		SocketStatus:  socketStatus,
		Hostname:      h.cfg.Hostname,
		PTR:           srv.PTR,
		Banner:        h.cfg.SMTPBanner,
	}
}

// filterName is the inbound spam filter's row among the milter sockets.
const filterName = "Spam filter"

// signerSocket is a row of the DKIM signer's: one socket for what
// applications send, one for the panel's own notifications. Postfix attaches
// both strictly, so either one down defers the mail that goes through it.
func signerSocket(path, note string) health.Socket {
	s := health.CheckSocket("dkim-signer", path, true)
	s.Note = note
	return s
}

// filterSocket is the inbound spam filter's row among the milter sockets. A
// filter that is down costs what the server's setting says: with the action
// tempfail Postfix defers inbound mail (an error, like the signer), otherwise it
// accepts the mail unfiltered (a warning, like the send log). The address is the
// one in INBOUND_ANTISPAM_MILTER as configured; Health and Overview, the only
// pages that show it, are for the global administrator.
func filterSocket(milter, action string) health.Socket {
	tempfail := action == "tempfail"
	s := health.CheckMilter(filterName, milter, tempfail)
	s.Note = "inbound"
	switch {
	case s.Status != health.StatusError && s.Status != health.StatusWarn:
	case tempfail:
		s.Detail += " Inbound mail is deferred until it is back."
	default:
		s.Detail += " Inbound mail goes through unfiltered."
	}
	return s
}

func queueSummary(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return strings.TrimSpace(strings.TrimPrefix(line, "--"))
		}
	}
	return ""
}

func statusFlash(r *http.Request) string {
	switch {
	case r.URL.Query().Get("reloaded") != "":
		return "Configuration regenerated from the database - the DKIM selector table, every map the panel writes for Postfix, the filter lists - and Postfix has re-read it."
	case r.URL.Query().Get("rechecked") != "":
		return "DNS re-checked."
	default:
		return ""
	}
}
