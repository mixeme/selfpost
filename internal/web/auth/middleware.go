package auth

import (
	"context"
	"net/http"
)

type ctxKey int

const usernameKey ctxKey = 0

// RequireAuth wraps a handler so only requests with a valid session cookie
// reach it; everyone else is redirected to the login page. The authenticated
// username is stashed in the request context for downstream handlers.
func (m *Module) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := m.sessionToken(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		username, ok := m.sessions.Lookup(token)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if isSessionActivity(r) && m.sessions.Touch(token) {
			m.setSessionCookie(w, token)
		}
		ctx := context.WithValue(r.Context(), usernameKey, username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

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
