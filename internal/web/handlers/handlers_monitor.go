package handlers

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/mixeme/selfpost/internal/logtail"
	"github.com/mixeme/selfpost/internal/mailhdr"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/view"
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

// HandleDeliveries renders the Outbound log over the send log: server-side
// filters by domain/application and pagination (architecture.md § Persistence).
// The table and the count under it are the polled region, rendered by the same
// template for the page and for HandleDeliveriesRows, so the initial page and
// its HTMX-polled refreshes never diverge.
func (h *Handlers) HandleDeliveries(w http.ResponseWriter, r *http.Request) {
	page, err := h.outLogPage(r)
	if err != nil {
		logf("panel: send log: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.view.Render(w, http.StatusOK, "out-log", page)
}

// HandleDeliveriesRows serves the HTMX polling fragment for the log
// (architecture.md § Panel HTTP surface: fragment endpoints return HTML, not
// JSON): the table, and the foot under it for the page to swap in.
func (h *Handlers) HandleDeliveriesRows(w http.ResponseWriter, r *http.Request) {
	page, err := h.outLogPage(r)
	if err != nil {
		logf("panel: send log rows: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.view.RenderFragment(w, http.StatusOK, "out_log_rows", page.Fragment())
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
	p, ok := h.principal(r)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	allowed, err := h.assignedDomains(p)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !p.IsGlobal() && !domainNameSet(allowed)[row.Domain] {
		http.NotFound(w, r)
		return
	}
	row.Subject = mailhdr.DecodeSubject(row.Subject)
	logLines, logNote := h.deliveryLog(row)

	in := view.MessageInput{
		ID: row.ID, QueueID: row.QueueID, Domain: row.Domain, App: row.AppLogin,
		From: row.From, To: row.To, Subject: row.Subject, Status: row.Status,
		Accepted: row.CreatedAt, Reported: row.UpdatedAt,
		// Where the row came from, so the way back returns to the page and filters
		// the operator was looking at rather than the top of an unfiltered log.
		BackHref: deliveriesBackURL(r),
	}
	// The names link to their pages when there is a page to go to: a domain
	// that was deleted since, or an application of another domain, is text.
	for _, d := range allowed {
		if d.Name != row.Domain {
			continue
		}
		in.DomainHref = view.DomainHref(d.ID)
		if a, err := h.store.GetApplicationByLogin(row.AppLogin); err == nil && a.DomainID == d.ID {
			in.AppHref = view.DomainHref(d.ID) + "/applications/" + strconv.FormatInt(a.ID, 10)
		}
		break
	}
	page := view.NewOutMessage(h.shellMeta(r), in).
		WithHistory(deliveryEvents(row, h.cfg.RetryPolicy)).
		// The mail.log lines for this message, and — when there are none — the
		// reason, which is a normal outcome rather than a failure.
		WithDeliveryLog(logLines, logNote)
	h.view.Render(w, http.StatusOK, "out-message", page)
}

// deliveryEvents turns a row's two timestamps into the history the page shows.
// The journal keeps no event table — a row is created when the message is
// accepted and updated once when Postfix reports the attempt — so the two
// timestamps *are* the history, and stating them as steps is what makes a row
// whose created_at and updated_at differ by six hours legible as "queued for
// six hours, then delivered" rather than as two dates in a list of fields.
// policy supplies the human intervals for deferred and bounced copy, the same
// strings the Mail queue card prints, so the two cannot drift.
func deliveryEvents(row store.SendLogRow, policy postfix.RetryPolicy) []view.Step {
	// step states what happened and when; outcome is ok, warn, error or "" for a
	// step that reports nothing either way, and a zero time is a step that has
	// not happened yet.
	step := func(at time.Time, outcome, title, detail string) view.Step {
		return view.Step{
			Time:   view.StepTime(at),
			Strong: title,
			Text:   view.Plain(" — " + detail),
			Level:  view.StepLevel(outcome, !at.IsZero()),
		}
	}

	// A rejected message has no second step, and its first one is not an
	// acceptance: the journal-milter refused it, so Postfix never queued it.
	if row.Status == store.StatusRejected {
		return []view.Step{step(row.CreatedAt, "error", "Refused before queueing",
			"The journal-milter refused the message under a rate limit. It was never queued, so there is no queue id and Postfix never attempted delivery.")}
	}

	events := []view.Step{step(row.CreatedAt, "", "Accepted and queued",
		"Postfix accepted the message over an authenticated submission and the journal-milter recorded it. Delivery to the recipient had not been attempted yet.")}

	switch row.Status {
	case store.StatusQueued:
		// The step that has not happened. Drawn as an open dot with no time.
		return append(events, step(time.Time{}, "", "Waiting for a delivery report",
			"Postfix has not reported an attempt for this recipient yet. The Mail queue page shows what it is still holding."))
	case store.StatusSent:
		return append(events, step(row.UpdatedAt, "ok", "Delivered",
			"The receiving server accepted the message. That is as far as this server can see — what the recipient's mailbox then did with it is not reported back."))
	case store.StatusDeferred:
		return append(events, step(row.UpdatedAt, "warn", "Deferred, will be retried",
			fmt.Sprintf("The receiving server could not take the message yet. Postfix retries: first after %s, then with increasing gaps up to %s, for up to %s. There is no fixed attempt count — a deferred message stays in the queue until it is delivered or that lifetime runs out.",
				policy.FirstRetry(), policy.BackoffCap(), policy.QueueLifetime())))
	case store.StatusBounced:
		return append(events, step(row.UpdatedAt, "error", "Bounced",
			fmt.Sprintf("Delivery failed for good: the receiving server refused the message permanently, or Postfix gave up after %s in the queue. The reason is in the delivery log below.",
				policy.QueueLifetime())))
	default:
		// A status the log-tailer learns to write before this switch does.
		return append(events, step(row.UpdatedAt, "", "Status reported",
			"The last state Postfix reported for this recipient."))
	}
}

// deliveryLog reads the mail.log lines Postfix wrote about one message and
// splits each into the time and the text the page shows it in. The second
// return value is what to say when there are none: every reason for an empty
// result here is an ordinary one — the message never reached the queue, or its
// lines have aged out of the log — so none of them is an error on the page. Only
// a log that cannot be read at all is reported as a fault, and that one is
// logged for the operator as well.
func (h *Handlers) deliveryLog(row store.SendLogRow) ([]view.LogLine, string) {
	if row.QueueID == "" {
		return nil, "This message never reached the queue, so Postfix wrote no delivery lines for it."
	}
	lines, err := logtail.QueueLines(h.cfg.MailLogPath, row.QueueID, deliveryLogLines)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		logf("panel: delivery log %s: %v", row.QueueID, err)
		return nil, "Could not read the mail log."
	}
	if len(lines) == 0 {
		days := h.sendLogRetentionDays()
		return nil, fmt.Sprintf("Nothing for this queue id in the current mail log. Its lines have most likely been rotated away (send-log rows are kept for %d days).", days)
	}
	out := make([]view.LogLine, len(lines))
	for i, line := range lines {
		// A line whose head is not a timestamp the log format recognises keeps its
		// whole text and no time: nothing is dropped from what the log says.
		stamp, rest := logtail.SplitTimestamp(line)
		out[i] = view.LogLine{Time: stamp, Text: rest, Level: view.LogLineLevel(rest)}
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
		return "/outbound/log"
	}
	return "/outbound/log?" + back.Encode()
}

// outLogPage reads the domain/app filters and page number off the query
// string, queries the store, and fills the log page (filter choices plus the
// current selection, rows, and pagination).
//
// The invariant this function owes the journal: a principal who is not global
// only ever reads rows for the domains assigned to them. That scope is stated
// to the store as SendLogFilter.Domains and holds for every number of
// assignments, including none — a domain administrator whose last domain was
// deleted gets an empty log, not the whole one. The query parameters are
// filters *within* that scope and can only narrow it: both are checked against
// the assigned domains and their applications before the query runs, because a
// dropdown that offers only permitted values is a courtesy to the browser, not
// a check on the request.
func (h *Handlers) outLogPage(r *http.Request) (*view.OutLog, error) {
	p, ok := h.principal(r)
	if !ok {
		return nil, errors.New("no principal")
	}
	q := r.URL.Query()

	assigned, err := h.assignedDomains(p)
	if err != nil {
		return nil, err
	}
	allowedNames := domainNameSet(assigned)

	domainNames := make([]string, 0, len(assigned))
	for _, d := range assigned {
		domainNames = append(domainNames, d.Name)
	}

	loginSet := make(map[string]bool)
	for _, d := range assigned {
		apps, err := h.store.ListApplicationsByDomain(d.ID)
		if err != nil {
			return nil, err
		}
		for _, a := range apps {
			loginSet[a.Login] = true
		}
	}
	logins := make([]string, 0, len(loginSet))
	for login := range loginSet {
		logins = append(logins, login)
	}
	sort.Strings(logins)

	filter := store.SendLogFilter{
		Domain:   q.Get("domain"),
		AppLogin: q.Get("app"),
		// A global administrator reads the whole journal, including rows left
		// behind by a domain that has since been deleted.
		Domains:    domainNames,
		AllDomains: p.IsGlobal(),
	}
	if !p.IsGlobal() {
		if filter.Domain != "" && !allowedNames[filter.Domain] {
			filter.Domain = ""
		}
		if filter.AppLogin != "" && !loginSet[filter.AppLogin] {
			filter.AppLogin = ""
		}
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

	lastPage := 1
	if total > 0 {
		lastPage = int((total + sendLogPageSize - 1) / sendLogPageSize)
	}
	out := view.NewOutLog(h.shellMeta(r), h.sendLogRetentionDays(), p.IsGlobal(), domainNames, logins, filter.Domain, filter.AppLogin)
	table := make([]view.OutLogRow, len(rows))
	for i, row := range rows {
		table[i] = view.OutLogRow{
			Time: view.FormatLogTime(row.CreatedAt), From: row.From, To: row.To,
			Subject: mailhdr.DecodeSubject(row.Subject), Status: view.Tag{Status: row.Status},
			Href: out.DetailHref(row.ID, page),
		}
	}
	return out.WithRows(table, page, lastPage), nil
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

// HandleMailQueue renders the Outbound queue page (architecture.md § Panel HTTP
// surface).
func (h *Handlers) HandleMailQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	policy := h.cfg.RetryPolicy
	h.view.Render(w, http.StatusOK, "out-queue", h.outQueuePage(r).
		WithRetryPolicy(policy.FirstRetry(), policy.BackoffCap(), policy.QueueLifetime(), policy.FromDefaults))
}

// HandleMailQueueBody serves the HTMX polling fragment for the queue view.
func (h *Handlers) HandleMailQueueBody(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	h.view.RenderFragment(w, http.StatusOK, "out_queue_body", h.outQueuePage(r))
}

// outQueuePage reads the queue and gives it to the view; the page and its
// fragment draw the same box from it.
func (h *Handlers) outQueuePage(r *http.Request) *view.OutQueue {
	out, errText := readQueue()
	return view.NewOutQueue(h.shellMeta(r), out, errText)
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
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	h.view.Render(w, http.StatusOK, "system-log", h.systemLogPage(r))
}

// HandleSystemLogBody serves the HTMX polling fragment for the log-tail view.
func (h *Handlers) HandleSystemLogBody(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	h.view.RenderFragment(w, http.StatusOK, "system_log_body", h.systemLogPage(r))
}

// systemLogPage reads the tail of mail.log and splits each line into the time
// and the text the page shows it in, as the delivery log of one message does. A
// line whose head is not a timestamp the log format recognises keeps its whole
// text and no time: nothing is dropped from what the log says.
func (h *Handlers) systemLogPage(r *http.Request) *view.SystemLog {
	raw, errText := h.readLogTail()
	lines := make([]view.LogLine, len(raw))
	for i, line := range raw {
		stamp, rest := logtail.SplitTimestamp(line)
		lines[i] = view.LogLine{Time: stamp, Text: rest, Level: view.LogLineLevel(rest)}
	}
	return view.NewSystemLog(h.shellMeta(r), lines, errText)
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
