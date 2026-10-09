package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/domain"
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

// The domain form's "Same as Settings" is, on the 2.0 schema, a domain that
// follows the default of the user who chose it; every other choice gives the
// domain an address of its own and stops it following anyone.
func TestDomainDMARCFollowsTheUserWhoChoseInherit(t *testing.T) {
	h, _ := settingsServer(t)
	h.domains = domain.NewService(h.store, nil, nil, "selfpost")
	d, err := h.store.AddDomain("example.com", "selfpost")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	admin, err := h.store.GetUserByUsername("admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	p := auth.Principal{ID: admin.ID, Username: "admin", Role: auth.RoleGlobal}
	if err := h.store.UpdateUser(admin.ID, "admin", admin.PasswordHash, "mix@example.org"); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if err := h.store.SetDMARCDefault(admin.ID, store.DMARCDefaultAccount, ""); err != nil {
		t.Fatalf("SetDMARCDefault: %v", err)
	}
	save := func(values url.Values) store.Domain {
		t.Helper()
		rec := postFormAs(h.HandleDomainDMARC, p, "/domains/1/dmarc", map[string]string{"id": "1"}, values)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("POST %v = %d, want 303:\n%s", values, rec.Code, rec.Body.String())
		}
		got, err := h.store.GetDomain(d.ID)
		if err != nil {
			t.Fatalf("GetDomain: %v", err)
		}
		return got
	}

	got := save(url.Values{"dmarc_rua_mode": {"inherit"}})
	if !got.DMARCRuaUserID.Valid || got.DMARCRuaUserID.Int64 != admin.ID {
		t.Fatalf("inherit: the domain follows %v, want user %d", got.DMARCRuaUserID, admin.ID)
	}
	if rua, err := h.domainReportAddress(got); err != nil || rua != "mix@example.org" {
		t.Fatalf("inherit resolves to %q, %v; want the user's account e-mail", rua, err)
	}

	got = save(url.Values{"dmarc_rua_mode": {"custom"}, "dmarc_rua_email": {"dmarc@hub.example"}})
	if got.DMARCRuaUserID.Valid || got.DMARCRua != "dmarc@hub.example" {
		t.Fatalf("custom: %q / follows %v", got.DMARCRua, got.DMARCRuaUserID)
	}

	got = save(url.Values{"dmarc_rua_mode": {"none"}})
	if got.DMARCRuaUserID.Valid || got.DMARCRua != "" {
		t.Fatalf("none: %q / follows %v", got.DMARCRua, got.DMARCRuaUserID)
	}

}

// Until the Account page replaces the old Settings form, its single address
// field is the account e-mail and switches the user's DMARC default between
// "my account e-mail" and "none". Nothing is written to the instance settings.
func TestSettingsFormMapsToAccountEmailAndDefault(t *testing.T) {
	h, password := settingsServer(t)
	submit := func(email string) store.User {
		t.Helper()
		rec := postFormAs(h.HandleSettings, globalPrincipal, "/settings", nil, url.Values{
			"username": {"admin"}, "current_password": {password}, "dmarc_report_email": {email},
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("POST /settings (%q) = %d, want 303:\n%s", email, rec.Code, rec.Body.String())
		}
		u, err := h.store.GetUser(globalPrincipal.ID)
		if err != nil {
			t.Fatalf("GetUser: %v", err)
		}
		return u
	}

	u := submit("mix@example.org")
	if u.Email != "mix@example.org" || u.DMARCDefaultMode != store.DMARCDefaultAccount {
		t.Fatalf("after setting the address: e-mail %q, default %q", u.Email, u.DMARCDefaultMode)
	}
	if v, err := h.store.GetSetting("dmarc_report_email"); err == nil && v != "" {
		t.Fatalf("the address was mirrored into the instance settings: %q", v)
	}

	u = submit("")
	if u.Email != "" || u.DMARCDefaultMode != store.DMARCDefaultNone {
		t.Fatalf("after clearing the address: e-mail %q, default %q", u.Email, u.DMARCDefaultMode)
	}
}

// A new domain takes its report address from whoever created it.
func TestFollowCreator(t *testing.T) {
	h, _ := settingsServer(t)
	d, err := h.store.AddDomain("example.com", "selfpost")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	h.followCreator(d.ID, globalPrincipal.ID)
	got, err := h.store.GetDomain(d.ID)
	if err != nil {
		t.Fatalf("GetDomain: %v", err)
	}
	if !got.DMARCRuaUserID.Valid || got.DMARCRuaUserID.Int64 != globalPrincipal.ID {
		t.Fatalf("the domain follows %v, want its creator (user %d)", got.DMARCRuaUserID, globalPrincipal.ID)
	}
	// The creator's default is "none" until they choose one, so the new
	// domain publishes a policy-only record — as a fresh 1.x install did.
	if rua, err := h.domainReportAddress(got); err != nil || rua != "" {
		t.Fatalf("resolved %q, %v; want no report address yet", rua, err)
	}
}
