package auth

import "testing"

// A rejected request's path is logged without the token of a link
// (security review, 2026-10-17).
func TestLoggedPathHidesTokens(t *testing.T) {
	for in, want := range map[string]string{
		"/login/reset/abcDEF123": "/login/reset/…",
		"/confirm/abcDEF123":     "/confirm/…",
		"/login/reset":           "/login/reset",
		"/confirm/":              "/confirm/",
		"/account/profile":       "/account/profile",
	} {
		if got := LoggedPath(in); got != want {
			t.Errorf("LoggedPath(%q) = %q, want %q", in, got, want)
		}
	}
}
