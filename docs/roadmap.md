# Roadmap: open work (1.x+)

**Status:** a working tracker of extensions to the v1.0 boundary, each taken up
only after explicit agreement ([product.md](product.md),
[.cursor/rules/agent-rules.mdc](../.cursor/rules/agent-rules.mdc)). Detailed
design lives in [plans/](plans/). Items marked `candidate` need an OK before
any code is written.

**Reading this from outside the project:** nothing here is a commitment or a
release promise. There are no dates, the order is a recommendation rather than
a schedule, and an item can be dropped or reshaped once its plan is written.
What the project *will not* do is a separate question, answered in
[product.md](product.md) — an item's absence from this file does not mean it is
planned but unlisted.

**Versioning:** SemVer MINOR in the **1.x+** line by default (`1.1.0`…), as long
as defaults and migrations stay compatible with `1.0.0`. A major `2.x` only for
an explicit break. One such break, when 2.x is cut for any reason, is
[schema-squash](#schema-squash) — replacing the 1.x SQLite migration chain
with a baseline. That item does not by itself justify a major.

**Process:** [development.md](development.md). The history of closed phases is
in `git log` and [CHANGELOG.md](../CHANGELOG.md).

---

## Index

| ID | Topic | Status | Progress | Plan |
|---|---|---|---|---|
| preflight | Installation check page (`/preflight`) | candidate | — | [plans/preflight.md](plans/preflight.md) |
| contributing | `CONTRIBUTING.md` | candidate | — | — |
| inbound-antispam-panel | Inbound antispam journal + allow/deny lists | agreed | 1/12 | [plans/inbound-antispam-panel.md](plans/inbound-antispam-panel.md) |
| inbound-quarantine | Inbound spam quarantine (hold / review / release) | candidate | — | [plans/inbound-quarantine.md](plans/inbound-quarantine.md) |
| csrf-tokens | Per-session CSRF tokens on state-changing forms | candidate | — | — |
| template-data-typing | Typed structs for template data instead of `map[string]any` | candidate | — | — |
| structured-logging | `log/slog` with levels and fields | candidate | — | — |
| review-2026-08-followups | Remaining findings of the 2026-08-19 code review | candidate | — | — |
| schema-squash | Squash SQLite migrations into a 2.x baseline | **2.x** | — | — |

**Recommended order** (not binding): the next feature is
**inbound-antispam-panel** (`1.10.0`). panel-docs shipped in
[CHANGELOG.md](../CHANGELOG.md) `[1.8.0]`; dmarc-reports in `[1.7.0]`
(security review of the ingest path pending); domain-stats-auto-ratelimit in
`[1.6.0]`; send-log-retention in `[1.5.0]`; inbound-relay in `[1.4.0]`;
queue-retries in `[1.3.1]`; the 2026-08-13 full-tree review follow-ups are in
`[1.3.0]`. Candidates need explicit agreement before they join the queue.

The 2026-08-19 full-tree code review is closed except for four items, all
listed above and none of them urgent: [csrf-tokens](#csrf-tokens),
[template-data-typing](#template-data-typing),
[structured-logging](#structured-logging) and
[review-2026-08-followups](#review-2026-08-followups). What that review
produced is in [CHANGELOG.md](../CHANGELOG.md) `[Unreleased]`; its own document
was removed once these four items carried everything still open, and stays in
git history.

After a context reset, pick an item marked `agreed` or `in progress`, then work
the **Implementation checklist** in its linked plan. The `Progress` column above
is `done/total` checklist steps in that plan ([development.md](development.md)
§ Plan checklists).

---

## contributing

**Goal:** `CONTRIBUTING.md` in the root — the dev loop, the checks to run
before a PR, the commit protocol; [development.md](development.md) links to it
rather than repeating it.

**Boundary:** process documentation; worth writing once there is an external
flow of PRs.

**Done when:** the file is in the root and development.md does not duplicate
it.

**Dependencies / risks:** with a single developer and no PRs, this is low
priority.
**Version:** no bearing on semver.

---

## panel-docs

**Status:** done — shipped in `[1.8.0]` (2026-08-18). The implementation plan
file was removed as shipped cleanup; release details remain in
[CHANGELOG.md](../CHANGELOG.md).

**Goal:** built-in operator documentation in the panel — short pages (or a
help drawer) that explain what each Status check and other controls mean,
without sending the operator out to `docs/guide.md`.

**Boundary:** in-panel help only; not a second copy of the full operator guide.
Seed content includes the Status blurbs removed from the cards in favour of a
denser layout — Machine (kernel counters / rate window), TLS certificate
(port 465, reverse-proxy mount), Hostname / reverse DNS (forward-confirmed
reverse DNS, PTR at the hosting provider), and similar notes for other panel
surfaces as they lose inline commentary.

**Done when:** an operator can open help from the panel for those topics; the
removed Status blurbs are preserved there (or equivalent); no requirement to
read the git tree for day-to-day meaning of a card.

**Dependencies / risks:** copy ownership and translation; keeping help in sync
when checks change; not bloating every page with a second column of prose.

**Version:** shipped `1.8.0`.

---

## inbound-antispam-panel

**Goal:** panel view of inbound anti-spam decisions (when, from, to, subject,
action) plus editable allow/deny lists that tune the external filter — without
shipping rspamd or another engine inside the image.

**Boundary:** metadata journal and list sync only; SelfPost does not run the
filter ([product.md](product.md)). Quarantine:
[inbound-quarantine](#inbound-quarantine). Design closed in
[plans/inbound-antispam-panel.md](plans/inbound-antispam-panel.md) § Decisions.

**Done when:** with inbound relay enabled, the operator sees journal rows
(accepts via inbound-journal milter, rejects via mail.log tailer) and can
maintain instance-wide allow/deny lists synced to rspamd maps when the antispam
hook is set.

**Dependencies / risks:** inbound relay `[1.4.0]`; rspamd map/header coupling;
journal PII; global-admin-only RBAC.

**Version:** `1.10.0` MINOR; **agreed**.

---

## inbound-quarantine

**Goal:** optional hold for suspicious inbound mail — review in the panel, then
release to upstream, discard, or tune lists. Design not settled.

**Boundary:** expands inbound from pure relay toward short-lived message
storage; may conflict with "no mailboxes" in [product.md](product.md) until
product explicitly agrees. Alternatives (rspamd-only quarantine vs SelfPost
store) are listed in [plans/inbound-quarantine.md](plans/inbound-quarantine.md).

**Done when:** TBD after open questions in the plan are answered.

**Dependencies / risks:** inbound relay `[1.4.0]`; storage/retention/backup
size; malware in held MIME; overlap with
[inbound-antispam-panel](#inbound-antispam-panel). No implementation checklist
yet.

**Version:** TBD; `candidate` until explicitly agreed.

---

## preflight

**Goal:** a `/preflight` page (global-admin only) that verifies instance-level
infrastructure: rDNS/PTR, TLS certificate + hostname match, port 25/465/587
reachability, HELO banner correctness, reverse-proxy headers, DKIM milter
socket, and a test-email sender. Each check shows an actionable fix
recommendation on failure.

**Boundary:** instance infrastructure only; per-domain DNS checks remain on
domain detail pages. Does not track test-email delivery beyond local MTA
acceptance.

**Done when:** the page loads, all checks run with traffic-light results and
recommendations, and the test-email form successfully submits a message through
Postfix.

**Dependencies / risks:** port-connectivity check may not reflect external
reachability behind NAT without hairpin; test email needs at least one domain or
falls back to `postmaster@SELFPOST_HOSTNAME`.

**Version:** `1.x` MINOR; `candidate`.

---

## csrf-tokens

**Goal:** a per-session CSRF token on every state-changing form, checked
server-side in addition to the `Sec-Fetch-Site` / `Origin` check the panel
relies on today (`originAllowed` in
[internal/web/security.go](../internal/web/security.go)).

**Boundary:** defence in depth, not a replacement — the header check stays and
keeps covering requests that carry no form. The token is bound to the existing
session record; no new session model, no cookie changes.

**Done when:** every POST form and htmx fragment that changes state carries a
token, a missing or mismatched token is refused with a test that proves it, and
[security.md](security.md) describes the layered model in one place instead of
two.

**Dependencies / risks:** [security.md](security.md) currently records the
opposite decision — origin checking without tokens, with the accepted risk and
its revisit triggers written out in the ADR § *CSRF via origin checking*. The
2026-08-19 review recorded "add tokens" without turning it into a task, which is
why this item exists. Settle which decision stands before writing code; both
outcomes are honest, and "keep headers" means deleting this item rather than
leaving the contradiction standing. The work itself is a hidden field in roughly
two dozen forms plus the htmx swap paths.

**Version:** `1.x` MINOR; `candidate`.

---

## template-data-typing

**Goal:** replace the `map[string]any` template payloads in
`internal/web/handlers` with typed structs (`DomainDetailData`, `SettingsData`,
...), so a renamed or mistyped key is a compile error instead of a silently
empty page section.

**Boundary:** handler-to-template plumbing only; no template redesign, no route
or behaviour change. `renderDomainDetail`
([handlers_apps.go](../internal/web/handlers/handlers_apps.go)) sets around
forty keys on one map and is the case that motivates the item; roughly ten
render sites still pass maps.

**Done when:** the page handlers pass structs, the shared footer/base fields
(`Version`, `Copyright`, `SourceURL`) still arrive on every page, and the
template guard tests pass unchanged.

**Dependencies / risks:** mechanical but wide — it touches every
`handlers_*.go` and every template that reads those keys, and a missed key is
invisible unless a test renders that page. Worth pairing with the handler tests
in [review-2026-08-followups](#review-2026-08-followups) so the pages are
covered before they are rewired. Reviewer's estimate: 2–3 hours.

**Version:** no bearing on semver; `candidate`.

---

## structured-logging

**Goal:** move from `log.Printf` to `log/slog` — levels, structured fields, and
one logger injected through constructors, instead of the package-level
`logf = log.Printf` alias repeated in five packages.

**Boundary:** internal logging only. The destination stays stdout/stderr under
supervisord; no log shipping, no new dependency. Lines the panel's own Status
page and log tailer depend on must keep parsing.

**Done when:** packages take a `*slog.Logger`, the per-package `logf` aliases
are gone, and levels are used deliberately (an error is not an info).

**Dependencies / risks:** the log tailer reads Postfix's log rather than the
panel's, so the blast radius is small — but anything that greps the panel's
output (e2e assertions, operator habits) sees a new format. Reviewer's
estimate: 3–4 hours.

**Version:** no bearing on semver; `candidate`.

---

## review-2026-08-followups

**Goal:** the leftovers of the 2026-08-19 full-tree code review that are too
small to be roadmap items of their own. Everything still open from that review
is in the table below; the rest of it shipped in
[CHANGELOG.md](../CHANGELOG.md) `[Unreleased]`.

| Area | Item |
|---|---|
| Security | Re-authenticate (ask for the current password) before a backup download — [handlers_backup.go](../internal/web/handlers/handlers_backup.go) hands the archive to any signed-in session |
| Tests | The `internal/dmarc` service layer (`service.go`, `addresses.go`) has no tests |
| Tests | No direct tests for the apps / domains / users / dmarc handlers; `authz_test` covers them only indirectly |
| Tests | Nothing covers error page templates, a corrupted backup archive, malformed or oversized DMARC XML, delivery-log pagination edges, concurrent store operations, or zero-traffic `store` stats |
| Edge case | `internal/secretfile` accepts an empty password at library level — guard it, or state that validation is the caller's job |
| Edge case | The log fingerprint in [logtail/offset.go](../internal/logtail/offset.go) can collide across a rotation — include the inode |
| Edge case | `parsePage` in [handlers_monitor.go](../internal/web/handlers/handlers_monitor.go) does not clamp to `lastPage` |
| Edge case | [dnscheck/resolver.go](../internal/dnscheck/resolver.go) fans out four parallel queries where a fallback chain would do — change it or write down why not |
| UI | `.pair` (DMARC templates) and `.split` (every other page) are two names for one layout |
| UI | Two `back_link` elements in a row in `dmarc_domain.html` |
| UI | `initShowWhen` is not re-run after an htmx swap — safe today (no form arrives that way), fragile if one ever does |

**Boundary:** no feature work and no behaviour change an operator would notice,
apart from the backup re-auth prompt. Anything here that grows past an hour
becomes its own roadmap item instead.

**Done when:** each row is either implemented or struck with a written reason.

**Dependencies / risks:** the test rows overlap
[template-data-typing](#template-data-typing) — write the handler tests first if
both are taken up. One finding of that review was **declined** and is
deliberately absent: `//go:build linux` for the `/proc` reader in
[internal/health/machine.go](../internal/health/machine.go). That code is
parameterised by root directory, calls nothing platform-specific, already
degrades gracefully when `/proc` is missing (`TestMachineSamplerWithoutProc`),
and tagging it would only delete cross-platform test coverage.

**Version:** no bearing on semver; `candidate`.

---

## schema-squash

**Goal:** when 2.x is cut, stop shipping the 1.x migration chain in the binary
and replace it with one baseline equal to the schema at the then-current head.
Fresh 2.x data directories no longer create-then-drop the historical `admin`
table. Current chain, legacy notes, and gate thresholds:
[schema-migrations.md](schema-migrations.md) (update that file when migrations
ship; adjust the squash gate to its head at cut time).

**Boundary:** 1.x keeps the full chain so a `1.0.0` data directory still boots.
Do not delete, rename, or reorder migration files while MINOR compatibility with
`1.0.0` holds. Git history keeps the old files either way; only the embedded
set in the 2.x image changes.

Restore remains a separate lock: the backup manifest version must match the
running binary ([architecture.md](architecture.md) § Persistence). It does not
replace the squash upgrade gate.

**Done when:** 2.x embeds a single baseline (plus any 2.x-only migrations after
it); the gate in [schema-migrations.md](schema-migrations.md) is tested; the
operator guide says a 2.x image will not open an unfinished 1.x database.

**Dependencies / risks:** a decided 2.x cut (another breaking change, or an
explicit major). Squashing the current short chain is not a reason to cut 2.x on
its own. A missed gate leaves a mid-chain 1.x database silently stuck.
**Version:** `2.x` major only; not a 1.x item.

