package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/web/auth"
	"github.com/mixeme/selfpost/internal/web/view"
)

// The old Status page is two pages in 2.0 (plan § Information architecture):
//
//   - Overview answers "is mail flowing?" with one verdict per check, each a
//     card that leads to its detail;
//   - Server › Health is everything behind those cards — the tables — and the
//     home of the two actions, Re-check DNS and Reload configuration.
//
// Both read the same checks (healthChecks), so a card and its table can never
// disagree. Until stage 2 gives each page its own template they share the old
// one, switched by .Overview.

// HandleOverview shows the verdicts.
func (h *Handlers) HandleOverview(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	data := h.overviewBody()
	data["Title"] = "SelfPost — overview"
	data["User"] = auth.CurrentUser(r)
	data["Active"] = "status"
	data["IsGlobal"] = true
	h.view.Render(w, http.StatusOK, "status", data)
}

// HandleOverviewFragment is the polled part of Overview.
func (h *Handlers) HandleOverviewFragment(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	h.view.RenderFragment(w, http.StatusOK, "status_body", h.overviewBody())
}

// HandleHealth shows every check in full, with the two server actions.
func (h *Handlers) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	data := h.healthChecks()
	data["Title"] = "SelfPost — health"
	data["User"] = auth.CurrentUser(r)
	data["Active"] = "health"
	data["IsGlobal"] = true
	data["Flash"] = statusFlash(r)
	h.view.Render(w, http.StatusOK, "status", data)
}

// HandleHealthFragment is the polled part of Health.
func (h *Handlers) HandleHealthFragment(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	h.view.RenderFragment(w, http.StatusOK, "status_body", h.healthChecks())
}

// HandleHealthRecheck re-runs the DNS checks of the server ignoring the cache.
func (h *Handlers) HandleHealthRecheck(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	h.dns.Server(h.cfg.Hostname, true)
	http.Redirect(w, r, "/server/health?rechecked=1", http.StatusSeeOther)
}

// overviewBody reduces the checks to what Overview shows: the overall verdict
// and one card per check. The cards are the typed input of the health_card
// partial of the component kit, so the stage-2 page renders them as they are.
func (h *Handlers) overviewBody() map[string]any {
	c := h.healthChecks()
	return map[string]any{
		"Overview":       true,
		"Hostname":       c["Hostname"],
		"OverallStatus":  c["OverallStatus"],
		"OverallHeading": c["OverallHeading"],
		"Cards":          healthCards(c),
	}
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

	running := 0
	for _, p := range procs {
		if p.Status == health.StatusOK {
			running++
		}
	}
	procValue := fmt.Sprintf("%d of %d running", running, len(procs))
	if c["ProcessError"].(bool) {
		procValue = "Could not be read"
	}

	certValue := cert.Detail
	if cert.Status == health.StatusOK || cert.Status == health.StatusWarn {
		certValue = fmt.Sprintf("Expires in %d days", cert.DaysLeft)
	}

	queueValue := c["QueueSummary"].(string)
	if e := c["QueueError"].(string); e != "" {
		queueValue = "Could not be read"
	}

	answering := 0
	for _, s := range sockets {
		if s.Present {
			answering++
		}
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
		card("Processes", "ti-server-cog", "/server/health#processes", c["ProcessStatus"].(health.Status), procValue, ""),
		card("TLS certificate", "ti-certificate", "/server/health#certificate", cert.Status, certValue, cert.Subject),
		card("Queue", "ti-stack-2", "/outbound/queue", c["QueueStatus"].(health.Status), queueValue, "outbound"),
		card("Milter sockets", "ti-plug-connected", "/server/health#sockets", c["SocketStatus"].(health.Status),
			fmt.Sprintf("%d of %d answering", answering, len(sockets)), ""),
		card("Reverse DNS", "ti-arrows-exchange", "/server/health#hostname", ptr.Status, ptrValue, c["Hostname"].(string)),
	}
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
	socketStatus := health.StatusUnknown
	for _, sock := range sockets {
		socketStatus = health.Worst(socketStatus, sock.Status)
	}

	machine := h.machine.Sample()
	srv := h.dns.Server(h.cfg.Hostname, false)

	overall := health.Worst(procStatus, queueStatus, cert.Status, socketStatus, machine.Status)
	return map[string]any{
		"Processes":      procs,
		"ProcessError":   procErr != nil,
		"ProcessStatus":  procStatus,
		"QueueSummary":   queueSummary(queueText),
		"QueueError":     queueErr,
		"QueueStatus":    queueStatus,
		"Machine":        machine,
		"Cert":           cert,
		"Sockets":        sockets,
		"SocketStatus":   socketStatus,
		"Hostname":       h.cfg.Hostname,
		"PTR":            srv.PTR,
		"OverallStatus":  overall,
		"OverallHeading": overallHeading(overall),
	}
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

func overallHeading(worst health.Status) string {
	switch worst {
	case health.StatusError:
		return "A component needs attention — see the details below."
	case health.StatusWarn:
		return "Running, with warnings below."
	case health.StatusOK:
		return "All components are running normally."
	default:
		return "Some checks could not be performed."
	}
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
