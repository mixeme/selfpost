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
| domain-admin | Domain administrator role | **agreed** | [plans/domain-admin.md](plans/domain-admin.md) |
| inbound-relay | Inbound relay (backup-MX / forwarding) | **agreed** | [plans/inbound-relay.md](plans/inbound-relay.md) |
| contributing | `CONTRIBUTING.md` | candidate | — |
| visual-style | Panel visual style | **agreed** | [plans/visual-style.md](plans/visual-style.md) |
| dmarc-reports | DMARC aggregate report ingestion and panel UI | candidate | [plans/dmarc-reports.md](plans/dmarc-reports.md) |
| logrotate-mode | `mail.log` stops rotating in some builds | candidate | [plans/logrotate-mode.md](plans/logrotate-mode.md) |

**Recommended order** (not binding): **domain-admin →
inbound-relay** — role-wide authorisation first, then the inbound relay vertical
slice. Deviating is allowed; there are no hard phases here.

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
**Order:** recommended after [domain-admin](plans/domain-admin.md).
**Version:** target bump `1.x`; `2.x` possible — to be settled once the
implementation lands.

---

## domain-admin

**Goal:** a role with access to one or several assigned domains (the list is
set by the global administrator) — applications, DKIM/DNS, and the send log for
each of them; without global operations (adding domains, full backup, the
queue, `mail.log`).

**Boundary:** an extension of v1.0 — [product.md](product.md) fixes a single
administrator. Not a second all-powerful admin, but limited access to the
assigned domains (one or several).

**Done when:** see [plans/domain-admin.md](plans/domain-admin.md).

**Dependencies / risks:** a users table, the role in the session, authorisation
in every handler, setup and backup. **Order:** recommended **before**
[inbound-relay](plans/inbound-relay.md).
**Version:** `1.x` MINOR, given a compatible migration of the current
administrator into a global one.

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

## visual-style

**Goal:** refresh the control panel's visual design — typography, colour tokens,
spacing, and component styling — without changing operator workflows or panel
behaviour.

**Boundary:** presentation only (`panel.css`, templates, static assets); no new
features. Styling must stay compatible with the panel CSP — rules live in
`panel.css`, not inline (see [security.md](security.md) and the stylesheet
header).

**Done when:** see [plans/visual-style.md](plans/visual-style.md). The agreed
direction is the mark's own — brick, warm paper, IBM Plex — taken from
[assets/selfpost-proof.html](assets/selfpost-proof.html); light and dark schemes
remain supported; readability and contrast are preserved.

**Dependencies / risks:** CSP constraints on how styles are applied (rules in
`panel.css`, never inline); visual regression across pages; the accent colour
must not read as a status badge.
**Order:** independent of the feature items; may be taken up between them.
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

