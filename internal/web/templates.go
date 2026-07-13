package web

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
)

// templates holds the parsed page and fragment templates. Each page is parsed
// together with the shared base layout so {{ template "base" . }} works.
// Fragments (HTMX polling targets, spec 7.1) are parsed standalone, without
// the layout, so they can be swapped into an existing page as an HTML snippet
// rather than a full document. Rendering always goes through html/template,
// which auto-escapes all interpolated data regardless (spec 7.6.7).
type templates struct {
	pages     map[string]*template.Template
	fragments map[string]*template.Template
}

// pageFiles maps a logical page name to its template files. Every page
// composes with layout.html; pages that embed a polling fragment (spec 7.1)
// list that fragment's file too, so the same {{define}} block renders both
// the initial page and the fragment's own refresh responses identically.
var pageFiles = map[string][]string{
	"setup":         {"templates/setup.html"},
	"login":         {"templates/login.html"},
	"dashboard":     {"templates/dashboard.html"},
	"domain_detail": {"templates/domain_detail.html"},
	"domain_delete": {"templates/domain_delete.html"},
	"sendlog":       {"templates/sendlog.html", "templates/sendlog_rows.html"},
	"queue":         {"templates/queue.html", "templates/queue_body.html"},
	"logtail":       {"templates/logtail.html", "templates/logtail_body.html"},
}

// fragmentFiles maps a fragment name (also its {{define}} block name) to its
// template file, for standalone rendering by the HTMX polling endpoints.
var fragmentFiles = map[string]string{
	"sendlog_rows": "templates/sendlog_rows.html",
	"queue_body":   "templates/queue_body.html",
	"logtail_body": "templates/logtail_body.html",
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
// no surrounding layout (spec 7.1: fragment endpoints return HTML, not JSON).
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
