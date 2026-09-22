"""Wrap every fragment in src/ with the panel shell and write it next to this file.

The mockups are static HTML, but thirty pages share one navbar; keeping that
navbar in one place is the only reason this script exists. Run it from
anywhere:  python docs/assets/panel-redesign/panel/build.py

A fragment starts with one meta comment:
  <!-- title: Outbound domains | section: outbound | page: domains -->
section/page pick the highlighted entries; "shell: bare" skips the navbar
(sign-in, setup).
"""
import pathlib
import re

HERE = pathlib.Path(__file__).parent

NAV = [
    ("overview", "Overview", "overview.html", "ti-activity-heartbeat", []),
    ("outbound", "Outbound", "out-domains.html", "ti-send", [
        ("domains", "Domains", "out-domains.html", "ti-world-upload"),
        ("log", "Log", "out-log.html", "ti-list-details"),
        ("queue", "Queue", "out-queue.html", "ti-stack-2"),
        ("dmarc", "DMARC reports", "dmarc.html", "ti-report-analytics"),
    ]),
    ("inbound", "Inbound", "in-domains.html", "ti-inbox", [
        ("domains", "Domains", "in-domains.html", "ti-world-download"),
        ("log", "Log", "in-log.html", "ti-list-details"),
        ("queue", "Queue", "in-queue.html", "ti-stack-2"),
        ("quarantine", "Quarantine", "in-quarantine.html", "ti-mail-pause"),
        ("filter-lists", "Filter lists", "in-filter-lists.html", "ti-filter"),
    ]),
    ("server", "Server", "health.html", "ti-server-2", [
        ("health", "Health", "health.html", "ti-heartbeat"),
        ("preflight", "Preflight", "preflight.html", "ti-checklist"),
        ("system-log", "System log", "system-log.html", "ti-file-text"),
        ("backup", "Backup", "backup.html", "ti-archive"),
        ("users", "Users", "users.html", "ti-users"),
        ("settings", "Settings", "settings.html", "ti-settings"),
    ]),
]

HEAD = """<!DOCTYPE html>
<html lang="en" data-theme="light">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title} · SelfPost</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/bulma@1.0.4/css/bulma.min.css">
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/@tabler/icons-webfont@3/dist/tabler-icons.min.css">
<link rel="stylesheet" href="../shared/brand.css">
<link rel="stylesheet" href="theme.css">
</head>
"""

FOOT = ('<footer class="sp-foot">SelfPost 1.8.0 · © 2026 Mix · <a href="#">License (AGPL-3.0)</a>'
        ' · <a href="#">Source</a> · No warranty</footer>')


def navbar(section, page):
    out = ['<nav class="navbar is-primary" aria-label="main navigation">', '  <div class="container">',
           '    <div class="navbar-brand"><a class="navbar-item" href="overview.html">'
           '<img src="../../selfpost-wordmark.svg" alt="SelfPost"></a></div>',
           '    <div class="navbar-menu is-active">', '      <div class="navbar-start">']
    for key, label, href, _icon, pages in NAV:
        cur = " sp-current" if key == section else ""
        if not pages:
            out.append(f'        <a class="navbar-item{cur}" href="{href}">{label}</a>')
            continue
        out.append(f'        <div class="navbar-item has-dropdown is-hoverable{cur}">')
        out.append(f'          <a class="navbar-link" href="{href}">{label}</a>')
        out.append('          <div class="navbar-dropdown">')
        for pkey, plabel, phref, picon in pages:
            act = " is-active" if key == section and pkey == page else ""
            out.append(f'            <a class="navbar-item{act}" href="{phref}"><i class="ti {picon}"></i>{plabel}</a>')
        out.append('          </div>')
        out.append('        </div>')
    out += ['      </div>', '      <div class="navbar-end">',
            '        <div class="navbar-item has-dropdown is-hoverable%s">' % (" sp-current" if section == "user" else ""),
            '          <a class="navbar-link" href="account.html"><i class="ti ti-user-circle"></i>admin</a>',
            '          <div class="navbar-dropdown is-right">',
            '            <a class="navbar-item" href="account.html"><i class="ti ti-key"></i>Account</a>',
            '            <a class="navbar-item" href="help.html"><i class="ti ti-help"></i>Help</a>',
            '            <hr class="navbar-divider">',
            '            <a class="navbar-item" href="login.html"><i class="ti ti-logout"></i>Sign out</a>',
            '          </div>', '        </div>', '      </div>', '    </div>', '  </div>', '</nav>',
            '<div class="sp-perf" aria-hidden="true"></div>']
    return "\n".join(out)


def subnav(section, page):
    for key, _label, _href, _icon, pages in NAV:
        if key == section and pages:
            cur = ' aria-current="page"'
            links = "".join(
                f'<a href="{phref}"{cur if pkey == page else ""}><i class="ti {picon}"></i>{plabel}</a>'
                for pkey, plabel, phref, picon in pages)
            return f'<div class="sp-subnav"><div class="container">{links}</div></div>'
    return ""


def build(path):
    text = path.read_text(encoding="utf-8")
    m = re.match(r"\s*<!--(.*?)-->\s*", text, re.S)
    meta = dict(part.split(":", 1) for part in m.group(1).split("|"))
    meta = {k.strip(): v.strip() for k, v in meta.items()}
    body = text[m.end():]
    html = HEAD.format(title=meta["title"])
    if meta.get("shell") == "bare":
        html += f"<body>\n{body}\n</body>\n</html>\n"
    else:
        section, page = meta.get("section", ""), meta.get("page", "")
        html += (f"<body>\n{navbar(section, page)}\n{subnav(section, page)}\n<main>\n"
                 f'  <div class="container sp-page">\n{body}\n    {FOOT}\n  </div>\n</main>\n</body>\n</html>\n')
    (HERE / path.name).write_text(html, encoding="utf-8", newline="\n")


if __name__ == "__main__":
    pages = sorted((HERE / "src").glob("*.html"))
    for p in pages:
        build(p)
    print(f"built {len(pages)} pages")
