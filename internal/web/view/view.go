// Package view embeds the panel's HTML templates and static assets and renders
// pages and HTMX polling fragments.
package view

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path"

	"github.com/mixeme/selfpost/internal/legal"
)

//go:embed templates/*.html static/*
var assetsFS embed.FS

// Engine holds parsed page and fragment templates.
type Engine struct {
	pages          map[string]*template.Template
	fragments      map[string]*template.Template
	version        string
	inboundEnabled bool
	dmarcEnabled   bool
}

// pageFiles maps a logical page name to its template files. Every page is built
// from the component kit: layout.html and components.html, then the page's own
// file. Pages that embed a polling fragment (architecture.md § Panel HTTP
// surface) list that fragment's file too, so the same {{define}} block renders
// both the initial page and the fragment's own refresh responses identically.
// The guard tests read this map.
var pageFiles = map[string][]string{
	"setup":               {"templates/setup.html"},
	"login":               {"templates/login.html"},
	"overview":            {"templates/overview.html", "templates/overview_cards.html"},
	"health":              {"templates/health.html", "templates/health_body.html"},
	"account":             {"templates/account.html"},
	"settings":            {"templates/settings.html"},
	"system-log":          {"templates/system_log.html", "templates/system_log_body.html"},
	"backup":              {"templates/backup.html"},
	"users":               {"templates/users.html"},
	"user":                {"templates/user.html"},
	"user-delete":         {"templates/user_delete.html"},
	"help":                {"templates/help.html"},
	"out-domains":         {"templates/out_domains.html"},
	"out-domain":          {"templates/out_domain.html"},
	"out-domain-settings": {"templates/out_domain_settings.html"},
	"out-app":             {"templates/out_app.html"},
	"out-app-created":     {"templates/out_app_created.html"},
	"out-domain-delete":   {"templates/out_domain_delete.html"},
	"in-domains":          {"templates/in_domains.html"},
	"in-domain":           {"templates/in_domain.html"},
	"in-domain-delete":    {"templates/in_domain_delete.html"},
	"out-log":             {"templates/out_log.html", "templates/out_log_rows.html"},
	"out-message":         {"templates/out_message.html"},
	"dmarc":               {"templates/dmarc.html"},
	"dmarc-domain":        {"templates/dmarc_domain.html"},
	"dmarc-report":        {"templates/dmarc_report.html"},
	"out-queue":           {"templates/out_queue.html", "templates/out_queue_body.html"},
	"components":          {"templates/kit.html"},
}

// fragmentFiles maps a fragment name (also its {{define}} block name) to its
// template file, for standalone rendering by the HTMX polling endpoints.
var fragmentFiles = map[string]string{
	"out_log_rows":    "templates/out_log_rows.html",
	"out_queue_body":  "templates/out_queue_body.html",
	"system_log_body": "templates/system_log_body.html",
	"health_body":     "templates/health_body.html",
	"overview_poll":   "templates/overview_cards.html",
}

// New parses embedded templates. version is stamped into every page footer.
func New(version string) (*Engine, error) {
	e := &Engine{
		pages:     make(map[string]*template.Template),
		fragments: make(map[string]*template.Template),
		version:   version,
	}
	for name, files := range pageFiles {
		// A page parses with the layout and the kit's partials. The template
		// is named after the layout file so that its content is the layout's.
		const layout = "layout.html"
		patterns := append([]string{"templates/" + layout, "templates/components.html"}, files...)
		tmpl, err := template.New(layout).Funcs(templateFuncs()).ParseFS(assetsFS, patterns...)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		e.pages[name] = tmpl
	}
	for name, file := range fragmentFiles {
		// A fragment calls the kit's partials, so it parses with them.
		patterns := []string{"templates/components.html", file}
		tmpl, err := template.New(path.Base(file)).Funcs(templateFuncs()).ParseFS(assetsFS, patterns...)
		if err != nil {
			return nil, fmt.Errorf("parse fragment %s: %w", name, err)
		}
		e.fragments[name] = tmpl
	}
	return e, nil
}

// SetInboundEnabled controls whether the Inbound nav item is shown. The
// listener and routes are gated the same way (INBOUND_RELAY_ENABLE).
func (e *Engine) SetInboundEnabled(v bool) {
	e.inboundEnabled = v
}

// SetDMARCEnabled controls whether the DMARC nav item is shown.
func (e *Engine) SetDMARCEnabled(v bool) {
	e.dmarcEnabled = v
}

// templateFuncs supplies helpers shared across page templates.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		// status_tag and wbr_at are the adapters of the component kit
		// (components.go): the one mapping of a status to Bulma's tag
		// classes, and the break opportunity after the @ of an address.
		"status_tag": StatusTag,
		"wbr_at":     wbrAt,
	}
}

// Page returns a parsed page template by logical name. It is exported for
// template guard tests that assert structural properties across all pages.
func (e *Engine) Page(name string) *template.Template {
	return e.pages[name]
}

// Pages returns all parsed page templates keyed by logical name.
func (e *Engine) Pages() map[string]*template.Template {
	return e.pages
}

// Render writes a page using its layout. Rendering to a buffer first means a
// template error yields a clean 500 instead of a half-written page.
func (e *Engine) Render(w http.ResponseWriter, status int, page string, data any) {
	tmpl, ok := e.pages[page]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	// The shell is typed data derived from the page's Meta (components.go), and
	// the page's own data travels beside it. With no user there is no
	// navigation — the signed-out screen.
	var buf bytes.Buffer
	layout := "layout.html"
	sh := e.shell(metaOf(data), legal.CopyrightLine, legal.SourceURL)
	if sh.User == "" {
		layout = "layout_signed_out"
	}
	frame := Frame{Shell: sh, Page: data}
	if err := tmpl.ExecuteTemplate(&buf, layout, frame); err != nil {
		log.Printf("panel: render %s: %v", page, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// RenderFragment writes an HTMX polling fragment as a bare HTML snippet, with
// no surrounding layout (architecture.md § Panel HTTP surface: fragment
// endpoints return HTML, not JSON).
func (e *Engine) RenderFragment(w http.ResponseWriter, status int, name string, data any) {
	tmpl, ok := e.fragments[name]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("panel: render fragment %s: %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}
