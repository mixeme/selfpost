package handlers

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/store"
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
			dns, _ := h.domainDNS(d, record, d.DMARCRua, false)
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
	h.view.Render(w, http.StatusOK, "health", healthPage(h.shellMeta(r), h.healthChecks(), statusFlash(r)))
}

// HandleHealthFragment is the polled part of Health: the same two rows of
// boxes the page holds, read again.
func (h *Handlers) HandleHealthFragment(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	page := healthPage(h.shellMeta(r), h.healthChecks(), "")
	page.Refresh = true
	h.view.RenderFragment(w, http.StatusOK, "health_body", page)
}

// HandleHealthRecheck re-runs the DNS checks of the server ignoring the cache.
func (h *Handlers) HandleHealthRecheck(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	h.dns.Server(h.cfg.Hostname, true)
	http.Redirect(w, r, "/server/health?rechecked=1", http.StatusSeeOther)
}

// healthPage turns one reading of the checks (healthChecks) into the Health
// page: a table row per resource, process and socket, and the verdict of each
// box in its head.
func healthPage(m view.Meta, c map[string]any, flash string) *view.Health {
	page := view.NewHealth(m, flash)

	machine := c["Machine"].(health.Machine)
	page.Machine.End = view.Verdict(string(machine.Status))
	page.MachineRows = machineRows(machine)

	page.Processes.End = view.Verdict(string(c["ProcessStatus"].(health.Status)))
	page.ProcessError = c["ProcessError"].(bool)
	for _, p := range c["Processes"].([]health.Process) {
		page.ProcessRows = append(page.ProcessRows, view.ProcessRow{
			Name: p.Name, State: view.Tag{Status: string(p.Status), Label: strings.ToLower(p.State)}, Detail: p.Detail,
		})
	}

	cert := c["Cert"].(health.Certificate)
	page.Certificate.End = view.Verdict(string(cert.Status))
	if !cert.NotAfter.IsZero() {
		page.CertFacts = append(page.CertFacts, view.Fact{Label: "Expires", Value: view.Plain(cert.NotAfter.UTC().Format("2006-01-02 15:04 UTC")), Mono: true})
	}
	if cert.Subject != "" {
		page.CertFacts = append(page.CertFacts, view.Fact{Label: "Names", Value: view.Plain(cert.Subject), Mono: true})
	}
	page.CertDetail, page.CertProblem = cert.Detail, cert.Status != health.StatusOK

	page.Sockets.End = view.Verdict(string(c["SocketStatus"].(health.Status)))
	for _, s := range c["Sockets"].([]health.Socket) {
		row := view.SocketRow{Name: s.Name, Note: s.Note, Path: s.Path, State: view.Tag{Status: string(s.Status)}}
		if s.Status != health.StatusOK {
			row.Detail = s.Detail
		}
		page.SocketRows = append(page.SocketRows, row)
	}

	ptr := c["PTR"].(dnscheck.Result)
	page.Hostname.End = view.Verdict(string(ptr.Status))
	host := c["Hostname"].(string)
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
	return page
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
func healthCards(c map[string]any) []view.HealthCard {
	machine := c["Machine"].(health.Machine)
	procs := c["Processes"].([]health.Process)
	cert := c["Cert"].(health.Certificate)
	sockets := c["Sockets"].([]health.Socket)
	ptr := c["PTR"].(dnscheck.Result)

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

	running, notRunning := 0, []string{}
	for _, p := range procs {
		if p.Status == health.StatusOK {
			running++
		} else {
			notRunning = append(notRunning, p.Name)
		}
	}
	procValue, procSub := fmt.Sprintf("%d of %d running", running, len(procs)), "all programs"
	if len(notRunning) > 0 {
		procSub = strings.Join(notRunning, ", ")
	}
	if c["ProcessError"].(bool) {
		procValue, procSub = "Could not be read", "supervisord"
	}

	certValue := cert.Detail
	if cert.Status == health.StatusOK || cert.Status == health.StatusWarn {
		certValue = fmt.Sprintf("Expires in %d days", cert.DaysLeft)
	}

	queueValue := queueCardValue(c["QueueSummary"].(string))
	if e := c["QueueError"].(string); e != "" {
		queueValue = "Could not be read"
	}

	answering, socketNames := 0, make([]string, 0, len(sockets))
	for _, s := range sockets {
		if s.Present {
			answering++
		}
		socketNames = append(socketNames, s.Name)
	}

	ptrValue := "Forward = reverse"
	if ptr.Status != health.StatusOK {
		ptrValue = "Needs attention"
		if ptr.Status == health.StatusUnknown {
			ptrValue = "Not checked"
		}
	}

	return []view.HealthCard{
		card("Machine", "ti-cpu", "/server/health#machine", machine.Status, cpu, ram),
		card("Processes", "ti-server-cog", "/server/health#processes", c["ProcessStatus"].(health.Status), procValue, procSub),
		card("TLS certificate", "ti-certificate", "/server/health#certificate", cert.Status, certValue, cert.Subject),
		card("Queue", "ti-stack-2", "/outbound/queue", c["QueueStatus"].(health.Status), queueValue, "outbound"),
		card("Milter sockets", "ti-plug-connected", "/server/health#sockets", c["SocketStatus"].(health.Status),
			fmt.Sprintf("%d of %d answering", answering, len(sockets)), strings.Join(socketNames, ", ")),
		card("Reverse DNS", "ti-arrows-exchange", "/server/health#hostname", ptr.Status, ptrValue, c["Hostname"].(string)),
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

// healthChecks runs every server check once.
func (h *Handlers) healthChecks() map[string]any {
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

	cert := health.CheckCertificate(h.cfg.TLSCertFile)
	sockets := []health.Socket{
		health.CheckSocket("OpenDKIM", h.cfg.OpenDKIMSocket, true),
		health.CheckSocket("send-log", h.cfg.JournalSocket, false),
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

	return map[string]any{
		"Processes":     procs,
		"ProcessError":  procErr != nil,
		"ProcessStatus": procStatus,
		"QueueSummary":  queueSummary(queueText),
		"QueueError":    queueErr,
		"QueueStatus":   queueStatus,
		"Machine":       machine,
		"Cert":          cert,
		"Sockets":       sockets,
		"SocketStatus":  socketStatus,
		"Hostname":      h.cfg.Hostname,
		"PTR":           srv.PTR,
	}
}

// filterSocket is the inbound spam filter's row among the milter sockets. A
// filter that is down costs what the server's setting says: with the action
// tempfail Postfix defers inbound mail (an error, like OpenDKIM), otherwise it
// accepts the mail unfiltered (a warning, like the send log). The address is the
// one in INBOUND_ANTISPAM_MILTER as configured; Health and Overview, the only
// pages that show it, are for the global administrator.
func filterSocket(milter, action string) health.Socket {
	tempfail := action == "tempfail"
	s := health.CheckMilter("Spam filter", milter, tempfail)
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
		return "Configuration regenerated from the database; OpenDKIM and Postfix have re-read it."
	case r.URL.Query().Get("rechecked") != "":
		return "DNS re-checked."
	default:
		return ""
	}
}
