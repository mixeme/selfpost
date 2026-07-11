package web

import (
	"context"
	"net/http"
)

type ctxKey int

const usernameKey ctxKey = 0

// requireAuth wraps a handler so only requests with a valid session cookie
// reach it; everyone else is redirected to the login page. The authenticated
// username is stashed in the request context for downstream handlers.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		username, ok := s.sessions.Lookup(c.Value)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), usernameKey, username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// currentUser returns the authenticated username from the request context.
func currentUser(r *http.Request) string {
	if v, ok := r.Context().Value(usernameKey).(string); ok {
		return v
	}
	return ""
}
