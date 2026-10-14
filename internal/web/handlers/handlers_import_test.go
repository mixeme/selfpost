package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
)

// A domain export is held to the rules the forms hold the same settings to,
// field by field, and refused in the validator's words (audit 2026-10-12,
// M10).
func TestDomainImportIsCheckedLikeTheForms(t *testing.T) {
	h, _ := inboundHandlers(t)
	good := func() domain.DomainExport {
		return domain.DomainExport{
			Domain:    "example.com",
			DMARCRua:  "reports@example.com",
			RateLimit: &domain.RateLimitExport{Mode: store.RateLimitModeManual, MaxMessages: 10, WindowSeconds: 3600},
			Applications: []domain.AppExport{{
				Login: "app1", AddressMode: store.AddressModeWildcard, Password: "imported-password",
				AuthIPRestrict: true, AuthAllowedIPs: []string{"192.0.2.1", " 2001:DB8::1 "},
				RateLimit: &domain.RateLimitExport{Mode: store.RateLimitModeAuto, AutoMultiplier: 2},
			}},
		}
	}
	exp := good()
	if msg := h.checkDomainImport(&exp); msg != "" {
		t.Fatalf("a good export refused: %s", msg)
	}
	if got := strings.Join(exp.Applications[0].AuthAllowedIPs, " "); got != "192.0.2.1 2001:db8::1" {
		t.Errorf("client addresses stored as %q, want as the form stores them", got)
	}

	for _, c := range []struct {
		name   string
		break_ func(*domain.DomainExport)
		want   string
	}{
		{"report address", func(e *domain.DomainExport) { e.DMARCRua = "not an address" },
			"Invalid report address in export file: "},
		{"no window", func(e *domain.DomainExport) { e.RateLimit.WindowSeconds = 0 },
			"Invalid rate limit for the domain in export file: enter a time window greater than zero seconds"},
		{"no messages", func(e *domain.DomainExport) { e.RateLimit.MaxMessages = 0 },
			"Invalid rate limit for the domain in export file: enter a message limit greater than zero"},
		{"over the backstop", func(e *domain.DomainExport) { e.RateLimit.MaxMessages = 1 << 20 },
			"Invalid rate limit for the domain in export file: message limit cannot exceed the level-1 backstop"},
		{"mode", func(e *domain.DomainExport) { e.RateLimit.Mode = "sometimes" },
			"Invalid rate limit for the domain in export file: choose manual or auto mode"},
		{"multiplier", func(e *domain.DomainExport) { e.Applications[0].RateLimit.AutoMultiplier = 1000 },
			"Invalid rate limit for app1 in export file: multiplier must be between"},
		{"client address", func(e *domain.DomainExport) { e.Applications[0].AuthAllowedIPs = []string{"not-an-ip"} },
			`Invalid client addresses for app1 in export file: "not-an-ip" is not a valid IP address`},
		{"allow-list on, empty", func(e *domain.DomainExport) { e.Applications[0].AuthAllowedIPs = nil },
			"Invalid client addresses for app1 in export file: enter at least one client IP when the allow-list is enabled"},
		{"legacy client address", func(e *domain.DomainExport) {
			e.Applications[0].AuthAllowedIPs = nil
			e.Applications[0].RateLimit = &domain.RateLimitExport{AllowedIPs: []string{"300.1.1.1"}}
		}, `Invalid client addresses for app1 in export file: "300.1.1.1" is not a valid IP address`},
	} {
		t.Run(c.name, func(t *testing.T) {
			exp := good()
			c.break_(&exp)
			if msg := h.checkDomainImport(&exp); !strings.HasPrefix(msg, c.want) {
				t.Fatalf("got %q, want it to start with %q", msg, c.want)
			}
		})
	}
}

// The check runs before anything is created: a refused file leaves no domain.
func TestDomainImportRefusedCreatesNothing(t *testing.T) {
	h, st := inboundHandlers(t)
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	raw, _ := json.Marshal(domain.DomainExport{
		Format: domain.FormatDomainExport, Domain: "example.com", DKIMSelector: "mail",
		RateLimit: &domain.RateLimitExport{Mode: store.RateLimitModeManual, MaxMessages: 10, WindowSeconds: 0},
	})
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "example.com.json")
	fw.Write(raw)
	mw.Close()
	req := httptest.NewRequest("POST", "/server/backup/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = auth.RequestWithPrincipal(req, root)
	rec := httptest.NewRecorder()
	h.HandleImportDomain(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "enter a time window greater than zero seconds") {
		t.Fatalf("import of a limit with no window = %d:\n%s", rec.Code, rec.Body.String())
	}
	if domains, _ := st.ListDomains(); len(domains) != 0 {
		t.Fatalf("the refused import created %v", domains)
	}
}
