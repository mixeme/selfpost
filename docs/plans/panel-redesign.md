# Plan: panel-redesign

**Status:** candidate  
**Date:** 2026-09-08  
**Version:** `1.x` MINOR once implementation is agreed; this file is givens, not a
build plan.

---

## What this file is

Source data for a visual redesign of the control panel. Mockups have not been
drawn yet. Implementation in `internal/web` waits on those mockups and on
roadmap status **agreed**.

HTML click-throughs in [docs/assets/panel-ui/](../assets/panel-ui/index.html)
are **not** the brief. They were an earlier layout experiment; new screens are
drawn from this file's vocabulary, not copied from that folder.

## Goal

Stop hand-tuning every card on every page to hide meaningless empty space.
Operators should get a panel whose width rules are the same on Status, a
delete-confirm, and a six-column log: a form stays at a reading measure, ops
content uses the remaining column, and a card fills its cell.

This is a visual and CSS-grammar change. Routes, POST behaviour, and htmx
fragments stay as they are unless a later mockup proves a control must move.

## Pain (why)

[panel.css](../../internal/web/view/static/panel.css) already juggles several
widths (`64rem` column, `48rem` measure, `main.wide`, `.card.narrow`,
`page-login`) and two names for one pairing (`.split` everywhere, `.pair` on
DMARC). Each new screen picks a width by eye. On a wide monitor that leaves
voids; stretching a single input to the window is unreadable. The fix is a
layout grammar, not another button library.

## Delivery constraints (fixed unless product says otherwise)

The panel is not an SPA. Changing any of these needs an explicit decision, not
a mockup flourish:

| Constraint | Where |
|---|---|
| Go `html/template`, server-rendered pages | [internal/web/view/](../../internal/web/view/) |
| Vendored htmx 2.x for polling fragments | [layout.html](../../internal/web/view/templates/layout.html), [development.md](../development.md) § Vendored front-end |
| Assets embedded (`//go:embed`), no CDN at runtime | [view.go](../../internal/web/view/view.go) |
| CSP `default-src 'self'` — no inline script or `style=""` | [security.go](../../internal/web/security.go), `TestNoTemplateUsesInlineScriptOrStyle` |
| State changes are native HTML POST | templates under `internal/web/view/templates/` |
| AGPL-3.0; new front-end must be MIT-or-better and listed in NOTICE | [NOTICE](../../NOTICE), [development.md](../development.md) |

JavaScript is not a product requirement. [security.md](../security.md) records
progressive enhancement so **authorization does not depend on JS** (origin
check, session, RBAC). `data-confirm` is deliberately not a security boundary.
The redesign may use JS for chrome (copy, `<dialog>`). It must not make a
script the only way to submit a form.

Brand carriers today: stamp, brick, IBM Plex — [product.md](../product.md) and
[docs/assets/selfpost-proof.html](../assets/selfpost-proof.html). New mockups
may restyle; they must not silently drop the mark or invent a second product
look.

## Screens in scope (information architecture)

Same pages as the live panel. Visibility flags (inbound, DMARC, global vs
domain-admin) stay as in [web.go](../../internal/web/web.go).

| Kind | Pages |
|---|---|
| Signed-out | login, setup |
| Global ops | Status (plus polled fragment), mail queue, system log, backup, users |
| Shared | domains, domain detail, deliveries, delivery, help, settings |
| Optional | inbound (+ domain, delete), DMARC hub / domain / report |
| Confirms / forms | domain delete, inbound delete, user create/edit/delete |

## Layout layer (decided)

**Hybrid.** Page-level regions are ours. In-page composition is vendored Web
Awesome **layout CSS only** (MIT Core), not the component library and not the
full `utilities.css` barrel.

Every Layout ([every-layout.dev](https://every-layout.dev/)) is the *school*
(Stack, Cluster, Sidebar, Grid). It is a paid book; redistribution is
forbidden. Do not vendor it, do not buy it for the image, do not put their
custom elements in the tree. Web Awesome `layout.css` is the MIT packaging of
those recipes (`wa-stack` ≈ Stack, `wa-cluster` ≈ Cluster, `wa-flank` ≈
Sidebar, `wa-grid` ≈ Grid/Switcher).

### Regions (our CSS)

Shell: navigation column plus content that takes the rest (today's `.shell`,
without a centred `64rem` well that leaves side voids).

Inside the content column, **two modes only**. A page must not invent a third
width:

- **`measure`** — one readable form (login, setup, delete confirm, user form).
  Cap around `42rem`, **left-aligned**. Fields fill the measure, not the
  window. Empty space to the right of a form on a wide monitor is acceptable.
- **`fill`** — ops: Status, tables, logs, queue, DNS, paired cards. One column
  cap for all fill pages (pick a single value in mockups, e.g. ~`90rem`; do
  not keep both `48rem` and `64rem`). A card fills its grid cell.

There is no `main.wide` or `.card.narrow` as a width exception. A narrow card
lives in `measure`. A wide table lives in `fill`.

### In-page (vendored WA CSS)

Embed under `/static`, same as htmx — never a runtime CDN.

| Take | Do not take |
|---|---|
| `utilities/layout.css` | barrel `utilities.css` |
| `utilities/gap.css` | `themes/default.css`, `native.css` |
| `align-items.css`, `justify-content.css`, `flex-wrap.css` if a mockup needs them | variants, prose, FOUCE, JS, icons, Pro |
| A short `:root` stub for `--wa-space-*` only | their `layers.css` (it styles `wa-page`) |

Declare `@layer wa-utilities` locally. One spacing scale for the whole layer
(map to today's `1.2rem` gaps if that looks right; do not keep a second scale).

`.card` with slots (head / body / actions) stays ours so a `wa-grid` cell can
stretch. Do not use `wa-card`.

### Class vocabulary

**Paired cards are `wa-grid`, not `wa-split`.** In Web Awesome, `wa-split` is
space-between (title ‖ action). `wa-grid` is `auto-fit` + `--min-column-size`,
which is today's `.split { minmax(22rem, 1fr) }`. Freeze `--min-column-size`
once (same order as `22rem`); do not retune it per page.

| Class | Use |
|---|---|
| `measure` / `fill` | page region only, never inside a card |
| `wa-stack` | form or card body; children stretch on the block axis |
| `wa-cluster` | button rows, tags, Copy beside a value |
| `wa-grid` | two or more equal cards or field columns |
| `wa-split` | card header: title ‖ help/action; label ‖ value |
| `wa-flank` | rare: fixed + fluid (icon beside a field) |
| `wa-gap-*` | change a gap; not a substitute for a region |

`wa-frame` is for media boxes; the panel has none — omit from mockups.

One child in `wa-grid` occupies the row (`auto-fit`, not `auto-fill`): a
domain-admin Settings card must not leave an empty second column.

**Forbidden in mockups and later in templates:** a one-off `max-width` “for
this page”; a second name for the same pairing (today `.pair` vs `.split`);
`margin-inline: auto` on a card inside a grid (that already collapsed `.split`
tracks).

**Ready when:** Status (`fill` + `wa-grid` pairs) and login (`measure` +
`wa-stack`) can be drawn with no extra width rule. If a third screen wants a
new `max-width`, the vocabulary is unfinished.

## Components (decided)

**No JavaScript component library.** Pico, Bootstrap JS, DaisyUI, FAST,
Shoelace, and Web Awesome custom elements (`wa-button`, `wa-dialog`,
`wa-drawer`, `wa-card`) are out. They do not fix empty cards; they add Lit or
another runtime, Shadow DOM, CSP risk, and friction with htmx swaps.

What the live panel actually uses — and what mockups should assume:

| Need | Source |
|---|---|
| Button, input, select, textarea, table | native HTML, our CSS (brick / Plex) |
| Card | our `.card` + slots |
| Flash, status tint | our CSS |
| Help / application panels | CSS disclosure (checkbox today); or `<details>` / Popover if a mockup prefers |
| Destructive confirm | `data-confirm` / `window.confirm`, plus existing confirm pages for high blast radius |
| Overlay, if a mockup truly needs one | HTML [`<dialog>`](https://developer.mozilla.org/docs/Web/HTML/Element/dialog), not `wa-dialog` |
| Copy-to-clipboard | existing `panel.js` |

There is no combobox, date picker, tab set, tree, or chart in the panel. If a
future mockup invents a widget native HTML cannot express, choose a library
then, for that widget only.

## Out of scope for this item

- New panel features, routes, or copy (those are other roadmap rows).
- Weakening CSP to accommodate a widget library.
- Web Awesome Pro, Font Awesome Kit, a JS bundler for Lit.
- Replacing htmx with a front-end framework.
- Implementing `docs/assets/panel-ui/` as-is.

## Next (not this file's job)

1. Draw new mockups using only the vocabulary above.
2. After mockups are accepted and this item is **agreed**: vendor the WA CSS
   files, NOTICE / development.md rows, then restyle templates without changing
   behaviour.

## Implementation checklist

None until mockups exist and the roadmap status is **agreed**. Do not start
vendoring or rewriting `internal/web` from this file alone.
