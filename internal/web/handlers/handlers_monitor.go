package handlers

import (
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/mixeme/selfpost/internal/logtail"
	"github.com/mixeme/selfpost/internal/mailhdr"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
)

// sendLogPageSize bounds each send-log page (product.md's monitoring screens
// call for pagination); logTailLines bounds how much of mail.log the log view
// shows per refresh, and deliveryLogLines how many of one message's own lines
// its page shows.
const (
	sendLogPageSize  = 50
	logTailLines     = 200
	deliveryLogLines = 200
)

// HandleDeliveries renders the Deliveries page over the send log: server-side
// filters by domain/application and pagination (architecture.md §
// Persistence). The row table itself is the "deliveries_rows" fragment, shared
// verbatim with HandleDeliveriesRows so the initial page and its HTMX-polled
// refreshes never diverge.
func (h *Handlers) HandleDeliveries(w http.ResponseWriter, r *http.Request) {
	data, err := h.sendLogData(r)
	if err != nil {
		logf("panel: send log: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data["Title"] = "SelfPost — deliveries"
	data["User"] = auth.CurrentUser(r)
	data["Active"] = "deliveries"
	h.view.Render(w, http.StatusOK, "deliveries", data)
}

// HandleDeliveriesRows serves the HTMX polling fragment for the delivery table
// (architecture.md § Panel HTTP surface: fragment endpoints return HTML, not
// JSON).
func (h *Handlers) HandleDeliveriesRows(w http.ResponseWriter, r *http.Request) {
	data, err := h.sendLogData(r)
	if err != nil {
		logf("panel: send log rows: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.view.RenderFragment(w, http.StatusOK, "deliveries_rows", data)
}

// HandleDelivery renders one send-log row in full. The log itself carries only
// what identifies a message at a glance — when, who to and from, what about,
// how it ended — and every remaining field (domain, application, queue id, when
// the status was last reported) lives here, one page per row, so widening the
// journal never costs the table a column.
//
// The page answers the question the log raises rather than restating it: what
// the journal recorded, in what order it happened, and what Postfix itself
// wrote about the message. So it is three blocks — the message's own facts and
// its history side by side, and the mail.log lines for its queue id under both.
// The queue id used to be printed here as something to go and search the system
// log for by hand; the search is done for the operator instead.
func (h *Handlers) HandleDelivery(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	row, err := h.store.GetSendLog(id)
	if err != nil {
		// A row pruned on the retention window is gone, not broken.
		if errors.Is(err, store.ErrSendLogNotFound) {
			http.NotFound(w, r)
			return
		}
		logf("panel: delivery %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	row.Subject = mailhdr.DecodeSubject(row.Subject)
	logRows, logNote := h.deliveryLog(row)
	h.view.Render(w, http.StatusOK, "delivery", map[string]any{
		"Title":  "SelfPost — delivery",
		"User":   auth.CurrentUser(r),
		"Active": "deliveries",
		"Row":    row,
		// The status in the panel's own badge vocabulary, so the headline reads
		// the same way as every other health signal in the panel.
		"Level":  deliveryLevel(row.Status),
		"Events": deliveryEvents(row),
		// The mail.log lines for this message, and — when there are none — the
		// reason, which is a normal outcome rather than a failure.
		"LogRows": logRows,
		"LogNote": logNote,
		// Where the row came from, so "Back" returns to the page and filters
		// the operator was looking at rather than the top of an unfiltered log.
		"BackURL": deliveriesBackURL(r),
	})
}

// deliveryLevel maps a send-log status onto the ok/warn/error/unknown badge
// vocabulary the status page and the DNS checks already use (see .st in
// panel.css), so a colour means the same thing on every page: delivered is the
// good outcome, deferred is not settled yet, and the two refusals are failures.
// A queued row is "unknown" rather than "warn" — nothing has gone wrong, it is
// simply that nothing has been reported.
func deliveryLevel(status string) string {
	switch status {
	case store.StatusSent:
		return "ok"
	case store.StatusDeferred:
		return "warn"
	case store.StatusBounced, store.StatusRejected:
		return "error"
	default:
		return "unknown"
	}
}

// deliveryEvent is one step of a message's history, as the timeline on the
// delivery page draws it. At is zero for the step that has not happened yet —
// the delivery report a queued message is still waiting for.
type deliveryEvent struct {
	At     time.Time
	Level  string // ok / warn / error / unknown, as deliveryLevel returns
	Status string // the send-log status value this step reached
	Title  string
	Detail string
}

// deliveryEvents turns a row's two timestamps into the history the page shows.
// The journal keeps no event table — a row is created when the message is
// accepted and updated once when Postfix reports the attempt — so the two
// timestamps *are* the history, and stating them as steps is what makes a row
// whose created_at and updated_at differ by six hours legible as "queued for
// six hours, then delivered" rather than as two dates in a list of fields.
func deliveryEvents(row store.SendLogRow) []deliveryEvent {
	// A rejected message has no second step, and its first one is not an
	// acceptance: the journal-milter refused it, so Postfix never queued it.
	if row.Status == store.StatusRejected {
		return []deliveryEvent{{
			At:     row.CreatedAt,
			Level:  "error",
			Status: store.StatusRejected,
			Title:  "Refused before queueing",
			Detail: "The journal-milter refused the message under a rate limit. It was never queued, so there is no queue id and Postfix never attempted delivery.",
		}}
	}

	events := []deliveryEvent{{
		At:     row.CreatedAt,
		Level:  "unknown",
		Status: store.StatusQueued,
		Title:  "Accepted and queued",
		Detail: "Postfix accepted the message over an authenticated submission and the journal-milter recorded it. Delivery to the recipient had not been attempted yet.",
	}}

	switch row.Status {
	case store.StatusQueued:
		// The step that has not happened. Drawn as an open dot with no time.
		return append(events, deliveryEvent{
			Level:  "unknown",
			Status: store.StatusQueued,
			Title:  "Waiting for a delivery report",
			Detail: "Postfix has not reported an attempt for this recipient yet. The Mail queue page shows what it is still holding.",
		})
	case store.StatusSent:
		return append(events, deliveryEvent{
			At:     row.UpdatedAt,
			Level:  "ok",
			Status: store.StatusSent,
			Title:  "Delivered",
			Detail: "The receiving server accepted the message. That is as far as this server can see — what the recipient's mailbox then did with it is not reported back.",
		})
	case store.StatusDeferred:
		return append(events, deliveryEvent{
			At:     row.UpdatedAt,
			Level:  "warn",
			Status: store.StatusDeferred,
			Title:  "Deferred, will be retried",
			Detail: "The receiving server could not take the message yet. Postfix keeps it queued and retries until it is delivered or the queue lifetime runs out.",
		})
	case store.StatusBounced:
		return append(events, deliveryEvent{
			At:     row.UpdatedAt,
			Level:  "error",
			Status: store.StatusBounced,
			Title:  "Bounced",
			Detail: "Delivery failed for good: the receiving server refused the message permanently, or Postfix gave up after the queue lifetime. The reason is in the delivery log below.",
		})
	default:
		// A status the log-tailer learns to write before this switch does.
		return append(events, deliveryEvent{
			At:     row.UpdatedAt,
			Level:  deliveryLevel(row.Status),
			Status: row.Status,
			Title:  "Status reported",
			Detail: "The last state Postfix reported for this recipient.",
		})
	}
}

// deliveryLogRow is one mail.log line split for the table on the delivery
// page: when it was written, and what it says. Time is empty for a line whose
// head is not a timestamp the log format recognises — the line still shows, in
// full, under Message.
type deliveryLogRow struct {
	Time string
	Text string
}

// deliveryLog reads the mail.log lines Postfix wrote about one message and
// splits each into the two columns the page shows it in. The second return
// value is what to say when there are none: every reason for an empty result
// here is an ordinary one — the message never reached the queue, or its lines
// have aged out of the log — so none of them is an error on the page. Only a
// log that cannot be read at all is reported as a fault, and that one is
// logged for the operator as well.
func (h *Handlers) deliveryLog(row store.SendLogRow) ([]deliveryLogRow, string) {
	if row.QueueID == "" {
		return nil, "This message never reached the queue, so Postfix wrote no delivery lines for it."
	}
	lines, err := logtail.QueueLines(h.cfg.MailLogPath, row.QueueID, deliveryLogLines)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		logf("panel: delivery log %s: %v", row.QueueID, err)
		return nil, "Could not read the mail log."
	}
	if len(lines) == 0 {
		// Send-log rows outlive mail.log: retention is ninety days by default
		// and rotation keeps fourteen files, so an older message having nothing
		// left to show is the normal end state, not a fault.
		return nil, "Nothing for this queue id in the current mail log. Its lines have most likely been rotated away."
	}
	out := make([]deliveryLogRow, len(lines))
	for i, line := range lines {
		stamp, rest := logtail.SplitTimestamp(line)
		out[i] = deliveryLogRow{Time: stamp, Text: rest}
	}
	return out, ""
}

// deliveriesBackURL rebuilds the delivery-log URL a detail page was opened
// from. Only the log's own parameters are carried over, and each is re-encoded
// by url.Values, so nothing a visitor appends to the link can travel back into
// the page as markup or as a different destination.
func deliveriesBackURL(r *http.Request) string {
	q := r.URL.Query()
	back := url.Values{}
	for _, k := range []string{"domain", "app", "p"} {
		if v := q.Get(k); v != "" {
			back.Set(k, v)
		}
	}
	if len(back) == 0 {
		return "/deliveries"
	}
	return "/deliveries?" + back.Encode()
}

// sendLogData reads the domain/app filters and page number off the query
// string, queries the store, and assembles everything the template needs
// (filter dropdown options plus the current selection, rows, and pagination).
func (h *Handlers) sendLogData(r *http.Request) (map[string]any, error) {
	q := r.URL.Query()
	filter := store.SendLogFilter{
		Domain:   q.Get("domain"),
		AppLogin: q.Get("app"),
	}
	page := parsePage(q.Get("p"))

	total, err := h.store.CountSendLog(filter)
	if err != nil {
		return nil, err
	}
	rows, err := h.store.QuerySendLog(filter, sendLogPageSize, (page-1)*sendLogPageSize)
	if err != nil {
		return nil, err
	}
	// Decode on the way out as well as on the way in: rows the journal-milter
	// wrote before it decoded subjects itself still hold the raw header, and
	// they are the ones an operator is most likely to be looking at.
	for i := range rows {
		rows[i].Subject = mailhdr.DecodeSubject(rows[i].Subject)
	}
	domains, err := h.store.ListDomains()
	if err != nil {
		return nil, err
	}
	domainNames := make([]string, len(domains))
	for i, d := range domains {
		domainNames[i] = d.Name
	}
	logins, err := h.store.ListApplicationLogins()
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

// HandleMailQueue renders the Mail queue page (architecture.md § Panel HTTP
// surface).
func (h *Handlers) HandleMailQueue(w http.ResponseWriter, r *http.Request) {
	out, errText := readQueue()
	h.view.Render(w, http.StatusOK, "mail_queue", map[string]any{
		"Title":  "SelfPost — mail queue",
		"User":   auth.CurrentUser(r),
		"Active": "mail_queue",
		"Output": out,
		"Error":  errText,
	})
}

// HandleMailQueueBody serves the HTMX polling fragment for the queue view.
func (h *Handlers) HandleMailQueueBody(w http.ResponseWriter, r *http.Request) {
	out, errText := readQueue()
	h.view.RenderFragment(w, http.StatusOK, "mail_queue_body", map[string]any{
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

// HandleSystemLog renders the System log page over mail.log (architecture.md §
// Panel HTTP surface).
func (h *Handlers) HandleSystemLog(w http.ResponseWriter, r *http.Request) {
	lines, errText := h.readLogTail()
	h.view.Render(w, http.StatusOK, "system_log", map[string]any{
		"Title":  "SelfPost — system log",
		"User":   auth.CurrentUser(r),
		"Active": "system_log",
		"Lines":  lines,
		"Error":  errText,
	})
}

// HandleSystemLogBody serves the HTMX polling fragment for the log-tail view.
func (h *Handlers) HandleSystemLogBody(w http.ResponseWriter, r *http.Request) {
	lines, errText := h.readLogTail()
	h.view.RenderFragment(w, http.StatusOK, "system_log_body", map[string]any{
		"Lines": lines,
		"Error": errText,
	})
}

func (h *Handlers) readLogTail() ([]string, string) {
	lines, err := logtail.TailLines(h.cfg.MailLogPath, logTailLines)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Rotation renamed the file away; Postfix recreates it on reload
			// (within about a second), so this is a normal, brief gap rather
			// than a failure worth alarming the operator about.
			return nil, ""
		}
		logf("panel: tail %s: %v", h.cfg.MailLogPath, err)
		return nil, "Could not read the mail log."
	}
	return lines, ""
}
