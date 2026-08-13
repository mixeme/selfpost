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

| ID | Topic | Status | Plan |
|---|---|---|---|
| code-review | Full-tree review follow-ups (authz, docs, GUI, tests) | **agreed** | [plans/code-review.md](plans/code-review.md) |
| queue-retries | Postfix retry policy in the panel (queue lifetime, backoff) | **agreed** | [plans/queue-retries.md](plans/queue-retries.md) |
| inbound-relay | Inbound relay (backup-MX / forwarding) | **agreed** | [plans/inbound-relay.md](plans/inbound-relay.md) |
| contributing | `CONTRIBUTING.md` | candidate | — |
| dmarc-reports | DMARC aggregate report ingestion and panel UI | candidate | [plans/dmarc-reports.md](plans/dmarc-reports.md) |
| panel-docs | In-panel operator documentation | candidate | — |
| schema-squash | Squash SQLite migrations into a 2.x baseline | **2.x** | — |

**Recommended order** (not binding): **code-review P0** first (shipped
domain-admin send-log leak — a defect, not a feature), then the rest of that
plan as listed; **queue-retries** is a small panel item that can land in
parallel after P0; then **inbound-relay**. Candidates need explicit agreement
before they join the queue.

After a context reset, pick an item marked `agreed` or `in progress`, then work
the checklist in its linked plan.

---

## code-review

**Goal:** close the 2026-08-13 full-tree review: domain-admin send-log
authorization, a few fail-closed paths, docs that still say “single-user”,
GUI flash/delete bugs, test gaps, licence/release hygiene.

**Boundary:** defects and docs/UI follow-ups inside the current 1.x product.
Not inbound-relay, not DMARC ingestion, not a layer rewrite.

**Done when:** see the criteria in
[plans/code-review.md](plans/code-review.md).

**Dependencies / risks:** P0 is confidentiality between panel roles; it
jumps the feature queue. Implementation models are in the plan (Opus / Sonnet
/ Haiku / Fable per [development.md](development.md)).
**Version:** patch.

---

## queue-retries

**Goal:** show on Mail queue and on a delivery's history how this Postfix
retries deferred mail — first delay, backoff cap, queue lifetime — reading
the effective config (`postconf -h`) once at panel start so a manual
override is visible.

**Boundary:** explanation only. Postfix stays as-is; no attempt counter, no
panel knobs for queue lifetime, no schema change. Domain administrators see
the intervals on `/deliveries/{id}` (they cannot open Mail queue).

**Done when:** see the criteria in
[plans/queue-retries.md](plans/queue-retries.md).

**Dependencies / risks:** `postconf` unavailable outside the container
(fallback + muted note). Copy must stay time-based — Postfix has no max
attempt count.
**Version:** patch.

---

## inbound-relay

**Goal:** optional acceptance of mail on port 25 for explicitly configured
domains, forwarded to an upstream (backup-MX / relay-forwarder). Off by default
(`INBOUND_RELAY_ENABLE=false`); without the flag the outbound path is
unchanged.

**Boundary:** an extension of v1.0 — [product.md](product.md) excludes inbound
mail and mailboxes. This is relay/forward, not IMAP/POP3/webmail; an anti-spam
engine stays outside the image, only the attachment point is provided.

**Done when:** see the criteria in
[plans/inbound-relay.md](plans/inbound-relay.md).

**Dependencies / risks:** a finished outbound path; open relay and backscatter;
a wider attack surface (port 25 accepting mail).
**Version:** target bump `1.x`; `2.x` possible — to be settled once the
implementation lands.

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

## dmarc-reports

**Goal:** SelfPost receives DMARC aggregate reports (RFC 7489) on SMTP,
parses the gzip/XML payloads, and shows pass/fail summaries in the panel — so
the operator does not need an external DMARC service or a separate mailbox
workflow.

**Boundary:** an extension of v1.0 — not IMAP/webmail and not a general
inbound relay. A dedicated inbound path for report messages only; forensic
reports (`ruf=`) out of scope for v1.

**Done when:** see [plans/dmarc-reports.md](plans/dmarc-reports.md).

**Dependencies / risks:** inbound SMTP in the image (may share infrastructure
with [inbound-relay](plans/inbound-relay.md) but must not require backup-MX);
storage and retention of parsed summaries; the `dmarc_report_email` setting
(migration `0005` moved it off the old `admin` table into `settings`) and
`domains.dmarc_rua` added in the DMARC template work must stay the source of
truth for `rua=` in DNS guidance.

**Order:** after the DMARC `rua=` settings ship; may follow or overlap with
inbound-relay depending on how port 25 acceptance is structured.

**Version:** `1.x` MINOR.

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

