package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/dmarc"
	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
)

// postFormAs sends a form to a handler as the given principal.
func postFormAs(h http.HandlerFunc, p auth.Principal, target string, pathValues map[string]string, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	rec := httptest.NewRecorder()
	h(rec, auth.RequestWithPrincipal(req, p))
	return rec
}

// dmarcRecordingMaps stands in for Postfix: the allow-lists a resync would write.
type dmarcRecordingMaps struct{ got []postfix.DMARCMapsConfig }

func (m *dmarcRecordingMaps) RebuildDMARCMaps(cfg postfix.DMARCMapsConfig) error {
	m.got = append(m.got, cfg)
	return nil
}

// hostedStand is an app stand whose server receives hosted DMARC reports.
func hostedStand(t *testing.T) (appStand, *dmarcRecordingMaps) {
	t.Helper()
	s := newAppStand(t)
	maps := &dmarcRecordingMaps{}
	s.h.cfg.DMARCEnabled, s.h.cfg.Hostname = true, "mail.example.org"
	s.h.dmarc = dmarc.NewService(s.h.store, maps, "mail.example.org", true)
	return s, maps
}

const hostedForExample = "dmarc-reports+example.org@mail.example.org"

// hostedEscaped is that address as html/template writes it into a page.
const hostedEscaped = "dmarc-reports&#43;example.org@mail.example.org"

// saveReports posts the report-address form of domain 1 as the global
// administrator.
func (s appStand) saveReports(form url.Values) *httptest.ResponseRecorder {
	return postFormAs(s.h.HandleDomainDMARC, globalPrincipal, "/outbound/domains/1/settings/reports", s.paths, form)
}

func (s appStand) reportAddress(t *testing.T) string {
	t.Helper()
	d, err := s.h.store.GetDomain(s.d.ID)
	if err != nil {
		t.Fatal(err)
	}
	return d.DMARCRua
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// A new domain has no report address: its settings show "No reports" selected,
// and its DMARC record says so.
func TestNewDomainHasNoReports(t *testing.T) {
	s, _ := hostedStand(t)
	// A second OpenDKIM that reloads nothing: the stand's has no supervisor.
	odk := domain.NewOpenDKIM(t.TempDir())
	odk.SetReloadHook(func() error { return nil })
	if _, err := odk.EnsureKey("example.org", "mail"); err != nil {
		t.Fatal(err)
	}
	s.h.domains = domain.NewService(s.h.store, odk, s.h.apps, "mail")
	rec := postFormAs(s.h.HandleAddDomain, globalPrincipal, "/outbound/domains", nil, url.Values{"name": {"new.example"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("adding a domain = %d:\n%s", rec.Code, rec.Body.String())
	}
	domains, _ := s.h.store.ListDomains()
	var added store.Domain
	for _, d := range domains {
		if d.Name == "new.example" {
			added = d
		}
	}
	if added.ID == 0 || added.DMARCRua != "" {
		t.Fatalf("a new domain reports to %q (%+v), want no reports", added.DMARCRua, added)
	}
	body := send(s.h.HandleDomainSettings, &globalPrincipal, "GET", "/outbound/domains/2/settings", map[string]string{"id": idStr(added.ID)}, nil).Body.String()
	has(t, "settings of a new domain", body, `<option value="none" selected>No reports</option>`)
	lacks(t, "settings of a new domain", body, `<option value="hosted" selected>`, `<option value="custom" selected>`)

	// The domain the stand started with has none either.
	has(t, "domain page", pageOf(t, s.h.HandleDomainDetail, "/outbound/domains/1"),
		`Report address: <a href="/outbound/domains/1/settings#reports">no reports</a>`)
}

// Each of the three choices is saved, comes back selected, and is what the
// DMARC record names. Hosted is the address SelfPost derives for the domain.
func TestDomainReportAddressThreeChoices(t *testing.T) {
	s, maps := hostedStand(t)
	settings := func() string {
		return pageOf(t, s.h.HandleDomainSettings, "/outbound/domains/1/settings")
	}
	// Offered in the order of the mockup, hosted because the server receives it.
	has(t, "settings", settings(),
		`<option value="none" selected>No reports</option>`,
		`<option value="hosted">SelfPost hosted — `+hostedEscaped+`</option>`,
		`<option value="custom">A specific address</option>`)

	if rec := s.saveReports(url.Values{"dmarc_rua_mode": {"hosted"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("hosted = %d:\n%s", rec.Code, rec.Body.String())
	}
	if got := s.reportAddress(t); got != hostedForExample {
		t.Fatalf("hosted stored %q, want %q", got, hostedForExample)
	}
	has(t, "settings after hosted", settings(), `<option value="hosted" selected>`)
	lacks(t, "settings after hosted", settings(), `value="`+hostedForExample+`"`) // the typed field stays empty
	has(t, "domain page after hosted", pageOf(t, s.h.HandleDomainDetail, "/outbound/domains/1"),
		`Report address: <a href="/outbound/domains/1/settings#reports">`+hostedEscaped+`</a>`,
		`rua=mailto:`+hostedEscaped)
	// The ingest allow-list was rebuilt for the new address.
	if last := maps.got[len(maps.got)-1]; !contains(last.Recipients, hostedForExample) {
		t.Errorf("the allow-list after choosing hosted: %v", last.Recipients)
	}

	if rec := s.saveReports(url.Values{"dmarc_rua_mode": {"custom"}, "dmarc_rua_email": {" reports@hub.example "}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("custom = %d:\n%s", rec.Code, rec.Body.String())
	}
	if got := s.reportAddress(t); got != "reports@hub.example" {
		t.Fatalf("custom stored %q", got)
	}
	has(t, "settings after custom", settings(), `<option value="custom" selected>`, `value="reports@hub.example"`)
	has(t, "domain page after custom", pageOf(t, s.h.HandleDomainDetail, "/outbound/domains/1"),
		`Report address: <a href="/outbound/domains/1/settings#reports">reports@hub.example</a>`, `rua=mailto:reports@hub.example`,
		`<h3>Report authorization</h3>`) // the address is on another domain than the senders
	if last := maps.got[len(maps.got)-1]; contains(last.Recipients, hostedForExample) || contains(last.Recipients, "reports@hub.example") {
		t.Errorf("the allow-list after choosing another host: %v", last.Recipients)
	}

	if rec := s.saveReports(url.Values{"dmarc_rua_mode": {"none"}, "dmarc_rua_email": {"left-over@hub.example"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("none = %d:\n%s", rec.Code, rec.Body.String())
	}
	if got := s.reportAddress(t); got != "" {
		t.Fatalf("none stored %q; the typed address belongs to the typed choice alone", got)
	}
	has(t, "settings after none", settings(), `<option value="none" selected>`)
	lacks(t, "settings after none", settings(), `left-over@hub.example`)
}

// Hosted reports are offered, and accepted, only while the server receives
// them; the old follow / keep choices are not accepted any more.
func TestDomainReportAddressRefusals(t *testing.T) {
	s := newAppStand(t) // hosted reports are off
	body := pageOf(t, s.h.HandleDomainSettings, "/outbound/domains/1/settings")
	lacks(t, "settings", body, `value="hosted"`, `SelfPost hosted`, `value="inherit"`, `value="keep"`)
	has(t, "settings", body, `<option value="none" selected>No reports</option>`, `<option value="custom">A specific address</option>`)

	if err := s.h.store.SetDomainDMARCAddress(s.d.ID, "reports@hub.example"); err != nil {
		t.Fatal(err)
	}
	for _, form := range []url.Values{
		{"dmarc_rua_mode": {"hosted"}},
		{"dmarc_rua_mode": {"inherit"}},
		{"dmarc_rua_mode": {"keep"}},
		{"dmarc_rua_mode": {"custom"}},
		{"dmarc_rua_mode": {"custom"}, "dmarc_rua_email": {"not an address<b>"}},
		{},
	} {
		if rec := s.saveReports(form); rec.Code != http.StatusBadRequest {
			t.Errorf("%v = %d, want 400", form, rec.Code)
		}
		if got := s.reportAddress(t); got != "reports@hub.example" {
			t.Fatalf("%v changed the address to %q", form, got)
		}
	}

	// A refused address comes back in the field, escaped, with its choice.
	rec := s.saveReports(url.Values{"dmarc_rua_mode": {"custom"}, "dmarc_rua_email": {`x"><i>@no`}})
	has(t, "refusal", rec.Body.String(), `<option value="custom" selected>`, `value="x&#34;&gt;&lt;i&gt;@no"`)
	lacks(t, "refusal", rec.Body.String(), `<i>@no`)
}

// Saving the form as it was shown changes nothing, whichever of the three
// states the domain is in.
func TestSavingTheReportFormUntouchedChangesNothing(t *testing.T) {
	selected := regexp.MustCompile(`<option value="([a-z]+)" selected>`)
	typed := regexp.MustCompile(`name="dmarc_rua_email" type="email" value="([^"]*)"`)
	for name, stored := range map[string]string{
		"no reports": "", "hosted": hostedForExample, "typed": "reports@hub.example",
	} {
		s, _ := hostedStand(t)
		if err := s.h.store.SetDomainDMARCAddress(s.d.ID, stored); err != nil {
			t.Fatal(err)
		}
		page := pageOf(t, s.h.HandleDomainSettings, "/outbound/domains/1/settings")
		form := url.Values{}
		if m := selected.FindStringSubmatch(page[strings.Index(page, `name="dmarc_rua_mode"`):]); m != nil {
			form.Set("dmarc_rua_mode", m[1])
		}
		if m := typed.FindStringSubmatch(page); m != nil {
			form.Set("dmarc_rua_email", m[1])
		}
		if form.Get("dmarc_rua_mode") == "" {
			t.Fatalf("%s: no choice is selected on the page:\n%s", name, page)
		}
		if rec := s.saveReports(form); rec.Code != http.StatusSeeOther {
			t.Fatalf("%s: saving untouched %v = %d:\n%s", name, form, rec.Code, rec.Body.String())
		}
		if got := s.reportAddress(t); got != stored {
			t.Errorf("%s: an untouched form changed %q to %q", name, stored, got)
		}
	}
}

// settingsAs is the settings page of domain 1 as the given principal.
func settingsAs(t *testing.T, s appStand, p auth.Principal) string {
	t.Helper()
	rec := send(s.h.HandleDomainSettings, &p, "GET", "/outbound/domains/1/settings", s.paths, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings as %s = %d", p.Username, rec.Code)
	}
	return rec.Body.String()
}

// The button that fills the typed address carries the signed-in user's profile
// e-mail, escaped; without an e-mail there is no button.
func TestFillButtonCarriesTheProfileEmail(t *testing.T) {
	s := newAppStand(t)
	lacks(t, "settings", settingsAs(t, s, globalPrincipal), `data-fill`, `Use my e-mail`)

	admin, _ := s.h.store.GetUser(globalPrincipal.ID)
	if err := s.h.store.UpdateUser(admin.ID, admin.Username, admin.PasswordHash, `o'brien&co@example.org`); err != nil {
		t.Fatal(err)
	}
	body := settingsAs(t, s, globalPrincipal)
	has(t, "settings", body, `data-fill="rua_email" data-fill-value="o&#39;brien&amp;co@example.org"`, `Use my e-mail`)
	lacks(t, "settings", body, `o'brien`)
	// It fills; the form's own button saves, so the fill button is not a submit.
	has(t, "settings", body, `<button type="button" class="button" data-fill=`)

	// A domain administrator is offered their own e-mail, never the global one's.
	ops := domainAdmin(t, s.h.store, "ops", s.d.ID)
	if got := settingsAs(t, s, ops); strings.Contains(got, "brien") || strings.Contains(got, "data-fill") {
		t.Errorf("the co-administrator without an e-mail is offered someone else's:\n%s", got)
	}
	opsUser, _ := s.h.store.GetUser(ops.ID)
	if err := s.h.store.UpdateUser(opsUser.ID, opsUser.Username, opsUser.PasswordHash, "ops@example.org"); err != nil {
		t.Fatal(err)
	}
	has(t, "settings for ops", settingsAs(t, s, ops), `data-fill-value="ops@example.org"`)
}

// Deleting a user leaves every domain's address as it was, and a domain
// administrator never sees another user's name or address in this form.
func TestDeletingAUserLeavesDomainAddressesAndNamesNobody(t *testing.T) {
	s, _ := hostedStand(t)
	admin, _ := s.h.store.GetUser(globalPrincipal.ID)
	if err := s.h.store.UpdateUser(admin.ID, admin.Username, admin.PasswordHash, "boss-private@example.org"); err != nil {
		t.Fatal(err)
	}
	if rec := s.saveReports(url.Values{"dmarc_rua_mode": {"custom"}, "dmarc_rua_email": {"reports@hub.example"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("save = %d", rec.Code)
	}
	ops := domainAdmin(t, s.h.store, "ops", s.d.ID)
	for name, body := range map[string]string{
		"settings": settingsAs(t, s, ops),
		"domain":   send(s.h.HandleDomainDetail, &ops, "GET", "/outbound/domains/1", s.paths, nil).Body.String(),
	} {
		lacks(t, name+" for the co-administrator", body, `boss-private@example.org`, `admin&#39;s`, `s default`, `Another user`)
	}

	rec := send(s.h.HandleUserDelete, &globalPrincipal, "POST", "/server/users/"+idStr(ops.ID)+"/delete", map[string]string{"uid": idStr(ops.ID)}, url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("deleting a user = %d:\n%s", rec.Code, rec.Body.String())
	}
	if got := s.reportAddress(t); got != "reports@hub.example" {
		t.Errorf("deleting a user changed the domain's address to %q", got)
	}
	confirm := send(s.h.HandleUserDeleteConfirm, &globalPrincipal, "GET", "/server/users/"+idStr(admin.ID)+"/delete", map[string]string{"uid": idStr(admin.ID)}, nil).Body.String()
	lacks(t, "delete confirmation", confirm, `report address`, `will have no`)
}
