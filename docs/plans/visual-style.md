# Plan: visual-style

**Status:** agreed  
**Version:** no bearing on semver — presentation only, no schema and no route
changes.  
**Order:** independent of the feature roadmap; may be taken up between feature
items.

---

## Goal

Bring the control panel's surface in line with the mark that was approved in
[selfpost-proof.html](../assets/selfpost-proof.html): its palette, its
typography, and the plainness of its components. Today the panel is a default
blue-on-cool-grey admin theme standing next to a warm brick stamp, so the mark
reads as pasted onto someone else's page.

## Scope

**In:**
- `internal/web/view/static/panel.css` — colour tokens, typography, spacing,
  every component rule.
- `internal/web/view/static/` — three self-hosted font files.
- Templates, only where a class has to be added or a wrapper introduced for a
  rule to have something to attach to.
- `NOTICE` — the OFL attribution the font files oblige.

**Out:**
- Any change to what a page does, which pages exist, or what an operator has to
  click. No new features, no copy rewriting.
- The navigation's position and the two-column shell. The proof's panel mock
  shows a horizontal bar on a dark header; the panel's left column also carries
  the per-page section index (`.sections` plus the scroll-spy in `panel.js`),
  which that layout has nowhere to put. Keeping the column is a deliberate
  divergence from the mock, not an oversight.
- The mark files themselves (`logo.svg`, `logo-compact.svg`, `favicon.*`) —
  already drawn, already converted to outlines.

## Constraint that shapes everything

The panel's Content-Security-Policy is a plain `default-src 'self'` with no
inline-style exemption ([security.md](../security.md)). Every rule lives in
`panel.css`; a `style="..."` attribute in a template is blocked and silently
does nothing. Self-hosted fonts are served from the panel's own origin and are
therefore already covered — no CSP change is needed, and none may be made.

## Typography

IBM Plex, self-hosted. The mark is Plex converted to outlines, so the panel
setting its own name in Segoe UI or Cantarell is the seam this whole item
exists to close.

| File | Covers | Size |
|---|---|---|
| `static/ibm-plex-sans.woff2` | variable, weights 100–700, latin | 45.7 KB |
| `static/ibm-plex-mono-400.woff2` | mono regular, latin | 14.8 KB |
| `static/ibm-plex-mono-600.woff2` | mono semibold, latin | 15.7 KB |

76 KB in total, in a 20 MB binary. The variable file replaces what would
otherwise be five static weights and lets the scale below use 300 and 500
without paying per weight.

Monospace is the one the operator actually reads: DKIM records, `mail.log`
lines, application logins, socket paths, generated passwords. `ui-monospace`
resolves to Consolas, SF Mono or DejaVu Sans Mono depending on the operator's
machine, and those differ in advance width — the six-column send log wraps
differently for each. A shipped mono makes those tables one layout.

| Role | Family | Size | Weight |
|---|---|---|---|
| Body | sans | 15px / 1.5 | 400 |
| `h1` | sans | 1.55rem, tracking −0.01em | 300 |
| `h2` | sans | 1.05rem | 600 |
| `label` | sans | 0.9rem | 600 |
| Nav entry / active | sans | 0.95rem | 400 / 600 |
| `th` | **mono**, uppercase, tracking 0.08em | 0.75rem | 500 |
| `.st` status badge | **mono** | 0.78rem | 500 |
| `.code`, `.mono`, `.metric` | **mono** | 0.85rem | 400 |

`font-display: swap`, so a cold load shows the system stack for a frame rather
than blank text.

## Colour tokens

Names stay as they are wherever they already exist: the dark scheme reassigns
the same custom properties, which is why no rule in the file needs
`!important`. Warm neutrals replace the cool greys; brick becomes the accent.

| Token | Light | Dark |
|---|---|---|
| `--bg` | `#F4F2ED` | `#16181B` |
| `--fg` | `#12161C` | `#E9E6E0` |
| `--muted` | `#6B7684` | `#9AA1A9` |
| `--card-bg` | `#FFFFFF` | `#1D2024` |
| `--border` | `#DEDCD7` | `#2C2F34` |
| `--control-border` | `#CBC8C1` | `#3A3E44` |
| `--input-bg` | `#FFFFFF` | `#14161A` |
| `--code-bg` | `#EFEDE9` | `#14161A` |
| `--surface-bg` | `#EAE7E0` | `#23262B` |
| `--accent-fill` / `--on-accent` | `#7A3B2E` / `#FFFFFF` | `#8E4535` / `#FFFFFF` |
| `--accent-text` | `#7A3B2E` | `#CE7B66` |
| `--nav-active-bg` | `#EDE4DE` | `#2A1F1B` |

Brick splits into a fill and a text value because `#7A3B2E` on `#16181B` is
about 2:1 — unreadable as a dark-scheme link. The fill lightens just enough to
keep white on it above 4.5:1; the text value lightens further.

Status families (`--st-ok-*`, `--st-warn-*`, `--st-error-*`, `--st-unknown-*`),
the flash, the credential card and `--danger-*` keep their hues and are only
warmed to sit on paper. The one thing to watch is brick against `st-error` red:
the proof rejected several candidate colours precisely so that the mark would
not read as a status, and the same test now applies to every brick button
standing in a row of `error` badges.

## Components

Everything already in `panel.css`, in the order it appears there: card, form
controls, buttons (filled, outlined, danger), flash, table, status badge,
`.code`, nav (brand, links, sections, session), application list and its
disclosure panels, credential card, status page meters and facts, delivery
timeline, log tables, split layout, encrypt fields, footer.

Two component-level changes rather than pure repaints, both forced by the
accent:

- Row actions (`td.actions a.danger`, `Delete`) become outlined instead of
  filled. A filled red button in a table row next to a filled brick button
  reads as one block of colour.
- Nav entries carry the active state as brick text on a warm tint rather than
  the current blue tint.

## Order of work

1. Fonts into `static/`, `@font-face` and the type scale in `panel.css`,
   `NOTICE` attribution. Nothing else changes shape.
2. Token block: light and dark, both schemes in one pass.
3. Chrome: `layout.html`'s nav, footer, shell.
4. Signed-out pages: `login`, `setup` — the mark and one card, where the seam
   is worst.
5. `dashboard`, `domain_detail`, `domain_delete`.
6. `deliveries`, `deliveries_rows`, `delivery`.
7. `status` + `status_body`, `mail_queue*`, `system_log*`.
8. `backup`, `account`, `users`, `user_form`, `encrypt_fields`.
9. `CHANGELOG.md` under `[Unreleased]`.

## Verification

- Every page rendered locally and screenshotted in both schemes before and
  after (`panel.exe` on Windows, headless Edge), including a 375px-wide pass —
  the nav column and the wide tables are where a repaint breaks layout.
- Contrast: body text and every status badge at 4.5:1 or better against its own
  background, UI borders at 3:1. Brick on white is 7.3:1 by the proof's own
  measurement; the dark-scheme values above are the ones to re-check.
- `go build ./... && go vet ./... && go test ./...`; the template guards in
  `internal/web/view/templates_test.go` must stay green, and the static-asset
  ETag test grows to cover the three font files.
- No `style=` attribute anywhere in `templates/` — the CSP would drop it.

## What is done

The restyle itself landed in `652f1fe`, with the table-wrapping fixes it
surfaced in `f44f533`. Every page in the order above was rendered in both
schemes from a panel running locally and checked: the signed-out pair, the
domain list empty and with three domains, the whole domain page (credential
card, DKIM/SPF/DMARC, DNS status, applications, rate limit, export, danger
zone), the delete confirmation, the send log with rows, a single delivery,
status, mail queue, system log, backup, settings, users and the user form.

Two of those need standing in for what the container provides: `supervisorctl`,
`saslpasswd2` and `postmap` stubs on `PATH`, `postfix/`, `opendkim/keys/` and
`sasl/` created inside the data dir by hand, and rows seeded into `send_log` —
without them the domain, application and send-log pages do not exist locally.

## Outstanding

Nothing here blocks the item; each is written down so it is not rediscovered.

1. **Send-log status is bare text**, while every other status in the panel is a
   `.st` badge. Making it one is not a repaint: it needs a mapping from
   `sent`/`queued`/`deferred`/`bounced` onto the four badge colours, which is a
   judgement about severity (is `deferred` a warning?) rather than a style.
   **Needs a decision before it is written.**
2. **The panel overflows horizontally at 375px** — `main` and its cards render
   wider than the window and the page scrolls sideways. Reproduced with the
   stylesheet at `f59befd` too, so it predates this work; tracked separately.
   Removing the navigation does not fix it, so it is in `main`/`.card`, not in
   the bar the `@media (max-width: 66rem)` block lies down.
3. **Three views were only ever seen empty**: the mail queue with entries, the
   system log with lines, and a delivery's own `mail.log` lines (`table.log`).
   All three need a running Postfix, so they are a test-server check, not a
   local one. `table.log` is the only restyled component with no screenshot
   behind it.
4. ~~**CSP and the font ETags**~~ — **done** on the test server at
   `1.1.0-post.669f928`. The policy is unchanged
   (`default-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self';
   frame-ancestors 'none'`) and admits all three fonts, which come back as
   `font/woff2` with `Cache-Control: no-cache` and a content ETag: a matching
   `If-None-Match` gets 304, a stale one gets the bytes. The signed-out page
   renders in Plex over the network.
5. **`font-display: swap` has never been observed** — every render had the fonts
   already on disk. Worth one cold load over the network to see how long the
   system stack is on screen.

## Done when

- The panel and the mark read as one design in both schemes, at the reading
  measure and on the wide data pages.
- Nothing an operator does changed: same pages, same controls, same copy.
- Fonts are served from the panel's own origin under the unchanged CSP, and the
  image works with no network access.
- `NOTICE` credits IBM Plex (OFL-1.1); build, vet and tests are green.
- The three views in **Outstanding** 3 are seen with real data on the test
  server, and 4 is confirmed there.

## Risks

- **Visual regression across 21 templates.** The panel has pages that are only
  reachable mid-workflow (the credential card, the delete confirmation, the
  encrypt fields). Mitigation: the page order above is a checklist, and each
  step is screenshotted rather than assumed.
- **Brick against the error red.** If the two fight in a real row of the send
  log, the accent gets pulled back to the mark and the buttons stay neutral —
  the mark's colour is fixed, the panel's accent is the negotiable one.
- **Thin weights on dark.** `h1` at 300 is the one place a variable font makes
  it easy to go too light; check it on the dark scheme before keeping it.
