package web

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
)

// templates holds the parsed page templates. Each page is parsed together with
// the shared base layout so {{ template "base" . }} works. Rendering goes
// through html/template, which auto-escapes all interpolated data (spec 7.6.7).
type templates struct {
	pages map[string]*template.Template
}

// pageFiles maps a logical page name to its template file. Every page composes
// with layout.html.
var pageFiles = map[string]string{
	"setup":     "templates/setup.html",
	"login":     "templates/login.html",
	"dashboard": "templates/dashboard.html",
}

func loadTemplates() (*templates, error) {
	t := &templates{pages: make(map[string]*template.Template)}
	for name, file := range pageFiles {
		tmpl, err := template.New("layout.html").ParseFS(assetsFS, "templates/layout.html", file)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		t.pages[name] = tmpl
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
