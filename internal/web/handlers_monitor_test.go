package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/store"
)

// After log rotation renames mail.log away, Postfix takes about a second to
// recreate it on reload (spec B.2); a missing file in that window is a normal,
// transient gap, not an operator-facing failure.
func TestReadLogTailMissingFileIsNotAnError(t *testing.T) {
	s := &Server{cfg: Config{MailLogPath: filepath.Join(t.TempDir(), "mail.log")}}

	lines, errText := s.readLogTail()
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
	s, row := serverWithDelivery(t)

	out := getBody(t, s.handleDeliveries, "/deliveries")
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
	s, _ := serverWithDelivery(t)

	for name, out := range map[string]string{
		"log":  getBody(t, s.handleDeliveries, "/deliveries"),
		"rows": getBody(t, s.handleDeliveriesRows, "/deliveries/rows"),
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
	s, row := serverWithDelivery(t)

	out := getBody(t, s.handleDelivery, "/deliveries/"+itoa(row.ID)+"?domain=bs.example.ru&p=2")
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

// Send-log rows are pruned on the retention window, so a bookmarked delivery
// that no longer exists is a 404, not a 500.
func TestDeliveryPageNotFound(t *testing.T) {
	s, _ := serverWithDelivery(t)

	for _, path := range []string{"/deliveries/999999", "/deliveries/abc", "/deliveries/0"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.SetPathValue("id", strings.TrimPrefix(path, "/deliveries/"))
		s.handleDelivery(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
}

// serverWithDelivery builds a panel over a store holding one delivery, written
// the way the journal-milter wrote them before it decoded subjects itself.
func serverWithDelivery(t *testing.T) (*Server, store.SendLogRow) {
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
	rows, err := st.QuerySendLog(store.SendLogFilter{}, 1, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("query: %v (%d rows)", err, len(rows))
	}

	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	return &Server{store: st, tmpl: tmpl, cfg: Config{Version: "test"}}, rows[0]
}

// getBody runs one handler over a GET and returns the page it wrote, failing
// the test on any non-200. The path's {id} is bound by hand because these calls
// bypass the router that would otherwise fill it in.
func getBody(t *testing.T, h http.HandlerFunc, target string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
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
