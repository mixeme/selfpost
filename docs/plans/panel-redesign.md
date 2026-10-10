# Plan: panel-redesign

**Status:** agreed (owner, 2026-10-09)  
**Date:** 2026-09-21 (replaces the 2026-09-08 givens)  
**Version:** `2.0.0` MAJOR — a breaking release by the owner's decision (§ No compatibility).

---

## What this file is

The design contract for the control panel and the plan to implement it. The
design itself is drawn and accepted:

| Artefact | Role |
|---|---|
| [docs/assets/panel-redesign/panel/](../assets/panel-redesign/index.html) | 33 screens — **the** reference for what each page looks like |
| [panel/components.html](../assets/panel-redesign/panel/components.html) | every component in every state — the only parts a page may be built from |
| [panel/outlines.json](../assets/panel-redesign/panel/outlines.json) | the component skeleton of each screen, generated from the mockups |
| [panel/check.py](../assets/panel-redesign/panel/check.py) | the contract as a program; its rules are ported to Go tests below |
| [panel/audit.js](../assets/panel-redesign/panel/audit.js) | what only a browser can measure: text size, contrast, overflow, clipped values — pasted into the console, on the mockups or on the running panel |

When this file and a mockup disagree, the mockup wins and this file is fixed.
When an implementation and a mockup disagree, the implementation is wrong.

The 2026-09-08 givens ("no component library, Web Awesome layout CSS only") are
withdrawn. `docs/assets/panel-ui/` and the stage-1 directions `a/`, `b/`, `c/`
are history, not a brief.

## Goal

A panel an operator opens a few times a year and still finds their way in: the
important things in view, details one or two clicks away, one visual language
on every page, and no page that tunes its own width.

## Delivery constraints (unchanged)

| Constraint | Where |
|---|---|
| Go `html/template`, server-rendered; htmx 2.x for polled fragments | [internal/web/view/](../../internal/web/view/) |
| Assets embedded (`//go:embed`), no CDN at runtime | [view.go](../../internal/web/view/view.go) |
| CSP `default-src 'self'` — no inline script, no `style=""` | [security.go](../../internal/web/security.go) |
| State changes are native HTML POST; no script is the only way to submit | templates |
| New front-end is MIT-or-better and listed in NOTICE | [NOTICE](../../NOTICE) |

Bulma was chosen partly because it needs nothing from this table relaxed: no
JavaScript, no `data:` URIs.

## Information architecture

Navigation is grouped by mail direction. Page titles carry the group
(**Outbound domains**, **Inbound log**); menu entries do not repeat it.

| Group | Pages | Today |
|---|---|---|
| Overview | Overview | `/status` (verdicts only) |
| Outbound | Domains · Log · Queue · DMARC reports | `/domains`, `/deliveries`, `/mail-queue`, `/dmarc` |
| Inbound | Domains · Log · Queue | `/inbound`; Log and Queue are new |
| Server | Health · System log · Backup · Users · Settings | Health is new (the tables that left `/status`) |
| User menu | Account · Help · Sign out | Account is the user's half of `/settings`: credentials and e-mail |

A hidden page leaves no gap in the menu. The inbound and DMARC feature flags
stay as in [web.go](../../internal/web/web.go); who sees what changes — see
§ Who sees what.

## No compatibility

**Decided 2026-09-21 (owner): nothing is kept compatible across the redesign,
and the migration chain is deleted to zero.** The redesign is `2.0.0`.

- **URLs.** Old paths are not redirected; they are gone. Bookmarks break once.
- **Schema.** `internal/store/migrations/` is emptied and replaced by one
  baseline, `0001_init.sql`, equal to the 2.0 schema (including the new
  assignment table below). No historical `admin` table, no create-then-drop.
  This is [schema-squash](../roadmap.md#schema-squash) with its trigger now
  decided and without the upgrade gate that item planned: 2.0 starts from an
  empty data directory.
- **Docs.** `schema-migrations.md` shrinks to the 2.0 baseline; the 1.x chain
  stays in git history.

The only live installation is the owner's, and it is set up again on 2.0.
Nothing is built to ease that move, and nothing needs to be.

## Who sees what

**Decided 2026-09-21 (owner): a domain administrator manages inbound domains
too, and that is granted separately from outbound.** Today every inbound
handler is `requireGlobal()` and `user_domains` references outbound domains
only.

| | `global` | `domain` |
|---|---|---|
| Overview, Server | yes | no (404) |
| Outbound: domain, applications, settings, log, DMARC | all domains | assigned outbound domains |
| Inbound: domain (upstream, recipients), log | all domains | assigned inbound domains |
| Add / delete a domain, either direction | yes | no |
| Queues, either direction | yes | no, until the queue is a filterable table |
| Inbound › Quarantine | all domains | assigned inbound domains |
| Inbound › Filter lists, Server › Preflight | yes | no — the lists are instance-wide |
| Account, Help | yes | yes |

- New table `user_inbound_domains (user_id, inbound_domain_id)`, mirroring
  `user_domains`; both live in the 2.0 baseline. The same name on both lists
  is two assignments — the two kinds of domain stay separate entities.
- The user form has two lists (mockup `user.html`); the *domain* role needs at
  least one assignment on either.
- **Each list has *All*, which includes domains added later** — a flag per
  user and direction (`users.all_domains`, `users.all_inbound_domains`), not a
  tick on every row: otherwise tomorrow's domain is silently unassigned. *All*
  widens the list, not the role: adding and deleting domains, the queues and
  Server stay with the global role. The form works without scripts — with
  *All* ticked the server ignores the rows below it.
- **The role is a reach, not a rank.** Everyone in the panel is an
  administrator, so the label is `global` or `domain` — in the users list, the
  role select and the page kickers; never "admin".
- A group with nothing assigned is absent from that user's menu.
- **The spam filter is visible to whoever it affects.** Whether the instance
  filters inbound mail is an environment setting, so a domain administrator
  cannot look it up: every inbound domain page has a *Spam filter* box (on /
  off, engine, what happens to suspicious mail, who keeps the lists), the
  inbound domains list shows it in its head, and for the global administrator
  the filter's socket is one of the milter sockets on Health and Overview.
- Authorization helpers come in pairs: `requireDomain(id)` and
  `requireInboundDomain(id)`; lists and logs are filtered by assignment in the
  query, not in the template.

## Account e-mail

**Decided 2026-10-10 (owner): a domain's DMARC report address is one of three
choices, and nothing else exists.** This replaces the 2026-09-21 design, where
a domain followed a named user's default: a co-administrator of a domain was
shown another user's login name, and three explicit choices are simpler.

- A domain's aggregate-report address (`rua=`) is exactly one of: **no
  reports** (the state of a new domain), **the SelfPost hosted address**
  (offered only while hosted reports are enabled on the server), or **an
  address typed for this domain**. There is no per-user default, no domain
  following a user, and no keep / inherit. Changing the choice changes the
  DMARC record to publish.
- It is set in **Domain settings** and nowhere else. Beside the typed-address
  field there is a button that fills it with the signed-in user's profile
  e-mail (`panel.js`, `data-fill`): it only fills, saving is the form's own
  button, and it is absent when the profile has no e-mail. Without scripts the
  field is typed in.
- The 2.0 baseline has `users.email`: the user's own address, edited on
  **Account**, set by the global administrator on the user form and shown in
  the users list. It is what the panel writes to — event notifications
  ([panel-notifications](../roadmap.md#panel-notifications)) and a reset link
  ([password-reset](../roadmap.md#password-reset)) — and what the fill button
  offers. **Account** is Profile and Password; Server › Settings keeps nothing
  of DMARC.
- Baseline: `domains.dmarc_rua` alone (`''` = no reports). The hosted choice is
  the address SelfPost derives for the domain and the server's hostname, so it
  is stored as that address and read back as hosted when it equals it. Deleting
  a user changes no domain's address.

## Routes

**Decided 2026-09-21 (owner): paths are renamed to follow the new names and
structure.** A URL reads like the menu: `/<group>/<page>`, an entity under its
list, an action under the thing it changes.

| Screen (mockup) | New | Today |
|---|---|---|
| overview | `GET /overview` | `/status` |
| health | `GET /server/health` · `POST …/recheck` · `POST …/reload` | tables of `/status` · `/status/recheck` · `/reload` |
| preflight | `GET /server/preflight` · `POST …/preflight/run` · `POST …/preflight/test-email` | — (preflight; its plan says `/preflight`) |
| out-domains | `GET, POST /outbound/domains` | `/domains` |
| out-domain | `GET /outbound/domains/{id}` · `POST …/dns-recheck` | `/domains/{id}` |
| out-domain-settings | `GET /outbound/domains/{id}/settings` · `POST …/settings/reports` · `…/settings/ratelimit` · `…/settings/ratelimit/recalc` · `…/settings/export` | cards of `/domains/{id}` · `…/dmarc` · `…/ratelimit` · `…/ratelimit/recalc` · `…/export` |
| out-domain-delete | `GET, POST /outbound/domains/{id}/delete` | `/domains/{id}/delete` |
| out-app | `GET, POST /outbound/domains/{id}/applications/new` · `GET, POST …/applications/{aid}` · `POST …/{aid}/ratelimit/recalc` · `POST …/{aid}/delete` | `POST /domains/{id}/applications` · `POST /applications/{aid}/mode`, `/authips`, `/ratelimit` · `…/recalc` · `…/delete` |
| out-app-created | the response of `POST …/applications/new` and `POST …/{aid}/password`; no GET path, `Cache-Control: no-store` | a card on `/domains/{id}` after the POST |
| out-log · out-message | `GET /outbound/log` · `GET /outbound/log/{id}` | `/deliveries` · `/deliveries/{id}` |
| out-queue | `GET /outbound/queue` | `/mail-queue` |
| dmarc · dmarc-domain · dmarc-report | `GET /outbound/dmarc` · `…/dmarc/domains/{id}` · `…/dmarc/reports/{id}` | `/dmarc/…` |
| in-domains · in-domain | `GET, POST /inbound/domains` · `GET /inbound/domains/{id}` · `POST …/dns-recheck`, `…/upstream`, `…/recipients` | `/inbound` · `/inbound/{id}/…` |
| in-domain-delete | `GET, POST /inbound/domains/{id}/delete` | `/inbound/{id}/delete` |
| in-log · in-queue | `GET /inbound/log` · `GET /inbound/queue` | — (new, see below) |
| in-quarantine · in-quarantine-message | `GET /inbound/quarantine` · `GET …/quarantine/{id}` · `POST …/{id}/release` · `POST …/{id}/discard` | — (inbound-quarantine) |
| in-filter-lists | `GET, POST /inbound/filter-lists` · `POST …/filter-lists/{id}/delete` | — (inbound-antispam-panel) |
| system-log | `GET /server/log` | `/system-log` |
| backup | `GET, POST /server/backup` · `POST /server/backup/import` | `/backup` · `POST /domains/import` |
| users · user · user-delete | `/server/users` · `…/new` · `…/{uid}` · `…/{uid}/delete` | `/users/…` |
| settings | `GET, POST /server/settings` | the instance half of `/settings`: log retention, rate limits |
| account | `GET /account` · `POST /account/profile`, `/password` | `/settings`: credentials and the DMARC default (today `/account` redirects there) |
| help · components | `GET /help` · `GET /server/components` | `/help` · — |

Unchanged: `/healthz`, `/license`, `/static/`, `/setup/{token}`, `/login`,
`/logout`. `GET /` goes to `/overview`, or to `/outbound/domains` for a domain
administrator.

Rules that come with the rename:

- **One fragment name.** Every polled page answers `<page>/fragment`
  (`/overview/fragment`, `/outbound/log/fragment`, …) — today it is
  `fragment`, `rows` and `body`.
- **The application form is one POST.** The mockup has a single *Save
  application*; sender, IP allow-list and rate limit are validated and saved
  together, or not at all. The three separate POSTs go away.
- **No redirects.** Old paths, GET and POST alike, are removed (§ No
  compatibility), including today's `/account` → `/settings` redirect. The
  full list goes under *Removed* in the CHANGELOG.
- **Authorization can follow the tree — for `/server/` only.** Everything
  under `/server/` is global-only, so `requireGlobal()` can guard that subtree
  once instead of once per handler. `/outbound/` and `/inbound/` are checked
  per domain (§ Who sees what). Both are security-relevant: they go through
  the security review, and a per-route test asserts 404 for a domain
  administrator on every path they must not reach — including another
  tenant's domain id.
- `TestRoutesFollowNavigation` — every path in this table is registered with
  the listed methods, every menu entry points at a registered path, every old
  path answers 404, and no template contains an old path.

**Needs its own roadmap decision before it is built** (the mockups show the
target; none of this is smuggled in with the restyle):

- Queue as a table — parse `postqueue -j` instead of printing `postqueue -p`.
- Inbound queue — the same queue filtered by recipient domain.
- Inbound log — rides on `inbound-antispam-panel` (`2.1.0`).
- Server › Health — a route for the tables removed from Overview, and the
  home of *Reload configuration* (today a card on `/status`).

Until a row above is agreed, its page ships in the old content inside the new
shell, and its menu entry is hidden if the page does not exist.

## Room for roadmap items

The mockups draw today's features, the four rows above, and the screens of
the open roadmap items — none of them needed a new component or layout. A
mockup of a feature is its target look, not a decision to build it: each item
keeps its own status and plan. From here on a feature's screens are drawn
mockup-first in this system (`panel/src/`, `build.py`, `check.py --write`) as
the first step of that feature's plan — not invented during implementation.

| Roadmap item | Where it lands | Built from | Drawn |
|---|---|---|---|
| inbound-antispam-panel (agreed, `2.1.0`) | journal = **Inbound › Log** (add client IP and engine to the mockup's columns; `quarantine` joins the decisions); lists = new **Inbound › Filter lists**, global-only because the lists are instance-wide | list layout: add form + two tables, `status_tag`, filter form in `box_head`; the log links *Allow / Deny sender* to the lists | `in-log`, `in-filter-lists` |
| inbound-quarantine (candidate) | new **Inbound › Quarantine**, list + detail with *Release* / *Discard* | list layout; detail like `out-message` with `facts`, `log_pane` for headers only, Release beside a `sp-danger-zone` Discard | `in-quarantine`, `in-quarantine-message` — the open questions of its plan (where mail lives, what release means, retention, RBAC) stay open; the mockup assumes SelfPost-held mail, release to upstream, 14 days, delegated like the rest of Inbound |
| preflight (candidate) | new **Server › Preflight**, next to Health: Health is what runs now, Preflight is the deeper on-demand installation check with a test e-mail form | `postmark` verdict, a table of checks with `status_tag` and a *What to do* column, one form box for the test e-mail | `preflight` |
| panel-notifications (candidate) | per-user event choice on **Account**; one switch under **Server › Settings** | checkboxes in a box, `help` | **no** — drawn once, taken out of `account` and `settings` on 2026-10-10 (owner): 2.0 ships no box for a feature that is not built; redrawn as the first step of its plan |
| password-reset (candidate) | a link on sign-in, two signed-out screens (request, set new password) | the signed-out layout | **no** |
| delivery-log-storage (candidate) | the *Delivery log* box of **Outbound › Log › message**, filled from a table instead of a grep of today's `mail.log` | `log_pane`, unchanged | `out-message` |
| csrf-tokens | no screen; every form gains a hidden field — forms are rendered through one helper so the token cannot be forgotten | — | n/a |
| template-data-typing | no screen; the component partials are the natural first users of typed data | — | n/a |
| structured-logging, contributing, review follow-ups | none | — | n/a |

The sibling strip under the navbar holds five or six entries per group without
wrapping; Inbound would reach five (Domains · Log · Queue · Quarantine ·
Filter lists), Server six. Past that a group is split rather than squeezed.

Ordering with 2.0 (owner, 2026-10-09: feature order is not fixed, take
whatever suits development): **the redesign ships first as `2.0.0`**, and
inbound-antispam-panel follows as `2.1.0`, built from the component kit on
the 2.0 baseline. The other way round would have styled two screens twice and
written a 1.x migration only to dissolve it into the baseline. The antispam
back end (journal milter, log tailer, list CRUD, rspamd map sync) touches
nothing under `internal/web/view` and may be built alongside stages 0–1 below
if time allows; its two screens wait for the kit.

## Three layers

1. **Bulma, as shipped.** Vendored `bulma.min.css`, never edited. Only the
   classes listed in `check.py` (`BULMA`) are used; reaching for another is a
   design change.
2. **Adapters** — where display logic lives once:
   `status_tag` (status → `tag is-… is-light`) as a template function, and
   `copy_field` (the read-only value with its Copy button).
3. **Own components** — everything with an `sp-` class, styled in one
   `panel.css`, each rendered by one partial in `components.html`:

| Partial | What it is |
|---|---|
| `page_head` | path, `<h1>`, lead, actions; optionally a `postmark` |
| `postmark` | the round verdict stamp: ok / `sp-warn` / `sp-fail` |
| `box`, `box_head`, `box_foot` | numbered mono head with an end slot and an optional `sp-help` link to the section's Help topic (replaces the help drawer); variants `sp-danger-zone`, `sp-credential` |
| `flash` | Bulma `notification is-success / is-danger is-light` between `page_head` and the first box; a field's error stays in `help is-danger` |
| `health_card` | Overview only; ok / `sp-warn` / `sp-fail`; links to its detail |
| `dns_record` | one block per record: status, what to publish, "in DNS now" only on a mismatch |
| `facts` | label / value grid, `sp-big` for headline numbers |
| `side_menu` | Bulma `menu` with icons and count tags; sticky |
| `timeline`, `log_pane`, `empty_state`, `confirm_list`, `help_topic` | as on the components page |
| form rows | `sp-row` (wide + short), `sp-pair` (two equal) |

The shell belongs to `layout.html` alone: a brick navbar with hover menus
that carries the **wordmark, not the stamp** (at nav size the stamp dissolves
into the bar; it stays on sign-in, setup and the favicon); under it the
stamp's paper margin with the perforation along its outer edge — round holes,
flat teeth, the mark's ink hairline, one tile `perf-edge.svg`; then the
sibling-page strip; and the footer.

## Component kit

The three layers above are implemented as one unit **before any page is
restyled** — stage 0 of the checklist. The question the kit answers is the
owner's: a screen must not be able to invent a shared component on the spot.
The mockup fragments in `panel/src/` cannot answer it, because they copy the
component markup by hand into every screen; in the code that markup exists in
exactly one place.

The kit is, in `internal/web/view`:

| Part | What it is |
|---|---|
| `templates/components.html` | one `{{define}}` per partial of § Three layers; the only file that contains `sp-box-head`, `sp-postmark`, `sp-record`, `sp-facts`, … literally |
| `components.go` | the typed input of every partial — `Head{Kicker, Title, Lead, Postmark, Actions}`, `Box{No, Title, End, Help, Variant}`, `Record{Name, Status, Host, Type, Value, InDNS}`, `Fact{Label, Value, Big}`, `SideMenu{Groups}`, `Tag{Status, Label}`, … — so a page passes a struct, not a `dict` it composes itself. A partial's input is part of its definition: components.html documents the look, `components.go` the data |
| template functions | `status_tag` (status → `tag is-… is-light`), `copy_field`, `wbr_at` (`<wbr>` after `@` in table cells) |
| `static/panel.css` | `panel/theme.css` + `shared/brand.css`, no rule the mockups do not have; the old stylesheet is deleted at the end, not merged |
| `static/bulma.min.css`, icons | vendored, pinned by checksum; the icon font subset to the icons the mockups use (57 today), not the whole Tabler set |
| `layout.html` | the shell: navbar with the wordmark, perforated edge, sibling strip, user menu, footer; the signed-out variant |
| `GET /server/components` | the kit page, global role only: every partial in every state rendered from fixtures — the mockup `components.html` rebuilt by the real templates |

**Kit acceptance** (stage 0 is not done without it):

- `TestPanelClassVocabulary` and `TestPanelCSSContract` are green on the kit
  alone (the old pages are still on the ratchet list at this point, see
  § Against resurrected pages);
- every partial named in § Three layers exists, renders on the kit page, and
  has a typed input in `components.go` with a doc comment saying where it is
  used;
- a screenshot of `/server/components` from the running container sits beside
  the mockup under `docs/assets/panel-redesign/evidence/components.png`, and
  the reviewer finds no difference they cannot name;
- `panel/audit.js` on the kit page reports an empty `problems` list.

After stage 0 a page template is thin: it calls partials with structs and
contains no `sp-` markup of its own. If a page needs a component the kit lacks,
that is contract rule 2 — stop and ask; the owner decides whether it is a
`design:` change to the components page first.

## Type and tables

- **Three sizes.** Body 16px; `--sp-fs-s` 14px for supporting text (labels,
  `help`, notes, mono values, logs); `--sp-fs-xs` 13px for upper-case mono
  micro-labels and tags only. Nothing a person has to read is under 13px.
  Bulma's own small sizes are overridden (`help`, `tag`, `label`,
  `menu-label`, the navbar dropdown) or not used (`is-small` on a control,
  `is-size-7` — `sp-small` instead). `check.py` enforces the floor.
- **Contrast.** Text is at least 4.5:1 against the ground it actually sits on
  (3:1 for large text). Two traps are closed in the theme: Bulma picks a solid
  button's text colour from the fill's lightness, and the brick and the red
  land on the wrong side — solid buttons are white on colour; and the muted
  grey is `#5c6774`, not the 4.1:1 `#6b7684` of the old panel.
- **A table sits in a `table-container`**, so a wide one scrolls inside its
  box instead of being cut off. Addresses in table cells get a `<wbr>` after
  the `@` from one template function, so a column can shrink without breaking a
  name mid-word. Screens fit without scrolling at 1280px and up; below that a
  dense log scrolls inside its box.

## Layout rules

A page is exactly one of:

- **list** — `page_head`, then full-width boxes: the add form, then the table.
- **detail** — `page_head`, then `columns`: `is-3` `side_menu` + `is-9` boxes.
- **form** — `page_head`, then boxes in equal `columns`, one submit row below.
- **signed out** — the split brand / form screen.

Width comes from the layout, never from the page. A narrow form does not get a
`max-width`; it gets a column. **What stands beside it must be real** — the
form's own result, the facts it changes, a second form. A box written to fill
the space beside another is the same defect as a width tuned by hand, and the
first draft of Preflight had one. If nothing real belongs there, the form
shares its box with its result, or the page uses fewer columns.

## The contract

An implementer — human or model — **must not**:

1. Write an `sp-` class, a Bulma class outside the subset, or any other class
   that the components page does not show.
2. Add a component, a state or a layout. If a screen seems to need one, **stop
   and ask**; do not approximate it with ad-hoc markup.
3. Add a box whose only job is to fill space, or text that restates the page.
   Every box answers a question the operator has on that page.
4. Put a selector in `panel.css` that names a page or an element id, uses
   `!important`, sets a width outside the `WIDTH_OK` list, or a font size under
   the 13px floor.
5. Write component markup by hand in a page template instead of calling the
   partial.
6. Edit the vendored Bulma, or load a second CSS framework, icon set or any
   JavaScript widget.
7. Change a mockup, `outlines.json`, `check.py` or a guard test in the same
   commit as `internal/web`, or at all without the owner's explicit
   instruction. **The guards are not the implementer's to adjust.** A failing
   guard means the implementation is wrong, not the guard.
8. Report a step as done on the strength of a description. Done means the
   evidence below exists.

**Changing the design** is allowed and has one route: mockup first. Edit the
fragment in `panel/src/`, the components page if a part changed, run
`build.py` and `check.py --write`, commit as `design: …` with the reason, get
it accepted — and only then touch `internal/web`.

## Enforcement

Guard tests live in `internal/web/view` and run in `go test ./...` and CI.
They land **before** any template is restyled (checklist step 2), written and
reviewed by models other than the one that implements the templates.

| Test | Fails when |
|---|---|
| `TestPanelClassVocabulary` | a template, or the page it renders, uses a class outside the Bulma subset + the `sp-` classes of `panel.css`, or an icon outside the vendored subset; an `sp-` class is defined but unused, or used but not shown on the components page |
| `TestPanelCSSIsTheMockupStylesheet` | `panel.css` is not `brand.css` + `theme.css` rule for rule — a rule the mockups do not have is a design change |
| `TestPanelCSSContract` | `panel.css` has `!important`, an id or page selector, a size under the type floor, or a width outside `WIDTH_OK` |
| `TestTemplatesUseComponents` | a page template writes a component's `sp-` class itself instead of `{{template "…"}}`, or builds a partial's input with `dict`; a partial or its typed input is missing |
| `TestPanelOutlinesMatchMockups` | a page rendered with its fixture has a different component skeleton than `outlines.json` — a different design cannot pass as this one |
| `TestPanelPageStructure` | a rendered page breaks a structure rule of `check.py`: one `<h1>`, opens with `page_head`, a box starts with `box_head`, a table sits in a `table-container`, the shell is the layout's |
| `TestRedesignedPagesLoadOnlyTheKit` | a redesigned page loads the old stylesheet, or any stylesheet or script outside the kit |
| `TestOutlinePortMatchesCheckPy` | the Go port of `outline()` reads a mockup differently from `check.py` |
| `TestLegacyRatchetMatchesTheEngine` | the ratchet names something that is not a page, or a page off the ratchet is not a drawn screen |
| `TestVendoredAssetsArePinned` | `bulma.min.css` or the icon subset differs from its pinned SHA-256 |
| `TestStaticHoldsOnlyTheKnownAssets` | `/static` holds a file outside the allow-list — a second framework, icon set or script |
| `TestRoutesFollowNavigation` | § Routes: a path is missing, an old path still answers, a template links to a path the router does not serve |
| `TestNoTemplateUsesInlineScriptOrStyle` | (exists) inline `style` / script |

The guards are the files `guard_*_test.go` under `internal/web`. They read
their lists from the mockup directory itself — the Bulma subset and
`WIDTH_OK` from `check.py`, the shell's classes from `build.py`, the
stylesheet, the outlines — so there is no second copy to drift.

CI adds one step, `design-first`
([.github/scripts/design-first.sh](../../.github/scripts/design-first.sh),
run on every commit of a push or pull request): a commit that touches
`docs/assets/panel-redesign/panel/**`, a guard test or the script itself must
not touch anything else under `internal/web/`.

### Against resurrected pages

It has happened once: an implementer took the old page templates out of git
history and reported them as the redesigned pages. Four guards make that
impossible to pass, and none of them depends on anyone looking:

1. **The ratchet.** One list,
   [internal/web/view/legacy_pages.txt](../../internal/web/view/legacy_pages.txt),
   names the pages that are still old and therefore exempt from
   `TestPanelOutlinesMatchMockups`, `TestPanelClassVocabulary` and
   `TestTemplatesUseComponents`. It is a file of its own, beside the guards
   and not inside them, because the step that restyles a page must edit it
   and must not edit a guard. It starts as every page and **can only
   shrink**: `design-first` fails a commit whose list is not a subset of its
   parent's. Restyling a page means removing it from the list in the same
   commit; from then on that page is held to its outline forever. Putting the
   old template back fails three tests at once, and putting the name back on
   the list fails CI. Two entries are not pages: `@kit` keeps the kit's own
   tests quiet until stage 0 is accepted, `@routes` does the same for
   `TestRoutesFollowNavigation` until stage 1 renames the paths — "red where
   expected" without a red main branch, and equally unable to come back.
2. **The outline is the design.** An old page has a different component
   skeleton from `outlines.json` — different boxes, no `page_head`, no
   `side_menu` — so it cannot match, however its classes are renamed.
3. **Old paths are forbidden.** `TestRoutesFollowNavigation` fails on any
   template that contains a pre-2.0 path; every old page does.
4. **No blob from the past.** The `design-first` CI step also compares every
   template a commit adds or changes under `internal/web/view/templates/`
   with every template blob in the history before it — under any path, so a
   rename does not hide it — whitespace-normalised, and fails on a match. This catches a byte-for-byte revert that happens to be on the
   ratchet list still; the three tests above catch everything else.

The evidence rules below close the remaining gap, a screenshot that is not
what it claims to be: screenshots come from the running container with the
e2e fixtures, so they show fixture data a mockup does not contain, and the
reviewer is a model that did not write the step.

### Evidence for "done"

Every checklist step that restyles pages ends with, in the commit:

- `go test ./internal/web/...` output in the commit message body (pass);
- the output of `panel/audit.js` run against the restyled pages of the running
  container, with an empty `problems` list;
- a screenshot of each restyled page at 1440×900, saved next to its mockup's
  name under `docs/assets/panel-redesign/evidence/`, rendered from the running
  container with the e2e fixtures — not from the mockup;
- a review by a model that did not write the step
  ([development.md](../development.md) § Model routing): mockup and screenshot
  side by side, every difference either fixed or listed for the owner.

The owner accepts a step by looking at the pairs. A step without them is not
reviewed.

## Out of scope

- New features beyond the four rows listed under Information architecture.
- Dark scheme and the narrow-screen shell: Bulma ships both; they are drawn
  and agreed as a `design:` change before anyone implements them.
- Weakening CSP; a JS bundler; a front-end framework.

## Implementation checklist
Roadmap status: **agreed** (owner, 2026-10-09). Stages are ordered for
development, not by feature priority (owner, 2026-10-09). Stage 1 touches no
file under `internal/web/view` and may run alongside stage 0; stage 2 starts
only when both are done. Every stage-2 step ends with the evidence of
§ Evidence for "done".

**Decided**

- [x] Owner decision recorded here: keep today's paths or rename with redirects — **owner** (2026-09-21: rename, § Routes)
- [x] Owner decision: no compatibility, migrations to zero, `2.0.0` — **owner** (2026-09-21, § No compatibility)
- [x] Owner decision: inbound is delegated per user, separately from outbound — **owner** (2026-09-21, § Who sees what)
- [x] Owner decision: the redesign ships before inbound-antispam-panel; feature order follows development convenience — **owner** (2026-10-09, § Room for roadmap items)
- [x] Decision inside 2.0: **Server › Health** is built with the restyle (the tables that leave Overview need a home, § Routes); **queue as a table** and the **inbound queue** stay roadmap decisions — both queue pages ship old content in the new shell, the Inbound › Queue entry hidden — (2026-10-09)

**Stage 0 — foundation and component kit** (§ Component kit)

- [x] Vendor Bulma 1.0.4 and the icon subset (MIT) under `/static`, NOTICE and development.md rows, pinned checksum — **Haiku**
- [x] Guard tests from § Enforcement and `TestRoutesFollowNavigation`, red against today's panel where expected, with the `legacyPages` ratchet listing every page; `design-first` CI step including the ratchet and the history-blob check (§ Against resurrected pages) — **Opus**
- [x] `panel.css` from `panel/theme.css` + `shared/brand.css`; `components.html` partials with typed inputs in `components.go`; `status_tag`, `copy_field`, `wbr_at` — **Sonnet**
- [x] `layout.html`: navbar, perforated edge, sibling strip, user menu, footer, visibility flags; the signed-out shell — **Sonnet**
- [x] `GET /server/components` behind the global role rendering every partial from fixtures; kit acceptance (§ Component kit) with evidence — **Sonnet**, reviewed by **Opus**

**Stage 1 — data and routes** (no template work; parallel with stage 0)

- [x] Schema from zero: single `0001_init.sql` baseline with `users.email`, `user_inbound_domains`, the two `all_*` flags; store tests; `schema-migrations.md` rewritten — **Opus**
- [x] Inbound delegation: `requireInboundDomain`, lists and log filtered in the query, two-list user form data, per-route 404 tests for both roles including another tenant's id — **Opus**
- [x] Routes renamed per § Routes: new paths, one fragment name, old paths removed, subtree `requireGlobal()` for `/server/`; links in the old templates retargeted so the panel works on every commit — **Opus**
- [x] The application form as one POST; `out-app-created` as the response with `Cache-Control: no-store` — **Opus**
- [x] Server › Health route and handler (tables and *Reload configuration* from `/status`); Overview handler reduced to verdicts — **Opus**

**Stage 2 — pages, in groups** (each step: partial calls only, page off the ratchet list, evidence pairs)

- [x] Signed-out pages: login, setup — **Sonnet**
- [x] Overview, Health, and the Account / Settings split — **Sonnet**
- [x] Outbound: domains, domain, domain settings, application form, shown-once password, delete — **Sonnet**
- [x] Outbound: log, message, DMARC hub / domain / report — **Sonnet**
- [x] Inbound: domains, domain with the *Spam filter* box, delete — **Sonnet**
- [x] Server: system log, backup, users, user form, delete; Help as `help_topic` partials, the drawer removed — **Sonnet**
- [x] Queues: old content inside the new shell (outbound), Inbound › Queue hidden — **Sonnet**
- [x] Remove the old `panel.css`, the drawer code in `panel.js` and every class nothing references; `legacyPages` empty, vocabulary test green with no allow-list — **Sonnet**

**Stage 3 — reviews, docs, 2.0.0**

- [ ] Technical review of the whole against the mockups, evidence pairs complete — **Opus**
- [x] Security review of the route rename, subtree authorization and inbound delegation — **Fable**
- [ ] guide.md screenshots and wording for the new navigation; architecture.md § Panel HTTP surface and § Persistence; security.md roles; *Removed* in the CHANGELOG with one line that 2.0 starts from an empty data directory; strike the three UI rows of review-2026-08-followups that the redesign retires — **Sonnet**
- [ ] `go vet`, `go test ./...`, e2e suite; version cut `2.0.0` — **Haiku**

**After 2.0** — features land in the kit, each from its own plan, mockup first
(§ Room for roadmap items): inbound-antispam-panel `2.1.0`; then, in whatever
order suits development, preflight, delivery-log-storage, panel-notifications,
password-reset (two screens to draw first), queue as a table with the inbound
queue, csrf-tokens
(one hidden field in the form partial once security.md is settled),
inbound-quarantine after its open questions.
