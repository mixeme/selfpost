package auth

import (
	"net/http"
)

// RequireAuth wraps a handler so only requests with a valid session cookie
// reach it; everyone else is redirected to the login page. The authenticated
// principal is stashed in the request context for downstream handlers.
func (m *Module) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := m.sessionToken(r)
		if !ok {
			toLogin(w, r)
			return
		}
		userID, ok := m.sessions.Lookup(token)
		if !ok {
			toLogin(w, r)
			return
		}
		u, err := m.store.GetUser(userID)
		if err != nil {
			toLogin(w, r)
			return
		}
		// Only a session whose user exists is kept alive by being used.
		if isSessionActivity(r) && m.sessions.Touch(token) {
			m.setSessionCookie(w, token)
		}
		p := principalFromUser(u)
		ctx := withPrincipal(r.Context(), p)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// toLogin sends a request without a session to the sign-in page. A browser
// gets a redirect; htmx gets HX-Redirect, which it follows with the whole
// page, before it looks at the status (htmx 2.0.4, src/htmx.js
// handleAjaxResponse(): location.href = the header's value). A 303 to htmx
// was followed inside the request, and the sign-in page swapped into the box
// that asked (audit 2026-10-12, M11).
func toLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") != "" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// isSessionActivity: a GET from htmx is a background poll - every panel hx-get
// is a load or a poll - and does not renew the session. htmx 2.0.4, src/htmx.js
// getHeaders(): every request it issues carries HX-Request: true (and
// loadHistoryFromServer() sets it too).
func isSessionActivity(r *http.Request) bool {
	return !(r.Method == http.MethodGet && r.Header.Get("HX-Request") != "")
}

// CurrentUser returns the authenticated username from the request context.
func CurrentUser(r *http.Request) string {
	if v, ok := r.Context().Value(usernameKey).(string); ok {
		return v
	}
	return ""
}
