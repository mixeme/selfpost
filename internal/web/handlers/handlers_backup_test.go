package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/backup"
	"github.com/mixeme/selfpost/internal/secretfile"
	"github.com/mixeme/selfpost/internal/web/validate"
)

// postForm builds the kind of request the backup and export forms submit.
func postForm(values url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/server/backup", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

// The encryption password is only ever typed once into a file nobody can
// recover without it, so every way of getting it wrong has to be caught before
// the archive is sealed — and leaving the box unticked has to keep producing
// the plain archive earlier versions produced.
func TestSecretFilePassword(t *testing.T) {
	long := strings.Repeat("x", validate.MinSecretFilePasswordLen)
	short := strings.Repeat("x", validate.MinSecretFilePasswordLen-1)

	tests := []struct {
		name     string
		form     url.Values
		wantPass string
		wantErr  bool
	}{
		{
			name:     "unticked box means no encryption",
			form:     url.Values{"password": {long}, "password_confirm": {long}},
			wantPass: "",
		},
		{
			name:     "ticked with a matching password",
			form:     url.Values{"encrypt": {"1"}, "password": {long}, "password_confirm": {long}},
			wantPass: long,
		},
		{
			name:    "mistyped confirmation",
			form:    url.Values{"encrypt": {"1"}, "password": {long}, "password_confirm": {long + "!"}},
			wantErr: true,
		},
		{
			name:    "too short",
			form:    url.Values{"encrypt": {"1"}, "password": {short}, "password_confirm": {short}},
			wantErr: true,
		},
		{
			name:    "ticked but empty",
			form:    url.Values{"encrypt": {"1"}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pass, errMsg := secretFilePassword(postForm(tt.form))
			if tt.wantErr {
				if errMsg == "" {
					t.Fatalf("password %q accepted, want a rejection", pass)
				}
				if pass != "" {
					t.Errorf("a rejected form still yielded password %q", pass)
				}
				return
			}
			if errMsg != "" {
				t.Fatalf("unexpected rejection: %s", errMsg)
			}
			if pass != tt.wantPass {
				t.Errorf("password = %q, want %q", pass, tt.wantPass)
			}
		})
	}
}

// The messages the import form shows must distinguish the operator's likely
// mistakes; a wrong password and a tampered file stay deliberately merged.
func TestDecryptErrorMessage(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{secretfile.ErrWrongPassword, "Wrong password"},
		{fmt.Errorf("read: %w", secretfile.ErrCorrupt), "damaged"},
		{secretfile.ErrNotEncrypted, "not a SelfPost export"},
		{errors.New("something else"), "Could not decrypt"},
	}
	for _, tt := range tests {
		if got := decryptErrorMessage(tt.err); !strings.Contains(got, tt.want) {
			t.Errorf("decryptErrorMessage(%v) = %q, want it to mention %q", tt.err, got, tt.want)
		}
	}
}

// The encryption controls are shared markup pulled into two pages; a page that
// forgets to include the partial (or the data it needs) loses the option
// silently, since the plain download still works.
func TestBackupPageOffersEncryption(t *testing.T) {
	h := &Handlers{view: mustView(t), cfg: Config{Version: "test"}}
	rec := httptest.NewRecorder()
	h.renderBackupPageWith(rec, httptest.NewRequest(http.MethodGet, "/server/backup", nil),
		http.StatusOK, "", "The two passwords do not match.")

	body := rec.Body.String()
	for _, want := range []string{
		`name="encrypt"`, `name="password"`, `name="password_confirm"`,
		`name="import_password"`, "data-encrypt-toggle", "data-encrypt-fields",
		"data-import-password-fields",
		fmt.Sprintf("at least %d characters", validate.MinSecretFilePasswordLen),
		"The two passwords do not match.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("backup page is missing %q", want)
		}
	}
}

// An empty certs/ is not "could not read": the page says what is missing and
// where it goes (review of audit stage 3).
func TestEmptyCertsMessage(t *testing.T) {
	msg := unreadableBackupErr(fmt.Errorf("check: %w", backup.ErrNoCerts), "/selfpost-deploy")
	if !strings.Contains(msg, "certs/ holds no certificate") || !strings.Contains(msg, "fullchain.pem") {
		t.Errorf("message = %q", msg)
	}
}

// A session is not enough for a secret download (backup-reauth): the full
// backup and a domain's export ask for the signed-in user's own password.
// Wrong or missing, nothing is sent and the page says so; right, the request
// goes on to its own checks. Guesses share the password change's limiter.
func TestSecretDownloadsAskForTheAccountPassword(t *testing.T) {
	s := newAppStand(t)
	exportPath := "/outbound/domains/" + idStr(s.d.ID) + "/settings/export"
	for _, pw := range []string{"", "wrong-password-entirely"} {
		rec := postFormAs(s.h.HandleExportDomain, globalPrincipal, exportPath, s.paths, url.Values{"account_password": {pw}})
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("Content-Disposition") != "" ||
			!strings.Contains(rec.Body.String(), "Your account password is incorrect.") {
			t.Fatalf("export with account password %q = %d, %q:\n%.600s", pw, rec.Code, rec.Header().Get("Content-Disposition"), rec.Body.String())
		}
		rec = postFormAs(s.h.HandleBackup, globalPrincipal, "/server/backup", nil, url.Values{"account_password": {pw}})
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("Content-Disposition") != "" ||
			!strings.Contains(rec.Body.String(), "Your account password is incorrect.") ||
			!strings.Contains(rec.Body.String(), `name="account_password" type="password" autocomplete="current-password" required`) {
			t.Fatalf("backup with account password %q = %d:\n%.600s", pw, rec.Code, rec.Body.String())
		}
	}

	// The right password lets the export through: a plain JSON download.
	rec := postFormAs(s.h.HandleExportDomain, globalPrincipal, exportPath, s.paths, url.Values{"account_password": {"correct-password-here!"}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), "selfpost-domain-example.org.json") {
		t.Fatalf("export with the right password = %d, %q:\n%.600s", rec.Code, rec.Header().Get("Content-Disposition"), rec.Body.String())
	}

	// Five checks were made above, and the limiter allows ten in its window:
	// after six more, a check is refused before the password is looked at,
	// the right one included.
	for i := 0; i < 6; i++ {
		postFormAs(s.h.HandleBackup, globalPrincipal, "/server/backup", nil, url.Values{"account_password": {"guess"}})
	}
	rec = postFormAs(s.h.HandleExportDomain, globalPrincipal, exportPath, s.paths, url.Values{"account_password": {"correct-password-here!"}})
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "Too many attempts.") {
		t.Fatalf("after ten checks = %d:\n%.600s", rec.Code, rec.Body.String())
	}
}
