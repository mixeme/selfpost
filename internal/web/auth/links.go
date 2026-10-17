package auth

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/validate"
	"github.com/mixeme/selfpost/internal/web/view"
	"golang.org/x/crypto/bcrypt"
)

// The one-time links of password-reset (owner, 2026-10-13: a reset link goes
// only to a confirmed address; 2026-10-17: notifications go to one confirmed
// or not). A link is 256 random bits in a mail; the database keeps its
// SHA-256 only, as a session's (store.CreateUserLink), so a copy of the
// database or a backup opens nothing. Its address is the server's own name:
// the Host a request names is the requester's to choose, and a reset link
// built from it would carry the token to whichever host they chose.
const (
	confirmLinkLife = 24 * time.Hour
	resetLinkLife   = 30 * time.Minute
	// resetSpacing is how long after a reset link another is not sent: the
	// one in the mailbox works, and a stranger repeating a username cannot
	// spend the account's links an hour in a minute (security review,
	// 2026-10-17).
	resetSpacing = 10 * time.Minute
	// resetsPerDay is how many reset links one account is mailed in a day.
	// Anyone who knows a username can ask for them; past this the owner is
	// told once that further requests are ignored for a day, and then nothing
	// more is sent - rather than a live link in the mailbox five times an
	// hour for ever (security audit 2026-10-17, A3).
	resetsPerDay = 5
)

// later runs f after the answer, counted for Wait.
func (m *Module) later(f func()) {
	m.mailing.Add(1)
	go func() {
		defer m.mailing.Done()
		f()
	}()
}

// Wait waits, until ctx ends, for the links still being mailed: the panel's
// stop calls it before the database closes.
func (m *Module) Wait(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		m.mailing.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		logf("panel: stopped with a link still being mailed")
	}
}

// Mailer sends the panel's own mail (notify.Mailer).
type Mailer interface {
	Send(to, subject, body string) error
}

// errNoMail is a link that cannot be sent: no address, no mailer, or no name
// to put in it.
var errNoMail = errors.New("no address to send a link to")

// SetMailer gives the module the mailer its links go out by.
func (m *Module) SetMailer(ml Mailer) { m.mailer = ml }

// CanMail reports whether a link can be sent at all: a mailer, and the
// server's name to put in it.
func (m *Module) CanMail() bool { return m.mailer != nil && m.cfg.Hostname != "" }

// linkURL is the address of a link: https:// the server's own name, the path
// and the token.
func (m *Module) linkURL(path, token string) string {
	return "https://" + m.cfg.Hostname + path + token
}

// AllowConfirmationLink says whether another confirmation link may be sent
// to the user's address now: five an hour, whoever asks (the user from
// Account, the global administrator from the user form) - a link is a mail
// to that address, and nobody is to fill a mailbox with them.
func (m *Module) AllowConfirmationLink(userID int64) bool {
	return m.mailLimiter.Allow("confirm:" + strconv.FormatInt(userID, 10))
}

// SendConfirmation mails u's address a link that confirms it, replacing the
// link sent before. The caller has asked AllowConfirmationLink.
func (m *Module) SendConfirmation(u store.User) error {
	if u.Email == "" || m.mailer == nil || m.cfg.Hostname == "" {
		return errNoMail
	}
	token := randomToken(32)
	now := time.Now()
	if err := m.store.CreateUserLink(hashToken(token), u.ID, store.LinkConfirm, u.Email, now, now.Add(confirmLinkLife)); err != nil {
		return err
	}
	// The account is not named: the address may be a mistyped one, a
	// stranger's (security review, 2026-10-17); confirming also takes being
	// signed in as the account (HandleConfirm).
	body := "The address " + u.Email + " was given for an account of the SelfPost panel at " + m.cfg.Hostname +
		". Sign in to that account, then open this link to confirm it:\n\n" + m.linkURL("/confirm/", token) +
		"\n\nIt works once, for 24 hours. A confirmed address can be sent a link to reset the account's password. " +
		"If you did not expect this message, ignore it: nothing changes.\n"
	if err := m.mailer.Send(u.Email, "[SelfPost] Confirm your e-mail", body); err != nil {
		return err
	}
	logf("panel: user %d: a confirmation link was sent", u.ID)
	return nil
}

// SendConfirmationLater is SendConfirmation after the answer: a stuck
// Postfix does not hold the form that saved the address (sendmail's own
// time-out is 20 s). A link that cannot go is logged, and Account offers to
// send it again.
func (m *Module) SendConfirmationLater(u store.User) {
	m.later(func() {
		if err := m.SendConfirmation(u); err != nil {
			logf("panel: user %d: confirmation link not sent: %v", u.ID, err)
		}
	})
}

// HandleReset serves /login/reset: the form that asks for a reset link (GET)
// and the request (POST). The answer to a request is the sign-in page with
// the same words whatever the name - one that exists or not, with a
// confirmed address or not - and the link, when there is one, goes out after
// the answer, so the time to answer tells nothing either.
func (m *Module) HandleReset(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		m.view.Render(w, http.StatusOK, "login-reset", view.NewReset(m.cfg.Hostname, ""))
	case http.MethodPost:
		// Per address and username, and per address alone at ten times
		// that: one stranger behind a shared proxy cannot stop everyone
		// asking for a link (security audit 2026-10-17, A1).
		m.proxyWatch.see(r, m.trustedProxies)
		addr := limitAddr(r, m.trustedProxies)
		tooMany := func() {
			m.view.Render(w, http.StatusTooManyRequests, "login-reset", view.NewReset(m.cfg.Hostname, "Too many attempts. Please wait and try again."))
		}
		if !m.resetAddrLimiter.Allow(addr) {
			tooMany()
			return
		}
		if err := r.ParseForm(); err != nil {
			m.view.Render(w, http.StatusBadRequest, "login-reset", view.NewReset(m.cfg.Hostname, "Invalid form submission."))
			return
		}
		username := strings.TrimSpace(r.PostFormValue("username"))
		if !m.resetLimiter.Allow(userKey(addr, username)) {
			tooMany()
			return
		}
		m.later(func() { m.sendReset(username) })
		http.Redirect(w, r, "/login?reset=sent", http.StatusSeeOther)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// sendReset mails the account named username a reset link if its address is
// confirmed: not within resetSpacing of the last, five a day (then one
// notice) and five an hour.
func (m *Module) sendReset(username string) {
	if username == "" || m.mailer == nil || m.cfg.Hostname == "" {
		return
	}
	u, err := m.store.GetUserByUsername(username)
	if err != nil {
		if !errors.Is(err, store.ErrUserNotFound) {
			logf("panel: reset: get user: %v", err)
		}
		return
	}
	if u.Email == "" || u.EmailConfirmed.IsZero() {
		logf("panel: user %d asked for a reset link and has no confirmed e-mail; none was sent", u.ID)
		return
	}
	if l, err := m.store.LatestUserLink(u.ID, store.LinkReset, time.Now()); err == nil && time.Since(l.CreatedAt) < resetSpacing {
		logf("panel: user %d: a reset link went less than %s ago; none was sent", u.ID, resetSpacing)
		return
	}
	key := "reset:" + strconv.FormatInt(u.ID, 10)
	if !m.resetDayLimiter.Allow(key) {
		if !m.resetNoticeLimiter.Allow(key) {
			logf("panel: user %d: reset links asked for again after today's %d; nothing was sent", u.ID, resetsPerDay)
			return
		}
		logf("panel: user %d: reset links asked for more than %d times today; the owner is told further requests are ignored for 24 hours", u.ID, resetsPerDay)
		body := "Reset links for the SelfPost account " + u.Username + " at " + m.cfg.Hostname + " have been asked for more than " +
			strconv.Itoa(resetsPerDay) + " times today. Further requests are ignored for 24 hours, and no more mail of this kind will come before then.\n\n" +
			"If it was not you, nothing needs doing: the password stays as it is. A global administrator can set a new password on Server > Users at any time.\n"
		if err := m.mailer.Send(u.Email, "[SelfPost] Reset links paused for a day", body); err != nil {
			logf("panel: user %d: notice not sent: %v", u.ID, err)
		}
		return
	}
	if !m.mailLimiter.Allow(key) {
		logf("panel: user %d: reset links asked for too often; none was sent", u.ID)
		return
	}
	token := randomToken(32)
	now := time.Now()
	if err := m.store.CreateUserLink(hashToken(token), u.ID, store.LinkReset, u.Email, now, now.Add(resetLinkLife)); err != nil {
		logf("panel: user %d: reset link: %v", u.ID, err)
		return
	}
	body := "Someone asked to reset the password of the SelfPost account " + u.Username + " at " + m.cfg.Hostname +
		". Open this link to set a new one:\n\n" + m.linkURL("/login/reset/", token) +
		"\n\nIt works once, for 30 minutes. If it was not you, ignore this message: the password stays as it is.\n"
	if err := m.mailer.Send(u.Email, "[SelfPost] Reset your password", body); err != nil {
		logf("panel: user %d: reset link not sent: %v", u.ID, err)
		return
	}
	logf("panel: user %d: a reset link was sent", u.ID)
}

// linkToken is the token of a link path under prefix; "" for anything else.
func linkToken(path, prefix string) string {
	token := strings.TrimPrefix(path, prefix)
	if token == path || token == "" || strings.Contains(token, "/") {
		return ""
	}
	return token
}

const deadLink = "This link has expired or was already used."

// allowLink is the limit of a link's page: per link, so that nobody can use up
// another's, and per address at a ceiling high enough for everyone behind one
// proxy (security audit 2026-10-17, A1).
func (m *Module) allowLink(r *http.Request, token string) bool {
	m.proxyWatch.see(r, m.trustedProxies)
	if !m.linkAddrLimiter.Allow(limitAddr(r, m.trustedProxies)) {
		return false
	}
	return token == "" || m.linkLimiter.Allow(tokenKey(token))
}

// HandleResetLink serves /login/reset/<token>: the new password's form (GET)
// and setting it (POST). Setting it takes the link, ends every session of the
// account and every other reset link, and leaves the person at sign-in.
func (m *Module) HandleResetLink(w http.ResponseWriter, r *http.Request) {
	token := linkToken(r.URL.Path, "/login/reset/")
	if !m.allowLink(r, token) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	if token == "" {
		http.NotFound(w, r)
		return
	}
	dead := func() {
		m.view.Render(w, http.StatusNotFound, "login-reset", view.NewReset(m.cfg.Hostname, deadLink+" Ask for a new one."))
	}
	switch r.Method {
	case http.MethodGet:
		l, err := m.store.PeekUserLink(hashToken(token), store.LinkReset, time.Now())
		if err != nil {
			dead()
			return
		}
		u, ok := m.linkUser(l)
		if !ok {
			dead()
			return
		}
		m.view.Render(w, http.StatusOK, "login-reset-set", view.NewResetSet(m.cfg.Hostname, u.Username, token, ""))
	case http.MethodPost:
		m.setPasswordFromLink(w, r, token, dead)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// linkUser is the user of a reset link while the link still holds: the
// account's address is the one it was sent to, and confirmed.
func (m *Module) linkUser(l store.UserLink) (store.User, bool) {
	u, err := m.store.GetUser(l.UserID)
	if err != nil {
		return store.User{}, false
	}
	return u, u.Email == l.Email && !u.EmailConfirmed.IsZero()
}

func (m *Module) setPasswordFromLink(w http.ResponseWriter, r *http.Request, token string, dead func()) {
	l, err := m.store.PeekUserLink(hashToken(token), store.LinkReset, time.Now())
	if err != nil {
		dead()
		return
	}
	u, ok := m.linkUser(l)
	if !ok {
		dead()
		return
	}
	fail := func(status int, msg string) {
		m.view.Render(w, status, "login-reset-set", view.NewResetSet(m.cfg.Hostname, u.Username, token, msg))
	}
	if err := r.ParseForm(); err != nil {
		fail(http.StatusBadRequest, "Invalid form submission.")
		return
	}
	password := r.PostFormValue("new_password")
	if password != r.PostFormValue("new_password_confirm") {
		fail(http.StatusBadRequest, "Passwords do not match.")
		return
	}
	if err := validate.AdminPassword(password); err != nil {
		fail(http.StatusBadRequest, err.Error())
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		logf("panel: reset: hashing password failed: %v", err)
		fail(http.StatusInternalServerError, "Internal error. Please try again.")
		return
	}
	// Taken only now, so a mistyped repeat does not use the link up; taken
	// and the password set in one transaction, which also checks the
	// address again (store.TakeResetLink): of two requests with the link one
	// sets it, and a write that fails leaves the link to try again.
	if _, err := m.store.TakeResetLink(hashToken(token), string(hash), time.Now()); err != nil {
		if errors.Is(err, store.ErrNoLink) {
			dead()
			return
		}
		logf("panel: reset: set password of user %d: %v", u.ID, err)
		fail(http.StatusInternalServerError, "Internal error. Please try again.")
		return
	}
	m.sessions.DestroyAll(u.ID)
	logf("panel: user %d set a new password from a reset link; every session was ended", u.ID)
	http.Redirect(w, r, "/login?reset=done", http.StatusSeeOther)
}

// HandleConfirm serves /confirm/<token>: the address and a button (GET), and
// confirming it (POST). The token proves its holder reads the mailbox; being
// signed in as the account proves the mailbox is the account's - without it
// a mistyped address would hand the account to whoever reads it, by a reset
// link (security review, 2026-10-17).
func (m *Module) HandleConfirm(w http.ResponseWriter, r *http.Request) {
	token := linkToken(r.URL.Path, "/confirm/")
	if !m.allowLink(r, token) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	if token == "" {
		http.NotFound(w, r)
		return
	}
	dead := func(msg string) {
		p := view.NewConfirm(m.cfg.Hostname, "", "", "")
		p.Error = msg
		m.view.Render(w, http.StatusNotFound, "confirm-email", p)
	}
	signIn := func(w http.ResponseWriter) {
		p := view.NewConfirm(m.cfg.Hostname, "", "", "")
		p.Error = "Sign in to the account in this browser, then open the link again."
		p.SignIn = true
		m.view.Render(w, http.StatusForbidden, "confirm-email", p)
	}
	switch r.Method {
	case http.MethodGet:
		l, err := m.store.PeekUserLink(hashToken(token), store.LinkConfirm, time.Now())
		if err != nil {
			dead(deadLink + " Send a new one from Account.")
			return
		}
		u, err := m.store.GetUser(l.UserID)
		if err != nil || u.Email != l.Email {
			dead("The account's e-mail has changed since this link was sent.")
			return
		}
		if !m.signedInAs(r, l.UserID) {
			signIn(w)
			return
		}
		m.view.Render(w, http.StatusOK, "confirm-email", view.NewConfirm(m.cfg.Hostname, u.Username, u.Email, token))
	case http.MethodPost:
		// Looked at first and taken only for its own account's session, so
		// a stranger's press does not use the link up.
		l, err := m.store.PeekUserLink(hashToken(token), store.LinkConfirm, time.Now())
		if err != nil {
			dead(deadLink + " Send a new one from Account.")
			return
		}
		if !m.signedInAs(r, l.UserID) {
			signIn(w)
			return
		}
		if l, err = m.store.TakeUserLink(hashToken(token), store.LinkConfirm, time.Now()); err != nil {
			dead(deadLink + " Send a new one from Account.")
			return
		}
		ok, err := m.store.ConfirmEmail(l.UserID, l.Email, time.Now())
		if err != nil {
			logf("panel: confirm: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !ok {
			dead("The account's e-mail has changed since this link was sent.")
			return
		}
		u, _ := m.store.GetUser(l.UserID)
		logf("panel: user %d confirmed their e-mail", l.UserID)
		p := view.NewConfirm(m.cfg.Hostname, u.Username, l.Email, "")
		p.Done = true
		m.view.Render(w, http.StatusOK, "confirm-email", p)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// signedInAs reports whether the request carries a live session of userID.
func (m *Module) signedInAs(r *http.Request, userID int64) bool {
	token, ok := m.sessionToken(r)
	if !ok {
		return false
	}
	id, ok := m.sessions.Lookup(token)
	return ok && id == userID
}

// loginInfo is the sign-in page's success flash for what brought the person
// back to it.
func loginInfo(r *http.Request) string {
	switch r.URL.Query().Get("reset") {
	case "sent":
		return "If that account has a confirmed e-mail, the link is on its way. It works once, for 30 minutes."
	case "done":
		return "Password set. Sign in with it."
	}
	return ""
}

// LoggedPath is a request's path as the log may hold it: the token of a
// password-reset or confirmation link is a secret, and the log is read by
// more than the person the link went to (security review, 2026-10-17).
// Every log line that names a request's path goes through it: the origin
// check's refusal and the duplicate-cookie line (audit 2026-10-17, A4).
func LoggedPath(p string) string {
	for _, prefix := range []string{"/login/reset/", "/confirm/", "/setup/"} {
		if strings.HasPrefix(p, prefix) && len(p) > len(prefix) {
			return prefix + "…"
		}
	}
	return p
}
