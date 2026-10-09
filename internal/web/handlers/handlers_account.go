package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mixeme/selfpost/internal/dnscheck"
	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/validate"
	"golang.org/x/crypto/bcrypt"
)

// Account is the signed-in user's own half of what used to be one Settings
// page: who they are to the panel (username, e-mail), their password, and their
// default DMARC report address. Every role has it. Each part is its own form
// and its own POST, so changing one never asks for the others.
//
// The e-mail is the user's — what the panel writes to. The DMARC default is a
// separate choice that may use it: a domain whose report address is "this
// user's default" resolves to whatever is chosen here (store.DMARCDefault).

// accountForm is what the page shows back: the stored values, or the ones just
// submitted when a form is re-rendered with an error.
type accountForm struct {
	Err          string
	Username     string
	Email        string
	DMARCMode    string
	DMARCAddress string
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
	return accountForm{Username: u.Username, Email: u.Email, DMARCMode: u.DMARCDefaultMode, DMARCAddress: u.DMARCDefaultAddress}
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
	data := h.pageBase(r)
	data["Title"] = "SelfPost — account"
	data["Active"] = "account"
	data["AccountPage"] = true
	data["Role"] = u.Role
	data["FormUsername"] = f.Username
	data["FormEmail"] = f.Email
	data["FormDMARCMode"] = f.DMARCMode
	data["FormDMARCAddress"] = f.DMARCAddress
	data["AccountEmail"] = u.Email
	data["Error"] = f.Err
	data["Flash"] = accountFlash(r)
	data["DMARCIngestEnabled"] = h.dmarc != nil && h.cfg.DMARCEnabled

	// The stored default, not the one being typed: the authorization record
	// belongs to the address domains are actually told to publish.
	def := u.DMARCDefault()
	if addr := def.Resolve(""); addr != "" && def.Mode != store.DMARCDefaultHosted {
		if hub := dnscheck.EmailDomain(addr); hub != "" {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			data["ReportAuthDNS"] = h.dns.ReportAuth(ctx, hub)
			cancel()
			data["ReportAuthName"] = dnscheck.ReportAuthRecordName(hub)
			data["ReportAuthExample"] = dnscheck.ReportAuthExample()
			data["ReportAuthHub"] = hub
		}
	}
	h.view.Render(w, status, "settings", data)
}

func accountFlash(r *http.Request) string {
	switch r.URL.Query().Get("done") {
	case "profile":
		return "Profile saved."
	case "password":
		return "Password changed. Any other signed-in sessions were signed out."
	case "dmarc":
		return "Default report address saved."
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
	if f.Username != u.Username {
		if token, ok := h.auth.SessionToken(r); ok {
			h.auth.RenameSession(token, f.Username)
		}
	}
	// The e-mail is the report address of every domain that follows this
	// user's "my account e-mail" default.
	if f.Email != u.Email && u.DMARCDefaultMode == store.DMARCDefaultAccount {
		h.resyncDMARC("account e-mail")
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
		h.auth.DestroyOtherSessions(token)
	}
	logf("panel: user %d changed their password", u.ID)
	http.Redirect(w, r, "/account?done=password", http.StatusSeeOther)
}

// HandleAccountDMARC saves the user's default report address: SelfPost hosted,
// the account e-mail, another address, or none.
func (h *Handlers) HandleAccountDMARC(w http.ResponseWriter, r *http.Request) {
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
	f.DMARCMode = strings.TrimSpace(r.PostFormValue("dmarc_default"))
	f.DMARCAddress = strings.TrimSpace(r.PostFormValue("dmarc_default_address"))
	switch f.DMARCMode {
	case store.DMARCDefaultNone:
	case store.DMARCDefaultHosted:
		if h.dmarc == nil || !h.dmarc.Enabled() {
			fail(http.StatusBadRequest, "SelfPost-hosted reports are not enabled on this server.")
			return
		}
	case store.DMARCDefaultAccount:
		if u.Email == "" {
			fail(http.StatusBadRequest, "Your account has no e-mail yet. Save one under Profile first, or choose another address.")
			return
		}
	case store.DMARCDefaultCustom:
		if f.DMARCAddress == "" {
			fail(http.StatusBadRequest, "Enter the report address or choose another option.")
			return
		}
		if err := validate.Email(f.DMARCAddress); err != nil {
			fail(http.StatusBadRequest, err.Error())
			return
		}
	default:
		fail(http.StatusBadRequest, "Choose where your DMARC reports go.")
		return
	}
	if err := h.store.SetDMARCDefault(u.ID, f.DMARCMode, f.DMARCAddress); err != nil {
		logf("panel: account: set dmarc default of user %d: %v", u.ID, err)
		fail(http.StatusInternalServerError, "Could not save the report address. Please check the logs and try again.")
		return
	}
	h.resyncDMARC("default report address")
	logf("panel: user %d set their default report address to %q", u.ID, f.DMARCMode)
	http.Redirect(w, r, "/account?done=dmarc", http.StatusSeeOther)
}

// resyncDMARC rebuilds the ingest allow-list after something that changes
// where a domain's reports go. A failure is logged, not shown: the setting is
// saved, and the next resync picks it up.
func (h *Handlers) resyncDMARC(why string) {
	if h.dmarc == nil || !h.dmarc.Enabled() {
		return
	}
	if err := h.dmarc.Resync(); err != nil {
		logf("panel: dmarc resync after %s change: %v", why, err)
	}
}
