package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/app"
	"github.com/mixeme/selfpost/internal/inbound"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

// A page that a domain administrator sees says what is wrong with what they
// typed. It never says what went wrong inside the server: paths, SQL, the
// output of a command. Those go to the log, and the page gets a fixed line.

// leak is a stand-in for internal detail that must never reach a page.
const leak = "secret-internal-detail"

const savedElsewhere = "Check the logs and try again."

type failingSenderMaps struct{}

func (failingSenderMaps) RebuildSenderLoginMaps([]postfix.Binding) error {
	return errors.New("write /srv/" + leak + "/sender_login_maps: permission denied")
}

type failingInboundMaps struct{}

func (failingInboundMaps) RebuildInboundMaps([]postfix.InboundRoute) error {
	return errors.New("postmap /srv/" + leak + "/transport_maps: exit status 1")
}

// breakDatabase makes the statements fail inside SQLite with an error that
// carries the leak, as a real storage failure would carry its own detail.
func breakDatabase(t *testing.T, dbPath string, triggers ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open database directly: %v", err)
	}
	defer db.Close()
	for _, stmt := range triggers {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

// refusal checks a refused request: the status it has today, none of the
// internal detail, and the fixed line in its place.
func refusal(t *testing.T, what, body string, wantCode, code int) {
	t.Helper()
	shown := noticeOf(body)
	if code != wantCode {
		t.Errorf("%s = %d, want %d; page says %q", what, code, wantCode, shown)
	}
	if strings.Contains(body, leak) {
		t.Errorf("%s shows internal detail; page says %q", what, shown)
	}
	if !strings.Contains(body, savedElsewhere) {
		t.Errorf("%s does not give the fixed line %q; page says %q", what, savedElsewhere, shown)
	}
}

// noticeOf is the text of the page's refusal notice, for failure messages.
func noticeOf(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "ti-alert-circle") {
			return strings.TrimSpace(line)
		}
	}
	return "(no notice)"
}

func TestApplicationFormDoesNotEchoInternalErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s := newAppStandAt(t, path)
	working := s.h.apps
	a, _, err := working.CreateWithSettings(s.d.ID, "prod-server", app.Settings{Mode: store.AddressModeWildcard}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{"id": idStr(s.d.ID), "aid": idStr(a.ID)}

	// The reload of the Postfix maps fails.
	sasl := app.NewSASLDB("/data/sasl/sasldb2", "mail.example.org").WithRunner(func([]string, []byte) error { return nil })
	s.h.apps = app.NewService(s.h.store, sasl, failingSenderMaps{})

	rec := postFormAs(s.h.HandleApplicationCreate, globalPrincipal, "/outbound/domains/1/applications/new", s.paths,
		url.Values{"login": {"new-server"}, "mode": {"wildcard"}})
	refusal(t, "create when the reload fails", rec.Body.String(), http.StatusBadRequest, rec.Code)

	rec = postFormAs(s.h.HandleApplicationSave, globalPrincipal, "/outbound/domains/1/applications/1", paths,
		url.Values{"mode": {"wildcard"}})
	refusal(t, "save when the reload fails", rec.Body.String(), http.StatusBadRequest, rec.Code)

	// The database fails.
	s.h.apps = working
	breakDatabase(t, path, `CREATE TRIGGER boom BEFORE UPDATE ON applications BEGIN SELECT RAISE(ABORT, '`+leak+`'); END`)
	rec = postFormAs(s.h.HandleApplicationSave, globalPrincipal, "/outbound/domains/1/applications/1", paths,
		url.Values{"mode": {"wildcard"}})
	refusal(t, "save when the database fails", rec.Body.String(), http.StatusBadRequest, rec.Code)
}

// What the administrator typed wrong is still said in full.
func TestApplicationFormStillShowsValidationMessages(t *testing.T) {
	s := newAppStand(t)
	a, _, err := s.h.apps.CreateWithSettings(s.d.ID, "prod-server", app.Settings{Mode: store.AddressModeWildcard}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{"id": idStr(s.d.ID), "aid": idStr(a.ID)}

	for name, tc := range map[string]struct {
		url.Values
		want string
	}{
		"a bad login":          {url.Values{"login": {"has space"}, "mode": {"wildcard"}}, "login may contain only letters, digits"},
		"another domain's one": {url.Values{"login": {"other-app"}, "mode": {"list"}, "addresses": {"a@evil.example"}}, "does not belong to domain example.org"},
		"a taken login":        {url.Values{"login": {"prod-server"}, "mode": {"wildcard"}}, "That login is already in use"},
	} {
		rec := postFormAs(s.h.HandleApplicationCreate, globalPrincipal, "/outbound/domains/1/applications/new", s.paths, tc.Values)
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("create with %s does not say %q:\n%s", name, tc.want, rec.Body.String())
		}
	}
	rec := postFormAs(s.h.HandleApplicationSave, globalPrincipal, "/outbound/domains/1/applications/1", paths,
		url.Values{"mode": {"list"}, "addresses": {"a@evil.example"}})
	if !strings.Contains(rec.Body.String(), "does not belong to domain example.org") {
		t.Errorf("save with another domain's address does not say so:\n%s", rec.Body.String())
	}
}

func TestInboundFormsDoNotEchoInternalErrors(t *testing.T) {
	h, st := inboundHandlers(t)
	d, err := st.AddInboundDomain("lists.example.com")
	if err != nil {
		t.Fatal(err)
	}
	h.inbound = inbound.NewService(st, failingInboundMaps{})
	id := itoa(d.ID)

	rec := inboundCall(h, http.MethodPost, "/inbound/domains/"+id+"/upstream",
		url.Values{"host": {"10.0.0.8"}, "port": {"25"}, "tls_mode": {"encrypt"}}, globalPrincipal)
	refusal(t, "upstream when the reload fails", rec.Body.String(), http.StatusBadRequest, rec.Code)

	rec = inboundCall(h, http.MethodPost, "/inbound/domains/"+id+"/recipients",
		url.Values{"recipient_mode": {"any"}}, globalPrincipal)
	refusal(t, "recipients when the reload fails", rec.Body.String(), http.StatusBadRequest, rec.Code)
}

func TestInboundFormsStillShowValidationMessages(t *testing.T) {
	h, st := inboundHandlers(t)
	d, err := st.AddInboundDomain("lists.example.com")
	if err != nil {
		t.Fatal(err)
	}
	id := itoa(d.ID)

	for name, tc := range map[string]struct {
		target string
		form   url.Values
		want   string
	}{
		"a bad host":      {"/upstream", url.Values{"host": {"bad host!"}, "port": {"25"}, "tls_mode": {"may"}}, "host is invalid"},
		"a bad port":      {"/upstream", url.Values{"host": {"10.0.0.8"}, "port": {"99999"}, "tls_mode": {"may"}}, "port must be between 1 and 65535"},
		"a bad TLS mode":  {"/upstream", url.Values{"host": {"10.0.0.8"}, "port": {"25"}, "tls_mode": {"sometimes"}}, "invalid TLS mode"},
		"another domain":  {"/recipients", url.Values{"recipient_mode": {"list"}, "addresses": {"abuse@other.com"}}, "does not belong to domain lists.example.com"},
		"an empty list":   {"/recipients", url.Values{"recipient_mode": {"list"}}, "requires at least one address"},
		"an unknown mode": {"/recipients", url.Values{"recipient_mode": {"some"}}, "invalid recipient mode"},
	} {
		rec := inboundCall(h, http.MethodPost, "/inbound/domains/"+id+tc.target, tc.form, globalPrincipal)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s = %d, does not say %q:\n%s", name, rec.Code, tc.want, rec.Body.String())
		}
	}
}

func TestRecalculationDoesNotEchoInternalErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s := newAppStandAt(t, path)
	a, _, err := s.h.apps.CreateWithSettings(s.d.ID, "prod-server", app.Settings{Mode: store.AddressModeWildcard}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for scope, ref := range map[string]int64{store.RateLimitScopeDomain: s.d.ID, store.RateLimitScopeApp: a.ID} {
		rl := store.RateLimit{Scope: scope, RefID: ref, Mode: store.RateLimitModeAuto, AutoMultiplier: store.DefaultAutoMultiplier, WindowSeconds: 3600}
		if err := s.h.store.SetRateLimit(rl); err != nil {
			t.Fatal(err)
		}
	}
	breakDatabase(t, path,
		`CREATE TRIGGER boom_insert BEFORE INSERT ON rate_limits BEGIN SELECT RAISE(ABORT, '`+leak+`'); END`,
		`CREATE TRIGGER boom_update BEFORE UPDATE ON rate_limits BEGIN SELECT RAISE(ABORT, '`+leak+`'); END`)

	rec := postFormAs(s.h.HandleDomainRateLimitRecalc, globalPrincipal, "/outbound/domains/1/settings/ratelimit/recalc", s.paths, url.Values{})
	refusal(t, "domain recalculation when the database fails", rec.Body.String(), http.StatusBadRequest, rec.Code)

	paths := map[string]string{"id": idStr(s.d.ID), "aid": idStr(a.ID)}
	rec = postFormAs(s.h.HandleAppRateLimitRecalc, globalPrincipal, "/outbound/domains/1/applications/1/ratelimit/recalc", paths, url.Values{})
	refusal(t, "application recalculation when the database fails", rec.Body.String(), http.StatusBadRequest, rec.Code)
}
