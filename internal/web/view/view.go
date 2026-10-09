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

// pageFiles maps a logical page name to its template files. Every page
// composes with a layout — layout_legacy.html for the pages still in the old
// design, layout.html and components.html for those that have been rebuilt
// from the component kit (kitPages); pages that embed a polling fragment
// (architecture.md § Panel HTTP surface) list that fragment's file too, so the
// same {{define}} block renders both the initial page and the fragment's own
// refresh responses identically. Pages sharing a block of markup (the
// encryption fields on the two secret downloads) list that partial the same
// way.
var pageFiles = map[string][]string{
	"setup":               {"templates/setup.html"},
	"login":               {"templates/login.html"},
	"overview":            {"templates/overview.html", "templates/overview_cards.html"},
	"health":              {"templates/health.html", "templates/health_body.html"},
	"account":             {"templates/account.html"},
	"settings":            {"templates/settings.html"},
	"users":               {"templates/users.html"},
	"user_form":           {"templates/user_form.html"},
	"user_delete":         {"templates/user_delete.html"},
	"backup":              {"templates/backup.html", "templates/encrypt_fields.html"},
	"out-domains":         {"templates/out_domains.html"},
	"out-domain":          {"templates/out_domain.html"},
	"out-domain-settings": {"templates/out_domain_settings.html"},
	"out-app":             {"templates/out_app.html"},
	"out-app-created":     {"templates/out_app_created.html"},
	"out-domain-delete":   {"templates/out_domain_delete.html"},
	"inbound":             {"templates/inbound.html"},
	"inbound_domain":      {"templates/inbound_domain.html"},
	"inbound_delete":      {"templates/inbound_delete.html"},
	"out-log":             {"templates/out_log.html", "templates/out_log_rows.html"},
	"out-message":         {"templates/out_message.html"},
	"dmarc":               {"templates/dmarc.html"},
	"dmarc-domain":        {"templates/dmarc_domain.html"},
	"dmarc-report":        {"templates/dmarc_report.html"},
	"mail_queue":          {"templates/mail_queue.html", "templates/mail_queue_body.html"},
	"system_log":          {"templates/system_log.html", "templates/system_log_body.html"},
	"help":                {"templates/help.html"},
	"components":          {"templates/kit.html"},
}

// kitPages are the pages built from the component kit: they are rendered by
// layout.html with the partials of components.html and load panel.css. Every
// other page is still the old design — layout_legacy.html and legacy.css — and
// is listed in legacy_pages.txt; a page is in exactly one of the two, and
// restyling one means moving it from the list to here in the same commit.
// pageFiles stays a plain map of file lists because the guard tests read it.
var kitPages = map[string]bool{
	"components": true,
	"login":      true,
	"setup":      true,
	"overview":   true,
	"health":     true,
	"account":    true,
	"settings":   true,

	"out-domains":         true,
	"out-domain":          true,
	"out-domain-settings": true,
	"out-app":             true,
	"out-app-created":     true,
	"out-domain-delete":   true,

	"out-log":      true,
	"out-message":  true,
	"dmarc":        true,
	"dmarc-domain": true,
	"dmarc-report": true,
}

// fragmentFiles maps a fragment name (also its {{define}} block name) to its
// template file, for standalone rendering by the HTMX polling endpoints.
var fragmentFiles = map[string]string{
	"out_log_rows":    "templates/out_log_rows.html",
	"mail_queue_body": "templates/mail_queue_body.html",
	"system_log_body": "templates/system_log_body.html",
	"health_body":     "templates/health_body.html",
	"overview_poll":   "templates/overview_cards.html",
}

// kitFragments are the fragments rendered with the component kit's partials.
var kitFragments = map[string]bool{"health_body": true, "overview_poll": true, "out_log_rows": true}

// New parses embedded templates. version is stamped into every page footer.
func New(version string) (*Engine, error) {
	e := &Engine{
		pages:     make(map[string]*template.Template),
		fragments: make(map[string]*template.Template),
		version:   version,
	}
	for name, files := range pageFiles {
		// A page parses with the layout of its design. The template is named
		// after the layout file so that its content is the layout's.
		layout, shared := "layout_legacy.html", []string{"templates/help_drawer.html"}
		if kitPages[name] {
			layout, shared = "layout.html", []string{"templates/components.html"}
		}
		patterns := append([]string{"templates/" + layout}, append(shared, files...)...)
		tmpl, err := template.New(layout).Funcs(templateFuncs()).ParseFS(assetsFS, patterns...)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		e.pages[name] = tmpl
	}
	for name, file := range fragmentFiles {
		// A fragment of a kit page calls the kit's partials, so it parses with
		// them; the old fragments define everything they use themselves.
		patterns := []string{file}
		if kitFragments[name] {
			patterns = []string{"templates/components.html", file}
		}
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
		"dict": func(values ...any) (map[string]any, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict: odd argument count")
			}
			m := make(map[string]any, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict: key %d is not a string", i)
				}
				m[key] = values[i+1]
			}
			return m, nil
		},
		// back builds the map back_link reads; keeps href and label paired at
		// the call site instead of repeating the <a class="back"> markup.
		"back": func(href, label string) map[string]string {
			return map[string]string{"Href": href, "Label": label}
		},
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
	var (
		buf    bytes.Buffer
		layout string
		frame  = data
	)
	if kitPages[page] {
		// A page of the component kit: the shell is typed data derived from the
		// page's Meta (components.go), and the page's own data travels beside it.
		// With no user there is no navigation — the signed-out screen.
		layout = "layout.html"
		sh := e.shell(metaOf(data), legal.CopyrightLine, legal.SourceURL)
		if sh.User == "" {
			layout = "layout_signed_out"
		}
		frame = Frame{Shell: sh, Page: data}
	} else {
		layout = "layout_legacy.html"
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
			m["InboundEnabled"] = e.inboundEnabled
			m["DMARCEnabled"] = e.dmarcEnabled
		}
	}
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
