package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

const testPassword = "correct-horse-battery"

// moduleWithAdmin returns a panel that has already been through setup, with one
// global administrator whose password is testPassword.
func moduleWithAdmin(t *testing.T) *Module {
	t.Helper()
	m := testModule(t, false)
	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := m.store.CreateGlobalUser("admin", string(hash)); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return m
}

// postLogin submits the sign-in form from remoteAddr (the limiter's key) and
// returns what the handler wrote.
func postLogin(m *Module, remoteAddr, username, password string) *httptest.ResponseRecorder {
	form := url.Values{"username": {username}, "password": {password}}
	r := httptest.NewRequest(http.MethodPost, "http://panel.example.com/login",
		strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	m.HandleLogin(rec, r)
	return rec
}

// sessionCookieValue returns the session token the response issued, or "" if it
// issued none.
func sessionCookieValue(t *testing.T, m *Module, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == m.sessionCookie() {
			return c.Value
		}
	}
	return ""
}

func TestLoginSignsInWithTheRightPassword(t *testing.T) {
	m := moduleWithAdmin(t)

	rec := postLogin(m, "203.0.113.7:5000", "admin", testPassword)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("status = %d, Location = %q; want a redirect to /", rec.Code, rec.Header().Get("Location"))
	}
	token := sessionCookieValue(t, m, rec)
	if token == "" {
		t.Fatal("no session cookie was issued")
	}
	name, ok := m.sessions.Lookup(token)
	if !ok || name != "admin" {
		t.Fatalf("the cookie's session resolves to %q, %t; want admin", name, ok)
	}
}

// A refused sign-in must not say which half was wrong: the panel is public, and
// distinguishable answers would turn the form into a list of usernames.
func TestLoginRefusesBadCredentialsWithoutSayingWhy(t *testing.T) {
	m := moduleWithAdmin(t)

	bodies := make(map[string]string, 2)
	for name, creds := range map[string][2]string{
		"wrong password": {"admin", "not-the-password"},
		"unknown user":   {"nobody", testPassword},
	} {
		rec := postLogin(m, "203.0.113.7:5000", creds[0], creds[1])
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, rec.Code)
		}
		if got := sessionCookieValue(t, m, rec); got != "" {
			t.Errorf("%s: a session cookie was issued: %q", name, got)
		}
		bodies[name] = rec.Body.String()
	}
	if bodies["wrong password"] != bodies["unknown user"] {
		t.Error("the two refusals differ, so the form tells an attacker which usernames exist")
	}
}

// The lockout is what makes online guessing pointless, so it has to hold even
// for the request that finally carries the right password — and it has to be
// scoped to the address that spent the attempts.
func TestLoginLocksOutAfterTooManyAttempts(t *testing.T) {
	m := moduleWithAdmin(t)
	const attacker = "203.0.113.7:5000"

	for i := 0; i < 10; i++ {
		if rec := postLogin(m, attacker, "admin", "guess"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401 (still under the limit)", i+1, rec.Code)
		}
	}

	rec := postLogin(m, attacker, "admin", testPassword)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429; the lockout was bypassed by guessing right", rec.Code)
	}
	if got := sessionCookieValue(t, m, rec); got != "" {
		t.Errorf("a locked-out request was signed in: %q", got)
	}

	if rec := postLogin(m, "198.51.100.9:5000", "admin", testPassword); rec.Code != http.StatusSeeOther {
		t.Errorf("another address got %d; one guesser locked out the whole internet", rec.Code)
	}
}

// Before the first administrator exists there is nothing to sign in as, so the
// form is replaced by a pointer to the setup link rather than a password box
// that can never succeed.
func TestLoginPointsAtSetupBeforeTheFirstAdministrator(t *testing.T) {
	m := testModule(t, false)

	rec := httptest.NewRecorder()
	m.HandleLogin(rec, httptest.NewRequest(http.MethodGet, "http://panel.example.com/login", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "No administrator has been created yet") {
		t.Errorf("the login page does not point at the setup link:\n%s", body)
	}
	if strings.Contains(body, `name="password"`) {
		t.Errorf("the login page offers a password field with no account to use it:\n%s", body)
	}
}

// getSetup performs the GET the operator's browser makes when it follows the
// one-time link.
func getSetup(m *Module, token string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	m.HandleSetup(rec, httptest.NewRequest(http.MethodGet, "http://panel.example.com/setup/"+token, nil))
	return rec
}

func postSetup(m *Module, token string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "http://panel.example.com/setup/"+token,
		strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	m.HandleSetup(rec, r)
	return rec
}

func setupForm(username, password, confirm string) url.Values {
	return url.Values{
		"username":         {username},
		"password":         {password},
		"password_confirm": {confirm},
	}
}

// The setup link creates the first global administrator and then stops
// existing: the persistent fact is the user row, so the link is dead after a
// restart too, not only for the process that served it.
func TestSetupCreatesTheFirstAdministratorAndThenCloses(t *testing.T) {
	m := testModule(t, false)
	token, ok := m.setup.activeToken()
	if !ok {
		t.Fatal("no setup token on a panel with no users")
	}

	if rec := getSetup(m, token); rec.Code != http.StatusOK {
		t.Fatalf("GET the setup link = %d, want the form", rec.Code)
	}

	rec := postSetup(m, token, setupForm("operator", "a-long-enough-password", "a-long-enough-password"))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("status = %d, Location = %q; want a redirect to /login", rec.Code, rec.Header().Get("Location"))
	}

	u, err := m.store.GetUserByUsername("operator")
	if err != nil {
		t.Fatalf("the administrator was not created: %v", err)
	}
	if u.Role != RoleGlobal {
		t.Errorf("the first administrator has role %q, want global", u.Role)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("a-long-enough-password")); err != nil {
		t.Errorf("the stored hash does not match the password that was set: %v", err)
	}

	if rec := getSetup(m, token); rec.Code != http.StatusNotFound {
		t.Errorf("the setup link still answers %d after setup completed, want 404", rec.Code)
	}
	if rec := postSetup(m, token, setupForm("second", "a-long-enough-password", "a-long-enough-password")); rec.Code != http.StatusNotFound {
		t.Errorf("a second administrator could be created through the setup link (%d)", rec.Code)
	}
}

// A token that is wrong, or one that has aged out and been replaced, is not a
// hint that setup exists: both answer 404, the same as any unknown path.
func TestSetupRejectsAWrongOrExpiredToken(t *testing.T) {
	m := testModule(t, false)
	token, ok := m.setup.activeToken()
	if !ok {
		t.Fatal("no setup token on a panel with no users")
	}

	if rec := getSetup(m, token+"x"); rec.Code != http.StatusNotFound {
		t.Errorf("a wrong token answered %d, want 404", rec.Code)
	}

	expireSetupToken(m)

	if rec := getSetup(m, token); rec.Code != http.StatusNotFound {
		t.Errorf("the expired token still opens setup (%d)", rec.Code)
	}
	fresh, _ := m.setup.activeToken()
	if fresh == token {
		t.Fatal("the expired token was not replaced")
	}
	if rec := getSetup(m, fresh); rec.Code != http.StatusOK {
		t.Errorf("the reissued token does not open setup (%d)", rec.Code)
	}
}

// The first account is the one that can never be locked out of the panel from
// outside, so the rules that apply to every other user apply here too — before
// anything is written.
func TestSetupRejectsCredentialsItWouldNotAcceptLater(t *testing.T) {
	for name, form := range map[string]url.Values{
		"username too short": setupForm("op", "a-long-enough-password", "a-long-enough-password"),
		"username not ASCII": setupForm("оператор", "a-long-enough-password", "a-long-enough-password"),
		"passwords differ":   setupForm("operator", "a-long-enough-password", "a-long-enough-passwerd"),
		"password too short": setupForm("operator", "short", "short"),
		"no password at all": setupForm("operator", "", ""),
		"no username at all": setupForm("", "a-long-enough-password", "a-long-enough-password"),
	} {
		m := testModule(t, false)
		token, _ := m.setup.activeToken()

		rec := postSetup(m, token, form)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
		if exists, err := m.store.UserExists(); err != nil || exists {
			t.Errorf("%s: an administrator was created anyway (err=%v)", name, err)
		}
		if rec := getSetup(m, token); rec.Code != http.StatusOK {
			t.Errorf("%s: the setup link was burned by a rejected form (%d)", name, rec.Code)
		}
	}
}

// Setup is unauthenticated by definition, so the only thing between the token
// and an offline guesser is the limiter in front of it.
func TestSetupIsRateLimited(t *testing.T) {
	m := testModule(t, false)

	for i := 0; i < 10; i++ {
		if rec := getSetup(m, "wrong-token"); rec.Code != http.StatusNotFound {
			t.Fatalf("attempt %d: status = %d, want 404 (still under the limit)", i+1, rec.Code)
		}
	}
	if rec := getSetup(m, "wrong-token"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 after the eleventh attempt", rec.Code)
	}
}

// expireSetupToken ages the current token out, the state the panel reaches when
// nobody follows the link within setupTokenTTL.
func expireSetupToken(m *Module) {
	m.setup.mu.Lock()
	defer m.setup.mu.Unlock()
	m.setup.expiresAt = m.setup.expiresAt.Add(-2 * setupTokenTTL)
}
