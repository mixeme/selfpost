package view

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func renderSignedOut(t *testing.T, page string, data any) string {
	t.Helper()
	engine, err := New("9.9.9-test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	engine.Render(rec, http.StatusOK, page, data)
	if rec.Code != http.StatusOK {
		t.Fatalf("render %s: status %d: %s", page, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestLoginFormPostsToLoginWithItsFields(t *testing.T) {
	out := renderSignedOut(t, "login", NewLogin("mail.example.org", ""))
	for _, want := range []string{
		`action="/login"`, `method="post"`,
		`name="username"`, `autocomplete="username"`, `autofocus`,
		`name="password"`, `autocomplete="current-password"`,
		`mail.example.org`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("login page is missing %q", want)
		}
	}
	if strings.Contains(out, "notification") {
		t.Error("an untouched login form shows an error")
	}
}

func TestLoginShowsAnErrorEscaped(t *testing.T) {
	out := renderSignedOut(t, "login", NewLogin("", `bad <script>alert(1)</script>`))
	if !strings.Contains(out, "notification is-danger is-light") {
		t.Error("the refusal is not shown as the error flash")
	}
	if strings.Contains(out, "<script>alert(1)") || !strings.Contains(out, "&lt;script&gt;alert(1)") {
		t.Errorf("the refusal is not escaped:\n%s", out)
	}
	if strings.Contains(out, `class="sp-kicker"`) {
		t.Error("a kicker is shown although the host name is not known")
	}
}

func TestLoginWithoutAnAdministratorHasNoForm(t *testing.T) {
	out := renderSignedOut(t, "login", NewLoginSetupHint("mail.example.org"))
	if !strings.Contains(out, "No administrator yet") || !strings.Contains(out, "one-time setup link") {
		t.Error("the setup hint is missing")
	}
	// Nothing on it can be filled in or sent.
	for _, bad := range []string{`<form`, `action=`, `<input`, `<button`, `name="password"`, `name="username"`} {
		if strings.Contains(out, bad) {
			t.Errorf("the hint page carries %q", bad)
		}
	}
}

func TestSetupFormPostsToItsOneTimeLink(t *testing.T) {
	out := renderSignedOut(t, "setup", NewSetup("abc123", ""))
	for _, want := range []string{
		`action="/setup/abc123"`, `method="post"`,
		`name="username"`, `autocomplete="username"`, `autofocus`,
		`name="password"`, `name="password_confirm"`, `autocomplete="new-password"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("setup page is missing %q", want)
		}
	}
	if strings.Contains(out, "notification") {
		t.Error("an untouched setup form shows an error")
	}
}

func TestSetupShowsAnErrorEscaped(t *testing.T) {
	out := renderSignedOut(t, "setup", NewSetup("t", `Passwords <b>do not</b> match.`))
	if !strings.Contains(out, "notification is-danger is-light") {
		t.Error("the refusal is not shown as the error flash")
	}
	if strings.Contains(out, "<b>do not</b>") {
		t.Errorf("the refusal is not escaped:\n%s", out)
	}
}
