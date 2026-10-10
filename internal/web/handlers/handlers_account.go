package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/validate"
	"github.com/mixeme/selfpost/internal/web/view"
	"golang.org/x/crypto/bcrypt"
)

// Account is the signed-in user's own half of what used to be one Settings
// page: who they are to the panel (username, e-mail) and their password. Every
// role has it. Each part is its own form and its own POST, so changing one
// never asks for the others.
//
// The e-mail is the user's — what the panel writes to. A domain's DMARC report
// address is set in the domain's settings, where a button fills it with this
// e-mail; nothing here refers to a domain.

// accountForm is what the page shows back: the stored values, or the ones just
// submitted when a form is re-rendered with an error.
type accountForm struct {
	Err      string
	Username string
	Email    string
}

func (h *Handlers) accountUser(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	p, ok := h.principal(r)
	if !ok {
		http.NotFound(w, r)
		return store.User{}, false
	}
	u, err := h.store.GetUser(p.ID)
	if err != nil {
		logf("panel: account: get user %d: %v", p.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return store.User{}, false
	}
	return u, true
}

func formOf(u store.User) accountForm {
	return accountForm{Username: u.Username, Email: u.Email}
}

// HandleAccount shows the Account page.
func (h *Handlers) HandleAccount(w http.ResponseWriter, r *http.Request) {
	u, ok := h.accountUser(w, r)
	if !ok {
		return
	}
	h.renderAccount(w, r, http.StatusOK, u, formOf(u))
}

func (h *Handlers) renderAccount(w http.ResponseWriter, r *http.Request, status int, u store.User, f accountForm) {
	page := view.NewAccount(h.shellMeta(r), string(u.Role))
	page.Username, page.Email = f.Username, f.Email
	page.WithResult(accountFlash(r), f.Err)
	h.view.Render(w, status, "account", page)
}

func accountFlash(r *http.Request) string {
	switch r.URL.Query().Get("done") {
	case "profile":
		return "Profile saved."
	case "password":
		return "Password changed. Any other signed-in sessions were signed out."
	}
	return ""
}

// HandleAccountProfile saves the username and the account e-mail.
func (h *Handlers) HandleAccountProfile(w http.ResponseWriter, r *http.Request) {
	u, ok := h.accountUser(w, r)
	if !ok {
		return
	}
	f := formOf(u)
	fail := func(status int, msg string) {
		f.Err = msg
		h.renderAccount(w, r, status, u, f)
	}
	if err := r.ParseForm(); err != nil {
		fail(http.StatusBadRequest, "Invalid form submission.")
		return
	}
	f.Username = strings.TrimSpace(r.PostFormValue("username"))
	f.Email = strings.TrimSpace(r.PostFormValue("email"))
	if f.Username == "" {
		f.Username = u.Username
	}
	if f.Username != u.Username {
		if err := validate.Username(f.Username); err != nil {
			fail(http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := validate.Email(f.Email); err != nil {
		fail(http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.UpdateUser(u.ID, f.Username, u.PasswordHash, f.Email); err != nil {
		if errors.Is(err, store.ErrUserExists) {
			fail(http.StatusConflict, "That username is already in use.")
			return
		}
		logf("panel: account: update user %d: %v", u.ID, err)
		fail(http.StatusInternalServerError, "Could not save the profile. Please check the logs and try again.")
		return
	}
	logf("panel: user %d profile updated (username: %t, e-mail: %t)", u.ID, f.Username != u.Username, f.Email != u.Email)
	http.Redirect(w, r, "/account?done=profile", http.StatusSeeOther)
}

// HandleAccountPassword changes the password. It asks for the current one and
// is rate-limited like a sign-in: the form is a place to guess a password from
// an unattended session.
func (h *Handlers) HandleAccountPassword(w http.ResponseWriter, r *http.Request) {
	u, ok := h.accountUser(w, r)
	if !ok {
		return
	}
	f := formOf(u)
	fail := func(status int, msg string) {
		f.Err = msg
		h.renderAccount(w, r, status, u, f)
	}
	if !h.auth.AllowLoginAttempt(r) {
		fail(http.StatusTooManyRequests, "Too many attempts. Please wait and try again.")
		return
	}
	if err := r.ParseForm(); err != nil {
		fail(http.StatusBadRequest, "Invalid form submission.")
		return
	}
	current := r.PostFormValue("current_password")
	password := r.PostFormValue("new_password")
	confirm := r.PostFormValue("new_password_confirm")
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)); err != nil {
		fail(http.StatusUnauthorized, "Current password is incorrect.")
		return
	}
	if password != confirm {
		fail(http.StatusBadRequest, "New passwords do not match.")
		return
	}
	if err := validate.AdminPassword(password); err != nil {
		fail(http.StatusBadRequest, err.Error())
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		logf("panel: account: hashing password failed: %v", err)
		fail(http.StatusInternalServerError, "Internal error. Please try again.")
		return
	}
	if err := h.store.UpdateUser(u.ID, u.Username, string(hash), u.Email); err != nil {
		logf("panel: account: update password of user %d: %v", u.ID, err)
		fail(http.StatusInternalServerError, "Could not save the password. Please check the logs and try again.")
		return
	}
	if token, ok := h.auth.SessionToken(r); ok {
		h.auth.DestroyOtherSessions(u.ID, token)
	}
	logf("panel: user %d changed their password", u.ID)
	http.Redirect(w, r, "/account?done=password", http.StatusSeeOther)
}
