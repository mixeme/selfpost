package web

import (
	"net/http"
	"strconv"

	"codeberg.org/mix/selfpost/internal/logtail"
	"codeberg.org/mix/selfpost/internal/postfix"
	"codeberg.org/mix/selfpost/internal/store"
)

// sendLogPageSize bounds each send-log page (spec 7.2's monitoring screens
// call for pagination); logTailLines bounds how much of mail.log the log view
// shows per refresh.
const (
	sendLogPageSize = 50
	logTailLines    = 200
)

// handleSendLog renders the send-log monitoring page: server-side filters by
// domain/application and pagination (spec 7.3.3). The row table itself is the
// "sendlog_rows" fragment, shared verbatim with handleSendLogRows so the
// initial page and its HTMX-polled refreshes never diverge.
func (s *Server) handleSendLog(w http.ResponseWriter, r *http.Request) {
	data, err := s.sendLogData(r)
	if err != nil {
		logf("panel: send log: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data["Title"] = "SelfPost — send log"
	data["User"] = currentUser(r)
	s.render(w, http.StatusOK, "sendlog", data)
}

// handleSendLogRows serves the HTMX polling fragment for the send-log table
// (spec 7.1: fragment endpoints return HTML, not JSON).
func (s *Server) handleSendLogRows(w http.ResponseWriter, r *http.Request) {
	data, err := s.sendLogData(r)
	if err != nil {
		logf("panel: send log rows: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.renderFragment(w, http.StatusOK, "sendlog_rows", data)
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

// handleQueue renders the mail-queue monitoring page (spec 7.2.11).
func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	out, errText := readQueue()
	s.render(w, http.StatusOK, "queue", map[string]any{
		"Title":  "SelfPost — mail queue",
		"User":   currentUser(r),
		"Output": out,
		"Error":  errText,
	})
}

// handleQueueBody serves the HTMX polling fragment for the queue view.
func (s *Server) handleQueueBody(w http.ResponseWriter, r *http.Request) {
	out, errText := readQueue()
	s.renderFragment(w, http.StatusOK, "queue_body", map[string]any{
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

// handleLogTail renders the mail.log monitoring page (spec 7.2.13).
func (s *Server) handleLogTail(w http.ResponseWriter, r *http.Request) {
	lines, errText := s.readLogTail()
	s.render(w, http.StatusOK, "logtail", map[string]any{
		"Title": "SelfPost — mail log",
		"User":  currentUser(r),
		"Lines": lines,
		"Error": errText,
	})
}

// handleLogTailBody serves the HTMX polling fragment for the log-tail view.
func (s *Server) handleLogTailBody(w http.ResponseWriter, r *http.Request) {
	lines, errText := s.readLogTail()
	s.renderFragment(w, http.StatusOK, "logtail_body", map[string]any{
		"Lines": lines,
		"Error": errText,
	})
}

func (s *Server) readLogTail() ([]string, string) {
	lines, err := logtail.TailLines(s.cfg.MailLogPath, logTailLines)
	if err != nil {
		logf("panel: tail %s: %v", s.cfg.MailLogPath, err)
		return nil, "Could not read the mail log."
	}
	return lines, ""
}
