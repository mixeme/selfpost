package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/store"
)

// The Outbound log and the page of one message, rendered from the real journal
// by the handlers. Their markup is held to the mockups by the guards in
// internal/web/view; what is checked here is the behaviour the pages keep from
// the legacy ones — the polled region, the paging, the scoping — on the new
// markup.

// regionOf cuts the polled region (the table and the foot under it) out of a
// body: from the table's container to the end of the foot.
func regionOf(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `<div class="table-container" id="out-log-rows"`)
	foot := strings.Index(body, `<div class="sp-box-foot"`)
	if start < 0 || foot < start {
		t.Fatalf("no polled region in:\n%s", body)
	}
	end := strings.Index(body[foot:], "</div>")
	return body[start : foot+end+len("</div>")]
}

// The fragment answers with the markup the page holds for the same region, so
// the first poll changes nothing a person sees: the table, byte for byte, and
// the foot, which only gains the attribute that swaps it into place by its id.
func TestOutLogFragmentIsTheRegionOfThePage(t *testing.T) {
	h, _ := serverWithDelivery(t)
	page := regionOf(t, getBody(t, h.HandleDeliveries, "/outbound/log?domain=bs.example.ru"))
	fragment := getBody(t, h.HandleDeliveriesRows, "/outbound/log/fragment?domain=bs.example.ru")

	if !strings.Contains(fragment, `id="out-log-foot" hx-swap-oob="true"`) {
		t.Errorf("the fragment's foot is not marked to be swapped in by its id:\n%s", fragment)
	}
	swapped := strings.Replace(fragment, ` hx-swap-oob="true"`, "", 1)
	if swapped != page {
		t.Errorf("the fragment is not the page's polled region\npage:\n%s\nfragment:\n%s", page, fragment)
	}
	// The poll asks for the same filters again, and never replaces the filter form.
	has(t, "region", page, `data-poll`, `aria-live="polite"`, `hx-trigger="load"`, `hx-swap="outerHTML"`,
		`hx-get="/outbound/log/fragment?domain=bs.example.ru"`)
	lacks(t, "fragment", fragment, `<select`, `<form`)
	if strings.Contains(page, "hx-swap-oob") {
		t.Errorf("the page itself marks its foot out of band:\n%s", page)
	}
}

// With more than a page of rows the foot says where the viewer is and leads to
// the newer and older rows with the filters kept, and the poll refreshes the page
// being looked at.
func TestOutLogPagesKeepTheFiltersAndThePage(t *testing.T) {
	h, _ := serverWithDelivery(t)
	for i := 0; i < sendLogPageSize; i++ {
		if err := h.store.InsertQueued(store.SendLogEntry{
			QueueID: fmt.Sprintf("Q%04d", i), Domain: "bs.example.ru", AppLogin: "Queuer3C",
			From: "noreply@bs.example.ru", To: "public@example.ru", Subject: fmt.Sprintf("Message %d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	first := getBody(t, h.HandleDeliveries, "/outbound/log?domain=bs.example.ru")
	has(t, "page 1", first, `Page 1 of 2`, `<a href="/outbound/log?domain=bs.example.ru&amp;p=2">Older →</a>`, `New rows appear on their own`)
	lacks(t, "page 1", first, `← Newer`)

	second := getBody(t, h.HandleDeliveries, "/outbound/log?domain=bs.example.ru&p=2")
	has(t, "page 2", second, `Page 2 of 2`, `<a href="/outbound/log?domain=bs.example.ru">← Newer</a>`,
		`hx-get="/outbound/log/fragment?domain=bs.example.ru&amp;p=2"`)
	lacks(t, "page 2", second, `Older →`)
	// A row's page is opened from the page it is on and leads back to it.
	has(t, "page 2", second, `?domain=bs.example.ru&amp;p=2">Details</a>`)
}

// Nothing logged yet is a sentence in the polled region, which goes on polling,
// not a table with no rows; the foot still says that rows will appear.
func TestOutLogEmpty(t *testing.T) {
	h, _ := serverWithTwoDomains(t)
	p := domainAdmin(t, h.store, "no-domains")
	body := getBodyAs(t, h.HandleDeliveries, "/outbound/log", p)
	has(t, "empty log", body, `No messages logged yet.`, `id="out-log-rows"`, `data-poll`, `New rows appear on their own`)
	lacks(t, "empty log", body, `<table`, `Page 1`)
	// Only the global role is pointed at the setting that decides how long rows live.
	lacks(t, "empty log", body, `href="/server/settings"`)
	has(t, "log", getBody(t, h.HandleDeliveries, "/outbound/log"), `href="/server/settings">Settings</a>`)
}

// What a sender wrote is text on the page: a subject or an address is never
// markup, in the list, in the fragment or on the message's own page.
func TestOutLogPagesEscapeWhatSendersWrote(t *testing.T) {
	h, _ := serverWithDelivery(t)
	if err := h.store.InsertQueued(store.SendLogEntry{
		QueueID: "EVIL0001", Domain: "bs.example.ru", AppLogin: "Queuer3C",
		From: `"><img src=x onerror=alert(1)>@bs.example.ru`, To: `<script>alert(2)</script>@example.ru`,
		Subject: `<script>alert(3)</script> "quoted"`,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := h.store.QuerySendLog(store.SendLogFilter{AllDomains: true}, 1, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("query: %v (%d rows)", err, len(rows))
	}
	for name, body := range map[string]string{
		"list":     getBody(t, h.HandleDeliveries, "/outbound/log"),
		"fragment": getBody(t, h.HandleDeliveriesRows, "/outbound/log/fragment"),
		"message":  getBody(t, h.HandleDelivery, "/outbound/log/"+itoa(rows[0].ID)),
	} {
		lacks(t, name, body, `<script>alert`, `<img src=x`)
		has(t, name, body, `&lt;script&gt;alert(3)&lt;/script&gt;`)
	}
}

// The names of the domain and the application on a message's page lead to
// theirs when there is a page to go to, and are plain text when there is not.
func TestMessagePageLinksTheDomainAndTheApplication(t *testing.T) {
	h, domains := serverWithTwoDomains(t)
	first := domains["first.example.ru"]
	app, err := h.store.GetApplicationByLogin("first-app")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := h.store.QuerySendLog(store.SendLogFilter{Domain: first.Name, AllDomains: true}, 1, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("query: %v (%d rows)", err, len(rows))
	}
	target := "/outbound/log/" + itoa(rows[0].ID)

	body := getBody(t, h.HandleDelivery, target)
	has(t, "message", body, `<a href="/outbound/domains/`+itoa(first.ID)+`">first.example.ru</a>`,
		`<a href="/outbound/domains/`+itoa(first.ID)+`/applications/`+itoa(app.ID)+`">first-app</a>`)

	// A domain administrator of that domain gets the same links.
	ops := domainAdmin(t, h.store, "ops", first.ID)
	has(t, "message for its administrator", getBodyAs(t, h.HandleDelivery, target, ops),
		`<a href="/outbound/domains/`+itoa(first.ID)+`">first.example.ru</a>`)

	// The domain was deleted since: its name stays, as text, for the global role.
	if err := h.store.DeleteDomain(first.ID); err != nil {
		t.Fatal(err)
	}
	body = getBody(t, h.HandleDelivery, target)
	has(t, "message of a deleted domain", body, `first.example.ru`)
	lacks(t, "message of a deleted domain", body, `<a href="/outbound/domains/`)
}

// Each outcome of a delivery is drawn at its own level in the history and in the
// log, so a message that did not go through reads so before a word is read.
func TestMessagePageDrawsTheOutcomeAtItsLevel(t *testing.T) {
	for _, tc := range []struct {
		status, step, tag string
	}{
		{store.StatusSent, `<li><time>`, `<span class="tag is-success is-light">sent</span>`},
		{store.StatusDeferred, `<li class="sp-warn"><time>`, `<span class="tag is-warning is-light">deferred</span>`},
		{store.StatusBounced, `<li class="sp-fail"><time>`, `<span class="tag is-danger is-light">bounced</span>`},
	} {
		h, row := serverWithDelivery(t)
		h.cfg.MailLogPath = writeMailLog(t,
			"2026-08-03T05:16:03.884210+00:00 host postfix/smtp[26]: 4A1B2C3D: to=<public@example.ru>, status="+tc.status+" (reply)")
		if _, err := h.store.UpdateStatus(row.QueueID, row.To, tc.status); err != nil {
			t.Fatal(err)
		}
		body := getBody(t, h.HandleDelivery, "/outbound/log/"+itoa(row.ID))
		has(t, tc.status, body, tc.step, tc.tag)
		switch tc.status {
		case store.StatusDeferred:
			has(t, tc.status, body, `<span class="sp-w">host postfix/smtp[26]: 4A1B2C3D: to=&lt;public@example.ru&gt;, status=deferred (reply)</span>`)
		case store.StatusBounced:
			has(t, tc.status, body, `<span class="sp-e">host postfix/smtp[26]: 4A1B2C3D: to=&lt;public@example.ru&gt;, status=bounced (reply)</span>`)
		default:
			lacks(t, tc.status, body, `class="sp-w"`, `class="sp-e"`)
		}
	}
}

// A message the milter refused never reached the queue: it has no queue id and
// no lines to show, and says why instead of showing an empty log.
func TestMessagePageOfARefusedMessage(t *testing.T) {
	h, _ := serverWithDelivery(t)
	if err := h.store.InsertRejected(store.SendLogEntry{
		Domain: "bs.example.ru", AppLogin: "Queuer3C", From: "noreply@bs.example.ru", To: "late@example.ru", Subject: "Too many",
	}); err != nil {
		t.Skipf("the store has no InsertRejected to refuse a message with: %v", err)
	}
	rows, err := h.store.QuerySendLog(store.SendLogFilter{AllDomains: true}, 1, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("query: %v (%d rows)", err, len(rows))
	}
	body := getBody(t, h.HandleDelivery, "/outbound/log/"+itoa(rows[0].ID))
	has(t, "refused message", body, `Refused before queueing`, `class="sp-fail"`, `never reached the queue`, `<span class="tag is-light">rejected</span>`)
	lacks(t, "refused message", body, `<pre`)
}

// Whatever the viewer reaches, another tenant's message is a 404 on its own
// page whether the row exists or not.
func TestMessagePageIsNotFoundForOtherTenants(t *testing.T) {
	h, domains := serverWithTwoDomains(t)
	rows, err := h.store.QuerySendLog(store.SendLogFilter{Domain: "second.example.ru", AllDomains: true}, 1, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("query: %v (%d rows)", err, len(rows))
	}
	ops := domainAdmin(t, h.store, "ops", domains["first.example.ru"].ID)
	for _, id := range []string{itoa(rows[0].ID), "999999"} {
		rec := send(h.HandleDelivery, &ops, "GET", "/outbound/log/"+id, map[string]string{"id": id}, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("message %s for another tenant = %d, want 404", id, rec.Code)
		}
	}
}
