"""Design-contract lint for the panel mockups, and the source of outlines.json.

The contract (docs/plans/panel-redesign.md) is only worth something if a
machine can say "no". This script is that machine for the mockups; the same
rules are ported to a Go test over internal/web templates when the redesign is
implemented, and that test compares rendered pages with outlines.json.

  python docs/assets/panel-redesign/panel/check.py            # lint
  python docs/assets/panel-redesign/panel/check.py --write    # lint + rewrite outlines.json

Exit code 1 on any violation.
"""
import json
import pathlib
import re
import sys
from html.parser import HTMLParser

HERE = pathlib.Path(__file__).parent
VOID = {"input", "img", "br", "hr", "meta", "link"}

# The subset of Bulma the panel uses. Reaching for a Bulma class outside this
# list is a design decision, not an implementation detail: add it here and to
# the components page in the same change, or do without it.
BULMA = set("""
box button buttons checkbox column columns control field file file-cta file-input file-label
file-name has-addons has-icons-left has-name has-text-danger help icon input label menu menu-label
menu-list notification progress select table table-container tag textarea title
is-3 is-4 is-5 is-6 is-7 is-9 is-active is-danger is-expanded is-fullwidth is-gapless is-half
is-hidden is-hoverable is-left is-light is-primary is-small is-success is-text is-warning
mb-1 mb-2 mb-3 mb-4 ml-2 mt-4 mt-5
""".split())
# Classes the shell (build.py, later layout.html) owns; screens never write them.
SHELL = {"sp-page", "sp-perf", "sp-subnav", "sp-current", "sp-foot"}
# The signed-out screens have no shell, so they carry their own footer.
SIGNED_OUT = {"sp-signin", "sp-signin-brand", "sp-signin-form", "sp-foot"}
# Selectors allowed to set a width. Everything else gets its width from the
# layout it sits in — that is the whole point of the redesign.
WIDTH_OK = (".navbar-brand img", ".sp-head .sp-lead", ".sp-head .sp-grow", ".sp-postmark", ".table td.sp-clip",
            ".sp-h-icon", ".sp-signin", ".sp-timeline li::before", ".sp-perf")


class Node:
    def __init__(self, tag, attrs, parent):
        self.tag, self.attrs, self.parent, self.kids, self.text = tag, dict(attrs), parent, [], ""

    @property
    def classes(self):
        return (self.attrs.get("class") or "").split()

    def has(self, cls):
        return cls in self.classes

    def walk(self):
        yield self
        for k in self.kids:
            yield from k.walk()

    def find(self, cls):
        return [n for n in self.walk() if n.has(cls)]


class Tree(HTMLParser):
    def __init__(self):
        super().__init__()
        self.root = Node("root", [], None)
        self.cur = self.root

    def handle_starttag(self, tag, attrs):
        n = Node(tag, attrs, self.cur)
        self.cur.kids.append(n)
        if tag not in VOID:
            self.cur = n

    def handle_endtag(self, tag):
        n = self.cur
        while n is not self.root and n.tag != tag:
            n = n.parent
        if n is not self.root:
            self.cur = n.parent

    def handle_data(self, data):
        self.cur.text += data


def parse(text):
    t = Tree()
    t.feed(text)
    return t.root


def text_of(node):
    return re.sub(r"\s+", " ", "".join(n.text for n in node.walk())).strip()


def css_rules(css):
    css = re.sub(r"/\*.*?\*/", "", css, flags=re.S)
    css = re.sub(r"@media[^{]*\{", "", css)
    for sel, body in re.findall(r"([^{}]+)\{([^{}]*)\}", css):
        yield sel.strip(), body


def box_kind(box):
    kinds = []
    for k in box.kids:
        if k.has("sp-box-head") or k.has("sp-box-foot"):
            continue
        for cls, name in (("sp-record", "record"), ("sp-health", "health"), ("sp-empty", "empty"),
                          ("sp-log", "log"), ("sp-help-topic", "help"), ("sp-box-body", "body")):
            if k.has(cls):
                kinds.append(name)
                break
        else:
            kinds.append("table" if k.tag == "table" or k.has("table-container") else k.tag)
    out = []
    for k in kinds:
        if not out or out[-1].split("×")[0] != k:
            out.append(k)
        else:
            n = int(out[-1].split("×")[1]) + 1 if "×" in out[-1] else 2
            out[-1] = f"{k}×{n}"
    return "+".join(out)


def outline(node):
    """The component skeleton of a page: what a reviewer sees with the text blurred."""
    out = []
    for k in node.kids:
        if k.has("sp-head"):
            parts = ["head"] + [p for p, c in (("postmark", "sp-postmark"), ("actions", "buttons")) if k.find(c)]
            if not k.find("buttons") and any(n.tag in ("a", "form") and n.parent is k for n in k.walk()):
                parts.append("actions")
            out.append("+".join(parts))
        elif k.has("box"):
            variant = next((v for v in ("sp-danger-zone", "sp-credential") if k.has(v)), "")
            head = k.find("sp-box-head")
            title = text_of(head[0].find("sp-no")[0].parent.kids[1]) if head else "?"
            out.append(f"box{'.' + variant if variant else ''}[{title}]:{box_kind(k)}")
        elif k.has("columns"):
            cols = []
            for c in k.kids:
                size = next((x for x in c.classes if re.fullmatch(r"is-\d+", x)), "auto")
                cols.append({size: outline(c)})
            out.append({"columns": cols})
        elif k.tag == "form" or k.has("menu") or k.tag == "aside":
            out.append({"form": outline(k)} if k.tag == "form" else "side_menu")
        elif k.has("buttons"):
            out.append("submit_row")
    return out


def main():
    errors = []
    css = (HERE / "theme.css").read_text(encoding="utf-8")
    defined = set(re.findall(r"\.(sp-[a-z0-9-]+)", css))

    for sel, body in css_rules(css):
        if "!important" in body:
            errors.append(f"theme.css: !important in `{sel}`")
        if re.search(r"(^|[\s,>+~])#[a-z]", sel):
            errors.append(f"theme.css: id selector `{sel}` — style components, not one element on one page")
        # The type floor: 13px. Icons (.ti) and the postmark imprint are not running text.
        for value, unit in re.findall(r"font-size\s*:\s*([\d.]+)(rem|em|px)", body):
            floor = {"rem": 0.8125, "em": 0.85, "px": 13}[unit]
            if float(value) < floor and ".sp-postmark" not in sel and ".ti" not in sel:
                errors.append(f"theme.css: `{sel}` sets font-size {value}{unit}, below the 13px floor — use --sp-fs-xs or --sp-fs-s")
        # `min-width: 0` sizes nothing: it only lets a flex or grid child shrink so its text can be clipped.
        sized = re.sub(r"min-width\s*:\s*0\s*(;|$)", "", body)
        if re.search(r"(?<![-\w])(max-width|min-width|width)\s*:", sized) and not any(sel.startswith(w) or w in sel for w in WIDTH_OK):
            errors.append(f"theme.css: `{sel}` sets a width; widths come from the page layout (WIDTH_OK)")

    used, documented, outlines = set(), set(), {}
    for src in sorted((HERE / "src").glob("*.html")):
        text = src.read_text(encoding="utf-8")
        name = src.stem
        bare = "shell: bare" in text.split("-->")[0]
        for bad in ("style=", "<style", "<script", " onclick=", " id=\"page-"):
            if bad in text:
                errors.append(f"{name}: `{bad}` is forbidden in a screen")
        root = parse(text)
        for n in root.walk():
            for c in n.classes:
                if c.startswith("sp-"):
                    (documented if name == "components" else used).add(c)
                    if c in SHELL and not (bare and c in SIGNED_OUT):
                        errors.append(f"{name}: `{c}` belongs to the shell")
                    elif c not in defined:
                        errors.append(f"{name}: `{c}` is not defined in theme.css")
                elif c == "ti" or c.startswith("ti-"):
                    continue
                elif c == "is-small" and "progress" not in n.classes:
                    errors.append(f"{name}: `is-small` on <{n.tag}> — controls keep the normal size; only a progress bar is small")
                elif c not in BULMA:
                    errors.append(f"{name}: class `{c}` is neither in the Bulma subset nor an sp- component")
        if bare:
            continue
        top = [k for k in root.kids if k.tag != "root"]
        if not top or not top[0].has("sp-head"):
            errors.append(f"{name}: the page must open with page_head (.sp-head)")
        if len([n for n in root.walk() if n.tag == "h1"]) != 1:
            errors.append(f"{name}: exactly one <h1>")
        marks = root.find("sp-postmark")
        if name != "components" and (len(marks) > 1 or any(not m.parent.has("sp-head") for m in marks)):
            errors.append(f"{name}: at most one postmark, inside page_head")
        for tbl in (n for n in root.walk() if n.tag == "table"):
            if not tbl.parent.has("table-container"):
                errors.append(f"{name}: a <table> must sit in a table-container so a wide one can scroll")
        for box in root.find("box"):
            if not box.kids or not box.kids[0].has("sp-box-head"):
                errors.append(f"{name}: a box must start with box_head")
        for k in top[1:]:
            # A flash is conditional, so it is allowed at the top level but never part of an outline.
            ok = k.has("box") or k.has("columns") or k.has("notification") or (k.tag == "form" and k.has("sp-form"))
            if not ok:
                errors.append(f"{name}: top-level <{k.tag} class=\"{' '.join(k.classes)}\"> — a page is boxes, columns or one form")
        if name != "components":
            outlines[name] = outline(root)

    for c in sorted(used - documented - SIGNED_OUT):
        errors.append(f"components page does not show `{c}` — document it or stop using it")
    shell_used = set(re.findall(r"sp-[a-z0-9-]+", (HERE / "build.py").read_text(encoding="utf-8")))
    for c in sorted(defined - used - documented - shell_used):
        errors.append(f"theme.css defines `{c}` but nothing uses it")

    if "--write" in sys.argv and not errors:
        (HERE / "outlines.json").write_text(json.dumps(outlines, ensure_ascii=False, indent=1) + "\n",
                                            encoding="utf-8", newline="\n")
    golden = HERE / "outlines.json"
    if golden.exists() and "--write" not in sys.argv:
        if json.loads(golden.read_text(encoding="utf-8")) != outlines:
            errors.append("outlines.json is stale — a screen's structure changed; re-run with --write and say so in the commit")

    for e in errors:
        print("FAIL", e)
    print(f"{len(outlines) + 1} screens, {len(used | documented)} sp- classes, {len(errors)} violation(s)")
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
