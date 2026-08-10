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

	"github.com/mixeme/selfpost/internal/legal"
)

//go:embed templates/*.html static/*
var assetsFS embed.FS

// Engine holds parsed page and fragment templates.
type Engine struct {
	pages     map[string]*template.Template
	fragments map[string]*template.Template
	version   string
}

// pageFiles maps a logical page name to its template files. Every page
// composes with layout.html; pages that embed a polling fragment
// (architecture.md § Panel HTTP surface) list that fragment's file too, so the
// same {{define}} block renders both the initial page and the fragment's own
// refresh responses identically. Pages sharing a block of markup (the
// encryption fields on the two secret downloads) list that partial the same
// way.
var pageFiles = map[string][]string{
	"setup":         {"templates/setup.html"},
	"login":         {"templates/login.html"},
	"dashboard":     {"templates/dashboard.html"},
	"account":       {"templates/account.html"},
	"backup":        {"templates/backup.html", "templates/encrypt_fields.html"},
	"domain_detail": {"templates/domain_detail.html", "templates/encrypt_fields.html"},
	"domain_delete": {"templates/domain_delete.html"},
	"deliveries":    {"templates/deliveries.html", "templates/deliveries_rows.html"},
	"delivery":      {"templates/delivery.html"},
	"mail_queue":    {"templates/mail_queue.html", "templates/mail_queue_body.html"},
	"system_log":    {"templates/system_log.html", "templates/system_log_body.html"},
	"status":        {"templates/status.html", "templates/status_body.html"},
}

// fragmentFiles maps a fragment name (also its {{define}} block name) to its
// template file, for standalone rendering by the HTMX polling endpoints.
var fragmentFiles = map[string]string{
	"deliveries_rows": "templates/deliveries_rows.html",
	"mail_queue_body": "templates/mail_queue_body.html",
	"system_log_body": "templates/system_log_body.html",
	"status_body":     "templates/status_body.html",
}

// New parses embedded templates. version is stamped into every page footer.
func New(version string) (*Engine, error) {
	e := &Engine{
		pages:     make(map[string]*template.Template),
		fragments: make(map[string]*template.Template),
		version:   version,
	}
	for name, files := range pageFiles {
		patterns := append([]string{"templates/layout.html"}, files...)
		tmpl, err := template.New("layout.html").ParseFS(assetsFS, patterns...)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		e.pages[name] = tmpl
	}
	for name, file := range fragmentFiles {
		tmpl, err := template.ParseFS(assetsFS, file)
		if err != nil {
			return nil, fmt.Errorf("parse fragment %s: %w", name, err)
		}
		e.fragments[name] = tmpl
	}
	return e, nil
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

// Render writes a page using the base layout. Rendering to a buffer first means
// a template error yields a clean 500 instead of a half-written page.
func (e *Engine) Render(w http.ResponseWriter, status int, page string, data any) {
	tmpl, ok := e.pages[page]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	// The layout's navigation compares .Active against each item, so the key
	// must exist on every authenticated page. Defaulting it here keeps a page
	// that forgets it from failing to render — it simply highlights nothing.
	// Footer fields (.Version, .Copyright, .SourceURL) are the same on every
	// page, so no handler should have to pass them.
	if m, ok := data.(map[string]any); ok {
		if _, has := m["Active"]; !has {
			m["Active"] = ""
		}
		m["Version"] = e.version
		m["Copyright"] = legal.CopyrightLine
		m["SourceURL"] = legal.SourceURL
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout.html", data); err != nil {
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
