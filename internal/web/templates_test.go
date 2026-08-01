package web

import (
	"bytes"
	"strings"
	"testing"
)

// The navigation is rendered from the layout, not copied into each page, so
// every page template must resolve it. This is what makes "the nav is on every
// authenticated page" a structural property instead of a checklist item.
func TestEveryPageResolvesNav(t *testing.T) {
	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	for name, page := range tmpl.pages {
		if page.Lookup("nav") == nil {
			t.Errorf("page %q does not resolve the shared nav template", name)
		}
	}
}

func TestNavMarksActivePage(t *testing.T) {
	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	var buf bytes.Buffer
	err = tmpl.pages["dashboard"].ExecuteTemplate(&buf, "nav", map[string]any{
		"User":   "admin",
		"Active": "queue",
	})
	if err != nil {
		t.Fatalf("execute nav: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `<span aria-current="page">Queue</span>`) {
		t.Errorf("active page is not marked:\n%s", out)
	}
	if strings.Contains(out, `href="/queue"`) {
		t.Errorf("active page still links to itself:\n%s", out)
	}
	if !strings.Contains(out, `href="/sendlog"`) {
		t.Errorf("inactive pages are not linked:\n%s", out)
	}
}
