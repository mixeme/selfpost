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
an explicit break.

**Process:** [development.md](development.md). The history of closed phases is
in `git log` and [CHANGELOG.md](../CHANGELOG.md).

---

## Index

| ID | Topic | Status | Plan |
|---|---|---|---|
| inbound-relay | Inbound relay (backup-MX / forwarding) | **agreed** | [plans/inbound-relay.md](plans/inbound-relay.md) |
| contributing | `CONTRIBUTING.md` | candidate | — |
| dmarc-reports | DMARC aggregate report ingestion and panel UI | candidate | [plans/dmarc-reports.md](plans/dmarc-reports.md) |
| panel-docs | In-panel operator documentation | candidate | — |

**Recommended order** (not binding): **inbound-relay** first among agreed
items — it is the largest remaining 1.x+ extension. Candidates need explicit
agreement before they join the queue.

After a context reset, pick an item marked `agreed` or `in progress`, then work
the checklist in its linked plan.

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
storage and retention of parsed summaries; the `admin.dmarc_report_email` and
`domains.dmarc_rua` settings added in the DMARC template work must stay the
source of truth for `rua=` in DNS guidance.

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

