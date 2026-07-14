package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"codeberg.org/mix/selfpost/internal/store"
)

// newCred carries a freshly generated login/password to the template so it can
// be shown exactly once (spec 7.6.1). It is never read back from storage.
type newCred struct {
	Login    string
	Password string
}

// detailView holds the one-shot, request-specific extras layered on top of a
// domain's persistent state when rendering its page: an application-form error,
// the values to repopulate that form, and any just-issued credential to show
// once.
type detailView struct {
	FormErr   string
	FormLogin string
	FormMode  string
	FormAddrs string
	NewCred   *newCred
	// RateLimitErr surfaces a validation error from a domain- or application-level
	// rate-limit form (spec 7.4) as a page banner.
	RateLimitErr string
}

// appRateLimitView pairs an application with its differentiated rate-limit
// settings for the domain page. store.Application is embedded so the existing
// template fields (Login, AddressMode, Addresses, ID) resolve unchanged.
type appRateLimitView struct {
	store.Application
	HasLimit  bool   // an active limit is configured
	IPsText   string // allowed IPs, newline-joined for the textarea
	MaxText   string // message ceiling, blank when unset
	WindowVal string // window seconds, defaulted when unset
}

// handleDomainDetail shows a single domain: its DKIM DNS record (spec 7.2.10)
// and its applications with the controls to add, edit, delete and re-issue
// credentials (spec 7.2.5-9).
func (s *Server) handleDomainDetail(w http.ResponseWriter, r *http.Request) {
	d, ok := s.lookupDomain(w, r)
	if !ok {
		return
	}
	s.renderDomainDetail(w, r, http.StatusOK, d, detailView{FormMode: store.AddressModeWildcard})
}

// renderDomainDetail renders the domain page. view supplies request-specific
// extras (form error/values, a one-time credential); everything else is loaded
// fresh from the stores so the page always reflects committed state.
func (s *Server) renderDomainDetail(w http.ResponseWriter, r *http.Request, status int, d store.Domain, view detailView) {
	record, err := s.domains.DKIMRecord(d)
	if err != nil {
		logf("panel: domain %d: dkim record: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	apps, err := s.apps.List(d.ID)
	if err != nil {
		logf("panel: domain %d: list applications: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	appViews := make([]appRateLimitView, 0, len(apps))
	for _, a := range apps {
		rl, ok, err := s.apps.RateLimit(a.ID)
		if err != nil {
			logf("panel: application %d: rate limit: %v", a.ID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		appViews = append(appViews, appRateLimitView{
			Application: a,
			HasLimit:    ok && rl.Active(),
			IPsText:     strings.Join(rl.AllowedIPs, "\n"),
			MaxText:     intOrBlank(rl.MaxMessages),
			WindowVal:   windowOrDefault(rl.WindowSeconds),
		})
	}

	domainRL, domainRLok, err := s.domains.RateLimit(d.ID)
	if err != nil {
		logf("panel: domain %d: rate limit: %v", d.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.render(w, status, "domain_detail", map[string]any{
		"Title":        "SelfPost — " + d.Name,
		"User":         currentUser(r),
		"Domain":       d,
		"Record":       record,
		"Apps":         appViews,
		"Error":        view.FormErr,
		"FormLogin":    view.FormLogin,
		"FormMode":     view.FormMode,
		"FormAddrs":    view.FormAddrs,
		"NewCred":      view.NewCred,
		"Flash":        detailFlash(r),
		"Wildcard":     store.AddressModeWildcard,
		"List":         store.AddressModeList,
		"RateLimitErr": view.RateLimitErr,
		"DomainHasRL":  domainRLok && domainRL.Active(),
		"DomainRLIPs":  strings.Join(domainRL.AllowedIPs, "\n"),
		"DomainRLMax":  intOrBlank(domainRL.MaxMessages),
		"DomainRLWin":  windowOrDefault(domainRL.WindowSeconds),
	})
}

// intOrBlank renders a non-positive number as an empty string so an unset field
// shows blank rather than "0".
func intOrBlank(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// windowOrDefault renders the window seconds, substituting the default when
// unset so the form always suggests a sensible value.
func windowOrDefault(n int) string {
	if n <= 0 {
		return strconv.Itoa(defaultRateLimitWindowSeconds)
	}
	return strconv.Itoa(n)
}

// detailFlash maps a fixed redirect flag to a fixed message, so status text
// after a redirect is never attacker-influenced.
func detailFlash(r *http.Request) string {
	switch {
	case r.URL.Query().Get("appdeleted") != "":
		return "Application deleted."
	case r.URL.Query().Get("modeupdated") != "":
		return "Application address mode updated."
	case r.URL.Query().Get("ratelimit") != "":
		return "Rate limit updated."
	default:
		return ""
	}
}

// handleAddApplication creates an application on a domain and renders the page
// back with the generated password shown once (spec 7.2.5, 7.6.1). Because the
// password cannot be recovered later, this deliberately renders inline rather
// than redirecting.
func (s *Server) handleAddApplication(w http.ResponseWriter, r *http.Request) {
	d, ok := s.lookupDomain(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderDomainDetail(w, r, http.StatusBadRequest, d,
			detailView{FormErr: "Invalid form submission.", FormMode: store.AddressModeWildcard})
		return
	}
	login := strings.TrimSpace(r.PostFormValue("login"))
	mode := r.PostFormValue("mode")
	addrs := splitAddresses(r.PostFormValue("addresses"))

	repopulate := detailView{
		FormLogin: login,
		FormMode:  mode,
		FormAddrs: r.PostFormValue("addresses"),
	}

	a, password, err := s.apps.Create(d.ID, login, mode, addrs)
	if err != nil {
		repopulate.FormErr = applicationErrorMessage(err)
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrLoginExists) {
			status = http.StatusConflict
		}
		s.renderDomainDetail(w, r, status, d, repopulate)
		return
	}
	s.renderDomainDetail(w, r, http.StatusCreated, d, detailView{
		FormMode: store.AddressModeWildcard,
		NewCred:  &newCred{Login: a.Login, Password: password},
	})
}

// handleUpdateAppMode switches an application's address mode / list (spec 7.2.7).
func (s *Server) handleUpdateAppMode(w http.ResponseWriter, r *http.Request) {
	a, ok := s.lookupApplication(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	mode := r.PostFormValue("mode")
	addrs := splitAddresses(r.PostFormValue("addresses"))

	if err := s.apps.UpdateMode(a.ID, mode, addrs); err != nil {
		d, derr := s.domains.Get(a.DomainID)
		if derr != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		s.renderDomainDetail(w, r, http.StatusBadRequest, d, detailView{
			FormErr:  fmt.Sprintf("Could not update %s: %s", a.Login, applicationErrorMessage(err)),
			FormMode: store.AddressModeWildcard,
		})
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/domains/%d?modeupdated=1", a.DomainID), http.StatusSeeOther)
}

// handleRegenPassword issues a new password for an application and shows it once
// (spec 7.2.9, 7.6.1). Rendered inline, like creation, so the password is visible.
func (s *Server) handleRegenPassword(w http.ResponseWriter, r *http.Request) {
	a, ok := s.lookupApplication(w, r)
	if !ok {
		return
	}
	d, err := s.domains.Get(a.DomainID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	password, err := s.apps.RegeneratePassword(a.ID)
	if err != nil {
		logf("panel: regenerate password for application %d: %v", a.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.renderDomainDetail(w, r, http.StatusOK, d, detailView{
		FormMode: store.AddressModeWildcard,
		NewCred:  &newCred{Login: a.Login, Password: password},
	})
}

// handleDeleteApplication removes an application and returns to its domain page
// (spec 7.2.8).
func (s *Server) handleDeleteApplication(w http.ResponseWriter, r *http.Request) {
	a, ok := s.lookupApplication(w, r)
	if !ok {
		return
	}
	if err := s.apps.Delete(a.ID); err != nil {
		logf("panel: delete application %d: %v", a.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/domains/%d?appdeleted=1", a.DomainID), http.StatusSeeOther)
}

// lookupApplication resolves the {aid} path value to an application, writing a
// 404 for a bad id or missing application.
func (s *Server) lookupApplication(w http.ResponseWriter, r *http.Request) (store.Application, bool) {
	id, err := strconv.ParseInt(r.PathValue("aid"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return store.Application{}, false
	}
	a, err := s.apps.Get(id)
	if err != nil {
		if errors.Is(err, store.ErrApplicationNotFound) {
			http.NotFound(w, r)
			return store.Application{}, false
		}
		logf("panel: get application %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return store.Application{}, false
	}
	return a, true
}

// splitAddresses turns the textarea/field input (addresses separated by
// newlines, commas or whitespace) into a raw slice. Normalisation and
// validation happen in the app service (spec 7.6.2).
func splitAddresses(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ' ' || r == '\t' || r == ';'
	})
}

// applicationErrorMessage turns a service error into a user-facing message,
// passing through the validation errors (which are safe, fixed strings) and
// masking anything unexpected.
func applicationErrorMessage(err error) string {
	switch {
	case errors.Is(err, store.ErrLoginExists):
		return "That login is already in use. Choose another."
	case errors.Is(err, store.ErrDomainNotFound), errors.Is(err, store.ErrApplicationNotFound):
		return "The item no longer exists."
	default:
		// Validation errors from the app service are safe to surface verbatim;
		// they describe what the admin must fix (login/address rules).
		return err.Error()
	}
}
