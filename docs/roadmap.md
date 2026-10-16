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
| inbound-upstream-result | Inbound › Log shows what the upstream answered: sent, deferred, bounced, with its last reply | shipped `2.10.0` | 7/7 | [plans/inbound-upstream-result.md](plans/inbound-upstream-result.md) |
| health-setup-checks | Server › Health also checks the certificate's name, Postfix's greeting and the trusted proxy (was preflight) | shipped `2.11.0` | — | — |
| test-email | Send a test message from the panel (was preflight) | shipped `2.12.0` | — | — |
| inbound-antispam-panel | Inbound antispam journal + allow/deny lists | shipped `2.1.0` | 12/12 | [plans/inbound-antispam-panel.md](plans/inbound-antispam-panel.md) |
| origin-strict | Refuse a state-changing request that carries neither `Sec-Fetch-Site` nor `Origin` (was csrf-tokens) | shipped `2.10.4` | — | — |
| backup-reauth | Ask for the account password before a backup or a domain export is downloaded | shipped `2.10.3` | — | — |
| review-2026-08-followups | Remaining findings of the 2026-08-19 code review | closed 2026-10-14 (rows placed in [audit-2026-10-12](audit-2026-10-12.md)) | — | — |
| panel-notifications | E-mail notifications about important events | shipped `2.3.0` | 10/10 | [plans/panel-notifications.md](plans/panel-notifications.md) |
| delivery-log-storage | Keep a message's `mail.log` lines with its send-log row | shipped `2.2.0` | 8/8 | [plans/delivery-log-storage.md](plans/delivery-log-storage.md) |
| password-reset | Password reset by e-mail | agreed (2026-10-13) | — | — |
| local-time | The panel shows time, and counts its days, in a time zone other than UTC | shipped `2.13.0` | — | — |
| notification-mail-recheck | The Notification mail records on Server › Settings can be checked again from where they are shown | shipped `2.9.0` | — | — |
| settings-save-row | Server › Settings: the Save button no longer stands alone under a long right column | shipped `2.9.0` | — | — |
| dns-record-collapse | A DNS record that is right is shown folded: its name and its tag, the values on request | shipped `2.9.0` | — | — |
| go-milter-upstream | Prepare an issue and a PR for go-milter: its header map outlives a message and is unbounded | prepared 2026-10-16, the owner posts | — | [third_party/go-milter/PATCHES.md](../third_party/go-milter/PATCHES.md) |
| own-mta-builds | Analysis: build Postfix and OpenDKIM ourselves, patched, instead of Debian's packages | analysed 2026-10-15: no own builds; three rows proposed instead | — | [own-mta-builds-2026-10-15.md](own-mta-builds-2026-10-15.md) |
| port25-bare-newline | The inbound relay's port 25 refuses bare newlines in DATA (`smtpd_forbid_bare_newline=normalize`) | shipped `2.6.1` | — | — |
| base-trixie | The image's base from Debian bookworm (LTS since 2026-07-12) to trixie: Postfix 3.10 with the 2026 security batches | shipped `2.7.0` | — | — |
| panel-dkim-signing | DKIM signing in the panel's own milter; OpenDKIM, the syslog relay and the parser port removed | shipped `2.8.0` | 14/14 | [plans/panel-dkim-signing.md](plans/panel-dkim-signing.md) |
| phone-desktop-mode | A phone browser in "desktop site" mode gets the phone navigation | shipped `2.10.2` | — | — |
| from-header-binding | Hold a message's `From:` header against the application that sends it | shipped `2.4.0` | 9/9 | [plans/from-header-binding.md](plans/from-header-binding.md) |
| panel-redesign | Panel redesign (accepted mockups, design contract) | shipped `2.0.0` | 27/27 | [plans/panel-redesign.md](plans/panel-redesign.md) |
| schema-squash | Squash SQLite migrations into a 2.x baseline | built in panel-redesign (stage 1) | — | [plans/panel-redesign.md](plans/panel-redesign.md) |
| audit-2026-10-12 | Follow-ups of the 2026-10-12 full-tree code audit, seven stages | done 2026-10-15 (stage 0 shipped `2.4.2`, stages 1+2 `2.5.0`, 2b `2.5.1`, 2c `2.5.2`, 3 `2.5.3`, 4 `2.5.4`, 5 `2.6.0`; stage 6 docs and tests, no cut) | 81/81 | [audit-2026-10-12.md](audit-2026-10-12.md) |

**Recommended order** (not binding; owner, 2026-10-09: feature order follows
development convenience): **panel-redesign** shipped as `2.0.0` (2026-10-11) and
**inbound-antispam-panel** as `2.1.0` (2026-10-12), built in the new component
kit — styling its two screens once instead of twice. **delivery-log-storage**
shipped as `2.2.0` the same day, **panel-notifications** as `2.3.0` and
**from-header-binding** as `2.4.0`. Stage 0 of
[audit-2026-10-12](audit-2026-10-12.md) shipped as `2.4.2` (2026-10-14), its
stages 1 and 2 as `2.5.0` the same day, stage 2b as `2.5.1`, stage 2c as
`2.5.2` (2026-10-15), and stages 3, 4 and 5 as `2.5.3`, `2.5.4` and
`2.6.0` the same day; stage 6 (documents and tests, no cut) closed the
audit. Of the three rows from [own-mta-builds](#own-mta-builds),
**port25-bare-newline** shipped as `2.6.1` the same day and
**base-trixie** as `2.7.0`, and **panel-dkim-signing** as `2.8.0` (2026-10-16). **dns-record-collapse**, **settings-save-row** and **notification-mail-recheck** shipped together as `2.9.0` the same day, with `/data/opendkim` removed as panel-dkim-signing planned, then `2.9.1` (log panes on a phone) and **inbound-upstream-result** as `2.10.0`. What is agreed after it is marked so in the index; every other
remaining item is a candidate. panel-docs shipped in
[CHANGELOG.md](../CHANGELOG.md) `[1.8.0]`; dmarc-reports in `[1.7.0]`
(its ingest path first ran in `2.0.0` and was reviewed on 2026-10-11, see
[security.md](security.md)); domain-stats-auto-ratelimit in
`[1.6.0]`; send-log-retention in `[1.5.0]`; inbound-relay in `[1.4.0]`;
queue-retries in `[1.3.1]`; the 2026-08-13 full-tree review follow-ups are in
`[1.3.0]`. Candidates need explicit agreement before they join the queue.

The 2026-08-19 full-tree code review is closed except for one item, listed
above and not urgent: [origin-strict](#origin-strict) (its "add CSRF tokens",
decided otherwise on 2026-10-13). Of its other three,
template-data-typing is done (the last untyped map, the health sampler's, went
in [audit-2026-10-12](audit-2026-10-12.md) stage 4, `2.5.4`),
structured-logging is [dropped](#dropped-with-the-condition-for-return)
(2026-10-13) and [review-2026-08-followups](#review-2026-08-followups) is
closed (2026-10-14). What that review produced is in
[CHANGELOG.md](../CHANGELOG.md) `[1.9.2]`; its own document was removed once
these four items carried everything still open, and stays in git history.

After a context reset, pick an item marked `agreed` or `in progress`, then work
the **Implementation checklist** in its linked plan. The `Progress` column above
is `done/total` checklist steps in that plan ([development.md](development.md)
§ Plan checklists).

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
filter ([product.md](product.md)). Quarantine is not on the roadmap
([Ideas](#ideas-not-on-the-roadmap)). Design closed in
[plans/inbound-antispam-panel.md](plans/inbound-antispam-panel.md) § Decisions.

**Done when:** with inbound relay enabled, the operator sees journal rows
(accepts via inbound-journal milter, rejects via mail.log tailer) and can
maintain instance-wide allow/deny lists synced to rspamd maps when the antispam
hook is set.

**Dependencies / risks:** inbound relay `[1.4.0]`; rspamd map/header coupling;
journal PII; global-admin-only RBAC.

**Version:** shipped `2.1.0` (2026-10-12), MINOR (was `1.10.0`; reordered after the redesign on 2026-10-09).

---

## health-setup-checks

**Decided by the owner on 2026-10-13**, in place of the former candidate
*preflight* (a page of its own, Server › Preflight, planned in August before
Server › Health existed). Of that plan's eight checks Health already makes
three (reverse DNS, certificate validity, the DKIM socket); five were missing.
Three of the five go into Health, one becomes [test-email](#test-email), one
is not built ([Dropped](#dropped-with-the-condition-for-return)). No second
page of checks.

**Goal:** three more lines on Server › Health, in the cards that are there:

- **The certificate is for this server**: its names include
  `SELFPOST_HOSTNAME` (TLS certificate card). Today a certificate for another
  name is "valid".
- **Postfix greets with the hostname**: the 220 line on port 25 names
  `SELFPOST_HOSTNAME`, not `localhost` or a container id (Hostname card).
- **The trusted proxy**: the address the panel sees this request from, and
  whether it took it from `X-Forwarded-For`. With `TRUSTED_PROXY_CIDR` wrong
  or empty behind a proxy the header is ignored
  ([internal/web/auth/auth.go](../internal/web/auth/auth.go)), every client
  has the proxy's address and the sign-in limiter is shared by all of them -
  silently.

**Boundary:** lines in existing cards, each with what to do when it fails;
no new card unless the mockup shows the lines do not fit. Mockup-first
([plans/panel-redesign.md](plans/panel-redesign.md) § Enforcement). The
mockup `preflight` stays drawn and unbuilt.

**Done when:** each line is shown wrong on the e2e stand or in a test with a
wrong certificate name, a wrong greeting and an untrusted proxy, and right on
the test server.

**Done (2026-10-16):** mockups `b2dcefc`, `cc33890`, `0feaa55` (the owner:
the greeting on a row of its own, the name check a tag beside *Names*).
The certificate: `health.Certificate.ForHost` - DNS names as a TLS client
matches them, the CN only without any, as OpenSSL and so a sending Postfix
still does - fails a certificate for another name. The greeting:
`postconf -x -h smtpd_banner`, read once at the start (main.cf is not
rewritten after), is what smtpd writes after `220`
(src/smtpd/smtpd.c:5979); a first word other than the hostname warns. The
proxy: `auth.Module.Seen` - an `X-Forwarded-For` left unread because the
peer is not trusted warns, with the `/32` (or `/128`) to set. Wrong cases in
unit tests, right ones in e2e (`testFormPostWithTheBrowsersOrigin`).

**Version:** `2.x` MINOR; `agreed`.

---

## test-email

**Decided by the owner on 2026-10-13**, taken out of the former *preflight*.

**Goal:** a global administrator sends a test message from the panel to an
address they type, and sees whether Postfix took it; the message then appears
in Outbound › Log like any other. Today nobody can send through the server
without setting up an application first.

**Boundary:** global role only; from `postmaster@SELFPOST_HOSTNAME`, the
sender notifications use, or from a sending domain the administrator picks -
settled at the mockup. It does not follow the message further than the log
does. Where the form lives (Health, or a sending domain's page) is settled at
the mockup; mockup-first.

**Done when:** the form sends, the row appears in the log, the form is
rate-limited and refuses a recipient list, and it passes a security review -
a form that sends mail is a new surface.

**Done (2026-10-17):** from `postmaster@` the hostname (the owner,
2026-10-16: "Да, адрес годится"), card 09 on Health in the static stack
(mockups `e92769f`, `e3b2501`, `8f0ccf9`). The row is written before the
hand-over and learns its queue id from cleanup's Message-ID line
(`logtail.AwaitMessageID`); the browser lands on it. One address
(`validate.AccountEmail`), ten an hour from the send log, global role.

**Version:** `2.x` MINOR; `agreed`.

---

## origin-strict

**Decided by the owner on 2026-10-13**, in place of the former candidate
*csrf-tokens* (per-session tokens on every form, recorded by the 2026-08-19
review): no tokens; the origin check is made strict instead.

**Goal:** `originAllowed`
([internal/web/security.go](../internal/web/security.go)) refuses a
state-changing request that carries neither `Sec-Fetch-Site` nor `Origin`.
Today such a request is let through, which leaves a client that sends neither
header open to a request forged by any site
([security.md](security.md) § Accepted risks).

**Boundary:** one branch of one function, its test, and the documents. No
template changes, no hidden fields. Before the change, find what posts to the
panel without a browser (the e2e stand, scripts) and make it send `Origin`.

**Done when:** a `POST` with neither header is refused with a test that proves
it, the e2e stand passes, the accepted risk about header-less requests is
struck from [security.md](security.md) and its ADR states the strict rule.

**Done (2026-10-16):** `originAllowed` returns false in the "neither
header" branch; the unit test's case is refused, e2e posts with neither
header and gets 403, and the e2e suite's client sends `Origin` on every
form it posts. The accepted risk is struck from security.md and the ADR's
decision says refused; architecture and the guide say so.

**Dependencies / risks:** a browser that sends neither header can no longer
save anything in the panel - accepted, the panel's users pick their own
browsers. Tokens stay a recorded alternative, not a task: the conditions under
which they are built are in [security.md](security.md), ADR § *CSRF via origin
checking* (**Revisit if**).

**Version:** `2.x` PATCH; `agreed`.

---

## backup-reauth

**Agreed with the owner on 2026-10-13**, out of
[review-2026-08-followups](#review-2026-08-followups).

**Problem:** Server › Backup hands the full backup and a domain export to any
live session of a global administrator
([handlers_backup.go](../internal/web/handlers/handlers_backup.go)). The
archive holds the whole database - panel password hashes, application (SASL)
passwords, DKIM keys - so a stolen session cookie, or a panel left open, is a
quiet copy of the server. *Encrypt with a password* does not help: the thief
chooses that password.

**Goal:** both downloads ask for the current account password, checked the
way a password change checks it and under the same limiter.

**Boundary:** one field in each of the two forms (mockup-first); import is
not touched; the `selfpost-backup` CLI is not touched - it runs inside the
container.

**Done when:** a download without the password or with a wrong one is refused
with a test that proves it, attempts are limited, and
[security.md](security.md) § Backup says so.

**Done (2026-10-16):** "Your account password" in both forms (mockup
`1888dce`), checked by `reauth` in `internal/web/handlers` with bcrypt and
`AllowPasswordChange` (ten in fifteen minutes per user, shared with the
Account form); a wrong or missing one is 401 on the same page, logged, and
nothing is sent. Handler and e2e tests; security.md § Backup and domain
export, guide § Full backup and restore, help.

**Version:** `2.x` MINOR; `agreed`.

---

## review-2026-08-followups

**Goal:** the leftovers of the 2026-08-19 full-tree code review that are too
small to be roadmap items of their own. Everything still open from that review
is in the table below; the rest of it shipped in
[CHANGELOG.md](../CHANGELOG.md) `[1.9.2]`.

| Area | Item |
|---|---|
| Security | ~~Re-authenticate (ask for the current password) before a backup download~~ — struck: agreed on 2026-10-13 as an item of its own, [backup-reauth](#backup-reauth) |
| Tests | ~~The `internal/dmarc` service layer (`service.go`, `addresses.go`) has no tests~~ — struck: a step of audit-2026-10-12 stage 4 (DMARC reports tested where they come in) |
| Tests | ~~No direct tests for the apps / domains / users / dmarc handlers; `authz_test` covers them only indirectly~~ — struck: users with stage 2 (`submitUserUpdate`), DMARC with stage 4; apps and domains get theirs when a step changes them - a test without a change finds nothing the e2e does not |
| Tests | ~~Nothing covers error page templates, a corrupted backup archive, malformed or oversized DMARC XML, delivery-log pagination edges, concurrent store operations, or zero-traffic `store` stats~~ — struck: the corrupted archive goes to stage 3, DMARC XML to stage 4, paging edges to stage 4 (`parsePage`), concurrency of the last global to stage 2; error templates, other concurrency and zero-traffic stats are dropped (owner, 2026-10-14) - code no step changes, and a test kept for its own sake finds nothing |
| Edge case | ~~`internal/secretfile` accepts an empty password at library level~~ — struck: audit-2026-10-12 stage 3 (M5) |
| Edge case | ~~The log fingerprint in `logtail/offset.go` can collide across a rotation — include the inode~~ — struck: agreed on 2026-10-13 as part of a step of [audit-2026-10-12](audit-2026-10-12.md) stage 2b (a log rotated while the panel was down is read to its end) |
| Edge case | ~~`parsePage` does not clamp to `lastPage`~~ — struck: audit-2026-10-12 stage 4 |
| Edge case | ~~`dnscheck/resolver.go` fans out four parallel queries where a fallback chain would do~~ — struck: finding M14 of [audit-2026-10-12](audit-2026-10-12.md) |
| UI | ~~`.pair` (DMARC templates) and `.split` (every other page) are two names for one layout~~ — struck: retired by [panel-redesign](#panel-redesign); both stylesheets are gone and every page is laid out by the component kit |
| UI | ~~Two `back_link` elements in a row in `dmarc_domain.html`~~ — struck: retired by [panel-redesign](#panel-redesign); the page is rebuilt on the kit with one breadcrumb in its head |
| UI | ~~`initShowWhen` is not re-run after an htmx swap~~ — struck: the show/hide rules listen on the document and are applied again on `htmx:load` |

**Boundary:** no feature work and no behaviour change an operator would notice,
apart from the backup re-auth prompt. Anything here that grows past an hour
becomes its own roadmap item instead.

**Done when:** each row is either implemented or struck with a written reason.
**Closed 2026-10-14** with the owner: every row is struck above, placed in a
stage of [audit-2026-10-12](audit-2026-10-12.md) or dropped with its reason.

**Dependencies / risks:** One finding of that review was **declined** and is
deliberately absent: `//go:build linux` for the `/proc` reader in
[internal/health/machine.go](../internal/health/machine.go). That code is
parameterised by root directory, calls nothing platform-specific, already
degrades gracefully when `/proc` is missing (`TestMachineSamplerWithoutProc`),
and tagging it would only delete cross-platform test coverage.

**Version:** no bearing on semver; `candidate`.

---

## delivery-log-storage

**Problem (owner, 2026-10-09):** the *Delivery log* box on a message's page
(`/deliveries/{id}`, mockup `out-message`) is usually empty. The lines are not
stored anywhere: `logtail.QueueLines`
([internal/logtail/logtail.go](../internal/logtail/logtail.go)) greps the
**current** `mail.log` on every page view, within its last 4 MB, and never
opens the rotated files. Rotation is daily
([build/logrotate-mail.conf](../build/logrotate-mail.conf)), so a message
older than the last rotation — in practice anything not sent today — shows
"Nothing for this queue id in the current mail log" while its send-log row
lives on for ninety days. The fourteen rotated files on disk hold the lines
and are never read.

**Goal:** a message's page shows what Postfix wrote about it for as long as
the message is in the send log.

**Options:**

1. **Store the lines** (recommended). The log tailer already reads every
   `mail.log` line once to update delivery status; a line whose queue id
   belongs to a send-log row is also appended to a new table
   (`send_log_lines`: row id, written at, text), five to ten short lines per
   message. Retention follows the row — the existing send-log sweep deletes
   them together — and the page reads the table instead of the file. Lines
   written while the panel was down are caught up the way status updates
   are, from the persisted read offset. The grep stays only as a fallback
   for rows older than the table.
2. **Grep the rotated files too.** `QueueLines` walks `mail.log.1`,
   `mail.log.2.gz`, … until it finds the id. No schema, but each view
   decompresses up to fourteen files, and past fourteen days the answer is
   still empty against a ninety-day row.

**Boundary:** outbound send-log rows only; the inbound journal
([inbound-antispam-panel](#inbound-antispam-panel)) decides for itself
whether it wants the same. No change to what the box looks like — the
`out-message` mockup already draws it as a `log_pane`.

**Done when:** a message sent before the last rotation shows its lines on its
page; the lines disappear with the row; the page wording no longer blames
rotation for an ordinary case.

**Dependencies / risks:** a queue id is reused by Postfix after a message
leaves the queue, so matching must be bounded to the row's lifetime (accepted
time to final status plus the queue lifetime), not to the id alone; the table
grows with the send log (small — a few hundred bytes per message) and is
covered by the same retention.

**Version:** shipped `2.2.0` (2026-10-12), MINOR; option 1. Decisions and
checklist: [plans/delivery-log-storage.md](plans/delivery-log-storage.md).

---

## panel-notifications

**Goal:** the panel tells its administrators when something needs them instead
of waiting to be opened. First version, four events: a sending domain's DNS
record stops matching, a level-2 rate limit refuses mail, the TLS certificate
is about to expire, a message has been in the queue for more than three days.

**Boundary:** e-mail only, from `postmaster@<hostname>`, submitted locally -
not from one of the sending domains. One checkbox per user under **Account**
(no choice per event), one instance switch under **Server › Settings**; a
domain administrator hears only about assigned domains. No streams, no
digests, no webhooks, no chat integrations. Not in the first version: a
process or milter down, an inbound upstream unreachable, DMARC failures.

**Done when:** each of the four events reaches the account e-mail of every
user who has notifications on and may see its subject, by the rules of the
plan (a "cleared" message for DNS and the queue only); a user without an
e-mail gets nothing; the switch turns it all off.

**Dependencies / risks:** the account e-mail (2.0 baseline, see
[plans/panel-redesign.md](plans/panel-redesign.md) § Account e-mail); a relay
that cannot send cannot report that it cannot send - none of the four events
is "the relay is down"; mail from the bare hostname needs an SPF record for
it to be believed, which Server › Health will check; event state must survive
restarts so a reboot does not re-announce everything. The screens are not
drawn: the Notifications box was taken out of `account` and `settings` on
2026-10-10 and is redrawn as the first step.

**Version:** shipped `2.3.0` (2026-10-12), MINOR - the design questions were closed
with the owner on 2026-10-12
([plans/panel-notifications.md](plans/panel-notifications.md)) and he took it into
work the same day.

---

## password-reset

**Goal:** a user who forgot their password gets a one-time link at their
account e-mail instead of asking the global administrator to set a new one.

**Boundary:** a link on the sign-in page, a short-lived single-use token, the
same answer whether or not the username exists. The only global administrator
without a working e-mail still recovers from the container (today's path).

**The e-mail must be confirmed (owner, 2026-10-13).** A reset link goes only
to an address its owner has confirmed; an account whose e-mail is not
confirmed has no reset, and recovers as today (the global administrator sets
a password, or the container). The panel does not confirm addresses today -
`users.email` is whatever was typed - so the item builds that first: saving
an e-mail under Account mails a one-time link to it, opening the link marks
the address confirmed (a migration: the mark on `users`), changing the
address clears the mark, and Account shows which it is. An address typed for
a user by the global administrator is unconfirmed until that user opens the
link. To settle at the mockup: whether notifications (2.3.0) keep going to an
unconfirmed address.

**Done when:** the flow works end to end, tokens expire and cannot be reused,
requests are rate-limited, an account with an unconfirmed e-mail gets no
link and the sign-in page answers the same for it, and it passes a security
review.

**Dependencies / risks:** the account e-mail; an account-takeover surface where
there was none — security review is part of the item, not an afterthought.
Not drawn yet. Changing the account e-mail must then require the current
password (2.0 security review), because the e-mail becomes where a reset link
goes and today Account saves it without the password.

**Version:** `2.x` MINOR; `agreed` (owner, 2026-10-13: domain administrators
to come need it - they are not the owner and should not have to ask him for
a new password). Mockup-first; changing the account e-mail asks for the
current password in the same cut.

---

## local-time

**Agreed with the owner on 2026-10-13.** It came from one cell: *Today* on
Health's Sender check card is the UTC day and does not say so. Offered a
label ("Today, UTC") or the local day, he chose the local day - which is a
decision about the whole panel, not one cell.

**Today:** every time the panel shows is UTC and says so (*Time, UTC* in the
logs, "UTC" after a date), and everything counted by day - the sender
check's days, the delivery-log measurement, the statistics behind the
charts, retention - is counted by the UTC day.

**Goal:** the panel shows time in a chosen time zone, and "today" is that
zone's day.

**Where the zone comes from (owner, 2026-10-13):** one setting of the
instance, *Time zone* under Server › Settings, UTC by default; it holds for
every user and for notification mail. Not the browser's zone: only a script
could read it, and the panel works without JavaScript. Not a setting of each
user yet - **that is added (the same list under Account, "as the server" by
default) when** a user of the panel lives in another zone than the one set
for the instance, which the first domain administrator who is not the owner
may bring.

**What is written beside a time (owner, 2026-10-13):** nothing. The "UTC"
labels go - the column is *Time*, a date is a date - and the zone is stated
in one place, the setting. His reason: there is nobody to send a line of the
panel to yet. **The offset is written where "UTC" is today ("Time, UTC+3",
"2026-09-21 16:40 UTC+3") when** times of the panel start to be read by
someone who does not know the instance's zone: a second user of the panel,
or lines copied out of it to another person.

**A trap this leaves, known in advance (owner asked for it to be written
down, 2026-10-13):** Server › System log, and the *Delivery log* box on a
message's page, show the lines of `mail.log` as Postfix wrote them, each with
its own stamp ("Oct 13 14:01:52"). Those stamps are the container's time -
UTC unless the container is given a `TZ` - and they are not rewritten. Once
the rest of the panel is in the instance's zone and no time carries a label,
a message that the Outbound log puts at 17:01 has lines stamped 14:01 on its
own page, with nothing on the screen to say why. The plan must close this
before the item ships, one way or another: rewrite the stamp when a line is
shown, say on those two screens which clock the lines use, or run the
container in the instance's zone (which moves `mail.log` itself and what the
tailer reads). The guide's section on time must say the same thing as the
screens.

**How a day is counted (owner, 2026-10-13):** the tables that keep a row
per day - `sender_check_days`, and `delivery_log_days` of
[audit-2026-10-12](audit-2026-10-12.md) stage 1 - key the row by the date in
the instance's zone at the moment of counting, from the release on. Rows
counted before it stay UTC days. Changing the zone later leaves the rows
already written as days of the old zone, a shift of a few hours in a
statistic; accepted - the zone is set once, and rows per hour (which would
survive a change) cost a migration for it. The charts and statistics group
the log by hour already and need nothing.

**Open, to settle at the mockups:** the trap above; which of the screens
that show a date show the year.

**Boundary:** what the panel shows and how it groups by day. `mail.log`,
the database's stored instants and the e2e stay as they are.

**Version:** `2.x` MINOR; `agreed`; the plan is written when it is taken up.

**Where the zone comes from, changed (owner, 2026-10-17):** not a setting
in the panel but the container's own zone, `TZ` in `.env`; Settings only
shows it ("Да" to "пояс в `.env`, на Settings только показ?"). His reason
was the trap above: a container cannot change its zone without a restart,
and only the container's zone moves Postfix's stamps with the rest. So the
trap is closed by the third way - the container runs in the zone, Postfix
stamps `mail.log` in it (`TZ` is in Postfix's `import_environment`), and the
tailer reads the stamps in the same zone, as it always did (`lineTime`,
`time.Local`).

**Done (2026-10-17):** design c205106 (no "UTC" on any screen, Settings box
05 *Time zone*, read-only, the help topic). The panel formats every time in
`time.Local`; the day tables key their rows by the local date, the sender
check cuts downtime at local midnights, the log filters' days run from local
midnight; stored instants stay UTC. Notification mail and the test message
write the zone's abbreviation after a time (they are read away from the
panel). The year question settled as the mockups have it. `tzdata` is named
in the image. Tests run in UTC (a `TestMain` in each package that formats
or counts days) and in Moscow where the zone is the point.

---

## phone-desktop-mode

**Problem (owner, 2026-10-11):** the panel opened on a phone with the
browser's "desktop site" switched on does not become the desktop panel.
Seen on Server › Health at `selfpost.mixfed.ru`: the page body is laid out in
columns (Machine and Processes side by side, three cards in the next row),
while the navigation is the phone one - not a bar but a stack with every
section open (Outbound, Inbound, Server, the user menu), filling the first
screen before the page starts. The content also sits flush against the left
edge of the screen.

**Goal:** find out why, then decide what the panel should be in that mode -
the desktop panel, or a phone panel that is at least consistent.

**Where to look first (a guess, not checked):** "desktop site" on Android
lays a page out about 980 px wide. Bulma turns the navigation into a bar at
1024 px and puts columns side by side from 769 px, so 980 px falls between
the two: desktop columns under a touch navigation. `panel.css` has its own
breakpoints at 768, 1023 and 1407 px. Check the width the phone really
reports, why every dropdown is open rather than folded behind the burger,
and where the left padding went.

**Boundary:** the navigation and page gutter between 769 and 1023 px, which
is also a tablet in portrait and a narrow desktop window - the same defect is
expected there. A design change is mockup-first
([plans/panel-redesign.md](plans/panel-redesign.md) § Enforcement).

**Done when:** the cause is written down here, and the panel at that width
has one coherent layout, checked on a phone in "desktop site" mode and in a
980 px window.

**Cause (2026-10-16):** not the mode - every width under 1024 px. The bar's
menu was always open (`navbar-menu is-active`, and no burger to close it),
and Bulma lays a navbar menu out below 1024 px as a block with every
dropdown open: on any phone the sections and all their pages filled the
first screen. "Desktop site" makes the page about 980 px wide, where Bulma
already puts columns side by side (from 769 px) but not yet the bar - the
mix in the screenshot. And a Bulma `.container` has side margins only from
1024 px, so below it the page met the screen's edge.

**Done (2026-10-16):** the owner chose "Вариант 1" - a menu behind a button
- with "сворачивание разделов". Below 1024 px the bar's menu is hidden and
*Menu* at the right of the brand opens a panel over the page: Overview, each
section a fold with the current one open, the user's last (Account, Help,
Sign out); `<details>` all through, no script. The page and the strip of
sibling pages have a 1rem gutter. From 1024 px nothing changed. Mockup
`e0e30e2`.

**Version:** `2.x` PATCH or MINOR; `agreed` (owner, 2026-10-13), taken up
after stages 1 and 2 of [audit-2026-10-12](audit-2026-10-12.md) - mail shown
wrong and the tenant boundary come before layout.

---

## from-header-binding

**Problem (security review of panel-notifications, 2026-10-12):** Postfix
holds the envelope sender against the SASL login
(`reject_sender_login_mismatch`), and OpenDKIM chooses the signing key by the
`From:` header. Nothing holds the header against the login. An application
of one sending domain can therefore write another registered domain - or,
since 2.3.0, the server's own name - into `From:` and have the message signed
with that name's key: mail that passes DMARC as a domain the application has
no right to, or that looks like a notification of the panel.

**Agreed with the owner on 2026-10-12**, built and reviewed the same day
([plans/from-header-binding.md](plans/from-header-binding.md); what is not
done is listed there under *Not done*). The rule he
chose is wider than the one first written here: `From:` is held to exactly
what the envelope is held to - any address of the application's domain, or
the addresses of its list - and anything else is refused, a stranger's domain
included. The check is a role of the panel on a strict socket of its own:
while the panel does not answer, mail is deferred, not accepted unchecked.
Server › Health counts what that costs, to decide later whether the check
needs a process of its own.

**Done when:** the e2e stand shows an application refused for another
tenant's `From:`, for the hostname's and for an address off its list, and
accepted for its own.

**Version:** shipped `2.4.0` (2026-10-12), MINOR. What it costs - the
panel's downtime is now mail's - is in [security.md](security.md) § Accepted
risks.

---

## panel-redesign

**Goal:** a panel an operator opens a few times a year and still finds their
way in — the important things in view, details one or two clicks away, one
visual language, no page tuning its own width. The design is drawn and
accepted: 33 screens and a components page under
[docs/assets/panel-redesign/](assets/panel-redesign/index.html) (Bulma base,
postmark / numbered heads / perforated edge, icons). Contract and checklist:
[plans/panel-redesign.md](plans/panel-redesign.md).

**Boundary:** templates, one `panel.css`, shared partials, vendored Bulma and
icons. Navigation regroups by mail direction (Overview · Outbound · Inbound ·
Server) and paths are renamed to match. **No compatibility is kept** (owner,
2026-09-21): no redirects, the migration chain is replaced by one baseline
and 2.0 starts from an empty data directory — the only live installation is
the owner's. Inbound domains become delegable to a
domain administrator, separately from outbound. Server › Health is built
with the restyle (decided 2026-10-09). Three things the mockups show keep
their own decision and are not smuggled in: queue as a table
(`postqueue -j`), inbound queue, inbound log (rides on
inbound-antispam-panel) — until then both queues ship the old content in the
new shell. No CSP change, no JavaScript widgets, no second framework.

**Done when:** every page renders with the component skeleton recorded in
`panel/outlines.json`, the guard tests in the plan are green with no
allow-list, and each page has an evidence screenshot accepted against its
mockup.

**Dependencies / risks:** an implementer substituting local patches or a look
of their own — answered by guard tests that land before the templates, a
`design-first` CI step and evidence-based acceptance (plan § Enforcement).
Competes with feature work; Bulma is ~680 kB minified, embedded once.

**Version:** shipped `2.0.0` (2026-10-11), MAJOR.

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

**Trigger decided 2026-09-21:** [panel-redesign](#panel-redesign) is the 2.0
cut, and the owner dropped the upgrade gate with it — the chain is deleted to
zero and 2.0 starts from an empty data directory. The
**Boundary** and **Done when** above are superseded on that point by
[plans/panel-redesign.md](plans/panel-redesign.md) § No compatibility; this
item ships as a step of that plan, not on its own.

**Built 2026-10-09** (panel-redesign stage 1): `internal/store/migrations/`
holds one baseline, `0001_init.sql`. The gate that survives is a refusal, not
an upgrade: `store.Open` rejects a database that was not created by 2.x
(`ErrForeignSchema`, tested), so a 1.x file is never half-read — the "missed
gate" risk below. See [schema-migrations.md](schema-migrations.md).

**Dependencies / risks:** a decided 2.x cut (another breaking change, or an
explicit major). Squashing the current short chain is not a reason to cut 2.x on
its own. A missed gate leaves a mid-chain 1.x database silently stuck.
**Version:** `2.x` major only; not a 1.x item.

---

## Dropped, with the condition for return

Items the owner took off the roadmap. Each names what would bring it back;
without that it is not offered again.

- **structured-logging** (dropped 2026-10-13) - move the panel from
  `log.Printf` to `log/slog` with levels and fields, one logger passed through
  constructors. Dropped because the panel's log has one reader, the owner, by
  eye in `docker logs`; the change is wide (some 225 call sites), changes no
  behaviour, and gives a new format to everything that reads the output.
  **Returns if:** the log goes to an outside collector, or it has to be
  filtered by level.
- **Certificate warnings measured by the certificate's lifetime** (left as
  it is, owner, 2026-10-14). The panel warns 14 and 3 days before a
  certificate expires, whatever its lifetime
  ([notify.go](../internal/notify/notify.go), `certFirstDays`,
  `certLastDays`). A certificate issued for less than 14 days is inside the
  first warning from its first hour, and each renewal of it is a new
  certificate and a new message. Not reachable with the 90-day certificate
  the instance has. **Returns if:** the instance takes a certificate that
  lives less than 30 days. The rule then offered: the first warning at a
  quarter of the lifetime left, the last at a tenth, and never earlier than
  the 14 and 3 days of today - so a longer certificate, a yearly one
  included, is warned about exactly as now.
- **contributing** (dropped 2026-10-13) - a `CONTRIBUTING.md` in the root:
  the dev loop, the checks before a PR, the commit protocol. Dropped because
  there is one developer and no PRs, and everything the file would say is in
  [development.md](development.md); a second file is a second copy to keep
  in step. **Returns if:** the first outside PR arrives, or the owner decides
  to invite contributors.
- **A check that ports 25, 465 and 587 are reachable** (dropped 2026-10-13,
  from the former *preflight*). The panel can only dial its own public
  address from inside the container: behind NAT without hairpin that shows an
  open port as closed, and with hairpin it shows as open a port the provider
  blocks from outside. A card that is wrong both ways is worse than none, and
  an honest check needs a probe outside the server, which is outside the
  product ([product.md](product.md)). The administrator's browser cannot do
  it either: 25, 465 and 587 are on the Fetch standard's list of blocked
  ports. Two substitutes were offered and declined the same day: a Health
  line "last connection from outside on each port", read from `mail.log`, and
  a command in the help to run from one's own machine. **Returns if:** the
  owner accepts an outside probe, or asks for one of the two substitutes.
- **preflight as a page of its own** (dropped 2026-10-13): see
  [health-setup-checks](#health-setup-checks). Its plan,
  `plans/preflight.md`, was removed and stays in git history.
- **csrf-tokens** (decided 2026-10-13) - session-bound tokens on every form.
  Replaced by [origin-strict](#origin-strict); the conditions for building
  tokens after all are in [security.md](security.md), ADR § *CSRF via origin
  checking*.

---

## Ideas, not on the roadmap

Thoughts kept so they are not lost. They are not candidates: nothing here is
waiting for an OK, has a version or takes a place in the order above. An idea
joins the index only when the owner says so.

- **Inbound quarantine** (moved here by the owner on 2026-10-12: "an idea to
  think about") - hold suspicious inbound mail, review it in the panel, then
  release it to the upstream or discard it. It would take the relay from
  passing mail on to storing it for a while, against "no mailboxes" in
  [product.md](product.md). Notes and open questions:
  [plans/inbound-quarantine.md](plans/inbound-quarantine.md); the screens
  `in-quarantine` and `in-quarantine-message` are drawn and stay unbuilt.

## dns-record-collapse

**Problem (owner, 2026-10-15):** a DNS record block - the DKIM, SPF and
DMARC records on an outbound domain's page, the server's records on
Server › Settings, the MX of an inbound domain - shows the host, the type
and the value to publish whether or not the record is already right. When
DNS holds the record as it should be, the values are of no use on the page:
they only push the records that need work further down.

**Goal:** a record whose check is *ok* is drawn folded - its name and its
tag, one line - and opens on request to show the host, type and value (to
copy them again, or to compare). A record that is missing, wrong or not
checked yet stays open, as it is now, with *In DNS now* where it differs.

**Notes:** the block is the one `dns_record` partial (components.html), so
the change is made once; it is a design change, mockup first (the kit's
dns_record section and the four pages that draw records), wording
included. No JavaScript needed for the folding itself: `<details>` opens
and closes without it under the panel's CSP. Whether a fold remembers being
opened across the polls of a page, and whether *Re-check DNS* folds a record
that has just turned right, are for the mockup.

**Done (2026-10-16):** a record whose tag is *published* and that has no
*In DNS now* is a `<details>` whose summary is the head: name, tag and
*Show values* (the eye of the icon subset - it has no chevron), the
values behind it. The head's note is hidden while folded, or a long one
(the server's SPF in a half column) wraps the fold to two lines; it shows
when opened. The two questions answered themselves: no page that draws
records polls them, and *Re-check DNS* posts and draws the page again, so a
fold is never kept across a refresh and a record that has just turned
right comes back folded.

## notification-mail-recheck

**Problem (owner, 2026-10-15):** Server › Settings shows the DNS records of
the server's own name in its *Notification mail* box (DKIM and SPF, with
their tags), and nothing on that page says how to check them again after
publishing. The way exists elsewhere: *Re-check DNS* on Server › Health
re-runs the server's checks, these records' included
(`HandleHealthRecheck` → `serverMail(true)`), and Health has a Notification
mail card of its own - but from Settings it cannot be found.

**Goal:** the records can be checked again from where they are shown - a
*Re-check DNS* action in the box's head, or at least the box saying where
the check is and how long a result is kept. Design change, mockup first;
decide together with [dns-record-collapse](#dns-record-collapse) (a record
that turns right on the re-check folds).

**Done (2026-10-16, the owner: "Да, бери notification-mail-recheck"):**
*Re-check DNS* in the page head of Settings - where an outbound domain's
page and Health put the same action; a box head holds a note, a tag, a
link or a filter form - posting to `POST /server/settings/dns-recheck`
(global role), which re-runs Health's lookups (the server's address and
`serverMail(true)`) and returns to Settings with "DNS re-checked."; the
Notification mail box's end slot reads "The server's own name · checked
3 min ago". A record that has turned right comes back folded.

## own-mta-builds

**Question (owner, 2026-10-15):** is it worth patching Postfix and OpenDKIM
and shipping our own builds of them, to optimise something in SelfPost or to
lift a limit we work around? Weigh it, for and against; nothing is agreed
beyond the analysis.

**For the analysis** (starting points, none of them weighed yet):

- *Limits met so far* - OpenDKIM logs through syslog(3) only, hence the
  syslog relay (audit 2026-10-12, L30); the panel reads Postfix's state from
  `mail.log` lines and `postconf`/`postqueue` output; the sender check reads
  From: the way OpenDKIM does rather than asking it; the image is held to
  what Debian bookworm packages (Postfix 3.7.11, OpenDKIM 2.11.0~beta2, a
  beta that upstream has not followed with a release).
- *For own builds* - such limits lifted at the source; newer upstream
  versions than the distribution's; options compiled in or out.
- *Against* - security fixes become ours to follow and rebuild (Debian's
  security team does it now); longer and less reproducible image builds; a
  patch set to carry across upstream versions by one maintainer; the docs'
  habit of citing upstream source by version would have to cite ours.
- *Middle ways to compare* - an unpatched upstream build only where the
  distribution's version is the limit; a patch sent upstream instead of
  carried; replacing OpenDKIM with signing in the panel's own milter (Go),
  which the panel already runs for the sender check.

**Outcome expected:** a recommendation with costs - which limits a build of
our own would remove, what each costs to keep, and whether a middle way
gets the same for less.

**Outcome (2026-10-15):** [own-mta-builds-2026-10-15.md](own-mta-builds-2026-10-15.md).
No own builds: no limit of Postfix's needs a patch (the panel reads it
through the interfaces it offers), and every limit worked around is
OpenDKIM's - a project with no release since 2018, three CVEs of 2026 open
in every Debian suite and no one answering. What the question pointed at
is answered by three rows instead, in this order:
[port25-bare-newline](#port25-bare-newline) (one line, now),
[base-trixie](#base-trixie) (bookworm is in LTS since 2026-07-12 and its
Postfix 3.7.11 lacks the three 2026 security batches, which carry no CVE
ids and so will not come), and
[panel-dkim-signing](#panel-dkim-signing) (signing in the panel's own
milter; OpenDKIM, the syslog relay and the parser port go). The owner
agreed to the three and their order on 2026-10-15.

## port25-bare-newline

**Finding (own-mta-builds, 2026-10-15):** Postfix 3.7.11 has the 2023
SMTP-smuggling parameter `smtpd_forbid_bare_newline` with the default `no`
(`src/smtpd/smtpd.c:807`, "Postfix < 3.9: no"; 3.9 made `normalize` the
default and the 3.7 `RELEASE_NOTES` recommend it). The inbound relay's
`smtp/inet` block in `build/postfix-config.sh` does not set it, so with
`INBOUND_RELAY_ENABLE=true` a bare `<LF>.<LF>` inside DATA ends the
message early and what follows is read as the next transaction. Ports 465
and 587 are not the case that matters: there the smuggled transaction is
still the authenticated session's, under its bindings and milters.

**Goal:** `smtp/inet/smtpd_forbid_bare_newline=normalize`, an e2e check on
port 25 (a bare-LF DATA, one message delivered, nothing smuggled), a
PATCH release. [base-trixie](#base-trixie) would bring the default, but
the relay should not wait for it.

## base-trixie

**Problem (own-mta-builds, 2026-10-15):** the image's base,
`debian:bookworm-slim`, left regular security support on 2026-07-12 (LTS
until 2028-06-30). Its `postfix 3.7.11-0+deb12u1` is code of mid-2024;
upstream's three security batches of 2026 (June: unbounded SMTP inputs;
August: BDAT memory exhaustion, a DATA crash, a policy bypass; September:
smuggling behind a proxy filter, a Milter-reachable over-read) carry no
CVE ids, and the LTS team works from CVE ids - no bookworm upload in 2026.
Trixie's `postfix 3.10.13-0+deb13u1` has the August batch; 3.10.14 with
September's is upstream and trixie-security follows within weeks.

**Goal:** `debian:trixie-slim` and `golang:1.26-trixie`; every citation of
`3.7.11` (331 lines in 65 files) re-read against the 3.10.13 tree, with
attention to 3.10's reject logging (`sasl_method`, `sasl_username`,
`sasl_sender` on the line), the queue id on an abnormal connection end,
cleanup's `canceled` line, 3.8's `postfix(1)` stderr logging under
supervisord; `dev/upstream` refetched from trixie; the cited-package table
and `checkCitedPackageVersions`; the e2e sink image; `opendkim-tools`
dropped from the image (used by nothing at run time). OpenDKIM's signing
and From:-parsing code is the same in trixie (its new Debian patches touch
the verifier and librbl), its citations stand. One stage, a MINOR release. Also
gained: `smtpd_hide_client_session` (3.10); the owner decided on
2026-10-15 to turn it on for 465 and 587 in the same release, so the
`Received:` header no longer shows the application's address.

## panel-dkim-signing

**Problem (own-mta-builds, 2026-10-15):** OpenDKIM is a beta of 2018 with
no upstream answering (CVE-2026-100888, -100889, -101277 open in every
Debian suite, "vendor did not respond"; Debian bug #1149875 unanswered).
Every limit SelfPost works around is its: it logs through syslog(3) only
(the relay), it chooses the key by `From:` with its own parser (the port,
the 256-byte refusal, the probe scripts), its socket file outlives a
crash, its tables are reloaded by a cross-uid signal (`CAP_KILL`), its
keys must be shared through a group.

**Goal:** the panel's own milter signs - `github.com/emersion/go-msgauth/dkim`
(the e2e suite's verifier already; `relaxed/simple`, `From` over-signed,
RFC 6376 § 5.4's header set, the body hashed as it streams), the key by
the login's domain on 465/587 and by the server's name for the panel's
local submission (`non_smtpd_milters` on the same socket, strict).
Removed: the `opendkim` and `opendkim-tools` packages, `opendkim.conf`,
two supervisord programs (OpenDKIM, the syslog relay), the table renderer
and its reload, the parser port and its scripts, Health's OpenDKIM
entries, probably `CAP_KILL` - about 1,500 lines. Keys, selectors and
published DNS records unchanged; nothing for an operator to do. The e2e
oracle must be an independent verifier (`opendkim-testmsg` in the sink
image, or rspamd), not the signing library, and a message to a real
mailbox precedes the cut. Two to three stages, a MINOR release; after
[base-trixie](#base-trixie) (remote exposure first; the two touch
disjoint citations).

## settings-save-row

**Problem (owner, 2026-10-15, a screenshot of `selfpost.mixfed.ru`):** on
Server › Settings the *Save settings* button sits under the two columns, at
the left, after a long gap: the left column (retention, sending rate limits)
is short, the right one (the notifications switch, then *Notification mail*
with its records open) is long. The button saves the retention fields and
the switch; the records under the switch are not fields, and they are what
pushes the button down.

**Goal:** the button reads as the save of the fields it saves. Either the
gap goes by itself when [dns-record-collapse](#dns-record-collapse) folds
the records that are right, or the page changes - the records out of the
form's columns, or the button nearer its fields. Decide with the mockup,
after dns-record-collapse, on a page whose records are missing as well as
on one whose records are right.

**Done (2026-10-16):** shown both layouts in both states, the owner chose
"Вариант 2: форма в левой колонке, кнопка под своими полями." Folding alone
closed the gap only while the records are right. The form is now the left
column - 01 Log retention, 02 Notifications, *Save settings* - and the
right column, outside it, holds 03 Notification mail and 04 Sending rate
limits.

## go-milter-upstream

**Problem (owner, 2026-10-16):** the panel builds go-milter v0.4.1 patched
(`third_party/go-milter`, its `PATCHES.md`): upstream adds every header of
a session to a map it hands to `Headers` and empties only on ABORT. Two
things follow, for every user of the library: the map of a second message
on one connection still holds the first message's headers (SelfPost met it
as audit 2026-10-12, L9, and keeps its own headers since), and the map is
bounded by no limit of its own - a message of nothing but headers, up to
the MTA's message size limit, is held whole in each milter. Upstream had no
issue about it on 2026-10-16; its last commit was 2026-04-04.

**Goal:** an issue and a PR, prepared here and posted by the owner from his
GitHub account (nothing is sent from this side):

- the issue: both effects, each with a reproduction through the library's
  own client (two messages on one connection; the map's size at EOH);
- the PR, small and compatible: the map emptied when a message starts
  (`CodeMail`), as it is on ABORT - this fixes the first effect and bounds
  the second to one message;
- in the issue, as a proposal: an option of `Server` not to keep the map,
  for a milter that reads headers through `Header` alone - what SelfPost's
  patch does unconditionally, which upstream cannot take as it is (it would
  break backends that read the map).

**Done when:** the texts and the branch are ready for the owner to post.

*Prepared (2026-10-16):* in `dev/upstream-pr` (not in git): `ISSUE.md`,
`PR.md`, a README with the steps, and a clone of go-milter on branch
`reset-headers-per-message` - one commit over `master` (0277691) that
empties the map on SMFIC_MAIL, with `TestMilterClient_HeadersPerMessage`,
which fails on `master` (the second message sees both `From` values)
and passes with it. Read for the issue: Postfix carries every message of
one SMTP session over the one milter connection smtpd opened (Postfix
3.10.13, src/milter/milter8.c:2723, LOCAL_SEND_FD of the milter socket to
cleanup; src/smtpd/smtpd.c:3475, milter_send()).
When upstream releases a version that does not keep the map (or offers the
option), `third_party/go-milter` goes and `go.mod`'s `replace` with it.
