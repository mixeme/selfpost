package web

import (
	"net/http"

	"github.com/mixeme/selfpost/internal/legal"
)

// handleLicense serves the AGPL-3.0 text so every interactive page's
// "License" footer link works without leaving the panel (AGPL Appropriate
// Legal Notices). Unauthenticated on purpose: the notice must be reachable
// from the login and setup screens too.
func (s *Server) handleLicense(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(legal.License)
}
