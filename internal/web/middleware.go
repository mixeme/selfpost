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
//
// It also extends the sliding session (plan B.1) on activity, defined as
// everything except a GET request carrying HX-Request: the four monitoring
// fragments (/status/fragment, /mail-queue/body, /system-log/body,
// /deliveries/rows)
// poll on a timer regardless of whether anyone is looking at the tab, so
// counting those as activity would make "N days idle" mean "N days since a
// browser tab was last open" instead.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := s.sessionToken(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		username, ok := s.sessions.Lookup(token)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if isSessionActivity(r) && s.sessions.Touch(token) {
			s.setSessionCookie(w, token)
		}
		ctx := context.WithValue(r.Context(), usernameKey, username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// isSessionActivity reports whether a request counts as administrator
// activity for the sliding session timeout, per requireAuth's doc comment.
func isSessionActivity(r *http.Request) bool {
	return !(r.Method == http.MethodGet && r.Header.Get("HX-Request") != "")
}

// currentUser returns the authenticated username from the request context.
func currentUser(r *http.Request) string {
	if v, ok := r.Context().Value(usernameKey).(string); ok {
		return v
	}
	return ""
}
