package handlers

import (
	"net/http"

	"github.com/mixeme/selfpost/internal/web/view"
)

// HandleHelp renders the Help page. It is for both roles; the topics about the
// checks of Overview and Health are left out for a viewer who cannot see them.
func (h *Handlers) HandleHelp(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(r)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.view.Render(w, http.StatusOK, "help", view.NewHelp(h.shellMeta(r), p.IsGlobal()))
}
