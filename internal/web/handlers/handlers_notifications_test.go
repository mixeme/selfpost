package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/web/auth"
)

// The panel pages of notifications (plans/panel-notifications.md): the user's
// checkbox on Account, the instance's switch and the DNS records of the server's
// own name on Server › Settings, and the state of those records on Server ›
// Health. What is sent and when is internal/notify's and tested there.

const notifyHost = "mail.example.org"

// ---- Account

// The checkbox under the e-mail is the user's own switch: the page shows what is
// stored, Save profile writes it, and an unchecked box is a form without the
// field, which means off. Saving it changes nothing else of the profile.
func TestAccountNotifyCheckbox(t *testing.T) {
	h, _ := settingsServer(t)
	notify := func() bool {
		u, _ := h.store.GetUser(globalPrincipal.ID)
		return u.Notify
	}
	save := func(extra url.Values) {
		t.Helper()
		form := url.Values{"username": {"admin"}, "email": {"mix@example.org"}}
		for k, v := range extra {
			form[k] = v
		}
		if rec := postFormAs(h.HandleAccountProfile, globalPrincipal, "/account/profile", nil, form); rec.Code != http.StatusSeeOther {
			t.Fatalf("POST /account/profile %v = %d:\n%s", extra, rec.Code, rec.Body.String())
		}
	}

	save(url.Values{"notify": {"on"}})
	if !notify() {
		t.Error("a ticked box did not turn notifications on")
	}
	if out := getBody(t, h.HandleAccount, "/account"); !strings.Contains(out, `name="notify" checked`) {
		t.Errorf("a user who has them on is shown an unticked box:\n%s", out)
	}

	save(nil)
	if notify() {
		t.Error("a form without the field did not turn notifications off")
	}
	if out := getBody(t, h.HandleAccount, "/account"); strings.Contains(out, `name="notify" checked`) || !strings.Contains(out, `name="notify"`) {
		t.Errorf("a user who has them off is shown a ticked box, or none:\n%s", out)
	}
	if u, _ := h.store.GetUser(globalPrincipal.ID); u.Email != "mix@example.org" {
		t.Errorf("saving the box lost the e-mail: %q", u.Email)
	}
}

// A profile that is refused saves nothing, the box included, and shows the form
// again as it was submitted.
func TestAccountNotifyIsNotSavedWithARefusedProfile(t *testing.T) {
	h, _ := settingsServer(t)
	if err := h.store.SetUserNotify(globalPrincipal.ID, false); err != nil {
		t.Fatal(err)
	}
	rec := postFormAs(h.HandleAccountProfile, globalPrincipal, "/account/profile", nil,
		url.Values{"username": {"admin"}, "email": {"not-an-address"}, "notify": {"on"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /account/profile with a bad e-mail = %d, want 400", rec.Code)
	}
	if u, _ := h.store.GetUser(globalPrincipal.ID); u.Notify {
		t.Error("a refused profile turned notifications on")
	}
	if !strings.Contains(rec.Body.String(), `name="notify" checked`) {
		t.Error("the form shown again with the error lost the box the user ticked")
	}
}

// ---- Server › Settings

// The switch is saved with the rest of the page and only a global user can save
// it: a domain administrator is told the page does not exist, and nothing moves.
func TestSettingsNotificationsSwitch(t *testing.T) {
	h, _ := settingsServer(t)
	on := func() bool {
		v, err := h.store.NotificationsEnabled()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	save := func(p auth.Principal, notify bool) *httptest.ResponseRecorder {
		form := url.Values{"send_log_retention_days": {"60"}}
		if notify {
			form.Set("notify", "on")
		}
		return postFormAs(h.HandleServerSettings, p, "/server/settings", nil, form)
	}

	if !on() {
		t.Fatal("a fresh instance has notifications off")
	}
	if rec := save(globalPrincipal, false); rec.Code != http.StatusSeeOther || on() {
		t.Errorf("an unticked box = %d, switch on: %v; want 303 and off", rec.Code, on())
	}
	if out := getBody(t, h.HandleServerSettings, "/server/settings"); strings.Contains(out, `name="notify" checked`) || !strings.Contains(out, `name="notify"`) {
		t.Errorf("the page shows the switch on while it is off:\n%s", out)
	}
	if rec := save(globalPrincipal, true); rec.Code != http.StatusSeeOther || !on() {
		t.Errorf("a ticked box = %d, switch on: %v; want 303 and on", rec.Code, on())
	}
	if out := getBody(t, h.HandleServerSettings, "/server/settings"); !strings.Contains(out, `name="notify" checked`) {
		t.Errorf("the page shows the switch off while it is on:\n%s", out)
	}

	d, err := h.store.AddDomain("example.com", "mail")
	if err != nil {
		t.Fatal(err)
	}
	ops := domainAdmin(t, h.store, "ops", d.ID)
	if rec := save(ops, false); rec.Code != http.StatusNotFound || !on() {
		t.Errorf("a domain administrator turning the switch off = %d, switch on: %v; want 404 and on", rec.Code, on())
	}
}

// A page that is refused (here: a retention out of range) leaves the switch as
// it was, like every other setting on it.
func TestSettingsRefusalLeavesTheNotificationsSwitch(t *testing.T) {
	h, _ := settingsServer(t)
	rec := postFormAs(h.HandleServerSettings, globalPrincipal, "/server/settings", nil, url.Values{"send_log_retention_days": {"3"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /server/settings = %d, want 400", rec.Code)
	}
	if on, _ := h.store.NotificationsEnabled(); !on {
		t.Error("a refused page turned notifications off")
	}
}

// serverMailStand is a panel that knows its own name, has the key notifications
// are signed with, and asks a DNS server that answers from a table: the address
// of the server's name is there, everything else is for the test to publish.
func serverMailStand(t *testing.T) (*Handlers, *fakeZone, domain.ServerMail) {
	t.Helper()
	h, _ := settingsServer(t)
	zone, addr := newFakeZone(t)
	zone.publishA(notifyHost, "192.0.2.10")
	h.dns = dnscheck.New([]string{addr})
	h.cfg.Hostname = notifyHost
	h.machine = &health.MachineSampler{}
	h.domains = domain.NewService(h.store, domain.NewKeys(t.TempDir()), nil, "sp2026")
	h.domains.SetServerName(notifyHost)
	mail, err := h.domains.ServerMail()
	if err != nil {
		t.Fatalf("ServerMail: %v", err)
	}
	return h, zone, mail
}

// publishMail puts both records of the server's own name into the zone.
func publishMail(zone *fakeZone, mail domain.ServerMail) {
	zone.publishTXT(mail.Record.Name, mail.Record.Value)
	zone.publishTXT(notifyHost, dnscheck.ServerMailSPF)
}

// With both records in DNS, Settings gives each with the value to publish and
// the state "published", and no warning about what is missing.
func TestSettingsMailRecordsPublished(t *testing.T) {
	h, zone, mail := serverMailStand(t)
	publishMail(zone, mail)

	out := getBody(t, h.HandleServerSettings, "/server/settings")
	for _, want := range []string{
		`id="notification-mail"`, `DNS records of the server&#39;s own name`,
		`<h3>DKIM</h3><span class="tag is-success is-light">published</span>`,
		`selector <code>sp2026</code>`, `value="sp2026._domainkey.mail.example.org"`,
		`<h3>SPF</h3><span class="tag is-success is-light">published</span>`,
		`For the server&#39;s own name, not for your domains`,
		`value="v=spf1 a -all"`,
		`<span class="sp-mono">postmaster@mail.example.org</span>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("settings page is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "may file them as spam") {
		t.Error("the warning about a missing SPF record is shown while it is published")
	}
}

// With nothing published the records are the same and the states say so, as a
// warning and not as the red of a domain: notifications are sent without them.
// The page says what that costs under SPF and what is missing under DKIM.
func TestSettingsMailRecordsMissing(t *testing.T) {
	h, _, _ := serverMailStand(t)

	out := getBody(t, h.HandleServerSettings, "/server/settings")
	for _, want := range []string{
		`<h3>DKIM</h3><span class="tag is-warning is-light">missing</span>`,
		`<h3>SPF</h3><span class="tag is-warning is-light">missing</span>`,
		`value="sp2026._domainkey.mail.example.org"`, `value="v=spf1 a -all"`,
		`Notifications are sent without it, and a receiver may file them as spam.`,
		`No TXT record is published at sp2026._domainkey.mail.example.org.`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("settings page is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `is-danger is-light">missing`) {
		t.Error("a record of the server's own name that is missing is drawn as an error")
	}
}

// A DKIM record that holds some other key is a mismatch, and the page shows what
// DNS holds next to what to publish.
func TestSettingsMailDKIMWithAnotherKey(t *testing.T) {
	h, zone, mail := serverMailStand(t)
	zone.publishTXT(mail.Record.Name, "v=DKIM1; k=rsa; p=AAAAB3NzaC1yc2EAAAADAQABAAABAQDhWrongKeyEntirely")

	out := getBody(t, h.HandleServerSettings, "/server/settings")
	for _, want := range []string{`<span class="tag is-warning is-light">mismatch</span>`, `In DNS now`, `WrongKeyEntirely`} {
		if !strings.Contains(out, want) {
			t.Errorf("settings page is missing %q:\n%s", want, out)
		}
	}
}

// If the key of the server's own name cannot be read the page is still there:
// the DKIM record is left out and SPF is shown unchecked.
func TestSettingsWithoutAServerKeyStillRenders(t *testing.T) {
	h, _ := settingsServer(t) // its domain service does not know the server's name
	out := getBody(t, h.HandleServerSettings, "/server/settings")
	for _, want := range []string{`id="notification-mail"`, `<h3>SPF</h3><span class="tag is-light">not checked</span>`, `name="notify"`} {
		if !strings.Contains(out, want) {
			t.Errorf("settings page is missing %q", want)
		}
	}
	if strings.Contains(out, "<h3>DKIM</h3>") {
		t.Error("a DKIM record is shown for a key that could not be read")
	}
}

// ---- Server › Health

// Health has the card with the state of each record, the host under its name,
// and the way to the values on Settings. A record that is missing makes the
// card a warning.
func TestHealthNotificationMailCard(t *testing.T) {
	h, zone, mail := serverMailStand(t)
	zone.publishTXT(mail.Record.Name, mail.Record.Value) // DKIM only

	out := getBody(t, h.HandleHealth, "/server/health")
	for _, want := range []string{
		`id="notification-mail"`, `<h2>Notification mail</h2><div class="sp-end"><span class="tag is-warning is-light">warn</span></div>`,
		`DKIM<br><span class="sp-muted sp-mono sp-small">sp2026._domainkey.mail.example.org</span></td><td><span class="tag is-success is-light">published</span>`,
		`SPF<br><span class="sp-muted sp-mono sp-small">mail.example.org</span></td><td><span class="tag is-warning is-light">missing</span>`,
		`Sent from <span class="sp-mono">postmaster@mail.example.org</span>`,
		`href="/server/settings#notification-mail"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("health page is missing %q:\n%s", want, out)
		}
	}
}

// With both records in place the card is green. The card is part of the polled
// fragment, swapped in by its id.
func TestHealthNotificationMailCardIsGreenAndPolled(t *testing.T) {
	h, zone, mail := serverMailStand(t)
	publishMail(zone, mail)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/server/health/fragment", nil)
	h.HandleHealthFragment(rec, auth.RequestWithPrincipal(req, globalPrincipal))
	out := rec.Body.String()
	for _, want := range []string{
		`id="notification-mail" hx-swap-oob="true"`,
		`<h2>Notification mail</h2><div class="sp-end"><span class="tag is-success is-light">ok</span></div>`,
		`<td><span class="tag is-success is-light">published</span></td>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("health fragment is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out[strings.Index(out, `id="notification-mail"`):], "is-warning") {
		t.Error("the card warns while both records are published")
	}
}

// The DNS answers are cached for a few minutes; Re-check DNS on Health asks for
// the records of the server's own name again as well as for its address.
func TestHealthRecheckAsksForNotificationMailAgain(t *testing.T) {
	h, zone, mail := serverMailStand(t)
	if out := getBody(t, h.HandleHealth, "/server/health"); strings.Count(out, `is-warning is-light">missing`) != 2 {
		t.Fatalf("before publishing, both records should be missing:\n%s", out)
	}

	publishMail(zone, mail)
	if out := getBody(t, h.HandleHealth, "/server/health"); strings.Contains(out, `is-success is-light">published`) {
		t.Fatal("the cached answer was not used: the test cannot tell a re-check from a fresh read")
	}

	rec := postFormAs(h.HandleHealthRecheck, globalPrincipal, "/server/health/recheck", nil, url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /server/health/recheck = %d, want 303", rec.Code)
	}
	if out := getBody(t, h.HandleHealth, "/server/health"); strings.Count(out, `is-success is-light">published`) != 2 {
		t.Errorf("after Re-check DNS both records should be published:\n%s", out)
	}
}
