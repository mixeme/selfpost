package handlers

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
	"golang.org/x/crypto/bcrypt"
)

func TestSettingsPageShowsSendLogRetention(t *testing.T) {
	h, _ := settingsServer(t)
	if err := h.store.SetSendLogRetentionDays(45); err != nil {
		t.Fatalf("SetSendLogRetentionDays: %v", err)
	}

	out := getBody(t, h.HandleServerSettings, "/server/settings")
	for _, want := range []string{
		`id="deliveries-retention"`,
		`name="send_log_retention_days"`,
		`value="45"`,
		"Send log retention",
		`action="/server/settings"`,
		`id="rate-limits"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("settings page missing %q:\n%s", want, out)
		}
	}
	// What belongs to a user is not on the instance's page.
	for _, gone := range []string{`name="current_password"`, `name="username"`, `name="dmarc_default"`} {
		if strings.Contains(out, gone) {
			t.Errorf("the instance settings page still carries the user's %s", gone)
		}
	}
}

// The instance settings ask for no password: they are the server's, and the
// page is behind the global role.
func TestSubmitSettingsSavesSendLogRetention(t *testing.T) {
	h, _ := settingsServer(t)
	rec := postFormAs(h.HandleServerSettings, globalPrincipal, "/server/settings", nil, url.Values{"send_log_retention_days": {"120"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/server/settings?saved=1" {
		t.Fatalf("POST /server/settings = %d to %q, want 303:\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	got, err := h.store.GetSendLogRetentionDays(90)
	if err != nil {
		t.Fatalf("GetSendLogRetentionDays: %v", err)
	}
	if got != 120 {
		t.Fatalf("retention = %d, want 120", got)
	}
}

func TestSubmitSettingsRejectsOutOfRangeRetention(t *testing.T) {
	h, _ := settingsServer(t)
	for _, bad := range []string{"3", "nine", ""} {
		rec := postFormAs(h.HandleServerSettings, globalPrincipal, "/server/settings", nil, url.Values{"send_log_retention_days": {bad}})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("POST /server/settings (%q) = %d, want 400:\n%s", bad, rec.Code, rec.Body.String())
		}
	}
	rec := postFormAs(h.HandleServerSettings, globalPrincipal, "/server/settings", nil, url.Values{"send_log_retention_days": {"3"}})
	if !strings.Contains(rec.Body.String(), "between 7 and 365") {
		t.Errorf("expected range error in body:\n%s", rec.Body.String())
	}
	if got, _ := h.store.GetSendLogRetentionDays(90); got != 90 {
		t.Errorf("a refused value changed the retention to %d", got)
	}
}

func TestServerSettingsIsGlobalOnly(t *testing.T) {
	h, _ := settingsServer(t)
	d, err := h.store.AddDomain("example.com", "mail")
	if err != nil {
		t.Fatal(err)
	}
	p := domainAdmin(t, h.store, "ops", d.ID)
	if rec := postFormAs(h.HandleServerSettings, p, "/server/settings", nil, url.Values{"send_log_retention_days": {"120"}}); rec.Code != http.StatusNotFound {
		t.Errorf("POST /server/settings as a domain administrator = %d, want 404", rec.Code)
	}
	if got, _ := h.store.GetSendLogRetentionDays(90); got != 90 {
		t.Errorf("a domain administrator changed the retention to %d", got)
	}
}

func settingsServer(t *testing.T) (*Handlers, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	const password = "correct-password-here!"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := st.CreateUser("admin", string(hash), store.RoleGlobal, store.Reach{}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	v := mustView(t)
	a := auth.New(st, auth.Config{}, v, filepath.Join(t.TempDir(), "setup-token"))
	return &Handlers{
		store: st,
		view:  v,
		auth:  a,
		dns:   dnscheck.New(nil),
		cfg:   Config{SendLogRetentionEnvDefault: 90},
	}, password
}
