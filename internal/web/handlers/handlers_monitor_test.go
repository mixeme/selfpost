package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
)

// After log rotation renames mail.log away, Postfix takes about a second to
// recreate it on reload (spec B.2); a missing file in that window is a normal,
// transient gap, not an operator-facing failure.
func TestReadLogTailMissingFileIsNotAnError(t *testing.T) {
	h := &Handlers{cfg: Config{MailLogPath: filepath.Join(t.TempDir(), "mail.log")}}

	lines, errText := h.readLogTail()
	if lines != nil {
		t.Errorf("lines = %v, want nil", lines)
	}
	if errText != "" {
		t.Errorf("errText = %q, want empty (missing file is not an error)", errText)
	}
}

// The delivery log is a list of messages, not a dump of the journal: it shows
// when, from, to, subject and status, and links each row to the page carrying
// the rest. A column added back here is one the table has no width for.
func TestDeliveryLogShowsOnlyTheIdentifyingColumns(t *testing.T) {
	h, row := serverWithDelivery(t)

	out := getBody(t, h.HandleDeliveries, "/deliveries")
	for _, want := range []string{
		row.CreatedAt.Format("2006-01-02 15:04:05"),
		"noreply@bs.example.ru", "public@example.ru",
		"Проверка", ">sent<", `href="/deliveries/` + itoa(row.ID),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("delivery log is missing %q:\n%s", want, out)
		}
	}
	// Domain and application stay available as filters; what the table must not
	// carry is a column of them per row.
	for _, unwanted := range []string{"<th>Domain</th>", "<th>App</th>", "Queuer3C"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("delivery log still shows %q; that detail belongs on the delivery page", unwanted)
		}
	}
}

// Subjects reached the journal as RFC 2047 encoded-words before the milter
// decoded them, and those rows are still in the send log. Decoding on the way
// out is what keeps them readable, so the encoding must not survive to the page.
func TestDeliveryLogDecodesStoredEncodedSubjects(t *testing.T) {
	h, _ := serverWithDelivery(t)

	for name, out := range map[string]string{
		"log":  getBody(t, h.HandleDeliveries, "/deliveries"),
		"rows": getBody(t, h.HandleDeliveriesRows, "/deliveries/rows"),
	} {
		if strings.Contains(out, "=?utf-8?Q?") {
			t.Errorf("%s shows the subject's MIME encoding instead of its text:\n%s", name, out)
		}
		if !strings.Contains(out, "Проверка") {
			t.Errorf("%s does not show the decoded subject:\n%s", name, out)
		}
	}
}

// Everything the log dropped has to be somewhere, and that somewhere is the
// per-row page — including for a row still holding an encoded subject.
func TestDeliveryPageShowsWhatTheLogOmits(t *testing.T) {
	h, row := serverWithDelivery(t)

	out := getBody(t, h.HandleDelivery, "/deliveries/"+itoa(row.ID)+"?domain=bs.example.ru&p=2")
	for _, want := range []string{
		"bs.example.ru", "Queuer3C", "4A1B2C3D", "Проверка",
		"noreply@bs.example.ru", "public@example.ru", "sent",
		`href="/deliveries?domain=bs.example.ru&amp;p=2"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("delivery page is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "=?utf-8?Q?") {
		t.Errorf("delivery page shows the subject's MIME encoding instead of its text:\n%s", out)
	}
}

// The page's second column is the message's history: the two timestamps the
// journal holds, stated as the steps they stand for, so a row is readable as
// what happened to the message rather than as a list of fields.
func TestDeliveryPageTellsTheMessagesHistory(t *testing.T) {
	h, row := serverWithDelivery(t)

	out := getBody(t, h.HandleDelivery, "/deliveries/"+itoa(row.ID))
	for _, want := range []string{
		"Accepted and queued", "Delivered",
		row.CreatedAt.Format("2006-01-02 15:04:05"),
		row.UpdatedAt.Format("2006-01-02 15:04:05"),
		// A delivered message is "ok" in the panel's own badge vocabulary, the
		// same one the status page and the DNS checks use.
		`class="st st-ok"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("delivery page is missing %q:\n%s", want, out)
		}
	}
	// Accepted comes before delivered: a history read in the wrong order is
	// worse than none.
	if strings.Index(out, "Accepted and queued") > strings.Index(out, "Delivered") {
		t.Errorf("the history is not in the order it happened:\n%s", out)
	}
}

// A queued message has no second timestamp to state, so the step it is waiting
// for is drawn as one that has not happened rather than dated with the moment
// the row was written.
func TestDeliveryPageMarksAQueuedMessageAsStillWaiting(t *testing.T) {
	h, _ := serverWithDelivery(t)
	if err := h.store.InsertQueued(store.SendLogEntry{
		QueueID: "7F7F7F7F", Domain: "bs.example.ru", AppLogin: "Queuer3C",
		From: "noreply@bs.example.ru", To: "waiting@example.ru", Subject: "Still going",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rows, err := h.store.QuerySendLog(store.SendLogFilter{AllDomains: true}, 1, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("query: %v (%d rows)", err, len(rows))
	}

	out := getBody(t, h.HandleDelivery, "/deliveries/"+itoa(rows[0].ID))
	for _, want := range []string{"Waiting for a delivery report", "pending", "not yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("delivery page does not mark the message as still waiting (%q):\n%s", want, out)
		}
	}
}

// The queue id used to be printed as something to go and search the system log
// for by hand; the page does that search now, and shows only this message's
// lines — as a table of when and what, so the seconds between the connection
// and the reply line up down one edge.
func TestDeliveryPageShowsThisMessagesLogLines(t *testing.T) {
	h, row := serverWithDelivery(t)
	h.cfg.MailLogPath = writeMailLog(t,
		"2026-08-03T05:15:52.219218+00:00 host postfix/smtpd[20]: 4A1B2C3D: client=mail.example.com[203.0.113.4]",
		"2026-08-03T05:15:52.300000+00:00 host postfix/qmgr[10]: 99999999: from=<other@example.ru>, size=500, nrcpt=1 (queue active)",
		"2026-08-03T05:16:03.884210+00:00 host postfix/smtp[26]: 4A1B2C3D: to=<public@example.ru>, dsn=2.0.0, status=sent (250 OK)",
	)

	out := getBody(t, h.HandleDelivery, "/deliveries/"+itoa(row.ID))
	for _, want := range []string{
		"<th>Time</th>", "<th>Message</th>",
		// The stamp is split off into its own cell, without the microseconds
		// and the offset that make it the widest thing on the line.
		`<td class="time muted">2026-08-03 05:15:52</td>`,
		`<td class="time muted">2026-08-03 05:16:03</td>`,
		"client=mail.example.com", "status=sent (250 OK)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("delivery log table is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "99999999") {
		t.Errorf("delivery page shows another message's log line:\n%s", out)
	}
}

// A line whose head is not a timestamp still has to show in full; the format is
// the log's, not ours, and a line we cannot split is a line we must not drop.
func TestDeliveryPageKeepsAnUnstampedLogLineWhole(t *testing.T) {
	h, row := serverWithDelivery(t)
	h.cfg.MailLogPath = writeMailLog(t, "host postfix/smtp[26]: 4A1B2C3D: to=<public@example.ru>, status=sent (250 OK)")

	out := getBody(t, h.HandleDelivery, "/deliveries/"+itoa(row.ID))
	if !strings.Contains(out, "host postfix/smtp[26]: 4A1B2C3D: to=&lt;public@example.ru&gt;, status=sent (250 OK)") {
		t.Errorf("an unstamped log line did not survive the split into columns:\n%s", out)
	}
}

// Rows outlive mail.log, and a message the milter refused never reached the
// queue at all. Neither is a fault, so neither may render as an error.
func TestDeliveryPageExplainsAnEmptyDeliveryLog(t *testing.T) {
	h, row := serverWithDelivery(t)
	h.cfg.MailLogPath = filepath.Join(t.TempDir(), "mail.log") // never created

	out := getBody(t, h.HandleDelivery, "/deliveries/"+itoa(row.ID))
	if !strings.Contains(out, "rotated away") {
		t.Errorf("delivery page does not explain the empty delivery log:\n%s", out)
	}
	if strings.Contains(out, `class="error"`) || strings.Contains(out, "Could not read the mail log") {
		t.Errorf("an aged-out delivery log is reported as a failure:\n%s", out)
	}
}

// Send-log rows are pruned on the retention window, so a bookmarked delivery
// that no longer exists is a 404, not a 500.
func TestDeliveryPageNotFound(t *testing.T) {
	h, _ := serverWithDelivery(t)

	for _, path := range []string{"/deliveries/999999", "/deliveries/abc", "/deliveries/0"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.SetPathValue("id", strings.TrimPrefix(path, "/deliveries/"))
		h.HandleDelivery(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
}

// A domain administrator reads the journal of the domains assigned to them and
// nothing else. The list used to be scoped only when exactly one domain was
// assigned, which meant two assignments read as none at all.
func TestSendLogScopedToAssignedDomains(t *testing.T) {
	h, domains := serverWithTwoDomains(t)

	for name, tc := range map[string]struct {
		username  string
		domainIDs []int64
		want      []string
		unwanted  []string
	}{
		"global sees both": {
			"", nil, []string{"First message", "Second message"}, nil,
		},
		"one assigned domain": {
			"one-domain", []int64{domains["first.example.ru"].ID},
			[]string{"First message"}, []string{"Second message", "second-app"},
		},
		"two assigned domains": {
			"two-domains", []int64{domains["first.example.ru"].ID, domains["second.example.ru"].ID},
			[]string{"First message", "Second message"}, nil,
		},
		// Every assigned domain deleted cascades the assignments away. That
		// leaves a principal entitled to nothing, which is an empty log — the
		// case that used to hand over the whole journal.
		"no assigned domains": {
			"no-domains", nil, []string{"No messages logged yet."},
			[]string{"First message", "Second message"},
		},
	} {
		var p auth.Principal
		if tc.username == "" {
			p = globalPrincipal
		} else {
			p = domainAdmin(t, h.store, tc.username, tc.domainIDs...)
		}
		for view, handler := range map[string]http.HandlerFunc{
			"page":     h.HandleDeliveries,
			"fragment": h.HandleDeliveriesRows,
		} {
			out := getBodyAs(t, handler, "/deliveries", p)
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("%s (%s): missing %q:\n%s", name, view, want, out)
				}
			}
			for _, unwanted := range tc.unwanted {
				if strings.Contains(out, unwanted) {
					t.Errorf("%s (%s): leaks %q:\n%s", name, view, unwanted, out)
				}
			}
		}
	}
}

// The filter dropdowns offer only permitted values, so a leak through them can
// only come from a hand-written URL — which is exactly why the values are
// checked against the principal's own domains and applications rather than
// trusted for having been rendered by us.
func TestSendLogIgnoresForgedFilters(t *testing.T) {
	h, domains := serverWithTwoDomains(t)
	p := domainAdmin(t, h.store, "forged-filter", domains["first.example.ru"].ID)

	for _, target := range []string{
		"/deliveries?domain=second.example.ru",
		"/deliveries?app=second-app",
		"/deliveries?domain=second.example.ru&app=second-app",
	} {
		out := getBodyAs(t, h.HandleDeliveries, target, p)
		if strings.Contains(out, "Second message") {
			t.Errorf("GET %s leaks another domain's journal:\n%s", target, out)
		}
		if !strings.Contains(out, "First message") {
			t.Errorf("GET %s hid the principal's own journal:\n%s", target, out)
		}
	}
}

// The detail page has always checked membership; keep it checked, because the
// list and the page are two ways to the same row.
func TestDeliveryPageForeignDomainNotFound(t *testing.T) {
	h, domains := serverWithTwoDomains(t)
	rows, err := h.store.QuerySendLog(store.SendLogFilter{Domain: "second.example.ru", AllDomains: true}, 1, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("query: %v (%d rows)", err, len(rows))
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/deliveries/"+itoa(rows[0].ID), nil)
	req.SetPathValue("id", itoa(rows[0].ID))
	req = auth.RequestWithPrincipal(req, domainAdmin(t, h.store, "foreign-detail", domains["first.example.ru"].ID))
	h.HandleDelivery(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("delivery page for a foreign domain = %d, want 404", rec.Code)
	}
}

// serverWithTwoDomains builds a panel over a store holding two domains, one
// application and one delivered message each, so a scoping test can tell "my
// rows" from "every row" by reading the page.
func serverWithTwoDomains(t *testing.T) (*Handlers, map[string]store.Domain) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	domains := make(map[string]store.Domain, 2)
	for _, d := range []struct{ name, app, subject string }{
		{"first.example.ru", "first-app", "First message"},
		{"second.example.ru", "second-app", "Second message"},
	} {
		dom, err := st.AddDomain(d.name, "mail")
		if err != nil {
			t.Fatalf("add domain %s: %v", d.name, err)
		}
		if _, err := st.AddApplication(dom.ID, d.app, store.AddressModeWildcard, nil); err != nil {
			t.Fatalf("add application %s: %v", d.app, err)
		}
		if err := st.InsertQueued(store.SendLogEntry{
			QueueID: "Q" + d.app, Domain: d.name, AppLogin: d.app,
			From: "noreply@" + d.name, To: "public@example.net", Subject: d.subject,
		}); err != nil {
			t.Fatalf("insert %s: %v", d.subject, err)
		}
		domains[d.name] = dom
	}

	return &Handlers{store: st, view: mustView(t), cfg: Config{Version: "test"}}, domains
}

var globalPrincipal = auth.Principal{ID: 1, Username: "admin", Role: auth.RoleGlobal}

func domainAdmin(t *testing.T, st *store.Store, username string, domainIDs ...int64) auth.Principal {
	t.Helper()
	const hash = "test-hash"
	if len(domainIDs) == 0 {
		placeholder, err := st.AddDomain(username+".placeholder.invalid", "mail")
		if err != nil {
			t.Fatalf("add placeholder domain: %v", err)
		}
		id, err := st.CreateUser(username, hash, store.RoleDomainAdmin, []int64{placeholder.ID})
		if err != nil {
			t.Fatalf("create domain admin %s: %v", username, err)
		}
		if err := st.DeleteDomain(placeholder.ID); err != nil {
			t.Fatalf("delete placeholder domain: %v", err)
		}
		domainIDs = nil
		u, err := st.GetUser(id)
		if err != nil {
			t.Fatalf("get domain admin %s: %v", username, err)
		}
		return auth.Principal{ID: u.ID, Username: u.Username, Role: u.Role, Domains: u.DomainIDs}
	}
	id, err := st.CreateUser(username, hash, store.RoleDomainAdmin, domainIDs)
	if err != nil {
		t.Fatalf("create domain admin %s: %v", username, err)
	}
	u, err := st.GetUser(id)
	if err != nil {
		t.Fatalf("get domain admin %s: %v", username, err)
	}
	return auth.Principal{ID: u.ID, Username: u.Username, Role: u.Role, Domains: u.DomainIDs}
}

// serverWithDelivery builds a panel over a store holding one delivery, written
// the way the journal-milter wrote them before it decoded subjects itself.
func serverWithDelivery(t *testing.T) (*Handlers, store.SendLogRow) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if err := st.InsertQueued(store.SendLogEntry{
		QueueID:  "4A1B2C3D",
		Domain:   "bs.example.ru",
		AppLogin: "Queuer3C",
		From:     "noreply@bs.example.ru",
		To:       "public@example.ru",
		Subject:  "=?utf-8?Q?=D0=9F=D1=80=D0=BE=D0=B2=D0=B5=D1=80=D0=BA=D0=B0?=",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := st.UpdateStatus("4A1B2C3D", "public@example.ru", store.StatusSent); err != nil {
		t.Fatalf("update status: %v", err)
	}
	rows, err := st.QuerySendLog(store.SendLogFilter{AllDomains: true}, 1, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("query: %v (%d rows)", err, len(rows))
	}

	return &Handlers{store: st, view: mustView(t), cfg: Config{Version: "test"}}, rows[0]
}

// getBody runs one handler over a GET as the global administrator.
func getBody(t *testing.T, h http.HandlerFunc, target string) string {
	t.Helper()
	return getBodyAs(t, h, target, globalPrincipal)
}

// getBodyAs runs one handler over a GET as the given principal and returns the
// page it wrote, failing the test on any non-200. The path's {id} is bound by
// hand because these calls bypass the router that would otherwise fill it in.
func getBodyAs(t *testing.T, h http.HandlerFunc, target string, p auth.Principal) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = auth.RequestWithPrincipal(req, p)
	if rest, ok := strings.CutPrefix(req.URL.Path, "/deliveries/"); ok && rest != "rows" {
		req.SetPathValue("id", rest)
	}
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200:\n%s", target, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// writeMailLog creates a mail.log holding the given lines and returns its path,
// for the pages that read the log rather than the journal.
func writeMailLog(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mail.log")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write mail.log: %v", err)
	}
	return path
}

// fixtureRetryPolicy is a distinctive policy so tests can tell the Config
// snapshot from live postconf and from compiled-in defaults (5 minutes / 5 days).
func fixtureRetryPolicy() postfix.RetryPolicy {
	return postfix.RetryPolicy{
		QueueRunDelay:        10 * time.Minute,
		MinimalBackoff:       10 * time.Minute,
		MaximalBackoff:       4000 * time.Second,
		MaximalQueueLifetime: 2 * 24 * time.Hour,
		BounceQueueLifetime:  2 * 24 * time.Hour,
	}
}

// The retry card sits on the page itself, outside the HTMX poll, and prints
// whatever policy was cached on Config — never a live postconf.
func TestMailQueueShowsRetryPolicyCard(t *testing.T) {
	h := &Handlers{view: mustView(t), cfg: Config{Version: "test", RetryPolicy: fixtureRetryPolicy()}}

	out := getBody(t, h.HandleMailQueue, "/mail-queue")
	for _, want := range []string{
		"How delivery retries work",
		"id=\"retry-policy\"",
		">10 minutes<",
		"doubling, cap about 1 hour 7 minutes",
		">2 days<",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("mail queue is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, ">5 minutes<") || strings.Contains(out, ">5 days<") {
		t.Errorf("mail queue shows stock defaults instead of the fixture:\n%s", out)
	}
	if strings.Contains(out, "compiled-in defaults") {
		t.Error("a fixture policy must not show the fallback note")
	}
}

func TestMailQueueBodyOmitsRetryPolicyCard(t *testing.T) {
	h := &Handlers{view: mustView(t), cfg: Config{RetryPolicy: fixtureRetryPolicy()}}

	out := getBody(t, h.HandleMailQueueBody, "/mail-queue/body")
	if strings.Contains(out, "How delivery retries work") || strings.Contains(out, "10 minutes") {
		t.Errorf("HTMX fragment includes the retry card:\n%s", out)
	}
}

func TestMailQueueNotesCompiledInFallback(t *testing.T) {
	h := &Handlers{view: mustView(t), cfg: Config{RetryPolicy: postfix.DefaultRetryPolicy()}}

	out := getBody(t, h.HandleMailQueue, "/mail-queue")
	if !strings.Contains(out, "compiled-in defaults") {
		t.Errorf("fallback note missing:\n%s", out)
	}
}

func TestDeliveryPageDeferredUsesRetryPolicy(t *testing.T) {
	h, row := serverWithDelivery(t)
	h.cfg.RetryPolicy = fixtureRetryPolicy()
	if _, err := h.store.UpdateStatus(row.QueueID, row.To, store.StatusDeferred); err != nil {
		t.Fatalf("update status: %v", err)
	}

	out := getBody(t, h.HandleDelivery, "/deliveries/"+itoa(row.ID))
	for _, want := range []string{
		"first after 10 minutes",
		"up to about 1 hour 7 minutes",
		"for up to 2 days",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("deferred history is missing %q:\n%s", want, out)
		}
	}
}

func TestDeliveryPageBouncedUsesRetryPolicy(t *testing.T) {
	h, row := serverWithDelivery(t)
	h.cfg.RetryPolicy = fixtureRetryPolicy()
	if _, err := h.store.UpdateStatus(row.QueueID, row.To, store.StatusBounced); err != nil {
		t.Fatalf("update status: %v", err)
	}

	out := getBody(t, h.HandleDelivery, "/deliveries/"+itoa(row.ID))
	if !strings.Contains(out, "gave up after 2 days in the queue") {
		t.Errorf("bounced history does not use the fixture lifetime:\n%s", out)
	}
}
