package handlers

import (
	"bytes"
	"encoding/json"
	"html"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/secretfile"
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
			Domain:       "example.com",
			DKIMSelector: "mail.2026",
			DMARCRua:     "reports@example.com",
			RateLimit:    &domain.RateLimitExport{Mode: store.RateLimitModeManual, MaxMessages: 10, WindowSeconds: 3600},
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
	// The domain's own hosted address is its own.
	own := good()
	own.DMARCRua = "dmarc-reports+example.com@mail.example.org"
	if msg := h.checkDomainImport(&own); msg != "" {
		t.Fatalf("the domain's own hosted address refused: %s", msg)
	}
	if got := strings.Join(exp.Applications[0].AuthAllowedIPs, " "); got != "192.0.2.1 2001:db8::1" {
		t.Errorf("client addresses stored as %q, want as the form stores them", got)
	}

	// An auto limit's numbers are worked out here, not taken from the file,
	// and its mode is stored trimmed.
	exp = good()
	exp.Applications[0].RateLimit = &domain.RateLimitExport{Mode: " auto ", MaxMessages: 1, WindowSeconds: 86400}
	if msg := h.checkDomainImport(&exp); msg != "" {
		t.Fatalf("an auto limit with numbers refused: %s", msg)
	}
	if rl := exp.Applications[0].RateLimit; rl.Mode != store.RateLimitModeAuto || rl.MaxMessages != 0 || rl.WindowSeconds != 0 {
		t.Errorf("the auto limit kept %+v", rl)
	}

	for _, c := range []struct {
		name   string
		break_ func(*domain.DomainExport)
		want   string
	}{
		{"selector", func(e *domain.DomainExport) { e.DKIMSelector = "mail;x=y" },
			"Invalid DKIM selector in export file: selector: domain may contain only"},
		{"long selector", func(e *domain.DomainExport) { e.DKIMSelector = strings.Repeat("a", 64) },
			"Invalid DKIM selector in export file: selector must be at most 63 characters"},
		{"report address", func(e *domain.DomainExport) { e.DMARCRua = "not an address" },
			"Invalid report address in export file: "},
		// As on Domain settings (security audit 2026-10-17, W1).
		{"report address on the hostname", func(e *domain.DomainExport) { e.DMARCRua = "ops@MAIL.example.org" },
			"The export's report address is on this server's own name"},
		{"another domain's hosted address", func(e *domain.DomainExport) { e.DMARCRua = "dmarc-reports+victim.example@mail.example.org" },
			"The export's report address is on this server's own name"},
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

// A domain export cut short or altered on the way is refused with what is
// wrong with it, before anything is created (audit 2026-10-12, stage 3).
func TestDomainImportOfADamagedFileCreatesNothing(t *testing.T) {
	h, st := inboundHandlers(t)
	root := userWith(t, st, "root", store.RoleGlobal, store.Reach{})
	plain, _ := json.Marshal(domain.DomainExport{
		Format: domain.FormatDomainExport, Domain: "example.com", DKIMSelector: "mail",
		Applications: []domain.AppExport{{Login: "app1", AddressMode: store.AddressModeWildcard, Password: "imported-password"}},
	})
	const password = "a-long-enough-password"
	var sealed bytes.Buffer
	w, err := secretfile.NewWriter(&sealed, secretfile.TypeDomainExport, password)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(plain)
	w.Close()
	flipped := bytes.Clone(sealed.Bytes())
	flipped[len(flipped)-20] ^= 1

	for _, c := range []struct {
		name, file, password string
		body                 []byte
		want                 string
	}{
		{"plain, cut", "example.com.json", "", plain[:len(plain)/2], "That file is not a valid SelfPost domain export."},
		{"encrypted, cut", "example.com.spde", password, sealed.Bytes()[:sealed.Len()-10], "That file is damaged or incomplete."},
		{"encrypted, altered", "example.com.spde", password, flipped, "Wrong password, or the file has been altered since it was exported."},
	} {
		t.Run(c.name, func(t *testing.T) {
			var body bytes.Buffer
			mw := multipart.NewWriter(&body)
			fw, _ := mw.CreateFormFile("file", c.file)
			fw.Write(c.body)
			mw.WriteField("import_password", c.password)
			mw.Close()
			req := httptest.NewRequest("POST", "/server/backup/import", &body)
			req.Header.Set("Content-Type", mw.FormDataContentType())
			req = auth.RequestWithPrincipal(req, root)
			rec := httptest.NewRecorder()
			h.HandleImportDomain(rec, req)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), html.EscapeString(c.want)) {
				t.Fatalf("import = %d, want 400 saying %q:\n%s", rec.Code, c.want, rec.Body.String())
			}
			if domains, _ := st.ListDomains(); len(domains) != 0 {
				t.Fatalf("the refused import created %v", domains)
			}
			if _, err := st.GetApplicationByLogin("app1"); err == nil {
				t.Fatal("the refused import created the application")
			}
		})
	}
}
