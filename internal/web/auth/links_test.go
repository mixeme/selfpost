package auth

import (
	"github.com/mixeme/selfpost/internal/store"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// sentMail is one message a recordingMailer was given.
type sentMail struct{ to, subject, body string }

// recordingMailer keeps what would have been sent.
type recordingMailer struct{ sent chan sentMail }

func (r *recordingMailer) Send(to, subject, body string) error {
	r.sent <- sentMail{to, subject, body}
	return nil
}

// next is the next message, or none within a moment: a reset link goes out
// after the answer.
func (r *recordingMailer) next(t *testing.T) (sentMail, bool) {
	t.Helper()
	select {
	case m := <-r.sent:
		return m, true
	case <-time.After(500 * time.Millisecond):
		return sentMail{}, false
	}
}

// linkModule is a panel named mail.example.org with the administrator of
// moduleWithAdmin, its address admin@example.org, confirmed when confirmed.
func linkModule(t *testing.T, confirmed bool) (*Module, *recordingMailer) {
	t.Helper()
	m := moduleWithAdmin(t)
	m.cfg.Hostname = "mail.example.org"
	ml := &recordingMailer{sent: make(chan sentMail, 8)}
	m.SetMailer(ml)
	u, err := m.store.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.UpdateUser(u.ID, u.Username, u.PasswordHash, "admin@example.org"); err != nil {
		t.Fatal(err)
	}
	if confirmed {
		if ok, err := m.store.ConfirmEmail(u.ID, "admin@example.org", time.Now()); err != nil || !ok {
			t.Fatalf("confirm: %v %v", ok, err)
		}
	}
	return m, ml
}

func postForm(h http.HandlerFunc, target, remoteAddr string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	h(rec, r)
	return rec
}

func get(h http.HandlerFunc, target string) *httptest.ResponseRecorder {
	return getAs(h, target, "")
}

// getAs is get with the session cookie of session, none for "".
func getAs(h http.HandlerFunc, target, session string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.RemoteAddr = "192.0.2.1:1234"
	if session != "" {
		r.AddCookie(&http.Cookie{Name: "selfpost_session", Value: session})
	}
	rec := httptest.NewRecorder()
	h(rec, r)
	return rec
}

// postAs is a POST of no fields with the session cookie of session.
func postAs(h http.HandlerFunc, target, session string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, target, nil)
	r.RemoteAddr = "192.0.2.1:1234"
	if session != "" {
		r.AddCookie(&http.Cookie{Name: "selfpost_session", Value: session})
	}
	rec := httptest.NewRecorder()
	h(rec, r)
	return rec
}

var resetLinkRe = regexp.MustCompile(`https://mail\.example\.org/login/reset/([A-Za-z0-9_-]{43})\n`)

// The answer is the same for every name; only an account with a confirmed
// address is sent a link, and the link names the server, whatever Host the
// request named.
func TestResetSendsALinkOnlyToAConfirmedAddress(t *testing.T) {
	for _, c := range []struct {
		name      string
		confirmed bool
		username  string
		mail      bool
	}{
		{"unconfirmed", false, "admin", false},
		{"unknown name", true, "nobody", false},
		{"confirmed", true, "admin", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, ml := linkModule(t, c.confirmed)
			r := httptest.NewRequest(http.MethodPost, "http://evil.example.net/login/reset",
				strings.NewReader(url.Values{"username": {c.username}}.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.RemoteAddr = "192.0.2.1:1234"
			rec := httptest.NewRecorder()
			m.HandleReset(rec, r)
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login?reset=sent" {
				t.Fatalf("answer %d %q", rec.Code, rec.Header().Get("Location"))
			}
			got, sent := ml.next(t)
			if sent != c.mail {
				t.Fatalf("mail sent = %v, want %v", sent, c.mail)
			}
			if !sent {
				return
			}
			if got.to != "admin@example.org" || got.subject != "[SelfPost] Reset your password" ||
				!resetLinkRe.MatchString(got.body) || strings.Contains(got.body, "evil") {
				t.Errorf("mail = %+v", got)
			}
		})
	}
	// The sign-in page it lands on says the same whatever happened.
	m, _ := linkModule(t, false)
	if body := get(m.HandleLogin, "/login?reset=sent").Body.String(); !strings.Contains(body,
		"If that account has a confirmed e-mail, the link is on its way. It works once, for 30 minutes.") {
		t.Errorf("sign-in after a request:\n%s", body)
	}
}

func askReset(t *testing.T, m *Module, ml *recordingMailer) string {
	t.Helper()
	postForm(m.HandleReset, "/login/reset", "192.0.2.1:1234", url.Values{"username": {"admin"}})
	got, ok := ml.next(t)
	if !ok {
		t.Fatal("no reset link was sent")
	}
	return resetLinkRe.FindStringSubmatch(got.body)[1]
}

// A link opens the form; a mistyped repeat does not use it up; the password
// it sets is the one that signs in, every session ends, and the link works
// once.
func TestResetLinkSetsThePasswordOnce(t *testing.T) {
	m, ml := linkModule(t, true)
	session := mustCreate(t, m.sessions, "admin")
	token := askReset(t, m, ml)
	path := "/login/reset/" + token

	page := get(m.HandleResetLink, path)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "mail.example.org · admin") ||
		!strings.Contains(page.Body.String(), `action="`+path+`"`) {
		t.Fatalf("the link's page: %d\n%s", page.Code, page.Body.String())
	}
	const fresh = "a-new-password-of-mine"
	if rec := postForm(m.HandleResetLink, path, "192.0.2.1:1234", url.Values{"new_password": {fresh}, "new_password_confirm": {"other"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("a mistyped repeat: %d", rec.Code)
	}
	if rec := postForm(m.HandleResetLink, path, "192.0.2.1:1234", url.Values{"new_password": {"short"}, "new_password_confirm": {"short"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("a short password: %d", rec.Code)
	}
	rec := postForm(m.HandleResetLink, path, "192.0.2.1:1234", url.Values{"new_password": {fresh}, "new_password_confirm": {fresh}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login?reset=done" {
		t.Fatalf("set: %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	u, _ := m.store.GetUserByUsername("admin")
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(fresh)) != nil {
		t.Error("the new password does not sign in")
	}
	if _, found, _ := m.store.LookupSession(hashToken(session)); found {
		t.Error("a session survived the reset")
	}
	if again := get(m.HandleResetLink, path); again.Code != http.StatusNotFound || !strings.Contains(again.Body.String(), deadLink) {
		t.Errorf("the link worked twice: %d", again.Code)
	}
	if again := postForm(m.HandleResetLink, path, "192.0.2.1:1234", url.Values{"new_password": {fresh + "x"}, "new_password_confirm": {fresh + "x"}}); again.Code != http.StatusNotFound {
		t.Errorf("the link set a password twice: %d", again.Code)
	}
}

// A link sent to an address the account no longer has does not work.
func TestResetLinkDiesWithTheAddress(t *testing.T) {
	m, ml := linkModule(t, true)
	token := askReset(t, m, ml)
	u, _ := m.store.GetUserByUsername("admin")
	if err := m.store.UpdateUser(u.ID, u.Username, u.PasswordHash, "other@example.org"); err != nil {
		t.Fatal(err)
	}
	if rec := get(m.HandleResetLink, "/login/reset/"+token); rec.Code != http.StatusNotFound {
		t.Errorf("a link of the old address opened: %d", rec.Code)
	}
}

// Requests for a link are counted per address: the sixth in 15 minutes is
// refused, and it says so without saying anything of the name.
func TestResetRequestsAreLimited(t *testing.T) {
	m, _ := linkModule(t, false)
	for i := range 5 {
		if rec := postForm(m.HandleReset, "/login/reset", "192.0.2.9:1", url.Values{"username": {"x"}}); rec.Code != http.StatusSeeOther {
			t.Fatalf("request %d: %d", i+1, rec.Code)
		}
	}
	if rec := postForm(m.HandleReset, "/login/reset", "192.0.2.9:1", url.Values{"username": {"x"}}); rec.Code != http.StatusTooManyRequests {
		t.Errorf("the sixth request: %d", rec.Code)
	}
}

var confirmLinkRe = regexp.MustCompile(`https://mail\.example\.org/confirm/([A-Za-z0-9_-]{43})\n`)

// The confirmation link's page changes nothing; its button confirms the
// address, once - and only for its own account's session: a mistyped address
// is a stranger's, and the mail does not name the account.
func TestConfirmLink(t *testing.T) {
	m, ml := linkModule(t, false)
	u, _ := m.store.GetUserByUsername("admin")
	if err := m.SendConfirmation(u); err != nil {
		t.Fatal(err)
	}
	got, ok := ml.next(t)
	if !ok || got.to != "admin@example.org" || got.subject != "[SelfPost] Confirm your e-mail" || !confirmLinkRe.MatchString(got.body) {
		t.Fatalf("mail = %+v %v", got, ok)
	}
	if strings.Contains(got.body, "admin ") || strings.Contains(got.body, "account admin") {
		t.Errorf("the confirmation mail names the account:\n%s", got.body)
	}
	path := "/confirm/" + confirmLinkRe.FindStringSubmatch(got.body)[1]
	// Without a session, or with another account's, the link asks to sign in
	// and is not used up.
	mustCreate(t, m.sessions, "other")
	other := mustCreate(t, m.sessions, "other")
	for name, session := range map[string]string{"no session": "", "another account": other} {
		if rec := getAs(m.HandleConfirm, path, session); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `href="/login"`) {
			t.Errorf("%s: GET %d", name, rec.Code)
		}
		if rec := postAs(m.HandleConfirm, path, session); rec.Code != http.StatusForbidden {
			t.Errorf("%s: POST %d", name, rec.Code)
		}
	}
	session := mustCreate(t, m.sessions, "admin")
	page := getAs(m.HandleConfirm, path, session)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `action="`+path+`"`) ||
		!strings.Contains(page.Body.String(), "admin@example.org") {
		t.Fatalf("the link's page: %d\n%s", page.Code, page.Body.String())
	}
	if u, _ = m.store.GetUser(u.ID); !u.EmailConfirmed.IsZero() {
		t.Fatal("opening the link confirmed the address")
	}
	if rec := postAs(m.HandleConfirm, path, session); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "E-mail confirmed") {
		t.Fatalf("confirm: %d\n%s", rec.Code, rec.Body.String())
	}
	if u, _ = m.store.GetUser(u.ID); u.EmailConfirmed.IsZero() {
		t.Fatal("the button did not confirm the address")
	}
	if rec := postAs(m.HandleConfirm, path, session); rec.Code != http.StatusNotFound {
		t.Errorf("the link worked twice: %d", rec.Code)
	}
}

// A second request within resetSpacing sends nothing and leaves the first
// link working: a stranger repeating a username neither kills the link in
// the mailbox nor spends the account's links.
func TestResetRequestsAreSpaced(t *testing.T) {
	m, ml := linkModule(t, true)
	first := askReset(t, m, ml)
	postForm(m.HandleReset, "/login/reset", "192.0.2.2:1234", url.Values{"username": {"admin"}})
	if got, sent := ml.next(t); sent {
		t.Fatalf("a second link went within %s: %+v", resetSpacing, got)
	}
	if rec := get(m.HandleResetLink, "/login/reset/"+first); rec.Code != http.StatusOK {
		t.Errorf("the first link died: %d", rec.Code)
	}
}

// A link path that is not one is not found.
func TestLinkPathsWithoutAToken(t *testing.T) {
	m, _ := linkModule(t, true)
	for _, p := range []string{"/login/reset/", "/login/reset/a/b", "/confirm/"} {
		h := m.HandleResetLink
		if strings.HasPrefix(p, "/confirm/") {
			h = m.HandleConfirm
		}
		if rec := get(h, p); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}

// Five reset links a day to one account, then one notice that further
// requests are ignored for a day, then nothing (security audit 2026-10-17,
// A3). The ten-minute spacing is stepped over by removing the last link.
func TestResetLinksPauseForADayAfterFive(t *testing.T) {
	m, ml := linkModule(t, true)
	u, _ := m.store.GetUserByUsername("admin")
	for i := 0; i < resetsPerDay+3; i++ {
		if err := m.store.DeleteUserLinks(u.ID, store.LinkReset); err != nil {
			t.Fatal(err)
		}
		m.sendReset("admin")
		got, sent := ml.next(t)
		switch {
		case i < resetsPerDay:
			if !sent || !resetLinkRe.MatchString(got.body) {
				t.Fatalf("link %d: sent %v, %+v", i+1, sent, got)
			}
		case i == resetsPerDay:
			if !sent || got.subject != "[SelfPost] Reset links paused for a day" || resetLinkRe.MatchString(got.body) ||
				!strings.Contains(got.body, "ignored for 24 hours") {
				t.Fatalf("the notice: sent %v, %+v", sent, got)
			}
		default:
			if sent {
				t.Fatalf("request %d after the notice sent %+v", i+1, got)
			}
		}
	}
}
