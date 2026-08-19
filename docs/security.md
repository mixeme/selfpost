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
(unauthenticated DMARC ingest, fail-open auth-IP enforcement).
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

### Output and process

- Rendering goes through `html/template` with auto-escaping (queue, log,
  journal, themes).
- The panel process is **not root** (`user=panel` in supervisord); path access
  is granted through the `selfpost` group with minimal permissions.

### Mail path (security-relevant)

- **Not an open relay** — SASL only on 465/587; `reject_unauth_destination`;
  `smtpd_sender_login_maps` + `reject_sender_login_mismatch`.
- **Inbound relay (optional, `INBOUND_RELAY_ENABLE`)** — port 25 is not an
  open relay either: SASL is off; `smtpd_relay_restrictions` /
  `smtpd_recipient_restrictions` are `reject_unauth_destination` and
  `reject_unlisted_recipient`; maps list only configured domains and
  recipients. Domains with no upstream host are omitted from the maps so mail
  is never accepted with nowhere to send it. Prefer recipient mode `list` to
  refuse unknown addresses at RCPT (no backscatter). OpenDKIM is not attached
  on inbound. An optional antispam milter is inbound-only; default action is
  fail-open (`accept`).
- TLS is mandatory before credentials are transmitted (465 wrapper / 587
  `encrypt`).
- `TRUSTED_PROXY_CIDR` — only explicitly trusted proxies may supply
  `X-Forwarded-For` for login rate limiting; empty means XFF is ignored.

### Backup and domain export

- Both files are secrets: a full backup carries DKIM keys, `sasldb2`, the
  administrator's password hash, `docker-compose.yml`, `.env`, and the TLS
  private key from `certs/` when present; a domain export carries the DKIM key
  and **working** application passwords in the clear (otherwise a transfer
  without recreating credentials would be impossible).
- Both downloads can be encrypted with a password (a checkbox on the form):
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
  deliberately: every panel user (global or domain-admin) is an operator who
  picks their own browser, not an untrusted party the panel needs to defend
  against, and a strict mode would not "protect" such a client, it would
  simply break the panel in it. Tightening is one line in `originAllowed`
  ([internal/web/security.go](../internal/web/security.go)): return `false`
  instead of `true` in the "neither header present" branch.
- **Session-bound CSRF tokens are not implemented.** The origin check closes the
  neighbouring-subdomain case but depends on browser behaviour; a token does
  not. The price is a hidden field in roughly two dozen forms. The trigger to
  revisit is a requirement for protection that holds regardless of the browser,
  or a domain-admin population the global administrator does not fully trust
  (see the ADR below). A token would not save the panel from XSS inside it
  either: code executing in the panel's origin sends the request itself —
  against that, `html/template` auto-escaping and CSP do the work, which is why
  templates must contain no inline scripts and no inline styles.
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
  round trip) for every one of these forms, which is what
  [`user_delete.html`](../internal/web/view/templates/user_delete.html) and
  `domain_delete.html` already do for the two highest-blast-radius deletes.
- **Encrypting backups and exports is an option, not the default.** With the
  checkbox cleared the file downloads in the clear, as in 1.0. Otherwise an
  operator with nowhere to keep a password would lose the ability to take a
  backup at all, and a permanently undecryptable archive is worse than an
  unencrypted one: SelfPost does not store the password. The trigger to make
  encryption mandatory is a second administrator (at which point "who
  downloaded it" stops being one person).
- **A journal row left without delivery lines is closed as `bounced` rather
  than left as it is.** The "forever `queued`" risk is gone: `mail.log` moved to
  `/data/log/` and survives container recreation, and the log tailer keeps its
  read position (`logtail_state`, migration `0003`), so the tail is read after a
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
  cross-checked against the `+tag` of the delivery address — and the data is
  advisory statistics only; nothing enforces off it. The trigger to revisit is
  any feature that lets report contents drive an automatic action (policy
  changes, rate limiting, alerting an operator into a config change).
- **The application auth-IP allow-list is enforced fail-open.** The check runs
  in the journal milter (`internal/milter/authip.go`) with
  `default_action=accept` and skips on store errors, so if the milter is down
  the restriction is not applied; the SASL login itself always succeeds. This
  is documented in [guide.md](guide.md) as **not an authentication boundary** —
  it is a blast-radius limiter for a leaked application password, on the same
  fail-open path as level-2 rate limiting, and a milter outage already stops
  the journal from recording sends at all. The trigger to revisit is a
  requirement for the allow-list to hold as a security boundary — at which
  point it must move to the SASL/smtpd layer and fail closed.
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
  diagnostics.

## ADR: CSRF via origin checking, without tokens

**Context.** The panel is forms (`POST`) with a cookie session — the classic
CSRF surface. What is needed is a way to tell a request from the panel's own
page apart from one initiated by a third-party site in a logged-in user's
browser. The panel is multi-user since 1.2.0 (a global administrator plus
zero or more domain-admin users, each scoped to their assigned domains), but
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
domain-admin's browser sending a request still needs that domain-admin's own
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
browser behaviour, or domain-admin accounts stop being trusted operators (for
example, if a future release lets a global administrator invite domain-admins
whose browsers/devices are not vetted) — at that point cross-role request
forgery inside the panel would need its own analysis, separate from the
external-site case this ADR covers.

## How this list grows

The pre-release vulnerability review (history — CHANGELOG `[0.5.0]` Security)
closes every finding in one of two ways: a fix before the tag, or an entry here
with its rationale and its condition for revisiting, like the items above.
There is no third option ("we looked at it and moved on").
