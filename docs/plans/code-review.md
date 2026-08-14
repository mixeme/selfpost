# Plan: code-review (full-tree review follow-ups)

**Status:** agreed  
**Date:** 2026-08-13  
**Reviewer:** Cursor Grok 4.6 (whole-tree authorship review). This is **not**
the Fable pre-release security audit in [development.md](../development.md) §
Model routing; a Fable pass is a later step on the P0 diff.  
**Version:** patch for defects; docs/UI follow-ups have no schema.  
**Order:** **P0 before inbound-relay.** P0 is a shipped RBAC hole, not a
feature. Remaining phases after P0, or interleaved with inbound-relay by
agreement.

---

## Goal

Record the 2026-08-13 full-tree review (architecture, complexity, quality,
docs, maintainability, logic, refactor, licence, legacy, stubs, GUI, disputed
decisions, edge cases, tests, duplication) and a phased implementation
checklist with a recommended model per step, using the routing table in
[development.md](../development.md).

---

## Verdict

SelfPost is a compact, well-bounded 1.x product: one image, one SQLite file,
thin `domain`/`app` services for multi-store writes, a fail-open journal
milter with a Postfix level-1 backstop, and documentation that is unusually
honest about accepted risks. Complexity matches the scale (~10.7k production
Go lines, ~5.8k unit-test lines, ~1.2k HTML, 749 CSS, 281 JS). Comments
explain decisions rather than restating code.

The outstanding defect is **send-log authorization for domain administrators**
(confidentiality). After that, the work is tightening a few fail-open paths,
catching docs that froze at “single administrator”, small GUI bugs from the
1.2.x layout pass, and filling test gaps around auth/RBAC. Do **not** use this
review as a licence to rewrite layers, squash migrations in 1.x, or add CSRF tokens
without revisiting the ADR.

---

## How to read this file

Findings are grouped by the sixteen review questions. Each finding has a
severity (**H**igh / **M**edium / **L**ow / **I**nfo). The implementation
checklist at the end is the work queue; it names the model for each step.

**Models** (from [development.md](../development.md)):

| Kind of work | Model |
|---|---|
| Security, infra, mail path, permissions, open-relay risk | **Opus** |
| UI / JS / CSS, templates, documentation (English) | **Sonnet** |
| Trivial mechanics: retarget links, grep, compose bump, comment fixes | **Haiku** |
| Security **review** of a diff (not authorship) | **Fable** |

Reviewers must not be the author of the code under review.

---

## 1. Architecture / structure

**Proportionate.** Composition root in `cmd/panel` (HTTP + journal-milter +
log-tailer, one `*store.Store`). `internal/domain` and `internal/app` own
multi-store writes and rollback. Adapters (`postfix`, `milter`, `logtail`,
`dnscheck`, `health`, `backup`, `secretfile`) are the only infrastructure-aware
code. Interfaces exist where they break import cycles or enable fakes
(`domain.Applications`, `app.SenderMaps`, `milter.Store`, `logtail.StatusStore`)
— not as a DI framework.

Handlers may call `store` directly for single-table reads (documented in
[architecture.md](../architecture.md) § Code layers). That is followed for
sessions, send-log queries, users, and DMARC settings. It is not a layering
violation; it is an incomplete service boundary that will hurt if those
surfaces keep growing.

No circular Go imports. `MaxOpenConns(1)` on SQLite is an intentional
single-connection trade-off for the three in-process roles.

| Sev | Finding |
|---|---|
| **M** | Users, send-log listing, and global DMARC settings have no service; handlers talk to `store`. Fine at current size; do not invent a service until a second writer appears. |
| **L** | `auth.RequireGlobal` (`internal/web/auth/middleware.go`) is unused; handlers duplicate `requireGlobal`. Either wire the middleware on `/users`, `/backup`, `/status`, `/mail-queue`, `/system-log` or delete the unused helper. |
| **L** | `HandleAccount` / `handlers_account.go` still use the pre-1.2.3 “account” name while the route is `/settings`. |
| **I** | Package comment on `internal/store` still says “the administrator account” after migration `0005` replaced `admin` with `users`. |

**Do not:** introduce a repository layer, split the panel binary, or move
SQLite behind an interface “for testability” — the existing fakes are enough.

---

## 2. Complexity vs project scale

The code is **not over-engineered**. A few files are large because the problem
is large, not because of unused abstraction:

| File | ~Lines | Note |
|---|---|---|
| `internal/health/machine.go` | 622 | Cohesive `/proc` sampler |
| `internal/logtail/logtail.go` | 528 | Follow + rotate + reconcile + retention |
| `internal/web/handlers/handlers_monitor.go` | 481 | Send-log UI + authz (this is where P0 lives) |
| `internal/web/view/templates/domain_detail.html` | 476 | DNS + apps + limits + export; composition debt |
| `internal/secretfile/secretfile.go` | ~423 | Isolated crypto envelope |
| `internal/web/view/static/panel.css` | 749 | Tokens + layout; comment-heavy by design |

Comments are long and mostly load-bearing (threat, fail-open, why not the
obvious alternative). The cost is scanability: some files are 30–40% prose.
That matches the project’s disclosed AI-authorship style
([development.md](../development.md) § Authorship). Do not strip comments in
the name of “cleanup”. Update the stale ones (see §4).

---

## 3. Code quality

Naming matches the docs (`domain` / `application`, level-1 / level-2). Errors
on the mail path log-and-continue (intentional fail-open). Panel paths log and
return 4xx/5xx. `crypto/rand` failure panics in `auth/token.go` — acceptable.

Context is used for process lifetime and DNS timeouts, not for SQLite (correct
with one connection). Dashboard DNS checks write distinct `rows[i]` from
goroutines; Go 1.22+ loop semantics make that safe (`go.mod` is 1.26).

Magic numbers are mostly named (`reservationTTL`, `renewThreshold`, CSP/HSTS).
Env defaults live in `loadConfig`.

| Sev | Finding |
|---|---|
| **M** | `sessionStore.Create` logs a DB error and still returns the token (`internal/web/auth/session.go`). Login sets the cookie; the next request bounces to `/login`. Fail closed: no cookie, error page. |
| **M** | `app.Service.Delete` removes the registry row **before** SASL / rate-limit cleanup. SASL failure → orphaned `sasldb2` account that can still authenticate. Domain delete does SASL first (`domain/service.go`). Align app delete with that order (or compensate: restore the row on SASL failure). |
| **L** | Login/setup `rateLimiter` sweeps expired buckets only when creating a **new** key. Many unique IPs grow the map until restart. Cap the map or sweep on a timer. |
| **L** | `parseTrustedProxies` skips invalid CIDRs instead of refusing to start. Silent misconfiguration of `TRUSTED_PROXY_CIDR`. |

---

## 4. Documentation completeness vs code; comments

Docs are a first-class artefact (env regression test, architecture as-built,
security accepted-risks). The drift is concentrated where **domain-admin
shipped in 1.2.0** and several files still argue “single-user”.

| Sev | Finding |
|---|---|
| **M** | [guide.md](../guide.md) Operations never mentions **Users** (`/users`) or the domain-admin role. Architecture and product do. An operator reading only the guide does not know the panel is multi-user. |
| **M** | [security.md](../security.md) CSRF ADR still says the panel is single-user and “revisit if multi-user”. Multi-user shipped. The origin-check decision can stand; the **rationale and revisit trigger must be rewritten**. |
| **M** | [security.md](../security.md) says passwords are “bcrypt (or argon2)”. Code is bcrypt only. |
| **M** | `internal/backup` package comment claims the panel **regenerates** Postfix/OpenDKIM maps from SQLite on every start after restore. Startup only runs `CheckRestore` (`cmd/panel/main.go`). Maps/keys are **in** the tarball. Heal path is the Status **Reload** button. |
| **L** | Architecture route table omits `/license` and the `/account` → `/settings` 308. |
| **L** | Guide Settings section: “change the administrator username and/or password” — global Settings also has the default DMARC `rua=` address. |
| **L** | Guide does not warn that restoring an **older** backup can resurrect sessions (architecture does). |
| **L** | Guide rate-limiting section does not stress that level 2 is **fail-open** (store error or missing client IP → mail continues; level 1 is the backstop). |
| **L** | [roadmap.md](../roadmap.md) and [plans/dmarc-reports.md](dmarc-reports.md) still say `admin.dmarc_report_email` after `0005` moved it to `settings`. |
| **L** | `setupManager` comments still say “admin row”; the fact is `users` / `UserExists()`. “Plan B.1 / C.4” comments are opaque to outsiders; keep them, they are history, not errors. |
| **I** | E2e coverage summary in development.md omits logrotate and supervisor-process checks that actually run. |
| **I** | [plans/logrotate-mode.md](logrotate-mode.md) is **done** but still in `docs/plans/` (active-plans directory). History belongs in git / CHANGELOG. |

Comments in production code are generally **high quality**. Missing comments
are on domain-admin authorization policy in `sendLogData` (the P0 hole has no
comment stating the intended invariant) and on `rateLimiter` memory bounds.

---

## 5. Human readability and maintainability

A new maintainer can follow the tree from [architecture.md](../architecture.md)
into `cmd/panel` → `internal/web/web.go` → services. Tests document *why*
(milter in-flight, queue-id anchoring, CSRF matrix).

Friction:

- `domain_detail.html` is the hardest HTML file to edit (repeated DNS
  host/type/value blocks, checkbox “Edit” panels).
- `panel.css` structure-tied selectors (`.muted + form > select:first-of-type`)
  will break on a copy change.
- Dual `CurrentUser` + `Principal` is redundant but works (`withPrincipal`
  sets both).
- `assignedDomains` loads **all** domains then filters in Go, while
  `store.listUserDomainNames` already exists and is unused by handlers.

None of this blocks maintenance at current size. Prefer small extractions
(DNS partial, `tryAdmit`) over a layer rewrite.

---

## 6. Logical errors

### H — Domain-admin send-log list leaks other domains

Detail page checks membership (`HandleDelivery`). The **list** does not.

`sendLogData` in `internal/web/handlers/handlers_monitor.go`:

- Empty `SendLogFilter.Domain` means “all rows” (`internal/store/sendlog.go`).
- For a non-global user, a disallowed `?domain=` is cleared to `""`. The
  assigned domain is filled in **only when there is exactly one**.
- A domain-admin with **0 or ≥2** assigned domains and no (or a forged)
  domain filter therefore sees **every** send-log row (From, To, Subject).
- `?app=` is applied to SQL **before** it is checked against the user’s
  application logins. The allowlist only updates the template’s selected
  filter. Forged `?app=<foreign-login>` with an empty domain filter returns
  that application’s rows.

The deliveries table’s domain dropdown still lists only assigned domains, so
the leak is silent.

**Invariant to implement:** a non-global principal’s `QuerySendLog` /
`CountSendLog` are always constrained to assigned domain names; if that set is
empty, the result is empty. Validate `AppLogin` against the allowlist
**before** the query.

### M — Level-2 check/reserve race

`enforceLimit` calls `flight.count` then `flight.reserve` under **separate**
mutex acquisitions (`internal/milter/ratelimit.go`, `inflight.go`). Two MAIL
FROM handlers can both observe `n == max-1` and both reserve. In-flight
tracking closes the *stored-count* race (and
`TestRateLimitCountsInFlightMessages` covers the **sequential** case). It does
not close parallel check-then-act. Severity is tempered by fail-open and
Postfix level-1. Fix: one `tryAdmit(key, since, max)` under the inflight
mutex.

### M — Session create fail-open

See §3. Not a stolen-session bug (hash never lands in the DB); it is a
logged-in-looking cookie that cannot be looked up.

### M — App delete ordering

See §3. Orphaned SASL is a mail-path consistency bug.

### L — Domain export `Version` ignored on import

`internal/domain/transfer.go` stamps `buildinfo.Version`; import checks format
only. Lower risk than full-backup `CheckRestore`; still a cross-version footgun.

### I — Journal milter fail-open; origin CSRF fail-open; queue-reconcile
`bounced`

Documented accepted risks in [security.md](../security.md). Not defects.
Revisit the CSRF ADR’s *framing* (multi-user), not necessarily the mechanism.

---

## 7. Refactoring and optimisation

Worth doing, in order:

1. `tryAdmit` (correctness, not speed).
2. `SendLogFilter` domain IN-list (correctness).
3. DNS field partial + settings credentials partial (drift).
4. One helper for the five `panel.js` show/hide field pairs.
5. `assignedDomains` via SQL for the current user (clarity, not performance).

Not worth doing now:

- Service layer for users / send-log.
- Replacing SQLite, HTMX, or the single-container model.
- Squashing migrations `0001`–`0005`.
- CSRF tokens (see §12).
- Rewriting `machine.go` or `logtail.go` for size.

---

## 8. Licence (AGPL-3.0)

Packaging is largely correct: root `LICENSE` ≡ embedded `internal/legal/LICENSE`
(test), unauthenticated `/license`, footer copyright + Source + “No warranty”
on login/setup, image copies `LICENSE`/`NOTICE`, Go deps are BSD-family, htmx
is 0BSD. Network-use §13 is stated in `NOTICE` and the README.

| Sev | Finding |
|---|---|
| **M** | IBM Plex WOFF2 files are shipped without the SIL OFL 1.1 text. OFL requires the licence to travel with the font. Add `OFL.txt` next to the fonts (and mention the path in `NOTICE`). |
| **M** | `NOTICE` tells modifiers to change the Source URL in `layout.html`. The URL is `legal.SourceURL` in `internal/legal/legal.go`, injected by `view.go`. |
| **L** | `/license` serves LICENSE only, not NOTICE. Optional: serve NOTICE at `/notice` or append attributions. |
| **L** | SPDX headers only on `cmd/panel` and `cmd/selfpost-backup`. AGPL does not require per-file SPDX; either add them everywhere or drop the two so the convention is consistent. |
| **I** | Debian package licences are pointed at packages.debian.org rather than a pinned list — normal for an image that installs from bookworm. |

No AGPL-incompatible Go dependency found in `go.mod`.

---

## 9. Legacy code and migrations

| Migration | Role | Removal |
|---|---|---|
| `0001_init.sql` | Core schema (including historical `admin`) | Keep for all **1.x** (`PRAGMA user_version` chain) |
| `0002_sessions.sql` | DB sessions | Keep for 1.x |
| `0003_logtail_state.sql` | Tailer offset | Keep for 1.x |
| `0004_dmarc_report_email.sql` | DMARC columns on `admin` | Keep for 1.x; `0005` moves the data |
| `0005_panel_users.sql` | `users` / `user_domains`; `DROP TABLE admin` | Keep for 1.x |

Squash is deferred to **2.x** — [roadmap.md](../roadmap.md) `schema-squash`.
Until then do not delete, rename, or reorder these files. Document the 1.x
rule in architecture § Persistence (one sentence).

Compat shims to keep until a major:

- `GET/POST /account` → 308 `/settings`.
- Domain rate-limit rows may still have an unused IP list column; enforcement
  ignores it.

`sessions.username` is a string, not a `user_id` FK. Renames update the column;
a missed rename would orphan sessions. Acceptable; a FK would be a 1.x
migration if usernames become mutable in more places.

**Delete** `docs/plans/logrotate-mode.md` once this review is the active plan
(status `done`; history is git / CHANGELOG `[1.2.3]`).

---

## 10. Stubs and claimed-but-unimplemented behaviour

| Item | Status |
|---|---|
| Inbound relay | Agreed plan, **no code stubs**, no `INBOUND_RELAY_*` env. Correct. |
| DMARC report **ingestion** | Candidate. UI copy already promises “a future release will be able to receive reports in the panel”. Settings `rua=` and DNS guidance **are** implemented. |
| `panel-docs` | Candidate. Status blurbs were removed in 1.2.2 in favour of this item. |
| `CONTRIBUTING.md` | Candidate, file absent. Matches roadmap. |
| CSRF tokens | Explicitly not implemented (ADR). |
| `auth.RequireGlobal` | Dead helper, not a feature stub. |

The DMARC “future release” sentence is the only user-visible promise of
unimplemented behaviour. Soften it to “SelfPost does not receive inbound mail”
or keep it and treat `dmarc-reports` as the fulfilment — product call, Sonnet
copy.

---

## 11. GUI: hacks and layout composition

The panel is CSP-strict (no inline script/style; `TestNoTemplateUsesInlineScriptOrStyle`).
No `!important`. Progressive enhancement is real (pages work without JS).
Adaptive polling in `panel.js` is a **documented** workaround: HTMX
`hx-trigger="every Ns [expr]"` uses `new Function`, which CSP would break.

| Sev | Finding |
|---|---|
| **M** | `RateLimitErr` uses `class="flash error"`. `.flash` is the **success** surface; `.error` only recolors text. There is no `.flash.error` rule. Validation failures look like success (red text on green). `domain_detail.html` + `panel.css`. |
| **M** | User **Delete** has no `data-confirm` and no confirm page. App delete / regen / rate-limit clear do; domain delete has `domain_delete.html`. One mis-click removes a panel user. |
| **M** | `domain_detail.html` repeats Host/Type/Value/`code-row` for DNS status **and** publishable records. Extract a partial (same pattern as `encrypt_fields.html`). |
| **M** | `settings.html` duplicates the credentials form (global split vs domain-admin narrow card). Drift already visible in the muted help text. |
| **L** | Adaptive polling: `outerHTML` swap every 5 s can steal clicks / focus; poll failures retry silently. Consider `aria-live="polite"` and a visible retry/error. Do not switch back to `hx-trigger="every"` under this CSP. |
| **L** | Checkbox-driven Edit panels instead of `<details>` (commented in the template). Works without JS; no `aria-expanded`. |
| **L** | Five near-identical show/hide helpers in `panel.js`. Encrypt/import fields can flash visible before `DOMContentLoaded`. |
| **L** | `hx-get` query params in `deliveries_rows.html` are not `urlquery`-encoded. Safe while domain/app charset is locked down. |
| **L** | Applications on a domain page are unpaginated. Fine until an operator has dozens of apps. |
| **L** | `<label>` used as a heading on DNS/status readouts (no `for`). |
| **I** | `{{define "wide"}}` override and `main:has(> .card.narrow)` are non-obvious but tested. Keep; do not “simplify” into per-page CSS files. |

`data-confirm` is skipped when JS is off (documented in `panel.js` only).
Domain delete already uses a real page; user delete should follow that
pattern or at least get `data-confirm`.

---

## 12. Weakly documented disputed decisions

These are real choices. Several are in [security.md](../security.md); the
problem is **stale framing** after domain-admin, not silence.

| Decision | Where | Gap |
|---|---|---|
| CSRF via origin / `Sec-Fetch-Site`; no tokens; POST with neither header allowed | security.md ADR | Still argued as “single-user”. Revisit trigger already fired. **Rewrite the ADR**; implementing tokens is a separate product call. |
| Journal-milter fail-open | architecture, milter comments | Guide rate-limit section should say L2 is best-effort. |
| Unencrypted backup/export by default | security.md | OK. Domain-admin can **export working SASL passwords** for assigned domains (`HandleExportDomain` uses `lookupDomain`). Guide/security should say so. |
| Queue reconcile marks lost lines `bounced` | security.md | OK. |
| Sliding session, no absolute cap; HTMX GET does not renew | architecture, guide | OK. |
| Restore can resurrect sessions from an older backup | architecture | Missing from the operator guide. |
| L2 skipped when client IP is unknown | milter + unit test | Not in the guide. |
| Backup encryption optional | security.md | OK. |
| Supervisord socket `0770` so the panel can `postfix reload` | supervisord.conf | Compromised panel ≈ mail-stack control. Documented as intentional; keep. |
| `workflow_dispatch` on `release.yml` derives version from `GITHUB_REF_NAME` | `.github/workflows/release.yml` | A manual run from `main` can publish a non-semver tag. Guard: only `vX.Y.Z` or an explicit version input. |

---

## 13. Edge cases

Covered above: 0 / 1 / ≥2 assigned domains on the send log; forged
`domain`/`app` query params; empty allowlist must not mean “all”.

Others:

- **Last global administrator** cannot be demoted/deleted (UI + server). Good.
- **Domain-admin with no domains** (all assigned domains deleted →
  `user_domains` cascade): today they see the full send log (P0). After the
  fix they should see an empty log, not an error.
- **Missing `mail.log`** after rotation: treated as empty, not an error
  (tested). Good.
- **Backup download after headers committed**: truncated file possible
  (streaming trade-off). Encrypted domain export is sealed in memory first.
  Acceptable; do not buffer full backups.
- **`parsePage`**: huge `p` yields a large offset and an empty page, not a
  500. Fine.
- **Concurrent domain DNS on the dashboard**: safe under Go 1.22+.
- **Import domain** is global-only; **export** is any principal who can
  access the domain. Intentional once documented.

---

## 14. Tests

**Strengths.** Milter L2 + in-flight, DNS grading, logtail follow/rotate/
reconcile, secretfile tamper, SASL argv hygiene (`--` before login), template
CSP/nav/legal footer, env-key ↔ guide regression, e2e mail path (AUTH, DKIM,
queued→sent, L1/L2, fail-open, hostname gate, session vs restart). Test
comments are better than average.

**Documented?** How to run tests: [development.md](../development.md) §
Testing. There is no e2e README (package comment in `test/e2e/main_test.go`
is the stand-in). Individual tests are not inventoried in docs — that is
fine; the e2e **summary** should mention logrotate.

**Gaps (high value):**

| Area | Gap |
|---|---|
| RBAC | **No** tests for `authz.go`, `CanAccessDomain`, domain-admin send-log scoping, `/users` 404 for domain-admin, backup 404. This is why P0 shipped. |
| Auth HTTP | No `HandleLogin` / `HandleSetup` tests (TTL, constant-time, setup complete → 404). No tests for `auth/ratelimit.go` `Allow`. |
| Sessions store | No `store/sessions*_test.go` (covered only via `auth_test` wrappers). |
| Backup as operator path | Create + `CheckRestore` unit-tested; **no** extract-onto-`/data`-and-boot test; panel `HandleBackup` POST untested. |
| Handlers | No tests for users CRUD, domain add/delete, account POST, DNS recheck endpoints. |
| `postfix.Queue` | Parser only; exec path untested (e2e does not open Mail queue). |

**Weak / low-value (keep, do not grow this style):**

- `TestDecryptErrorMessage` — substring mapping.
- `TestBackupPageOffersEncryption` — `strings.Contains` over HTML.
- Many `templates_test.go` cases — structural guards (CSP, nav). Valuable as
  guards, not as behaviour tests.
- E2e `testNoAuthRejected` vs `testForeignRelayRejected` — nearly the same
  unauthenticated send.

**E2e hygiene:**

- Fatal string in `testLevel1RateLimit` says `RATE_LIMIT_MESSAGES_PER_IP=5`;
  override is `50` (`test/e2e/negative_test.go` vs `compose.override.yml`).
- `coredns/coredns:latest` is unpinned.
- `TestImageBuildPreservesLogrotateMode` chmods the source conf then rebuilds
  — can race a dirty tree.

Do not add snapshot tests of entire pages. Add **authorization** tests that
would have caught P0.

---

## 15. Duplication and local patches

| Local patch | Systemic fix |
|---|---|
| Send-log domain/app allowlist after/around the query | Store filter: `Domains []string` required for non-global; validate app login first |
| `assignedDomains` loads all domains | Use `listUserDomainNames` / `ListDomainsForUser` |
| `requireGlobal` on each handler | Optional: `auth.RequireGlobal` on those muxes |
| Five JS field-sync helpers | One `data-show-when` helper |
| DNS host/type/value markup × many | Template partial |
| Settings credentials form × 2 | Partial |
| `web/validate` vs `app/validate` | Keep separate (different alphabets); do not merge |

The send-log allowlist is the textbook “local patch instead of a store
invariant”.

---

## 16. Other improvements

- Pin CoreDNS in e2e.
- Guard `release.yml` `workflow_dispatch` versioning (**Opus**, infra).
- Optional: `Resync` once after a successful `CheckRestore` (heal drifted
  maps). Small, mail-path, **Opus**. Not required if the tarball is the
  restore story — but then **fix the backup package comment**.
- Optional: serve `NOTICE` next to `/license`.
- Do not start inbound-relay until P0 is closed.

---

## Implementation checklist

Work top to bottom. Commit per phase (or per coherent sub-step) when asked.
Update [CHANGELOG.md](../../CHANGELOG.md) `[Unreleased]` with each user-visible
change. After Go changes: `go build`, `go vet`, `go test ./...`.

### P0 — Domain-admin send-log authorization (defect)

**Model: Opus.** Tests in the same change. **Fable** on the diff after it
lands (reviewer ≠ author).

- [x] Extend `SendLogFilter` so a non-empty domain list is an `IN` constraint.
      Empty list for a non-global user → zero rows, not “all”. Done as
      `Domains` + `AllDomains`: the zero value matches nothing, so a caller
      that states no scope cannot read the journal.
- [x] `sendLogData`: for `!p.IsGlobal()`, always constrain to assigned domain
      names; validate `AppLogin` against the user’s apps **before** query.
- [x] Tests: domain-admin with 0, 1, and 2 assigned domains; unfiltered list;
      forged `?domain=` and `?app=`; detail page still 404s on a foreign id
      (already true — keep a regression test).
- [x] Comment the invariant next to `sendLogData` (the comment that was
      missing).

**Done when:** a domain-admin cannot read another domain’s send-log rows via
the list, the fragment, or query parameters. `go test ./...` green.

### P1 — Fail-closed consistency (mail path / auth)

**Model: Opus.**

- [x] `inflight.tryAdmit` (count + reserve under one lock). Extend milter
      tests with overlapping `MailFrom` (true concurrency, not sequential).
      Two tests: concurrent `MailFrom` sessions gated so they all read the
      stored count before anyone reserves (exactly one admitted), and a
      saturation test on `tryAdmit` that overshoots the ceiling whenever count
      and reserve are separate critical sections.
- [x] `sessionStore.Create` returns an error; login does not set a cookie on
      failure.
- [x] `app.Service.Delete`: SASL (and rate-limit row) before or compensating
      with the registry row; match domain-delete ordering. Test the failure
      path with a fake SASL that errors.

**Done when:** unit tests cover the race and the two fail-closed paths.

### P2 — Security/operator docs that are wrong today

**Model: Sonnet** (English docs). No code behaviour change except copy.

- [x] Rewrite the CSRF ADR in [security.md](../security.md) for a panel that
      already has global + domain-admin. Keep the origin-check mechanism
      unless a new decision says otherwise. New revisit trigger (e.g. untrusted
      domain-admins, or a requirement that does not depend on browser
      headers).
- [x] Drop “or argon2” unless argon2 is implemented.
- [x] [guide.md](../guide.md): Users / roles; Settings DMARC field; L2
      fail-open; restore can resurrect sessions; domain-admin can export
      working SASL passwords for assigned domains.
- [x] Architecture route table: `/license`, `/account` → `/settings`.
- [x] Fix `internal/backup` package comment (restore = extract tarball +
      `CheckRestore`; maps come from the archive; Reload heals drift).
- [x] `admin.dmarc_report_email` → `settings` in roadmap + dmarc-reports plan.
- [x] development.md e2e summary: logrotate + process checks.
- [x] `setupManager` / `store` package comments: `users`, not `admin` row.

**Done when:** an operator who reads only the guide knows the panel has two
roles, and security.md no longer calls the panel single-user.

### P3 — GUI defects from the 1.2.x layout pass

**Model: Sonnet.**

- [x] `.flash.error` (or stop using `.flash` for `RateLimitErr`) — danger
      surface, not success.
- [x] User delete: `data-confirm` at minimum; prefer a confirm page like
      domain delete. Done as a confirm page (`GET/POST /users/{uid}/delete`),
      matching `domain_delete.html`.
- [x] DNS field partial; settings credentials partial.
- [x] Optional: `urlquery` on deliveries fragment params; `aria-live` on
      polled regions; confirm-without-JS note next to the CSRF accepted risks.

**Done when:** a rate-limit validation error is visually an error; user delete
cannot be a single unmarked click.

### P4 — Tests and e2e hygiene

**Model: Opus** for auth/RBAC/limiter tests; **Haiku** for the L1 fatal-string
typo; **Sonnet** if e2e docs need a paragraph.

- [x] `auth/ratelimit.go` unit tests (window, lockout, sweep). Also the
      per-key scope: one locked-out address must not lock out the others.
- [x] Login/setup handler tests (happy path + lockout + setup expiry). The
      lockout test also states that a correct password does not bypass it, and
      that the two refusals are byte-identical (no username enumeration).
- [x] Domain-admin 404 on `/users`, `/backup`, `/mail-queue`, `/system-log`,
      `/status` — as a table of every global-only route (`internal/web/handlers/authz_test.go`),
      including the write routes, plus the same 404 for a request with no
      principal and a positive control so the table cannot pass on a handler
      that always 404s.
- [x] Fix e2e L1 fatal string (`50`, not `5`).
- [x] Pin `coredns` image: tag `1.14.6`, not a digest — the tag is a multi-arch
      manifest and the stand has to come up on arm64 developer machines.
- [x] Optional: backup extract + `CheckRestore` + panel boot. Done as an
      in-process integration test (`cmd/panel/restore_test.go`) rather than
      e2e, so it runs in `go test ./...`: the archive is downloaded from a
      running panel through `POST /backup` (which closes the “`HandleBackup`
      POST untested” gap in §14 as well), unpacked the way `tar -xzf` unpacks
      it, and a second panel is booted on the result through run()'s own
      startup order. Also covers the encrypted download, the version-mismatch
      refusal, that a restore does not reopen the setup link, and that sessions
      travel in the archive. `serveHTTP` was split so the composition it
      performs (`newPanel`) can be booted without binding a port.

**Done when:** P0 cannot regress without a red test; e2e L1 message matches
the override.

### P5 — Licence and release infra

**Model: Sonnet** for OFL/NOTICE prose; **Opus** for `release.yml`; **Haiku**
for SPDX consistency and deleting the done logrotate plan.

- [x] Add SIL OFL 1.1 text beside the Plex WOFF2 files; point `NOTICE` at it.
      IBM Plex `LICENSE.txt` as `internal/web/view/static/OFL.txt` (copyright
      + OFL 1.1). Copied into the image at `/usr/share/doc/selfpost/OFL.txt`;
      served at `/static/OFL.txt`.
- [x] `NOTICE` Source URL instructions → `internal/legal/legal.go`.
- [x] `release.yml`: `workflow_dispatch` must not publish `main` as a version
      (require `vX.Y.Z` or an explicit `version` input that matches SemVer).
- [x] Delete [plans/logrotate-mode.md](logrotate-mode.md) (done; git keeps it).
- [x] Decide SPDX-everywhere vs SPDX-nowhere; do not leave two files special
      without a one-line note in development.md. SPDX-nowhere: dropped the
      two `cmd/` headers; development.md § External libraries records that
      AGPL-3.0 does not require per-file SPDX.

**Done when:** OFL travels with the fonts; a dispatch from `main` cannot tag
`ghcr.io/...:main`.

### P6 — Optional cleanup (do not start until P0–P3 are done)

**Model: Sonnet** unless noted.

- [ ] Use `auth.RequireGlobal` or delete it (**Haiku** if delete).
- [ ] Rename `handlers_account.go` / `HandleAccount` to settings (**Haiku**).
- [ ] `ListDomainsForUser` instead of load-all-and-filter.
- [ ] Cap or periodically sweep the login limiter map (**Opus**, small).
- [ ] Collapse `panel.js` field-sync helpers.
- [ ] Soften or keep DMARC “future release” UI copy (product call).
- [ ] Optional startup `Resync` after restore (**Opus**). Only if P2’s comment
      fix is judged insufficient.

### P7 — Security review of the P0–P1 diff

**Model: Fable.** Not authorship.

- [ ] Review the send-log authz change, `tryAdmit`, session create, and app
      delete ordering against [security.md](../security.md). Close each finding
      with a fix or an accepted-risk entry.

---

## What not to do

- Do not squash SQLite migrations in **1.x** (see roadmap `schema-squash` for 2.x).
- Do not implement inbound-relay, DMARC ingestion, or in-panel docs as part of
  this plan.
- Do not add CSRF tokens in the same breath as rewriting the ADR. Tokens are a
  new decision.
- Do not “simplify” comments that record threat models.
- Do not introduce a general service/repository layer for users.

---

## Done when (this plan)

1. P0 is shipped and covered by tests.
2. P2 has removed “single-user” from the CSRF ADR and documented Users in the
   operator guide.
3. P3 flash/delete bugs are gone.
4. P7 has run on the P0–P1 diff.
5. This file’s remaining boxes are either checked or explicitly dropped in
   [roadmap.md](../roadmap.md) with a reason.
6. [CHANGELOG.md](../../CHANGELOG.md) `[Unreleased]` lists the user-visible
   items (authz, docs, GUI).

After that, delete this plan (history in git) and return the recommended
order on the roadmap to inbound-relay.
