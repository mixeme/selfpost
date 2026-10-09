package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var componentsRoute = route{"GET", "/server/components", func(h *Handlers) http.HandlerFunc { return h.HandleComponents }, nil}

// The kit page is part of Server, so the rule of the subtree holds for it: a
// domain administrator, and a request with no principal at all, get 404 and not
// a hint that the page exists (security.md).
func TestComponentsPageIsGlobalOnly(t *testing.T) {
	h, domains := serverWithTwoDomains(t)

	p := domainAdmin(t, h.store, "kit-reader", domains["first.example.ru"].ID)
	if rec := call(h, componentsRoute, p); rec.Code != http.StatusNotFound {
		t.Errorf("GET /server/components as a domain administrator = %d, want 404:\n%s", rec.Code, rec.Body.String())
	}

	rec := httptest.NewRecorder()
	h.HandleComponents(rec, httptest.NewRequest(http.MethodGet, "/server/components", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /server/components with no principal = %d, want 404", rec.Code)
	}
}

// For the global role it renders the kit: the shell, with the Server group of
// the menu marked, and the page's own partials.
func TestComponentsPageRendersTheKitForTheGlobalRole(t *testing.T) {
	h, _ := serverWithTwoDomains(t)

	rec := call(h, componentsRoute, globalPrincipal)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /server/components as a global administrator = %d, want 200:\n%s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	for _, want := range []string{
		`<h1 class="title is-3">Components</h1>`,
		`href="/static/panel.css"`,
		`<div class="navbar-item has-dropdown is-hoverable sp-current">`,
		`class="sp-postmark sp-fail"`,
		`class="box sp-danger-zone"`,
		`class="sp-record"`,
		`>admin</a>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the kit page is missing %q", want)
		}
	}
	if strings.Contains(out, "/static/legacy.css") {
		t.Error("the kit page loads the old stylesheet")
	}
}
