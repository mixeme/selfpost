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
| contributing | `CONTRIBUTING.md` | candidate | — | — |
| inbound-antispam-panel | Inbound antispam journal + allow/deny lists | agreed | 1/12 | [plans/inbound-antispam-panel.md](plans/inbound-antispam-panel.md) |
| inbound-quarantine | Inbound spam quarantine (hold / review / release) | candidate | — | [plans/inbound-quarantine.md](plans/inbound-quarantine.md) |
| schema-squash | Squash SQLite migrations into a 2.x baseline | **2.x** | — | — |

**Recommended order** (not binding): the next feature is
**inbound-antispam-panel** (`1.10.0`). panel-docs shipped in
[CHANGELOG.md](../CHANGELOG.md) `[1.8.0]`; dmarc-reports in `[1.7.0]`
(security review of the ingest path pending); domain-stats-auto-ratelimit in
`[1.6.0]`; send-log-retention in `[1.5.0]`; inbound-relay in `[1.4.0]`;
queue-retries in `[1.3.1]`; the 2026-08-13 full-tree review follow-ups are in
`[1.3.0]`. Candidates need explicit agreement before they join the queue.

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

**Status:** done — shipped in `[1.8.0]` (2026-08-18). Plan:
[plans/panel-docs.md](plans/panel-docs.md) (checklist complete).

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

