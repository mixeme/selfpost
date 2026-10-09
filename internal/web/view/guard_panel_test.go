package view

// GUARD FILE — docs/plans/panel-redesign.md § The contract, rule 7. Not edited
// together with internal/web, and never to make an implementation pass: a
// failing guard means the implementation is wrong.
//
// These tests are the design contract as a program. They hold everything that
// has left legacy_pages.txt to the accepted mockups under
// docs/assets/panel-redesign/panel/, and they read their rules from that
// directory — the Bulma subset and the width allow-list from check.py, the
// shell's classes from build.py, the stylesheet from theme.css and brand.css,
// the skeletons from outlines.json — so the mockups stay the one source and a
// design change has one route: mockup first.
//
// What the guards expect of the implementation (it may not be renamed away):
//   - pageFiles (view.go) maps every page name to its template files; a
//     redesigned page carries the name of its mockup (out-domains, in-domain…);
//   - templates/components.html holds every component partial, layout.html the
//     shell; a page template calls them and writes no component markup itself;
//   - static/panel.css is theme.css + brand.css, rule for rule;
//   - Engine.Render renders a page with the fixture in fixtures_test.go.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const mockupDir = "../../../docs/assets/panel-redesign/panel"

// Screens that are drawn but not part of 2.0: each belongs to a roadmap item
// with its own plan (panel-redesign.md § Room for roadmap items). They are the
// only keys of outlines.json the finished panel may lack.
var deferredScreens = map[string]string{
	"in-log":                "inbound-antispam-panel, 2.1.0",
	"in-filter-lists":       "inbound-antispam-panel, 2.1.0",
	"in-queue":              "queue as a table / inbound queue, undecided",
	"in-quarantine":         "inbound-quarantine, candidate",
	"in-quarantine-message": "inbound-quarantine, candidate",
	"preflight":             "preflight, candidate",
}

// Pages whose 2.0 content is deliberately not the mockup's (decided
// 2026-10-09: both queues ship the old content inside the new shell until the
// queue is a table). They obey every rule except the outline comparison.
var outlineExempt = map[string]string{
	"out-queue": "old postqueue output in the new shell until queue-as-a-table is decided",
}

// Pages that are redesigned but have no outline: the kit page is the
// vocabulary itself, and the signed-out screens have no shell to outline.
var noOutline = map[string]bool{"components": true, "login": true, "setup": true}

// The classes a page template may write itself: text and table-cell utilities
// and the form grid. Every other sp- class is a component's and is written in
// components.html (or layout.html) only.
var pageUtilities = set("sp-muted", "sp-small", "sp-mono", "sp-nowrap", "sp-clip", "sp-actions", "sp-form", "sp-row", "sp-pair")

// The partials of panel-redesign.md § Three layers. Each is a {{define}} in
// components.html.
var requiredPartials = []string{
	"page_head", "postmark", "box_head", "box_foot", "flash", "health_card", "dns_record", "facts",
	"side_menu", "timeline", "log_pane", "empty_state", "confirm_list", "help_topic",
}

// The typed inputs of § Component kit. Each is declared in components.go with
// a doc comment that says where it is used.
var requiredInputs = []string{"Head", "Box", "Record", "Fact", "SideMenu", "Tag"}

// Vendored files, pinned. A change here is a deliberate upgrade (or, for the
// icon subset, a design change that added an icon) and nothing else.
var vendoredSHA256 = map[string]string{
	"bulma.min.css":      "67fa26df1ca9e95d8f2adc7c04fa1b15fa3d24257470ebc10cc68b9aab914bee",
	"tabler-icons.css":   "956cfe253a5588684af3e970bd98b666e97c7044d6c2eca3a1c34ebb10f4298e",
	"tabler-icons.woff2": "4a0e1ab8991d10497efa72e1c37c307db7b9509e96450198fc421d8d36b78187",
}

// Everything /static may hold. A second framework, icon set or script does not
// get in by being dropped into the directory.
var staticFiles = set(
	"OFL.txt", "favicon.png", "favicon.svg", "htmx.min.js", "panel.js", "panel.css",
	"ibm-plex-sans.woff2", "ibm-plex-mono-400.woff2", "ibm-plex-mono-600.woff2",
	"bulma.min.css", "tabler-icons.css", "tabler-icons.woff2",
	"logo.svg", "wordmark.svg", "perf-edge.svg",
)

// What only the old pages need; gone with the last of them.
var legacyStaticFiles = set("legacy.css", "logo-compact.svg")

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, i := range items {
		m[i] = true
	}
	return m
}

func sorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ratchet is legacy_pages.txt: what is still exempt.
type ratchet struct {
	pages  map[string]bool
	kit    bool // the kit is not complete
	routes bool // the old route table is still in place
}

func readRatchet(t *testing.T) ratchet {
	t.Helper()
	b, err := os.ReadFile("legacy_pages.txt")
	if err != nil {
		if os.IsNotExist(err) {
			return ratchet{pages: map[string]bool{}}
		}
		t.Fatalf("read legacy_pages.txt: %v", err)
	}
	r := ratchet{pages: map[string]bool{}}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case line == "@kit":
			r.kit = true
		case line == "@routes":
			r.routes = true
		case strings.HasPrefix(line, "@"):
			t.Fatalf("legacy_pages.txt: unknown entry %q", line)
		default:
			r.pages[line] = true
		}
	}
	return r
}

// skipUntilKit keeps the kit's own tests quiet while stage 0 is under way. The
// entry can only be removed, so the quiet ends for good with the kit.
func skipUntilKit(t *testing.T) ratchet {
	t.Helper()
	r := readRatchet(t)
	if r.kit {
		t.Skip("legacy_pages.txt still lists @kit: the component kit is not complete, so its contract is not enforced yet")
	}
	return r
}

func mockup(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(mockupDir, name))
	if err != nil {
		t.Fatalf("the guard tests read the accepted mockups and cannot run without them: %v", err)
	}
	return string(b)
}

func embedded(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(assetsFS, name)
	if err != nil {
		t.Fatalf("%s is not embedded: %v", name, err)
	}
	return string(b)
}

func quoted(src string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(src, -1) {
		out = append(out, m[1])
	}
	return out
}

// contract is the vocabulary check.py and build.py define.
type contract struct {
	bulma      map[string]bool // the Bulma subset a screen may use
	shellBulma map[string]bool // Bulma classes of the shell (navbar…), layout.html only
	shell      map[string]bool // sp- classes of the shell, layout.html only
	signedOut  map[string]bool // sp- classes of the signed-out screens
	widthOK    []string
}

func readContract(t *testing.T) contract {
	t.Helper()
	py := mockup(t, "check.py")
	grab := func(re string) string {
		m := regexp.MustCompile(re).FindStringSubmatch(py)
		if m == nil {
			t.Fatalf("check.py no longer matches %s — the guard reads its lists from there", re)
		}
		return m[1]
	}
	c := contract{
		bulma:      set(strings.Fields(grab(`(?s)BULMA = set\("""(.*?)"""`))...),
		shell:      set(quoted(grab(`SHELL = \{([^}]*)\}`))...),
		signedOut:  set(quoted(grab(`SIGNED_OUT = \{([^}]*)\}`))...),
		widthOK:    quoted(grab(`(?s)WIDTH_OK = \((.*?)\)\n`)),
		shellBulma: map[string]bool{},
	}
	for _, cls := range staticClasses(mockup(t, "build.py")) {
		if !strings.HasPrefix(cls, "sp-") && !strings.HasPrefix(cls, "ti") && !strings.ContainsAny(cls, "{}%") {
			c.shellBulma[cls] = true
		}
	}
	if len(c.bulma) < 40 || len(c.shell) < 4 || len(c.signedOut) < 3 || len(c.widthOK) < 5 || !c.shellBulma["navbar"] {
		t.Fatalf("check.py / build.py were read but the lists look wrong: %d Bulma, %d shell, %d signed-out, %d widths, %d shell Bulma",
			len(c.bulma), len(c.shell), len(c.signedOut), len(c.widthOK), len(c.shellBulma))
	}
	return c
}

var spClass = regexp.MustCompile(`\.(sp-[a-z0-9-]+)`)

func definedSP(css string) map[string]bool {
	out := map[string]bool{}
	for _, m := range spClass.FindAllStringSubmatch(cssComment.ReplaceAllString(css, ""), -1) {
		out[m[1]] = true
	}
	return out
}

func definedIcons(t *testing.T) map[string]bool {
	out := map[string]bool{"ti": true}
	for _, m := range regexp.MustCompile(`\.(ti-[a-z0-9-]+):`).FindAllStringSubmatch(embedded(t, "static/tabler-icons.css"), -1) {
		out[m[1]] = true
	}
	return out
}

func newGuardEngine(t *testing.T) *Engine {
	t.Helper()
	engine, err := New("9.9.9-test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	engine.SetInboundEnabled(true)
	engine.SetDMARCEnabled(true)
	return engine
}

// renderFixture renders a redesigned page the way a handler would.
func renderFixture(t *testing.T, engine *Engine, page string) *gnode {
	t.Helper()
	fixture, ok := pageFixtures[page]
	if !ok {
		t.Fatalf("page %q has left legacy_pages.txt but has no fixture in fixtures_test.go, so nothing can check it", page)
	}
	rec := httptest.NewRecorder()
	engine.Render(rec, http.StatusOK, page, fixture())
	if rec.Code != http.StatusOK {
		t.Fatalf("page %q does not render with its fixture: status %d, %s", page, rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	return parseHTML(rec.Body.String())
}

// redesigned lists the engine's pages that are held to the contract.
func redesigned(engine *Engine, r ratchet) []string {
	var out []string
	for name := range engine.Pages() {
		if !r.pages[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// pageTemplates lists the template files of the redesigned pages — everything
// but the kit's own two files.
func pageTemplates(engine *Engine, r ratchet) []string {
	seen := map[string]bool{}
	for _, name := range redesigned(engine, r) {
		for _, f := range pageFiles[name] {
			if base := path.Base(f); base != "components.html" && base != "layout.html" {
				seen[f] = true
			}
		}
	}
	return sorted(seen)
}

// The ratchet and the engine must describe the same panel: a name that is not
// a page exempts nothing and only hides a typo, and a page that is off the
// list must be a screen that was actually drawn.
func TestLegacyRatchetMatchesTheEngine(t *testing.T) {
	r := readRatchet(t)
	engine := newGuardEngine(t)
	var golden map[string]any
	if err := json.Unmarshal([]byte(mockup(t, "outlines.json")), &golden); err != nil {
		t.Fatalf("outlines.json: %v", err)
	}
	for name := range r.pages {
		if engine.Page(name) == nil {
			t.Errorf("legacy_pages.txt lists %q, which is not a page of the engine — delete the line", name)
		}
	}
	for _, name := range redesigned(engine, r) {
		if _, drawn := golden[name]; !drawn && !noOutline[name] {
			t.Errorf("page %q is not on legacy_pages.txt and is not a drawn screen: a redesigned page carries the name of its mockup", name)
		}
		if _, deferred := deferredScreens[name]; deferred {
			t.Errorf("page %q belongs to a roadmap item (%s) and is not built with the redesign", name, deferredScreens[name])
		}
	}
	if len(r.pages) == 0 && !r.kit {
		for name := range golden {
			if _, deferred := deferredScreens[name]; !deferred && engine.Page(name) == nil {
				t.Errorf("screen %q is drawn and in scope, but the engine has no such page", name)
			}
		}
	}
	for _, name := range redesigned(engine, r) {
		if r.kit && name != "components" {
			t.Errorf("page %q has left legacy_pages.txt while @kit is still listed: no page is restyled before the kit is accepted", name)
		}
	}
}

// The Go port of outline() must read the mockups exactly as check.py does, or
// the comparison below would be between two different measures.
func TestOutlinePortMatchesCheckPy(t *testing.T) {
	var golden map[string]any
	if err := json.Unmarshal([]byte(mockup(t, "outlines.json")), &golden); err != nil {
		t.Fatalf("outlines.json: %v", err)
	}
	if len(golden) < 30 {
		t.Fatalf("outlines.json has %d screens; it should hold every drawn screen", len(golden))
	}
	for name, want := range golden {
		root := parseHTML(mockup(t, name+".html"))
		pages := root.find("sp-page")
		if len(pages) != 1 {
			t.Errorf("%s.html: %d sp-page containers", name, len(pages))
			continue
		}
		if got := outlineJSON(outline(pages[0])); got != outlineJSON(want) {
			t.Errorf("%s: the Go outline differs from outlines.json\n got: %s\nwant: %s", name, got, outlineJSON(want))
		}
	}
}

// A different design cannot pass as this one: rendered with its fixture, a
// page has the component skeleton recorded for its mockup.
func TestPanelOutlinesMatchMockups(t *testing.T) {
	r := skipUntilKit(t)
	engine := newGuardEngine(t)
	var golden map[string]any
	if err := json.Unmarshal([]byte(mockup(t, "outlines.json")), &golden); err != nil {
		t.Fatalf("outlines.json: %v", err)
	}
	for _, name := range redesigned(engine, r) {
		if noOutline[name] || outlineExempt[name] != "" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			want, ok := golden[name]
			if !ok {
				t.Fatalf("no outline is recorded for %q", name)
			}
			pages := renderFixture(t, engine, name).find("sp-page")
			if len(pages) != 1 {
				t.Fatalf("the rendered page has %d sp-page containers; the layout provides exactly one", len(pages))
			}
			if got := outlineJSON(outline(pages[0])); got != outlineJSON(want) {
				t.Errorf("the page's component skeleton is not its mockup's\n got: %s\nwant: %s", got, outlineJSON(want))
			}
		})
	}
}

// The structure rules of check.py, on the rendered page.
func TestPanelPageStructure(t *testing.T) {
	r := skipUntilKit(t)
	engine := newGuardEngine(t)
	for _, name := range redesigned(engine, r) {
		t.Run(name, func(t *testing.T) {
			root := renderFixture(t, engine, name)
			if n := len(root.byTag("h1")); n != 1 {
				t.Errorf("%d <h1>; a page has exactly one", n)
			}
			for _, tbl := range root.byTag("table") {
				if !tbl.parent.has("table-container") {
					t.Error("a <table> must sit in a table-container so a wide one can scroll")
				}
			}
			for _, box := range root.find("box") {
				if len(box.kids) == 0 || !box.kids[0].has("sp-box-head") {
					t.Errorf("a box must start with box_head (box: %q)", textOf(box))
				}
			}
			pages := root.find("sp-page")
			if name == "login" || name == "setup" {
				if len(pages) != 0 || len(root.find("sp-signin")) != 1 || len(root.find("navbar")) != 0 {
					t.Error("a signed-out page is the split sp-signin screen, without the navbar and the page container")
				}
				return
			}
			if len(pages) != 1 || len(root.find("navbar")) != 1 || len(root.find("sp-perf")) != 1 || len(root.find("sp-foot")) != 1 {
				t.Fatal("a signed-in page is rendered by the shell: one navbar, one perforated edge, one sp-page container, one footer")
			}
			var top []*gnode
			for _, k := range pages[0].kids {
				if !k.has("sp-foot") {
					top = append(top, k)
				}
			}
			if len(top) == 0 || !top[0].has("sp-head") {
				t.Fatal("the page must open with page_head")
			}
			marks := root.find("sp-postmark")
			if name != "components" {
				if len(marks) > 1 {
					t.Errorf("%d postmarks; at most one", len(marks))
				}
				for _, m := range marks {
					if !m.parent.has("sp-head") {
						t.Error("the postmark belongs inside page_head")
					}
				}
			}
			for _, k := range top[1:] {
				// A flash is conditional: allowed at the top level, never part of an outline.
				if !(k.has("box") || k.has("columns") || k.has("notification") || (k.tag == "form" && k.has("sp-form"))) {
					t.Errorf("top-level <%s class=%q> — a page is boxes, columns or one form", k.tag, k.attrs["class"])
				}
			}
		})
	}
}

// panel.css is the mockups' stylesheet and nothing else: brand.css followed by
// theme.css, rule for rule. A rule the mockups do not have is a design change,
// and a design change is made in the mockup first.
func TestPanelCSSIsTheMockupStylesheet(t *testing.T) {
	skipUntilKit(t)
	got := parseCSS(embedded(t, "static/panel.css"))
	want := append(parseCSS(mockup(t, "../shared/brand.css")), parseCSS(mockup(t, "theme.css"))...)
	for i := 0; i < len(got) || i < len(want); i++ {
		switch {
		case i >= len(got):
			t.Fatalf("panel.css ends after %d rules; the mockup stylesheet goes on with\n  %s", len(got), want[i])
		case i >= len(want):
			t.Fatalf("panel.css has a rule the mockups do not have:\n  %s", got[i])
		case got[i] != want[i]:
			t.Fatalf("panel.css differs from brand.css + theme.css at rule %d\n got: %s\nwant: %s", i+1, got[i], want[i])
		}
	}
	for _, m := range cssURL.FindAllStringSubmatch(embedded(t, "static/panel.css"), -1) {
		if strings.Contains(m[1], "/") || strings.Contains(m[1], ":") {
			t.Errorf("panel.css loads %q; assets sit beside it in /static and are named without a path", m[1])
		} else if _, err := fs.Stat(assetsFS, "static/"+m[1]); err != nil {
			t.Errorf("panel.css loads %q, which is not embedded", m[1])
		}
	}
}

// The CSS rules of check.py: no !important, no id or page selector, no size
// under the type floor, no width outside WIDTH_OK.
func TestPanelCSSContract(t *testing.T) {
	skipUntilKit(t)
	c := readContract(t)
	var (
		idSel    = regexp.MustCompile(`(^|[\s,>+~])#[a-zA-Z]`)
		pageSel  = regexp.MustCompile(`\b(html|body|main)[.\[#]|\[data-page`)
		fontSize = regexp.MustCompile(`font-size\s*:\s*([\d.]+)(rem|em|px)`)
		noShrink = regexp.MustCompile(`min-width\s*:\s*0\s*(;|$)`)
		width    = regexp.MustCompile(`(^|[^-\w])(max-width|min-width|width)\s*:`)
		floor    = map[string]float64{"rem": 0.8125, "em": 0.85, "px": 13}
	)
	for _, rule := range parseCSS(embedded(t, "static/panel.css")) {
		sel, body := rule.selector, rule.body
		if strings.Contains(body, "!important") {
			t.Errorf("`%s`: !important", sel)
		}
		if idSel.MatchString(sel) || pageSel.MatchString(sel) {
			t.Errorf("`%s`: an id or page selector — style components, not one element on one page", sel)
		}
		// Icons (.ti) and the postmark imprint are not running text.
		for _, m := range fontSize.FindAllStringSubmatch(body, -1) {
			v, _ := strconv.ParseFloat(m[1], 64)
			if v < floor[m[2]] && !strings.Contains(sel, ".sp-postmark") && !strings.Contains(sel, ".ti") {
				t.Errorf("`%s`: font-size %s%s is below the 13px floor — use --sp-fs-xs or --sp-fs-s", sel, m[1], m[2])
			}
		}
		// `min-width: 0` sizes nothing: it only lets a flex or grid child shrink.
		if width.MatchString(noShrink.ReplaceAllString(body, "")) {
			ok := false
			for _, w := range c.widthOK {
				ok = ok || strings.Contains(sel, w)
			}
			if !ok {
				t.Errorf("`%s` sets a width; widths come from the page layout (WIDTH_OK in check.py)", sel)
			}
		}
	}
}

// One vocabulary: the Bulma subset, the sp- components of panel.css, the
// vendored icons — in the template sources and in what they render.
func TestPanelClassVocabulary(t *testing.T) {
	r := skipUntilKit(t)
	c := readContract(t)
	engine := newGuardEngine(t)
	defined := definedSP(embedded(t, "static/panel.css"))
	icons := definedIcons(t)

	check := func(t *testing.T, where string, classes []string, inShell, inKit bool) {
		for _, cls := range classes {
			switch {
			case strings.HasPrefix(cls, "sp-"):
				switch {
				case !defined[cls]:
					t.Errorf("%s: `%s` is not defined in panel.css", where, cls)
				case c.shell[cls] && !inShell:
					t.Errorf("%s: `%s` belongs to the shell (layout.html)", where, cls)
				case c.signedOut[cls] && !inShell && !inKit:
					t.Errorf("%s: `%s` belongs to the signed-out shell", where, cls)
				}
			case cls == "ti" || strings.HasPrefix(cls, "ti-"):
				if !icons[cls] {
					t.Errorf("%s: icon `%s` is not in the vendored subset (tabler-icons.css) — it would render as nothing", where, cls)
				}
			case c.bulma[cls], inShell && c.shellBulma[cls]:
			default:
				t.Errorf("%s: class `%s` is neither in the Bulma subset nor an sp- component", where, cls)
			}
		}
	}
	smallOnlyOnProgress := func(t *testing.T, where string, root *gnode) {
		root.walk(func(n *gnode) {
			if n.has("is-small") && !n.has("progress") {
				t.Errorf("%s: `is-small` on <%s> — controls keep the normal size; only a progress bar is small", where, n.tag)
			}
		})
	}

	used := map[string]bool{}
	for _, f := range []string{"templates/components.html", "templates/layout.html"} {
		classes := staticClasses(embedded(t, f))
		check(t, f, classes, f == "templates/layout.html", true)
		for _, cls := range classes {
			used[cls] = true
		}
	}
	for _, f := range pageTemplates(engine, r) {
		src := embedded(t, f)
		check(t, f, staticClasses(src), false, false)
		smallOnlyOnProgress(t, f, parseHTML(templateAction.ReplaceAllString(src, "")))
	}

	kit := renderFixture(t, engine, "components")
	shown := map[string]bool{}
	kit.walk(func(n *gnode) {
		for _, cls := range n.classes() {
			shown[cls], used[cls] = true, true
		}
	})
	for _, name := range redesigned(engine, r) {
		root := renderFixture(t, engine, name)
		var all []string
		root.walk(func(n *gnode) { all = append(all, n.classes()...) })
		check(t, "rendered "+name, all, true, true)
		smallOnlyOnProgress(t, "rendered "+name, root)
		for _, cls := range all {
			used[cls] = true
			if strings.HasPrefix(cls, "sp-") && !shown[cls] && !c.shell[cls] && !c.signedOut[cls] {
				t.Errorf("page %s uses `%s`, which the components page does not show — document it there or stop using it", name, cls)
			}
		}
	}
	for cls := range defined {
		if !used[cls] {
			t.Errorf("panel.css defines `%s` but no template uses it", cls)
		}
	}
	for _, cls := range sorted(definedSP(mockup(t, "theme.css"))) {
		if !defined[cls] {
			t.Errorf("the mockups define `%s`; panel.css does not", cls)
		}
	}
}

// Component markup exists in one place. A page calls the partial and passes
// it a struct; it does not rebuild the component, and it does not assemble
// the partial's input on the spot.
func TestTemplatesUseComponents(t *testing.T) {
	r := skipUntilKit(t)
	engine := newGuardEngine(t)

	components := embedded(t, "templates/components.html")
	for _, name := range requiredPartials {
		if !regexp.MustCompile(`\{\{-?\s*define\s+"` + name + `"`).MatchString(components) {
			t.Errorf("components.html does not define the partial %q", name)
		}
	}
	funcs := templateFuncs()
	for _, name := range []string{"status_tag", "wbr_at"} {
		if _, ok := funcs[name]; !ok {
			t.Errorf("the template function %q is missing", name)
		}
	}
	if _, ok := funcs["copy_field"]; !ok && !strings.Contains(components, `define "copy_field"`) {
		t.Error("copy_field is neither a template function nor a partial")
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "components.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("components.go holds the typed input of every partial: %v", err)
	}
	documented := map[string]bool{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts := spec.(*ast.TypeSpec)
			doc := ts.Doc
			if doc == nil && len(gen.Specs) == 1 {
				doc = gen.Doc
			}
			documented[ts.Name.Name] = doc != nil && strings.TrimSpace(doc.Text()) != ""
		}
	}
	for _, name := range requiredInputs {
		if doc, ok := documented[name]; !ok {
			t.Errorf("components.go does not declare the partial input %s", name)
		} else if !doc {
			t.Errorf("components.go: %s has no doc comment saying where it is used", name)
		}
	}

	defined := definedSP(embedded(t, "static/panel.css"))
	dict := regexp.MustCompile(`\{\{[^}]*\bdict\b`)
	for _, f := range pageTemplates(engine, r) {
		src := embedded(t, f)
		for _, cls := range staticClasses(src) {
			if strings.HasPrefix(cls, "sp-") && defined[cls] && !pageUtilities[cls] {
				t.Errorf("%s writes `%s` itself; that markup belongs to a partial of components.html — call it", f, cls)
			}
		}
		if dict.MatchString(src) {
			t.Errorf("%s builds a partial's input with dict; a page passes the struct from components.go", f)
		}
	}
}

// A redesigned page leaves nothing of the old panel on screen: it does not
// load the old stylesheet and does not render through the old layout.
func TestRedesignedPagesLoadOnlyTheKit(t *testing.T) {
	r := skipUntilKit(t)
	engine := newGuardEngine(t)
	for _, name := range redesigned(engine, r) {
		root := renderFixture(t, engine, name)
		var sheets []string
		for _, l := range root.byTag("link") {
			if l.attrs["rel"] == "stylesheet" {
				sheets = append(sheets, l.attrs["href"])
			}
		}
		want := []string{"/static/bulma.min.css", "/static/tabler-icons.css", "/static/panel.css"}
		if strings.Join(sheets, " ") != strings.Join(want, " ") {
			t.Errorf("page %s loads the stylesheets %v, want exactly %v in that order", name, sheets, want)
		}
		for _, s := range root.byTag("script") {
			if src := s.attrs["src"]; src != "/static/htmx.min.js" && src != "/static/panel.js" {
				t.Errorf("page %s loads the script %q", name, src)
			}
		}
		for _, img := range root.byTag("img") {
			if !strings.HasPrefix(img.attrs["src"], "/static/") {
				t.Errorf("page %s shows an image from %q", name, img.attrs["src"])
			}
		}
	}
}

func TestVendoredAssetsArePinned(t *testing.T) {
	for name, want := range vendoredSHA256 {
		b, err := fs.ReadFile(assetsFS, "static/"+name)
		if err != nil {
			t.Errorf("%s is not vendored: %v", name, err)
			continue
		}
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s has SHA-256 %s, pinned %s — a vendored file is replaced by a deliberate upgrade, never edited", name, got, want)
		}
	}
}

func TestStaticHoldsOnlyTheKnownAssets(t *testing.T) {
	r := readRatchet(t)
	entries, err := fs.ReadDir(assetsFS, "static")
	if err != nil {
		t.Fatalf("read static: %v", err)
	}
	for _, e := range entries {
		switch name := e.Name(); {
		case e.IsDir():
			t.Errorf("static/%s is a directory; assets are flat files", name)
		case staticFiles[name]:
		case legacyStaticFiles[name] && len(r.pages) > 0:
		case legacyStaticFiles[name]:
			t.Errorf("static/%s served the old pages only; the last of them is gone, delete it", name)
		default:
			t.Errorf("static/%s is not an asset the panel is allowed to ship: no second framework, icon set or script", name)
		}
	}
}
