package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mixeme/selfpost/internal/secretfile"
)

// postForm builds the kind of request the backup and export forms submit.
func postForm(values url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/backup", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

// The encryption password is only ever typed once into a file nobody can
// recover without it, so every way of getting it wrong has to be caught before
// the archive is sealed — and leaving the box unticked has to keep producing
// the plain archive earlier versions produced.
func TestSecretFilePassword(t *testing.T) {
	long := strings.Repeat("x", minSecretFilePasswordLen)
	short := strings.Repeat("x", minSecretFilePasswordLen-1)

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
	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	s := &Server{tmpl: tmpl, cfg: Config{Version: "test"}}
	rec := httptest.NewRecorder()
	s.renderBackupPageWith(rec, httptest.NewRequest(http.MethodGet, "/backup", nil),
		http.StatusOK, "", "The two passwords do not match.")

	body := rec.Body.String()
	for _, want := range []string{
		`name="encrypt"`, `name="password"`, `name="password_confirm"`,
		`name="import_password"`, "data-encrypt-toggle", "data-encrypt-fields",
		fmt.Sprintf("at least %d characters", minSecretFilePasswordLen),
		"The two passwords do not match.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("backup page is missing %q", want)
		}
	}
}
