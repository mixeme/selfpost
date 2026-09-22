package handlers

import (
	"net/http"
	"net/http/httptest"
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

	out := getBody(t, h.HandleSettings, "/settings")
	for _, want := range []string{
		`id="deliveries-retention"`,
		`name="send_log_retention_days"`,
		`value="45"`,
		"Send log retention",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("settings page missing %q:\n%s", want, out)
		}
	}
}

func TestSubmitSettingsSavesSendLogRetention(t *testing.T) {
	h, password := settingsServer(t)

	values := url.Values{
		"username":                {"admin"},
		"current_password":        {password},
		"send_log_retention_days": {"120"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = auth.RequestWithPrincipal(req, globalPrincipal)

	rec := httptest.NewRecorder()
	h.HandleSettings(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /settings = %d, want 303:\n%s", rec.Code, rec.Body.String())
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
	h, password := settingsServer(t)

	values := url.Values{
		"username":                {"admin"},
		"current_password":        {password},
		"send_log_retention_days": {"3"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = auth.RequestWithPrincipal(req, globalPrincipal)

	rec := httptest.NewRecorder()
	h.HandleSettings(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /settings = %d, want 400:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "between 7 and 365") {
		t.Errorf("expected range error in body:\n%s", rec.Body.String())
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
	if _, err := st.CreateUser("admin", string(hash), store.RoleGlobal, nil); err != nil {
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
