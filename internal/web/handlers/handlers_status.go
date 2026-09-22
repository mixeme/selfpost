package handlers

import (
	"net/http"
	"strings"

	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/web/auth"
)

func (h *Handlers) HandleStatus(w http.ResponseWriter, r *http.Request) {
	data := h.statusBody()
	srv := h.dns.Server(h.cfg.Hostname, false)

	data["Title"] = "SelfPost — status"
	data["User"] = auth.CurrentUser(r)
	data["Active"] = "status"
	data["Flash"] = statusFlash(r)
	data["Hostname"] = h.cfg.Hostname
	data["PTR"] = srv.PTR
	h.view.Render(w, http.StatusOK, "status", data)
}

func (h *Handlers) HandleStatusFragment(w http.ResponseWriter, _ *http.Request) {
	h.view.RenderFragment(w, http.StatusOK, "status_body", h.statusBody())
}

func (h *Handlers) HandleStatusRecheck(w http.ResponseWriter, r *http.Request) {
	h.dns.Server(h.cfg.Hostname, true)
	http.Redirect(w, r, "/status?rechecked=1", http.StatusSeeOther)
}

func (h *Handlers) statusBody() map[string]any {
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
