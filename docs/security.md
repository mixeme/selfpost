# Security

**What is here.** (1) **Mandatory requirements** — the checklist v1.0 has to
meet; the full v1.0 audit passed. The pre-release review (plan § D, 2026-08-06)
covered the whole diff from the v1.0 audit (Phase 11) to HEAD and the checklist
in full: no exploitable findings; one defence-in-depth change — `--` before the
login in the `saslpasswd2` argv
([internal/app/sasl.go](../internal/app/sasl.go)). The 2026-08-14 review
(code-review plan § P7, Fable; reviewer ≠ author) covered the P0–P1 diff of the
2026-08-13 full-tree review against this document — send-log authorization for
domain administrators, the atomic level-2 admit (`tryAdmit`), fail-closed
session creation, and application-delete ordering: no findings, and nothing
needed adding to the accepted risks. The 2026-08-19 review (Fable) covered the
whole diff from 1.3.0 (`7052395`) to HEAD past 1.9.1 — the inbound-relay path
(port 25, maps, panel), DMARC aggregate-report ingestion, automatic rate
limiting, application auth-IP allow-lists, and the new panel surface
(handlers, templates, settings): no exploitable findings; two deliberate
behaviours the pass examined are now recorded under Accepted risks
(unauthenticated DMARC ingest, fail-open auth-IP enforcement). The 2.0 review
(Fable, 2026-10-10; fixed and re-checked by other models) covered the route
rename, the `/server/` subtree guard, inbound delegation and the new pages: no
Critical or High finding; sessions were keyed by user name (now by user id),
a password change ended other users' sessions, a password set for a user on
the user form did not end that user's sessions, internal errors reached forms a
`domain` user can open, and `/static/` listed its files — all fixed
(see [Authentication and sessions](#authentication-and-sessions) and [Errors
shown to users](#errors-shown-to-users)); two deliberate behaviours are
recorded under Accepted risks (upstream of a delegated inbound domain, profile
saved without the password). The 2026-10-11 review (Fable) covered the DMARC
report path from port 25 to the report pages, which ran for the first time in
2.0.0 - the 2026-08-19 pass read code that Postfix could not yet start: no
Critical or High finding; a report's size was bounded only as bytes (now 5 000
records, 255 bytes a field), the retention cap was one for the whole server
(now per domain), the delivery command could run a hundred at once (now one),
and the reason logged for a dropped message carried the sender's bytes as they
came (now cut and reduced to printable ASCII). The 2026-10-12 reviews of
inbound-antispam-panel (`2.1.0`: the inbound journal milter, refusals read from
`mail.log`, the filter lists and their pages) were two, by different models
over the same diff. The technical one (Opus) found, outside the feature and
there since `1.9.0`, that **the level-2 rate limit and an application's
client-address list held only for the first message of a connection** - see
[Mail path](#mail-path-security-relevant) - which was reproduced on the test
stand and fixed. The security one (Fable) found no Critical or High: a client's
HELO name could supply the sender, recipient and reply of a journal row and so
make a refusal look like the deny list's (the reply now ends at Postfix's own
`; from=<`); the journal's row cap was applied only every six hours, between
which refusals were written at whatever rate a stranger sent recipients (the
tailer now cuts to the cap every 500 rows); and two behaviours are recorded
under Accepted risks (the allow list keyed on an address anyone can claim, the
list files readable by anyone). A third it named - with the filter down, a
sender's own verdict headers were recorded as the filter's - was closed the
same day at the owner's suggestion: the filter signs each message and a
verdict is read only next to its signature.
(2) **Accepted risks** —
deliberate departures beyond the mandatory, recorded so the decision is not
lost.

Hardening beyond the mandatory (security headers, origin checking, `__Host-`
cookie with duplicate detection — Phase 14) is done; the history is in
[CHANGELOG.md](../CHANGELOG.md) and `git log`.

Product boundaries: [product.md](product.md). As-built design:
[architecture.md](architecture.md).

---

## Mandatory requirements

The panel is exposed to the internet — the items below are **not optional**.

### First-run administrator setup

- A one-time secret link `/setup/<token>`, **not** an env variable holding a
  ready-made password hash.
- Token ≥128 bits (`crypto/rand`); mirrored to `/data/setup-token`.
- Token lifetime — **10 minutes**; after expiry, or after a restart with setup
  unfinished, it is regenerated and logged again.
- Rate limiting on `/setup/<token>` per IP, separate from login.
- Token comparison is **constant-time** (`subtle.ConstantTimeCompare`).
- Failed attempts do **not** invalidate the token early (protects setup from
  being DoS-ed).
- Once the administrator exists the token is void forever, `/setup/*` → 404.
- The administrator password is bcrypt in SQLite only; no plaintext and no
  MD5.
- `PANEL_USERNAME` / `PANEL_PASSWORD_HASH` in env are **not used**.

### Application SASL passwords

- The panel **generates** the password on creation or reissue and shows it
  **once**.
- In `sasldb2` it is stored in the form SASL requires (not plaintext held by the
  panel); a lost password can only be reissued.

### Input and configuration

- Server-side validation of addresses and domains (character whitelist);
  client-side validation does not count as protection.
- In address-list mode every address is checked to belong to the application's
  domain before it is written.
- `postfix reload` and any `exec` run **without** shell interpolation of user
  input; arguments are passed as separate elements.
- Writes to config files are escaped (no injection of Postfix directives).

### Authentication and sessions

- Rate limiting on login (per IP, with lockout or delay).
- Sessions: cryptographically random token; cookie `HttpOnly`, `Secure`,
  `SameSite`.
- Sessions live in SQLite (SHA-256 of the token, not the token itself); sliding
  idle timeout (`PANEL_SESSION_IDLE_DAYS`).
- **A session belongs to a user by id** (`sessions.user_id`, deleted with the
  user). The user is loaded on every request and a session is renewed only
  after it was found, so the cookie of a deleted user is dead and cannot become
  the session of whoever is created under the same name later; renaming a user
  leaves their sessions alone.
- **What ends a user's sessions:** deleting the user (all); a password change on
  Account (that user's *other* sessions — never another user's); a password set
  for them by a global user on the user form (all of theirs, except the current
  one when they set their own); signing out (the current one only). There is no
  "sign out everywhere", and a restored backup brings back the session rows it
  held.
- Polling by the monitoring screens (`HX-Request` `GET`s) does not renew a
  session.

### Roles

Everyone who signs in to the panel is an administrator, so the two roles name a
*reach*, never a rank: `global` and `domain` (in the users list, the role
select, the page headers and these documents — not "admin"). A `domain` user is
assigned outbound domains and inbound domains **separately**, each list with
*All* (which includes domains added later and widens the list, not the role).

| | `global` | `domain` |
|---|---|---|
| Overview, Server (Health, System log, Backup, Users, Settings, kit page) | yes | no — 404 |
| Outbound queue | yes | no — 404 (a queue is not filtered by domain) |
| Outbound: domain page, applications, Domain settings (report address, rate limit, export), log | every domain | assigned outbound domains |
| Outbound › DMARC reports | every report, with the server's ingest statistics | reports of assigned domains, without the ingest box |
| Inbound: domain page, upstream, recipients | every inbound domain | assigned inbound domains |
| Inbound › Log | every row, including those of a domain deleted since | rows of assigned inbound domains (by id: not those of an earlier domain of the same name), without the links into the filter lists |
| Inbound › Filter lists; the inbound log's retention | yes | no — 404 |
| Add or delete a domain, either direction; import a domain | yes | no |
| Account, Help | yes | yes |

- A path a user may not reach, a domain or application that does not exist,
  and one that belongs to another tenant answer the **same 404**, so the panel
  does not confirm what exists. A group with nothing assigned is absent from
  the user's menu.
- `/server/` is guarded once, by `globalOnly` where its routes are registered
  (a route outside the subtree cannot be added through that helper), and every
  handler checks again. Outbound and inbound handlers check per object
  (`lookupDomain`, `requireInboundDomain`) and lists are filtered by assignment
  in the query. Each denied path has a test for a `domain` user, including
  another tenant's id.
- The panel will not remove or demote the last `global` user. The guard is
  inside the transaction that deletes or demotes: the write comes first and
  the count after it, so two administrators deleting or demoting each other
  at the same moment cannot both succeed; first-user setup is one statement
  that inserts only into an empty table. A user's name, password, e-mail,
  role and assignments are saved in one transaction, so a failure part way
  leaves no domain user with nothing assigned.
- A log row - outbound or inbound - belongs to a domain by its id, written
  when the row is: the outbound row's from the application that sent it. A
  domain deleted and added again starts with an empty log for whoever it is
  assigned to; the rows of the deleted one are the global role's alone.
- A `domain` user can export an assigned domain, and an export carries the
  working application passwords (encrypted unless the box is unticked): assign
  domains with that in mind.

### State-changing requests

- Every state change is a `POST`; a `GET` never changes state (the delete
  confirmation pages only render a form).
- Every non-read request first passes the origin check
  (`originAllowed`, [internal/web/security.go](../internal/web/security.go)):
  `Sec-Fetch-Site` must be `same-origin`, or `Origin` must name the panel's own
  host; the session cookie is `SameSite` as well.
- There are **no CSRF tokens**. The decision, its price and the conditions
  for revisiting it are in the accepted risks and the ADR below.

### Errors shown to users

A page shows a validation message (what to fix) and nothing else of an error. A
database, `saslpasswd2` or Postfix-reload failure is logged and the page says
only that the change could not be saved and to check the logs. This holds on
every form a `domain` user can open. The global-only Backup page (full backup,
domain import) still shows the underlying message of an import failure, so the
operator can act on it.

### Output and process

- Rendering goes through `html/template` with auto-escaping (queue, log,
  journal, themes).
- The panel process is **not root** (`user=panel` in supervisord); path access
  is granted through the `selfpost` group with minimal permissions.

### Mail path (security-relevant)

- **Not an open relay** — SASL only on 465/587; `reject_unauth_destination`;
  `smtpd_sender_login_maps` + `reject_sender_login_mismatch`.
- **An application signs only as itself (`2.4.0`).** OpenDKIM chooses the
  key by the `From:` header and Postfix holds only the envelope sender
  against the login, so until `2.4.0` an application of one sending domain
  could write another registered domain - or, since `2.3.0`, the server's
  own name - into `From:` and have the message signed with that name's key
  (found by the security review of panel-notifications, 2026-10-12). The
  panel's sender check now holds the header to the table the envelope is
  held to and refuses anything else with `550 5.7.1`, a domain the server
  does not sign for included (the owner's rule: the header may say what the
  envelope may, nothing more). It is the first milter of the chain and
  strict: a message it could not judge is deferred, never passed on to the
  signer. It reads the header as one field holding one address and then
  looks at the raw value once more for any other name of OpenDKIM's
  `SigningTable`, so that a line its parser and OpenDKIM's could read
  differently is refused rather than argued about; outside quoted strings
  the line may hold one `@`. That the two agree is tested, not only argued:
  the e2e sends every `From:` line the check passes - a list it shares with
  the check's unit test, `internal/milter/testdata/from-lines-passed.txt` -
  and wants the message signed for the application's domain and no other;
  and every line of a second list, lines that name another sender of the
  server in a quoted string, a comment, a quoted pair, capitals or an encoded
  word (`from-lines-other.txt`), must be refused and never arrive signed for
  that sender. The reply to a refusal says that the line names another
  sender of the server, not which. That a line naming a domain is refused
  when the domain is a sender of this server, and passed when it is not,
  still tells an application which domains are hosted here; the probe needs
  an application of this server, and refusing every foreign address in a
  display name would change the 2.4.0 rule - the owner decides (accepted
  risks). What this does not promise is what a display
  name looks like to a person: a name of another sender written without an
  `@` the scan can find (a full-width dot, a space inside the name) passes,
  signed with the application's own key - a forged display name is as old
  as mail, and no key of another's is involved. What the client wrote
  reaches the reply, the log and the send log only as printable ASCII, cut,
  and a refused message is one row of the log whatever its recipient count.
  Mail the panel submits itself does not pass the check: only root,
  `postfix` and the panel can submit locally. Architecture § Sender check.
- **Inbound relay (optional, `INBOUND_RELAY_ENABLE`)** — port 25 is not an
  open relay either: SASL is off; `smtpd_relay_restrictions` /
  `smtpd_recipient_restrictions` are `reject_unauth_destination` and
  `reject_unlisted_recipient`; maps list only configured domains and
  recipients. Domains with no upstream host are omitted from the maps so mail
  is never accepted with nowhere to send it. Prefer recipient mode `list` to
  refuse unknown addresses at RCPT (no backscatter). OpenDKIM is not attached
  on inbound. An optional antispam milter is inbound-only; default action is
  fail-open (`accept`).
- **The panel's milters judge every message of a connection.** Postfix
  introduces a client to a milter once per connection, and the milter library
  replaces the milter's session after any reply that is not "continue". Until
  `2.1.0` the journal milter knew the client's address only from that
  introduction, and both of its checks passed a message that came without one:
  a second `MAIL FROM` after a refused one, or any message after the first on
  one connection, got past the level-2 limit and past an application's
  client-address list. Now the address comes with every `MAIL FROM`
  (`milter_mail_macros` carries `{client_addr}`), the level-2 limit does not
  ask for it at all, an application with a client-address list is refused when
  no address is known, and neither milter replies anything but "continue" to a
  message it lets through. `test/e2e` sends two `MAIL FROM` on one connection.
- **The inbound journal is written from what strangers send.** The inbound
  journal milter is the last on port 25, read-only and attached fail-open; it
  keeps of the filter's verdict only what its patterns recognise. **A verdict
  is read only next to the filter's signature.** The filter is attached so
  that mail flows when it is down, and then nothing removes verdict headers a
  sender wrote himself. So rspamd adds a third header, `X-SelfPost-Filter`:
  the HMAC-SHA256 of the message's queue id under a key SelfPost generates
  (`data/antispam/filter.key`) and the sidecar reads from the directory it
  mounts. The journal milter recomputes it at end-of-message; with no
  signature, or one that does not fit, the row is that of mail nobody judged.
  The signature goes on to the upstream with the mail and is of no use there:
  it fits one queue id. Refusals are
  parsed from `mail.log` from the head of the line, with the reply ending at
  Postfix's own `; from=<` so that the HELO name after it - the client's text,
  logged as written - cannot stand in for the sender, the recipient or the
  reply; an address with a quote, a bracket or a space in it is not kept.
  Only a recipient at an inbound domain makes a row. The journal is cut to
  10 000 rows every 500 rows the tailer writes, and by age every six hours.
- **Filter lists** reach another container as files: values are whitelisted on
  the way in (address, domain, canonical IP or network; no `/0`, no
  IPv4-mapped network), the writer refuses a stored value with whitespace, a
  newline or `#`, and a change of the table and the rewrite of the six files
  are one step under a lock.
- TLS is mandatory before credentials are transmitted (465 wrapper / 587
  `encrypt`).
- `TRUSTED_PROXY_CIDR` — only explicitly trusted proxies may supply
  `X-Forwarded-For` for login rate limiting; empty means XFF is ignored.

### Backup and domain export

- Both files are secrets: a full backup carries DKIM keys, `sasldb2`, the
  panel users' password hashes, `docker-compose.yml`, `.env`, and the TLS
  private key from `certs/` when present; a domain export carries the DKIM key
  and **working** application passwords in the clear (otherwise a transfer
  without recreating credentials would be impossible).
- Since 2.2.0 a full backup also carries, in the database, what Postfix logged
  about each message still in the send log (`send_log_lines`): the submitting
  client's address and SASL login, the message id, the receiving server and
  its reply. `mail.log` itself stays out of a backup; these lines are state
  the panel shows, kept for the send log's retention and no longer.
- Both downloads are encrypted with a password unless the box on the form is
  unticked (it is ticked by default):
  scrypt (N=2¹⁵, r=8, p=1) → AES-256-GCM, streamed in 64 KiB chunks, each
  authenticated with the header, the chunk number, and an end-of-stream flag —
  a truncated or substituted file fails to open instead of silently restoring a
  partial "tail". Format and wrapper:
  [internal/secretfile](../internal/secretfile/secretfile.go).
- Extensions: `.spbk` (**S**elf**P**ost **b**ac**k**up — full backup), `.spde`
  (**S**elf**P**ost **d**omain **e**xport — domain export); unencrypted files
  stay `.tar.gz` / `.json`. Domain import detects encryption by the file's magic
  bytes, not by extension.
- The password is never stored: without it the file cannot be recovered. In the
  CLI the password comes only from `SELFPOST_BACKUP_PASSWORD` or
  `-password-file`, never as an argument (the process list is readable by any
  process in the container).
- Minimum password length matches the administrator password (12): the file
  sits offline and can be attacked without a time limit.

---

## Accepted risks

An accepted risk is a decision with a condition for revisiting it, not a
deferred item from the roadmap.

- **A `POST` with neither `Sec-Fetch-Site` nor `Origin` is allowed through.**
  A client that sends neither — a genuinely old browser, or a webview with a
  frozen engine — stays vulnerable to CSRF from any site. Accepted
  deliberately: every panel user (role `global` or `domain`) is an operator who
  picks their own browser, not an untrusted party the panel needs to defend
  against, and a strict mode would not "protect" such a client, it would
  simply break the panel in it. Tightening is one line in `originAllowed`
  ([internal/web/security.go](../internal/web/security.go)): return `false`
  instead of `true` in the "neither header present" branch. That tightening
  is agreed (owner, 2026-10-13) as
  [origin-strict](roadmap.md#origin-strict); this bullet goes when it ships.
- **Session-bound CSRF tokens are not implemented.** The origin check closes the
  neighbouring-subdomain case but depends on browser behaviour; a token does
  not. The price is a hidden field in roughly two dozen forms. The trigger to
  revisit is a requirement for protection that holds regardless of the browser,
  or a population of `domain` users the global user does not fully trust
  (see the ADR below). A token would not save the panel from XSS inside it
  either: code executing in the panel's origin sends the request itself —
  against that, `html/template` auto-escaping and CSP do the work, which is why
  templates must contain no inline scripts and no inline styles. Decided by
  the owner on 2026-10-13: no tokens; the roadmap item that asked for them is
  dropped and the origin check is made strict instead
  ([origin-strict](roadmap.md#origin-strict)). This bullet and the ADR below
  are the single record of the decision, and the ADR's **Revisit if** says
  when tokens are built after all.
- **Destructive-action confirmation (`data-confirm`) is JavaScript-only.**
  Delete, regenerate-password, and clear-rate-limit forms carry a
  `data-confirm` prompt handled entirely in
  [panel.js](../internal/web/view/static/panel.js); with JavaScript disabled
  or blocked the form submits immediately, exactly as it did before the
  prompts existed. Accepted deliberately: the prompt is a mis-click guard,
  not an authorization boundary — the same origin check and session/RBAC
  gate every one of these `POST`s whether or not JavaScript ran. Progressive
  enhancement means the panel must work with JavaScript off; a
  server-rendered confirmation step would need a second page (or a `?confirm=1`
  round trip) for every one of these forms, which is what the delete pages
  ([`user_delete.html`](../internal/web/view/templates/user_delete.html),
  [`out_domain_delete.html`](../internal/web/view/templates/out_domain_delete.html),
  [`in_domain_delete.html`](../internal/web/view/templates/in_domain_delete.html))
  already do for the highest-blast-radius deletes.
- **Encrypting backups and exports can be switched off.** The panel's
  *Encrypt with a password* box is ticked by default, but with it cleared the
  file downloads in the clear, and the `selfpost-backup` CLI writes a plain
  archive unless it is given a password. Otherwise an operator with nowhere to
  keep a password would lose the ability to take a backup at all, and a
  permanently undecryptable archive is worse than an unencrypted one:
  SelfPost does not store the password. The trigger to make encryption
  mandatory is a second administrator (at which point "who downloaded it"
  stops being one person).
- **A journal row left without delivery lines is closed as `bounced` rather
  than left as it is.** The "forever `queued`" risk is gone: `mail.log` moved to
  `/data/log/` and survives container recreation, and the log tailer keeps its
  read position (`logtail_state`), so the tail is read after a
  start. What remains are rows whose delivery lines are lost for good (the log
  rotated past 14 files while the panel was down, or was deleted): the
  reconciliation against `postqueue -p` sees the message is not in the queue and
  after a 2-minute grace marks it `bounced`. If the message did in fact go out,
  the status is a false negative. Accepted deliberately: a delivery the panel
  cannot confirm must not be shown as confirmed, and a permanent `queued` is
  indistinguishable from "in flight right now". Reconciliation does not run
  until the tailer has read the log to the end, and touches nothing if
  `postqueue` is unreadable. See [architecture.md](architecture.md) § Log
  tailer.
- **DMARC aggregate reports are accepted without authentication.** Anyone on
  the internet can mail a fabricated report (or displace a genuine one — the
  `UNIQUE(reporter, report_id, domain)` upsert is delete-then-insert) and skew
  the alignment statistics the panel shows. Inherent to how `rua` works:
  reporters are arbitrary third parties and the mailbox address is published in
  DNS. The ingest already constrains the meaningful part — a report cannot be
  filed under a domain this instance does not host, and the claimed domain is
  cross-checked against the `+tag` of the delivery address (effective since
  2.0.0: before it the recipient never reached the command) — and the data is
  advisory statistics only; nothing enforces off it. What a stranger can do is
  limited to a domain that chose a SelfPost-hosted address, through that
  address: file reports under it, replace one whose reporter and id they know,
  and fill that domain's 500 — not another domain's. A message that is not a
  report the relay keeps is dropped, not bounced (the envelope sender is the
  author's to write); a genuine report the parser cannot read is lost the same
  way, with a count and a line in the mail log. The trigger to revisit is
  any feature that lets report contents drive an automatic action (policy
  changes, rate limiting, alerting an operator into a config change).
- **The application auth-IP allow-list is enforced fail-open.** The check runs
  in the journal milter (`internal/milter/authip.go`) with
  `default_action=accept` and skips on store errors, so if the journal milter
  does not answer the restriction is not applied; the SASL login itself
  always succeeds. (While the milter is up the list holds for every message,
  and an unknown client address is refused - see Mail path; what is accepted
  here is the milter not answering. Since `2.4.0` a panel that is down
  altogether defers mail through the sender check, which shares its process;
  what is left of this window is a journal milter that hangs or a database
  that errs while the check beside it still answers.) This
  is documented in [guide.md](guide.md) as **not an authentication boundary** —
  it is a blast-radius limiter for a leaked application password, on the same
  fail-open path as level-2 rate limiting, and a milter outage already stops
  the journal from recording sends at all. The trigger to revisit is a
  requirement for the allow-list to hold as a security boundary — at which
  point it must move to the SASL/smtpd layer and fail closed.
- **The key the spam filter signs with is readable by anyone on the host.**
  The inbound journal reads the filter's verdict only next to the filter's
  signature (see Mail path), and the key for it, `data/antispam/filter.key`,
  is 0644 like the list files beside it: the filter is another user in another
  container. Someone with a shell on the host could therefore sign a verdict
  of his own - into a monitoring page, on a host where he could do far more.
  Senders on the internet, whom the signature is for, cannot read it. Trigger
  to revisit: the same as for the list files - a shared host.
- **An allow entry for a sender address or domain trusts what the sending
  server says.** The filter matches the envelope sender, which is not
  authenticated: anyone who knows or guesses an allowed address can send as it
  and is given the entry's weight, which no spam score outweighs. The page's
  Help and the guide say so and recommend an entry for the client's address or
  network instead. Deny entries have no such problem - claiming to be a denied
  sender only gets mail refused - and deny wins over allow. Trigger to
  revisit: allow entries being used as the main way to receive mail from a
  sender, at which point the rule should also require the sender's SPF or
  DKIM to pass.
- **The filter lists are files readable by anyone on the host.**
  `/data/antispam` is 0755 and its six files 0644, unlike the rest of `/data`:
  the filter runs under another user in another container. They hold sender
  addresses, domains and networks - who the operator blocks and whom he lets
  through - but no message data. The inbound journal itself is in the
  database, 0640. Trigger to revisit: a shared host, where the sidecar should
  instead be given the files through a group it shares with the panel.
- **An application can learn which domains are senders of this server.**
  A `From:` whose display name or comment names an address at a domain this
  server signs for is refused; one naming any other domain passes. The reply
  no longer names the domain (audit 2026-10-12, L8), but refused or passed
  answers the question for whoever sends the line. Accepted for now: it needs
  an application of this server, and closing it means refusing every
  foreign address in a display name, which changes the owner's 2.4.0 rule
  (a stranger's address in the name passes) - his decision, asked on
  2026-10-14.
- ~~**A row of the inbound log belongs to a domain by name.**~~ Struck in
  `2.5.0` (audit 2026-10-12, L12): both logs now hold a domain by id, see
  § Roles.
- **Level-2 in-flight reservations can outlive a dropped SMTP session until TTL.**
  The journal milter cannot observe connection-close directly through
  go-milter, so if a client disconnects without an explicit `Abort`, its
  in-memory reservation is released by TTL expiry, not immediately. The map is
  bounded by key and TTL (currently 10 minutes), so the effect is temporary
  over-counting on that app/domain key under churn. Accepted deliberately:
  immediate cleanup would require protocol hooks the current milter interface
  does not expose; the TTL bound keeps it finite. Trigger to revisit: sustained
  high-volume false throttling attributable to orphaned reservations.
- **Access to `mail.log` from the unprivileged panel.** The `/data/log`
  directory is `2750 postfix:selfpost` and the file is `0640`: `postlogd` (user
  `postfix`) writes, the panel reads through the shared `selfpost` group, and
  the file is inaccessible to others. The log holds envelope addresses and
  client IPs, but neither message bodies nor headers; it is excluded from
  backups (`log/` is skipped) so that a dump stays state rather than
  diagnostics. Since 2.2.0 the tailer copies the lines of queued outbound
  messages into the database for the message's page, and those are in a
  backup (§ Backup and domain export); mail offered on port 25 is not copied.
- **The panel submits mail of its own.** Notifications leave through
  `sendmail(1)` run by the unprivileged panel user: a fixed argument list, the
  two addresses after `--`, no shell. The recipient is a panel user's account
  e-mail, validated when it was saved and checked again before it reaches a
  header or the command line; the subject and body are composed by the panel
  from its own readings. Two of those readings can carry text from outside - a
  DNS check's finding, and a queued message's sender address - and both are
  reduced to printable ASCII and cut before they are printed, so a
  notification's body can contain an address a stranger chose, as a few plain
  characters and nothing else. A refused-mail notification names the domain
  and the applications from the registry, never the domain a client wrote in
  `MAIL FROM`: that string is not yet held against the login when the milter
  refuses, and the first build took it for the subject - an application over
  its limit could have made the panel write a message per made-up domain, to
  another tenant's users among others (security review, 2026-10-12; fixed
  before release). The panel's membership in `postdrop` is what lets it
  submit: under `no-new-privileges` the setgid bit of `postdrop` does not
  take, so only root, `postfix` and the panel can. Notifications are signed
  with a key of the server's own name and bypass the level-1 and level-2
  limits. What bounds them: the subjects are the registered domains, the
  certificate and the queue; a domain's DNS can be reported and cleared about
  a dozen times a day at most by someone who flaps the zone; a domain's
  limits once a day; and no more than 60 messages leave in any hour whatever
  the rules arrive at.
- **The sender check stops mail while the panel does not answer.** It is a
  role of the panel process, attached with `default_action=tempfail`, so a
  panel that is down or slower than the milter timeouts (15 s to connect
  and for a command, 30 s for the verdict at the end of the headers) defers
  every submission: availability of sending now depends on the panel,
  which it did not before `2.4.0`. Chosen by the owner over the alternative,
  a check that is absent whenever the panel is - that one could be switched
  off by whoever can make the panel slow. Supervisord restarts the panel at
  once and takes the container down after three failed starts, so the
  outage is seconds or the whole instance; Server › Health counts the
  downtime and the mail deferred for it. Trigger to revisit: those numbers
  not staying at nothing - then the check moves into a process of its own
  ([plans/from-header-binding.md](plans/from-header-binding.md)).
- **A malformed packet on a milter socket of the panel ends the panel.** The
  milter library indexes into an empty packet and nobody recovers the panic,
  so the process exits and supervisord starts it again. The sockets are
  reachable only inside the container, by the group Postfix is in (mode
  0660); the journal milter always had this, and since `2.4.0` the seconds
  of the restart defer mail instead of passing it unjournalled (technical
  review, 2026-10-12). Trigger to revisit: anything untrusted able to open
  a Unix socket in the container.
- **When the hostname is itself a sending domain,** the server's mail is
  signed with that domain's key. Whoever may export the domain has the key,
  and its applications may send as `postmaster@<hostname>` by right. The
  guide says so; a hostname that is nobody's sending domain avoids it.
- **A message's page can show another message's log lines after a queue-id
  collision.** Postfix's short queue ids repeat: the id is the microsecond of
  arrival and the queue file's inode, and inodes come round within seconds.
  The page takes the stored lines of the row's queue id dated within the
  row's own queue lifetime (about six days), so a second message has to get
  the same microsecond on the same inode in that span - about one in a
  million per reuse of the inode, and not something a sender can aim. When it
  happens, a `domain` user could read the log lines of a message that is not
  theirs. The search of the current `mail.log` that the page did before
  2.2.0 had the same exposure over a day. Trigger to revisit: a report of
  foreign lines on a page, or a reason to turn on `enable_long_queue_ids`,
  which ends it.
- **A `domain` user may point an inbound domain's upstream at any host and
  port.** The upstream form checks that the host is a well-formed name or IP
  and the port is 1–65535; loopback, private and link-local addresses are
  allowed, so a `domain` user with an assigned inbound domain can make this
  server's Postfix open SMTP connections to any address it can reach and hand
  mail for that domain to it. Accepted deliberately: delegation is within the trusted-operator model — a `domain`
  user is an operator the `global` user chose to give that domain, and the
  upstream of a backup-MX is by nature a host on someone's private network.
  The connection is made by Postfix and carries only mail for that domain; the
  panel itself makes no request to the upstream. Trigger to revisit: `domain`
  users who are not trusted operators — the same condition as for CSRF tokens
  — at which point an allow-list of upstream networks, or restricting the
  field to the global role, is the fix.
- **Account's profile (username, e-mail) is saved without the password.**
  Saving the profile does not ask for the current password, as changing the
  password does. Since 2.3.0 the e-mail is where notifications go, so a stolen
  session can turn them to another address: the thief then learns what they
  say - which domain's DNS is wrong, which application is refused, when the
  certificate expires, sender addresses of old queued mail - and the user
  stops hearing it. Nothing in a notification gives access. A `domain` user
  can likewise point signed mail of this server at a third party's address,
  a dozen messages a day per assigned domain at most. It
  becomes a requirement to ask for the current password when changing
  `users.email` as soon as [password-reset](roadmap.md#password-reset) mails a
  link to it — from then on whoever can change the address could take the
  account over.

## ADR: CSRF via origin checking, without tokens

**Context.** The panel is forms (`POST`) with a cookie session — the classic
CSRF surface. What is needed is a way to tell a request from the panel's own
page apart from one initiated by a third-party site in a logged-in user's
browser. The panel is multi-user since 1.2.0 (a `global` user plus
zero or more `domain` users, each scoped to their assigned domains), but
that is an authorization boundary (who can see or change what), not a change
to the CSRF threat: the attacker in scope here is still an external site
riding a legitimate user's cookie, not one panel user attacking another
through the browser.

**Decision.** `originAllowed` in
[internal/web/security.go](../internal/web/security.go) checks `Sec-Fetch-Site`
(when the browser sends it) or `Origin` (fallback) against the panel's host; a
request carrying neither header is **allowed through** rather than rejected.
There are no session-bound tokens embedded in forms. The check applies the same
way regardless of the requesting user's role.

**Why not tokens.** Cross-user CSRF is not the threat model here: a
`domain` user's browser sending a request still needs that user's own
cookie, so a token would not add a boundary between roles that the
authorization checks (`Principal.CanAccessDomain`,
[internal/web/auth/principal.go](../internal/web/auth/principal.go); route
gating in [internal/web/handlers/authz.go](../internal/web/handlers/authz.go))
don't already enforce. The remaining case is an external site making a
logged-in user's browser send a request, which the origin check covers without
touching a single template.
A token would need a hidden field in roughly two dozen forms and
synchronisation with every new form, and it would still not protect against
XSS inside the panel — code executing in the panel's origin reads the token
and sends the request itself. XSS is handled by `html/template` auto-escaping
and CSP, so that is a separate line of defence, not a CSRF token.

**Trade-off.** A client that sends neither `Sec-Fetch-Site` nor `Origin` (a
genuinely old browser, or a webview with a frozen engine) stays vulnerable — see
"Accepted risks" above. This is a deliberate choice not to break the panel in
such a client, at the price of a narrow residual surface.

**Revisit if:** a requirement appears for protection that does not depend on
browser behaviour, or `domain` accounts stop being trusted operators (for
example, if a future release lets a `global` user invite `domain` users
whose browsers/devices are not vetted) — at that point cross-role request
forgery inside the panel would need its own analysis, separate from the
external-site case this ADR covers.

**Decision of 2026-10-13 (owner).** Asked to choose between this ADR, tokens,
and a strict origin check, the owner chose the ADR with the strict check
([origin-strict](roadmap.md#origin-strict)) and no tokens. Session-bound tokens
are the answer to return to - a hidden field in every state-changing form and
htmx path, checked against the session, on top of the origin check - when
either condition above comes true, and also when: a browser the panel must
support is found to send neither header or to send them wrongly; the panel
gains a state-changing request that a browser sends without these headers; or
an outside review requires a defence that the server alone holds.

## How this list grows

The pre-release vulnerability review (history — CHANGELOG `[0.5.0]` Security)
closes every finding in one of two ways: a fix before the tag, or an entry here
with its rationale and its condition for revisiting, like the items above.
There is no third option ("we looked at it and moved on").
