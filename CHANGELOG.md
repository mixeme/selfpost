# Changelog

All notable changes to this project are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning follows [SemVer](https://semver.org/).

## [Unreleased]

### Changed

- panel: **Status** is wide again so paired cards fill the column. Layout:
  Overall; Machine ‖ Processes; Mail queue ‖ TLS certificate; Milter sockets ‖
  Hostname / reverse DNS; Configuration. Dropped lead blurbs on Machine, TLS
  certificate, and Hostname (and Hostname's trailing detail line); milter
  socket paths omitted from the table; queue link reads **View queue**; milter
  ok detail is `Listening` without a trailing period and sits in its own
  Detail column beside the status badge; CPU detail is only core and thread
  counts (no load average); memory detail is `N used of M` without the
  «available to new work» clause; network detail lists per-interface totals
  only (rates stay in the Usage column). No «On this page» section index —
  the paired layout is short enough. Hostname stays in the polled fragment so
  the pair survives HTMX refresh. In-panel docs for the removed blurbs filed
  as roadmap `panel-docs`.

### Fixed

- panel: cards inside `.split` used `margin: 0 auto`, which in a CSS grid
  shrinks each card to its content and centres it in the track instead of
  filling half the row. Side margins are cancelled for `.split > .card`
  (Status, Settings, and a delivery's message/history).
- panel: on the user create/edit form, **Assigned domains** checkboxes stacked
  the box above the domain name (and stretched it full-width) because the form's
  block-label and full-width input rules applied to them. Checkbox rows now use
  the shared `label.check` layout; the fieldset has matching spacing.

## [1.2.1] - 2026-08-11

Panel refinements after 1.2.0: navigation icons, status-badge centreing,
drill-down back-link placement, and safeguards for the sole global
administrator. Upgrading is a tag bump; no migration.

### Fixed

- panel: status badge text sat low in the box (and below the heading or label
  beside it). IBM Plex Mono sits low in its em square; the previous top-heavy
  padding made that worse. Bottom padding is now heavier so the word centres
  optically.
- panel: the **Users** navigation icon was two full silhouettes with staggered
  baselines, so the pair looked lopsided at 16 px. The rear person is now a
  right-side crescent (head + shoulder) behind a full front silhouette aligned
  with `icon-account`.
- panel: the **Settings** navigation icon was a sun-with-rays (circle plus
  spokes), not a gear. It now uses a toothed cog so it matches the other
  session icons and the 1.2.0 release note.
- panel: the user create/edit form placed «Back to users» at the bottom of the
  card instead of under the heading like the delivery, domain, and domain-delete
  pages. A shared `back_link` template now renders every drill-down up-link, and
  `TestDrillDownPagesPlaceBackLinkAboveContent` guards its position.

### Changed

- panel: the user edit form disables role change and delete for the only global
  administrator, with a short note, instead of allowing the action and showing
  an error on submit.
- panel: the user create/edit form hides **Assigned domains** when the role is
  global administrator, since that role manages every domain anyway.
- panel: **Settings** shows panel credentials and DMARC aggregate reports side
  by side for global administrators (the same `.split` layout as a delivery's
  message and history). Domain-scoped users keep the single narrow card.

## [1.2.0] - 2026-08-11

The second MINOR after 1.0.0: domain administrators with per-domain scope, a
panel visual refresh on the SelfPost palette, and refinements to navigation and
the send log. Upgrading runs one SQLite migration (`0005_panel_users`); the
single administrator becomes a global user.

### Added

- panel: **domain-admin role** — global administrators manage panel users and
  assign domains; domain administrators see only their domains (applications,
  DKIM/DNS, per-domain DMARC, deliveries, export, L2 limits). Status, full
  backup, mail queue, system log, domain add/delete, and `/reload` stay
  global-only. SQLite migration `0005_panel_users` migrates the single
  administrator into a global user; sessions and full backup restore carry users
  and domain bindings.

### Fixed

- Docker build: `LICENSE` is no longer excluded by `.dockerignore`, so the
  runtime image can copy it into `/usr/share/doc/selfpost/` as AGPL requires. A
  clean build failed once the cached layer was invalidated.
- panel: on signed-in pages with a narrow card (**Settings**, the user form)
  the heading, flash, card and footer now share one left edge. `.card.narrow`
  had overridden only `max-width` while `main > *` still centred siblings on
  the 48rem measure, so the card floated 12rem to the right of the title.
  `main:has(> .card.narrow) > *` keeps the stack on 24rem without narrowing
  the column, so the navigation stays put; login/setup are unchanged.

### Changed

- panel: navigation session icons are distinct — **Settings** uses a gear,
  the signed-in user line carries the single-user icon, and **Users** a
  two-person group mark instead of the same account silhouette for all three.
- panel: `/` redirects domain administrators to `/domains`; global users still
  land on `/status`. Navigation hides global-only sections for domain
  administrators.
- panel: **Sign out** in the navigation column uses the same type size and
  weight as the page entries above it; only the red tint and border mark it as
  destructive.
- panel: **visual style** brought in line with the SelfPost mark — brick accent
  and warm paper in place of the blue-on-cool-grey defaults, IBM Plex Sans and
  IBM Plex Mono served by the panel itself, squarer corners, and column
  headings, status badges and small labels set in the mono face. Light and dark
  schemes both keep their contrast; no page, control or workflow changed. The
  send log stops breaking `Details` and `deferred` across two lines when a row
  is tight. Badge padding and line-height are tuned so lowercase labels sit
  centred in the box. The three WOFF2 files add ~76 KB to the image and are
  served from the panel's own origin, so the Content-Security-Policy is
  unchanged (`default-src 'self'`).
- panel: the send log's **status is a badge**, in the same ok/warn/error/unknown
  colours the status page and the DNS checks use, instead of the one place in
  the panel where a status was bare text. The mapping is the one the delivery
  page already applied — `sent` is ok, `deferred` a warning, `bounced` and
  `rejected` errors, `queued` unknown because nothing has gone wrong yet.
- docs: [roadmap.md](docs/roadmap.md) and [product.md](docs/product.md) no
  longer list domain-admin or visual-style as open work — both ship in this line.
  Completed plan files (`domain-admin`, `visual-style`, `web-split`,
  `narrow-page-alignment`) are removed; history stays in git and the entries
  above. Inbound relay is the main agreed 1.x+ item left on the roadmap.

## [1.1.0] - 2026-08-10

The first MINOR after 1.0.0: send-only DMARC guidance in the panel, AGPL
packaging on every page, and an internal split of `internal/web` ahead of
domain-admin work. Upgrading runs one SQLite migration (empty defaults;
existing DNS guidance is unchanged until you set a report address).

### Changed

- `internal/web` split into subpackages (`web/view`, `web/auth`, `web/validate`,
  `web/handlers`); the composition root (`web.New`, `web.Config`, `Server.Handler`)
  is unchanged for `cmd/panel`. Templates and static assets moved under
  `internal/web/view/`.

### Added

- panel: DMARC guidance for send-only relays — the suggested `_dmarc` record
  is now `p=none` without `rua=` by default; *Settings* and each domain page
  let you configure an optional aggregate-report address (profile default plus
  per-domain inherit / none / custom). When `rua=` targets another domain, the
  panel shows and DNS-checks the hub's `_report._dmarc` authorisation record.
  Domain export/import carries per-domain overrides.
- AGPL packaging hygiene: [NOTICE](NOTICE) names the copyright holder and the
  bundled third-party works (htmx 0BSD, IBM Plex OFL in outlined logos); the
  panel footer on every page — including login and setup — shows copyright, a
  link to `/license` (embedded AGPL text), a Source link to the public
  repository, and "No warranty"; the runtime image ships `LICENSE` and
  `NOTICE` under `/usr/share/doc/selfpost/`. `docs/development.md` now lists
  the vendored htmx asset beside the Go module licences.
- [docs/roadmap.md](docs/roadmap.md) — candidate item **visual-style** (panel
  visual refresh: typography, colour tokens, spacing, and component styling
  without behaviour changes). Starting reference:
  [docs/assets/selfpost-proof.html](docs/assets/selfpost-proof.html). No semver
  impact; explicit agreement required before coding, like other candidates.

## [1.0.1] - 2026-08-09

A documentation and packaging release: no change to the mail path, the
database, or the on-disk layout. Upgrading is a tag bump.

### Added

- `SECURITY.md` — how to report a vulnerability privately (GitHub private
  vulnerability reporting, `public@mixeme.ru` as fallback), which releases get
  fixes, and what is in and out of scope for a relay. No response time is
  promised. Without it a finder's default move is a public issue, which
  discloses a relay flaw to everyone the moment it is filed.
- [docs/plans/](docs/plans/) — one document per agreed extension: the optional
  inbound relay, the domain-admin role, and splitting the oversized `web`
  package. Each states scope, open questions, and what has to be true before
  coding starts. [docs/roadmap.md](docs/roadmap.md) is restructured around them
  as a 1.x+ tracker instead of a 2.x wishlist, and now says how to read it from
  outside the project: nothing in it is a commitment, there are no dates, and
  the stated order is a recommendation.

### Changed

- The panel's **Account** entry is now called **Settings** — nav link, page
  heading, browser title, and the operator guide. The route stays `/account`,
  so existing links and bookmarks are unaffected.
- The signed-in name in the panel's nav is now labelled `User:`, so it reads as
  the current account rather than as a stray word above the Settings link.
- [docs/product.md](docs/product.md) reframes the future line: the inbound
  relay and the domain-admin role are agreed **1.x+** extensions tracked in the
  roadmap and the plans, with the inbound relay targeting a MINOR bump by
  default and a 2.x major still possible pending implementation. Only items the
  roadmap still marks *candidate* need explicit approval before coding. It
  previously put the whole line behind a 2.x.x that nothing had committed to.
- [docs/security.md](docs/security.md) is now in English, matching the rest of
  the published docs — it is linked from the README table and from
  `SECURITY.md`, so a reader following either landed in Russian. Content is
  unchanged: same requirements, same accepted risks, same ADR. The reviewing
  model is no longer named in the text; the fact that a pre-release review ran,
  and its date, stay. The roadmap and the plans are in English for the same
  reason, and neither records the model assigned to an item any more.
- The README documentation table now points at `SECURITY.md` for reporting a
  vulnerability, and the `docs/security.md` row is renamed *Security design* —
  with two files a reader could reasonably call "security", the table said
  which is which only by accident. The roadmap row no longer calls the file
  internal and Russian, because it is neither. `development.md` lists
  `SECURITY.md` among the user-facing deliverables.
- `docs/development.md` records the decision on authorship: SelfPost is written
  by AI agents under a maintainer's direction and the project discloses that,
  so the `Co-Authored-By` trailers, the model routing table, and the agent
  rules file all stay. Written down to settle the question rather than have it
  reopened at each release.
- The two remaining Russian source comments are in English:
  `deploy/traefik/extract-cert.sh` (quote from spec 10.3) and
  `internal/app/sasl.go`, where the quotation from the closed plan is dropped
  rather than translated — rendered in English it restated the sentence it was
  attached to. The Cyrillic that remains is test data, where it is the point.

### Fixed

- The panel's static assets are served with a content ETag and
  `Cache-Control: no-cache`. They are embedded in the binary, so their
  modification times are the zero value and no `Last-Modified` was sent; with
  no validator at all the browser was free to guess how long to keep them,
  which is why a tab kept showing the previous favicon after the new mark
  shipped. Each asset is now hashed once at startup, so an unchanged one costs
  a bodyless 304 and a changed one is picked up on the next load. A browser
  that cached an asset *before* this release still has nothing to revalidate
  against, so that one copy has to be cleared by hand.

## [1.0.0] - 2026-08-09

### Added

- A **Delivery log** on each delivery's page (`/deliveries/{id}`): the
  `mail.log` lines Postfix wrote about that message, oldest first — the
  connection to the receiving server, its reply, and the status that reply was
  filed as. It is a table of two columns, when and what, so the seconds between
  the connection and the reply line up down one edge; the timestamp is the
  log's own wall clock without its microseconds and offset, and a line whose
  head is not a timestamp keeps its whole text under *Message*
  (`logtail.SplitTimestamp`, which reads both postlogd's format and syslog's).
  The queue id was printed on this page as something to go and search
  the system log for by hand; the search is done for the operator instead
  (`logtail.QueueLines`). The read is a bounded tail of the current log and
  matches only lines carrying this message's queue id, anchored so a shorter id
  is not found inside a longer one. Send-log rows outlive `mail.log` — retention
  is ninety days, rotation keeps fourteen files — so a message with no lines
  left says so rather than reporting a failure.
- A **History** block on the same page: the journal's two timestamps stated as
  the steps they stand for — accepted and queued, then delivered, deferred,
  bounced, or refused before queueing — each with the status it reached in the
  panel's own ok/warn/error/unknown badge vocabulary. A message still queued
  shows the delivery report it is waiting for as a step that has not happened.

### Fixed

- Postfix config: copy TLS cert/key from the (often `:ro`, host-owned) mount
  into `/etc/postfix/tls-internal` as `root:root` before `postconf` /
  `postfix check`. Bind-mounted keys owned by the CI/host UID made
  `postfix check` fail and the container exit before supervisord started.
- Postfix config: allow `maillog_file` under `/data` via
  `maillog_file_prefixes=/var,/dev/stdout,/data`, and pin the `postlog`
  master.cf service. After mail.log moved to `/data/log`, `postfix check`
  fatally rejected the path (default prefixes are only `/var` and
  `/dev/stdout`) and often left stderr/mail.log empty.
- E2e: read `/data/setup-token` via `docker compose exec` (file is `0600`
  panel-owned; host `ReadFile` got permission denied on CI). Reclaim `/data`
  ownership before TempDir/stage cleanup so panel/postfix UIDs do not fail
  Go's `RemoveAll`.
- Entrypoint: check `SELFPOST_HOSTNAME` before `/data` setup (so a bad identity
  fails with the FATAL text, not an earlier `set -e` abort), and `chmod 755
  /data` after chown so OpenDKIM/Postfix can traverse bind mounts that arrive
  as mode `0700` (Go `TempDir`, some host umasks) — otherwise KeyTable is
  unreachable and the container crash-loops. E2e stand uses `restart: "no"` and
  surfaces selfpost logs from the supervisor readiness check.
- E2e gate: wait for host-published `/healthz` before panel setup, and stop
  ordered `TestE2E` subtests after a failure so a nil panel client cannot panic
  and mask the real error (release CI on both amd64 and arm64).
- Release CI: retry `docker push` / `imagetools create` on transient GHCR
  `unknown blob` (and similar) errors after layers already uploaded.
- A send-log row could stay `queued` forever after the container was recreated.
  `mail.log` moved from the ephemeral `/var/log` into the data volume
  (`/data/log/mail.log`, `./data/log/` on the host), so the delivery lines that
  resolve a queued row now outlive the container the same way the journal does.
  `postlogd` writes the file as user `postfix` and the unprivileged panel reads
  it through the shared `selfpost` group (directory `2750`, file `0640`,
  re-normalised on every start); logrotate creates each new file the same way.
  Existing deployments need no action beyond the upgrade — the directory is
  created on first start — but the log written by the previous image is gone
  with its container, and the tailer starts the new file from its end.
- A row whose delivery lines are gone for good is no longer left `queued`
  indefinitely: every five minutes the tailer compares rows still queued from
  more than two minutes ago against `postqueue -p`, and marks `bounced` those
  whose message Postfix no longer holds — it will never report on them again.
  The sweep waits until the tailer has read the log to its end (on a restart the
  log itself holds the answer) and does nothing at all if the queue cannot be
  listed, so a message merely in flight, or a `postqueue` that fails, never
  closes a row.

### Changed

- Document what `.spbk` and `.spde` stand for (SelfPost backup / SelfPost domain
  export) in the operator guide, security notes, architecture, and the Backup /
  Export panel copy.
- Docs aligned with the code: setup URL is `/setup/<token>` (README and
  guide; local trial rewrites the printed `https://<hostname>/…` link to
  `http://127.0.0.1:8080/…`); domain import uses the file extension / magic
  bytes for the password field, not an "encrypted" checkbox; architecture
  layering and route table match `web`→`store` and `POST /domains/import`;
  OpenDKIM drops to `opendkim` via `UserID`; guide drops the archived
  "spec 7.5" pointer, clarifies `POSTFIX_SENDER_LOGIN_MAPS` vs panel writes,
  and states logrotate keeps 14 daily files. Intermediate CHANGELOG cuts from
  before the published `1.0.0` image are called out in the guide and
  `development.md`. Roadmap points at CHANGELOG `[0.5.0]` Security and
  refreshed `internal/web` size / symbol links.
- Full backups no longer carry `/data/log`. It is Postfix's raw log plus its
  fourteen rotated copies — diagnostic output rather than state to restore, and
  otherwise by far the largest thing in the archive.
- Monitoring screens (status, mail queue, system log, deliveries) use adaptive
  HTMX polling: 5 s while the operator is active on the page, 30 s when the tab
  is visible but idle, and no requests while the tab is hidden. Scheduling
  lives in `panel.js` (`data-poll` markers) instead of `hx-trigger="every …"`,
  which would need `unsafe-eval` under the panel's CSP.
- Documentation package consolidated into `docs/development.md`: Documentation
  map, user-facing deliverables, maintenance rules, and code-to-prose
  verification table (from closed `documentation-plan.md`); resuming work,
  model routing, commits, and phase closure (from closed `progress.md`).
  `docs/archive/` removed — history is git + CHANGELOG. README Documentation
  index lists operator docs plus the internal roadmap. Agent rules point at
  `development.md`.
- The delivery page is laid out in two columns: what the journal recorded on
  the left, what happened to the message on the right, and the delivery log at
  full width under both. The facts the page used to stack one per line — domain,
  application, queue id, journal id and the two timestamps — are a grid of tiles
  instead, since a page of mostly empty rows was what the full-width stack came
  to for six short values. The subject heads the page and the sender, recipient
  and outcome are the line under it, so what the message was and how it ended
  are both on the first line. The page takes the whole column rather than the
  reading measure, as the other three monitoring pages already did.
- `docs/development.md` restructured into stack, dependencies, build, release,
  testing, and CI; agent rules moved to `.cursor/rules/agent-rules.mdc`;
  dev-host-specific workflow and `example.com` references removed from docs.
- `docs/development.md` and `.cursor/rules/agent-rules.mdc` translated to
  English; `roadmap.md` remains Russian (internal tracker).
- Deploy pin and local-trial image tag set to `ghcr.io/mixeme/selfpost:1.0.0`
  (compose and git tag `v1.0.0` cut together). Retired
  `docs/implementation-plan.md` and `docs/v1.x-closure-plan.md`; Makefile,
  `release.yml`, and e2e comments point at `docs/development.md`. Roadmap
  v1.x documentation/deploy tail closed.

## [0.6.0] - 2026-08-08

### Added

- A page per delivery (`/deliveries/{id}`), reached from the *Details* link on
  every send-log row. It carries what the log itself no longer shows — the
  sending domain, the application the message was submitted under, the Postfix
  queue id to search the system log for, when the status was last reported —
  so the journal can grow fields without the table having to find columns for
  them. *Back* returns to the page and filters the row was opened from, rebuilt
  from the log's own parameters only.

- A **DNS** badge in the domain list, one per row, carrying the same
  ok/warn/error/unknown vocabulary as the rest of the panel: the worst of that
  domain's DKIM, SPF and DMARC checks, so a domain whose records were never
  published is visible without opening it. The badge links to that domain's
  DNS status card. The checks run concurrently across the listed domains —
  each carries its own timeout, and in series a dead resolver would multiply
  that wait by the number of domains — and share the checker's cache with the
  domain page, so a repeat view costs no lookups. A domain whose DKIM key
  cannot be read stays "unknown" rather than being reported as misconfigured,
  since the missing half is this server's.
- Machine metrics on the status page: a **Machine** card reporting the
  processor (busy percentage, core count, load average), memory and swap, and
  network throughput and totals per interface, read from the kernel's counters
  in `/proc` (`internal/health/machine.go`). CPU and throughput are differences
  between two readings, so they are measured against the previous poll of the
  status fragment and reported as still being measured until a second reading
  exists — a page opened after a long idle stretch re-baselines rather than
  presenting that stretch as the current load. A fully busy processor (≥90%)
  warns and an exhausted machine (≥97% of memory in use) errors, both counting
  towards the page's headline verdict, since either delays or kills the mail
  path; throughput is reported and never graded. Counters that cannot be read
  — no `/proc` outside Linux — leave the card in place showing "unknown". The
  usage bars are `<meter>` elements: the panel's CSP has no inline-style
  exemption, so a bar's length has to travel on an attribute.
- An index of the current page's own sections in the navigation column, for the
  two pages long enough to need one: the domain page (nine cards, from the DNS
  records to publish down to the danger zone) and the status page (eight). Each
  card carries an id and the page's template defines the list
  (`{{define "sections"}}`); every other page defines nothing and shows no
  index. `panel.js` marks the section currently in view, looking its targets up
  by id on each pass so the status page swapping its cards out every five
  seconds cannot leave it measuring boxes that have left the document. The
  links are plain fragment links and work with JavaScript blocked; only the
  highlight needs it.

### Changed

- Every panel page is laid out in one column of the same width, so moving
  between them no longer shifts the navigation and the cards sideways. The
  column used to be the 48rem reading measure, which the send log, the mail
  queue and the system log widened to 64rem for their tables — and since the
  navigation and the page are centred as a pair, that difference moved
  everything on screen on the way between two pages. The column is now 64rem
  throughout, with the reading measure kept inside it: a page's heading, cards,
  back link and version footer are held to 48rem and centred in the column,
  and the three pages made of data opt out and take the column whole. Which
  pages those are is declared by the page itself (a `wide` block in its
  template, the same mechanism as the section index) rather than derived from
  the navigation entry, so a single delivery's page — prose, but filed under
  the send log — keeps the measure. The scrollbar's width is now reserved on
  every page as well: without it a short page and a long one were laid out in
  viewports differing by that width, which moved the same things again.

- The mark's small-size variant — the tab icon and the `SP` initials it
  carries — sets its S in Medium where the wordmark sets it in ExtraLight.
  Against the P's SemiBold the ExtraLight S is a 0.90 stem against 3.40,
  which at 16px is a quarter of a pixel against most of one, so the pair
  rasterised to a P with a smudge beside it. Medium gives up the
  Self/Post weight play, which needs more pixels than this variant exists
  to work in, in exchange for both letters being there. The variants big
  enough to carry the contrast keep it. `favicon.png` is regenerated to
  match, and the outlines are IBM Plex Sans as before — the same
  font-size, letter-spacing and baseline, with only the S's weight moved.
- The stamp's `SELF-HOSTED SMTP RELAY` line is set at 11.5/0.15 instead of
  7.2/2.8, and no longer carries `opacity=".78"`. At the old size its stems
  rasterised to about half a device pixel, so more than half its ink landed
  as antialiasing — the typical pixel reached 2.1:1 against the brown rather
  than the 7.3:1 the two colours are worth, and none reached full strength.
  The line keeps its footprint and its monospaced cells: the width the
  tracking was spending went to the glyphs, whose cap height rises from 5.2
  to 8.3. The mark is used at 330px on the login and setup pages, which is
  where this was worst. `internal/web/static/logo.svg` is a copy of
  `docs/assets/selfpost-stamp.svg` and both carry the change, as does the
  proof sheet the outlines are drawn from.
- The delivery log lists what identifies a message and nothing else: time,
  sender, recipient, subject, status. Domain and application, which were a
  column each, remain the log's two filters and now appear per message on the
  delivery page. The two dropped columns were the widest thing in the table
  after the addresses, and both repeat down the page whenever a filter is set.
- Subjects are decoded for display as well as on the way in, so the rows the
  journal-milter recorded before it decoded them itself — the ones an operator
  is most likely to still be reading — show their text rather than
  `=?utf-8?Q?…?=`. The decoder moved to `internal/mailhdr` and is shared by the
  milter and the panel; it is idempotent, so a row decoded once passes through
  unchanged.
- The panel's navigation is a column down the left edge instead of a bar across
  the top. As a bar it did not fit on one row — six page entries and the
  session block against the panel's width — and had to be split into two,
  costing the top of every page; standing it up removes the compromise, gives
  the entries one left edge to scan down, and leaves room under them for the
  section index above. It is sticky, so both lists stay in view on the long
  pages, and the current entry is marked down its leading edge rather than
  underlined. Below the width the two columns need, it lies back down into the
  wrapping rows it used to be — no drawer and no hamburger, since six entries
  fit. The markup now lists the blocks in the order they are drawn, so the tab
  order follows the eye instead of starting at Sign out.
- An application's mode and rate-limit fields open under its row of controls
  instead of inside it. Both panels were `<details>`, so each opened where its
  own toggle sat and cut the row of four in half, pushing New password and
  Delete below a block of fields — the buttons moved every time a panel was
  opened or closed. The toggle is now a hidden checkbox with its label drawn as
  the button, and the panel is the last child of the row, so the four controls
  keep their places and what a panel reveals is laid out beneath all of them.
  It stays keyboard-reachable and, being pure CSS, still works with JavaScript
  blocked, as the disclosure did. Inside a panel the submit buttons take the
  ordinary form spacing back from the compact row style that was leaving them
  flush against the field above, and Save limit and Remove limit — two posts,
  hence two forms — share one row, the first button bound to its form by the
  `form` attribute rather than by sitting inside it.
- The mark at the head of the navigation column takes the column's full width
  instead of the 110px it kept from the bar. In a row that size was all there
  was room for; in a column it left the mark ending halfway across, with no
  edge shared with anything below it. At the column's width its edges line up
  with the page entries under it, as the full mark already does with the card
  beneath it on the signed-out pages. Where the column lies back down into a
  bar it returns to the compact size, which is what fits beside the entries.
- The import card reads the file's extension instead of asking whether the file
  is encrypted. The checkbox was a question the server never consulted — it
  decides from the envelope's magic bytes — so the answer could only be wrong.
  Choosing a `.spde` file reveals the password field and a `.json` file hides
  it; an unrecognised extension reveals it, and with no file chosen the field
  stays hidden, since there is nothing yet for a password to open. With
  JavaScript blocked the field is shown, so an encrypted import still works.

## [0.5.0] - 2026-08-06

### Fixed

- A bounce could be recorded as a successful delivery. The log-tailer's
  delivery-line pattern matched `status=` greedily, so it took the *last*
  occurrence on the line — and Postfix appends the remote server's reply
  verbatim, which the far end controls. A rejection whose reply text contained
  `status=sent` was filed as `sent` in the send log. The pattern now takes the
  first `status=` after the recipient, which is the real field
  (`internal/logtail/logtail.go`); found while extending `TestParseDelivery`.
- Log-tailer resumes where it stopped instead of jumping to end-of-file on
  every start (phase 3, `docs/code-review.md`): the read position and a
  fingerprint of the log's head are persisted (`logtail_state`, migration
  `0003`), so delivery lines written while the panel was down are parsed and
  their send-log rows no longer stay `queued` forever. A log that changed
  identity while the panel was down is read from the start; a first-ever start,
  with nothing stored, still begins at the end. Container recreate remains a
  gap — `mail.log` is not in `/data` (`docs/security.md`).
- Level-2 rate limit no longer overshoots under concurrency: messages that
  passed the check at MAIL FROM but have not reached the send log yet are
  counted alongside the stored rows (`internal/milter/inflight.go`), so
  parallel SMTP sessions cannot each spend the same last slot. Slots are
  released at end-of-message, on ABORT, and after a 10-minute TTL, so a client
  that drops mid-transaction cannot hold one — the limiter stays fail-open.

### Changed

- The project has a single public home: `github.com/mixeme/selfpost`. Codeberg
  is being retired, so the Go module path moved with it — `go.mod`,
  `test/e2e/go.mod`, every import, the `Makefile` `MODULE` variable and the
  `-ldflags` version stamp in `build/Dockerfile` and `docs/development.md`. An
  import path pointing at a host that is going away would break `go get` and
  `go install` outright, which is why this is not only a documentation change.
  README no longer lists a primary/mirror pair.
- Code comments no longer cite the archived specification. References like
  "spec 7.6.1" or "spec 5.1" pointed into `docs/archive/specification-v1.0.md`,
  which is explicitly not a source of truth; each is now a reference to the
  live document that owns the subject — `docs/architecture.md` (with section),
  `docs/product.md`, `docs/security.md`, or the README. Comments only; no
  behaviour is affected.
- `docs/code-review.md` is gone. Its plan is finished — phases 0 (bar the
  release-commit steps), 1, 1.5, 2 and 3 are all closed — and the rest of the
  document had become a second copy of what `architecture.md`, `security.md`
  and the code comments already say. What was genuinely open moved to
  `docs/roadmap.md`: splitting `internal/web` into subpackages, a consolidated
  documentation index in the README, the adaptive polling interval for an idle
  but visible tab, and `CONTRIBUTING.md`. The review text stays in git history
  (`522425a`); the CHANGELOG entries below that cite it are left as written.
- `docs/architecture.md` gained a *Code layers* section: a diagram of
  handlers → services → store plus the adapters, and the reason the services
  layer exists (multi-store writes and their rollback) — closing item A2 of
  `docs/code-review.md`.
- Phase 1 doc/code hygiene (`docs/code-review.md`): removed ~30 stale
  "Phase N" / historical-staging references from code and shell-script
  comments (`cmd/panel`, `internal/*`, `build/*`) now that v1.0 is done;
  fixed a stale dashboard comment (`internal/web/handlers_domains.go`)
  claiming applications/send-log were unimplemented; added a CSRF ADR to
  `docs/security.md` (why Origin-check, not tokens); resolved `docs/logo` in
  `docs/roadmap.md` (directory doesn't exist, criterion already met); added a
  `gofmt -l` check to CI (`.github/workflows/test.yml`).
- Phase 2 GUI polish (`docs/code-review.md`): the monitoring pages stop
  polling while their tab is hidden — the skip is done in an
  `htmx:beforeRequest` listener (`internal/web/static/panel.js`) rather than
  htmx's own trigger filter, which is evaluated with `new Function` and would
  be blocked by the panel's CSP. Dark mode is now a single reassignment of CSS
  custom properties under `prefers-color-scheme: dark` instead of a cascade of
  `!important` overrides, and the duplicate `main { max-width }` rule is
  consolidated into one base rule with documented per-page overrides
  (`internal/web/static/panel.css`).

### Security

- Pre-release security review (plan § D, model Fable, 2026-08-06): full pass
  over the diff from the v1.0 audit (Phase 11, `bd64e80`) to HEAD plus the
  complete spec 7.6 checklist. No exploitable findings; one defence-in-depth
  fix below. Accepted risks in `docs/security.md` unchanged.
- `saslpasswd2` argv: the application login is now passed after a `--`
  end-of-options marker (`internal/app/sasl.go`), so a login starting with
  `-` (legal under the whitelist) can never be parsed as a flag by getopt.

### Added

- Optional password encryption for the two secret-bearing downloads (plan
  phase 1.5, `docs/code-review.md`): an *Encrypt with a password* checkbox on
  the full-backup and domain-export forms writes a `.spbk` / `.spde` envelope
  instead of the plain `.tar.gz` / `.json` — scrypt key derivation and
  AES-256-GCM over 64 KiB chunks, each authenticated with the header, its
  counter and an end-of-stream flag, so a truncated or altered file refuses to
  open (`internal/secretfile`). Unticked, both downloads are byte-for-byte what
  they were.
- Domain import accepts an encrypted export: the envelope is detected by its
  magic bytes, and a password field appears next to the file picker
  (`internal/web/handlers_backup.go`, `templates/encrypt_fields.html`).
- `selfpost-backup` writes encrypted archives and reads them back:
  `-decrypt` (with `-i`/`-o`) turns a `.spbk` into the plain `.tar.gz` a
  restore unpacks. The password comes from `SELFPOST_BACKUP_PASSWORD` or
  `-password-file`, never from argv.
- `TestParseDelivery` covers the exotic mail.log shapes the review asked for
  (`docs/code-review.md` § 3): a `status=` quoted inside the remote reply, the
  null recipient of a double bounce, `orig_to=` alongside `to=`, an
  unrecognised status word, a capitalised one, and a cleanup line.
- docs: README *Encrypting a backup or export*; `docs/security.md` §
  *Backup and domain export* + accepted risk (encryption is opt-in);
  `docs/architecture.md` persistence § envelope summary.
- docs: `docs/roadmap.md` v1.x tail — retire `implementation-plan.md` in the
  release commit (move to `docs/archive/`, retarget its references in README,
  docs, Makefile, release workflow and the e2e test comment).
- docs: `docs/code-review.md` — phase 1.5 plan for optional password encryption
  of full backup (`.spbk`) and domain export (`.spde`); checkbox UI pattern;
  remove session-resurrection-from-backup as accepted risk.
- docs: `docs/code-review.md` — full codebase review (architecture, code quality,
  documentation, GUI, legacy, risks) with prioritized implementation plan and
  model routing; cross-links in `implementation-plan.md` and `progress.md`.
- docs (D6): Docker `HEALTHCHECK` probes `/healthz`; endpoint returns 503 unless
  opendkim, panel, and postfix are RUNNING (`internal/health.Liveness`).
- docs (D6): README *Container health* — scope of `/healthz` vs authenticated Status.
- docs (D7): `cmd/panel/envdoc_test.go` — regression test that every
  `loadConfig` and build-script env key is listed in README documentation.
- docs (D8): `docs/architecture.md` — as-built processes, mail path, routes,
  persistence (verified against code).
- docs (D8): `docs/development.md` — local Go workflow, `make e2e`, dev-server
  loop, commit/CHANGELOG protocol, agent rules.
- docs (D9): `docs/product.md` — product purpose, assumptions, out-of-scope,
  multi-domain model.
- docs (D9): `docs/security.md` — self-contained mandatory security checklist
  (former spec §7.6).
- docs (D9): `specification.md` archived to
  `docs/archive/specification-v1.0.md`; live docs updated (`progress.md`,
  `implementation-plan.md`, `roadmap.md`, `documentation-plan.md`).
- docs (D1): README *Operations* — panel screens (`/status`, domains,
  deliveries, mail queue, system log, backup, account), upgrade procedure,
  session behaviour (sliding idle, monitoring polls do not extend, password
  change signs out other sessions), and `mail.log` rotation cadence.
- docs (D1): README *Rate limiting* — level-1 anvil limits
  (`RATE_LIMIT_MESSAGES_PER_IP`, `RATE_LIMIT_WINDOW_SECONDS`) and level-2
  per-domain/application limits from the panel; fixes the `.env.example` link
  that pointed at a missing section.
- docs (D2): README environment-variable reference — public `.env` table with
  code-accurate defaults, `TRUSTED_PROXY_CIDR` security note, explicit
  internal-variable list; `TRUSTED_PROXY_CIDR` wired through
  `deploy/docker-compose.yml`.

### Removed

- docs: `security.md` — accepted risk «restore old backup revives session rows»
  (not a concern in operator deployment).

### Changed

- docs: `implementation-plan.md` trimmed to the sole open v1.x gate — pre-release
  security review (§ D); closed B.1–C.4 material moved to as-built and ops docs.
- docs: `architecture.md` — sessions (SQLite, idle renew, password change),
  `mail.log` rotation (rename + `postfix reload`), `SELFPOST_HOSTNAME` startup
  gate, log-tailer known gaps.
- docs: `development.md` — expanded e2e stack and `release.yml` matrix workflow.
- docs: `security.md` — accepted risk for send-log rows stuck at `queued` after
  panel restart or container recreate.
- docs: `roadmap.md` — optional send-log / `mail.log` follow-ups under v1.x tail.
- docs: `progress.md`, `documentation-plan.md` — cross-links updated for the new layout.
- docs: `documentation-plan.md` marked closed (D1–D9); trimmed to package
  checklist, code-verification method, and ongoing maintenance rules.
- docs: `roadmap.md` — v1.x doc/deploy tail (Codeberg Quick start, compose
  image tag at release, `docs/logo`); archived-spec references replaced with
  `product.md` / `security.md` / `development.md`.
- docs: `progress.md` — documentation pass closed; deferred polish in roadmap.
- `/healthz` now checks supervisord mail-path processes, not HTTP alone.
- `build/Dockerfile`: `curl` for `HEALTHCHECK`; probe on port 8080.
- docs (D3): README backup — stopped-container `tar` of `./data` (with live-container
  WAL warning), `manifest.json` consumed after a matching restore.
- docs (D4): README status banner (v1.0 implemented, links to open questions and
  documentation pass); new *Published ports* note for 587; compose usage comment
  corrected (TLS via `./certs` bind mount, not `.env`).
- docs (D5): `implementation-plan.md` B.1 — password change signs out other
  sessions only (implementation diverged from original plan; README was already
  correct).
- docs: documentation plan now targets retiring `specification.md` after D9 —
  migration map to `product.md`, `architecture.md`, `development.md`, and
  expanded `security.md`; D9 added to the release gate.

## [0.4.0] - 2026-08-04

### Added

- The project's mark is now in use rather than only on file. The README opens
  with the full stamp; the panel carries the compact one at the left of its
  navigation bar, linking to the status page, and the full one above the card
  on the two pages that have no navigation — sign-in and first-run setup. The
  browser tab icon changes with it, from the earlier envelope drawing to the
  stamp's small-size variant, so the tab, the panel and the README are one
  identity. The four brand files in `docs/assets/` had their wordmark converted
  from live text to outlines: they were set in IBM Plex Sans, which is not
  installed on the machines that render them, and the light/semibold contrast
  between *Self* and *Post* — the whole of the mark — collapsed into whatever
  fallback the viewer happened to have.

### Changed

- panel: the sign-in and setup pages are now a column the width of their own
  card. Both are a single narrow card, which centred itself while the heading
  above it stayed at the panel's left edge; adding the mark would have made
  that three alignments on a page with four elements.

- panel: the three monitoring pages — Deliveries, Mail queue, System log — are
  now laid out wider (64rem against the 48rem the rest of the panel keeps).
  They carry data rather than prose: the send-log's seven columns had no room
  to breathe, and the raw `mail.log` lines wrapped every second line.

### Fixed

- panel: Deliveries now shows the subject as text rather than as its MIME
  encoding. A subject in any non-Latin alphabet reaches the milter as RFC 2047
  encoded-words (`=?utf-8?Q?=D0=9F…?=`), and the panel printed that verbatim —
  unreadable, and as one unbreakable run wide enough to push the Status column
  outside the card. Subjects are decoded when the message is journalled and
  capped at 200 characters; the column clips anything still too long to one
  line, with the full text in the tooltip. Rows logged before this release keep
  their raw string. Subjects in the legacy single-byte charsets (windows-1251,
  koi8-r) are still stored as sent — there is no decoder for them.

- panel: table cells may now break inside a word, so no single long value can
  push a table past the edge of its card. A 40-character recipient address did
  it just as readily as an undecoded subject: a column is at least as wide as
  the longest unbreakable run it holds, and email addresses have nothing to
  break on. Timestamps are exempt and stay on one line.

- panel: the Applications list on a domain page no longer comes apart. It was a
  four-column table whose last column held six controls, two of them expanding
  panels with textareas — far more than the width of a column, so the controls
  broke into a staircase, the login cell grew into a block as tall as the row,
  and the two text columns were left stranded on the baseline halfway down it.
  An application is now a block rather than a row: the login on one line, mode
  and addresses on the next, and the controls in a single wrapping row, with an
  opened panel claiming the full width for its fields.

## [0.3.0] - 2026-08-03

### Fixed

- panel: the PTR (reverse DNS) check no longer reports a correctly published
  record as wrong. The checks went through the container's own resolver, which
  forwards to the host's systemd-resolved — and systemd-resolved answers the
  reverse lookup of the machine's own IP from the local hostname instead of
  asking public DNS. A server with `203.0.113.10 → selfpost.example.com` in DNS
  was told its PTR pointed at the provider-assigned hostname. All four
  deliverability checks (PTR, SPF, DKIM, DMARC) now query recursive resolvers
  directly, so the panel reports what a receiving mail server actually sees.
  Set `SELFPOST_DNS_RESOLVERS` if outbound port 53 is closed or you run your
  own recursor; it defaults to 1.1.1.1, 8.8.8.8 and 9.9.9.9.

### Changed

- panel: the three monitoring pages now live at URLs that match their nav
  labels — Deliveries at `/deliveries` (was `/sendlog`), Mail queue at
  `/mail-queue` (was `/queue`), System log at `/system-log` (was `/logtail`).
  Bookmarks to the old paths stop working.

- panel: each entry in the navigation bar now carries an icon beside its label,
  so the bar is scannable at a glance instead of a row of similar-length words.
  The icons are inline SVG drawn in the entry's own colour — no extra request,
  no exemption from the panel's Content-Security-Policy — and are hidden from
  screen readers, which still announce the label alone.

- panel: the navigation bar is laid out as two rows on purpose — the signed-in
  user, Account and Sign out along the top right, the page entries below. It no
  longer fits on one line and used to wrap on its own, which left the session
  block sitting left-aligned under the entries as if it were more navigation.

## [0.2.0] - 2026-08-03

- panel: every authenticated page now ends with the running version
  (`SelfPost 0.2.0`) in a small footer. It is the value a backup manifest is
  checked against on restore, and the first thing to establish when the panel
  behaves unexpectedly. The login and setup pages deliberately do not show it.

- panel: the domain page now shows the **SPF and DMARC records it expects**,
  with host, value and a Copy button, next to the DKIM record it already
  showed — previously it only said "also configure SPF and DMARC (see the
  documentation)" and the concrete example appeared only once a check had
  already failed. The SPF value names the addresses this server's hostname
  resolves to (falling back to an `a:` mechanism if it does not resolve), and
  the DNS checks below build their remediation advice from the same source, so
  the page and its checks cannot recommend different records.
- panel: one appearance for actions. Several controls — a POST wrapped in an
  inline form (Re-check, Export domain, Sign out, New password…), the
  `<details>` toggles in the applications table, the delete links — used to
  render as bold blue text while everything else was a button, so the same
  kind of control looked like two different things, sometimes within one card.
  They are all buttons now: filled for a card's own action, compact and
  outlined where actions cluster in a table row or the nav bar. The two
  actions that are really navigations — "Delete domain" and the status page's
  "Full queue" — are anchors carrying the same button styling. A bare link is
  left only where it reads as part of a sentence, a table cell or the nav.
- panel: on the domain page **Add an application** now sits directly above the
  **Applications** list — the same order the domains page uses for its own add
  form — instead of being stranded below the domain rate limit.
- ci: hermetic container e2e suite (`test/e2e`, a separate Go module) gates
  image publishing — `make e2e` locally, and `go test ./...` in `test/e2e` as
  a required step in `release.yml` before a version tag's image is pushed.
  It builds the real image, brings up the shipped `deploy/docker-compose.yml`
  plus a test-only override (self-signed cert, low ports, a fake DNS zone
  served by CoreDNS, a `smtp-sink` sink-MX) on an isolated compose project,
  then drives the panel over HTTP exactly like an administrator: setup →
  login → add a domain → publish the DKIM record it prints into the fake zone
  → add an application → send over SMTP AUTH → verify the delivered message's
  DKIM signature against the record the panel published → poll the send log
  to `sent`. Negative coverage: no-AUTH and unauthenticated-relay rejection,
  sender/login mismatch, the level-1 (anvil) and level-2 (panel-configured)
  rate limits, the journal-milter's fail-open behaviour when the panel process
  is stopped, a missing/malformed `SELFPOST_HOSTNAME` failing the container
  fast, and a login session surviving `docker restart`. `release.yml` moved
  off qemu to a native per-architecture build (`ubuntu-latest` /
  `ubuntu-24.04-arm`), each gated by this suite before its tag is pushed and
  merged into the version manifest — running the full Postfix/OpenDKIM stack
  under emulation for the gate was impractically slow.
- ops: `mail.log` rotation switched from `copytruncate` to rename +
  `postfix reload` (the same mechanism `postfix logrotate` itself uses),
  eliminating the up-to-one-second window in which `copytruncate` could drop
  in-flight delivery lines — a lost line meant a send-log row stuck at
  `queued` forever. `logrotate-mail.conf` keeps `create 0644 root root`
  rather than `nocreate`: verified on a live container that letting Postfix
  recreate the file itself on reload produces `0600`, which the unprivileged
  panel process cannot read, breaking the mail-log view until the next
  restart. The panel's log-tailer (`internal/logtail`) re-drains the old file
  descriptor once more right before switching to the rotated one, closing a
  similar small window between polls; a missing `mail.log` right after
  rotation is now a normal empty screen rather than a logged error.
- panel: login sessions now persist in SQLite instead of memory, so an
  administrator's login survives a container restart or redeploy. Only the
  SHA-256 of the session token is stored, never the token itself. The
  absolute 12-hour TTL is replaced by a **sliding idle timeout**
  (`PANEL_SESSION_IDLE_DAYS`, default 7 days, no absolute cap): the
  monitoring screens' background polling does not count as activity, so a
  forgotten open tab does not keep a session alive forever. Changing the
  password still signs out every other session.
- panel: security headers on every response — `Content-Security-Policy`,
  `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, and
  `Strict-Transport-Security` where the deployment is HTTPS-only. They are
  emitted by the panel itself, so the reverse proxy still needs no security
  configuration of its own.
- panel: state-changing requests are now checked against the panel's own
  origin (`Sec-Fetch-Site`, falling back to `Origin` vs `Host`). This closes
  cross-site request forgery from a *neighbouring host on the same domain* —
  a CMS or a forgotten staging subdomain next to the panel — which the session
  cookie's `SameSite=Lax` counts as same-site and therefore cannot stop. A
  request that sends neither header is still let through, so genuinely ancient
  browsers keep working. **The reverse proxy must pass the original `Host`
  header through** (every shipped fragment already does); one that rewrites it
  makes the panel refuse every form submission, and the log line names both
  the `Origin` and the `Host` it compared.
- panel: the session cookie is now named `__Host-selfpost_session` wherever it
  is `Secure` (the standard deployment), which makes the browser enforce that
  no other host can set or overwrite it. **Upgrading signs the administrator
  out once.** With `PANEL_COOKIE_SECURE=false` the old name is kept, because
  the prefix is invalid without TLS. Signing out clears both names.
- panel: if a request arrives with two cookies of the session cookie's name —
  what a neighbouring host does when it overwrites the session — the request
  counts as signed out and the log says so, instead of the panel silently
  picking the other host's value and looping back to the login form forever.
- panel: the layout's stylesheet moved to `/static/panel.css` and the
  confirmation prompts on destructive buttons moved into `/static/panel.js`.
  No visible change; the panel's CSP allows no inline script or style, and
  this is what keeps that policy free of exemptions.
- docs: the first-run setup link is also written to `/data/setup-token`
  (`0600`) — documented in the README as the way to read it without the token
  passing through a container-log pipeline.
- panel: new **Status** page — supervised processes, mail queue, TLS
  certificate expiry, milter sockets and the server's own hostname/reverse-DNS
  (FCrDNS) check — and it is now the panel's landing page. The local checks
  refresh by polling; the DNS lookup is cached with a *Re-check* button.
- panel: the domain page shows a **DNS status** card: the published DKIM record
  compared against the key this server actually signs with, plus SPF and DMARC.
  The SPF check is deliberately shallow — it looks for a mechanism literally
  covering this server's address and does not follow `include:`/`redirect=`, so
  a record that authorises the server through an include is reported as "cannot
  tell", not as a failure.
- panel: the domain list moved from `/` to `/domains`; `/` redirects to the
  status page. The **Reload** button moved from the domain list to the status
  page and now explains what it regenerates and when to use it.
- fix: the panel could never read the mail queue in the documented deployment.
  `postqueue` relies on its setgid-`postdrop` bit, which `no-new-privileges`
  (set in the shipped compose file) disables, so the *Queue* screen always said
  "Could not read the mail queue". The `panel` user is now a real member of
  `postdrop`.
- panel: navigation bar is now rendered once from the shared layout, so every
  authenticated page has it — including the domain page and the delete
  confirmation, which had no navigation links at all — and the current page is
  highlighted instead of silently missing from the list.
- panel: new *Account* page to change the administrator's username and/or
  password (the current password is required, throttled on the same limiter as
  the login form). Changing the password invalidates all other sessions.
- panel: *Backup & migration* moved off the domain list onto its own *Backup*
  page, with the full backup and the domain import as two separate cards.
- panel: the domain page now shows the *Sending server settings* (server,
  port and encryption) needed to configure a mail client; port 587 is listed
  only when `SUBMISSION_ENABLE=true` for this deployment.
- panel: *Copy* buttons on the DKIM record, on a newly issued application
  login/password and on the sending server name.
- panel: the *Addresses* field is hidden while an application's address mode is
  *Any address of the domain*, where the server ignores it.
- ci: disable provenance attestation on release image push, so the ghcr.io
  manifest list shows only `linux/amd64`/`linux/arm64` (no `unknown/unknown`).
- ci: run `go vet` and `go test ./...` on every push to `main` and every pull
  request, not only the image build on a release tag.
- security: optionally honour `X-Forwarded-For` for login/setup rate-limiting
  when the request's direct peer is in the new `TRUSTED_PROXY_CIDR` list,
  giving real per-client limits behind a reverse proxy instead of one global
  bucket. Unset by default (unchanged `RemoteAddr`-only behaviour).

## [0.1.0] - 2026-07-15

Initial feature-complete implementation of the v1.0 specification (phases 0-11
of `docs/implementation-plan.md`).

### Added

- Panel (Go, single static binary) with SQLite persistence, one-time
  crypto-random setup link, bcrypt admin auth, session cookies.
- Domain management with per-domain DKIM (RSA-2048, generated in pure Go) and
  OpenDKIM KeyTable/SigningTable regeneration + privilege-safe reload.
- Application (sender identity) management: SASL credentials via `sasldb2`,
  `smtpd_sender_login_maps` enforcing sender/domain ownership, no open relay.
- Full Postfix relay config generated from env at container start: SMTPS 465,
  optional STARTTLS submission 587, SASL auth, TLS for outbound delivery,
  anvil-based rate limiting (level 1).
- Journal milter (pure Go, `go-milter`) recording every send to `send_log`;
  fail-open by design so a milter fault never blocks mail.
- Monitoring UI: send log, Postfix queue, and mail.log tail, all
  HTMX-polling, HTML-escaped.
- Per-domain/per-application sending rate limit (level 2), enforced in the
  journal milter at `MAIL FROM`, fail-open on the limiter's own errors.
- Full backup/restore (`tar.gz` of `/data`, consistent SQLite snapshot via
  `VACUUM INTO`) with a version guard that refuses to start on a
  manifest/binary version mismatch. Per-domain export/import for moving a
  single domain between hosts without re-issuing DNS records.
- Deployment: Docker image + compose, reverse-proxy fragments for Apache
  (default), nginx, Caddy, and Traefik; CI workflow publishing tagged,
  multi-arch images to `ghcr.io` on `vX.Y.Z` tags.
- Security pass against spec 7.6 (exec safety, config-write sanitization,
  server-side validation, rate limiting, session/cookie hardening, output
  escaping, non-root panel) — full compliance, no code changes required.
- Live production deployment with a real Let's Encrypt certificate;
  end-to-end delivery confirmed (DKIM pass, SPF pass).
