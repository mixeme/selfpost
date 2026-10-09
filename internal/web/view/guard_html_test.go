package view

// GUARD FILE — docs/plans/panel-redesign.md § The contract, rule 7. Not edited
// together with internal/web, and never to make an implementation pass.
//
// The HTML reading the guard tests share: a small tolerant parser and the port
// of outline() from docs/assets/panel-redesign/panel/check.py. The port is
// itself tested against the mockups (TestOutlinePortMatchesCheckPy), so the Go
// tests and the Python lint cannot drift apart unnoticed.

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
)

// gnode is one element of a parsed page.
type gnode struct {
	tag    string
	attrs  map[string]string
	parent *gnode
	kids   []*gnode
	text   string // the text directly inside this element
}

func (n *gnode) classes() []string { return strings.Fields(n.attrs["class"]) }

func (n *gnode) has(class string) bool {
	for _, c := range n.classes() {
		if c == class {
			return true
		}
	}
	return false
}

// walk visits n and every element under it, parents first.
func (n *gnode) walk(fn func(*gnode)) {
	fn(n)
	for _, k := range n.kids {
		k.walk(fn)
	}
}

func (n *gnode) find(class string) []*gnode {
	var out []*gnode
	n.walk(func(m *gnode) {
		if m.has(class) {
			out = append(out, m)
		}
	})
	return out
}

func (n *gnode) byTag(tag string) []*gnode {
	var out []*gnode
	n.walk(func(m *gnode) {
		if m.tag == tag {
			out = append(out, m)
		}
	})
	return out
}

// textOf is the element's text with its descendants', whitespace collapsed.
func textOf(n *gnode) string {
	var b strings.Builder
	n.walk(func(m *gnode) { b.WriteString(m.text) })
	return strings.Join(strings.Fields(b.String()), " ")
}

var (
	voidTags = map[string]bool{"input": true, "img": true, "br": true, "hr": true, "meta": true, "link": true, "wbr": true}
	rawTags  = map[string]bool{"script": true, "style": true, "textarea": true, "title": true}
	tagName  = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]*`)
)

// parseHTML builds the element tree of a document or a fragment. Like the
// parser check.py uses, it never fails: an end tag closes the nearest open
// element of that name and is ignored when there is none.
func parseHTML(src string) *gnode {
	root := &gnode{tag: "root", attrs: map[string]string{}}
	cur := root
	for i := 0; i < len(src); {
		lt := strings.IndexByte(src[i:], '<')
		if lt < 0 {
			cur.text += html.UnescapeString(src[i:])
			break
		}
		cur.text += html.UnescapeString(src[i : i+lt])
		i += lt
		rest := src[i:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			end := strings.Index(rest, "-->")
			if end < 0 {
				return root
			}
			i += end + 3
		case strings.HasPrefix(rest, "<!"), strings.HasPrefix(rest, "<?"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				return root
			}
			i += end + 1
		case strings.HasPrefix(rest, "</"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				return root
			}
			name := strings.ToLower(strings.TrimSpace(rest[2:end]))
			for n := cur; n != root; n = n.parent {
				if n.tag == name {
					cur = n.parent
					break
				}
			}
			i += end + 1
		default:
			name := tagName.FindString(rest[1:])
			if name == "" {
				cur.text += "<"
				i++
				continue
			}
			name = strings.ToLower(name)
			attrs, n := parseAttrs(rest[1+len(name):])
			i += 1 + len(name) + n
			node := &gnode{tag: name, attrs: attrs, parent: cur}
			cur.kids = append(cur.kids, node)
			switch {
			case voidTags[name]:
			case rawTags[name]:
				end := strings.Index(strings.ToLower(src[i:]), "</"+name)
				if end < 0 {
					return root
				}
				node.text = html.UnescapeString(src[i : i+end])
				i += end
				if gt := strings.IndexByte(src[i:], '>'); gt >= 0 {
					i += gt + 1
				}
			default:
				cur = node
			}
		}
	}
	return root
}

// parseAttrs reads the attributes of a start tag and reports how many bytes it
// consumed, the closing '>' included.
func parseAttrs(s string) (map[string]string, int) {
	attrs := map[string]string{}
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' || s[i] == '/') {
			i++
		}
		if i >= len(s) {
			break
		}
		if s[i] == '>' {
			return attrs, i + 1
		}
		start := i
		for i < len(s) && !strings.ContainsRune(" \t\r\n=>/", rune(s[i])) {
			i++
		}
		name := strings.ToLower(s[start:i])
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
			i++
		}
		value := ""
		if i < len(s) && s[i] == '=' {
			i++
			for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
				i++
			}
			if i < len(s) && (s[i] == '"' || s[i] == '\'') {
				q := s[i]
				end := strings.IndexByte(s[i+1:], q)
				if end < 0 {
					return attrs, len(s)
				}
				value = s[i+1 : i+1+end]
				i += end + 2
			} else {
				start = i
				for i < len(s) && !strings.ContainsRune(" \t\r\n>", rune(s[i])) {
					i++
				}
				value = s[start:i]
			}
		}
		if _, dup := attrs[name]; !dup && name != "" {
			attrs[name] = html.UnescapeString(value)
		}
	}
	return attrs, i
}

var sizeClass = regexp.MustCompile(`^is-\d+$`)

// boxKind names what a box holds under its head: check.py box_kind().
func boxKind(box *gnode) string {
	var kinds []string
	for _, k := range box.kids {
		if k.has("sp-box-head") || k.has("sp-box-foot") {
			continue
		}
		kind := k.tag
		if k.tag == "table" || k.has("table-container") {
			kind = "table"
		}
		for _, m := range [][2]string{{"sp-record", "record"}, {"sp-health", "health"}, {"sp-empty", "empty"},
			{"sp-log", "log"}, {"sp-help-topic", "help"}, {"sp-box-body", "body"}} {
			if k.has(m[0]) {
				kind = m[1]
				break
			}
		}
		kinds = append(kinds, kind)
	}
	var out []string
	for i := 0; i < len(kinds); {
		j := i
		for j < len(kinds) && kinds[j] == kinds[i] {
			j++
		}
		if j-i == 1 {
			out = append(out, kinds[i])
		} else {
			out = append(out, kinds[i]+"×"+itoa(j-i))
		}
		i = j
	}
	return strings.Join(out, "+")
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// outline is the component skeleton of a page — what a reviewer sees with the
// text blurred: check.py outline(). The result marshals to the JSON shape of
// outlines.json.
func outline(node *gnode) []any {
	out := []any{}
	for _, k := range node.kids {
		switch {
		case k.has("sp-head"):
			parts := []string{"head"}
			if len(k.find("sp-postmark")) > 0 {
				parts = append(parts, "postmark")
			}
			if len(k.find("buttons")) > 0 {
				parts = append(parts, "actions")
			} else {
				for _, c := range k.kids {
					if c.tag == "a" || c.tag == "form" {
						parts = append(parts, "actions")
						break
					}
				}
			}
			out = append(out, strings.Join(parts, "+"))
		case k.has("box"):
			variant := ""
			for _, v := range []string{"sp-danger-zone", "sp-credential"} {
				if k.has(v) {
					variant = "." + v
					break
				}
			}
			title := "?"
			if heads := k.find("sp-box-head"); len(heads) > 0 {
				title = "?!" // a head without the number + title pair is not box_head
				if nos := heads[0].find("sp-no"); len(nos) > 0 && len(nos[0].parent.kids) > 1 {
					title = textOf(nos[0].parent.kids[1])
				}
			}
			out = append(out, "box"+variant+"["+title+"]:"+boxKind(k))
		case k.has("columns"):
			cols := []any{}
			for _, c := range k.kids {
				size := "auto"
				for _, cls := range c.classes() {
					if sizeClass.MatchString(cls) {
						size = cls
						break
					}
				}
				cols = append(cols, map[string]any{size: outline(c)})
			}
			out = append(out, map[string]any{"columns": cols})
		case k.tag == "form":
			out = append(out, map[string]any{"form": outline(k)})
		case k.has("menu") || k.tag == "aside":
			out = append(out, "side_menu")
		case k.has("buttons"):
			out = append(out, "submit_row")
		}
	}
	return out
}

// outlineJSON renders an outline the way two of them are compared and shown.
func outlineJSON(v any) string {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return "unmarshalable outline: " + err.Error()
	}
	return string(b)
}

// classAttr finds every class="…" in template or HTML source.
var classAttr = regexp.MustCompile(`\bclass\s*=\s*"([^"]*)"`)

// templateAction matches one {{…}} of a Go template.
var templateAction = regexp.MustCompile(`(?s)\{\{.*?\}\}`)

// staticClasses lists the class tokens a template writes literally. A token an
// action contributes to is dropped here — the rendered page is checked too, and
// that is where a computed class is seen whole.
func staticClasses(src string) []string {
	const dyn = "\x00"
	var out []string
	for _, m := range classAttr.FindAllStringSubmatch(templateAction.ReplaceAllString(src, dyn), -1) {
		for _, c := range strings.Fields(m[1]) {
			if !strings.Contains(c, dyn) {
				out = append(out, c)
			}
		}
	}
	return out
}

// cssRule is one rule of a stylesheet, normalised for comparison.
type cssRule struct {
	media    string // the enclosing @media prelude, "" at the top level
	selector string
	body     string
}

func (r cssRule) String() string {
	s := r.selector + " { " + r.body + " }"
	if r.media != "" {
		s = r.media + " { " + s + " }"
	}
	return s
}

var (
	cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssURL     = regexp.MustCompile(`url\(\s*["']?([^"')]*)["']?\s*\)`)
	cssSpace   = regexp.MustCompile(`\s+`)
)

// parseCSS splits a stylesheet into its rules, in order. Selectors and
// declarations are whitespace-normalised and a url() keeps only its file name,
// so the same rule reads the same wherever the file it came from lives.
func parseCSS(css string) []cssRule {
	return parseCSSBlock(cssComment.ReplaceAllString(css, ""), "")
}

func parseCSSBlock(css, media string) []cssRule {
	var rules []cssRule
	for {
		open := strings.IndexByte(css, '{')
		if open < 0 {
			return rules
		}
		prelude := cssSpace.ReplaceAllString(strings.TrimSpace(css[:open]), " ")
		depth, end := 1, -1
		for i := open + 1; i < len(css); i++ {
			if css[i] == '{' {
				depth++
			} else if css[i] == '}' {
				depth--
				if depth == 0 {
					end = i
					break
				}
			}
		}
		if end < 0 {
			return rules
		}
		inner := css[open+1 : end]
		css = css[end+1:]
		if strings.HasPrefix(prelude, "@media") || strings.HasPrefix(prelude, "@supports") {
			rules = append(rules, parseCSSBlock(inner, prelude)...)
			continue
		}
		var decls []string
		for _, d := range strings.Split(inner, ";") {
			d = cssSpace.ReplaceAllString(strings.TrimSpace(d), " ")
			if d == "" {
				continue
			}
			if c := strings.IndexByte(d, ':'); c >= 0 {
				d = strings.TrimSpace(d[:c]) + ": " + strings.TrimSpace(d[c+1:])
			}
			d = cssURL.ReplaceAllStringFunc(d, func(u string) string {
				name := cssURL.FindStringSubmatch(u)[1]
				if s := strings.LastIndexByte(name, '/'); s >= 0 {
					name = name[s+1:]
				}
				return "url(" + name + ")"
			})
			decls = append(decls, d)
		}
		var sels []string
		for _, s := range strings.Split(prelude, ",") {
			sels = append(sels, strings.TrimSpace(s))
		}
		rules = append(rules, cssRule{media: media, selector: strings.Join(sels, ", "), body: strings.Join(decls, "; ")})
	}
}
