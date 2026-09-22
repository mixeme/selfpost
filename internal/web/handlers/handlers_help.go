package handlers

import (
	"net/http"

	"github.com/mixeme/selfpost/internal/web/auth"
)

func (h *Handlers) HandleHelp(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(r)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data := map[string]any{
		"Title":    "SelfPost — help",
		"User":     auth.CurrentUser(r),
		"Active":   "help",
		"IsGlobal": p.IsGlobal(),
	}
	h.view.Render(w, http.StatusOK, "help", data)
}
