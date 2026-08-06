package web

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
)

// templates holds the parsed page and fragment templates. Each page is parsed
// together with the shared base layout so {{ template "base" . }} works.
// Fragments (HTMX polling targets, architecture.md § Panel HTTP surface) are
// parsed standalone, without the layout, so they can be swapped into an
// existing page as an HTML snippet rather than a full document. Rendering
// always goes through html/template, which auto-escapes all interpolated data
// regardless (security.md).
type templates struct {
	pages     map[string]*template.Template
	fragments map[string]*template.Template
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

func loadTemplates() (*templates, error) {
	t := &templates{
		pages:     make(map[string]*template.Template),
		fragments: make(map[string]*template.Template),
	}
	for name, files := range pageFiles {
		patterns := append([]string{"templates/layout.html"}, files...)
		tmpl, err := template.New("layout.html").ParseFS(assetsFS, patterns...)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		t.pages[name] = tmpl
	}
	for name, file := range fragmentFiles {
		tmpl, err := template.ParseFS(assetsFS, file)
		if err != nil {
			return nil, fmt.Errorf("parse fragment %s: %w", name, err)
		}
		t.fragments[name] = tmpl
	}
	return t, nil
}

// render writes a page using the base layout. Rendering to a buffer first means
// a template error yields a clean 500 instead of a half-written page.
func (s *Server) render(w http.ResponseWriter, status int, page string, data any) {
	tmpl, ok := s.tmpl.pages[page]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	// The layout's navigation compares .Active against each item, so the key
	// must exist on every authenticated page. Defaulting it here keeps a page
	// that forgets it from failing to render — it simply highlights nothing.
	// .Version, shown in the layout's footer, is supplied the same way: it is
	// the same value on every page, so no handler should have to pass it.
	if m, ok := data.(map[string]any); ok {
		if _, has := m["Active"]; !has {
			m["Active"] = ""
		}
		m["Version"] = s.cfg.Version
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout.html", data); err != nil {
		logf("panel: render %s: %v", page, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// renderFragment writes an HTMX polling fragment as a bare HTML snippet, with
// no surrounding layout (architecture.md § Panel HTTP surface: fragment
// endpoints return HTML, not JSON).
func (s *Server) renderFragment(w http.ResponseWriter, status int, name string, data any) {
	tmpl, ok := s.tmpl.fragments[name]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		logf("panel: render fragment %s: %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}
