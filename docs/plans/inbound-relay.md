# Plan: inbound-relay (inbound relay)

**Status:** done — shipped in `[1.4.0]` (2026-08-17)  
**Version:** 1.4.0 MINOR (flag off is compatible; not a 2.x break).  
**Order:** after queue-retries `[1.3.1]`. One checklist step remains:
security review of the inbound path (Fable).

---

## Goal

The ability to accept mail on port 25 for explicitly configured domains and
forward it to a given upstream backend (a backup-MX / relay-forwarder role), as
a **module disabled by default** that changes neither the behaviour nor the
attack surface of the base outbound relay.

## What it is for (scenarios)

- **Backup-MX** — accept mail while the domain's primary mail server is
  temporarily unreachable, and hand it over when it comes back.
- **A front for a server without a public IP** — the operator runs their own
  mail server which, for whatever reason, **cannot accept mail from the
  internet itself** (no static or public IP, behind NAT, a private address,
  inbound port 25 blocked, and so on). SelfPost, with a public IP and a correct
  PTR, acts as the domain's public entry node (the MX points at it) and
  forwards mail to that internal or otherwise unreachable server.

## Scope boundary (critical — what this is NOT)

- **IT IS:** acceptance on 25 for domains from an explicit list, plus
  forwarding (relay/forward) to an upstream (`relay_domains` +
  `transport_maps` + `relay_recipient_maps`). Postfix here is a pure forwarder,
  with no local delivery.
- **IT IS NOT (out of scope, [product.md](../product.md)):** local delivery to
  mailboxes, IMAP/POP3, webmail, Dovecot. No mailboxes at all. SelfPost also
  **neither implements nor bundles** an anti-spam or anti-virus engine
  (rspamd/ClamAV) — but, unlike the earlier wording, it does **not** push
  filtering onto the backend either (see the "Anti-spam" section below): it
  provides an attachment point for an external filter.

## Why as an option / plugin

- Accepting on port 25 changes the threat model (open relay for inbound,
  backscatter, spam ingress). So it is **off** by default behind the
  `INBOUND_RELAY_ENABLE=false` env flag; turning it on is a deliberate step by
  the operator.
- Isolation: separate SQLite tables, separate panel handlers and pages, a
  separate branch of config generation. With the flag off, the inbound
  listener, the tables and the UI are absent — the base outbound path is
  byte-for-byte unchanged.

## What to do

- The `INBOUND_RELAY_ENABLE` env flag (default false); when `true`, generate
  the inbound service and its config from panel state the same way the rest of
  the config is generated (`postfix-config.sh`).
- **`master.cf`:** an inbound `smtp inet` on 25 for accepting from the internet
  (today 25 is used only for outbound delivery). Separate from 465/587: on 25
  SASL is **not** offered and sending outwards is **not** allowed — inbound
  only, for `relay_domains`.
- **Anti-open-relay for inbound (mandatory):** the inbound smtpd's
  `smtpd_relay_restrictions` / `smtpd_recipient_restrictions` accept mail
  **only** for domains in `relay_domains` and **only** for known recipients
  (`relay_recipient_maps`); everything else gets
  `reject_unauth_destination` / `reject_unlisted_recipient`. An open relay, or
  accepting "for anyone", is impossible.
- **Backscatter:** knowing the valid recipients is preferable (reject unknown
  recipient at RCPT stage) so that bounces to non-existent addresses are never
  generated.
- **The panel manages:** the list of inbound domains; for each one the upstream
  destination (`host:port`, transport), an optional list of valid recipients,
  and optional TLS to the upstream. Strict validation of domain, host and port
  (whitelist), injection-safe writing of map files (as with
  `sender_login_maps` in Phase 4), `os/exec` without a shell
  ([security.md](../security.md)).
- **Milters:** OpenDKIM is not needed on the inbound path (we do not sign
  someone else's inbound mail). The journal-milter can optionally be reused for
  an inbound journal (extra work), or the inbound path can go without it in the
  first stage; fail-open behaviour is preserved.
- **Rate limit / size:** a coarse per-client-IP limit (`anvil`, as L1) and
  `message_size_limit` on the inbound smtpd.

## Anti-spam (important, but optional)

This is a valuable option, but it is **not mandatory**: some operators will be
perfectly served by **blind forwarding without filtering** — when the backend
can filter on content itself, when the upstream is trusted, or when the volume
and risk are low. So the anti-spam hook is **off** by default (an empty
`INBOUND_ANTISPAM_MILTER`), and the inbound relay is fully functional without
it.

What matters is something else: where filtering is technically possible. With a
"blind" relay the destination backend sees **SelfPost's** address as the
connecting IP, not the original sender's, so everything on the backend that
depends on the origin IP breaks (DNSBL and reputation are checked against
SelfPost's IP; SPF returns fail, since SelfPost is not in the sending domain's
SPF). **The only point where the real client IP is still visible is the inbound
hop at SelfPost** — so for those who need filtering, it has to be *attachable
right here*, not delegated to a backend that has already lost the information.

The attachment design:

- **The anti-spam engine is a separate optional container** (rspamd or
  similar), which the operator runs **only if this option is wanted** (the same
  principle as the reverse proxy — a separate container outside the SelfPost
  image). SelfPost **neither contains nor starts it** — the image and the "one
  container, three processes" principle are unchanged, and
  [product.md](../product.md)'s out-of-scope list is not violated (SelfPost
  does not implement anti-spam).
- **SelfPost provides the attachment point:** a milter hook on the inbound
  smtpd. The engine's address is set via env (for example,
  `INBOUND_ANTISPAM_MILTER=inet:antispam:11332`, empty → the hook is off) and
  is added to `smtpd_milters` for the **inbound path only** (not on 465/587).
  Postfix passes the milter the real client IP, HELO and PTR — the filter sees
  the true origin. `milter_default_action` for that milter is configurable
  (fail-open vs tempfail); the default is to be decided during implementation.
- **A native backstop with no dependencies:** on that same inbound hop,
  Postfix's own origin-IP facilities are available — `reject_rbl_client`
  (DNSBL) and HELO/PTR checks — and they work even without an external
  container. Plus preserving authentication results for downstream through ARC
  or `Received`, where part of the filtering does remain on the backend.
- **docker-compose:** document an optional anti-spam sidecar fragment (like the
  alternative reverse-proxy fragments) — the container comes up with the stack
  only when the option is enabled.
- **Persistence:** new tables and map files under `/data` — they land in the
  full backup automatically (Phase 9). Domain export/import can be extended
  with the inbound configuration — optional, to be flagged.
- **DNS documentation:** an inbound domain needs an `MX` record pointing at the
  server (unlike outbound, where no MX is required) — to be reflected in the
  README's DNS section.

## Security

[security.md](../security.md): server-side input validation, escaped writes to
config files, `exec` without interpolation, no open relay, protection against
backscatter.

## Done when

With `INBOUND_RELAY_ENABLE=true` and a configured domain, mail arriving on port
25 for that domain is forwarded to the given upstream; mail for unconfigured
domains or recipients is rejected (not an open relay, no backscatter); with
`INBOUND_ANTISPAM_MILTER` set, inbound mail passes through the external filter
with the real origin IP (verified with a sidecar container), and with it empty
the hook stays out of the way; with `INBOUND_RELAY_ENABLE=false` the inbound
port, tables and UI are absent and the base outbound relay is unchanged;
`build`/`vet`/`test`/image green.

## Risks

- open relay / backscatter — removed by `relay_domains` +
  `relay_recipient_maps` + `reject_unauth_destination`;
- the loss of the origin IP for filtering on the backend when forwarding —
  removed by the anti-spam milter hook plus native DNSBL on the inbound hop,
  where the origin IP is still visible;
- port 25 accepting mail widens the attack surface (off by default);
- semver: if the contract turns out incompatible (ports, backup, behaviour with
  the flag off) a major `2.x` is possible; the decision comes after the
  implementation.

**External deployment dependency:** the optional anti-spam container — outside
the SelfPost image, brought up by the operator when the option is enabled.

## Dependencies

A finished outbound path (already implemented). Agreement obtained — see the
status above.

## Implementation checklist

Target version cut: **`1.4.0`** (MINOR). One commit per step; see
[development.md](../development.md) § Plan checklists. UI reference:
[panel-ui inbound mockups](../assets/panel-ui/inbound.html).

- [x] Migration: inbound domain / recipient / transport tables under `/data` — **Opus**
- [x] `INBOUND_RELAY_ENABLE` (default false) in entrypoint + `postfix-config.sh` — **Opus**
- [x] `master.cf`: inbound `smtp inet` on 25; separate from 465/587 — **Opus**
- [x] Generate `relay_domains`, `transport_maps`, `relay_recipient_maps` (injection-safe) — **Opus**
- [x] `smtpd_relay_restrictions` / recipient maps — no open relay, no backscatter — **Opus**
- [x] `internal/store` CRUD + validation (domain, host, port) — **Opus**
- [x] Panel: list, add, domain detail, recipients, danger zone (mockups) — **Sonnet**
- [x] Rate limit + `message_size_limit` on inbound smtpd — **Opus**
- [x] Optional `INBOUND_ANTISPAM_MILTER` + compose fragment — **Opus**
- [x] DNS MX copy in README/guide; `.env.example` — **Sonnet**
- [x] Backup/export inbound config (per plan optional flag) — **Opus**
- [x] Unit + handler tests; image build and container smoke — **Opus**
- [x] [guide.md](../guide.md) and [security.md](../security.md) — **Sonnet**
- [ ] Security review inbound path — **Fable**
- [x] `go vet`, `go test`, e2e if applicable — **Haiku**
