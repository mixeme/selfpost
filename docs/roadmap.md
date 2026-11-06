# Roadmap: open work

**Status:** a working tracker of extensions to the product boundary
([product.md](product.md),
[.cursor/rules/agent-rules.mdc](../.cursor/rules/agent-rules.mdc)), each taken
up only after explicit agreement. Items marked `candidate` need an OK before
any code is written; detailed design, when an item needs it, lives in
[plans/](plans/). A shipped item leaves this file: what it did is in
[CHANGELOG.md](../CHANGELOG.md), its section and plan stay in git history.

**Reading this from outside the project:** nothing here is a commitment or a
release promise. There are no dates, the order is a recommendation rather than
a schedule, and an item can be dropped or reshaped once its plan is written.
What the project *will not* do is a separate question, answered in
[product.md](product.md) — an item's absence from this file does not mean it is
planned but unlisted.

**Versioning:** SemVer in the 2.x line — MINOR for a feature or a migration,
PATCH for a fix. A major only for an explicit break; `2.0.0` was one
([schema-migrations.md](schema-migrations.md) § 2.0 starts from an empty data
directory).

**Process:** [development.md](development.md).

---

## Index

| ID | Topic | Status | Progress | Plan |
|---|---|---|---|---|
| security-audit-2026-10-17 | Stage H of the 2026-10-17 security audit: the test server's host (S1, S2, S5) and its DNS (S3) | stages 0-6 shipped `2.14.4`, the product side of S3 `2.15.0`; stage H the owner's | 27/29 | [security-audit-2026-10-17.md](security-audit-2026-10-17.md) § Stage H |
| go-milter-upstream | An issue and a PR for go-milter: its header map outlives a message and is unbounded | prepared 2026-10-16, the owner posts | — | [third_party/go-milter/PATCHES.md](../third_party/go-milter/PATCHES.md) |
| audit-2026-11-05 | The three reviews of `2.16.13` — the full-tree audit, the second code review and the external security audit — consolidated into one triage: 4 mediums, 8 lows, 8 nits, the test suite's cost | agreed — triaged 2026-11-06, in progress | 5/29 | [plans/audit-2026-11-05.md](plans/audit-2026-11-05.md) |

After a context reset, pick an item marked `agreed` or `in progress`, then work
the **Implementation checklist** in its linked plan. The `Progress` column above
is `done/total` checklist steps in that plan ([development.md](development.md)
§ Plan checklists).

---

## security-audit-2026-10-17

The audit's stages 0-6 shipped together as `2.14.4`, the server's DMARC hint
(the product side of S3) and Verified TLS to the upstream (M7) as `2.15.0`.
What is left is stage H, the owner's host and DNS, about an hour of his time
and no product code: S1, S2 and S5, whose commands are in
`dev/security-audit-2026-10-17-host.md` (not published), and S3's records -
`_dmarc.selfpost.mixfed.ru TXT "v=DMARC1; p=reject; rua=mailto:<an address
you read>"` and `mixfed.ru CAA 0 issue "letsencrypt.org"`. The report's
§ Stage H has the "done when" of each.

---

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

---

## audit-2026-11-05

The full-tree audit of `2.16.13`
([audit-2026-11-05.md](audit-2026-11-05.md)) found nothing above medium, and
nothing that loses mail, crosses a tenant boundary or weakens an
authentication. The two mediums: **FT1**, five external commands in request and
probe paths that have no deadline while every other one in the tree does
(`supervisorctl status` and `postconf` in the polled Health fragment and in
`/healthz`, `saslpasswd2` and `db_dump` in the application forms) — a child
that stops answering leaks a goroutine and a process per poll against
`pids_limit: 1024`; and **FT2**, the compose build fallback whose context is
the deployment directory's parent, which in the layout the guide's own step 1
creates is the operator's home directory. Four lows (a reconcile sweep that
re-reads the log per closed row, a line of `architecture.md` about
`user_links`, an N+1 on the polled Outbound log, a queue test that races a
product constant) and four nits are in the same file, with the order this
reader would do them in at its end.

The same release has a second whole-tree review, committed to `main` while this
one was running: [code-review-2026-11-05.md](code-review-2026-11-05.md)
(`cbc7c00`) — two mediums of its own (**M1**, a password the panel asks for and
then cannot hash; **M2**, the release procedure written twice in
[development.md](development.md), the copies contradicting each other and
`release.yml`), five lows and four nits. The two lists overlap at one finding
(its L2 is FT5 here); § *The other review of the same date* in the audit file
reconciles them. The external security audit of the same date
([security-audit-2026-11-05.md](security-audit-2026-11-05.md)) added nothing
above low: its one note (**E1**, the absent absolute session cap not recorded
under Accepted risks) was the owner's to place or decline.

**Triaged 2026-11-06** — all three folded into one plan,
[plans/audit-2026-11-05.md](plans/audit-2026-11-05.md): the four mediums and
the panel-edge lows first, the nits all taken (FT7, FT8, FT10 included), E1
placed under Accepted risks, the test suite's five speedups in their measured
order, and no refactoring — the audit's § *Refactoring and optimisation*
declines it, and the `SetMaxOpenConns(1)` revisit trigger stands as written.
The offered second line-by-line reads (milter internals, the view layer) were
declined. The plan's checklist is the list to work from; the three reports
stay as the record of what was read and what held up.

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
- **A domain administrator's rows in the queues** (decided 2026-10-18:
  "Пока только Global. Пересмотр по необходимости."). Since `2.16.0` both
  queues are tables split by the recipient's domain, so the rows of a
  domain could be shown to the administrator it is assigned to, as the logs
  are; Outbound › Queue and Inbound › Queue stay the global role's.
  **Returns if:** the owner asks for it. The work then: the rows filtered by
  the user's reach in the handler (outbound by the send-log row's domain or
  the sender's, inbound by the recipient's), the menu entry and the help
  topic shown to a `domain` user, and the per-route 404 tests turned into
  reach tests.
- **csrf-tokens** (decided 2026-10-13) - session-bound tokens on every form.
  Replaced by origin-strict (`2.10.4`); the conditions for building
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
