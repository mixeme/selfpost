# Changelog

All notable changes to this project are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning follows [SemVer](https://semver.org/).

## [Unreleased]

### Upgrading

- **Give the TLS private key to group 10997** (audit I1): `chgrp 10997
  certs/privkey.pem` - or certbot's `archive/<name>/privkey*.pem` - and the
  same in whatever renews it. Group 10998, which the guide named until now,
  has Postfix in it; the backup keeps working with either, but only 10997
  keeps the key from Postfix's daemons.

### Security

- **One stranger behind a shared proxy no longer locks everyone out of
  signing in** (audit A1, A8, A9): sign-in counts failures per address and
  username (a hundred per address alone), a right password no longer
  counts, a link's page is limited per link, a reset request per address
  and name, and an IPv6 client is one per /64. With every request coming
  from one private address with `X-Forwarded-For` and `TRUSTED_PROXY_CIDR`
  empty, the log says what to set. Refused sign-ins are logged.
- **Reset links pause for a day after five** (audit A3): anyone who knew a
  username could keep a live reset link arriving in that mailbox five times
  an hour. After five in a day the owner is told once that further requests
  are ignored for 24 hours.
- **`/healthz` is not answered through the reverse proxy** (audit I6):
  only a request with no `X-Forwarded-For` from loopback or a private
  address gets the liveness answer - the container's own check, the host,
  a monitor next to it; the internet gets `404`.
- **Every container has a process ceiling** (audit I3): `pids_limit` 1024
  for SelfPost, 256 for the proxies and rspamd, 64 for certbot.
- **A save that changes no map no longer reloads Postfix, and DNS
  re-checks are limited** (audit W6/M9): every application, upstream,
  recipient or report-address save reloaded Postfix, and *Re-check DNS*
  bypassed the cache without limit. Now only a changed file is written and
  reloaded (Health's *Reload configuration* always re-reads), and a user
  re-checks twenty times in 15 minutes.
- **The TLS private key no longer has to be readable by Postfix** (audit
  I1): the guide gave it to group 10998 for the panel's backup, and
  `postfix` is in that group, so every Postfix daemon - an smtpd talking to
  the internet included - could read it. A new group, `backupkey` (10997),
  holds the panel alone; the guide, Help and the backup's refusal name it.

- **A stolen `global` session no longer takes a full backup** (security
  audit 2026-10-17, A2, high): the user form set the signed-in user's own
  password without the current one, which then passed the backup's password
  check. Adding a user, setting a password, changing an e-mail or a role on
  the user form, and deleting a user now ask for your own account password,
  as a backup does, under the same limit on wrong guesses - which now
  counts wrong passwords only.
- **A domain's rate limit is taken only by its own applications** (audit
  M1): the level-2 domain limit was read from `MAIL FROM`, which reaches the
  milter before Postfix holds it to the login, so a client that never
  signed in, or another tenant's application, could use up a domain's
  limit. It is now the signed-in application's domain.
- **One accepted message is one Inbound › Log row per recipient** (audit
  M3): a recipient repeated in RCPT TO, up to Postfix's thousand, was a row
  each, and the journal milter's rows did not count toward the 10 000-row
  cut between the six-hourly prunes. Repeats are one row, a message's rows
  are written in one transaction, and both writers count toward the cut.
- **A domain's custom DMARC report address may not be on the server's own
  name** (audit W1): it opened a mailbox the whole internet could file
  reports into, under any domain, or another domain's hosted one. Only the
  hosted address of a domain that chose it is a mailbox now, and a report
  must arrive at the address tagged with its domain.
- **A domain's DMARC reports are deleted with it** (audit A5/W2): reports
  belong to a domain by name, and a domain deleted and added again showed
  its old reports to whoever it was assigned to then.
- **The sender map is rebuilt one change at a time** (audit W3): two
  application changes at once could leave the map without the newer one
  until the next change.
- **A stranger's recipient no longer writes an Inbound › Log row** (audit
  M2): Postfix quotes a refused recipient in its reply with the quotes
  removed, so `RCPT TO:<"; from=<x> to=<y> …"@domain>` spelled a second
  sender and recipient there, and the parser took the first. It now takes
  Postfix's own tail at the end of the line.
- **The sender check keeps two `From:` and two `Sender:` lines** (audit M5),
  not every one an application sends: the judgement needs no more, and a
  hundred long lines were megabytes held per connection.
- **A link's token stays out of the duplicate-cookie log line** (audit A4),
  as it already did out of the origin check's.
- **ETRN is refused and no longer offered** (audit S6): Postfix's default
  let anyone on port 25 flush the queue toward an inbound domain's upstream.
- **SECURITY.md names the 2.x line as the supported one** (audit X1); its
  table still said 1.x.
- **The entrypoint follows no link at the DMARC maps** (audit I2): the
  three `dmarc_*` maps root creates at the start were missing from the list
  of paths where a symbolic link is removed first.

## [2.14.3] - 2026-10-17

A tag beside a label. No migration; schema 13.

### Fixed

- **A tag beside a label no longer squeezes the field under it**: the
  e-mail's "not confirmed" on Account made its label taller and left less
  room above the field than Username has (the owner's phone).

## [2.14.2] - 2026-10-17

The last small items of the day. No migration; schema 13.

### Added

- **Health dials the inbound journal's socket** with the inbound relay on:
  the milter that writes Inbound › Log was the one the panel did not check.
  The send log's socket is named as the mockup names it, `journal-milter`.

### Changed

- **A DMARC report says which day it is**: its day stays the reporter's, in
  UTC (owner, 2026-10-17: accepted as it is), and the report's page now says
  so under its window; Settings and Help name it as the one exception to
  the panel's zone. A daily report from midnight to midnight is one day, not
  two.

### Documentation

- The guide's gaps from the docs audit: `/license` and `/server/components`,
  DMARC pages' paths, `selfpost-backup -o`, `-i` and `-version`, the
  Inbound log's retention field, `PANEL_COOKIE_SECURE`.

## [2.14.1] - 2026-10-17

Overview's head in the mockup's words. No migration; schema 13.

### Changed

- **Overview's head says it as the mockup does** (overview-head): "Mail is
  flowing. One thing needs attention." - or "Nothing needs attention", or
  "Mail may not be flowing" when a check fails - and under it each check
  that is not fine with its value ("TLS certificate: expires in 12 days.").

## [2.14.0] - 2026-10-17

Password reset by e-mail, to a confirmed address. Migration 0013; schema 13.

### Added

- **Password reset by e-mail** (password-reset): *Forgot your password?* on
  the sign-in page sends a one-time link, for 30 minutes, to the account's
  e-mail - once that address has been confirmed by a link sent to it. A new
  address is sent its confirmation link when it is saved (Account, or the
  user form); Account shows whether it is confirmed and can send the link
  again. Notifications go to the address confirmed or not. Setting a new
  password from a link signs every session of the account out. Migration
  0013; schema 13.

### Changed

- **Changing your e-mail on Account asks for your password**, as changing
  the password does: the address is where a reset link goes.

## [2.13.1] - 2026-10-17

The documents and the panel's words checked against the code. No migration;
schema 12.

### Fixed

- **A panel password's twelve characters are characters**: the minimum
  counted bytes, so six letters of a non-Latin alphabet passed for twelve.
  It counts characters now, as the backup password always did. Passwords
  already set are not affected.

### Changed

- **The panel and its mockups say the same** (docs-audit): an inbound
  domain's MX box says when it was checked and its side menu links the
  domain's Inbound log; a deferred message's history ends with the wait in
  the queue; Overview says it refreshes on its own; DMARC's hint for an
  unaligned third party reads as the mockup. Help says what the code does:
  an application's password is kept by the server so an export can carry
  it, every password check of a backup counts, a password set for another
  user signs them out everywhere, the limit mail is daily per sending
  domain, and a domain administrator is no longer shown the Filter lists
  topic (their page is the global role's).

### Documentation

- The documents checked against the code (docs-audit, 2026-10-17): the
  guide, architecture, schema-migrations, security, development, README and
  product say what the code does.

## [2.13.0] - 2026-10-17

The panel in the container's time zone. No migration; schema 12.

### Added

- **The panel in your time zone** (local-time): set `TZ` in `.env` (an IANA
  name, e.g. `Europe/Moscow`) and every time the panel shows, every day it
  counts - today on Health, the statistics' days, the days of the log
  filters - and the stamps of `mail.log` are that zone's. The screens no
  longer write "UTC" beside a time; **Server › Settings** names the zone in
  a new read-only box. Unset, it is UTC as before. Changing it is a
  restart; days counted before stay the old zone's.

## [2.12.0] - 2026-10-17

A test message from Server › Health. No migration; schema 12.

### Added

- **A test message from Server › Health** (test-email): one message to an
  address you type, from `postmaster@` the hostname, the way notifications
  go. The page that opens is its row in the Outbound log, which follows it
  to the receiving server. It tests the server's own path, not a domain's.
  Global role, ten an hour.

## [2.11.0] - 2026-10-16

Health checks the certificate's name, Postfix's greeting and the reverse
proxy. No migration; schema 12.

### Added

- **Server › Health checks three things it did not** (health-setup-checks):
  whether the TLS certificate is for the server's own name - a tag beside
  its names, and a failure when it is not, since a client that checks the
  name refuses it however valid it is; Postfix's greeting, the `220` line,
  which must name the hostname; and the address the panel sees your browser
  at, warning when a reverse proxy it does not trust stands in front - every
  visitor then shares one sign-in limit - and naming what to put in
  `TRUSTED_PROXY_CIDR`.

## [2.10.4] - 2026-10-16

The origin check is strict. No migration; schema 12.

### Security

- **A change with no word of where it came from is refused**
  (origin-strict): a `POST` that carries neither `Sec-Fetch-Site` nor
  `Origin` is answered 403, as a cross-origin one is. It was let through
  before, as an accepted risk, which left a browser that sends neither open
  to a request forged by any site. Every current browser sends `Origin` with
  a form; a browser too old to (Firefox before 70) can no longer save
  anything in the panel.

## [2.10.3] - 2026-10-16

The backup asks for your password. No migration; schema 12.

### Changed

- **A backup and a domain's export ask for your password** (backup-reauth):
  both downloads take the signed-in user's account password, checked like a
  password change and under its limit, so a stolen session or a panel left
  open no longer hands over the server's every secret.

### Fixed

- **The file field of a domain's import is drawn as designed**: Bulma's
  file button and the name of the file chosen, not the browser's own
  "Browse…".

## [2.10.2] - 2026-10-16

The panel on a phone. No migration; schema 12.

### Changed

- **A narrow screen gets a Menu** (phone-desktop-mode): below 1024 px - a
  phone, a phone in "desktop site" mode, a tablet, a narrow window - the
  navigation was a sheet of every section and page, always open, filling
  the first screen. It is now *Menu* at the right of the header, which opens
  the sections as folds, the current one open, the account's entries last;
  no script. The page no longer meets the screen's edge. From 1024 px
  nothing changes.

## [2.10.1] - 2026-10-16

Postfix's queue ids no longer repeat. No migration; schema 12.

### Fixed

- **Queue ids no longer repeat**: Postfix makes long, non-repeating queue ids
  (`enable_long_queue_ids=yes`). A short id - the microsecond of arrival and
  the queue file's inode - came round once the inode did, and a later message
  could then rewrite an older row of the Outbound log with the same id and
  recipient, put its lines on that row's page, or reach the Inbound log's
  row of an earlier message. New ids read like `3Pt2mN2VXxznjll`; mail
  already in the queue keeps its short id; Outbound › Queue's Queue ID
  column is wider.

## [2.10.0] - 2026-10-16

Inbound › Log says what became of accepted mail: the upstream's answer,
its last reply and the host that gave it. Migration `0012`; schema 12.
Nothing for an operator to do.

### Added

- **Inbound › Log shows what the upstream answered** (inbound-upstream-result,
  [the plan](docs/plans/inbound-upstream-result.md)): an *Upstream* column
  beside the decision - *queued*, *held*, *sent*, *deferred* (a greylisting, an
  upstream that cannot be reached), *bounced* (refused, or expired in the
  queue) - opening the last reply, the host that gave it and when. Read from
  the delivery lines of `mail.log`, one update of the row per attempt; the
  lines of port-25 mail are still not stored. Rows logged before have no
  result. Migration `0012`, schema 12.

## [2.9.1] - 2026-10-16

Log panes read on a phone. No migration; schema 11.

### Fixed

- **Log panes wrap their lines instead of scrolling sideways**: a phone drew
  two scroll bars over the Delivery log of a message, and the end of a
  delivery line - `status=` and the reply - was out of sight. A long line
  wraps, the rest indented under its start; copied text keeps one line per
  line. The Delivery log, System log and the queue's listing.

## [2.9.0] - 2026-10-16

The panel's DNS records read faster: a record DNS already holds as shown
is one line, its values on request. Server › Settings puts its Save button
under its own fields and can re-check the records of the server's own
name. `/data/opendkim`, which 2.8.x kept for a rollback to 2.7.x, is
removed at the first start; an installation still on 2.7.x goes through
2.8.x first (guide, § Upgrading). No migration; schema 11.

### Changed

- **A DNS record DNS already holds as shown is folded** (dns-record-collapse):
  on an outbound domain's page, an inbound domain's and in Server ›
  Settings › Notification mail, a *published* record is one line - its
  name, its tag and *Show values*, which opens the host, type and value to
  copy them again. A record that is missing, weak, wrong or not checked
  stays open as before, with *In DNS now* where it differs. A `<details>`
  block, no script; *Re-check DNS* draws the page again, so a record that
  has just turned right comes back folded.
- **Server › Settings: *Save settings* sits under the fields it saves**
  (settings-save-row): the form is the left column - Log retention,
  Notifications, the button - and the right column holds what is not a
  field, Notification mail and the read-only Sending rate limits. Before,
  the button stood under both columns, after a long gap whenever the
  records of the server's own name were open.

### Added

- **Re-check DNS on Server › Settings** (notification-mail-recheck): the
  records of the server's own name can be checked again where they are
  shown, as a domain's can on its page; the Notification mail box says when
  they were checked. Before, the only way was Re-check DNS on Server ›
  Health, which nothing on Settings pointed to. `POST
  /server/settings/dns-recheck`, global role.

### Removed

- **`/data/opendkim`, the key tree of 2.7.x** (panel-dkim-signing, as
  planned for the release after 2.8.0): 2.8.x copied its keys to
  `/data/dkim` and kept it for a rollback to 2.7.x; the first start of
  this image removes it once `/data/dkim` holds a key, and says so. A
  `/data` whose keys are under the old tree only - one that never ran
  2.8.x - stops the start rather than give the server's own name a new
  key: start 2.8.x once first (guide, § Upgrading). The copy and the
  backup's exclusion of the tree went with it. A rollback from here goes
  to 2.8.x; to 2.7.x only through a 2.7.x backup.

## [2.8.0] - 2026-10-16

The panel signs outgoing mail with DKIM itself, and OpenDKIM - a 2018 beta
with open 2026 CVEs - has left the image. Keys, selectors and DNS records do
not change; the first start copies the keys to `/data/dkim`. The sender
check holds a `Sender:` line too. Nothing for an operator to do; a rollback
to 2.7.x needs one command first (guide, § Upgrading). No migration;
schema 11.

### Added

- **The panel signs outgoing mail with DKIM itself** (panel-dkim-signing,
  [the plan](docs/plans/panel-dkim-signing.md)): a milter of the panel's on
  two sockets, one for applications' mail - signed with the key of the
  domain the login belongs to - and one for the panel's own, signed with the
  server's name. `relaxed/simple`, RFC 6376 § 5.4.1's headers and `From`
  once more, as OpenDKIM signed; strict. Before OpenDKIM left, both signed
  every message on the e2e stand and agreed on `h=` and `bh=`; dkimpy - an
  independent implementation - verifies the panel's signature on every run.
  Beyond what OpenDKIM signed: `Sender` and `List-Unsubscribe-Post` (RFC 8058
  wants it signed for one-click unsubscribe), and the shown headers a message
  has - Subject, Date, To, Cc, Reply-To, Sender - named once more, so that
  one added on the way breaks the signature. A header section over 1 MiB is
  refused (`552 5.3.4`); a body line over 998 bytes, which Postfix breaks on
  delivery, is logged with its domain.

### Removed

- **OpenDKIM** - a 2018 beta with open 2026 CVEs and no maintainer - and what
  SelfPost kept around it: the syslog relay and `/dev/log`, the reload by a
  signal through supervisord, the port of OpenDKIM's `From:` parser.
  Server › Health shows the signer's two sockets in its place.

### Changed

- **The DKIM keys moved to `/data/dkim`** and are the panel's alone (0600):
  the first start copies them from `/data/opendkim`, which is kept, the
  panel's alone as well, for a rollback to 2.7.x, and left out of backups.
  Keys, selectors and DNS records do not change. The KeyTable and
  SigningTable are gone; a selector table the signer reads takes their
  place, rewritten from the database at every start. `OPENDKIM_DIR` and
  `OPENDKIM_SOCKET` are gone.
- **The sender check reads `From:` as a mail program does** and no longer as
  OpenDKIM did: the line must be one address at a domain the application
  may send as, and the 256-byte limit of OpenDKIM's buffer goes. The key is
  the login's, so no `From:` line can choose another tenant's.
- **A `Sender:` line is held to the same rule as `From:`** when a message
  has one - the address a mail program shows as "on behalf of". A message
  with another domain's address there, or with two `Sender:` lines, is
  refused with `550 5.7.1 Sender: …`, tagged `wrong Sender` in the
  Outbound log. A system writing for people outside your domains puts its
  own address in `From:` and theirs in `Reply-To:`.

### Fixed

- **An 8-bit message could leave with a broken signature.** Postfix rewrote
  it as quoted-printable for a server that does not announce 8BITMIME, after
  it was signed; `disable_mime_output_conversion=yes` now delivers it as
  signed. OpenDKIM's signatures were exposed to this too.
- **A message of nothing but headers was held whole by every milter.** The
  milter library (go-milter v0.4.1) kept each header of a message in a map
  of its own that no milter of the panel read - up to the message size
  limit, on port 25 from strangers too. The panel builds it patched not to
  (`third_party/go-milter`).
- **A rotation of `mail.log` due at the container's start logged an error.**
  It runs before Postfix does, and its `postfix reload` failed ("the Postfix
  mail system is not running"); nothing was lost - the Postfix starting
  next opens the new file - but logrotate reported a failure. It reloads
  only a running Postfix now.
- **A start on a queue of an earlier image could stop at `postfix check`.**
  The numbers the image's package accounts get depend on what else it
  installs, and the queue under `/data` kept the old ones; `postsuper`, which
  the check runs as the mail owner, could not read it. The entrypoint hands
  the queue to Postfix's users by name before the check now. The first
  rollout of this version to the test server met it and was rolled back.

## [2.7.0] - 2026-10-15

The image runs Debian trixie and Postfix 3.10.13, with upstream's 2026
security fixes that bookworm will not get. The `Received:` header no
longer shows where an application runs. The container's `selfpost` group
is 10998 and the panel user 10999: **on upgrade, `chgrp 10998` the TLS
private key in `certs/` on the host** (a certbot deploy hook too). The
panel's database is the panel's alone. No migration; schema 11.

### Security

- **The panel's database was readable by every user of the container.**
  `selfpost.db` and its `-wal` and `-shm` files were 0644 under a
  traversable `/data`; they hold the panel users' password hashes and the
  session token hashes, and Postfix's smtpd - which faces the Internet -
  and OpenDKIM run as other users there. The panel now makes them 0600 at
  every start; SQLite gives the next `-wal` and `-shm` the database's mode.
  Found by the security review of this release.
- **The entrypoint no longer follows a symbolic link the panel user could
  leave under `/data`.** Several of its steps run as root and chmod,
  create or regroup paths in a tree the panel writes; a link where a
  directory or table is expected is now removed first. No escalation was
  found - a compromised panel could only have turned it against the
  container itself.

- **The image runs Debian trixie instead of bookworm** (base-trixie).
  Bookworm left regular security support on 2026-07-12, and its Postfix
  3.7.11 lacks upstream's 2026 security fixes, which carry no CVE ids and
  so will not reach bookworm LTS. Trixie brings Postfix 3.10.13 with the
  August batch (among them a BDAT memory exhaustion, a crash by DATA after
  a BDAT error, and a policy and milter bypass after an end-of-data
  reject), Debian's security support until 2028, OpenSSL 3.5 and libmilter
  8.18.1. OpenDKIM's signing and From:-parsing code is unchanged (Debian's
  new patches touch the verifier's Authentication-Results and librbl).
  The build stage is
  `golang:1.26-trixie`.

### Changed

- **Every listener holds clients to two SMTP rules,** by Postfix 3.10's
  defaults: DATA ends only at `<CR><LF>.<CR><LF>`
  (`smtpd_forbid_bare_newline=normalize`, now on 465 and 587 too), and a
  client that sends commands ahead of the replies where RFC 2920 does not
  allow it gets `554 5.5.0 Error: SMTP protocol synchronization` and is
  disconnected (`smtpd_forbid_unauth_pipelining=yes`). An application whose
  mail library breaks either rule stops sending; the guide says so under
  Ports.
- **The code's citations of other programs name the trixie sources:**
  every place that relies on Postfix, Cyrus SASL, supervisor, logrotate or
  libmilter was re-read against the trixie package and its comment
  updated; the cited-package table in development.md and the e2e check of
  it hold the trixie versions.
- **Postfix's own start, stop and reload messages reach `docker logs`**
  as well as mail.log: since 3.8 postfix(1) writes to stderr even when it
  is not a terminal.
- **The SASL configuration's directory is set by the image:**
  bookworm's Postfix found `/etc/postfix/sasl/smtpd.conf` through a Debian
  patch that trixie dropped; trixie's package writes
  `cyrus_sasl_config_path=/etc/postfix/sasl` into its main.cf instead. The
  image now sets it itself, so the login on 465 and 587 does not hang on
  the package's file.
- **The `Received:` header no longer shows where an application runs.**
  On 465 and 587 Postfix now writes the short form, `Received: by <host>
  (Postfix) id <queue id> for <recipient>; <date>`, without the
  application's address, host name, HELO name, TLS details or login
  (`smtpd_hide_client_session=yes`, new in Postfix 3.10; the owner's
  decision). The queue id still leads to the message in *Outbound › Log*
  and to the client address in mail.log. Port 25 keeps the full header, as
  RFC 5321 requires; e2e checks both.
- **The container's `selfpost` group is 10998, no longer 998.** 998 lies
  in the range the distribution hands out to system groups (100-999): in
  the trixie image a package took it and the build failed, and on a host it
  is often a system group of the host's own, which could then read the key
  as well (on the test server's host it is `systemd-network`). 10998 is
  outside that range. The panel user is 10999:10999 for the same reason
  (it was 999:999, the host's `systemd-journal` group on the test server).
  **On upgrade, give the private key in `certs/` to
  the new group on the host** - `chgrp 10998` where the guide said
  `chgrp 998`, in a certbot deploy hook too - or a full backup is refused
  until then; Postfix reads its own copy and is not affected. The other
  accounts keep the numbers their packages give them, and the entrypoint
  hands everything under `/data` back to them by name on every start.
- `opendkim-tools` is no longer installed: nothing in the image ran it.
  `openssl` is installed by name, for the certificate reload.

- **Made ready for what trixie's programs do differently:** the log tailer
  reads Postfix 3.10's refusal lines that carry `sasl_method=`,
  `sasl_username=` and `sasl_sender=`, and cleanup's `removed (canceled)`
  for a transaction that did not complete; the rotation loop refuses on
  logrotate 3.22's "Ignoring" output, now with exit status 1 where 3.21
  gave 0; Debian's chroot sync, which trixie's postfix runs at every
  start, is turned off (`SYNC_CHROOT=` in `/etc/default/postfix`; nothing
  runs chrooted), so `/etc/resolv.conf` and the like are not copied into
  the queue under `/data`; a mail.log Postfix creates itself is `0640`, as
  the entrypoint and logrotate create it.

### Fixed

- **The retry policy shown in the panel fell back to Postfix's compiled-in
  defaults whenever `postconf` warned:** the panel read `postconf`'s
  standard error together with its values, and a warning there - an
  unused parameter in main.cf, and since Postfix 3.9 a deprecated one -
  was one value too many. The image's own main.cf sets none, so it took a
  parameter an operator added. The panel reads the values alone now and
  logs the warning. `db_dump`'s output, which the panel parses to list the
  SASL accounts, is read the same way.

## [2.6.1] - 2026-10-15

The inbound relay's port 25 ends a message only at the standard
`<CR><LF>.<CR><LF>`: the SMTP-smuggling defence Postfix 3.7 leaves off.
No migration; schema 11.

### Security

- **Port 25 could be made to read a second transaction out of a message's
  body.** Postfix 3.7 leaves its 2023 SMTP-smuggling defence off
  (`smtpd_forbid_bare_newline`, default `no` before 3.9), and the inbound
  relay's listener did not turn it on: a `.` line ended by a bare newline
  ended DATA, and what followed was read as the next transaction's commands.
  The listener now sets `normalize` - the setting Postfix recommends and 3.9
  made the default: DATA ends only at the standard `<CR><LF>.<CR><LF>`, a
  bare newline elsewhere is read as a line end. Found while reading for
  [own-mta-builds](docs/own-mta-builds-2026-10-15.md) (§ 5); `test/e2e`
  sends the sequence and finds one message at the upstream with all of it
  inside. Only installations with `INBOUND_RELAY_ENABLE=true` or
  `DMARC_REPORTS_ENABLE=true` have the listener.

## [2.6.0] - 2026-10-15

Stage 5 of the 2026-10-12 audit: the image and Postfix. The supervisor
socket is the panel's alone; Postfix takes TLS 1.2 at least, and the start
refuses settings that would kill its daemons at the first connection;
OpenDKIM's log reaches the container log and it oversigns From:; a program
that keeps crashing takes the container down; logrotate remembers what it
rotated; extract-cert.sh never replaces a certificate with nothing. A new
program, `syslog-relay`, and a new group, `supervisor`, in the image; no
migration.

### Security

- **Postfix and OpenDKIM could stop every program in the container.** The
  supervisord control socket was open to the group the two share with the
  panel for files under `/data`; it is now a group of its own, the panel's
  alone (audit 2026-10-12, M17).
- **Postfix accepted TLS 1.0, answered VRFY and named its distribution.**
  It takes TLS 1.2 at least, opportunistic or required, and asks it of an
  inbound domain's upstream where TLS is required; VRFY no longer says
  which recipients port 25 accepts; the banner is `<hostname> ESMTP`
  (audit 2026-10-12, L31).
- **A From: added on the way could sit above the signed one.** OpenDKIM now
  signs From: once more than it is present, so an added one breaks the
  signature, and canonicalises the header fields relaxed, so a relay that
  refolds them does not (audit 2026-10-12, L30).

### Fixed

- **OpenDKIM's errors went nowhere.** It logs through syslog(3) alone, and
  nothing in the container listened on `/dev/log`. A small relay program
  (the panel's binary, `-syslog-relay`) passes those lines to the container
  log, marked `syslog:`, errors as such, at most 200 a second (audit
  2026-10-12, L30).
- **A non-numeric rate limit, size limit or milter timeout killed every
  smtpd** at its first connection after `postfix check` had passed it, and a
  zero window or timeout the rate limiter or smtpd. The start now stops with
  the variable's name and what it must be (audit 2026-10-12, L31).
- **`mail.log` was never rotated on a host that recreates the container
  daily.** logrotate kept its state inside the container, so each new
  container took the log for rotated today. The state is now under
  `/data/log` (audit 2026-10-12, M19).
- **A program that crashed after its first second was restarted for ever.**
  supervisord gives up only on crashes during start-up; the container is now
  restarted when a program of the mail path exits unexpectedly five times in
  ten minutes, as it is when it gives up (audit 2026-10-12, L32).
- **`deploy/traefik/extract-cert.sh` could replace the certificate with two
  empty files** when `acme.json` did not hold the domain, and exited 0. It
  now refuses anything that is not PEM, writes the new files beside the old
  ones and renames them whole, owner-only until then, with the group the
  old files had (audit 2026-10-12, M21). It needs bash.

## [2.5.4] - 2026-10-15

Stage 4 of the 2026-10-12 audit: the panel's behaviour. Signing in tells
nothing by its timing and ends a planted session; the server bounds its
connections; monitoring pages recover from a failed refresh; the Outbound
log filters by status and by day; a notification is owed to each recipient;
a DNS outage is unknown, not wrong; Reload configuration writes everything a
restore writes. Migration `0011` (a table, nothing existing changes).

### Added

- **The Outbound log filters by status and by day**, beside domain and
  application: *queued*, *sent*, *deferred*, *bounced* or *rejected* (the
  three kinds of refusal together), and from and to a day, as the Inbound
  log does. The filters survive the pages, the refresh and the way back
  from a message.

### Changed

- **Reload configuration writes everything a restore's first start writes.**
  The button on Server › Health left the DMARC report maps and the filter
  lists out while it said the configuration was regenerated; it now writes
  them too, and a step that fails is a message on the page that names it,
  not a plain-text error (audit 2026-10-12, L13).

### Security

- **The sign-in told which usernames exist by how long it took.** A wrong
  password cost a bcrypt comparison, an unknown name none. Both now cost one
  (audit 2026-10-12, M8).
- **Signing in kept the session the browser already presented.** A sign-in
  now ends it before it issues its own, so a session planted before the
  sign-in is worth nothing after (audit 2026-10-12, L16).

### Fixed

- **Ten wrong current passwords on Account locked the address out of
  signing in for 15 minutes.** The check has its own limit now, ten in 15
  minutes per user (audit 2026-10-12, L15).
- **A client could hold the panel's server as long as it liked** once its
  request header was read. The body now has 30 seconds, the answer 60, an
  idle kept-alive connection 2 minutes; a full backup download moves its own
  limit to two hours (audit 2026-10-12, L14).
- **One failed refresh stopped a monitoring page for good.** Overview,
  Health and the logs refresh their boxes on a timer; a refresh that failed
  - the panel restarting, the network gone - scheduled no next one, and the
  page stood still until the tab was hidden and shown again. Each refresh
  now schedules the next however it ended, waiting twice as long after each
  failure in a row, up to a minute. A box whose session had expired was
  filled with the sign-in page; the whole page goes to sign-in now (audit
  2026-10-12, M11).
- **The Outbound log showed an empty table under "Page 9 of 2"** for a page
  past its end - a link kept while old rows were pruned, or a number typed.
  It shows the last page, as the Inbound log does (audit 2026-10-12, L10).
- **An auto limit's multiplier took "NaN"**, stored nothing, and was
  recalculated with the default without a word. It is refused like any
  number out of range (audit 2026-10-12, L11).
- **A message refused under a rate limit was a grey tag** beside its two
  red siblings, *wrong From* and *wrong client*. It is red (audit
  2026-10-12, L18).
- **Recalculate rate limit dropped what was typed on the page.** The button
  leaves the page; when a field holds an edit not saved yet, it now asks
  first (audit 2026-10-12, L19).
- **A domain deletion that failed half way said "internal error".** When
  OpenDKIM's tables failed to rebuild after the domain's row was gone, the
  sender map was not rebuilt either and kept the deleted applications'
  logins (their accounts were already gone), and the DKIM key stayed. Every
  rebuild now runs, the page says the domain is deleted and that Reload
  configuration finishes the rest, and Reload configuration removes the
  keys of domains that no longer exist (audit 2026-10-12, L17).
- **A notification whose copy failed for one user was never sent to them**
  once another user's copy had gone out, and a copy held back by the hourly
  ceiling was dropped. A notification is now owed to each recipient: the
  next check sends it to those who did not get it, once; a user added while
  the problem is open is not mailed about it; "it is over" goes to those
  who were told.
- **A DNS resolver that dropped packets made the checks report records as
  wrong.** The first resolver had the whole check's five seconds, so the
  others were asked with no time left; a lookup that was not answered then
  read as "does not resolve" or "no PTR record", and the server's addresses
  were dropped, so every domain's SPF went unknown with it. Each resolver
  now has 2 seconds of its own and the one that answered last is asked
  first; an unanswered lookup is *unknown*, the addresses the last answer
  gave are kept, and notifications report what is wrong, never what is not
  known (audit 2026-10-12, M14, M15).
- **A DMARC report too large was refused as a broken one.** A message over
  10 MiB, or a report that unpacked past 8 MiB, was cut at the limit and then
  failed as damaged; it is now refused for its size, and `mail.log` says so.
- **Two inbound domains to one upstream with different TLS modes** wrote two
  policy lines for one next hop, and Postfix used the first; the form's
  choice for the other domain did nothing. An upstream now has one TLS mode:
  saving it for one domain changes it for the others forwarded there, when
  the one saving manages them all; a domain administrator who does not is
  refused, told the mode in use and not the other domain. The policy map
  holds each next hop once (audit 2026-10-12, L5).
- **A monitoring refresh that never got an answer stopped the refreshes.**
  Requests the panel's pages make in the background now give up after 30
  seconds and are tried again.
- **An empty filtered Outbound log said "No messages logged yet."** It says
  that no message matches the filters.

## [2.5.3] - 2026-10-15

Stage 3 of the 2026-10-12 audit: persistence and restore. A restore is
ended by its Resync and refuses an archive unpacked in part; the send log
answers its frequent questions from indexes; the backup CLI refuses a
missing database and a short password and writes its file whole; an
encrypted file is bounded in what it may ask before its password is
checked. Migration `0010` (indexes only).

### Security

- **An encrypted file could make the panel allocate 4 GiB before its
  password was checked.** The key-derivation parameters an encrypted backup
  or export carries were capped so loosely that a crafted header, uploaded
  to *Import a domain*, asked scrypt for 4 GiB and sixteen times the work.
  A header may now ask for 128 MiB at most - four times what SelfPost
  writes, with the rest of scrypt's memory under 32 KiB - and is refused as
  damaged otherwise. Bytes after an envelope's
  last block, such as a second file appended to it, are refused too (audit
  2026-10-12, M4, L3).
- **`TRUSTED_PROXY_CIDR=::ffff:10.0.0.1` trusted the wrong addresses.** A
  bare IPv4-mapped address written as IPv6 was given "/32", which made it
  `::/32`: every address beginning with 32 zero bits, `::1` among them, and
  not the proxy meant. A bare address is now one host however it is written
  (audit 2026-10-12, L4).

### Fixed

- **Questions asked of the send log on every message or every few minutes
  walked the whole table.** Measured with SQLite's own plans against the
  statements the panel runs: the sweep for rows still queued or deferred
  (every 5 minutes) and the check whether a refusal line from `mail.log` is
  already on a row (every such line) read every row of the retention
  period; a level-2 limit of an application counted through all of its
  domain's rows; a page of a domain administrator's log sorted the domain's
  whole log. Migration `0010` adds four indexes, and a test now holds each
  of these statements to its index (audit 2026-10-12, M2).
- **`selfpost-backup` with a wrong `SELFPOST_DB_PATH` made a backup of
  nothing.** SQLite created an empty database where the path pointed, and
  the archive of it passed as a backup - one that restores a server offering
  its first-run setup while it holds live DKIM keys. A database that is not
  there is now refused, and the snapshot opens it read-only (audit
  2026-10-12, M3).
- **The CLI encrypted a backup with any password.** It now holds the
  password to the panel's floor of 12 characters (audit 2026-10-12, M5).
  Decrypting takes any password, as before.
- **`selfpost-backup -o FILE` left a half-written file** when it failed,
  and `-decrypt -o FILE` with a wrong password emptied a file that may have
  been a good archive. The output is written beside its name and takes it
  only when whole; `-decrypt` checks the password on the first block before
  it touches the output (audit 2026-10-12, L1).
- **A backup left links out without a word, and an empty `certs/` passed.**
  An entry a backup does not carry - a link under `data/`, a link under
  `certs/` to a directory - is now named in a warning (the CLI's standard
  error, the panel's log), and a `certs/` with no file in it is refused: the
  archive would restore a server without its certificate (audit
  2026-10-12, L2).
- **A power loss could bring back a map the panel had just replaced.** The
  files the panel writes for Postfix and OpenDKIM were synced, but not the
  directory their rename changed, so the old file could return while the
  database said otherwise. The directory is synced after the rename now, as
  `sasldb2` has been since 2.5.2, and the CLI's `-o` file too (audit
  2026-10-12, L7).
- **A restore whose first start failed never ran its Resync.** The panel
  removed the restore manifest as soon as it had checked the version, before
  it opened the database and regenerated the mail maps from it; a start that
  failed in between left a data directory the next start took for an
  ordinary one. The manifest is now removed after the Resync is done, so the
  next start is a restore again (audit 2026-10-12, M1).
- **An archive unpacked in part was started on.** A download cut short, or a
  `tar` stopped by a damaged block, left the manifest (the archive's first
  entry) and half of the rest, and the panel opened whatever database was
  there. Every archive now ends with `data/contents.json`, the size and
  SHA-256 of each file under `data/`, and the first start after a restore
  compares `/data` with it before it opens anything: an archive unpacked in
  part, a file that differs, or a database journal file left beside the
  database from before, is refused with a message that names it, and the
  directory is left as it was (guide § Restore). The entrypoint now hands
  every map under `data/postfix` to the panel, whatever its name - a restore
  unpacked as root left the DMARC maps root's, and the comparison could not
  read them. A damaged or cut domain export was already refused before
  anything is created; a test now says so.
- **The guide's in-place restore moved `.`**, which `mv` refuses; it now
  steps out of the project directory first.

## [2.5.2] - 2026-10-15

Stage 2c of the 2026-10-12 audit: every place SelfPost relied on what Postfix,
OpenDKIM, Cyrus SASL, supervisord, logrotate, rspamd, SQLite or a browser does
was read in that program's source and is cited in the code; what the sources
contradicted is fixed below. The sender check reads `From:` with OpenDKIM's own
parser. No migration.

### Security

- **A sender on a private address could have his own spam verdict signed.**
  rspamd's header module by default leaves a message's verdict headers
  alone - removes none, adds none - when the client counts as local (a
  private range, which every client has when Docker's proxy relays port 25)
  or authenticated, while the routine that signs a verdict for SelfPost runs
  for every message. Such a sender's `X-Spamd-Result` and `X-Rspamd-Action`
  then reached Inbound > Log as rspamd's. `milter_headers.conf` now turns
  both exceptions off; tried against rspamd 3.11.1 before and after
  (audit 2026-10-12, stage 2c; assumptions C-R7). Copy the new
  `antispam/rspamd/` files next to your compose fragment.
- **An address listed for one application could be put in `From:` by
  another of its domain.** The sender check accepted a `From:` address when
  any of its keys in the sender map - the address, the address without its
  extension, the domain - named the application; Postfix takes the first
  key it finds and asks only its owners. An application set to any address
  of a domain could therefore write in `From:` an address listed for
  another application of that domain - which Postfix refuses it in the
  envelope - and have it signed. The check now reads the map as Postfix
  does: the first key found decides, logins are compared without regard to
  case, owners may be separated by blanks or continue on the next line, and
  a key written twice makes the check defer, as it makes Postfix refuse the
  whole table (assumptions B-PS1, B-PS4).
- **The sender check reads `From:` with OpenDKIM's own parser.** It used its
  own parser and, to cover where the two could disagree, refused any line
  naming another domain of the server in a display name or a comment - which
  told an application, by refused or passed, which domains are hosted here
  (audit 2026-10-12, L8, an accepted risk until now). It now runs OpenDKIM's
  `dkim_mail_parse()`, ported line for line and tested against the C on two
  hundred thousand lines, and judges the address OpenDKIM signs for. A display
  name or a comment no longer matters - a line with another sender's address
  only in its display name now passes, signed with the application's own
  key; a line OpenDKIM finds no address in, or two addresses in, is refused,
  and so is an address over 256 bytes, which OpenDKIM would match against its
  signing table cut short - cut there, a quoted local part could end in
  another domain's name.
- **The `From:` line must also read as one address to a mail program, at the
  domain that is signed** (review of the stage). OpenDKIM's parser takes
  lines no mail program reads as one address - `ceo@bank.com:me@mine.com`,
  `ceo@bank.com <me@mine.com>` - and signs for the last address while a
  lenient program shows the first; such lines are refused. A quoted display
  name may still say anything. And the address a reader sees, its local part
  unquoted, is the one held against the application's addresses: `"x"@…` no
  longer passes where `x@…` belongs to another application.
- **Over IPv6 every client looks the same to SelfPost** - documented, not
  changed. The compose network has no IPv6, so Docker's proxy carries an
  IPv6 connection and the container sees the network's gateway
  (`172.18.0.1` by default), measured on a live server: all IPv6 clients
  share one level-1 rate limit, and an application's client IP list cannot
  name them - do not put the gateway's address on such a list, or every
  IPv6 client passes it. IPv4 clients keep their own address (guide, "Rate
  limiting - level 1"; assumptions C-K5).

### Fixed

- **A message Postfix gave up on no longer stays "deferred" for ever.**
  Postfix records the end of a message's queue lifetime in one line for the
  whole message, without the recipients; the panel waited for a line per
  recipient that Postfix never writes, and the reconcile sweep closed only
  `queued` rows, so such rows said `deferred` until they were pruned. The
  log tailer now reads that line and closes the message's rows as
  `bounced`; the sweep also closes a `deferred` row whose message Postfix no
  longer holds (which covers the rows left so far, and `postsuper -d`); a
  message forced to expire (`postsuper -e`, `#` in the queue listing) is
  still counted as held (audit 2026-10-12, stage 2c; assumptions A-8,
  B-PQ1).
- **Lines Postfix writes while `mail.log` is being rotated are read.**
  Postfix's log writer keeps the old file open until the reload that
  follows the rotation; the log tailer switched to the new file at once,
  so a delivery written in between never reached its row, which the sweep
  later closed as `bounced`. The tailer now reads the rotated file for a
  minute more (assumptions A-33).
- **Postfix's time values are read as Postfix reads them.** A bare number
  took seconds, so `maximal_queue_lifetime = 3` was three seconds to the
  panel and three days to Postfix; `1h7m` was 67 minutes to the panel and
  one hour to Postfix. A value now takes the parameter's own default unit
  and one unit only; `$name` references are expanded; a
  `bounce_queue_lifetime` longer than `maximal_queue_lifetime` is cut to it,
  as Postfix's queue manager does (assumptions B-PQ10, B-PQ11).
- **A refusal's line goes on the right application's row.** Postfix's line
  about a refusal at `MAIL FROM` was matched to its row by client address,
  sender and time; two applications behind one address sending as one
  sender could take each other's line. The line carries the login after
  all, and the match now uses it. A login read from smtpd's `client=` line
  no longer keeps a trailing comma when the client also gave `AUTH=` on
  `MAIL FROM`. A refusal written from `mail.log` after a restart says that
  Postfix's line names the last recipient only (assumptions A-15, A-21,
  A-23).
- **A delivery reaches the row of every recipient Postfix delivers to.** A
  quoted local part with a blank in it (`"a b"@x`) was cut at the blank, a
  needlessly quoted one (`"ab"@x`) kept quotes Postfix does not log, a
  recipient Postfix had rewritten was looked for under its new address, and
  a recipient given twice made two rows of which Postfix reports one - each
  row then waited for a line that never named it, and the sweep closed it
  as `bounced`. The row now holds the recipient as Postfix logs it, once,
  and is found under `orig_to=` when Postfix rewrote it (assumptions B-G5,
  A-9, A-31, D-S20).
- **Inbound mail between 10 MB and the port's limit is accepted.** Port 25
  said it took messages up to `INBOUND_MESSAGE_SIZE_LIMIT` (25 MiB by
  default), but Postfix writes the message to its queue under a limit of
  its own, main.cf's, never set and so 10240000 bytes: a larger message was
  taken through `DATA` and then refused with 552. That limit now follows
  port 25's when it is larger; 465 and 587 keep theirs (assumptions
  C-P31).
- **Mail to an address literal of the container is refused, not delivered
  locally.** An empty `mydestination` left one way to local delivery:
  `user@[127.0.0.1]` and the container's other addresses. Local delivery is
  now Postfix's refusal, and Postfix refuses such a recipient at `RCPT TO`:
  `550 5.1.1 ... local mail delivery is disabled` (assumptions C-P8).
- **A bounce is judged by the filter lists as the panel counts it.**
  rspamd's sender rules took a bounce's `From:` header when the envelope
  sender was empty, so a bounce could be refused or allowed by an entry the
  Filter lists page never showed as used. They now look at the envelope
  sender only (assumptions C-R4).
- **Stopping or restarting the container stops Postfix as Postfix means it
  to be stopped.** Supervisord's stop signal reached only the shell that
  started Postfix, never Postfix's master process: a restart of the program
  found Postfix "already running", and a container stop cut Postfix off at
  the end. The start wrapper now runs `postfix stop` (assumptions C-P42).
- **Postfix that cannot start takes the container down instead of waiting
  for ever.** When the milter sockets never appear, the wrapper gives up
  after `MILTER_WAIT_TIMEOUT`; that exit was taken for an ordinary one and
  the wrapper started again, endlessly. It now counts as a failed start,
  and once supervisord's three retries are spent the container exits for
  Docker to restart it. For `MILTER_WAIT_TIMEOUT` plus ten seconds after
  each start, Server > Health shows Postfix as starting (assumptions C-V6).
- **A logrotate configuration it would ignore ends in a fault shown on
  Server > Health,** where logrotate was restarted every second for ever;
  the container stays up, mail is not affected (assumptions C-V5).
- **A failed Postfix reload is reported.** The panel took a reload as done
  as soon as it was started, and a reload already running as its own. It now
  waits for `postfix reload` to finish and shows its failure on the page that
  saved; a reload while Postfix is still starting is left to the start
  (assumptions B-U2).
- **Changing an application's password no longer rewrites the live SASL
  database.** `saslpasswd2` wrote into the file Postfix reads at every
  `AUTH` and the backup copies, with no lock between them: an application
  signing in at that moment could be refused, and a backup taken then could
  carry a damaged database whose restore would refuse every application.
  The change is now made to a copy that replaces the file whole (assumptions
  C-S3, C-Q3).
- **Server › Health connects to the milter sockets instead of finding their
  files.** A socket's file outlives a killed OpenDKIM or panel and refuses
  every connection; Health showed it as listening while Postfix answered
  every sender with a temporary error. The panel now connects as Postfix
  does and says when nothing listens (assumptions A-38).
- **Server › Health shows the certificate Postfix serves.** Postfix serves
  its own copy of the mounted certificate and refreshes it once a day; the
  card read the mounted file, so a renewal showed as served up to a day
  before it was, and a renewal Postfix never took up - a key that does not
  belong to it, a failed reload - not at all. The card now shows Postfix's
  copy, names a renewal waiting in the mount, and warns when it has waited
  longer than the daily check allows; the expiry notice is not sent while a
  renewal waits on time (assumptions A-65).
- **The panel works in browsers that do not say where a request comes
  from.** The panel's `Referrer-Policy: no-referrer` made a browser send a
  form's `Origin` as `null`, which the panel refuses unless the browser also
  sends `Sec-Fetch-Site`: in Safari before 16.4, or in any browser on a
  panel served over plain HTTP at a name, every form - signing in
  included - was refused. The policy is now `same-origin`, which still
  sends nothing to other sites (assumptions D-B3).
- **A password shown once is emptied when its page is left,** so a browser
  that keeps the page for its Back button shows an empty field; the
  meaningless `Pragma` header goes (assumptions D-B13).
- **A database update runs once when two programs open the database at
  the same moment.** After an upgrade, the panel and the DMARC report
  intake could both find the database one version behind and both apply
  the next update; the second then failed, and with it that start or that
  report (nothing was damaged). Each update now holds the database from
  its start and checks the version again (assumptions D-S3).
- **Certificates from certbot reach Postfix and the backup.** The guide had
  `certs` linked to certbot's `live/<host>`, and the nginx fragment mounted
  that directory; its files are links into `archive/`, which the container
  did not have, so Postfix started without a certificate and a full backup
  went without one. `certs/` now holds two links into `/etc/letsencrypt`,
  which is mounted read-only at the same path (a commented line in
  `docker-compose.yml`, mounted in the nginx fragment); the guide gives the
  two commands certbot's documentation has for a server that is not root.
  A full backup follows links under `certs/` and carries the files; a link
  that leads nowhere stops it, named (assumptions C-A1, C-Q4). If you use
  certbot this way, follow the guide's new "Reverse proxy" steps.
- **A DMARC report with `pct=0` says 0.** An explicit `<pct>0</pct>` - a
  policy applied to no message - was shown as 100, the value for a report
  that leaves `pct` out (assumptions C-M7).
- **The nginx fragment's first certificate is issued, and renewed.** The
  command it gave passed its arguments to the renewal loop, which ignored
  them, and used the webroot nginx could not serve yet. It is now
  `run --entrypoint certbot ... --standalone` with port 80 free, and the
  renewal loop names nginx's webroot, which a standalone certificate would
  otherwise not use (assumptions D-guide:301).
- **The guide, architecture and security documents say what the code
  does.** Some forty claims the sources contradict, or only partly bear out,
  are corrected: what the rate limits count, what a backup restore message
  looks like, how to pass the backup password, what the queue and Health
  show, a backup MX's preference, the DMARC report address, what the
  password store keeps, what the delivery log holds, and others
  (assumptions, Answers E).
- **Words for what happens to mail match Postfix's.** A message refused with
  a temporary error while the panel or the spam filter does not answer was
  called deferred, which in Postfix's words is mail waiting in its queue;
  the Upstream help said Required TLS holds the mail until TLS works, but
  an upstream that refuses STARTTLS with a permanent error bounces it
  (assumptions D-P1, D-P3).

### Changed

- **The rspamd image is pinned to one build**, `rspamd/rspamd:3.11.1-3`:
  `3.11` and `3.11.1` move with every rebuild (assumptions C-R12). The rules
  lose their `prefilter = true` lines, which rspamd 3.11.1 does not read.

## [2.5.1] - 2026-10-14

Stage 2b of the 2026-10-12 audit: the log tailer and what the panel shows of
a refusal. Migration `0009` keeps Postfix's line about a refusal on its row,
the client a refusal came from, a mark on rows written from `mail.log`, and
the Sender check's two counts and its estimate mark.

### Fixed

- **A log rotated while the panel was down is read to its end.** On start,
  a saved place in a file that had been rotated meant "start on the new
  file", and what Postfix wrote between the panel's stop and the rotation -
  deliveries, refusals, deferrals - was never read. The tailer now reads the
  rotated file (`mail.log.1`) from the saved place to its end first; when
  that file is not the one the place belongs to (rotated twice), it says so
  in the log, with the time from which nothing was read. The log's
  fingerprint now includes the file's inode, so two files that begin alike
  are still told apart (audit 2026-10-12, stage 2b).
- **A second deferral of the same second is counted after a restart.** The
  Sender check's count of deferred mail remembered the time of the last
  line it counted, to pass over lines read again after an unclean stop;
  `mail.log` stamps whole seconds, so a second warning of that second was
  passed over too. It now remembers the place in the log, saved with the
  count in one transaction (audit 2026-10-12, stage 2b).
- **A refused message's page shows what Postfix wrote.** Its Delivery log
  box said Postfix wrote nothing about it, which was untrue. A message
  refused for its `From:` line had a queue id, given before its headers were
  judged; the row now keeps it and the page shows the message's lines, the
  refusal among them - only those of the minutes around the refusal, since
  Postfix frees the id at once. A message refused at `MAIL FROM` (a level-2
  limit, a client address off the list) has no queue id; the log tailer puts
  Postfix's `NOQUEUE: milter-reject` line on its row (migration `0009`) and
  the page shows it. A row refused before this says the line was not kept.
  A refusal's line is red for a permanent reply, amber for a temporary one
  (audit 2026-10-12, stage 2b).
- **A message the panel closed itself says so.** The history of a `bounced`
  row read "the receiving server refused the message permanently, or
  Postfix gave up ... The reason is in the delivery log below" also for a
  row the reconcile sweep closed, where neither is known and the log has
  nothing. Such a row's step is now "Closed by the panel", with why.
- **The Sender check card counts connections and messages apart.** What was
  one column, "Messages deferred", was two things: connections Postfix
  turned away while the panel did not answer - refused before any message
  is offered, so a client trying three times is three - and messages
  deferred in the middle, when the panel stopped answering after a message
  had begun. The card now has a column for each (the owner's decision).
- **A start at which supervisord does not answer still counts the downtime.**
  The panel asks supervisord once at start since when Postfix runs; when it
  did not answer, that start's downtime was not counted at all. It now asks
  for half a minute, and then takes the time since its last sign of life -
  an upper bound - and the card marks such a period with ≈.
- **A refusal the panel did not get to write is written from the log.** The
  sender check answers 550 and writes the row a moment later; a panel that
  stopped in between - a rollout, a crash - left an application refused and
  a log that did not know it. When the log tailer reads cleanup's refusal
  line and, after giving the panel half a minute, finds no row for that
  queue id, it writes the row from the line, marked on its page as written
  from `mail.log` after a restart. A refusal at `MAIL FROM` needs no such
  thing: its row is written before Postfix is answered.

## [2.5.0] - 2026-10-14

Stages 1 and 2 of the 2026-10-12 audit: mail the delivery log showed wrong,
and the boundary between tenants. Two migrations: `0007` adds the delivery
log's own measurement, `0008` makes application logins unique without regard
to case and ties every log row to its domain by id (filled once on upgrade).

### Fixed

- **A queue too large to list no longer closes its own messages.** The
  reconcile sweep asks `postqueue -p` which messages Postfix still holds and
  closes every `queued` row that is not among them. The listing is capped at
  4 MiB, and the ids past the cap were read as gone: during a large deferred
  backlog - the outage the sweep exists for - mail Postfix still held was
  marked `bounced`. A listing over the cap now tells the sweep nothing, as it
  already did for the notification about mail waiting too long; the Mail
  queue page still shows the cut listing with its note (audit 2026-10-12, H3).
- **The sweep closes only what is still queued, and says it did.** Its close
  was the tailer's plain status update, so a `sent` the log wrote an instant
  earlier could be overwritten with `bounced`, and a reused queue id could
  rewrite an old row of the same recipient. The sweep now closes a row by its
  id and only while it is `queued`, and the row it closes carries the reason
  `reconcile`; a delivery line updates only the newest row of its queue id
  and recipient (audit 2026-10-12, H3).
- **A recipient with a quoted local part is followed to its delivery.** The
  journal cut `"a b"@example.net` at its space, and the log tailer cut
  `"a>b"@example.net` at its `>`: the row matched no delivery line and the
  sweep closed it as bounced. Both now take the whole address.
- **A stranger's HELO name no longer reads as a delivery result.** The
  pattern that turns a `mail.log` line into a status matched anywhere in the
  line, so a port-25 client whose HELO name read
  `<id>: to=<address>, status=sent` could mark the matching row delivered. It
  is now matched only at the head of the line, after the host and the
  program, as the other patterns of the log tailer are (audit 2026-10-12,
  M12).

- **A renewed TLS certificate is served without a restart.** Postfix reads a
  copy of the mounted certificate made when the container starts, and the
  daily reload re-read that copy, not the renewed files: a certificate renewed
  while the container ran expired on 465 and 587 at the old one's end. The
  daily job now compares the mount with the copy and, when the certificate and
  key belong together, renews the copy and reloads Postfix; when nothing
  changed it no longer reloads (audit 2026-10-12, H2).

- **Health counts mail deferred by a panel that hangs mid-message.** The
  figure "messages deferred" on the Sender check card read only smtpd's
  lines, but once a message's DATA has begun it is cleanup that talks to the
  check, and its warning was never counted - the very case that tells a hung
  panel from a stopped one. Seen on the e2e stand with the panel frozen, and
  now counted, once per message (audit 2026-10-12, M13).

- **Application logins are one name whatever their case.** Postfix compares
  the SASL login with the sender map without regard to case, the panel did
  not: `Ops` in one domain and `ops` in another were two applications to the
  panel and one sender to Postfix, so a tenant could send with another
  tenant's envelope sender. A new login is now stored in lower case, and
  migration `0008` refuses two logins that differ only in case. A login
  stored earlier, or imported from another instance, keeps its case, since
  its client uses it (audit 2026-10-12, H4).

- **The last global administrator stays.** Deleting or demoting a global
  administrator counted the others first and then wrote, so two
  administrators deleting or demoting each other at the same moment could
  both succeed and leave nobody able to open Server. The count is now taken
  in the same transaction as the write; first-user setup is one statement
  (audit 2026-10-12, M7).
- **A user is saved whole or not at all.** The users form wrote the name,
  the password, the role and the assignments one after another; a failure
  part way left a user renamed with the old role, or a domain administrator
  with nothing assigned. Saving and creating a user are now one transaction
  each, the e-mail included (audit 2026-10-12, M9).

- **The users form keeps a password changed meanwhile, and says when a
  domain it offered is gone.** Saving a user wrote back the password hash
  read when the form was opened, which undid a password the user changed on
  Account in between; a domain deleted while the form was open ended in an
  error page.
- **A domain import is checked like the forms.** The import trusted the file
  for the report address, the rate limits and the client addresses: a limit
  with a window of no seconds counted nothing, and an address that was not
  one made the application refuse every client without a word. The file is
  now held to the forms' rules before anything is created, and refused with
  the form's own message, naming the field and the application; the DKIM
  selector is held to DNS labels; an auto limit's numbers are worked out on
  this server right after the import rather than taken from the file
  (audit 2026-10-12, M10).

- **An application's numbers are its own.** Statistics, the level-2 count
  and an auto limit's recalculation counted every row of the login, so an
  application created with the login of a deleted one - in another domain,
  even - started with the old one's traffic. They now count the login's rows
  of its domain, from the application's creation on. A message with an empty
  envelope sender (`MAIL FROM:<>`) is held to its domain's level-2 limit by
  its login, like the application's other mail.
- **A domain added again starts with an empty log.** Send-log rows belonged
  to a domain by its name, so a domain deleted and added again showed its
  earlier mail to whoever it was assigned to next, and its statistics and
  level-2 count went on counting it. A row now belongs to the domain of the
  application that sent it, by id (migration `0008`, filled once from the
  registry); the rows of a deleted domain are the global role's alone. The
  inbound log follows the same rule (audit 2026-10-12, L12).

- **A refusal no longer names the other domain.** A `From:` line that named
  another sender of the server was refused with that sender's domain in the
  reply. The reply now stops at "it names another sender of this server";
  the line as written stays in the reply and on the refused message's page.
  That such a line is refused at all still tells the sending application
  that the domain is hosted here (security.md, accepted risks) (audit
  2026-10-12, L8).

### Added

- **The delivery log measures its own guesses** (migration `0007`). A table
  `delivery_log_days` counts, per UTC day, the rows the reconcile sweep
  closed, those of them whose delivery line was in `mail.log` all along (the
  log tailer missed it), those a delivery line read later contradicted (the
  log's status then replaces the sweep's, and the row is marked
  `reconcile-contradicted`), the seconds the log tailer could not read
  `mail.log`, and the rows still queued past Postfix's queue lifetime. No page
  shows it: it is read off a copy of the database after a month, to decide
  by a rule written in the audit whether the sweep stays (audit 2026-10-12,
  H3).

## [2.4.2] - 2026-10-14

Release hygiene: stage 0 of the 2026-10-12 audit. Nothing the panel or the
mail path does changes; the release workflow, the tests, the dependencies and
the documents do.

### Fixed

- **A release image says which release it is.** The e2e gate in
  `release.yml` rebuilt the image it was about to push with its own
  `VERSION=e2e`, so every published image printed `e2e` for `-version` and in
  the panel's footer, and the backup/restore version check compared a
  backup's version with `e2e` - any backup restored into any release (audit
  2026-10-12, H1). The gate now builds with the release's version
  (`SELFPOST_E2E_VERSION`), the e2e checks `panel -version` and
  `selfpost-backup -version` against it, and `release.yml` checks the stamp
  again on the image right before pushing it. Images already published are
  left as they are.
- **The documents no longer promise an image that is not there.** README,
  the guide and the compose file pinned `ghcr.io/mixeme/selfpost:2.4.1`, but
  no 2.x image has been published (ghcr.io holds `1.0.0` and `1.3.0`). They
  now say so and show how to build the image locally under the pinned name,
  which Compose and `docker run` then use without pulling (audit 2026-10-12,
  M6). The pin stays: the next publication makes it true.
- **Dependencies raised**: `modernc.org/sqlite` 1.53.0 → 1.60.1 (with
  `modernc.org/libc` 1.77.1), `golang.org/x/crypto` 0.54.0 → 0.58.0,
  `golang.org/x/sys` 0.47.0 → 0.49.0. The three published `x/crypto` fixes
  were in code SelfPost does not call (audit 2026-10-12). development.md says
  the image is rebuilt after each Go patch release, which is where the
  standard library's security fixes come from.
- **The e2e module is compiled on every push** (`test.yml`: `go vet` and
  `go test -c` in `test/e2e`), so a type error there no longer first shows
  up in `release.yml` after a release is published (audit 2026-10-12, M20).
- **`test/` is out of the image's build context** (`.dockerignore`): a
  developer build after an e2e run sent the stand's leftovers - DKIM keys,
  `sasldb2` - to the builder (audit 2026-10-12, L33).

## [2.4.1] - 2026-10-12

Documentation only; no product code changes.

### Added

- **A full-tree code audit** at `2.4.0`, with its plan of work:
  [docs/audit-2026-10-12.md](docs/audit-2026-10-12.md). Six zones read file
  by file against what `architecture.md` promises; what the readers could not
  settle from source was settled on the test server. Four high findings - the
  published release images are stamped `VERSION=e2e` because the e2e gate
  rebuilds the image it is about to push, so the backup version check has
  never checked anything; a renewed TLS certificate is not picked up while
  the container runs; the queue-reconcile sweep can mark delivered mail
  `bounced` while the log tailer is blind; application logins are
  case-sensitive to the panel and case-insensitive to Postfix - twenty-one
  medium, thirty-four low, the doc drift and the test gaps, and what was
  checked and found sound. The plan is six stages, each a cut of its own;
  nothing in it is agreed yet. The published `1.x` tags and images stay as
  they are (owner, 2026-10-12): fixes go forward only.

## [2.4.0] - 2026-10-12

### Security

- **An application signs only as itself.** OpenDKIM chooses the key by the
  `From:` header, and Postfix held only the envelope sender against the
  login: an application of one sending domain could write another registered
  domain, or the server's own name, into `From:` and have the message signed
  with that name's key (security review of `2.3.0`, where it was recorded as
  an accepted risk). The panel now holds the header to what the envelope is
  held to - any address of the application's domain, or the addresses of its
  list, with or without a `+extension` - and refuses anything else with
  `550 5.7.1`, a domain this server does not sign for included
  ([plans/from-header-binding.md](docs/plans/from-header-binding.md)).
- **The check is strict.** It is the first milter of the chain, on a socket
  of its own (`SENDER_CHECK_SOCKET`), attached with
  `default_action=tempfail`: while the panel does not answer, Postfix defers
  mail and accepts nothing unchecked. The journal milter keeps its socket and
  stays fail-open.

### Added

- **Server › Health counts what the strict check costs** in a card of its
  own, *Sender check*: for how long the panel did not answer while Postfix
  was running, and how many submissions Postfix turned away because of it -
  today, in the last 30 days and in the last 12 months (UTC days, kept a
  year). The card warns when mail was deferred in the last 24 hours. The
  numbers are there to decide whether the check needs a process of its own.
- **A refused row of the Outbound log says why**, in its tag: `wrong From`,
  `wrong client` (the application's client IP list) or `rate-limited`; a row
  refused before this version says `rejected`. A message refused for its
  `From:` line shows on its page what the line said and what the application
  may send as. Help has a *Sender check* topic.
- Migration `0006_sender_check.sql` (DDL only): `send_log.reason`,
  `send_log.header_from`, `sender_check_days`.

### Changed

- **Mail with a `From:` the application is not bound to is refused.** What
  stops working on upgrade: an application sending with the `From:` of
  another of your domains (give that domain an application); an application
  with a list whose envelope and `From:` addresses differ (add the second to
  the list); a form that puts its visitor's address into `From:` (use
  `Reply-To:`). The line is read strictly: one `From:` field, one address,
  one `@` outside a quoted display name; a name with a comma or brackets has
  to be quoted. Guide § Who an application may send as.
- **With the panel down, mail is deferred, not accepted.** Until now a
  stopped panel cost the send log and the level-2 limits and nothing else.
- **Server › Health is laid out as two stacks of boxes numbered by
  importance**, read left to right and top to bottom, each stack closed up:
  Processes, Milter sockets, Sender check, TLS certificate, Hostname and
  reverse DNS, Notification mail, Machine, Configuration. On a phone they
  come in that order.
- A refused message is one row of the log, naming its first recipient and
  how many more there were.
- `recipient_delimiter` is set to `+` by the image instead of being left to
  the distribution's default; the start removes milter sockets a killed
  container left behind.

## [2.3.0] - 2026-10-12

### Added

- **Schema for notifications** (migration `0004_notifications.sql`, DDL only):
  a user's own switch, `users.notify`, on for everyone; `notify_state`, what
  the panel has already written to its users about. The instance switch is a
  settings key, on unless turned off
  ([plans/panel-notifications.md](docs/plans/panel-notifications.md)).
- **The panel writes to its users when something needs them.** Four things,
  each said once: a sending domain's DKIM, SPF or DMARC record stops matching
  (after two hourly checks in a row, and once more when it matches again); a
  level-2 rate limit refuses mail (at most once a day per sending domain); the TLS
  certificate is 14 days from expiry, and 3; a message has waited in the
  queue for more than three days (and once more when none has). Global users
  are told all of it, domain users about their own domains. Sent from
  `postmaster@<SELFPOST_HOSTNAME>` by local submission, one message per
  recipient; not rows of the Outbound log and counted against no limit.
- **The server's own name is signed.** The panel generates a DKIM key for
  `SELFPOST_HOSTNAME` and OpenDKIM signs mail submitted locally as that name
  (`non_smtpd_milters`), so that notifications arrive. The name is not a
  sending domain: no application can use it as an envelope sender.
- **Notifications in the panel.** Account has *Send me notifications* beside
  the e-mail; Server › Settings has the switch for everyone and the two DNS
  records of the server's own name, DKIM and SPF, with the values to publish;
  Server › Health has a *Notification mail* card with the state of each. Help
  has a Notifications topic. Every user starts with notifications on, and so
  does the instance.

### Changed

- **An account e-mail may be at a public mail host.** Account and Server ›
  Users refused addresses at Gmail, Outlook, Proton and the like, a rule that
  belongs to DMARC report addresses. A user's own address is now where the
  panel writes to them.
- **A lookup that does not answer is not an SPF mismatch.** When the address
  lookup behind an `a` or `mx` mechanism timed out, a domain's SPF record was
  shown as not authorising this server. It is now a warning that the check
  could not tell.

### Security

- Technical and security reviews of 2026-10-12 of the notifications, before
  release: no Critical finding. The notification about refused mail was built
  on the domain a client writes in `MAIL FROM`; it is now built on the
  registry. Recorded in [security.md](docs/security.md) as accepted, with a
  roadmap candidate (from-header-binding): an application can have a message
  signed as another registered domain, or as the server's own name, by
  writing it into `From:` - the first part of which has always been so.

## [2.2.0] - 2026-10-12

### Added

- **Schema: `send_log_lines`** (migration `0003_send_log_lines.sql`, DDL only).
  The table that will hold what Postfix wrote about a message for as long as
  its send-log row lives, with the store methods to write a line, to write it
  once, to read the lines of a row and to prune
  ([plans/delivery-log-storage.md](docs/plans/delivery-log-storage.md)).
- **The log tailer keeps what Postfix writes about queued messages.** A
  `mail.log` line headed by a queue id is stored with the line's own time, one
  transaction per pass over the log; after an unclean stop a line read twice
  is stored once. Mail that came in on port 25 is left out: a stranger's
  message never has a send-log row, and what he can make Postfix log is not
  written to the database. Lines whose queue id has no send-log row - the row
  was pruned, or the message never had one (bounces) - are deleted once they
  are an hour old, at start and with every six-hourly send-log sweep.

### Changed

- **A message's page keeps its delivery log after `mail.log` rotates.** The
  *Delivery log* box on Outbound › Log › a message was filled by searching
  the current `mail.log`, which is rotated daily, so it was empty for anything
  not sent today while the row itself lives for ninety days. The page now
  reads the lines the tailer stored: they are there for as long as the row
  is. Postfix reuses queue ids, so a row is given only the lines dated within
  its own queue lifetime. A message accepted before this version shows its
  lines only while they are still in the current file, as before; the note
  shown when there are none and the *Outbound log* help topic say so.

### Security

- **A full backup now carries the log lines of messages in the send log** -
  client addresses, SASL logins, message ids, receivers' replies - where
  `mail.log` itself is not in a backup. Recorded in
  [security.md](docs/security.md) with the accepted risk of a queue-id
  collision showing one message's lines on another's page. Technical and
  security reviews of 2026-10-12: no Critical or High finding.

## [2.1.0] - 2026-10-12

### Security

- **The level-2 rate limit and an application's client IP list held only for
  the first message of a connection.** After a refused `MAIL FROM`, a second
  one on the same connection was accepted, and so was every message after the
  first that a connection carried: an application over its limit could go on
  sending, and one restricted to certain client addresses could send from any
  other with a retry. In the code since `1.9.0` (the client IP list) and
  earlier (the limit); found by the technical review of 2026-10-12 and
  reproduced on the test stand. Postfix now gives the panel's milters the
  client address with every message, the limit no longer depends on it, and an
  application with a client IP list is refused when no address is known.
- Reviews of the inbound antispam feature before its release (technical and
  security, 2026-10-12; nothing Critical or High in the feature itself). Fixed:
  a client's HELO name could supply the sender, recipient and reply of an
  inbound log row; the log's row cap was applied only every six hours; rows of
  a connection's later messages had no client address; two changes of the
  filter lists at once could leave the filter's files behind the page; a held
  message was logged twice; after an unclean stop of the panel the refusals of
  its last seconds were logged twice; with the spam filter down, a sender's
  own verdict headers were recorded as the filter's. Recorded as accepted in
  [docs/security.md](docs/security.md): an allow entry for a sender address
  trusts an address anyone can claim; the list files are readable by anyone on
  the host.
- `INBOUND_ANTISPAM_MILTER` and `INBOUND_JOURNAL_MILTER_SOCKET` are checked
  character by character at start; the check looked at their first characters
  only.

### Changed

- **Port 25 logs as `postfix/inbound`.** Its lines in the mail log (Server ›
  System log) now read `postfix/inbound/smtpd[…]` where they read
  `postfix/smtpd[…]`; the submission ports keep `postfix/smtpd`. This is how a
  refusal on port 25 is told from one on 465 / 587.

### Added

- Schema: migration `0002_inbound_spam_log.sql`, the first after the 2.0
  baseline - two empty tables for the inbound antispam journal and the filter
  lists (inbound-antispam-panel). A 2.0.x database is migrated when the panel
  starts.
- **Inbound mail is recorded.** With the inbound relay on, the panel runs a
  second milter, the last one on port 25, and writes a row for every recipient
  of a message that was accepted for an inbound domain: client address,
  envelope, subject and - with a spam filter attached - the filter's action,
  score and heaviest symbols. It only watches: it refuses nothing and Postfix
  accepts mail when it is down.
- **Refused inbound mail is recorded too**, from the mail log: a recipient
  Postfix refused at an inbound domain (unknown user, sender domain not
  found), and a message the spam filter rejected, deferred, held or
  discarded. The row is dated by its log line and carries the reply text. A
  refusal to relay for a domain that is not an inbound one is not recorded.
- The inbound journal is pruned every six hours: rows older than 90 days
  (`INBOUND_SPAM_LOG_RETENTION_DAYS`, 7-365) and anything beyond 10 000 rows.
- The antispam compose fragment mounts `antispam/rspamd/milter_headers.conf`
  into the rspamd sidecar, which makes it write its verdict into
  `X-Rspamd-Action` and `X-Spamd-Result`. **Copy `deploy/antispam/rspamd/`
  next to the fragment before starting it** - Docker creates a directory in
  place of a missing file and the sidecar then fails to read its
  configuration.
- **Allow and deny lists for the spam filter**, kept by SelfPost and applied
  by rspamd: sender addresses, sender domains, client addresses and networks.
  They are written as six files under `data/antispam/`, which the fragment
  mounts into the sidecar together with `antispam/rspamd/multimap.conf`;
  rspamd rereads a changed file by itself. Deny wins over allow. The files
  are there from the first start with the inbound relay on.
- **Inbound › Log**: the journal as a page - time, sender and client address,
  recipient, subject and decision, filtered by domain, decision and day. The
  decision opens a sheet over the page with who decided and why: the filter's
  verdict and symbols, or the reply a refusal was given. A user with the
  *domain* role sees the rows of the inbound domains assigned to them. For the
  global role a row links to the filter lists with the sender filled in:
  *Deny sender* on accepted mail, *Allow sender* on mail the filter refused -
  unless the sender is spelt in a way the lists do not take.
- **Inbound › Filter lists** (global role): add and remove allow and deny
  entries; the page says where the lists go and whether the filter is on to
  read them. Each entry shows how often it decided a message - *Matched*, or
  *never* - and its kind opens a sheet with the note, the day it was added and
  the last match, so that entries nothing matches any more can be found and
  removed. The count is read from the inbound log and reaches back as far as
  the log does. A network of one address (`192.0.2.7/32`) is stored as the
  address, so it cannot be on a list twice.
- **Server › Settings**: *Keep inbound log rows, days*, shown with the inbound
  relay on.
- Help: *Inbound log* and *Filter lists*.
- New setting `ANTISPAM_DIR` (default `/data/antispam`).
- **The spam filter signs what it judged.** rspamd adds `X-SelfPost-Filter` to
  every message it lets through - an HMAC of the message's queue id under a
  key SelfPost creates in `data/antispam/filter.key` - and the inbound log
  shows a verdict only next to a signature that fits. Nothing to configure,
  but the sidecar needs the new `antispam/rspamd/milter_headers.conf` and a
  restart to read it; until then accepted mail is logged as *selfpost*, with
  no verdict.
- A full backup carries the inbound log, the filter lists and their files
  under `data/antispam/`. It does not carry the `antispam/` directory with the
  compose fragment and the rspamd configuration - copy that from
  `deploy/antispam/` when restoring.
- New setting `INBOUND_JOURNAL_MILTER_SOCKET` (default
  `/run/selfpost/inbound-journal.sock`).

## [2.0.1] - 2026-10-11

### Changed

- **A message that is not a report this relay keeps is dropped at once.**
  One that cannot be read as an aggregate report, reports on a domain not
  sent from here, or came to another domain's report address used to be put
  off and offered again by Postfix for days, counted as a parse failure each
  time. It is now accepted and discarded - no retry, and no bounce, since the
  envelope sender of such a message is whatever its author wrote. It counts
  as one parse failure and the reason is in the mail log, on the message's
  own delivery line (Server › System log, `dropped: …`). Only a failure of
  the database is still retried.
- **A full backup no longer holds the Postfix queue**, from the panel or from
  `selfpost-backup`. It is taken while the instance runs, and a copy of a
  queue Postfix is working on can hold a message twice or not at all. Mail
  still waiting for delivery is not carried over; a restored instance starts
  with an empty queue. To keep the queue, stop the instance and `tar` the
  project directory.
- DMARC reports are kept 500 **for each domain** (and 90 days), not 500 for
  the whole server - see Security.

### Fixed

- **Full backup from the panel works.** Since the Postfix queue moved under
  `/data` in 1.3.0 the button answered with a 200 and ten bytes: the archive
  includes the queue, Postfix keeps the queue readable by its own user alone,
  and the panel is not that user ("open /data/postfix/queue/active:
  permission denied" in the log). `selfpost-backup` run with `docker exec`
  was not affected - it runs as root. The queue is no longer part of a backup
  (see Changed), so the two archives are the same again. Everything is read
  through before the download starts, so a failure is a message on the page
  and not a file cut short. One thing the panel may still be unable to read
  is the private key in `certs/`: it then refuses and says what to do -
  `chgrp 998 certs/privkey.pem && chmod 640 certs/privkey.pem` on the host,
  998 being the container's `selfpost` group, whose id is now fixed in the
  image.

### Security

Review of the DMARC report path (Fable, 2026-10-11), which started to run for
the first time in 2.0.0. Nothing Critical or High.

- **A report is bounded before it is stored.** The XML comes from anyone who
  knows a report address, and its 8 MiB limit left room for some 160 000
  records or a single name of megabytes - rows in the database and cells of
  the report page. A report holds at most 5 000 records (a larger one is
  dropped), every text field is cut to 255 bytes, and a record's count is
  capped so the sums cannot wrap.
- **One domain's reports cannot push out another's.** The cap of 500 was for
  the whole server, so a stranger filing reports under one domain's address
  could evict every other domain's history. It is counted per domain now.
- The delivery command runs one at a time, not up to a hundred in parallel.
- The reason written to the mail log for a dropped message is cut to 200
  bytes and anything outside printable ASCII is replaced: parts of it are the
  sender's own bytes (a header line, a file name inside an archive).
- `SELFPOST_DATA_DIR` / `SELFPOST_DB_PATH` with whitespace or `$` are refused
  at start when DMARC ingest is on, instead of handing the delivery command
  another path and so an empty database of its own.

### Added

- e2e: the run now ends on port 25 with both optional features on. A DMARC
  report is mailed to a domain's hosted address and has to appear under
  Outbound › DMARC reports; a report on another domain and a message with no
  report are accepted and dropped; mail for an inbound domain reaches its
  upstream and other recipients are refused. Until now the suite only read
  back the generated configuration of these features.
- e2e: full backup and restore. The run takes a backup from the panel, throws
  the installation away, unpacks the archive into the empty project directory
  and starts it: the administrator signs in, the domain has the same DKIM key,
  and mail sent by the application verifies against the record published
  before the backup.

## [2.0.0] - 2026-10-11

The panel redesign. A breaking release: every panel URL changed and the
database starts from zero - a 1.x `/data` is not upgraded (see Changed and
Removed).

### Added

- panel-redesign (stage 0): Bulma 1.0.4 and a 57-icon subset of Tabler Icons
  3.49.0 are vendored under `/static` (MIT, listed in NOTICE). Nothing links
  them yet; the old panel is unchanged.
- panel-redesign (stage 0): the design contract as guard tests
  (`guard_*_test.go` under `internal/web`) and a `design-first` CI step. The
  tests read their rules from the accepted mockups — class vocabulary,
  stylesheet, page structure, component outlines, the 2.0 route table — and
  pin the vendored assets. `internal/web/view/legacy_pages.txt` lists what is
  still pre-2.0 and may only shrink; CI also rejects a commit that changes a
  guard together with the implementation, or brings back a template from
  history. The kit and route tests stay skipped until their stages land.
- panel-redesign (stage 0): the component kit. `panel.css` is the mockups'
  stylesheet rule for rule; `components.html` holds every partial with its
  typed input in `components.go`; the new `layout.html` is the shell (brick
  navbar with the wordmark, perforated edge, sibling strip, user menu,
  footer) and the signed-out screen; `GET /server/components` (global role)
  renders every partial in every state. `@kit` is off the ratchet: the kit
  is held to the design contract from here on.
- panel-redesign (design): Sign out in the user menu is a POST form in the
  mockups too, and its button takes the same hover, focus and press tint as
  Account and Help (owner, 2026-10-09).

- panel-redesign (stage 1): the 2.0 schema. `users.email` is the user's own
  address; the default DMARC report address is a user's setting
  (`dmarc_default_mode` / `dmarc_default_address`) and a domain either has an
  address of its own or follows a named user's default
  (`domains.dmarc_rua_user_id`) — a new domain follows whoever created it,
  and deleting that user leaves it with no report address (replaced before
  release — see the entry of 2026-10-10 below).
  `user_inbound_domains` and the per-user `all_domains` /
  `all_inbound_domains` flags are in the schema for the inbound delegation
  that follows. Details: `docs/schema-migrations.md`.
- panel-redesign (stage 1): inbound domains are delegated to domain
  administrators, separately from outbound. The user form has two lists, each
  with *All* (which includes domains added later; with it ticked the rows
  under it are ignored); a domain administrator needs at least one assignment
  on either. Such a user sees the inbound domains assigned to them — the list
  is filtered in the query — and can change their upstream and recipients;
  adding and deleting an inbound domain stays with the global role. Another
  tenant's domain, a missing one and a user with no inbound reach all get the
  same 404. A menu group with nothing assigned is absent, and `/` leads an
  inbound-only administrator to Inbound.
- panel-redesign (stage 1): **Account** (`/account`, every role) and
  **Server › Settings** (`/server/settings`, global role) replace the one
  Settings page. Account has three forms, each its own POST: profile
  (username, e-mail), password, and the default DMARC report address
  (SelfPost hosted, my account e-mail, another address, none) (replaced before
  release — see the entry of 2026-10-10 below). Saving the profile no longer
  asks for the password; changing the password still does.
- panel-redesign (stage 1): the application form is one POST. Who the
  application may send as, its client-IP allow-list and its rate limit
  ("use the domain limit", manual or auto) are validated together and saved
  together or not at all — also at creation, so an application can be added
  with its limit already in place. The page that shows a generated password
  is the response to its POST only and is sent with `Cache-Control: no-store`.
- panel-redesign (stage 1): **Server › Health** (`/server/health`) holds the
  tables of the old Status page and its two actions, *Re-check DNS* and
  *Reload configuration*. **Overview** (`/overview`) is reduced to one
  verdict per check — machine, processes, TLS certificate, queue, milter
  sockets, reverse DNS — each leading to its table on Health; a check that
  could not run shows as a warning, never as fine. Both read the same checks.
- panel-redesign (stage 2): **Sign in** and **Create administrator** are the
  first pages in the new design - the split brand / form screen, built from
  the component kit. Sign in names the mail host above its title; a refused
  attempt is shown as a notice above the fields. `login` and `setup` are off
  the ratchet.
- panel-redesign (design): the sign-in page before any administrator exists
  is a drawn screen (`login-no-admin`): the heading *No administrator yet* and
  the pointer to the one-time setup link, in the same column as the form. The
  signed-out column is sized through `.sp-form`, not through `form` (owner,
  2026-10-09).
- panel-redesign (design): the Notifications boxes are out of the Account
  and Server › Settings mockups. Event notifications are not built
  (panel-notifications is a candidate), and 2.0 carries no box for a feature
  that does not exist; they are drawn again as the first step of that
  feature (owner, 2026-10-10).
- panel-redesign (stage 2): **Overview**, **Server › Health**, **Account**
  and **Server › Settings** are in the new design. Overview shows the verdict
  as a stamp with one sentence, the six checks as cards and both domain
  lists; Health holds the tables behind the cards. Both refresh themselves.
  The Queue card reads "Empty" or "N queued". `status` and `settings` are
  off the ratchet.
- panel-redesign (stage 2): the **Outbound** domain pages are in the new
  design: the domain list, the domain page (DNS records with what to
  publish and what is in DNS now, applications, connection), **Domain
  settings** as a page of its own (report address, rate limit, export,
  delete), the application form, the password shown once and the delete
  confirmation. `dashboard`, `domain_detail` and `domain_delete` are off
  the ratchet.
- panel-redesign (stage 2): the **Outbound log**, the message page (facts,
  history, the lines of `mail.log`) and the three **DMARC report** pages are
  in the new design. The log refreshes its rows without touching the filter
  being chosen. `deliveries`, `delivery`, `dmarc`, `dmarc_domain` and
  `dmarc_report` are off the ratchet.
- panel-redesign (stage 2): the **Inbound** pages are in the new design: the
  domain list, the domain page (MX record, upstream, valid recipients) and
  the delete confirmation. Every inbound domain page has a **Spam filter**
  box and the list names the filter's state: the panel reads
  `INBOUND_ANTISPAM_MILTER` and `INBOUND_ANTISPAM_MILTER_ACTION` and says
  whether inbound mail is filtered and what happens while the filter is
  down. The filter's address is shown to the global role only. `inbound`,
  `inbound_domain` and `inbound_delete` are off the ratchet.
- panel-redesign (stage 2): the **Server** pages - System log, Backup,
  Users, the user form and its delete confirmation - and **Help** are in the
  new design. Help is a page of topics that the `?` in a box's head opens;
  the help drawer is gone. The user form sets the user's e-mail. The backup
  download is encrypted unless the box is unticked. Only the queue page is
  left on the ratchet.
- panel-redesign (design, stage 3): **Help** covers every section -
  seventeen topics for Overview, Outbound, Inbound, Server and Account - and
  the boxes of nearly every page carry a `?` that opens their topic (owner,
  2026-10-10). A reader sees the topics of the sections their menu shows.
- panel-redesign (design, stage 3): **a domain's DMARC report address is one
  of three choices** - no reports (a new domain), the SelfPost hosted
  address, or an address typed for the domain - set in Domain settings, with
  a button that fills in one's own profile e-mail. The per-user default
  report address, a domain "following" a user and the DMARC box on Account
  are gone, and with them `users.dmarc_default_mode`,
  `users.dmarc_default_address` and `domains.dmarc_rua_user_id` (owner,
  2026-10-10; replaces the design of 2026-09-21 described above).
- panel-redesign (stage 3): with the inbound spam filter on, its socket is one
  of the milter sockets on **Server › Health** and counts in Overview's card.
  The panel connects to the configured address (`inet:host:port` or
  `unix:/path`, one second at most, nothing sent). A filter that does not
  answer is an error with `INBOUND_ANTISPAM_MILTER_ACTION=tempfail` - inbound
  mail is deferred - and a warning otherwise - mail goes through unfiltered.
- **Port 25 no longer accepts `dmarc-reports@<hostname>`**, the report
  mailbox of the server as a whole. Only an address a domain has chosen is
  accepted: its hosted address `dmarc-reports+<domain>@<hostname>`, or a
  typed address that is on this host. A domain whose published `rua=` still
  names the general mailbox must be set to *SelfPost hosted* and its DMARC
  record republished (owner, 2026-10-10).
- panel-redesign (design, stage 3): **New password** and **Delete** are in
  each row of a domain's Applications table again, as POST forms that look
  like the links beside them.
- panel-redesign (stage 2): the **Outbound queue** page is in the new shell
  with its content unchanged - the listing of `postqueue -p` and how
  retries work. With it every page is built from the component kit: the old
  stylesheet, the old layout and the engine's second rendering path are
  deleted, and the ratchet is empty.

### Changed

- **Breaking — every panel URL changed.** A path now reads like the menu
  (`/overview`, `/outbound/…`, `/inbound/…`, `/server/…`, `/account`); the
  old paths are removed, not redirected — see *Removed*. Polled fragments all
  answer `<page>/fragment`. Everything under `/server/` is guarded for the
  global role where the routes are registered, in addition to each handler's
  own check. An application is addressed under its domain
  (`/outbound/domains/{id}/applications/{aid}`), and an application id that
  does not belong to the domain in the path is a 404.
- **Breaking — the database starts from zero.** The 1.x migration chain
  (`0001`–`0010`) is replaced by one baseline, `0001_init.sql`. A 1.x
  `/data/selfpost.db` is refused at start-up with an explicit error and left
  untouched; nothing is migrated. A domain can be carried over with its
  export file.
- The panel role `domain_admin` is stored as `domain`.
- On the old domain page, "Same as Settings" makes the domain follow the
  default report address of the user who chose it (replaced before release —
  see the entry of 2026-10-10 below).
- The old panel is untouched on screen but steps aside in the tree: its
  stylesheet is `legacy.css` and its layout `layout_legacy.html` until the
  last old page is restyled. The 2.0 menu paths in the new shell do not
  exist before stage 1; only the kit page uses that shell so far (a stage-0
  state: both files are deleted and every page is on the shell — see
  *Removed*).
- panel-redesign is `agreed` (owner, 2026-10-09); work starts with stage 0,
  the component kit.
- panel-redesign (stage 3): the operator guide, `architecture.md` (Panel HTTP
  surface, Persistence) and `security.md` (roles, sessions, errors shown to
  users) describe the 2.0 panel. `security.md` records two accepted risks from
  the 2.0 review: a delegated inbound upstream may be any host and port, and
  Account's profile is saved without the password.

### Security

- A panel session belongs to a user by id and ends with them. It used to be
  keyed by user name: the cookie of a deleted or renamed user stayed valid
  and became a session of whoever was next created under that name. Signing
  in again is required after upgrading (there is no upgrade from 1.x; this
  concerns development builds of 2.0).
- Changing one's password ends one's own other sessions only; it used to
  sign every other user out. A password set by an administrator on the user
  form ends that user's sessions.
- Internal errors (database, `saslpasswd2`, Postfix reload) are no longer
  shown on the application, inbound upstream / recipients and limit forms;
  they are logged and the page says so.
- `/static/` no longer answers with a listing of the embedded files.
- The DMARC reports hub is open to a domain administrator for the domains
  assigned to them, without the server's ingest statistics; the list of
  reports no longer treats "no domains" as "all domains".

### Fixed

- Saving a domain's report-address form untouched no longer moves a domain
  that follows another user's default to whoever saved it: that user's
  default is its own choice, and "My default instead" is the change
  (replaced before release — see the entry of 2026-10-10 below).
- The auto rate-limit fields follow the limit mode on the domain and
  application forms (the script applied only the first matching rule).
- The DMARC page of a domain no longer answers 500 once the domain has a
  reported source.
- **The panel can read the mail queue.** The container's entrypoint left the
  queue's `public/` and `maildrop/` directories in group `postfix` on every
  start; Postfix keeps them in `postdrop`, the group `postqueue` works
  through, so the panel's `postqueue -p` was refused ("Connect to the Postfix
  showq service: Permission denied"), the Queue card said "Could not be read"
  and the queue page had no listing. Present since the persistent queue of
  1.3.0; mail flow was not affected. The e2e suite opens the queue page now.
- **Port 25 answers again with DMARC report ingest on.** The generated
  `master.cf` gave the port-25 service `-o smtpd_recipient_restrictions=
  check_recipient_access texthash:…,…` with a space in the value; Postfix
  cuts a service line at whitespace, so `smtpd` was started with a stray
  argument and died with "fatal: unexpected command-line argument" on every
  connection - no inbound relay and no DMARC reports were accepted while
  `DMARC_REPORTS_ENABLE=true`, although `postfix check` and the suite were
  silent. The list is written with commas only, the generator refuses a
  value with whitespace, and the e2e check asks Postfix what it reads.
  Present since DMARC ingest was fitted into the 1.9.1 fix of the same
  stanza.
- **DMARC reports are accepted with the inbound relay on.** With both
  features enabled every report address was refused as "User unknown in
  relay recipient table": the relay's unlisted-recipient check runs after the
  restriction list, and the report addresses - on the server's own name, a
  relay domain - were not in `relay_recipient_maps`. Their table is a
  recipient table now. Hidden until now by the port-25 failure above.
- **DMARC reports reach the panel.** A report accepted on port 25 stayed in
  the queue: the generated delivery service was a bare `pipe` ("fatal:
  missing user= command-line attribute"), because the generator addressed it
  under a type Postfix does not have and set `flags=`, `user=` and `argv=`
  as `-o` parameters, which they are not. Behind that, the command would
  have refused to run (Postfix starts it without the container's
  environment) and was never told the recipient, so the check that a report
  came to the address of its own domain was skipped. The service line
  carries its arguments, the recipient is one of them, and
  `export_environment` names what the command reads. No report had been
  delivered since the feature shipped in 1.7.0; the e2e check asks Postfix
  how it reads the service.
- Postfix no longer logs "warning: not owned by root" for the queue
  directory and its `pid/` at every start: the entrypoint hands the two back
  to root after its recursive change of owner.
- Overview's Processes card counts the programs that are meant to stay up
  ("6 of 6 running"); the idle one-shot `postfix-reload` was counted as
  running. The Milter sockets card no longer clips its second line with three
  sockets, and a DKIM value is shown whole instead of scrolling inside its
  field.
- Server › Health shows the idle reload helper (`postfix-reload`) as
  **on demand**, with when it last ran, instead of `stopped` beside the
  running programs (owner, 2026-10-10).
- The confirmation before deleting a user names the domains that follow
  that user's default report address and will be left without one; it used
  to say the domains were not affected (replaced before release — see the
  entry of 2026-10-10 below).

### Removed

- **2.0 starts from an empty data directory.** A 1.x `/data` is not upgraded:
  its database is refused at start and left untouched, and a 1.x backup is not
  restored; a domain is carried over with its export file. Gone with it, as
  built: the 1.x migration chain, every pre-2.0 panel path (listed next), the
  help drawer (Help is a page of topics), the default DMARC report address —
  the panel-wide one of 1.x (`dmarc_report_email`, "Same as Settings") and the
  per-user one with a domain following a user that earlier entries of this
  release describe (`users.dmarc_default_*`, `domains.dmarc_rua_user_id`) —
  together with the DMARC box on Account, the old stylesheet `legacy.css`, the
  old layout `layout_legacy.html`, and the unused `rate_limits.allowed_ips`
  column.
- Old panel paths, GET and POST alike, without redirects: `/status`,
  `/status/fragment`, `/status/recheck`, `/reload`, `/domains` and everything
  under it (`/domains/import`, `/domains/{id}`, `…/dns-recheck`, `…/delete`,
  `…/ratelimit`, `…/ratelimit/recalc`, `…/dmarc`, `…/export`), `/inbound` and
  `/inbound/{id}/…`, `/dmarc`, `/dmarc/reports/{id}`, `/dmarc/domains/{id}`,
  `/deliveries`, `/deliveries/rows`, `/deliveries/{id}`, `/mail-queue`,
  `/mail-queue/body`, `/system-log`, `/system-log/body`, `/backup`, `/users`
  and everything under it, `/settings`, and the `/account` → `/settings`
  redirect; `POST /domains/{id}/applications` and `/applications/{aid}/mode`,
  `/authips`, `/ratelimit`, `/ratelimit/recalc`, `/password`, `/delete` — the
  three separate saves of an application are one form now.
- The instance setting `dmarc_report_email` (a mirror of the global user's
  profile field) and the unused `rate_limits.allowed_ips` column.

## [1.9.5] - 2026-10-09

CI-only cut: the release workflow no longer breaks the multi-arch image it
just published. No product code changes.

### Fixed

- ci (GHCR): the release no longer deletes the `X.Y.Z-amd64` / `X.Y.Z-arm64`
  package versions after the manifest merge. Those versions are the images the
  `X.Y.Z` manifest points at, so `docker pull ghcr.io/mixeme/selfpost:1.3.0`
  failed with a missing manifest. The per-arch tags now stay; the merge job
  instead removes untagged versions that no tagged manifest references.

## [1.9.4] - 2026-10-09

Plan-only cut, owner-requested: the redesign goes first as `2.0.0` with a
component-kit stage and guards against resurrected pages; antispam moves to
`2.1.0`; a new candidate explains the empty delivery log. No product code
changes.

### Changed

- Roadmap order (owner, 2026-10-09): the panel redesign ships first as
  `2.0.0`; inbound-antispam-panel follows as `2.1.0` on the 2.0 baseline
  (its migration becomes `0002_inbound_spam_log.sql`), built from the
  redesign's component kit so its two screens are styled once. The redesign
  plan gains a **Component kit** section and stage (partials with typed
  inputs, one `panel.css`, a `/server/components` kit page, acceptance before
  any page is restyled), a staged checklist (5/27), Health decided inside 2.0
  and the queue pages left as old content in the new shell, and an **Against
  resurrected pages** section: a shrink-only `legacyPages` ratchet in the
  guard tests, the outline golden, the old-path test and a history-blob
  check in CI, so an old template from git history cannot pass as a
  redesigned page.
- New roadmap candidate `delivery-log-storage`: the *Delivery log* box on a
  message's page is empty for anything older than the last daily rotation,
  because the lines are grepped from the current `mail.log` on each view and
  never stored; the item proposes keeping them with the send-log row.

## [1.9.3] - 2026-09-22

Panel-redesign prep, docs only: stage 1-2 mockups on all 33 screens, the
accepted header/type-scale/contrast corrections, the account e-mail and
inbound-delegation decisions, and the routes/quarantine/preflight roadmap
screens. Nothing in `internal/web` changes; the design stays `candidate`
(3/21) until the redesign itself ships as `2.0.0`.

### Added

- Panel redesign, account e-mail and notifications: a user's e-mail is its own
  field (2.0 baseline `users.email`, today `dmarc_report_email`) — edited under
  **Account**, seen and set by the global administrator on the user form and in
  the users list; DMARC may *use* it, as one choice beside the hosted address,
  another address or none (Settings and domain settings). **Account** gains a
  Notifications box and **Settings** a switch for the new roadmap candidate
  `panel-notifications`; `password-reset` is added as a candidate too. The
  inbound spam filter becomes visible: a *Spam filter* box on every inbound
  domain, a tag on the inbound domains list, its socket among the milter
  sockets. Preflight loses a box that only filled space — the test form now
  shares its box with its result — and the plan's layout rule and contract say
  so: what stands beside a form must be real. The role is labelled by its reach,
  `global` or `domain` — everyone in the panel is an administrator — and each
  assignment list on the user form gains **All**, a per-user flag that covers
  domains added later. The DMARC default report address is a **user's**
  setting, not the server's: it moves from Server › Settings to **Account**
  with its report-authorization record, and a domain follows the default of a
  *named* user, so two people sharing a domain never pull it two ways.
- Panel redesign, contrast: a sweep of every text node against its real ground
  found the solid red button at 3:1 (Bulma derives a solid button's text from
  the fill's lightness; brick had been patched, red had not), the muted grey
  at 4.1:1, the amber postmark at 4.0 and Bulma's menu labels at 4.2. Solid
  buttons are white on colour, muted is `#5c6774`, the postmark uses the warn
  token; all 33 screens now measure 4.5:1 or better. The sweep is kept as
  `panel/audit.js` — text size, contrast, overflow, clipped values, missing
  icons — and its clean output joins the evidence a restyle step must show.
- Panel redesign, header (owner's choice: variant A, with the perforation on a
  light edge as on the mark): the navbar carries the **wordmark** instead of
  the stamp — `docs/assets/selfpost-wordmark.svg` and `-ink`, the stamp's own
  outlines, recorded in `docs/product.md` — and under the brick bar runs the
  stamp's paper margin with the perforation along its outer edge: round holes,
  flat teeth, the ink hairline (`panel/perf-edge.svg`). All 33 screens rebuilt;
  the components page documents the shell; the comparison sheet is removed.
  The six health cards now split into six, three or two columns, so one card
  is never left alone on a row. Three corrections after the owner's review:
  the paper edge is 8px under the 60px bar — it had been scaled from the hole
  size to 16px, where the mark's margin is a tenth of the brick field; the
  health row is compact again (71px: smaller icons, each line a block with its
  own line height, a verdict never wraps and stretches the row); and a
  read-only value is as tall as the Copy button joined to it. `check.py` no
  longer counts `min-width: 0` as setting a width. Text sizes: the filter
  controls in a box head and the navbar's dropdown entries are 15px like the
  bar itself (they had Bulma's `is-small` and its .875rem dropdown); `check.py`
  now rejects `is-small` on anything but a progress bar.
- Panel redesign, type scale: about 40 % of the panel's text measured under
  13px (labels 11.7, table heads 10.5, fact labels 10.2, tags and help 11.3).
  Body is now 16px with two tokens under it — `--sp-fs-s` 14px for supporting
  text and `--sp-fs-xs` 13px for upper-case mono labels and tags; Bulma's
  small sizes are overridden or dropped (`sp-small` replaces `is-size-7`), and
  `check.py` fails any font size under the floor. Every table now sits in a
  `table-container` (enforced), addresses in tables break after the `@`, the
  inbound log folds the client IP under the sender; all screens fit without
  scrolling at 1280px and up. Documented on the components page and in the
  plan (§ Type and tables).
- Panel redesign, header comparison sheet
  ([docs/assets/panel-redesign/header/](docs/assets/panel-redesign/header/index.html)):
  at nav size the stamp dissolves into the brick bar and the perforated edge
  was drawn inverted (round bumps instead of round holes). Three replacements
  beside the current header — **A** wordmark with the stamp's real hole-to-tooth
  perforation bitten into the bar, **B** wordmark on a plain bar, **C** a
  stamp-paper header with an ink hairline and brick only behind the name. The
  wordmark is cut from the stamp's own outlines. Pending the owner's choice;
  the mockups are unchanged. `.claude/launch.json` serves the mockups locally
  (`python -m http.server 8765` from the repository root).
- Panel redesign, the roadmap's screens: **Inbound › Filter lists** and the
  adjusted **Inbound › Log** (client, decision, engine and reason; *Allow /
  Deny sender*) for inbound-antispam-panel, **Inbound › Quarantine** (list and
  held message with Release / Discard) for inbound-quarantine, and **Server ›
  Preflight** for preflight — all from existing components; 33 screens. Their
  paths join § Routes; the quarantine and preflight plans link their mockups.
  A mockup is the target look, not a decision to build: each item keeps its
  status, and quarantine's open questions stay open.
- Panel redesign, two owner decisions recorded in
  `docs/plans/panel-redesign.md`: **no compatibility** — the redesign is
  `2.0.0`, old paths are removed without redirects, the SQLite migration chain
  is replaced by one baseline and 2.0 starts from an empty data directory
  (schema-squash ships as a step of this plan, without its upgrade gate; the
  only live installation is the owner's); and
  **inbound delegation** — a domain administrator manages assigned inbound
  domains, granted separately from outbound (`user_inbound_domains`, a second
  list on the user form, per-domain authorization under `/inbound/`). Mockups `users`, `user`,
  `user-delete` redrawn for the two lists. Checklist `3/21`.
- Panel redesign, routes: the owner decided that paths follow the new names
  and structure. `docs/plans/panel-redesign.md` § Routes maps every screen to
  its new path (`/overview`, `/outbound/…`, `/inbound/…`, `/server/…`,
  `/account`) with the rules that come along — one `<page>/fragment` name, the
  application form as a single POST, 301s from old GET paths for one MINOR,
  subtree `requireGlobal()` under security review, and
  `TestRoutesFollowNavigation`. Mockups gained what the first pass missed: the
  *Reload configuration* box (now on Server › Health), the `flash` component
  and the `sp-help` link that replaces the help drawer. Checklist `1/17`.
- Panel redesign, design contract:
  [docs/plans/panel-redesign.md](docs/plans/panel-redesign.md) rewritten for
  the accepted direction — three layers (Bulma as shipped, adapters, own `sp-`
  components), three page layouts, what an implementer must not do, the guard
  tests that land before any template, a `design-first` CI step and
  evidence-based acceptance. A
  [components page](docs/assets/panel-redesign/panel/components.html) shows
  every building block in every state; `panel/check.py` lints the mockups
  against the contract and writes `panel/outlines.json`, the per-screen
  component skeleton an implementation is compared with. Own modifier classes
  renamed to carry the `sp-` prefix so the vocabulary is machine-checkable.
  Agent rule `.cursor/rules/panel-design.mdc`; roadmap row updated (`0/15`).
- Panel redesign, stage 2: the accepted direction drawn on every screen —
  29 pages under [docs/assets/panel-redesign/panel/](docs/assets/panel-redesign/index.html).
  Direction C (Bulma) is the base, with A's postal signature (postmark verdict,
  numbered mono section heads, the stamp's perforated edge) and B's icons.
  Overview keeps verdicts only and the tables move to a new **Server › Health**
  page; the own password becomes **Account**; queues are drawn as tables
  (needs `postqueue -j`) and **Inbound** gets its own Log and Queue. Pages are
  built from fragments by `panel/build.py`; the built HTML is committed.
  Mockups only; nothing in `internal/web` changes.
- Panel redesign, stage 1: three directions drawn on the three key screens
  (Overview, an outbound domain, sign-in) under
  [docs/assets/panel-redesign/](docs/assets/panel-redesign/index.html) —
  **A** Pico CSS ("Blank"), **B** Tabler ("Console"), **C** Bulma ("Counter"),
  with a cover sheet comparing size, JavaScript, CSP fit and own CSS. All three
  share a navigation grouped by mail direction (Overview · Outbound · Inbound ·
  Server, with a symmetric Log and Queue on both sides), one block per DNS
  record instead of separate "status" and "records to publish" cards, and
  applications as a table. Mockups only; nothing in `internal/web` changes.
  The brief behind them supersedes the "no component library" decision in
  `docs/plans/panel-redesign.md`; that file is rewritten once a direction is
  accepted.
- Roadmap item and givens for a panel visual redesign
  ([docs/plans/panel-redesign.md](docs/plans/panel-redesign.md)) — layout
  grammar (`measure` / `fill` plus Web Awesome layout CSS), no JS component
  library, new mockups rather than `docs/assets/panel-ui/`. Status: candidate.
- Roadmap item and plan for `/preflight` installation check page
  ([docs/plans/preflight.md](docs/plans/preflight.md)) — instance-level
  infrastructure verification (rDNS, TLS, ports, HELO banner, proxy headers,
  DKIM socket, test email). Status: candidate.

## [1.9.2] - 2026-09-22

Review pass over `1.4.0`…`1.9.1`: port 25 no longer shares the submission
milter chain, DMARC reports are validated before they are stored, and the auto
rate limit is correct for a non-default level-1 window. Schema migration
`0010` (index only). Upgrading is a tag bump.

A full-tree code review on 2026-08-19 (architecture, code quality, security,
GUI, tests, documentation, AGPL compliance; 38 findings) produced most of what
follows. Its four open items live in [roadmap.md](docs/roadmap.md); its working
document is not part of the release.

### Added

- SQLite migration `0010_send_log_app_login_index.sql` —
  `idx_send_log_app_login_created_at`. The journal-milter counts an
  application's messages on every `MAIL FROM`, and the domain page computes
  per-application statistics on every render; both were scanning the
  `created_at` range because `app_login` was unindexed.
- DMARC aggregate reports are accepted as **zip** as well as gzip or plain XML
  (several reporters ship a single-entry zip; those reports previously counted
  as parse failures).
- e2e: `checkPort25ConfigCombinations` regenerates the Postfix configuration
  inside the container for all four combinations of `INBOUND_RELAY_ENABLE` /
  `DMARC_REPORTS_ENABLE` / `INBOUND_ANTISPAM_MILTER` and requires
  `postfix check` to pass. The `1.9.1` crash-loop shipped in `1.4.0` and
  survived three releases because the suite only ever exercised the flags-off
  path.

### Changed

- README hero line and a **Positioning** block in `docs/product.md` record the
  tagline (`YOUR SMTP. YOUR RULES.`) and hero copy; the stamp subtitle stays
  `SELF-HOSTED SMTP RELAY`. `docs/assets/selfpost-proof.html` updated to match.

### Fixed

- The setup account's password is hashed at bcrypt cost `12` like every other
  panel password; it was still being written at `bcrypt.DefaultCost` (10). The
  work factor now has one definition, `auth.BcryptCost`. bcrypt records the
  cost in the hash, so existing passwords keep verifying unchanged.
- Login/setup rate limiter: protecting blocked buckets from eviction is now
  bounded. Blocked buckets are still never evicted to make room, but past
  `maxBuckets × 4` they are evicted too (nearest expiry first) — otherwise an
  attacker with many source addresses could grow the bucket map without limit
  by making each address fail its way to blocked. The overflow is logged at
  most once per window.
- `postqueue -p` output is bounded to 4 MiB (the timeout alone left the size of
  the listing, and therefore the panel's allocation, up to the queue).
- [schema-migrations.md](docs/schema-migrations.md): the schema section was
  still headed `v9` while the chain head is `v10`, and three links pointed at
  plan files deleted when those features shipped.
- postqueue reader is now time-bounded: `postqueue -p` runs under a 5-second
  context timeout so a stuck queue command cannot hang the queue panel path
  indefinitely.
- DMARC panel layout classes now exist in
  `internal/web/view/static/panel.css` (`.pair`, `.attn`, `.desk-only`,
  `.phone-only`, `.phone-list`, `.check-row`), so desktop/mobile DMARC views and
  attention highlighting render as intended.
- `dmarc-ingest` now bounds stdin with `io.LimitReader` (10 MiB) before parsing.
- User-role update no longer contains a duplicate "last global admin" demotion
  guard; one check now returns a context-specific message for self-demotion.
- Settings flash text now reflects what actually changed and only mentions
  session sign-out when the password changed.
- Help page/drawer links now use `SourceURL` (`{{.SourceURL}}/blob/main/docs/...`)
  instead of a hardcoded repository URL.
- Help drawer close controls include accessibility attributes (`role="button"`,
  `aria-label`) for the scrim and close action.
- Test coverage expanded for DMARC map token injection rejection and email
  validation edge cases (ASCII/local-part/domain/freemail cases).
- **Port 25 no longer runs the submission milter chain.** `smtpd_milters` is
  now always overridden on `smtp/inet` — to the optional antispam milter, or
  to nothing. Previously, with `INBOUND_ANTISPAM_MILTER` unset, inbound mail
  inherited main.cf's chain: OpenDKIM (strict, `tempfail`) could defer relayed
  mail during a signing outage, and the journal-milter filed inbound mail into
  the outbound send log under the **sender's** domain — so Deliveries showed
  other people's mail, and a forged `From:` on an inbound message consumed that
  sending domain's level-2 rate-limit budget. [architecture.md](docs/architecture.md)
  already described the intended behaviour; the code now matches it.
- **DMARC reports are validated before they are stored.** A report is accepted
  only when the domain in `policy_published` is a sending domain configured
  here and, for a per-domain hosted address, matches that address's `+tag`.
  The rua address form is predictable, so anyone able to reach it could
  previously file reports under any domain, skew the panel's alignment figures
  and the tighten-`p=` hint, and evict genuine reports through the 500-report
  retention cap.
- Auto level-2 rate limit: the ceiling is now scaled to the level-1 window
  before the multiplier is applied. `ceil(avg msg/h × multiplier)` was stored
  as the ceiling for a window of `RATE_LIMIT_WINDOW_SECONDS`, so any window
  other than the default `3600` shifted the effective allowance by
  `window/3600` (a 1800s window allowed twice the intended rate). A domain
  that has sent anything now also gets a floor of 1.
- Single-domain export no longer drops rate limits silently when the store
  errors — the error is returned instead of being swallowed by `err == nil && ok`.

### Changed

- [roadmap.md](docs/roadmap.md) now carries everything the 2026-08-19 code
  review left unimplemented, so no finding lives only in a plan file:
  `csrf-tokens` (the recorded §4.4 decision that never became a task),
  `template-data-typing`, `structured-logging`, and
  `review-2026-08-followups` for the small remainder (backup re-auth, the
  missing service/handler/error-page tests, and nine minor findings that were
  never carried into the review's own phase tables). Each roadmap row stands on
  its own — file, symptom, and what "done" means — and the declined finding
  (`//go:build linux` for the `/proc` reader) is recorded as declined so it is
  not raised again. The review's working document is gone from `docs/plans/`
  now that nothing depends on it; git history keeps it.
- Extracted `internal/configsafe` — the empty-value and forbidden-character
  check every generated config file shares. Each writer keeps its own forbidden
  set next to the file it protects (an OpenDKIM table and a Postfix map break on
  different characters); only the check itself is shared.
- Build, deploy and migration comments no longer cite the archived
  specification. The `spec N` references CHANGELOG `[0.5.0]` said were gone
  survived in `build/`, `deploy/`, the first two migrations and the release
  workflow; they are removed and the affected comment paragraphs re-wrapped.
  Comments only — no build or configuration behaviour changes.
- Dropped the `parseDomainRateLimitForm` / `parseAppRateLimitForm` pass-through
  wrappers left behind when the two parsers were merged: nothing but their own
  tests still called them.
- Extracted shared helpers: `internal/atomicfile` (atomic temp-write+rename) and
  `internal/supervisor` (supervisorctl start/signal wrappers), and reused them in
  domain/postfix code paths.
- `docs/roadmap.md`: shipped `panel-docs` now references CHANGELOG history
  instead of a removed plan file.
- [schema-migrations.md](docs/schema-migrations.md) notes that migration `0009`
  changed what the IP list *means* — permissive (those IPs got the application
  ceiling) before 1.9.0, restrictive (every other IP refused) after. Its
  conversion branch only fires for a database that carried application-scope
  `allowed_ips` from before 1.9.0.
- Panel help, [guide.md](docs/guide.md) and [architecture.md](docs/architecture.md):
  the client IP allow-list is a **sending** control, not an authentication
  boundary — the SASL login still succeeds, the check runs in the
  journal-milter at `MAIL FROM`, and it is fail-open like the rate limits.
  architecture.md's rate-limit description still documented the pre-1.9.0
  trusted-IP semantics.
- Removed unused duplicates of the inbound validators (`Host`, `Port`,
  `TLSMode`, `RecipientMode`, `MailboxInDomain`, `NormalizeHost`) from
  `internal/web/validate` — the live path uses `internal/inbound/validate.go`;
  the copies were reachable only from their own tests.
- `gofmt` across `cmd/` and `internal/` (10 files had drifted); dead branch
  removed from `postfix.inboundNexthop`.
- The planned inbound-antispam migration is renumbered `0011_inbound_spam_log.sql`.
- Operator and as-built docs aligned with the code after a full pass —
  [architecture.md](docs/architecture.md) (port 25 opens for inbound relay
  and/or DMARC ingest; antispam requires inbound relay; DMARC pipe transport
  and map paths; `/help` and `/dmarc*` routes; four panel roles including
  rate-limit recalc; restore Resync vs manual reload); [guide.md](docs/guide.md)
  and [README.md](README.md) (image pin `1.9.1`, port 25 conditions, combined
  listener limits, reload vs Resync); [roadmap.md](docs/roadmap.md) (`panel-docs`
  closed at `[1.8.0]`, next item inbound-antispam-panel); [development.md](docs/development.md)
  version-cut table through `1.9.1`; [schema-migrations.md](docs/schema-migrations.md)
  last-updated note. No behaviour change.

### Removed

- Unused legacy asset `docs/assets/icon.png` (superseded by the four brand SVGs
  and `internal/web/static/favicon.png`; nothing referenced it).
- Plan documents for already-shipped features: `domain-stats-auto-ratelimit`,
  `dmarc-reports`, `inbound-relay`, `panel-docs`, `queue-retries`,
  `send-log-retention`. Only unimplemented plans remain in `docs/plans/`.

## [1.9.1] - 2026-08-19

Port-25 Postfix startup when inbound relay, DMARC ingest, or the inbound antispam
milter are enabled. Schema-migration reference and roadmap plans for inbound
antispam and quarantine. Upgrading from 1.9.0 is a tag bump; no migration.

### Added

- [docs/plans/inbound-antispam-panel.md](docs/plans/inbound-antispam-panel.md) —
  agreed plan for inbound antispam journal (inbound-journal milter + mail.log
  rejects) and instance-wide allow/deny lists synced to rspamd maps; target
  `1.10.0`; Composer → Opus → Fable workflow.
- [docs/plans/inbound-quarantine.md](docs/plans/inbound-quarantine.md) — candidate
  plan for held-mail review/release (design TBD).
- [docs/schema-migrations.md](docs/schema-migrations.md) — living reference for the
  SQLite migration chain (`user_version` head, per-file history, legacy artefacts,
  1.x rules, and planned 2.x squash gate).

### Changed

- [docs/roadmap.md](docs/roadmap.md) — `inbound-antispam-panel` (agreed, 1/12)
  and `inbound-quarantine` (candidate) in the index.
- [docs/development.md](docs/development.md) — plan checklists may override
  model routing with Composer → Opus → Fable.
- [docs/development.md](docs/development.md), [docs/roadmap.md](docs/roadmap.md),
  and [docs/architecture.md](docs/architecture.md) link to the new file;
  `schema-squash` defers gate thresholds to it instead of a stale v5 snapshot.

### Fixed

- `build/postfix-config.sh`: enabling inbound relay, DMARC ingest, and/or the
  inbound antispam milter no longer crash-loops on start — `postconf -P` cannot
  set `master.cf` `-o` values that contain whitespace, so port-25
  `smtpd_recipient_restrictions` and `smtpd_milters` are patched directly; DMARC
  pipe `argv` uses a `dmarc-ingest` symlink instead of `panel -dmarc-ingest`.

## [1.9.0] - 2026-08-18

Application sending controls: client IP allow-list for authorization, and
level-2 rate limits that override the domain ceiling per application.

### Added

- Per-application **client IP allow-list** on the domain page: when enabled,
  only listed addresses may authenticate and submit as that application; when
  off, any client IP is allowed. Form `POST /applications/{id}/authips`.
- SQLite migration `0009_application_auth_ips.sql` (moves legacy trusted IPs
  from `rate_limits` into `applications.auth_allowed_ips`).

### Changed

- Application level-2 rate limit **overrides** the domain limit for that login
  — the ceiling may be higher or lower than the domain setting (still ≤ level
  1). Rate limits are no longer tied to client IPs; enforcement is in the
  journal-milter at `MAIL FROM`.
- Domain export/import carries `auth_ip_restrict` / `auth_allowed_ips`; legacy
  `allowed_ips` on an application `rate_limit` in export JSON migrate on import.
- Panel help, Settings, and [guide.md](docs/guide.md) describe override semantics
  and the IP allow-list boundary.
- [guide.md](docs/guide.md): full-backup section documents all four capture
  methods (panel, `selfpost-backup`, stopped `tar` of the project directory,
  stopped `tar` of `./data` only), warns against `tar` on a live container, and
  compares downtime, archive contents, and restore behaviour in a table.

## [1.8.0] - 2026-08-18

Built-in operator documentation in the panel: a CSS-only help drawer and Help
page so Status checks and domain controls are explained without leaving the
panel.

### Added

- **Help** navigation entry and `/help` index page.
- CSS-checkbox help drawer (no extra script): seeded topics for Status (Machine,
  TLS, PTR, mail queue, inbound when enabled) and domain cards (DNS, records,
  DMARC, connection, applications, settings, export).
- «?» entry points on Status and domain card heads open the matching drawer
  topic.

### Changed

- [guide.md](docs/guide.md) documents the boundary: in-panel help for card
  meaning; the repository guide for installation, env vars, and operations.

## [1.7.0] - 2026-08-18

Optional DMARC aggregate report ingest: accept `rua=` mail on port 25, parse
gzip/XML into SQLite, and show summaries in the panel.

### Added

- Optional DMARC aggregate report ingest (`DMARC_REPORTS_ENABLE`): port 25
  accepts only configured `rua=` addresses, gzip/XML is parsed into SQLite, and
  the panel shows per-domain roll-ups and individual reports under *DMARC*.
- Per-domain **SelfPost hosted** `rua=` mode when ingest is enabled
  (`dmarc-reports+<domain>@<hostname>`).

## [1.6.0] - 2026-08-18

30-day sending statistics per domain and application, plus optional auto
level-2 rate limits derived from average send rate.

### Added

- Domain page **Sending statistics** card: total messages, peak and average
  msg/h over the last 30 days (or shorter when send-log retention is below
  30 days). Per-application stats in the application list.
- Level-2 rate limit **Manual** / **Auto** mode on domain and application
  forms. Auto sets `max_messages` from `ceil(avg × multiplier)` over the
  level-1 window, capped at level 1; background recalc every six hours and
  **Recalculate now** on the domain page.
- Domain export JSON now includes rate limits (`mode`, `auto_multiplier`,
  ceilings, trusted IPs).

### Changed

- Auto mode uses the level-1 window (`RATE_LIMIT_WINDOW_SECONDS`); manual
  mode behaviour is unchanged.

## [1.5.0] - 2026-08-18

Send-log retention is configurable from panel **Settings** (global
administrator); `SEND_LOG_RETENTION_DAYS` seeds the initial default only.

### Added

- Panel **Settings** (global administrator): **Send log retention (days)** —
  how long delivery journal rows on `/deliveries` are kept (7–365). Stored in
  SQLite `settings`; `SEND_LOG_RETENTION_DAYS` seeds the initial default only.
  The log-tailer re-reads the value every prune cycle (no restart).

### Changed

- Deliveries list and delivery detail pages show the configured retention
  instead of a hardcoded ninety-day message.

## [1.4.0] - 2026-08-17

Optional inbound relay (backup-MX / forwarder) on port 25, off by default.
Mail is accepted only for domains configured in the panel and forwarded to
an upstream; there are still no mailboxes. A dedicated security review of
the inbound path is still open on the plan (Fable).

### Added

- `INBOUND_RELAY_ENABLE` (default `false`): when true, Postfix listens on
  port 25 without SASL, only for `relay_domains` + known recipients, and
  forwards via `transport_maps`. Empty upstream host is omitted from the
  maps so mail is never accepted with nowhere to send it.
- Panel **Inbound** pages (global administrator): domains, upstream host/port
  and TLS, recipient list or any-at-domain, MX check against
  `SELFPOST_HOSTNAME`.
- Optional `INBOUND_ANTISPAM_MILTER` on the inbound listener only, plus
  [deploy/antispam/docker-compose.antispam.yml](deploy/antispam/docker-compose.antispam.yml).
  Default milter action is fail-open (`accept`).
- Inbound rate limit (`INBOUND_RATE_LIMIT_MESSAGES_PER_IP`, default 20) and
  `INBOUND_MESSAGE_SIZE_LIMIT` (default 25 MiB) on smtpd port 25.

### Changed

- Compose publishes host port 25 even when the flag is off (nothing listens
  until it is on), the same pattern as 587 / `SUBMISSION_ENABLE`.
- Full-backup restore Resync also rebuilds inbound maps when the flag is on.
  Single-domain export/import is still sending domains only.

## [1.3.1] - 2026-08-17

Retry policy in the panel, persistent Postfix queue, and self-contained
full backups. Mail queue and delivery history show this Postfix's first
retry, backoff cap, and queue lifetime. Deferred mail survives container
recreate. Full backups now archive `data/`, compose, `.env`, and `certs/`;
the archive layout breaks 1.3.0 flat backups (unpack those with
`tar xzf backup.tar.gz -C ./data` as before). Upgrading from 1.3.0 is a
tag bump; no schema migration.

### Added

- Mail queue and a delivery's history show this Postfix's retry policy
  (first delay, backoff cap, queue lifetime), read from `postconf -h` once
  at panel start. A manual `postconf -e` override is visible after the next
  panel restart. There is no attempt counter — Postfix retries on time.

- docs: **Plan checklists** in [development.md](docs/development.md) — format,
  `Progress` column in [roadmap.md](docs/roadmap.md), per-step commit +
  CHANGELOG, version cuts `1.3.1`…`1.8.0` per roadmap stage.
- docs: **Implementation checklists** with model routing in
  [docs/plans/](docs/plans/) (queue-retries, inbound-relay, send-log-retention,
  domain-stats-auto-ratelimit, dmarc-reports, panel-docs).
- docs: roadmap candidates **send-log-retention** (panel Settings for delivery
  journal retention) and **domain-stats-auto-ratelimit** (30-day send stats per
  domain/application, auto level-2 rate limit from avg × multiplier) — plans in
  [docs/plans/](docs/plans/).

- Postfix queue under `/data/postfix/queue` — deferred and active mail survive
  container recreate and are included in full backups.
- Full backup archives the whole operator project: `data/`, `docker-compose.yml`,
  `.env`, and `certs/` (requires `.:/selfpost-deploy:ro` in compose). Restore
  by unpacking into an empty project directory.

### Changed

- **Breaking:** full-backup archive layout — paths are prefixed with `data/`;
  deploy files sit at the archive root. Old flat archives restore with
  `tar xzf backup.tar.gz -C ./data` as before.
- docs: operator guide, architecture, security, and backup UI updated for
  self-contained backups and persistent queue.

- docs: HTML mockups for a full panel UI refresh
  ([docs/assets/panel-ui/](docs/assets/panel-ui/index.html)) — current screens,
  agreed and candidate roadmap surfaces (queue-retries, inbound-relay,
  dmarc-reports, panel-docs), a hybrid width (ops pages use the window, forms
  keep a reading measure), and a 390px emergency layout. The stamp and brick
  palette stay; extra nav icons are proposals only. Design artefact; the
  running panel is unchanged.

### Changed

- docs: panel UI mockups after review — Status restored to live readings
  (queue line, PTR, machine/process tables, Configuration); inbound MX DNS
  check, recipient mode (list or any), and Danger zone beside recipients;
  Backup and Settings as two-column ops pages; Save and Delete user on one
  row; help «?» on domain cards; Host/name ‖ Type height matched to the live
  panel (`b0ebe06`).

- docs: panel UI design system and mockups rebuilt as separate pages
  (`system.html`, `status.html`, `domain.html`, …), not one hash sheet.
  Regions are `stack` / `pair` / `measure` / `fill`; Host ‖ Type is
  `field-row`; Save + Delete is `actions-row`. Shared chrome is `shell.js`.

- docs: panel UI mockups — add-domain sits in the list card (Domains and
  Inbound); DMARC candidate screens drill into a domain roll-up and a
  parsed aggregate report (aligned vs third-party fail), not a hub-only
  summary.

### Fixed

- ci (GHCR): per-arch package tags (`X.Y.Z-amd64`, `X.Y.Z-arm64`) are dropped
  after the manifest merge via the GitHub Packages API. The `[1.3.0]` merge job
  called `docker buildx imagetools rm`, which is not a valid subcommand — cleanup
  failed with a warning and the side-effect tags stayed in the registry until
  this fix.

## [1.3.0] - 2026-08-14

Security and quality after 1.2.5: domain-admin send-log authorization,
fail-closed sign-in and application delete, level-2 rate-limit race fix,
restore Resync, expanded tests, operator docs, release CI, and OFL for IBM
Plex. Upgrading from 1.2.x is a tag bump; no migration.

### Added

- licence: the SIL Open Font License 1.1 text now travels with the IBM Plex
  WOFF2 files (`internal/web/view/static/OFL.txt`). The image copies it next
  to LICENSE and NOTICE under `/usr/share/doc/selfpost/`; the panel serves it
  at `/static/OFL.txt`. OFL requires the licence to accompany the font.

- docs: agreed roadmap item **queue-retries** — show this Postfix's retry
  policy (first delay, backoff cap, queue lifetime) on Mail queue and on a
  delivery's history, reading `postconf -h` once at panel start so a manual
  override is visible. Plan: `docs/plans/queue-retries.md` (removed once shipped).
  Explanation only; no attempt counter and no panel knobs. Not yet
  implemented.

### Security

- The independent security review of the send-log authorization and
  fail-closed fixes below (code-review plan § P7; reviewer model ≠ author
  model) found no further issues: the domain scope holds on every query path,
  a rate-limit refusal cannot consume window budget, and each failure residue
  of the reordered application delete fails safe. Nothing was added to
  [docs/security.md](docs/security.md) § Accepted risks; the review is
  recorded in that file's header.

### Fixed

- test (e2e): send-log status scrapers follow the badge markup in
  `deliveries_rows`. The release gate still looked for bare `<td>sent</td>`
  after the panel started rendering status as `<span class="st st-*">` badges,
  so `send_verify_dkim_and_status` timed out even when mail was delivered and
  logged. A handler regression test catches this drift in `go test ./...`
  without Docker.

- security (panel): the Deliveries list is scoped to a domain administrator's
  assigned domains for every number of assignments. Previously the send log was
  narrowed only when exactly one domain was assigned, so an administrator with
  none or with two or more read every domain's rows (sender, recipient, subject)
  on the list and its polled fragment. The domain scope is now an `IN`
  constraint carried by the store query — a filter that states no scope returns
  nothing — and the `domain` and `app` query parameters are checked against the
  principal's own domains and applications before the query runs, so a
  hand-written URL cannot widen the scope. Global administrators are unaffected.

- mail (level-2 rate limit): the ceiling is no longer overshot by messages that
  arrive at the same instant. The milter counted the stored and in-flight
  messages and reserved its own slot in two separate steps, so several SMTP
  sessions could pass the same check before any of them had reserved. Counting
  and reserving now happen as one operation, and the ceiling is handed out
  exactly as many times as configured. Postfix's level-1 limit remains the
  backstop and the level-2 check stays fail-open on store errors.

- panel (sign-in): a session that cannot be written to the database no longer
  produces a session cookie. The login used to log the failure, set the cookie
  and redirect to the dashboard, leaving the browser looking signed in while
  every request bounced back to `/login`; it now fails closed with an error on
  the sign-in page.

- panel (applications): deleting an application removes its SASL credentials
  before its registry row. If `saslpasswd2` fails, the application stays listed
  and the delete can be retried, instead of leaving a hidden account that could
  still authenticate to Postfix. This matches the order domain deletion already
  used.

- panel (GUI): a rejected rate-limit change on a domain's page now renders on
  the danger surface (`.flash.error`) instead of the success one — it was
  green with red text, reading as good news. Deleting a panel user now goes
  through a confirmation page, the same pattern as domain deletion, instead of
  a plain submit button next to Save with no confirmation at all.

- panel (restore): after a backup is extracted and the version guard passes,
  the panel runs one mail-path Resync on the first boot — OpenDKIM's tables
  and Postfix's sender map are re-derived from SQLite and the daemons are
  reloaded, so drift between the archive and the database is healed before
  mail flows. Later starts skip that step; the Status page Reload button runs
  the same Resync on demand. The `internal/backup` package comment now matches
  this behaviour.

### Changed

- docs: operator and as-built docs aligned with the code after a full
  pass — [architecture.md](docs/architecture.md) route table now marks
  **global** routes (404 for domain administrators) and documents the
  one-time restore Resync in Persistence; session/password and restore-session
  wording corrected in [guide.md](docs/guide.md) and architecture (own-password
  change vs admin reset, no "logout everywhere", immediate session restore on
  the next request, PTR cache ≈1 min, decrypt has no version check, restore
  Resync on first boot, domain add/delete/import and `POST /reload` global-only,
  Settings DMARC global-only); [README.md](README.md) port-587 and quick-start
  volume wording fixed; [security.md](docs/security.md) CSRF ADR points at
  `authz.go` for route gating. No behaviour change.

- docs: [guide.md](docs/guide.md) reorganised into **Installation**, **Instance
  administration**, and **Domain administration** — DNS setup, operations,
  rate limiting, and backup sections follow the instance/domain boundary
  instead of mixing them. **Installation** now reads Ports → Local trial →
  Initial setup → Full deployment (with the fixed image tag nested under it) →
  Environment variables → Reverse proxy; the step-by-step production deploy and
  per-proxy TLS commands move here from README's "Reference deploy" (README
  keeps a short pointer). Internal (non-operator) environment variables move
  to [architecture.md](docs/architecture.md) § Configuration; the guide keeps
  a one-line pointer. **Full backup and restore** gains worked commands for
  in-place restore, move-to-a-new-host, and encrypted-backup decrypt-first,
  plus the version-mismatch error text. README anchors updated for the new
  headings. No behaviour change.

- docs: the 2026-08-13 full-tree review plan is complete — every phase (P0–P7)
  is closed — and `docs/plans/code-review.md` is deleted per its own exit
  criteria (history in git and in this file). The plan covered architecture,
  quality, GUI, tests, and licence work; P0 was domain-admin send-log
  authorization. The [roadmap](docs/roadmap.md)'s recommended order returns to
  **queue-retries** and then **inbound-relay**; it still records
  **schema-squash** (replace the 1.x SQLite migration chain with a 2.x baseline;
  not a reason to cut a major on its own).

- licence: [NOTICE](NOTICE) tells modifiers to update `SourceURL` in
  `internal/legal/legal.go` (the value the panel footer actually injects), not
  `layout.html`. Per-file `SPDX-License-Identifier` headers on the two command
  packages were dropped so the tree is consistent; AGPL-3.0 does not require
  them ([development.md](docs/development.md) § External libraries). Deleted the
  completed `docs/plans/logrotate-mode.md` (history in git and
  [1.2.3](#123---2026-08-12)).

- ci: the release image is published only for a **published** GitHub Release
  (`vX.Y.Z`) or a manual `workflow_dispatch` with an explicit SemVer version — a
  bare git tag push no longer starts the build. `release.yml` listens for
  `release: published`, checks out that tag (not `main` HEAD), e2e-gates each
  native arch build, and merges `X.Y.Z-amd64` and `X.Y.Z-arm64` into one
  manifest. Reliable removal of the per-arch side-effect tags from GHCR landed
  in `[1.3.1]`. A dispatch whose version input is missing or not `X.Y.Z` fails
  in `prepare`. [development.md](docs/development.md) documents draft vs
  published releases, why deleting a release tag converts it back to draft, and
  Gitea → GitHub tag-mirror pitfalls (do not prune release tags on GitHub; a
  mirrored `v1.0.0` still runs that tag's `on: push: tags` workflow).

- test: the authorization and sign-in surfaces that had no tests now have them.
  The login limiter is covered for its ceiling, its per-address scope, the reset
  at the end of a window and the sweep that keeps finished buckets out of
  memory; sign-in for a successful session, for refusals that do not reveal
  which usernames exist, and for a lockout that a correct password cannot
  bypass; the one-time setup link for creating the first administrator, closing
  afterwards, rejecting a wrong or expired token, and refusing credentials the
  panel would not accept later. Every global-only route (`/users`, `/backup`,
  domain import, `/status`, `/mail-queue`, `/system-log`, domain add and delete,
  reload) is checked to answer a domain administrator — and a request with no
  principal — with 404, the check that would have caught the send-log leak.

- test: restore is covered as the operator performs it, in process. A backup is
  downloaded from a running panel through `POST /backup` (plain and encrypted),
  unpacked the way `tar -xzf` unpacks it onto the `/data` bind mount, and a
  second panel is booted on the result through the startup order the panel
  itself uses — version guard, database, one Resync when restoring, then
  services and the HTTP application. The restored panel shows the domain and
  journal the archive carried, finds the DKIM key, SASL database and Postfix
  sender map where its configuration says they are, does not reopen the
  one-time setup link, and still honours a session that predates the backup.
  Drifted on-disk maps are healed by that Resync step
  (`TestResyncAfterRestoreHealsDriftedMaps`). A data directory left by another
  version is refused with both versions named and the manifest kept. `serveHTTP`
  is split in two so that composition can be started without binding a port; no
  behaviour change.

- test (e2e): the CoreDNS image is pinned to `1.14.6` instead of `latest`, so
  the release gate cannot change under a commit between two runs. The level-1
  rate-limit failure message quoted `RATE_LIMIT_MESSAGES_PER_IP=5` while the
  stand sets `50`.

- ci: gofmt on eight files that failed the formatting workflow check (panel
  config, DNS check, domain transfer export, rate-limit tests, auth principal,
  domain and delivery handlers, web package doc comment).

- panel (templates): the repeated Host/Type/Value DNS record markup on a
  domain's page and the duplicated credentials form on Settings are now
  shared partials (`host_type`, `host_type_copy`, `field_value`,
  `field_values`, `credentials_fields`) instead of copy-pasted blocks. No
  behaviour or visible change.

- panel (GUI, accessibility): the Deliveries fragment's `hx-get` and pagination
  links now `urlquery`-encode the `domain`/`app` filters instead of splicing
  them into the query string raw. The four polled regions (deliveries rows,
  status, mail queue, system log) carry `aria-live="polite"` so a screen
  reader announces the refreshed content.

- docs: [security.md](docs/security.md) accepted risks now note that
  `data-confirm` prompts on destructive forms are JavaScript-only — with
  JavaScript disabled the form submits immediately, the same as before the
  prompts existed — and why that is acceptable (the prompt is a mis-click
  guard, not an authorization boundary).

- docs: security and operator docs updated for the panel that has shipped
  global administrators and domain-admins since 1.2.0. The CSRF ADR in
  [security.md](docs/security.md) no longer argues from "single-user"; it now
  states that cross-user CSRF between panel roles is not the threat the origin
  check defends against, and gives a new revisit trigger. Dropped the
  unimplemented "or argon2" alternative for the password hash.
  [guide.md](docs/guide.md) documents the Users page and the two roles,
  level-2 rate limiting's fail-open behaviour, and that a domain-admin can
  export working SASL passwords for domains assigned to them.
  [architecture.md](docs/architecture.md) gains `/license` and the
  `/account` → `/settings` redirect in the route table (later expanded for
  RBAC in the doc-alignment pass above). Corrected stale
  `admin.dmarc_report_email` references in
  [roadmap.md](docs/roadmap.md) and
  `docs/plans/dmarc-reports.md` (removed once shipped) to the setting's
  actual home after migration `0005`. No behaviour change.

- panel: code-review P6 cleanup — the unused `auth.RequireGlobal` middleware is
  gone (handlers already call `requireGlobal`); the settings route handler is
  named `HandleSettings` in `handlers_settings.go`; domain lists for a
  domain-admin now come from `ListDomainsForUser` in SQL instead of loading
  every domain and filtering in Go; the login and setup rate limiters sweep
  expired buckets on a timer and cap the map at 4096 keys; the five
  show/hide field helpers in `panel.js` are one rule table; DMARC copy no
  longer promises in-panel report reception in a future release — SelfPost
  does not receive inbound mail.

## [1.2.5] - 2026-08-13

Rate-limit form polish after 1.2.4. Upgrading is a tag bump; no migration.

### Changed

- panel: rate-limit forms refined — level-1 backstop as muted copy on the
  domain settings form (`N messages / Ws — Settings`) and in message-limit
  labels (`max N`); Settings shows the L1 ceiling in a code-row. Domain,
  application, and Edit-toggle limit state use the shared `st` badge instead of
  bold text or parenthetical copy. Address mode and trusted-IP override columns
  carry muted leads and matched control height; trusted-IP help sits under the
  IP field. Domain settings pairs DMARC reports with the level-2 rate limit
  using CSS subgrid so titles, fields, and Save / Remove buttons line up across
  columns. Domain page Export and Danger cards stack naturally again — a
  shared-baseline flex pin on `.split` cards made mismatched body lengths look
  worse than a plain stack.

## [1.2.4] - 2026-08-12

Level-2 rate-limit semantics inverted after 1.2.3, plus a small DNS field
height fix. Upgrading is a tag bump; no migration.

### Changed

- rate limiting (level 2): domain ceilings apply to every client IP (no IP
  allowlist). An application ceiling with trusted IPs is an override
  **above** the domain limit (still ≤ level 1) and skips the domain check for
  those IPs; without IPs the application override is inactive. When no domain
  ceiling is set, non-privileged senders use level 1 only. The panel shows the
  level-1 backstop on domain/application forms and Settings, rejects maxima
  above level 1, and requires an application override to exceed the domain
  maximum. Operator guide and architecture updated.

### Fixed

- panel: on the domain DNS status grid, the Type (TXT) field height matches
  the Host fields.

## [1.2.3] - 2026-08-12

Domain detail layout and panel polish after 1.2.2. Upgrading is a tag bump; no
migration.

### Changed

- panel: the domain detail page is wide with paired cards (DKIM+SPF ‖ DMARC;
  connection settings ‖ add application; export ‖ danger). DNS status,
  Applications and Domain settings are full-width. DNS status is two rows
  (DKIM ‖ SPF, DMARC ‖ report authorization) with Host ‖ Type (narrow TXT)
  and a Value label when records are present. Domain settings pairs DMARC
  report mode with the optional level-2 domain rate limit; application Edit
  opens address mode and an optional level-2 application rate limit side by
  side (with a note that domain level-2 and global level-1 still apply); the
  custom `rua=` address field is shown only for Custom address. The in-nav
  “On this page” section index is removed. Shorter blurbs; *Sending server
  settings* renamed **Connection settings**.
- panel: Domains list — **Add domain** sits beside the domain field; the
  lead blurb under the form is dropped.
- panel: page URLs, browser titles, and headings are aligned — **Settings** is
  now `/settings` (legacy `/account` redirects with 308); the domains list title
  is `SelfPost — domains`; Status, Users, and user create/edit titles match their
  nav labels and `<h1>` text; the backup page title is `SelfPost — backup &
  migration` to reflect domain import as well as full backup. Operator guide and
  architecture route tables updated.

### Fixed

- image: `mail.log` rotation no longer silently stops when the build context
  ships `logrotate-mail.conf` with group/other write bits (common after a
  Windows checkout sync). Runtime `COPY --chmod` pins config and script modes
  in the Dockerfile; `logrotate-loop.sh` refuses a config logrotate would
  ignore. E2e checks mode `644`, forced rotation, and a group-writable context
  build.

## [1.2.2] - 2026-08-12

Status page layout after 1.2.1: paired cards in a wide column, denser machine
and check copy, and a user-form checkbox fix. Upgrading is a tag bump; no
migration.

### Changed

- panel: **Status** is wide again so paired cards fill the column. Layout:
  Overall; Machine ‖ Processes; Mail queue ‖ TLS certificate; Milter sockets ‖
  Hostname / reverse DNS; Configuration. Dropped lead blurbs on Machine, TLS
  certificate, and Hostname (and Hostname's trailing detail line); milter
  socket paths omitted from the table; queue link reads **View queue**; milter
  ok detail is `Listening` without a trailing period and sits in its own
  Detail column beside the status badge; CPU detail is only core and thread
  counts (no load average); memory detail is `N used of M` without the
  «available to new work» clause; network detail lists per-interface totals
  only (rates stay in the Usage column). No «On this page» section index —
  the paired layout is short enough. Hostname stays in the polled fragment so
  the pair survives HTMX refresh. In-panel docs for the removed blurbs filed
  as roadmap `panel-docs`.

### Fixed

- panel: cards inside `.split` used `margin: 0 auto`, which in a CSS grid
  shrinks each card to its content and centres it in the track instead of
  filling half the row. Side margins are cancelled for `.split > .card`
  (Status, Settings, and a delivery's message/history).
- panel: on the user create/edit form, **Assigned domains** checkboxes stacked
  the box above the domain name (and stretched it full-width) because the form's
  block-label and full-width input rules applied to them. Checkbox rows now use
  the shared `label.check` layout; the fieldset has matching spacing.

## [1.2.1] - 2026-08-11

Panel refinements after 1.2.0: navigation icons, status-badge centreing,
drill-down back-link placement, and safeguards for the sole global
administrator. Upgrading is a tag bump; no migration.

### Fixed

- panel: status badge text sat low in the box (and below the heading or label
  beside it). IBM Plex Mono sits low in its em square; the previous top-heavy
  padding made that worse. Bottom padding is now heavier so the word centres
  optically.
- panel: the **Users** navigation icon was two full silhouettes with staggered
  baselines, so the pair looked lopsided at 16 px. The rear person is now a
  right-side crescent (head + shoulder) behind a full front silhouette aligned
  with `icon-account`.
- panel: the **Settings** navigation icon was a sun-with-rays (circle plus
  spokes), not a gear. It now uses a toothed cog so it matches the other
  session icons and the 1.2.0 release note.
- panel: the user create/edit form placed «Back to users» at the bottom of the
  card instead of under the heading like the delivery, domain, and domain-delete
  pages. A shared `back_link` template now renders every drill-down up-link, and
  `TestDrillDownPagesPlaceBackLinkAboveContent` guards its position.

### Changed

- panel: the user edit form disables role change and delete for the only global
  administrator, with a short note, instead of allowing the action and showing
  an error on submit.
- panel: the user create/edit form hides **Assigned domains** when the role is
  global administrator, since that role manages every domain anyway.
- panel: **Settings** shows panel credentials and DMARC aggregate reports side
  by side for global administrators (the same `.split` layout as a delivery's
  message and history). Domain-scoped users keep the single narrow card.

## [1.2.0] - 2026-08-11

The second MINOR after 1.0.0: domain administrators with per-domain scope, a
panel visual refresh on the SelfPost palette, and refinements to navigation and
the send log. Upgrading runs one SQLite migration (`0005_panel_users`); the
single administrator becomes a global user.

### Added

- panel: **domain-admin role** — global administrators manage panel users and
  assign domains; domain administrators see only their domains (applications,
  DKIM/DNS, per-domain DMARC, deliveries, export, L2 limits). Status, full
  backup, mail queue, system log, domain add/delete, and `/reload` stay
  global-only. SQLite migration `0005_panel_users` migrates the single
  administrator into a global user; sessions and full backup restore carry users
  and domain bindings.

### Fixed

- Docker build: `LICENSE` is no longer excluded by `.dockerignore`, so the
  runtime image can copy it into `/usr/share/doc/selfpost/` as AGPL requires. A
  clean build failed once the cached layer was invalidated.
- panel: on signed-in pages with a narrow card (**Settings**, the user form)
  the heading, flash, card and footer now share one left edge. `.card.narrow`
  had overridden only `max-width` while `main > *` still centred siblings on
  the 48rem measure, so the card floated 12rem to the right of the title.
  `main:has(> .card.narrow) > *` keeps the stack on 24rem without narrowing
  the column, so the navigation stays put; login/setup are unchanged.

### Changed

- panel: navigation session icons are distinct — **Settings** uses a gear,
  the signed-in user line carries the single-user icon, and **Users** a
  two-person group mark instead of the same account silhouette for all three.
- panel: `/` redirects domain administrators to `/domains`; global users still
  land on `/status`. Navigation hides global-only sections for domain
  administrators.
- panel: **Sign out** in the navigation column uses the same type size and
  weight as the page entries above it; only the red tint and border mark it as
  destructive.
- panel: **visual style** brought in line with the SelfPost mark — brick accent
  and warm paper in place of the blue-on-cool-grey defaults, IBM Plex Sans and
  IBM Plex Mono served by the panel itself, squarer corners, and column
  headings, status badges and small labels set in the mono face. Light and dark
  schemes both keep their contrast; no page, control or workflow changed. The
  send log stops breaking `Details` and `deferred` across two lines when a row
  is tight. Badge padding and line-height are tuned so lowercase labels sit
  centred in the box. The three WOFF2 files add ~76 KB to the image and are
  served from the panel's own origin, so the Content-Security-Policy is
  unchanged (`default-src 'self'`).
- panel: the send log's **status is a badge**, in the same ok/warn/error/unknown
  colours the status page and the DNS checks use, instead of the one place in
  the panel where a status was bare text. The mapping is the one the delivery
  page already applied — `sent` is ok, `deferred` a warning, `bounced` and
  `rejected` errors, `queued` unknown because nothing has gone wrong yet.
- docs: [roadmap.md](docs/roadmap.md) and [product.md](docs/product.md) no
  longer list domain-admin or visual-style as open work — both ship in this line.
  Completed plan files (`domain-admin`, `visual-style`, `web-split`,
  `narrow-page-alignment`) are removed; history stays in git and the entries
  above. Inbound relay is the main agreed 1.x+ item left on the roadmap.

## [1.1.0] - 2026-08-10

The first MINOR after 1.0.0: send-only DMARC guidance in the panel, AGPL
packaging on every page, and an internal split of `internal/web` ahead of
domain-admin work. Upgrading runs one SQLite migration (empty defaults;
existing DNS guidance is unchanged until you set a report address).

### Changed

- `internal/web` split into subpackages (`web/view`, `web/auth`, `web/validate`,
  `web/handlers`); the composition root (`web.New`, `web.Config`, `Server.Handler`)
  is unchanged for `cmd/panel`. Templates and static assets moved under
  `internal/web/view/`.

### Added

- panel: DMARC guidance for send-only relays — the suggested `_dmarc` record
  is now `p=none` without `rua=` by default; *Settings* and each domain page
  let you configure an optional aggregate-report address (profile default plus
  per-domain inherit / none / custom). When `rua=` targets another domain, the
  panel shows and DNS-checks the hub's `_report._dmarc` authorisation record.
  Domain export/import carries per-domain overrides.
- AGPL packaging hygiene: [NOTICE](NOTICE) names the copyright holder and the
  bundled third-party works (htmx 0BSD, IBM Plex OFL in outlined logos); the
  panel footer on every page — including login and setup — shows copyright, a
  link to `/license` (embedded AGPL text), a Source link to the public
  repository, and "No warranty"; the runtime image ships `LICENSE` and
  `NOTICE` under `/usr/share/doc/selfpost/`. `docs/development.md` now lists
  the vendored htmx asset beside the Go module licences.
- [docs/roadmap.md](docs/roadmap.md) — candidate item **visual-style** (panel
  visual refresh: typography, colour tokens, spacing, and component styling
  without behaviour changes). Starting reference:
  [docs/assets/selfpost-proof.html](docs/assets/selfpost-proof.html). No semver
  impact; explicit agreement required before coding, like other candidates.

## [1.0.1] - 2026-08-09

A documentation and packaging release: no change to the mail path, the
database, or the on-disk layout. Upgrading is a tag bump.

### Added

- `SECURITY.md` — how to report a vulnerability privately (GitHub private
  vulnerability reporting, `public@mixeme.ru` as fallback), which releases get
  fixes, and what is in and out of scope for a relay. No response time is
  promised. Without it a finder's default move is a public issue, which
  discloses a relay flaw to everyone the moment it is filed.
- [docs/plans/](docs/plans/) — one document per agreed extension: the optional
  inbound relay, the domain-admin role, and splitting the oversized `web`
  package. Each states scope, open questions, and what has to be true before
  coding starts. [docs/roadmap.md](docs/roadmap.md) is restructured around them
  as a 1.x+ tracker instead of a 2.x wishlist, and now says how to read it from
  outside the project: nothing in it is a commitment, there are no dates, and
  the stated order is a recommendation.

### Changed

- The panel's **Account** entry is now called **Settings** — nav link, page
  heading, browser title, and the operator guide. The route stays `/account`,
  so existing links and bookmarks are unaffected.
- The signed-in name in the panel's nav is now labelled `User:`, so it reads as
  the current account rather than as a stray word above the Settings link.
- [docs/product.md](docs/product.md) reframes the future line: the inbound
  relay and the domain-admin role are agreed **1.x+** extensions tracked in the
  roadmap and the plans, with the inbound relay targeting a MINOR bump by
  default and a 2.x major still possible pending implementation. Only items the
  roadmap still marks *candidate* need explicit approval before coding. It
  previously put the whole line behind a 2.x.x that nothing had committed to.
- [docs/security.md](docs/security.md) is now in English, matching the rest of
  the published docs — it is linked from the README table and from
  `SECURITY.md`, so a reader following either landed in Russian. Content is
  unchanged: same requirements, same accepted risks, same ADR. The reviewing
  model is no longer named in the text; the fact that a pre-release review ran,
  and its date, stay. The roadmap and the plans are in English for the same
  reason, and neither records the model assigned to an item any more.
- The README documentation table now points at `SECURITY.md` for reporting a
  vulnerability, and the `docs/security.md` row is renamed *Security design* —
  with two files a reader could reasonably call "security", the table said
  which is which only by accident. The roadmap row no longer calls the file
  internal and Russian, because it is neither. `development.md` lists
  `SECURITY.md` among the user-facing deliverables.
- `docs/development.md` records the decision on authorship: SelfPost is written
  by AI agents under a maintainer's direction and the project discloses that,
  so the `Co-Authored-By` trailers, the model routing table, and the agent
  rules file all stay. Written down to settle the question rather than have it
  reopened at each release.
- The two remaining Russian source comments are in English:
  `deploy/traefik/extract-cert.sh` (quote from spec 10.3) and
  `internal/app/sasl.go`, where the quotation from the closed plan is dropped
  rather than translated — rendered in English it restated the sentence it was
  attached to. The Cyrillic that remains is test data, where it is the point.

### Fixed

- The panel's static assets are served with a content ETag and
  `Cache-Control: no-cache`. They are embedded in the binary, so their
  modification times are the zero value and no `Last-Modified` was sent; with
  no validator at all the browser was free to guess how long to keep them,
  which is why a tab kept showing the previous favicon after the new mark
  shipped. Each asset is now hashed once at startup, so an unchanged one costs
  a bodyless 304 and a changed one is picked up on the next load. A browser
  that cached an asset *before* this release still has nothing to revalidate
  against, so that one copy has to be cleared by hand.

## [1.0.0] - 2026-08-09

### Added

- A **Delivery log** on each delivery's page (`/deliveries/{id}`): the
  `mail.log` lines Postfix wrote about that message, oldest first — the
  connection to the receiving server, its reply, and the status that reply was
  filed as. It is a table of two columns, when and what, so the seconds between
  the connection and the reply line up down one edge; the timestamp is the
  log's own wall clock without its microseconds and offset, and a line whose
  head is not a timestamp keeps its whole text under *Message*
  (`logtail.SplitTimestamp`, which reads both postlogd's format and syslog's).
  The queue id was printed on this page as something to go and search
  the system log for by hand; the search is done for the operator instead
  (`logtail.QueueLines`). The read is a bounded tail of the current log and
  matches only lines carrying this message's queue id, anchored so a shorter id
  is not found inside a longer one. Send-log rows outlive `mail.log` — retention
  is ninety days, rotation keeps fourteen files — so a message with no lines
  left says so rather than reporting a failure.
- A **History** block on the same page: the journal's two timestamps stated as
  the steps they stand for — accepted and queued, then delivered, deferred,
  bounced, or refused before queueing — each with the status it reached in the
  panel's own ok/warn/error/unknown badge vocabulary. A message still queued
  shows the delivery report it is waiting for as a step that has not happened.

### Fixed

- Postfix config: copy TLS cert/key from the (often `:ro`, host-owned) mount
  into `/etc/postfix/tls-internal` as `root:root` before `postconf` /
  `postfix check`. Bind-mounted keys owned by the CI/host UID made
  `postfix check` fail and the container exit before supervisord started.
- Postfix config: allow `maillog_file` under `/data` via
  `maillog_file_prefixes=/var,/dev/stdout,/data`, and pin the `postlog`
  master.cf service. After mail.log moved to `/data/log`, `postfix check`
  fatally rejected the path (default prefixes are only `/var` and
  `/dev/stdout`) and often left stderr/mail.log empty.
- E2e: read `/data/setup-token` via `docker compose exec` (file is `0600`
  panel-owned; host `ReadFile` got permission denied on CI). Reclaim `/data`
  ownership before TempDir/stage cleanup so panel/postfix UIDs do not fail
  Go's `RemoveAll`.
- Entrypoint: check `SELFPOST_HOSTNAME` before `/data` setup (so a bad identity
  fails with the FATAL text, not an earlier `set -e` abort), and `chmod 755
  /data` after chown so OpenDKIM/Postfix can traverse bind mounts that arrive
  as mode `0700` (Go `TempDir`, some host umasks) — otherwise KeyTable is
  unreachable and the container crash-loops. E2e stand uses `restart: "no"` and
  surfaces selfpost logs from the supervisor readiness check.
- E2e gate: wait for host-published `/healthz` before panel setup, and stop
  ordered `TestE2E` subtests after a failure so a nil panel client cannot panic
  and mask the real error (release CI on both amd64 and arm64).
- Release CI: retry `docker push` / `imagetools create` on transient GHCR
  `unknown blob` (and similar) errors after layers already uploaded.
- A send-log row could stay `queued` forever after the container was recreated.
  `mail.log` moved from the ephemeral `/var/log` into the data volume
  (`/data/log/mail.log`, `./data/log/` on the host), so the delivery lines that
  resolve a queued row now outlive the container the same way the journal does.
  `postlogd` writes the file as user `postfix` and the unprivileged panel reads
  it through the shared `selfpost` group (directory `2750`, file `0640`,
  re-normalised on every start); logrotate creates each new file the same way.
  Existing deployments need no action beyond the upgrade — the directory is
  created on first start — but the log written by the previous image is gone
  with its container, and the tailer starts the new file from its end.
- A row whose delivery lines are gone for good is no longer left `queued`
  indefinitely: every five minutes the tailer compares rows still queued from
  more than two minutes ago against `postqueue -p`, and marks `bounced` those
  whose message Postfix no longer holds — it will never report on them again.
  The sweep waits until the tailer has read the log to its end (on a restart the
  log itself holds the answer) and does nothing at all if the queue cannot be
  listed, so a message merely in flight, or a `postqueue` that fails, never
  closes a row.

### Changed

- Document what `.spbk` and `.spde` stand for (SelfPost backup / SelfPost domain
  export) in the operator guide, security notes, architecture, and the Backup /
  Export panel copy.
- Docs aligned with the code: setup URL is `/setup/<token>` (README and
  guide; local trial rewrites the printed `https://<hostname>/…` link to
  `http://127.0.0.1:8080/…`); domain import uses the file extension / magic
  bytes for the password field, not an "encrypted" checkbox; architecture
  layering and route table match `web`→`store` and `POST /domains/import`;
  OpenDKIM drops to `opendkim` via `UserID`; guide drops the archived
  "spec 7.5" pointer, clarifies `POSTFIX_SENDER_LOGIN_MAPS` vs panel writes,
  and states logrotate keeps 14 daily files. Intermediate CHANGELOG cuts from
  before the published `1.0.0` image are called out in the guide and
  `development.md`. Roadmap points at CHANGELOG `[0.5.0]` Security and
  refreshed `internal/web` size / symbol links.
- Full backups no longer carry `/data/log`. It is Postfix's raw log plus its
  fourteen rotated copies — diagnostic output rather than state to restore, and
  otherwise by far the largest thing in the archive.
- Monitoring screens (status, mail queue, system log, deliveries) use adaptive
  HTMX polling: 5 s while the operator is active on the page, 30 s when the tab
  is visible but idle, and no requests while the tab is hidden. Scheduling
  lives in `panel.js` (`data-poll` markers) instead of `hx-trigger="every …"`,
  which would need `unsafe-eval` under the panel's CSP.
- Documentation package consolidated into `docs/development.md`: Documentation
  map, user-facing deliverables, maintenance rules, and code-to-prose
  verification table (from closed `documentation-plan.md`); resuming work,
  model routing, commits, and phase closure (from closed `progress.md`).
  `docs/archive/` removed — history is git + CHANGELOG. README Documentation
  index lists operator docs plus the internal roadmap. Agent rules point at
  `development.md`.
- The delivery page is laid out in two columns: what the journal recorded on
  the left, what happened to the message on the right, and the delivery log at
  full width under both. The facts the page used to stack one per line — domain,
  application, queue id, journal id and the two timestamps — are a grid of tiles
  instead, since a page of mostly empty rows was what the full-width stack came
  to for six short values. The subject heads the page and the sender, recipient
  and outcome are the line under it, so what the message was and how it ended
  are both on the first line. The page takes the whole column rather than the
  reading measure, as the other three monitoring pages already did.
- `docs/development.md` restructured into stack, dependencies, build, release,
  testing, and CI; agent rules moved to `.cursor/rules/agent-rules.mdc`;
  dev-host-specific workflow and `example.com` references removed from docs.
- `docs/development.md` and `.cursor/rules/agent-rules.mdc` translated to
  English; `roadmap.md` remains Russian (internal tracker).
- Deploy pin and local-trial image tag set to `ghcr.io/mixeme/selfpost:1.0.0`
  (compose and git tag `v1.0.0` cut together). Retired
  `docs/implementation-plan.md` and `docs/v1.x-closure-plan.md`; Makefile,
  `release.yml`, and e2e comments point at `docs/development.md`. Roadmap
  v1.x documentation/deploy tail closed.

## [0.6.0] - 2026-08-08

### Added

- A page per delivery (`/deliveries/{id}`), reached from the *Details* link on
  every send-log row. It carries what the log itself no longer shows — the
  sending domain, the application the message was submitted under, the Postfix
  queue id to search the system log for, when the status was last reported —
  so the journal can grow fields without the table having to find columns for
  them. *Back* returns to the page and filters the row was opened from, rebuilt
  from the log's own parameters only.

- A **DNS** badge in the domain list, one per row, carrying the same
  ok/warn/error/unknown vocabulary as the rest of the panel: the worst of that
  domain's DKIM, SPF and DMARC checks, so a domain whose records were never
  published is visible without opening it. The badge links to that domain's
  DNS status card. The checks run concurrently across the listed domains —
  each carries its own timeout, and in series a dead resolver would multiply
  that wait by the number of domains — and share the checker's cache with the
  domain page, so a repeat view costs no lookups. A domain whose DKIM key
  cannot be read stays "unknown" rather than being reported as misconfigured,
  since the missing half is this server's.
- Machine metrics on the status page: a **Machine** card reporting the
  processor (busy percentage, core count, load average), memory and swap, and
  network throughput and totals per interface, read from the kernel's counters
  in `/proc` (`internal/health/machine.go`). CPU and throughput are differences
  between two readings, so they are measured against the previous poll of the
  status fragment and reported as still being measured until a second reading
  exists — a page opened after a long idle stretch re-baselines rather than
  presenting that stretch as the current load. A fully busy processor (≥90%)
  warns and an exhausted machine (≥97% of memory in use) errors, both counting
  towards the page's headline verdict, since either delays or kills the mail
  path; throughput is reported and never graded. Counters that cannot be read
  — no `/proc` outside Linux — leave the card in place showing "unknown". The
  usage bars are `<meter>` elements: the panel's CSP has no inline-style
  exemption, so a bar's length has to travel on an attribute.
- An index of the current page's own sections in the navigation column, for the
  two pages long enough to need one: the domain page (nine cards, from the DNS
  records to publish down to the danger zone) and the status page (eight). Each
  card carries an id and the page's template defines the list
  (`{{define "sections"}}`); every other page defines nothing and shows no
  index. `panel.js` marks the section currently in view, looking its targets up
  by id on each pass so the status page swapping its cards out every five
  seconds cannot leave it measuring boxes that have left the document. The
  links are plain fragment links and work with JavaScript blocked; only the
  highlight needs it.

### Changed

- Every panel page is laid out in one column of the same width, so moving
  between them no longer shifts the navigation and the cards sideways. The
  column used to be the 48rem reading measure, which the send log, the mail
  queue and the system log widened to 64rem for their tables — and since the
  navigation and the page are centred as a pair, that difference moved
  everything on screen on the way between two pages. The column is now 64rem
  throughout, with the reading measure kept inside it: a page's heading, cards,
  back link and version footer are held to 48rem and centred in the column,
  and the three pages made of data opt out and take the column whole. Which
  pages those are is declared by the page itself (a `wide` block in its
  template, the same mechanism as the section index) rather than derived from
  the navigation entry, so a single delivery's page — prose, but filed under
  the send log — keeps the measure. The scrollbar's width is now reserved on
  every page as well: without it a short page and a long one were laid out in
  viewports differing by that width, which moved the same things again.

- The mark's small-size variant — the tab icon and the `SP` initials it
  carries — sets its S in Medium where the wordmark sets it in ExtraLight.
  Against the P's SemiBold the ExtraLight S is a 0.90 stem against 3.40,
  which at 16px is a quarter of a pixel against most of one, so the pair
  rasterised to a P with a smudge beside it. Medium gives up the
  Self/Post weight play, which needs more pixels than this variant exists
  to work in, in exchange for both letters being there. The variants big
  enough to carry the contrast keep it. `favicon.png` is regenerated to
  match, and the outlines are IBM Plex Sans as before — the same
  font-size, letter-spacing and baseline, with only the S's weight moved.
- The stamp's `SELF-HOSTED SMTP RELAY` line is set at 11.5/0.15 instead of
  7.2/2.8, and no longer carries `opacity=".78"`. At the old size its stems
  rasterised to about half a device pixel, so more than half its ink landed
  as antialiasing — the typical pixel reached 2.1:1 against the brown rather
  than the 7.3:1 the two colours are worth, and none reached full strength.
  The line keeps its footprint and its monospaced cells: the width the
  tracking was spending went to the glyphs, whose cap height rises from 5.2
  to 8.3. The mark is used at 330px on the login and setup pages, which is
  where this was worst. `internal/web/static/logo.svg` is a copy of
  `docs/assets/selfpost-stamp.svg` and both carry the change, as does the
  proof sheet the outlines are drawn from.
- The delivery log lists what identifies a message and nothing else: time,
  sender, recipient, subject, status. Domain and application, which were a
  column each, remain the log's two filters and now appear per message on the
  delivery page. The two dropped columns were the widest thing in the table
  after the addresses, and both repeat down the page whenever a filter is set.
- Subjects are decoded for display as well as on the way in, so the rows the
  journal-milter recorded before it decoded them itself — the ones an operator
  is most likely to still be reading — show their text rather than
  `=?utf-8?Q?…?=`. The decoder moved to `internal/mailhdr` and is shared by the
  milter and the panel; it is idempotent, so a row decoded once passes through
  unchanged.
- The panel's navigation is a column down the left edge instead of a bar across
  the top. As a bar it did not fit on one row — six page entries and the
  session block against the panel's width — and had to be split into two,
  costing the top of every page; standing it up removes the compromise, gives
  the entries one left edge to scan down, and leaves room under them for the
  section index above. It is sticky, so both lists stay in view on the long
  pages, and the current entry is marked down its leading edge rather than
  underlined. Below the width the two columns need, it lies back down into the
  wrapping rows it used to be — no drawer and no hamburger, since six entries
  fit. The markup now lists the blocks in the order they are drawn, so the tab
  order follows the eye instead of starting at Sign out.
- An application's mode and rate-limit fields open under its row of controls
  instead of inside it. Both panels were `<details>`, so each opened where its
  own toggle sat and cut the row of four in half, pushing New password and
  Delete below a block of fields — the buttons moved every time a panel was
  opened or closed. The toggle is now a hidden checkbox with its label drawn as
  the button, and the panel is the last child of the row, so the four controls
  keep their places and what a panel reveals is laid out beneath all of them.
  It stays keyboard-reachable and, being pure CSS, still works with JavaScript
  blocked, as the disclosure did. Inside a panel the submit buttons take the
  ordinary form spacing back from the compact row style that was leaving them
  flush against the field above, and Save limit and Remove limit — two posts,
  hence two forms — share one row, the first button bound to its form by the
  `form` attribute rather than by sitting inside it.
- The mark at the head of the navigation column takes the column's full width
  instead of the 110px it kept from the bar. In a row that size was all there
  was room for; in a column it left the mark ending halfway across, with no
  edge shared with anything below it. At the column's width its edges line up
  with the page entries under it, as the full mark already does with the card
  beneath it on the signed-out pages. Where the column lies back down into a
  bar it returns to the compact size, which is what fits beside the entries.
- The import card reads the file's extension instead of asking whether the file
  is encrypted. The checkbox was a question the server never consulted — it
  decides from the envelope's magic bytes — so the answer could only be wrong.
  Choosing a `.spde` file reveals the password field and a `.json` file hides
  it; an unrecognised extension reveals it, and with no file chosen the field
  stays hidden, since there is nothing yet for a password to open. With
  JavaScript blocked the field is shown, so an encrypted import still works.

## [0.5.0] - 2026-08-06

### Fixed

- A bounce could be recorded as a successful delivery. The log-tailer's
  delivery-line pattern matched `status=` greedily, so it took the *last*
  occurrence on the line — and Postfix appends the remote server's reply
  verbatim, which the far end controls. A rejection whose reply text contained
  `status=sent` was filed as `sent` in the send log. The pattern now takes the
  first `status=` after the recipient, which is the real field
  (`internal/logtail/logtail.go`); found while extending `TestParseDelivery`.
- Log-tailer resumes where it stopped instead of jumping to end-of-file on
  every start (phase 3, `docs/code-review.md`): the read position and a
  fingerprint of the log's head are persisted (`logtail_state`, migration
  `0003`), so delivery lines written while the panel was down are parsed and
  their send-log rows no longer stay `queued` forever. A log that changed
  identity while the panel was down is read from the start; a first-ever start,
  with nothing stored, still begins at the end. Container recreate remains a
  gap — `mail.log` is not in `/data` (`docs/security.md`).
- Level-2 rate limit no longer overshoots under concurrency: messages that
  passed the check at MAIL FROM but have not reached the send log yet are
  counted alongside the stored rows (`internal/milter/inflight.go`), so
  parallel SMTP sessions cannot each spend the same last slot. Slots are
  released at end-of-message, on ABORT, and after a 10-minute TTL, so a client
  that drops mid-transaction cannot hold one — the limiter stays fail-open.

### Changed

- The project has a single public home: `github.com/mixeme/selfpost`. Codeberg
  is being retired, so the Go module path moved with it — `go.mod`,
  `test/e2e/go.mod`, every import, the `Makefile` `MODULE` variable and the
  `-ldflags` version stamp in `build/Dockerfile` and `docs/development.md`. An
  import path pointing at a host that is going away would break `go get` and
  `go install` outright, which is why this is not only a documentation change.
  README no longer lists a primary/mirror pair.
- Code comments no longer cite the archived specification. References like
  "spec 7.6.1" or "spec 5.1" pointed into `docs/archive/specification-v1.0.md`,
  which is explicitly not a source of truth; each is now a reference to the
  live document that owns the subject — `docs/architecture.md` (with section),
  `docs/product.md`, `docs/security.md`, or the README. Comments only; no
  behaviour is affected.
- `docs/code-review.md` is gone. Its plan is finished — phases 0 (bar the
  release-commit steps), 1, 1.5, 2 and 3 are all closed — and the rest of the
  document had become a second copy of what `architecture.md`, `security.md`
  and the code comments already say. What was genuinely open moved to
  `docs/roadmap.md`: splitting `internal/web` into subpackages, a consolidated
  documentation index in the README, the adaptive polling interval for an idle
  but visible tab, and `CONTRIBUTING.md`. The review text stays in git history
  (`522425a`); the CHANGELOG entries below that cite it are left as written.
- `docs/architecture.md` gained a *Code layers* section: a diagram of
  handlers → services → store plus the adapters, and the reason the services
  layer exists (multi-store writes and their rollback) — closing item A2 of
  `docs/code-review.md`.
- Phase 1 doc/code hygiene (`docs/code-review.md`): removed ~30 stale
  "Phase N" / historical-staging references from code and shell-script
  comments (`cmd/panel`, `internal/*`, `build/*`) now that v1.0 is done;
  fixed a stale dashboard comment (`internal/web/handlers_domains.go`)
  claiming applications/send-log were unimplemented; added a CSRF ADR to
  `docs/security.md` (why Origin-check, not tokens); resolved `docs/logo` in
  `docs/roadmap.md` (directory doesn't exist, criterion already met); added a
  `gofmt -l` check to CI (`.github/workflows/test.yml`).
- Phase 2 GUI polish (`docs/code-review.md`): the monitoring pages stop
  polling while their tab is hidden — the skip is done in an
  `htmx:beforeRequest` listener (`internal/web/static/panel.js`) rather than
  htmx's own trigger filter, which is evaluated with `new Function` and would
  be blocked by the panel's CSP. Dark mode is now a single reassignment of CSS
  custom properties under `prefers-color-scheme: dark` instead of a cascade of
  `!important` overrides, and the duplicate `main { max-width }` rule is
  consolidated into one base rule with documented per-page overrides
  (`internal/web/static/panel.css`).

### Security

- Pre-release security review (plan § D, model Fable, 2026-08-06): full pass
  over the diff from the v1.0 audit (Phase 11, `bd64e80`) to HEAD plus the
  complete spec 7.6 checklist. No exploitable findings; one defence-in-depth
  fix below. Accepted risks in `docs/security.md` unchanged.
- `saslpasswd2` argv: the application login is now passed after a `--`
  end-of-options marker (`internal/app/sasl.go`), so a login starting with
  `-` (legal under the whitelist) can never be parsed as a flag by getopt.

### Added

- Optional password encryption for the two secret-bearing downloads (plan
  phase 1.5, `docs/code-review.md`): an *Encrypt with a password* checkbox on
  the full-backup and domain-export forms writes a `.spbk` / `.spde` envelope
  instead of the plain `.tar.gz` / `.json` — scrypt key derivation and
  AES-256-GCM over 64 KiB chunks, each authenticated with the header, its
  counter and an end-of-stream flag, so a truncated or altered file refuses to
  open (`internal/secretfile`). Unticked, both downloads are byte-for-byte what
  they were.
- Domain import accepts an encrypted export: the envelope is detected by its
  magic bytes, and a password field appears next to the file picker
  (`internal/web/handlers_backup.go`, `templates/encrypt_fields.html`).
- `selfpost-backup` writes encrypted archives and reads them back:
  `-decrypt` (with `-i`/`-o`) turns a `.spbk` into the plain `.tar.gz` a
  restore unpacks. The password comes from `SELFPOST_BACKUP_PASSWORD` or
  `-password-file`, never from argv.
- `TestParseDelivery` covers the exotic mail.log shapes the review asked for
  (`docs/code-review.md` § 3): a `status=` quoted inside the remote reply, the
  null recipient of a double bounce, `orig_to=` alongside `to=`, an
  unrecognised status word, a capitalised one, and a cleanup line.
- docs: README *Encrypting a backup or export*; `docs/security.md` §
  *Backup and domain export* + accepted risk (encryption is opt-in);
  `docs/architecture.md` persistence § envelope summary.
- docs: `docs/roadmap.md` v1.x tail — retire `implementation-plan.md` in the
  release commit (move to `docs/archive/`, retarget its references in README,
  docs, Makefile, release workflow and the e2e test comment).
- docs: `docs/code-review.md` — phase 1.5 plan for optional password encryption
  of full backup (`.spbk`) and domain export (`.spde`); checkbox UI pattern;
  remove session-resurrection-from-backup as accepted risk.
- docs: `docs/code-review.md` — full codebase review (architecture, code quality,
  documentation, GUI, legacy, risks) with prioritized implementation plan and
  model routing; cross-links in `implementation-plan.md` and `progress.md`.
- docs (D6): Docker `HEALTHCHECK` probes `/healthz`; endpoint returns 503 unless
  opendkim, panel, and postfix are RUNNING (`internal/health.Liveness`).
- docs (D6): README *Container health* — scope of `/healthz` vs authenticated Status.
- docs (D7): `cmd/panel/envdoc_test.go` — regression test that every
  `loadConfig` and build-script env key is listed in README documentation.
- docs (D8): `docs/architecture.md` — as-built processes, mail path, routes,
  persistence (verified against code).
- docs (D8): `docs/development.md` — local Go workflow, `make e2e`, dev-server
  loop, commit/CHANGELOG protocol, agent rules.
- docs (D9): `docs/product.md` — product purpose, assumptions, out-of-scope,
  multi-domain model.
- docs (D9): `docs/security.md` — self-contained mandatory security checklist
  (former spec §7.6).
- docs (D9): `specification.md` archived to
  `docs/archive/specification-v1.0.md`; live docs updated (`progress.md`,
  `implementation-plan.md`, `roadmap.md`, `documentation-plan.md`).
- docs (D1): README *Operations* — panel screens (`/status`, domains,
  deliveries, mail queue, system log, backup, account), upgrade procedure,
  session behaviour (sliding idle, monitoring polls do not extend, password
  change signs out other sessions), and `mail.log` rotation cadence.
- docs (D1): README *Rate limiting* — level-1 anvil limits
  (`RATE_LIMIT_MESSAGES_PER_IP`, `RATE_LIMIT_WINDOW_SECONDS`) and level-2
  per-domain/application limits from the panel; fixes the `.env.example` link
  that pointed at a missing section.
- docs (D2): README environment-variable reference — public `.env` table with
  code-accurate defaults, `TRUSTED_PROXY_CIDR` security note, explicit
  internal-variable list; `TRUSTED_PROXY_CIDR` wired through
  `deploy/docker-compose.yml`.

### Removed

- docs: `security.md` — accepted risk «restore old backup revives session rows»
  (not a concern in operator deployment).

### Changed

- docs: `implementation-plan.md` trimmed to the sole open v1.x gate — pre-release
  security review (§ D); closed B.1–C.4 material moved to as-built and ops docs.
- docs: `architecture.md` — sessions (SQLite, idle renew, password change),
  `mail.log` rotation (rename + `postfix reload`), `SELFPOST_HOSTNAME` startup
  gate, log-tailer known gaps.
- docs: `development.md` — expanded e2e stack and `release.yml` matrix workflow.
- docs: `security.md` — accepted risk for send-log rows stuck at `queued` after
  panel restart or container recreate.
- docs: `roadmap.md` — optional send-log / `mail.log` follow-ups under v1.x tail.
- docs: `progress.md`, `documentation-plan.md` — cross-links updated for the new layout.
- docs: `documentation-plan.md` marked closed (D1–D9); trimmed to package
  checklist, code-verification method, and ongoing maintenance rules.
- docs: `roadmap.md` — v1.x doc/deploy tail (Codeberg Quick start, compose
  image tag at release, `docs/logo`); archived-spec references replaced with
  `product.md` / `security.md` / `development.md`.
- docs: `progress.md` — documentation pass closed; deferred polish in roadmap.
- `/healthz` now checks supervisord mail-path processes, not HTTP alone.
- `build/Dockerfile`: `curl` for `HEALTHCHECK`; probe on port 8080.
- docs (D3): README backup — stopped-container `tar` of `./data` (with live-container
  WAL warning), `manifest.json` consumed after a matching restore.
- docs (D4): README status banner (v1.0 implemented, links to open questions and
  documentation pass); new *Published ports* note for 587; compose usage comment
  corrected (TLS via `./certs` bind mount, not `.env`).
- docs (D5): `implementation-plan.md` B.1 — password change signs out other
  sessions only (implementation diverged from original plan; README was already
  correct).
- docs: documentation plan now targets retiring `specification.md` after D9 —
  migration map to `product.md`, `architecture.md`, `development.md`, and
  expanded `security.md`; D9 added to the release gate.

## [0.4.0] - 2026-08-04

### Added

- The project's mark is now in use rather than only on file. The README opens
  with the full stamp; the panel carries the compact one at the left of its
  navigation bar, linking to the status page, and the full one above the card
  on the two pages that have no navigation — sign-in and first-run setup. The
  browser tab icon changes with it, from the earlier envelope drawing to the
  stamp's small-size variant, so the tab, the panel and the README are one
  identity. The four brand files in `docs/assets/` had their wordmark converted
  from live text to outlines: they were set in IBM Plex Sans, which is not
  installed on the machines that render them, and the light/semibold contrast
  between *Self* and *Post* — the whole of the mark — collapsed into whatever
  fallback the viewer happened to have.

### Changed

- panel: the sign-in and setup pages are now a column the width of their own
  card. Both are a single narrow card, which centred itself while the heading
  above it stayed at the panel's left edge; adding the mark would have made
  that three alignments on a page with four elements.

- panel: the three monitoring pages — Deliveries, Mail queue, System log — are
  now laid out wider (64rem against the 48rem the rest of the panel keeps).
  They carry data rather than prose: the send-log's seven columns had no room
  to breathe, and the raw `mail.log` lines wrapped every second line.

### Fixed

- panel: Deliveries now shows the subject as text rather than as its MIME
  encoding. A subject in any non-Latin alphabet reaches the milter as RFC 2047
  encoded-words (`=?utf-8?Q?=D0=9F…?=`), and the panel printed that verbatim —
  unreadable, and as one unbreakable run wide enough to push the Status column
  outside the card. Subjects are decoded when the message is journalled and
  capped at 200 characters; the column clips anything still too long to one
  line, with the full text in the tooltip. Rows logged before this release keep
  their raw string. Subjects in the legacy single-byte charsets (windows-1251,
  koi8-r) are still stored as sent — there is no decoder for them.

- panel: table cells may now break inside a word, so no single long value can
  push a table past the edge of its card. A 40-character recipient address did
  it just as readily as an undecoded subject: a column is at least as wide as
  the longest unbreakable run it holds, and email addresses have nothing to
  break on. Timestamps are exempt and stay on one line.

- panel: the Applications list on a domain page no longer comes apart. It was a
  four-column table whose last column held six controls, two of them expanding
  panels with textareas — far more than the width of a column, so the controls
  broke into a staircase, the login cell grew into a block as tall as the row,
  and the two text columns were left stranded on the baseline halfway down it.
  An application is now a block rather than a row: the login on one line, mode
  and addresses on the next, and the controls in a single wrapping row, with an
  opened panel claiming the full width for its fields.

## [0.3.0] - 2026-08-03

### Fixed

- panel: the PTR (reverse DNS) check no longer reports a correctly published
  record as wrong. The checks went through the container's own resolver, which
  forwards to the host's systemd-resolved — and systemd-resolved answers the
  reverse lookup of the machine's own IP from the local hostname instead of
  asking public DNS. A server with `203.0.113.10 → selfpost.example.com` in DNS
  was told its PTR pointed at the provider-assigned hostname. All four
  deliverability checks (PTR, SPF, DKIM, DMARC) now query recursive resolvers
  directly, so the panel reports what a receiving mail server actually sees.
  Set `SELFPOST_DNS_RESOLVERS` if outbound port 53 is closed or you run your
  own recursor; it defaults to 1.1.1.1, 8.8.8.8 and 9.9.9.9.

### Changed

- panel: the three monitoring pages now live at URLs that match their nav
  labels — Deliveries at `/deliveries` (was `/sendlog`), Mail queue at
  `/mail-queue` (was `/queue`), System log at `/system-log` (was `/logtail`).
  Bookmarks to the old paths stop working.

- panel: each entry in the navigation bar now carries an icon beside its label,
  so the bar is scannable at a glance instead of a row of similar-length words —
  Backup uses a filing-cabinet mark; Sign out has its own icon too. The icons
  are inline SVG drawn in the entry's own colour — no extra request, no
  exemption from the panel's Content-Security-Policy — and are hidden from
  screen readers, which still announce the label alone.

- panel: the navigation bar is laid out as two rows on purpose — the signed-in
  user, Account and Sign out along the top right, the page entries below. It no
  longer fits on one line and used to wrap on its own, which left the session
  block sitting left-aligned under the entries as if it were more navigation.

## [0.2.0] - 2026-08-03

- panel: every authenticated page now ends with the running version
  (`SelfPost 0.2.0`) in a small footer. It is the value a backup manifest is
  checked against on restore, and the first thing to establish when the panel
  behaves unexpectedly. The login and setup pages deliberately do not show it.

- panel: the domain page now shows the **SPF and DMARC records it expects**,
  with host, value and a Copy button, next to the DKIM record it already
  showed — previously it only said "also configure SPF and DMARC (see the
  documentation)" and the concrete example appeared only once a check had
  already failed. The SPF value names the addresses this server's hostname
  resolves to (falling back to an `a:` mechanism if it does not resolve), and
  the DNS checks below build their remediation advice from the same source, so
  the page and its checks cannot recommend different records.
- panel: one appearance for actions. Several controls — a POST wrapped in an
  inline form (Re-check, Export domain, Sign out, New password…), the
  `<details>` toggles in the applications table, the delete links — used to
  render as bold blue text while everything else was a button, so the same
  kind of control looked like two different things, sometimes within one card.
  They are all buttons now: filled for a card's own action, compact and
  outlined where actions cluster in a table row or the nav bar. The two
  actions that are really navigations — "Delete domain" and the status page's
  "Full queue" — are anchors carrying the same button styling. A bare link is
  left only where it reads as part of a sentence, a table cell or the nav.
- panel: on the domain page **Add an application** now sits directly above the
  **Applications** list — the same order the domains page uses for its own add
  form — instead of being stranded below the domain rate limit.
- ci: hermetic container e2e suite (`test/e2e`, a separate Go module) gates
  image publishing — `make e2e` locally, and `go test ./...` in `test/e2e` as
  a required step in `release.yml` before a version tag's image is pushed.
  It builds the real image, brings up the shipped `deploy/docker-compose.yml`
  plus a test-only override (self-signed cert, low ports, a fake DNS zone
  served by CoreDNS, a `smtp-sink` sink-MX) on an isolated compose project,
  then drives the panel over HTTP exactly like an administrator: setup →
  login → add a domain → publish the DKIM record it prints into the fake zone
  → add an application → send over SMTP AUTH → verify the delivered message's
  DKIM signature against the record the panel published → poll the send log
  to `sent`. Negative coverage: no-AUTH and unauthenticated-relay rejection,
  sender/login mismatch, the level-1 (anvil) and level-2 (panel-configured)
  rate limits, the journal-milter's fail-open behaviour when the panel process
  is stopped, a missing/malformed `SELFPOST_HOSTNAME` failing the container
  fast, and a login session surviving `docker restart`. `release.yml` moved
  off qemu to a native per-architecture build (`ubuntu-latest` /
  `ubuntu-24.04-arm`), each gated by this suite before its tag is pushed and
  merged into the version manifest — running the full Postfix/OpenDKIM stack
  under emulation for the gate was impractically slow.
- ops: `mail.log` rotation switched from `copytruncate` to rename +
  `postfix reload` (the same mechanism `postfix logrotate` itself uses),
  eliminating the up-to-one-second window in which `copytruncate` could drop
  in-flight delivery lines — a lost line meant a send-log row stuck at
  `queued` forever. `logrotate-mail.conf` keeps `create 0644 root root`
  rather than `nocreate`: verified on a live container that letting Postfix
  recreate the file itself on reload produces `0600`, which the unprivileged
  panel process cannot read, breaking the mail-log view until the next
  restart. The panel's log-tailer (`internal/logtail`) re-drains the old file
  descriptor once more right before switching to the rotated one, closing a
  similar small window between polls; a missing `mail.log` right after
  rotation is now a normal empty screen rather than a logged error.
- panel: login sessions now persist in SQLite instead of memory, so an
  administrator's login survives a container restart or redeploy. Only the
  SHA-256 of the session token is stored, never the token itself. The
  absolute 12-hour TTL is replaced by a **sliding idle timeout**
  (`PANEL_SESSION_IDLE_DAYS`, default 7 days, no absolute cap): the
  monitoring screens' background polling does not count as activity, so a
  forgotten open tab does not keep a session alive forever. Changing the
  password still signs out every other session.
- panel: security headers on every response — `Content-Security-Policy`,
  `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, and
  `Strict-Transport-Security` where the deployment is HTTPS-only. They are
  emitted by the panel itself, so the reverse proxy still needs no security
  configuration of its own.
- panel: state-changing requests are now checked against the panel's own
  origin (`Sec-Fetch-Site`, falling back to `Origin` vs `Host`). This closes
  cross-site request forgery from a *neighbouring host on the same domain* —
  a CMS or a forgotten staging subdomain next to the panel — which the session
  cookie's `SameSite=Lax` counts as same-site and therefore cannot stop. A
  request that sends neither header is still let through, so genuinely ancient
  browsers keep working. **The reverse proxy must pass the original `Host`
  header through** (every shipped fragment already does); one that rewrites it
  makes the panel refuse every form submission, and the log line names both
  the `Origin` and the `Host` it compared.
- panel: the session cookie is now named `__Host-selfpost_session` wherever it
  is `Secure` (the standard deployment), which makes the browser enforce that
  no other host can set or overwrite it. **Upgrading signs the administrator
  out once.** With `PANEL_COOKIE_SECURE=false` the old name is kept, because
  the prefix is invalid without TLS. Signing out clears both names.
- panel: if a request arrives with two cookies of the session cookie's name —
  what a neighbouring host does when it overwrites the session — the request
  counts as signed out and the log says so, instead of the panel silently
  picking the other host's value and looping back to the login form forever.
- panel: the layout's stylesheet moved to `/static/panel.css` and the
  confirmation prompts on destructive buttons moved into `/static/panel.js`.
  No visible change; the panel's CSP allows no inline script or style, and
  this is what keeps that policy free of exemptions.
- docs: the first-run setup link is also written to `/data/setup-token`
  (`0600`) — documented in the README as the way to read it without the token
  passing through a container-log pipeline.
- panel: new **Status** page — supervised processes, mail queue, TLS
  certificate expiry, milter sockets and the server's own hostname/reverse-DNS
  (FCrDNS) check — and it is now the panel's landing page. The local checks
  refresh by polling; the DNS lookup is cached with a *Re-check* button.
- panel: the domain page shows a **DNS status** card: the published DKIM record
  compared against the key this server actually signs with, plus SPF and DMARC.
  The SPF check is deliberately shallow — it looks for a mechanism literally
  covering this server's address and does not follow `include:`/`redirect=`, so
  a record that authorises the server through an include is reported as "cannot
  tell", not as a failure.
- panel: the domain list moved from `/` to `/domains`; `/` redirects to the
  status page. The **Reload** button moved from the domain list to the status
  page and now explains what it regenerates and when to use it.
- fix: the panel could never read the mail queue in the documented deployment.
  `postqueue` relies on its setgid-`postdrop` bit, which `no-new-privileges`
  (set in the shipped compose file) disables, so the *Queue* screen always said
  "Could not read the mail queue". The `panel` user is now a real member of
  `postdrop`.
- panel: navigation bar is now rendered once from the shared layout, so every
  authenticated page has it — including the domain page and the delete
  confirmation, which had no navigation links at all — and the current page is
  highlighted instead of silently missing from the list.
- panel: new *Account* page to change the administrator's username and/or
  password (the current password is required, throttled on the same limiter as
  the login form). Changing the password invalidates all other sessions.
- panel: *Backup & migration* moved off the domain list onto its own *Backup*
  page, with the full backup and the domain import as two separate cards.
- panel: the domain page now shows the *Sending server settings* (server,
  port and encryption) needed to configure a mail client; port 587 is listed
  only when `SUBMISSION_ENABLE=true` for this deployment.
- panel: *Copy* buttons on the DKIM record, on a newly issued application
  login/password and on the sending server name.
- panel: the *Addresses* field is hidden while an application's address mode is
  *Any address of the domain*, where the server ignores it.
- deploy: `cap_add` gains `CAP_FOWNER` and `CAP_FSETID` so the entrypoint can
  chmod and set the setgid bit on `/data` directories it has just chowned under
  `cap_drop: ALL`, and `CAP_KILL` so supervisord can signal OpenDKIM across uids
  when domains are added or removed. Production boot under the hardened compose
  file exposed both gaps after Phase 10.
- ci: disable provenance attestation on release image push, so the ghcr.io
  manifest list shows only `linux/amd64`/`linux/arm64` (no `unknown/unknown`).
- ci: run `go vet` and `go test ./...` on every push to `main` and every pull
  request, not only the image build on a release tag.
- security: optionally honour `X-Forwarded-For` for login/setup rate-limiting
  when the request's direct peer is in the new `TRUSTED_PROXY_CIDR` list,
  giving real per-client limits behind a reverse proxy instead of one global
  bucket. Unset by default (unchanged `RemoteAddr`-only behaviour).

## [0.1.0] - 2026-07-15

Initial feature-complete implementation of the v1.0 specification (phases 0-11
of `docs/implementation-plan.md`).

### Added

- Panel (Go, single static binary) with SQLite persistence, one-time
  crypto-random setup link, bcrypt admin auth, session cookies.
- Domain management with per-domain DKIM (RSA-2048, generated in pure Go) and
  OpenDKIM KeyTable/SigningTable regeneration + privilege-safe reload.
- Application (sender identity) management: SASL credentials via `sasldb2`,
  `smtpd_sender_login_maps` enforcing sender/domain ownership, no open relay.
- Full Postfix relay config generated from env at container start: SMTPS 465,
  optional STARTTLS submission 587, SASL auth, TLS for outbound delivery,
  anvil-based rate limiting (level 1).
- Journal milter (pure Go, `go-milter`) recording every send to `send_log`;
  fail-open by design so a milter fault never blocks mail.
- Monitoring UI: send log, Postfix queue, and mail.log tail, all
  HTMX-polling, HTML-escaped.
- Per-domain/per-application sending rate limit (level 2), enforced in the
  journal milter at `MAIL FROM`, fail-open on the limiter's own errors.
- Full backup/restore (`tar.gz` of `/data`, consistent SQLite snapshot via
  `VACUUM INTO`) with a version guard that refuses to start on a
  manifest/binary version mismatch. Per-domain export/import for moving a
  single domain between hosts without re-issuing DNS records.
- Deployment: Docker image + compose, reverse-proxy fragments for Apache
  (default), nginx, Caddy, and Traefik; CI workflow publishing tagged,
  multi-arch images to `ghcr.io` on `vX.Y.Z` tags.
- Security pass against spec 7.6 (exec safety, config-write sanitization,
  server-side validation, rate limiting, session/cookie hardening, output
  escaping, non-root panel) — full compliance, no code changes required.
- Live production deployment with a real Let's Encrypt certificate;
  end-to-end delivery confirmed (DKIM pass, SPF pass).
