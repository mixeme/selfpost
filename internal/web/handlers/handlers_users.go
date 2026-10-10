package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/auth"
	"github.com/mixeme/selfpost/internal/web/validate"
	"github.com/mixeme/selfpost/internal/web/view"
	"golang.org/x/crypto/bcrypt"
)

// userFormView is what the user form is drawn with: a refusal, and the values
// the form holds — the stored user's, or what was typed into a form that was
// refused.
type userFormView struct {
	FormErr      string
	FormUsername string
	FormEmail    string
	FormRole     string
	Reach        store.Reach
}

// HandleUsers lists panel users (global only).
func (h *Handlers) HandleUsers(w http.ResponseWriter, r *http.Request) {
	p, ok := h.requireGlobal(w, r)
	if !ok {
		return
	}
	rows, err := h.store.ListUserRows()
	if err != nil {
		logf("panel: list users: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	list := make([]view.UserRow, len(rows))
	for i, row := range rows {
		u := row.User
		list[i] = view.NewUserRow(view.UserRowInput{
			ID: u.ID, Username: u.Username, Email: u.Email, Global: u.Role == store.RoleGlobal, You: u.ID == p.ID,
			AllOutbound: u.AllDomains, AllInbound: u.AllInboundDomains,
			Outbound: row.DomainNames, Inbound: row.InboundDomainNames,
		})
	}
	page := view.NewUsers(h.shellMeta(r), h.cfg.InboundEnabled, usersFlash(r)).WithRows(list)
	h.view.Render(w, http.StatusOK, "users", page)
}

func usersFlash(r *http.Request) string {
	switch r.URL.Query().Get("done") {
	case "created":
		return "User created."
	case "updated":
		return "User updated."
	case "deleted":
		return "User deleted."
	default:
		return ""
	}
}

// HandleUserNew creates a panel user (global only).
func (h *Handlers) HandleUserNew(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.renderUserForm(w, r, http.StatusOK, 0, userFormView{FormRole: string(store.RoleDomain)})
	case http.MethodPost:
		h.submitUserCreate(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleUserEdit edits or deletes a panel user (global only).
func (h *Handlers) HandleUserEdit(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	uid, ok := parseUserID(w, r)
	if !ok {
		return
	}
	u, err := h.store.GetUser(uid)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			http.NotFound(w, r)
			return
		}
		logf("panel: get user %d: %v", uid, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.renderUserForm(w, r, http.StatusOK, u.ID, userFormView{
			FormUsername: u.Username,
			FormEmail:    u.Email,
			FormRole:     string(u.Role),
			Reach:        u.Reach(),
		})
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: "Invalid form submission.", FormUsername: u.Username, FormEmail: u.Email, FormRole: string(u.Role)})
			return
		}
		h.submitUserUpdate(w, r, u)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handlers) renderUserForm(w http.ResponseWriter, r *http.Request, status int, userID int64, form userFormView) {
	domains, err := h.store.ListDomains()
	if err != nil {
		logf("panel: user form: list domains: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	in := view.UserFormInput{
		ID: userID, Username: form.FormUsername, Email: form.FormEmail, Role: form.FormRole,
		RoleLocked:  lastGlobalLocked(h, userID, form.FormRole),
		PasswordMin: validate.MinAdminPasswordLen,
		ShowInbound: h.cfg.InboundEnabled,
		AllOut:      form.Reach.AllDomains, AllIn: form.Reach.AllInbound,
		Error: form.FormErr,
	}
	// The head names the user as stored, not as typed into a form that was
	// refused; Delete user is not offered for the only global user.
	if userID != 0 {
		in.Name = form.FormUsername
		if u, err := h.store.GetUser(userID); err == nil {
			in.Name = u.Username
		}
		in.CanDelete = !in.RoleLocked
	}
	out := idSet(form.Reach.DomainIDs)
	for _, d := range domains {
		in.OutDomains = append(in.OutDomains, view.DomainChoice{ID: d.ID, Name: d.Name, Checked: out[d.ID]})
	}
	// The two lists of the form, each with its All tick. Inbound is offered
	// only where the feature is on.
	if h.cfg.InboundEnabled {
		inboundDomains, err := h.store.ListInboundDomains()
		if err != nil {
			logf("panel: user form: list inbound domains: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		inb := idSet(form.Reach.InboundDomainIDs)
		for _, d := range inboundDomains {
			in.InDomains = append(in.InDomains, view.DomainChoice{ID: d.ID, Name: d.Name, Checked: inb[d.ID]})
		}
	}
	h.view.Render(w, status, "user", view.NewUserForm(h.shellMeta(r), in))
}

// lastGlobalLocked is whether the form's user is the only global one, whose role
// cannot be changed (the form shows it read-only).
func lastGlobalLocked(h *Handlers, userID int64, formRole string) bool {
	if userID == 0 || formRole != string(store.RoleGlobal) {
		return false
	}
	n, err := h.store.CountGlobalUsers()
	return err == nil && n <= 1
}

func (h *Handlers) submitUserCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderUserForm(w, r, http.StatusBadRequest, 0, userFormView{FormErr: "Invalid form submission."})
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")
	role := store.Role(r.PostFormValue("role"))
	reach := h.reachFromForm(r, store.Reach{})

	if err := validate.Username(username); err != nil {
		h.renderUserForm(w, r, http.StatusBadRequest, 0, userFormView{FormErr: err.Error(), FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
		return
	}
	if err := validate.Email(email); err != nil {
		h.renderUserForm(w, r, http.StatusBadRequest, 0, userFormView{FormErr: err.Error(), FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
		return
	}
	if err := validate.AdminPassword(password); err != nil {
		h.renderUserForm(w, r, http.StatusBadRequest, 0, userFormView{FormErr: err.Error(), FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
		return
	}
	if role != store.RoleGlobal && role != store.RoleDomain {
		h.renderUserForm(w, r, http.StatusBadRequest, 0, userFormView{FormErr: "Choose a valid role.", FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
		return
	}
	if role == store.RoleDomain && reach.Empty() {
		h.renderUserForm(w, r, http.StatusBadRequest, 0, userFormView{FormErr: "Assign at least one outbound or inbound domain to a domain administrator.", FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		logf("panel: create user hash: %v", err)
		h.renderUserForm(w, r, http.StatusInternalServerError, 0, userFormView{FormErr: "Internal error. Please try again."})
		return
	}
	id, err := h.store.CreateUser(username, string(hash), role, reach)
	if err != nil {
		if errors.Is(err, store.ErrUserExists) {
			h.renderUserForm(w, r, http.StatusConflict, 0, userFormView{FormErr: "That username is already in use.", FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
			return
		}
		logf("panel: create user: %v", err)
		h.renderUserForm(w, r, http.StatusInternalServerError, 0, userFormView{FormErr: "Could not create user. Please check the logs."})
		return
	}
	// No domain follows a user who has just been created, so there is nothing
	// to resync when their address is first stored.
	if email != "" {
		if err := h.store.UpdateUser(id, username, string(hash), email); err != nil {
			logf("panel: create user %d: store e-mail: %v", id, err)
			h.renderUserForm(w, r, http.StatusInternalServerError, id, userFormView{FormErr: "The user was created, but the e-mail could not be saved. Set it here.", FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
			return
		}
	}
	http.Redirect(w, r, "/server/users?done=created", http.StatusSeeOther)
}

func (h *Handlers) submitUserUpdate(w http.ResponseWriter, r *http.Request, u store.User) {
	if err := r.ParseForm(); err != nil {
		h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: "Invalid form submission.", FormUsername: u.Username, FormEmail: u.Email, FormRole: string(u.Role)})
		return
	}
	p, ok := h.principal(r)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	// The form always sends the e-mail, empty when cleared. A post without the
	// field at all is not a request to clear it.
	email := u.Email
	if _, sent := r.PostForm["email"]; sent {
		email = strings.TrimSpace(r.PostFormValue("email"))
	}
	password := r.PostFormValue("password")
	role := store.Role(r.PostFormValue("role"))
	reach := h.reachFromForm(r, u.Reach())

	if username == "" {
		username = u.Username
	}
	if err := validate.Username(username); err != nil {
		h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: err.Error(), FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
		return
	}
	if err := validate.Email(email); err != nil {
		h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: err.Error(), FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
		return
	}
	if role != store.RoleGlobal && role != store.RoleDomain {
		h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: "Choose a valid role.", FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
		return
	}
	if role == store.RoleDomain && reach.Empty() {
		h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: "Assign at least one outbound or inbound domain to a domain administrator.", FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
		return
	}

	if u.Role == store.RoleGlobal && role == store.RoleDomain {
		n, err := h.store.CountGlobalUsers()
		if err != nil || n <= 1 {
			msg := "Cannot demote the last global administrator."
			if u.ID == p.ID {
				msg = "You cannot demote yourself without another global administrator."
			}
			h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: msg, FormUsername: username, FormEmail: email, FormRole: string(u.Role), Reach: reach})
			return
		}
	}

	hash := u.PasswordHash
	if password != "" {
		if err := validate.AdminPassword(password); err != nil {
			h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: err.Error(), FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
			return
		}
		newHash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
		if err != nil {
			logf("panel: update user hash: %v", err)
			h.renderUserForm(w, r, http.StatusInternalServerError, u.ID, userFormView{FormErr: "Internal error. Please try again.", FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
			return
		}
		hash = string(newHash)
	}

	if err := h.store.UpdateUser(u.ID, username, hash, email); err != nil {
		if errors.Is(err, store.ErrUserExists) {
			h.renderUserForm(w, r, http.StatusConflict, u.ID, userFormView{FormErr: "That username is already in use.", FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
			return
		}
		logf("panel: update user: %v", err)
		h.renderUserForm(w, r, http.StatusInternalServerError, u.ID, userFormView{FormErr: "Could not save user. Please check the logs.", FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
		return
	}
	if password != "" {
		h.endSessionsAfterPasswordSet(r, p, u)
	}

	if role != u.Role {
		if err := h.store.SetUserRole(u.ID, role); err != nil {
			logf("panel: set user role: %v", err)
			h.renderUserForm(w, r, http.StatusInternalServerError, u.ID, userFormView{FormErr: "Could not update role.", FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
			return
		}
		if role == store.RoleGlobal {
			if err := h.store.ClearUserReach(u.ID); err != nil {
				logf("panel: clear user reach: %v", err)
			}
		}
	}

	if role == store.RoleDomain {
		if err := h.store.SetUserReach(u.ID, reach); err != nil {
			logf("panel: set user reach: %v", err)
			h.renderUserForm(w, r, http.StatusInternalServerError, u.ID, userFormView{FormErr: "Could not save domain assignments.", FormUsername: username, FormEmail: email, FormRole: string(role), Reach: reach})
			return
		}
	}

	http.Redirect(w, r, "/server/users?done=updated", http.StatusSeeOther)
}

// endSessionsAfterPasswordSet signs out the logins a user had under the old
// password when an administrator sets a new one on the user form. An
// administrator who sets their own password keeps the browser they are using.
func (h *Handlers) endSessionsAfterPasswordSet(r *http.Request, by auth.Principal, u store.User) {
	if u.ID == by.ID {
		if token, ok := h.auth.SessionToken(r); ok {
			h.auth.DestroyOtherSessions(u.ID, token)
			return
		}
	}
	h.auth.DestroyUserSessions(u.ID)
}

// HandleUserDeleteConfirm shows the cascade warning before a panel user is
// removed — the same pattern as HandleDeleteConfirm for domains, so a single
// mis-click on Delete cannot remove a user (P3, code-review.md).
func (h *Handlers) HandleUserDeleteConfirm(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	uid, ok := parseUserID(w, r)
	if !ok {
		return
	}
	u, err := h.store.GetUser(uid)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			http.NotFound(w, r)
			return
		}
		logf("panel: get user %d: %v", uid, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	in := view.UserDeleteInput{
		ID: u.ID, Username: u.Username, Global: u.Role == store.RoleGlobal,
		AllOutbound: u.AllDomains, AllInbound: u.AllInboundDomains,
	}
	if !in.Global {
		if in.Outbound, in.Inbound, err = h.assignedNames(u); err != nil {
			logf("panel: delete user %d: assigned domains: %v", u.ID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	h.view.Render(w, http.StatusOK, "user-delete", view.NewUserDelete(h.shellMeta(r), in))
}

// assignedNames names the domains a domain user is assigned one by one, outbound
// and inbound: what deleting the user takes away. A direction the user holds as
// All has no names.
func (h *Handlers) assignedNames(u store.User) (outbound, inbound []string, err error) {
	if len(u.DomainIDs) > 0 && !u.AllDomains {
		domains, err := h.store.ListDomains()
		if err != nil {
			return nil, nil, err
		}
		assigned := idSet(u.DomainIDs)
		for _, d := range domains {
			if assigned[d.ID] {
				outbound = append(outbound, d.Name)
			}
		}
	}
	if len(u.InboundDomainIDs) > 0 && !u.AllInboundDomains {
		domains, err := h.store.ListInboundDomains()
		if err != nil {
			return nil, nil, err
		}
		assigned := idSet(u.InboundDomainIDs)
		for _, d := range domains {
			if assigned[d.ID] {
				inbound = append(inbound, d.Name)
			}
		}
	}
	return outbound, inbound, nil
}

// HandleUserDelete performs the deletion confirmed on HandleUserDeleteConfirm
// and returns to the user list.
func (h *Handlers) HandleUserDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	uid, ok := parseUserID(w, r)
	if !ok {
		return
	}
	u, err := h.store.GetUser(uid)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			http.NotFound(w, r)
			return
		}
		logf("panel: get user %d: %v", uid, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.submitUserDelete(w, r, u)
}

func (h *Handlers) submitUserDelete(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := h.principal(r)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if u.ID == p.ID {
		h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: "You cannot delete your own account while signed in.", FormUsername: u.Username, FormEmail: u.Email, FormRole: string(u.Role)})
		return
	}
	if err := h.store.DeleteUser(u.ID); err != nil {
		if errors.Is(err, store.ErrLastGlobal) {
			h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: "Cannot delete the last global administrator.", FormUsername: u.Username, FormEmail: u.Email, FormRole: string(u.Role)})
			return
		}
		logf("panel: delete user %d: %v", u.ID, err)
		h.renderUserForm(w, r, http.StatusInternalServerError, u.ID, userFormView{FormErr: "Could not delete user.", FormUsername: u.Username, FormEmail: u.Email, FormRole: string(u.Role)})
		return
	}
	http.Redirect(w, r, "/server/users?done=deleted", http.StatusSeeOther)
}

func parseUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("uid"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}

// reachFromForm reads the two assignment lists of the user form. With an All
// box ticked the rows under it are ignored — the form works without scripts,
// so both may arrive — and All then also covers domains added later. Where
// inbound is switched off the form has no inbound list, and what the user
// already had there (current) is kept rather than wiped by an unrelated save.
func (h *Handlers) reachFromForm(r *http.Request, current store.Reach) store.Reach {
	reach := store.Reach{
		AllDomains: r.PostFormValue("all_domains") != "",
		AllInbound: current.AllInbound, InboundDomainIDs: current.InboundDomainIDs,
	}
	if !reach.AllDomains {
		reach.DomainIDs = parseIDs(r, "domain_ids")
	}
	if h.cfg.InboundEnabled {
		reach.AllInbound = r.PostFormValue("all_inbound_domains") != ""
		reach.InboundDomainIDs = nil
		if !reach.AllInbound {
			reach.InboundDomainIDs = parseIDs(r, "inbound_domain_ids")
		}
	}
	return reach
}

func parseIDs(r *http.Request, field string) []int64 {
	var ids []int64
	seen := make(map[int64]bool)
	for _, v := range r.PostForm[field] {
		id, err := strconv.ParseInt(v, 10, 64)
		if err == nil && id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func idSet(ids []int64) map[int64]bool {
	m := make(map[int64]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}
