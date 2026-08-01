package web

import (
	"net/http"
	"strings"

	"codeberg.org/mix/selfpost/internal/health"
)

// handleStatus renders the server status page: the panel's landing page and the
// one screen that answers "is the service healthy and will mail be accepted"
// (phase 13.A). The cheap local checks live in the polled "status_body"
// fragment; the hostname/PTR lookup and the configuration reload sit outside it,
// because neither belongs on a five-second timer.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	data := s.statusBody()
	srv := s.dns.Server(s.cfg.Hostname, false)

	data["Title"] = "SelfPost — status"
	data["User"] = currentUser(r)
	data["Active"] = "status"
	data["Flash"] = statusFlash(r)
	data["Hostname"] = s.cfg.Hostname
	data["PTR"] = srv.PTR
	s.render(w, http.StatusOK, "status", data)
}

// handleStatusFragment serves the HTMX polling fragment for the local checks
// (spec 7.1: fragment endpoints return HTML, not JSON).
func (s *Server) handleStatusFragment(w http.ResponseWriter, _ *http.Request) {
	s.renderFragment(w, http.StatusOK, "status_body", s.statusBody())
}

// handleStatusRecheck forces a fresh hostname/PTR lookup, bypassing the cache,
// and returns to the page. DNS is the one part of this screen that talks to the
// network, so it refreshes on demand rather than with the poll.
func (s *Server) handleStatusRecheck(w http.ResponseWriter, r *http.Request) {
	s.dns.Server(s.cfg.Hostname, true)
	http.Redirect(w, r, "/status?rechecked=1", http.StatusSeeOther)
}

// statusBody collects the four local checks the fragment renders. Each one
// reports its own problem rather than failing the page, so a broken component
// costs one line and not the whole screen.
func (s *Server) statusBody() map[string]any {
	procs, procErr := health.Processes()
	procStatus := health.StatusUnknown
	if procErr != nil {
		// Outside the container (or if the control socket is gone) there is
		// nothing to report — "unknown", not "everything is broken".
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

	cert := health.CheckCertificate(s.cfg.TLSCertFile)
	sockets := []health.Socket{
		// OpenDKIM signs every outgoing message and Postfix is configured to
		// tempfail without it: a missing socket stops mail.
		health.CheckSocket("OpenDKIM", s.cfg.OpenDKIMSocket, true),
		// The journal-milter only records the send log and fails open.
		health.CheckSocket("send-log", s.cfg.JournalSocket, false),
	}
	socketStatus := health.StatusUnknown
	for _, sock := range sockets {
		socketStatus = health.Worst(socketStatus, sock.Status)
	}

	overall := health.Worst(procStatus, queueStatus, cert.Status, socketStatus)
	return map[string]any{
		"Processes":      procs,
		"ProcessError":   procErr != nil,
		"ProcessStatus":  procStatus,
		"QueueSummary":   queueSummary(queueText),
		"QueueError":     queueErr,
		"QueueStatus":    queueStatus,
		"Cert":           cert,
		"Sockets":        sockets,
		"SocketStatus":   socketStatus,
		"OverallStatus":  overall,
		"OverallHeading": overallHeading(overall),
	}
}

// queueSummary reduces postqueue's listing to the one line worth showing on the
// status page; the full listing has its own screen (spec 7.2.11). postqueue
// prints either "Mail queue is empty" or a trailing "-- N Kbytes in M Requests."
func queueSummary(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return strings.TrimSpace(strings.TrimPrefix(line, "--"))
		}
	}
	return ""
}

// overallHeading turns the worst check into the page's one-line verdict.
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

// statusFlash maps a fixed redirect flag to a fixed message, so status text
// after a redirect is never attacker-influenced.
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
