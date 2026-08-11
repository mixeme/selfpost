package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/mixeme/selfpost/internal/store"
	"github.com/mixeme/selfpost/internal/web/validate"
	"golang.org/x/crypto/bcrypt"
)

type userFormView struct {
	FormErr      string
	FormUsername string
	FormRole     string
	FormDomains  map[int64]bool
	FormPassword string
}

// HandleUsers lists panel users (global only).
func (h *Handlers) HandleUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireGlobal(w, r); !ok {
		return
	}
	rows, err := h.store.ListUserRows()
	if err != nil {
		logf("panel: list users: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data := h.pageBase(r)
	data["Title"] = "SelfPost — users"
	data["Active"] = "users"
	data["Users"] = rows
	data["Flash"] = usersFlash(r)
	h.view.Render(w, http.StatusOK, "users", data)
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
		h.renderUserForm(w, r, http.StatusOK, 0, userFormView{FormRole: string(store.RoleDomainAdmin)})
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
		selected := make(map[int64]bool, len(u.DomainIDs))
		for _, id := range u.DomainIDs {
			selected[id] = true
		}
		h.renderUserForm(w, r, http.StatusOK, u.ID, userFormView{
			FormUsername: u.Username,
			FormRole:     string(u.Role),
			FormDomains:  selected,
		})
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: "Invalid form submission.", FormUsername: u.Username, FormRole: string(u.Role)})
			return
		}
		if r.PostFormValue("action") == "delete" {
			h.submitUserDelete(w, r, u)
			return
		}
		h.submitUserUpdate(w, r, u)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handlers) renderUserForm(w http.ResponseWriter, r *http.Request, status int, userID int64, view userFormView) {
	domains, err := h.store.ListDomains()
	if err != nil {
		logf("panel: user form: list domains: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data := h.pageBase(r)
	data["Title"] = "SelfPost — user"
	data["Active"] = "users"
	data["UserID"] = userID
	data["Domains"] = domains
	data["Error"] = view.FormErr
	data["FormUsername"] = view.FormUsername
	data["FormRole"] = view.FormRole
	data["GlobalRole"] = store.RoleGlobal
	data["FormDomains"] = view.FormDomains
	data["FormPassword"] = view.FormPassword
	data["IsEdit"] = userID != 0
	data["LastGlobalLocked"] = lastGlobalLocked(h, userID, view.FormRole)
	h.view.Render(w, status, "user_form", data)
}

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
	password := r.PostFormValue("password")
	role := store.Role(r.PostFormValue("role"))
	domainIDs := parseDomainIDs(r)

	if err := validate.Username(username); err != nil {
		h.renderUserForm(w, r, http.StatusBadRequest, 0, userFormView{FormErr: err.Error(), FormUsername: username, FormRole: string(role), FormDomains: domainIDSetFromForm(r)})
		return
	}
	if err := validate.AdminPassword(password); err != nil {
		h.renderUserForm(w, r, http.StatusBadRequest, 0, userFormView{FormErr: err.Error(), FormUsername: username, FormRole: string(role), FormDomains: domainIDSetFromForm(r)})
		return
	}
	if role != store.RoleGlobal && role != store.RoleDomainAdmin {
		h.renderUserForm(w, r, http.StatusBadRequest, 0, userFormView{FormErr: "Choose a valid role.", FormUsername: username, FormRole: string(role), FormDomains: domainIDSetFromForm(r)})
		return
	}
	if role == store.RoleDomainAdmin && len(domainIDs) == 0 {
		h.renderUserForm(w, r, http.StatusBadRequest, 0, userFormView{FormErr: "Select at least one domain for a domain administrator.", FormUsername: username, FormRole: string(role), FormDomains: domainIDSetFromForm(r)})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		logf("panel: create user hash: %v", err)
		h.renderUserForm(w, r, http.StatusInternalServerError, 0, userFormView{FormErr: "Internal error. Please try again."})
		return
	}
	if _, err := h.store.CreateUser(username, string(hash), role, domainIDs); err != nil {
		if errors.Is(err, store.ErrUserExists) {
			h.renderUserForm(w, r, http.StatusConflict, 0, userFormView{FormErr: "That username is already in use.", FormUsername: username, FormRole: string(role), FormDomains: domainIDSetFromForm(r)})
			return
		}
		logf("panel: create user: %v", err)
		h.renderUserForm(w, r, http.StatusInternalServerError, 0, userFormView{FormErr: "Could not create user. Please check the logs."})
		return
	}
	http.Redirect(w, r, "/users?done=created", http.StatusSeeOther)
}

func (h *Handlers) submitUserUpdate(w http.ResponseWriter, r *http.Request, u store.User) {
	if err := r.ParseForm(); err != nil {
		h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: "Invalid form submission.", FormUsername: u.Username, FormRole: string(u.Role)})
		return
	}
	p, ok := h.principal(r)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	role := store.Role(r.PostFormValue("role"))
	domainIDs := parseDomainIDs(r)
	selected := domainIDSetFromForm(r)

	if username == "" {
		username = u.Username
	}
	if err := validate.Username(username); err != nil {
		h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: err.Error(), FormUsername: username, FormRole: string(role), FormDomains: selected})
		return
	}
	if role != store.RoleGlobal && role != store.RoleDomainAdmin {
		h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: "Choose a valid role.", FormUsername: username, FormRole: string(role), FormDomains: selected})
		return
	}
	if role == store.RoleDomainAdmin && len(domainIDs) == 0 {
		h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: "Select at least one domain for a domain administrator.", FormUsername: username, FormRole: string(role), FormDomains: selected})
		return
	}

	if u.Role == store.RoleGlobal && role == store.RoleDomainAdmin {
		n, err := h.store.CountGlobalUsers()
		if err != nil || n <= 1 {
			h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: "Cannot demote the last global administrator.", FormUsername: username, FormRole: string(u.Role), FormDomains: selected})
			return
		}
	}

	if u.ID == p.ID && u.Role == store.RoleGlobal && role == store.RoleDomainAdmin {
		n, err := h.store.CountGlobalUsers()
		if err != nil || n <= 1 {
			h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: "You cannot demote yourself without another global administrator.", FormUsername: username, FormRole: string(u.Role), FormDomains: selected})
			return
		}
	}

	hash := u.PasswordHash
	if password != "" {
		if err := validate.AdminPassword(password); err != nil {
			h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: err.Error(), FormUsername: username, FormRole: string(role), FormDomains: selected})
			return
		}
		newHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			logf("panel: update user hash: %v", err)
			h.renderUserForm(w, r, http.StatusInternalServerError, u.ID, userFormView{FormErr: "Internal error. Please try again.", FormUsername: username, FormRole: string(role), FormDomains: selected})
			return
		}
		hash = string(newHash)
	}

	if err := h.store.UpdateUser(u.ID, username, hash, u.DMARCReportEmail); err != nil {
		if errors.Is(err, store.ErrUserExists) {
			h.renderUserForm(w, r, http.StatusConflict, u.ID, userFormView{FormErr: "That username is already in use.", FormUsername: username, FormRole: string(role), FormDomains: selected})
			return
		}
		logf("panel: update user: %v", err)
		h.renderUserForm(w, r, http.StatusInternalServerError, u.ID, userFormView{FormErr: "Could not save user. Please check the logs.", FormUsername: username, FormRole: string(role), FormDomains: selected})
		return
	}

	if role != u.Role {
		if err := h.store.SetUserRole(u.ID, role); err != nil {
			logf("panel: set user role: %v", err)
			h.renderUserForm(w, r, http.StatusInternalServerError, u.ID, userFormView{FormErr: "Could not update role.", FormUsername: username, FormRole: string(role), FormDomains: selected})
			return
		}
		if role == store.RoleGlobal {
			if err := h.store.ClearUserDomains(u.ID); err != nil {
				logf("panel: clear user domains: %v", err)
			}
		}
	}

	if role == store.RoleDomainAdmin {
		if err := h.store.SetUserDomains(u.ID, domainIDs); err != nil {
			logf("panel: set user domains: %v", err)
			h.renderUserForm(w, r, http.StatusInternalServerError, u.ID, userFormView{FormErr: "Could not save domain assignments.", FormUsername: username, FormRole: string(role), FormDomains: selected})
			return
		}
	}

	http.Redirect(w, r, "/users?done=updated", http.StatusSeeOther)
}

func (h *Handlers) submitUserDelete(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := h.principal(r)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if u.ID == p.ID {
		h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: "You cannot delete your own account while signed in.", FormUsername: u.Username, FormRole: string(u.Role)})
		return
	}
	if err := h.store.DeleteUser(u.ID); err != nil {
		if errors.Is(err, store.ErrLastGlobal) {
			h.renderUserForm(w, r, http.StatusBadRequest, u.ID, userFormView{FormErr: "Cannot delete the last global administrator.", FormUsername: u.Username, FormRole: string(u.Role)})
			return
		}
		logf("panel: delete user %d: %v", u.ID, err)
		h.renderUserForm(w, r, http.StatusInternalServerError, u.ID, userFormView{FormErr: "Could not delete user.", FormUsername: u.Username, FormRole: string(u.Role)})
		return
	}
	http.Redirect(w, r, "/users?done=deleted", http.StatusSeeOther)
}

func parseUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("uid"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}

func parseDomainIDs(r *http.Request) []int64 {
	var ids []int64
	for _, v := range r.PostForm["domain_ids"] {
		id, err := strconv.ParseInt(v, 10, 64)
		if err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func domainIDSetFromForm(r *http.Request) map[int64]bool {
	m := make(map[int64]bool)
	for _, id := range parseDomainIDs(r) {
		m[id] = true
	}
	return m
}
