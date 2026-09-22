# Plan: panel-redesign

**Status:** candidate  
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
| User menu | Account · Help · Sign out | Account is the user's half of `/settings`: credentials, e-mail, DMARC default |

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

**Decided 2026-09-21 (owner): a user's e-mail is theirs, not a DMARC field.**
Today the column is `users.dmarc_report_email` and the only place it shows is
the DMARC card, so it reads as if it existed for that alone.

- The 2.0 baseline has `users.email`. It is what the panel writes to: event
  notifications ([panel-notifications](../roadmap.md#panel-notifications)) and
  a reset link ([password-reset](../roadmap.md#password-reset)).
- **Account** edits it; the global administrator sees and sets it on the user
  form and sees it in the users list.
- DMARC only *may* use it. **The default report address is a user's setting,
  not the server's** (owner, 2026-09-21): it lives on **Account** with its
  report-authorization record, as a choice — SelfPost hosted, *my account
  e-mail*, another address, none. Historically it was the administrator's
  profile field, later mirrored into an instance setting only the global user
  could edit; Server › Settings keeps nothing of it.
- A domain can have several users, so "the default" must be someone's. A
  domain's report address is *the default of a named user* (shown as
  `admin's default — mix@example.org`), the hosted address, a custom one or
  none. A new domain follows whoever created it; any user of the domain may
  switch it to their own default. If that user is deleted the domain falls
  to *none* and its DMARC check says so. Baseline: `users.dmarc_default_mode`,
  `users.dmarc_default_address`, `domains.dmarc_rua_user_id`.

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
| settings | `GET, POST /server/settings` | the instance half of `/settings`: log retention, rate limits; plus the notifications switch |
| account | `GET /account` · `POST /account/profile`, `/password`, `/notifications`, `/dmarc` | `/settings`: credentials and the DMARC default (today `/account` redirects there) |
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
- Inbound log — rides on `inbound-antispam-panel` (`1.10.0`).
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
| inbound-antispam-panel (agreed, `1.10.0`) | journal = **Inbound › Log** (add client IP and engine to the mockup's columns; `quarantine` joins the decisions); lists = new **Inbound › Filter lists**, global-only because the lists are instance-wide | list layout: add form + two tables, `status_tag`, filter form in `box_head`; the log links *Allow / Deny sender* to the lists | `in-log`, `in-filter-lists` |
| inbound-quarantine (candidate) | new **Inbound › Quarantine**, list + detail with *Release* / *Discard* | list layout; detail like `out-message` with `facts`, `log_pane` for headers only, Release beside a `sp-danger-zone` Discard | `in-quarantine`, `in-quarantine-message` — the open questions of its plan (where mail lives, what release means, retention, RBAC) stay open; the mockup assumes SelfPost-held mail, release to upstream, 14 days, delegated like the rest of Inbound |
| preflight (candidate) | new **Server › Preflight**, next to Health: Health is what runs now, Preflight is the deeper on-demand installation check with a test e-mail form | `postmark` verdict, a table of checks with `status_tag` and a *What to do* column, one form box for the test e-mail | `preflight` |
| panel-notifications (candidate) | per-user event choice on **Account**; one switch under **Server › Settings** | checkboxes in a box, `help` | `account`, `settings` |
| password-reset (candidate) | a link on sign-in, two signed-out screens (request, set new password) | the signed-out layout | **no** |
| csrf-tokens | no screen; every form gains a hidden field — forms are rendered through one helper so the token cannot be forgotten | — | n/a |
| template-data-typing | no screen; the component partials are the natural first users of typed data | — | n/a |
| structured-logging, contributing, review follow-ups | none | — | n/a |

The sibling strip under the navbar holds five or six entries per group without
wrapping; Inbound would reach five (Domains · Log · Queue · Quarantine ·
Filter lists), Server six. Past that a group is split rather than squeezed.

Ordering with 2.0: inbound-antispam-panel ships first in `1.10.0` and adds its
migration to the 1.x chain as planned; the 2.0 baseline then folds it in.

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
| `TestPanelClassVocabulary` | a template uses a class outside the Bulma subset + the `sp-` classes of `panel.css`; an `sp-` class is defined but unused, or used but absent from `components.html` |
| `TestPanelCSSContract` | `panel.css` has `!important`, an id or page-name selector, or a width outside `WIDTH_OK` |
| `TestTemplatesUseComponents` | a page template contains `sp-box-head`, `sp-postmark`, `sp-record`, `sp-facts`, … literally instead of `{{template "…"}}` |
| `TestPanelOutlinesMatchMockups` | a page rendered with fixtures has a different component skeleton than `outlines.json` — a different design cannot pass as this one |
| `TestVendoredBulmaChecksum` | `bulma.min.css` differs from the pinned SHA-256 |
| `TestNoTemplateUsesInlineScriptOrStyle` | (exists) inline `style` / script |

CI adds one step, `design-first`: a commit that touches
`internal/web/view/**` must not touch `docs/assets/panel-redesign/panel/**`
or the guard tests.

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
No code until the roadmap status is **agreed**.

- [x] Owner decision recorded here: keep today's paths or rename with redirects — **owner** (2026-09-21: rename, § Routes)
- [x] Owner decision: no compatibility, migrations to zero, `2.0.0` — **owner** (2026-09-21, § No compatibility)
- [x] Owner decision: inbound is delegated per user, separately from outbound — **owner** (2026-09-21, § Who sees what)
- [ ] Vendor Bulma 1.0.4 and the used Tabler icons (MIT) under `/static`, NOTICE and development.md rows, pinned checksum — **Haiku**
- [ ] Guard tests from the table above and `TestRoutesFollowNavigation`, red against today's panel where expected; `design-first` CI step — **Opus**
- [ ] Schema from zero: single `0001_init.sql` baseline with `user_inbound_domains`, store tests, `schema-migrations.md` rewritten — **Opus**
- [ ] Inbound delegation: `requireInboundDomain`, filtered lists and log, two-list user form, per-route 404 tests for both roles — **Opus**
- [ ] Routes renamed per § Routes: new paths, one fragment name, old paths removed, subtree `requireGlobal()` for `/server/` — **Opus**
- [ ] `panel.css` from `panel/theme.css`; `components.html` partials; `status_tag`; a `/components` page behind the global role rendering every partial — **Sonnet**
- [ ] `layout.html`: navbar, perforated edge, sibling strip, user menu, footer; visibility flags — **Sonnet**
- [ ] Signed-out pages: login, setup — **Sonnet**
- [ ] Overview (verdicts) and Account / Settings split — **Sonnet**
- [ ] Outbound: domains, domain, domain settings, application form as one POST, shown-once password, delete — **Sonnet**
- [ ] Outbound: log, message, DMARC hub / domain / report — **Sonnet**
- [ ] Inbound: domains, domain, delete — **Sonnet**
- [ ] Server: system log, backup, users, user form, delete; Help — **Sonnet**
- [ ] Remove the old `panel.css` rules and classes nothing references; vocabulary test green with no allow-list — **Sonnet**
- [ ] Technical review of the whole against the mockups, evidence pairs complete — **Opus**
- [ ] Security review of the route rename, subtree authorization and inbound delegation — **Fable**
- [ ] guide.md screenshots and wording for the new navigation; architecture.md § Panel HTTP surface and § Persistence; security.md roles; *Removed* in the CHANGELOG with one line that 2.0 starts from an empty data directory — **Sonnet**
- [ ] `go vet`, `go test ./...`, e2e suite — **Haiku**
