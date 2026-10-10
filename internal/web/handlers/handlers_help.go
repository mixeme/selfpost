package handlers

import (
	"net/http"

	"github.com/mixeme/selfpost/internal/web/view"
)

// HandleHelp renders the Help page. It is for both roles; the sections of pages
// a viewer cannot open (Overview and Server, Inbound without a reach or with the
// feature off, Outbound without a reach) are left out, as is their menu.
func (h *Handlers) HandleHelp(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.principal(r); !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.view.Render(w, http.StatusOK, "help", view.NewHelp(h.shellMeta(r), h.cfg.InboundEnabled))
}
