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
| panel-docs | In-panel operator documentation | agreed | 6/6 | [plans/panel-docs.md](plans/panel-docs.md) |
| schema-squash | Squash SQLite migrations into a 2.x baseline | **2.x** | — | — |

**Recommended order** (not binding): the next feature is **panel-docs** once
agreed. dmarc-reports shipped in
[CHANGELOG.md](../CHANGELOG.md) `[1.7.0]` (security review of the ingest path
pending); domain-stats-auto-ratelimit in `[1.6.0]`; send-log-retention in
`[1.5.0]`; inbound-relay in `[1.4.0]`; queue-retries in `[1.3.1]`; the
2026-08-13 full-tree review follow-ups are in `[1.3.0]`. Candidates need
explicit agreement before they join the queue.

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

**Version:** `1.x` MINOR; `candidate` until explicitly agreed.

---

## schema-squash

**Goal:** when 2.x is cut, stop shipping the 1.x migration files
(`0001_init.sql` … `0005_panel_users.sql`) in the binary and replace them with
one baseline that is the schema as of `user_version = 5`. Fresh 2.x data
directories no longer create-then-drop the historical `admin` table.

**Boundary:** 1.x keeps the full chain so a 1.0.0 data directory still boots.
Do not delete, rename, or reorder those files while MINOR compatibility with
`1.0.0` holds. `migrate()` maps **file order** to `PRAGMA user_version` (`target
= i + 1`); dropping a file in 1.x would skip or mis-apply steps on existing
databases. Git history keeps the old files either way; only the embedded set
in the 2.x image changes.

**Upgrade gate (required with the squash):**

| `user_version` | 2.x behaviour |
|---|---|
| `0` (empty file) | Apply the baseline; set `user_version` to the new chain’s head |
| `>= 5` (fully migrated 1.x) | Skip; schema is already the baseline |
| `1`…`4` (mid-chain 1.x) | **Refuse to start** — boot the last 1.x once, then 2.x |

Restore remains a separate lock: the backup manifest version must match the
running binary ([architecture.md](architecture.md) § Persistence). It does not
replace this gate.

**Done when:** 2.x embeds a single baseline (plus any 2.x-only migrations after
it); the gate above is tested; the operator guide says a 2.x image will not
open an unfinished 1.x database.

**Dependencies / risks:** a decided 2.x cut (another breaking change, or an
explicit major). Squashing five short files is not a reason to cut 2.x on its
own. A missed gate leaves a `user_version = 3` database silently stuck.
**Version:** `2.x` major only; not a 1.x item.

