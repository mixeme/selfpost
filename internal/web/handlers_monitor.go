package web

import (
	"errors"
	"io/fs"
	"net/http"
	"strconv"

	"github.com/mixeme/selfpost/internal/logtail"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

// sendLogPageSize bounds each send-log page (product.md's monitoring screens
// call for pagination); logTailLines bounds how much of mail.log the log view
// shows per refresh.
const (
	sendLogPageSize = 50
	logTailLines    = 200
)

// handleDeliveries renders the Deliveries page over the send log: server-side
// filters by domain/application and pagination (architecture.md §
// Persistence). The row table itself is the "deliveries_rows" fragment, shared
// verbatim with handleDeliveriesRows so the initial page and its HTMX-polled
// refreshes never diverge.
func (s *Server) handleDeliveries(w http.ResponseWriter, r *http.Request) {
	data, err := s.sendLogData(r)
	if err != nil {
		logf("panel: send log: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data["Title"] = "SelfPost — deliveries"
	data["User"] = currentUser(r)
	data["Active"] = "deliveries"
	s.render(w, http.StatusOK, "deliveries", data)
}

// handleDeliveriesRows serves the HTMX polling fragment for the delivery table
// (architecture.md § Panel HTTP surface: fragment endpoints return HTML, not
// JSON).
func (s *Server) handleDeliveriesRows(w http.ResponseWriter, r *http.Request) {
	data, err := s.sendLogData(r)
	if err != nil {
		logf("panel: send log rows: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.renderFragment(w, http.StatusOK, "deliveries_rows", data)
}

// sendLogData reads the domain/app filters and page number off the query
// string, queries the store, and assembles everything the template needs
// (filter dropdown options plus the current selection, rows, and pagination).
func (s *Server) sendLogData(r *http.Request) (map[string]any, error) {
	q := r.URL.Query()
	filter := store.SendLogFilter{
		Domain:   q.Get("domain"),
		AppLogin: q.Get("app"),
	}
	page := parsePage(q.Get("p"))

	total, err := s.store.CountSendLog(filter)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.QuerySendLog(filter, sendLogPageSize, (page-1)*sendLogPageSize)
	if err != nil {
		return nil, err
	}
	domains, err := s.store.ListDomains()
	if err != nil {
		return nil, err
	}
	domainNames := make([]string, len(domains))
	for i, d := range domains {
		domainNames[i] = d.Name
	}
	logins, err := s.store.ListApplicationLogins()
	if err != nil {
		return nil, err
	}

	lastPage := 1
	if total > 0 {
		lastPage = int((total + sendLogPageSize - 1) / sendLogPageSize)
	}
	return map[string]any{
		"Rows":          rows,
		"FilterDomains": domainNames,
		"FilterApps":    logins,
		"FilterDomain":  filter.Domain,
		"FilterApp":     filter.AppLogin,
		"Page":          page,
		"PrevPage":      page - 1,
		"NextPage":      page + 1,
		"LastPage":      lastPage,
		"HasPrev":       page > 1,
		"HasNext":       page < lastPage,
	}, nil
}

// parsePage clamps the "p" query parameter to a valid page number, defaulting
// to 1 for anything missing or malformed rather than rejecting the request.
func parsePage(v string) int {
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// handleMailQueue renders the Mail queue page (architecture.md § Panel HTTP
// surface).
func (s *Server) handleMailQueue(w http.ResponseWriter, r *http.Request) {
	out, errText := readQueue()
	s.render(w, http.StatusOK, "mail_queue", map[string]any{
		"Title":  "SelfPost — mail queue",
		"User":   currentUser(r),
		"Active": "mail_queue",
		"Output": out,
		"Error":  errText,
	})
}

// handleMailQueueBody serves the HTMX polling fragment for the queue view.
func (s *Server) handleMailQueueBody(w http.ResponseWriter, r *http.Request) {
	out, errText := readQueue()
	s.renderFragment(w, http.StatusOK, "mail_queue_body", map[string]any{
		"Output": out,
		"Error":  errText,
	})
}

// readQueue runs postqueue -p, returning a friendly message instead of the
// error itself: a transient postqueue failure should degrade the monitoring
// view, not surface internals to the panel.
func readQueue() (string, string) {
	out, err := postfix.Queue()
	if err != nil {
		logf("panel: postqueue -p: %v", err)
		return "", "Could not read the mail queue."
	}
	return out, ""
}

// handleSystemLog renders the System log page over mail.log (architecture.md §
// Panel HTTP surface).
func (s *Server) handleSystemLog(w http.ResponseWriter, r *http.Request) {
	lines, errText := s.readLogTail()
	s.render(w, http.StatusOK, "system_log", map[string]any{
		"Title":  "SelfPost — system log",
		"User":   currentUser(r),
		"Active": "system_log",
		"Lines":  lines,
		"Error":  errText,
	})
}

// handleSystemLogBody serves the HTMX polling fragment for the log-tail view.
func (s *Server) handleSystemLogBody(w http.ResponseWriter, r *http.Request) {
	lines, errText := s.readLogTail()
	s.renderFragment(w, http.StatusOK, "system_log_body", map[string]any{
		"Lines": lines,
		"Error": errText,
	})
}

func (s *Server) readLogTail() ([]string, string) {
	lines, err := logtail.TailLines(s.cfg.MailLogPath, logTailLines)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Rotation renamed the file away; Postfix recreates it on reload
			// (within about a second), so this is a normal, brief gap rather
			// than a failure worth alarming the operator about.
			return nil, ""
		}
		logf("panel: tail %s: %v", s.cfg.MailLogPath, err)
		return nil, "Could not read the mail log."
	}
	return lines, ""
}
