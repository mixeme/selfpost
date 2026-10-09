package handlers

import (
	"net/http"

	"github.com/mixeme/selfpost/internal/web/auth"
	"github.com/mixeme/selfpost/internal/web/view"
)

// HandleComponents serves the kit page: every partial of the panel's component
// kit in every state, rendered from fixtures (view.KitPage). It is the reference
// a reviewer holds a restyled page against, and it is part of Server, so only
// the global role sees it — a domain administrator is answered 404, as on every
// other Server page.
func (h *Handlers) HandleComponents(w http.ResponseWriter, r *http.Request) {
	p, ok := h.requireGlobal(w, r)
	if !ok {
		return
	}
	page := view.KitPage()
	page.User = auth.CurrentUser(r)
	page.IsGlobal = p.IsGlobal()
	h.view.Render(w, http.StatusOK, "components", page)
}
