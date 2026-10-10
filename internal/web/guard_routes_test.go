package web

// GUARD FILE — docs/plans/panel-redesign.md § The contract, rule 7. Not edited
// together with internal/web, and never to make an implementation pass.
//
// The route table of panel-redesign.md § Routes as a program: a URL reads like
// the menu, the pre-2.0 paths are gone without redirects, and no template
// points anywhere the router does not answer.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The 2.0 routes behind a session, pattern for pattern as registered.
var routes = struct{ always, inbound, dmarc []string }{
	always: []string{
		"GET /{$}",
		"GET /overview", "GET /overview/fragment",
		"GET /server/health", "POST /server/health/recheck", "POST /server/health/reload",
		"GET /outbound/domains", "POST /outbound/domains",
		"GET /outbound/domains/{id}", "POST /outbound/domains/{id}/dns-recheck",
		"GET /outbound/domains/{id}/settings",
		"POST /outbound/domains/{id}/settings/reports",
		"POST /outbound/domains/{id}/settings/ratelimit",
		"POST /outbound/domains/{id}/settings/ratelimit/recalc",
		"POST /outbound/domains/{id}/settings/export",
		"GET /outbound/domains/{id}/delete", "POST /outbound/domains/{id}/delete",
		"GET /outbound/domains/{id}/applications/new", "POST /outbound/domains/{id}/applications/new",
		"GET /outbound/domains/{id}/applications/{aid}", "POST /outbound/domains/{id}/applications/{aid}",
		"POST /outbound/domains/{id}/applications/{aid}/password",
		"POST /outbound/domains/{id}/applications/{aid}/ratelimit/recalc",
		"POST /outbound/domains/{id}/applications/{aid}/delete",
		"GET /outbound/log", "GET /outbound/log/fragment", "GET /outbound/log/{id}",
		"GET /outbound/queue", "GET /outbound/queue/fragment",
		"GET /server/log", "GET /server/log/fragment",
		"GET /server/backup", "POST /server/backup", "POST /server/backup/import",
		"GET /server/users",
		"GET /server/users/new", "POST /server/users/new",
		"GET /server/users/{uid}", "POST /server/users/{uid}",
		"GET /server/users/{uid}/delete", "POST /server/users/{uid}/delete",
		"GET /server/settings", "POST /server/settings",
		"GET /server/components",
		"GET /account", "POST /account/profile", "POST /account/password",
		"GET /help",
	},
	inbound: []string{
		"GET /inbound/domains", "POST /inbound/domains",
		"GET /inbound/domains/{id}",
		"POST /inbound/domains/{id}/dns-recheck",
		"POST /inbound/domains/{id}/upstream",
		"POST /inbound/domains/{id}/recipients",
		"GET /inbound/domains/{id}/delete", "POST /inbound/domains/{id}/delete",
	},
	dmarc: []string{
		"GET /outbound/dmarc", "GET /outbound/dmarc/domains/{id}", "GET /outbound/dmarc/reports/{id}",
	},
}

// Requests the pre-2.0 panel answered. None of them is answered now — not by a
// page and not by a redirect (§ No compatibility).
var oldRequests = []string{
	"GET /status", "GET /status/fragment", "POST /status/recheck", "POST /reload",
	"GET /domains", "POST /domains", "POST /domains/import",
	"GET /domains/7", "POST /domains/7/dns-recheck", "GET /domains/7/delete", "POST /domains/7/delete",
	"POST /domains/7/applications", "POST /domains/7/ratelimit", "POST /domains/7/ratelimit/recalc",
	"POST /domains/7/dmarc", "POST /domains/7/export",
	"POST /applications/7/mode", "POST /applications/7/authips", "POST /applications/7/password",
	"POST /applications/7/ratelimit", "POST /applications/7/ratelimit/recalc", "POST /applications/7/delete",
	"GET /inbound", "POST /inbound", "GET /inbound/7", "POST /inbound/7/dns-recheck",
	"POST /inbound/7/upstream", "POST /inbound/7/recipients", "GET /inbound/7/delete", "POST /inbound/7/delete",
	"GET /dmarc", "GET /dmarc/reports/7", "GET /dmarc/domains/7",
	"GET /settings", "POST /settings", "POST /account",
	"GET /users", "GET /users/new", "POST /users/new", "GET /users/7", "POST /users/7",
	"GET /users/7/delete", "POST /users/7/delete",
	"GET /backup", "POST /backup",
	"GET /deliveries", "GET /deliveries/rows", "GET /deliveries/7",
	"GET /mail-queue", "GET /mail-queue/body", "GET /system-log", "GET /system-log/body",
}

// routesStillOld reports whether view/legacy_pages.txt lists @routes: the
// pre-2.0 table is in place and stage 1 has not renamed it yet. The entry can
// only be removed (.github/scripts/design-first.sh).
func routesStillOld(t *testing.T) bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("view", "legacy_pages.txt"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "@routes" {
			return true
		}
	}
	return false
}

// answers reports the pattern that serves a request, "" when nothing does.
func answers(mux *http.ServeMux, method, target string) string {
	_, pattern := mux.Handler(httptest.NewRequest(method, target, nil))
	return pattern
}

// concrete turns a pattern into a request for it: {id} becomes a number.
func concrete(pattern string) (method, target string) {
	method, target, _ = strings.Cut(pattern, " ")
	target = strings.ReplaceAll(target, "{$}", "")
	return method, regexp.MustCompile(`\{[a-z]+\}`).ReplaceAllString(target, "7")
}

func TestRoutesFollowNavigation(t *testing.T) {
	if routesStillOld(t) {
		t.Skip("view/legacy_pages.txt still lists @routes: the route rename of stage 1 has not landed")
	}
	on := &Server{cfg: Config{InboundEnabled: true, DMARCEnabled: true}}
	public, authed := on.muxes()

	// Every path of the table is registered with the listed method.
	for _, group := range [][]string{routes.always, routes.inbound, routes.dmarc} {
		for _, want := range group {
			method, target := concrete(want)
			if got := answers(authed, method, target); got != want {
				t.Errorf("%s %s is answered by %q, want the route %q", method, target, got, want)
			}
		}
	}

	// The feature flags still take their routes away entirely.
	_, bare := (&Server{}).muxes()
	for _, group := range [][]string{routes.inbound, routes.dmarc} {
		for _, p := range group {
			method, target := concrete(p)
			if got := answers(bare, method, target); got != "" {
				t.Errorf("%s %s is answered (%q) with its feature switched off", method, target, got)
			}
		}
	}
	for _, p := range routes.always {
		method, target := concrete(p)
		if got := answers(bare, method, target); got != p {
			t.Errorf("%s %s is answered by %q with inbound and DMARC off, want %q", method, target, got, p)
		}
	}

	// The old paths are gone, GET and POST alike.
	for _, req := range oldRequests {
		method, target, _ := strings.Cut(req, " ")
		if got := answers(authed, method, target); got != "" {
			t.Errorf("the pre-2.0 request %s is still answered, by %q — old paths are removed, not redirected", req, got)
		}
		if got := answers(public, method, target); got != "" && got != "/" {
			t.Errorf("the pre-2.0 request %s is answered by the public route %q", req, got)
		}
	}

	// What stays as it was.
	for _, p := range []string{"/healthz", "/license", "/static/panel.css", "/setup/token", "/login", "/logout"} {
		if got := answers(public, http.MethodGet, p); got == "" || got == "/" {
			t.Errorf("GET %s is no longer a public route", p)
		}
	}

	// No template points at a path the router does not answer: every menu
	// entry, link, form and polled fragment resolves to a registered route.
	// An old path is such a dead end, so this is also what keeps a pre-2.0
	// template from coming back.
	files, err := filepath.Glob(filepath.Join("view", "templates", "*.html"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no templates found: %v", err)
	}
	var (
		link   = regexp.MustCompile(`\b(href|action|hx-get|hx-post)\s*=\s*"([^"]*)"`)
		action = regexp.MustCompile(`(?s)\{\{.*?\}\}`)
		menu   = 0
	)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(f)
		for _, m := range link.FindAllStringSubmatch(action.ReplaceAllString(string(b), "7"), -1) {
			attr, target := m[1], m[2]
			if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
				continue // another site, a fragment, or a value the handler supplies whole
			}
			target, _, _ = strings.Cut(target, "#")
			target, _, _ = strings.Cut(target, "?")
			if attr == "hx-get" && !strings.HasSuffix(target, "/fragment") {
				t.Errorf("%s polls %s; a polled page answers <page>/fragment", name, target)
			}
			if name == "layout.html" && attr == "href" {
				menu++
			}
			found := false
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				if p := answers(public, method, target); p != "" && p != "/" {
					found = true
				}
				found = found || answers(authed, method, target) != ""
			}
			if !found {
				t.Errorf("%s: %s=%q is not a route of the panel", name, attr, m[2])
			}
		}
	}
	if menu < 10 {
		t.Errorf("layout.html links to %d paths; the menu was not found", menu)
	}
}
