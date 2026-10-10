package handlers

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/app"
	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

// offlineDNS answers every check from a resolver nobody listens on. It is
// shared, so the one slow failure is paid once and cached for the other tests.
var offlineDNS = dnscheck.New([]string{"127.0.0.1:9"})

type nopSenderMaps struct{}

func (nopSenderMaps) RebuildSenderLoginMaps([]postfix.Binding) error { return nil }

// appStand is a panel with one sending domain and a working application
// service whose SASL writes are recorded instead of run.
type appStand struct {
	h     *Handlers
	d     store.Domain
	sasl  map[string]string
	paths map[string]string
}

func newAppStand(t *testing.T) appStand {
	t.Helper()
	return newAppStandAt(t, filepath.Join(t.TempDir(), "test.db"))
}

// newAppStandAt is newAppStand over a database at a path the test knows.
func newAppStandAt(t *testing.T, dbPath string) appStand {
	t.Helper()
	h, _ := settingsServerAt(t, dbPath)
	s := appStand{h: h, sasl: map[string]string{}}
	sasl := app.NewSASLDB("/data/sasl/sasldb2", "mail.example.org").WithRunner(func(args []string, stdin []byte) error {
		login := args[len(args)-1]
		for _, a := range args {
			if a == "-d" {
				delete(s.sasl, login)
				return nil
			}
		}
		s.sasl[login] = string(stdin)
		return nil
	})
	h.apps = app.NewService(h.store, sasl, nopSenderMaps{})
	// The domain page is rendered with the domain's DKIM record and its DNS
	// state: a real key in a temporary directory, and a resolver nobody
	// listens on, so every check answers at once without leaving the machine.
	odk := domain.NewOpenDKIM(t.TempDir())
	if _, err := odk.EnsureKey("example.org", "mail"); err != nil {
		t.Fatalf("EnsureKey: %v", err)
	}
	h.domains = domain.NewService(h.store, odk, h.apps, "mail")
	h.dns = offlineDNS
	h.cfg.RateLimitMessagesPerIP, h.cfg.RateLimitWindowSeconds = 600, 3600
	var err error
	if s.d, err = h.store.AddDomain("example.org", "mail"); err != nil {
		t.Fatal(err)
	}
	s.paths = map[string]string{"id": idStr(s.d.ID)}
	return s
}

func (s appStand) app(t *testing.T, login string) (store.Application, store.RateLimit, bool) {
	t.Helper()
	a, err := s.h.store.GetApplicationByLogin(login)
	if err != nil {
		return store.Application{}, store.RateLimit{}, false
	}
	rl, ok, err := s.h.store.GetRateLimit(store.RateLimitScopeApp, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	return a, rl, ok
}

func TestParseApplicationForm(t *testing.T) {
	h := &Handlers{cfg: Config{RateLimitMessagesPerIP: 600, RateLimitWindowSeconds: 3600}}
	parse := func(v url.Values) (app.Settings, error) {
		r, _ := http.NewRequest(http.MethodPost, "/", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return h.parseApplicationForm(r)
	}

	set, err := parse(url.Values{
		"mode": {"list"}, "addresses": {"a@example.org\nb@example.org"},
		"auth_ip_restrict": {"1"}, "auth_allowed_ips": {"203.0.113.10, 203.0.113.11"},
		"rl_mode": {"manual"}, "max_messages": {"200"}, "window_seconds": {"1800"},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if set.Mode != "list" || len(set.Addresses) != 2 || !set.AuthIPRestrict || len(set.AuthAllowedIPs) != 2 ||
		set.Limit.Mode != store.RateLimitModeManual || set.Limit.MaxMessages != 200 || set.Limit.WindowSeconds != 1800 {
		t.Errorf("settings = %+v", set)
	}

	// The limit choice: nothing or "domain" means no limit of its own; auto
	// takes the level-1 window and a default multiplier.
	for _, v := range []url.Values{{"mode": {"wildcard"}}, {"mode": {"wildcard"}, "rl_mode": {"domain"}, "max_messages": {"50"}}} {
		if set, err := parse(v); err != nil || set.Limit.Mode != "" || set.Limit.MaxMessages != 0 {
			t.Errorf("%v = %+v, %v; want the domain limit", v, set.Limit, err)
		}
	}
	if set, err := parse(url.Values{"rl_mode": {"auto"}}); err != nil || set.Limit.Mode != store.RateLimitModeAuto ||
		set.Limit.AutoMultiplier != store.DefaultAutoMultiplier || set.Limit.WindowSeconds != 3600 {
		t.Errorf("auto = %+v, %v", set.Limit, err)
	}

	for name, v := range map[string]url.Values{
		"manual without a number":     {"rl_mode": {"manual"}},
		"manual above level 1":        {"rl_mode": {"manual"}, "max_messages": {"601"}},
		"manual with a zero window":   {"rl_mode": {"manual"}, "max_messages": {"10"}, "window_seconds": {"0"}},
		"an unknown limit mode":       {"rl_mode": {"always"}},
		"a multiplier out of range":   {"rl_mode": {"auto"}, "auto_multiplier": {"99"}},
		"the allow-list without IPs":  {"auth_ip_restrict": {"1"}},
		"something that is not an IP": {"auth_ip_restrict": {"1"}, "auth_allowed_ips": {"not-an-ip"}},
	} {
		if _, err := parse(v); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// One POST creates the application with its sender rule, client IPs and limit,
// and the response is the page with the password — shown once, never cached.
func TestApplicationCreateIsOnePost(t *testing.T) {
	s := newAppStand(t)
	rec := postFormAs(s.h.HandleApplicationCreate, globalPrincipal, "/outbound/domains/1/applications/new", s.paths, url.Values{
		"login": {"prod-server"}, "mode": {"list"}, "addresses": {"alerts@example.org"},
		"auth_ip_restrict": {"1"}, "auth_allowed_ips": {"203.0.113.10"},
		"rl_mode": {"manual"}, "max_messages": {"200"}, "window_seconds": {"3600"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d:\n%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q; the page shows a password once and must not be stored", got)
	}
	password := s.sasl["prod-server"]
	if password == "" || !strings.Contains(rec.Body.String(), password) {
		t.Errorf("the response does not show the generated password")
	}
	a, rl, ok := s.app(t, "prod-server")
	if a.AddressMode != store.AddressModeList || len(a.Addresses) != 1 || !a.AuthIPRestrict || len(a.AuthAllowedIPs) != 1 ||
		!ok || rl.MaxMessages != 200 || rl.WindowSeconds != 3600 {
		t.Errorf("application = %+v, limit %+v (%v)", a, rl, ok)
	}

	// The password page has no GET of its own: the form's address, read
	// again, shows the form and not the secret.
	again := postFormAs(s.h.HandleApplicationEdit, globalPrincipal, "/outbound/domains/1/applications/1",
		map[string]string{"id": idStr(s.d.ID), "aid": idStr(a.ID)}, nil)
	if strings.Contains(again.Body.String(), password) {
		t.Error("a later request shows the password again")
	}
}

// A form that does not validate creates nothing — whichever of its three parts
// is wrong, and although the other two are fine.
func TestApplicationCreateIsAllOrNothing(t *testing.T) {
	s := newAppStand(t)
	good := url.Values{
		"login": {"prod-server"}, "mode": {"wildcard"},
		"auth_ip_restrict": {"1"}, "auth_allowed_ips": {"203.0.113.10"},
		"rl_mode": {"manual"}, "max_messages": {"200"},
	}
	for name, change := range map[string][2]string{
		"a bad client IP":              {"auth_allowed_ips", "nope"},
		"a limit above level 1":        {"max_messages", "100000"},
		"an address of another domain": {"addresses", "a@evil.example"},
		"a bad login":                  {"login", "has space"},
	} {
		form := url.Values{}
		for k, v := range good {
			form[k] = v
		}
		form.Set(change[0], change[1])
		if change[0] == "addresses" {
			form.Set("mode", "list")
		}
		rec := postFormAs(s.h.HandleApplicationCreate, globalPrincipal, "/outbound/domains/1/applications/new", s.paths, form)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, rec.Code)
		}
	}
	apps, _ := s.h.store.ListApplicationsByDomain(s.d.ID)
	if len(apps) != 0 || len(s.sasl) != 0 {
		t.Fatalf("the refused forms left %d application(s) and %d SASL account(s)", len(apps), len(s.sasl))
	}
}

func TestApplicationSaveIsOnePost(t *testing.T) {
	s := newAppStand(t)
	a, pw, err := s.h.apps.CreateWithSettings(s.d.ID, "prod-server", app.Settings{
		Mode: store.AddressModeWildcard, Limit: app.Limit{Mode: store.RateLimitModeManual, MaxMessages: 100, WindowSeconds: 3600},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{"id": idStr(s.d.ID), "aid": idStr(a.ID)}
	save := func(form url.Values) int {
		return postFormAs(s.h.HandleApplicationSave, globalPrincipal, "/outbound/domains/1/applications/1", paths, form).Code
	}

	if code := save(url.Values{
		"mode": {"list"}, "addresses": {"alerts@example.org"},
		"auth_ip_restrict": {"1"}, "auth_allowed_ips": {"203.0.113.10"}, "rl_mode": {"domain"},
	}); code != http.StatusSeeOther {
		t.Fatalf("save = %d", code)
	}
	got, _, hasLimit := s.app(t, "prod-server")
	if got.AddressMode != store.AddressModeList || !got.AuthIPRestrict || hasLimit {
		t.Errorf("after the save: %+v, own limit: %v", got, hasLimit)
	}
	if s.sasl["prod-server"] != pw {
		t.Error("saving the form changed the password")
	}

	// One wrong part refuses the whole form and leaves all three as they were.
	if code := save(url.Values{
		"mode": {"wildcard"}, "rl_mode": {"manual"}, "max_messages": {"100000"},
	}); code != http.StatusBadRequest {
		t.Errorf("a limit above level 1 = %d, want 400", code)
	}
	if got, _, hasLimit := s.app(t, "prod-server"); got.AddressMode != store.AddressModeList || !got.AuthIPRestrict || hasLimit {
		t.Errorf("a refused form changed the application: %+v, own limit: %v", got, hasLimit)
	}

	// The login is not renamed through this form.
	if code := save(url.Values{"login": {"renamed"}, "mode": {"wildcard"}}); code != http.StatusBadRequest {
		t.Errorf("a changed login = %d, want 400", code)
	}
	if _, err := s.h.store.GetApplicationByLogin("renamed"); err == nil {
		t.Error("the application was renamed")
	}
}

// A new password is the response to its POST, shown once and never cached.
func TestRegeneratedPasswordIsNotCached(t *testing.T) {
	s := newAppStand(t)
	a, old, err := s.h.apps.CreateWithSettings(s.d.ID, "prod-server", app.Settings{Mode: store.AddressModeWildcard}, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{"id": idStr(s.d.ID), "aid": idStr(a.ID)}
	rec := postFormAs(s.h.HandleRegenPassword, globalPrincipal, "/outbound/domains/1/applications/1/password", paths, url.Values{})
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("new password = %d, Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	now := s.sasl["prod-server"]
	if now == old || !strings.Contains(rec.Body.String(), now) {
		t.Error("the response does not show the new password")
	}
}
