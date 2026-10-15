# Plan: panel-dkim-signing

**Status:** agreed, 2026-10-15 - the row is agreed (owner, 2026-10-15, the
order port25-bare-newline -> base-trixie -> panel-dkim-signing); three
questions below were settled one at a time the same day.  
**Date:** 2026-10-15  
**Version:** `2.8.0` MINOR (after base-trixie, shipped `2.7.0`). No migration;
nothing for an operator to do.

---

## Goal

The panel signs outgoing mail itself, in its own milter, and OpenDKIM
leaves the image. The analysis ([own-mta-builds](../own-mta-builds-2026-10-15.md)
§ 3 E) and the roadmap section give the reasons: OpenDKIM is a 2018 beta
with three open 2026 CVEs and nobody upstream; every OpenDKIM limit
SelfPost works around goes with it (the syslog relay, the From: parser
port and its 256-byte refusal, the tables reloaded by a cross-uid signal
and `CAP_KILL`, keys shared through a group).

What stays the same for an operator: the keys, the selectors, the DNS
records, the panel's pages (one label on Health excepted, Q3).

## What the signer does

- **Library:** `github.com/emersion/go-msgauth/dkim` v0.7.0 (MIT; the
  author of the go-milter v0.4.1 we run; the e2e suite verifies with its
  v0.6.8 today). Read for this plan, `dkim/sign.go`: `NewSigner` hashes the
  body as it is written (an `io.Pipe` into a goroutine), so a message is
  not buffered; `HeaderKeys` is written to `h=` in the order given, each
  name picked last instance first (`header.go`, `newHeaderPicker`), and a
  name listed once more than it occurs is kept in `h=` with nothing hashed
  for it - the over-signing RFC 6376 § 5.4.2 describes; RSA-SHA256; the
  signature is the caller's to insert.
- **What is signed:** `relaxed/simple`, as `build/opendkim.conf` signs
  today. `h=`: every header of the message whose name is in OpenDKIM's
  `dkim_should_signhdrs[]` (`libopendkim/dkim.c:223-246`: from, reply-to,
  subject, date, to, cc, the five resent-*, in-reply-to, references, the
  seven list-*), each occurrence, in message order, plus `From` once more
  (`OversignHeaders From` today). Absent names are not listed, as
  OpenDKIM does not list them.
- **Which key:** on 465/587 the key of the domain the SASL login belongs
  to - `{auth_authen}`, then `Bindings.DomainOf(login)`, the table the
  sender check already reads (`internal/milter/sender.go:262-269`). For the
  panel's own notifications (`sendmail(1)`, `non_smtpd_milters`) the key of
  the server's name, as `EnsureServerSigned` provides it today. The sender
  check has already held `From:` to that same domain, so the signature's
  `d=` is the `From:` domain DMARC aligns on.
- **What Postfix hands a milter,** read in Postfix 3.10.13,
  `src/milter/milter8.c:2329-2400`, `milter8_header()`: Postfix's own
  `Received:` is not shown (and an inserted header at index 0 lands above
  it, "relative to the message as delivered"); a multi-line header comes
  as text separated by `\n`; one space after the colon is eaten unless the
  milter asks for `SMFIP_HDR_LEADSPC` (`OptHeaderLeadingSpace`, which the
  sender check already asks for); body records end in `\r\n`
  (`milter8_body()`, :2470-2500). The signer rebuilds each header as
  `Name:` + value with `\n` made `\r\n` - go-msgauth's own
  `cmd/dkim-milter` writes `name + ": " + value + "\r\n"` and would sign a
  folded header wrongly. A unit test signs folded, tab-led and
  space-free headers and an independent verifier checks them.
- **When it cannot sign** (no key file, a login with no domain, a read
  error): temporary failure, `default_action=tempfail`, as OpenDKIM is
  attached today. A message is never sent unsigned by accident.
- **8-bit mail:** `disable_mime_output_conversion=yes`. Postfix converts an
  8-bit message to 7-bit for a server that does not announce 8BITMIME
  (default `no`, `src/global/mail_params.h:3088`), which breaks any body
  hash - OpenDKIM's today as well. Not set until now; set with this row.

## Decisions settled before this plan

From the roadmap section and the analysis, agreed with the row: the
library; `relaxed/simple`; the header set and `From` over-signed; the key
by the login's domain and by the server's name for local mail; strict;
the removals below; keys, selectors and records unchanged; the e2e oracle
an independent verifier; a message to a real mailbox before the cut.

Decided here (technical, the owner may overrule):

- **The oracle is dkimpy** (`python3-dkim` 1.1.8-2 in trixie,
  `/usr/bin/dkimverify`, seen in `debian:trixie` 2026-10-15) - a separate
  implementation in another language, maintained, packaged by Debian; not
  OpenDKIM's `opendkim-testmsg` (the code this row removes) and not
  go-msgauth (the signer's own library). It runs in the e2e sink's
  network against the stand's CoreDNS.
- **A cross-check before OpenDKIM goes:** on the stand of stage 1 both
  sign the same messages for a while (OpenDKIM after the panel in the
  chain) and the two signatures' `bh=` and `h=` are compared on every
  e2e message; they must agree before stage 2 removes OpenDKIM.
- **The From: parser port goes.** It exists so that the sender check
  judges the address OpenDKIM chooses the key by. With the key chosen by
  the login, the check keeps `net/mail` (one RFC 5322 address at an
  allowed domain) and the 256-byte refusal goes; the refusal texts that
  name OpenDKIM are rewritten.

## Open questions (to the owner, one at a time)

- **Q1. Where the signer runs.** (a) Its own role in the panel process on
  two sockets of its own: `sign.sock` for 465/587 (the key by the login)
  and `sign-local.sock` for local submission (the server's key only), in
  the chain where OpenDKIM is now: sender check, signer, journal. (b)
  Inside the sender check's milter: one socket fewer, but the check would
  receive every body, and local mail - which has no login - would need the
  same milter to tell "no login" from "local", deciding a key by the
  absence of something. **Recommended: (a)** - the socket a message comes
  through, not a missing login, decides that the server's key may sign
  it; the sender check stays a header-only milter; and moving the signer
  into a process of its own later is a change of where it is started.
  **Decided (owner, 2026-10-15): (a).**
- **Q2. The key directory's name.** `/data/opendkim/keys/<domain>/<sel>.private`
  stays as it is, or becomes `/data/dkim/...` (the entrypoint moves the
  tree once; KeyTable and SigningTable are deleted). A backup restores
  only into the image of its own version, so no older archive meets the
  new name. **Recommended: rename** - the cost is the entrypoint's move,
  the paths in the code and the backup tests; a directory named after a
  program the image no longer has would mislead whoever reads `/data`.
  **Decided (owner, 2026-10-15): rename to `/data/dkim`.**
- **Q3. Health.** The Overview's socket check and the process list name
  OpenDKIM and the syslog relay. **Recommended:** the socket row becomes
  "DKIM signer" (the panel's `sign.sock`, checked the same way), the two
  programs leave the process list. A mockup first, as for any page
  change.
  **Settled 2026-10-15:** shown to the owner as a picture; it follows from
  the card's own rule (a row per milter socket Postfix needs, and the
  signer is strict), so it is not his choice - the two rows as drawn.

## Stages

### Stage 1 - the signer, beside OpenDKIM (no release) - done `14de9a0`

- [x] `internal/milter/sign.go`: the session (Connect, MailFrom with
  `{auth_authen}`, Header, Headers, BodyChunk, Body), `OptHeaderLeadingSpace`,
  `InsertHeader(0, "DKIM-Signature", ...)` (the server's `Actions` gain
  add-header), the key read per message from the key file (a small cache
  keyed by path and mtime), the selector from the store as `Record` reads
  it.
- [x] Unit tests: sign and verify with go-msgauth; folded and tab-led
  headers; a header absent and present twice; an empty body; a body
  without a final newline; a login with no domain and a missing key file
  answer tempfail; the server's socket never signs with a domain's key.
- [x] `cmd/panel`: the role, its two sockets (`listenMilter`, group
  selfpost, 0660), the wrapper waits for them.
- [x] `build/postfix-config.sh`: `smtpd_milters` = sender, sign, OpenDKIM,
  journal (OpenDKIM kept for the cross-check); `non_smtpd_milters` =
  sign-local, OpenDKIM; `disable_mime_output_conversion=yes`.
- [x] e2e: dkimpy in the stand; every positive send and the notification
  verified by dkimpy against the panel's signature; the panel's and
  OpenDKIM's `bh=`/`h=` compared.

*Done (2026-10-15, `14de9a0`):* e2e exit 0 on the server; on an
application's message and on a notification the panel's and OpenDKIM's
signatures agree on `h=`, `bh=`, `c=`, `s=`, `a=` and dkimpy verifies both.
Two findings while writing it: a folded header reaches a milter
`
`-separated (the signer rebuilds CRLF), and go-milter calls no Abort on a
dropped connection (a one-hour watchdog closes the signer).

### Stage 2 - OpenDKIM removed (no release)

- [ ] Chain: OpenDKIM out of both milter lists.
- [ ] Go: `internal/domain/opendkim.go` reduced to key storage (tables,
  `Rebuild`, `assertConfigSafe`, the USR1 reload go), `internal/supervisor`
  (its only caller is the reload), `internal/syslogrelay`,
  `internal/milter/mailparse.go` and its tests and testdata, the
  sender-check texts; Resync's "OpenDKIM's tables" step; Health's
  liveness set and socket row (Q3); `OPENDKIM_SOCKET`/`OPENDKIM_DIR`.
- [ ] Image: the `opendkim` package, `opendkim.conf`, the two supervisord
  programs, the `/dev/log` link and `/run/opendkim`, `usermod -aG selfpost
  opendkim`, crashexit's spared list; `deploy/docker-compose.yml`'s `KILL`
  capability (its only use was the reload).
- [ ] Keys: `/data/opendkim/keys` -> `/data/dkim/keys` (Q2): the entrypoint
  copies the tree once and leaves the old one for a rollback to 2.7.x until
  the next release; KeyTable and SigningTable go with the new layout.
- [ ] Tests: `test/upstream/*.sh`, e2e's OpenDKIM log and process checks,
  the 256-byte test rewritten for the new rule.

### Stage 3 - documents, the canary, the cut

- [ ] architecture § OpenDKIM -> § DKIM signing; security.md (the
  mail-path bullets, the accepted risks that were OpenDKIM's); guide;
  development's cited-package table (opendkim and libopendkim11 leave);
  the assumptions register's header notes what left; help (design commit
  first) and Health's label (Q3, mockup first).
- [ ] Reviews: technical and security.
- [ ] On the test server: a day of the owner's mail; one message from an
  application through 465 and one notification to a real mailbox, the
  headers read (dkim=pass with the panel's `d=`).
- [ ] Cut `2.8.0`.

## Risks

- **A signing defect fails DMARC for every tenant at once.** The answer is
  the oracle (another implementation), the cross-check against OpenDKIM on
  the same messages before it goes, and the canary before the cut.
- **go-msgauth is small and released slowly.** It can be vendored if it
  ever matters; the signing path is about 300 lines of it.
- **Rollback:** until stage 2 ships, the chain change is one line; after
  the cut, the previous image (2.7.x) still has OpenDKIM and reads the same
  keys - under Q2's rename, from the old path, which the move leaves as a
  copy until the next release.
