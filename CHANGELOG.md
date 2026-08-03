# Changelog

All notable changes to this project are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning follows [SemVer](https://semver.org/).

## [Unreleased]

### Changed

- panel: the three monitoring pages now live at URLs that match their nav
  labels — Deliveries at `/deliveries` (was `/sendlog`), Mail queue at
  `/mail-queue` (was `/queue`), System log at `/system-log` (was `/logtail`).
  Bookmarks to the old paths stop working.

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
- Live production deployment on `selfpost.mixfed.ru` with a real Let's
  Encrypt certificate; end-to-end delivery confirmed (DKIM pass, SPF pass).
