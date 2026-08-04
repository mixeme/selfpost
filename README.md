![SelfPost](docs/assets/selfpost-stamp.svg)

# SelfPost

Self-hosted outbound SMTP relay with a web control panel, shipped as a single
Docker image. Postfix + OpenDKIM + a small Go panel run together under
`supervisord`; the panel manages multiple sending domains, per-domain DKIM keys
and SASL-authenticated applications bound to their domain.

SelfPost sends mail straight to the internet from your own IP, with DKIM
signing, and is configured once through the panel. It is **outbound only** — it
does not receive mail, provide mailboxes, or offer webmail.

> **Status: under active development.** See [docs/specification.md](docs/specification.md)
> for the full requirements, [docs/implementation-plan.md](docs/implementation-plan.md)
> for the phased build plan, and [docs/security.md](docs/security.md) for the
> security trade-offs that were accepted knowingly.

## Requirements (site checklist)

Providing these is the operator's job, not a feature of SelfPost — the panel
can't fix a blocked port or a missing PTR record for you.

- [ ] A static IP address.
- [ ] Outbound TCP port 25 unblocked (many consumer/cloud hosts block it by
      default — check with your provider before anything else).
- [ ] PTR/rDNS for that IP set to your mail hostname (see [DNS setup](#dns-setup)).
- [ ] Reasonable starting IP reputation — a fresh IP still needs [warmup](#ip-warmup).
- [ ] A reverse proxy in front of the panel (see [Reverse proxy](#reverse-proxy-mandatory)) — SelfPost never terminates HTTPS itself.
- [ ] Docker + Compose v2 on the host.

## Quick start

```sh
mkdir -p selfpost && cd selfpost
curl -O https://raw.githubusercontent.com/mixeme/selfpost/main/deploy/docker-compose.yml
curl -O https://raw.githubusercontent.com/mixeme/selfpost/main/deploy/.env.example
mv .env.example .env   # then edit SELFPOST_HOSTNAME etc.
docker compose up -d
```

`SELFPOST_HOSTNAME` is required — the container exits immediately with an
explanatory error if it's unset, since it doubles as the Postfix HELO name,
the SASL realm, and must match both the PTR record and the certificate
CN/SAN.

This starts SelfPost alone; it assumes Apache is already installed on the host
as the reverse proxy (see below) and expects certificates at `./certs`. The
first log line (`docker compose logs -f`) prints the one-time setup link —
open it to create the admin account. That username and password can be changed
later from the panel's *Account* page (changing the password signs out every
other session).

The same link is also written to `/data/setup-token` inside the container —
`./data/setup-token` on the host, mode `0600` — and deleted the moment setup
completes. If this host ships its container logs to a central aggregator,
prefer the file: the link is a bearer token valid for ten minutes, and reading
it this way keeps it out of the log pipeline (and out of whatever retains it
afterwards) entirely.

```sh
docker compose exec selfpost cat /data/setup-token
```

## Reverse proxy (mandatory)

SelfPost's panel speaks plain HTTP and never terminates TLS itself — a reverse
proxy in front of it is not optional. The proxy is also the project's only
source of TLS certificates: whatever it obtains via ACME/Let's Encrypt gets
bind-mounted **read-only** into the SelfPost container, and Postfix uses those
same PEM files for TLS on 465 (and 587, if enabled). If the panel and the mail
service share one hostname — the common case — it's genuinely one certificate
serving both.

SelfPost isn't tied to a specific proxy; pick whichever fits your host:

| Proxy | Where certs live | Fragment |
|---|---|---|
| **Apache** (default/recommended) | Host disk, via the certbot Apache plugin — PEM files ready to bind-mount, no extraction step. | [deploy/apache/selfpost-vhost.conf](deploy/apache/selfpost-vhost.conf) |
| nginx | Host disk, via a certbot sidecar container — same PEM-ready shape as Apache. | [deploy/nginx/](deploy/nginx/) |
| Caddy | Automatic ACME, zero extra containers — simplest, but its on-disk cert path is versioned internal layout, not a stable API; verify it for the Caddy version you run. | [deploy/caddy/](deploy/caddy/) |
| Traefik | Bundled inside `acme.json` — needs a small extraction script to produce standalone PEM files. | [deploy/traefik/](deploy/traefik/) |

Apache is the recommended default because the certbot Apache plugin already
writes plain `fullchain.pem`/`privkey.pem` files to a predictable path with no
extra moving parts between "certificate issued" and "Postfix can read it."

**The proxy needs no security configuration of its own.** The panel emits its
own `Content-Security-Policy`, `Strict-Transport-Security`, `X-Frame-Options`,
`X-Content-Type-Options` and `Referrer-Policy` — deliberately, so the part
that's easy to get wrong lives in the service rather than in a config file
somebody edits under pressure. There is exactly one thing the proxy must do:
**pass the original `Host` header through**. All four fragments above already
do (Apache `ProxyPreserveHost On`, nginx `proxy_set_header Host $host`, Caddy
and Traefik by default). A proxy that rewrites `Host` instead makes the panel
reject every form submission as cross-origin — the log says so explicitly,
printing the `Origin` and `Host` it compared.

## Environment variables

Copy [deploy/.env.example](deploy/.env.example) to `.env` next to your
`docker-compose.yml`. The table below lists every variable an operator is
expected to set; defaults match the code exactly.

| Variable | Purpose | Default | Set in |
|---|---|---|---|
| `SELFPOST_HOSTNAME` | Mail-server identity: Postfix HELO/EHLO, SASL realm, certificate CN/SAN, and the hostname the PTR check expects. Bare FQDN only — no scheme or port. | *(required)* | `.env` |
| `SUBMISSION_ENABLE` | When `true`, also listen on port 587 with STARTTLS (RFC 6409 submission) alongside the primary 465/smtps listener. | `false` | `.env` |
| `RATE_LIMIT_MESSAGES_PER_IP` | Level-1 backstop: maximum messages one client IP may submit per window (Postfix `smtpd_client_message_rate_limit`). See [Rate limiting](#rate-limiting). | `100` | `.env` |
| `RATE_LIMIT_WINDOW_SECONDS` | Level-1 window length in seconds (Postfix `anvil_rate_time_unit`). | `3600` | `.env` |
| `SEND_LOG_RETENTION_DAYS` | Days of send-log history kept before the background sweep deletes rows — the main driver of `/data` growth over time. | `90` | `.env` |
| `PANEL_SESSION_IDLE_DAYS` | Sliding idle timeout for the panel login session, in days. There is no absolute cap: an admin who keeps coming back stays signed in indefinitely. | `7` | `.env` |
| `SELFPOST_DNS_RESOLVERS` | Comma-separated recursive resolvers the panel's PTR/SPF/DKIM/DMARC checks query directly (so they report what the internet sees, not what this host's stub resolver synthesises). | `1.1.1.1:53`, `8.8.8.8:53`, `9.9.9.9:53` when unset | `.env` |
| `TRUSTED_PROXY_CIDR` | Comma-separated CIDRs (bare IPs allowed) of reverse proxies allowed to supply `X-Forwarded-For` for login, setup, and account-change rate-limiting. **Leave unset unless you know the exact address of your reverse proxy.** A wrong value lets a client spoof its rate-limit key by sending a forged `X-Forwarded-For` header — the panel trusts the last hop only when the TCP peer matches one of these CIDRs. Behind the default Apache host-network setup this is typically the Docker bridge gateway, e.g. `172.18.0.1`. | *(empty — XFF ignored)* | `.env` |

TLS certificate paths (`TLS_CERT_FILE`, `TLS_KEY_FILE`) are fixed in
[deploy/docker-compose.yml](deploy/docker-compose.yml) to match the `./certs`
bind mount — configure the mount, not these variables.

**Internal variables (not part of the operator interface).** The following are
read by the panel or startup scripts but are not meant to be changed in a
normal deployment; documenting them here avoids treating accidental overrides as
supported configuration:

- **Panel paths and tuning:** `SELFPOST_DATA_DIR` (`/data`), `SELFPOST_DB_PATH`
  (`/data/selfpost.db`), `SELFPOST_SETUP_TOKEN_FILE`
  (`/data/setup-token`), `PANEL_HTTP_ADDR` (`:8080`),
  `JOURNAL_MILTER_SOCKET` (`/run/selfpost/journal.sock`), `MAIL_LOG`
  (`/var/log/mail.log`), `PANEL_COOKIE_SECURE` (`true`), `OPENDKIM_SOCKET`
  (`/run/opendkim/opendkim.sock`), `OPENDKIM_DIR` (`/data/opendkim`),
  `DKIM_SELECTOR_DEFAULT` (`selfpost`), `SASL_DB_PATH`
  (`/data/sasl/sasldb2`), `SASL_REALM` (defaults to `SELFPOST_HOSTNAME`),
  `POSTFIX_DIR` (`/data/postfix`), `POSTFIX_SENDER_LOGIN_MAPS`
  (`/data/postfix/sender_login_maps`).
- **Milter and Postfix startup:** `MILTER_CONNECT_TIMEOUT` (`15s`),
  `MILTER_COMMAND_TIMEOUT` (`15s`), `MILTER_CONTENT_TIMEOUT` (`30s`),
  `MILTER_WAIT_TIMEOUT` (`30` seconds).
- **Background maintenance:** `TLS_RELOAD_INTERVAL_SECONDS` (`86400` — daily
  `postfix reload` to pick up renewed certificates),
  `LOGROTATE_INTERVAL_SECONDS` (`21600` — check `mail.log` rotation every six
  hours; rotated logs are kept 14 days and each rotation triggers
  `postfix reload`).

## DNS setup

Two different scopes — don't confuse them:

**Server level (once, for the machine itself):**
- **PTR/rDNS** for the server's IP, pointing at its mail hostname. Most
  receiving mail servers weigh this heavily; get it from whoever assigns the IP
  (hosting provider's panel/support), not from your own DNS zone.

**Domain level (for *every* sending domain you add in the panel):**
- **SPF** — a TXT record on the domain authorizing this server to send on its
  behalf (e.g. `v=spf1 a mx ip4:<server IP> -all`, adjusted to your setup).
- **DKIM** — a TXT record with the exact value the panel shows on that
  domain's page (`domain page → DKIM TXT record`), one selector per domain.
- **DMARC** — a `_dmarc` TXT record (even a conservative `p=none` starts
  building reporting/reputation history).

Skipping any of the three per-domain records is the single most common reason
mail lands in spam even though SelfPost delivered it correctly — DKIM passing
doesn't help if SPF/DMARC are absent. **Whenever you add a new domain in the
panel, add its DNS records at the same time**, not later.

The panel checks both scopes for you and tells you what is actually published:
the *Status* page verifies the server's hostname and its reverse record
(forward-confirmed reverse DNS), and each domain's page shows a *DNS status*
card comparing the published DKIM record against the key this server signs with,
plus the domain's SPF and DMARC records. Results are cached for a few minutes;
use *Re-check* right after publishing a record. The SPF check is deliberately
shallow — it looks for a mechanism that literally covers this server's address
and does not follow `include:` or `redirect=`, so a record that authorizes the
server through an include is reported as "cannot tell" rather than as a failure.

## IP warmup

A brand-new IP has no sending history, so receiving servers are cautious with
it regardless of how correct your DKIM/SPF/DMARC are. Start with low volume to
a domain, increase gradually over days/weeks rather than sending everything on
day one, and check the IP against major blocklists (Spamhaus and similar)
before and during warmup. This is inherent to how mail reputation works on the
public internet, not something SelfPost's configuration can shortcut.

## Operations

After sign-in the panel opens on **Status** — the place to answer "is the
service healthy and will mail be accepted?"

- **Status** (`/status`) — supervised processes (Postfix, OpenDKIM, panel),
  TLS certificate validity and expiry, milter socket presence, and a short
  Postfix queue summary. The hostname block compares `SELFPOST_HOSTNAME`
  against the PTR record the internet publishes for this server's IP
  (forward-confirmed reverse DNS); use *Re-check* after changing DNS. The
  **Reload configuration** button re-applies OpenDKIM tables and the Postfix
  sender map from the database — use it if daemons drifted from what the panel
  shows after manual edits under `/data`.
- **Domains** (`/domains`) — add sending domains, inspect each domain's DKIM
  TXT value, SPF/DMARC checks, and SASL applications. Per-domain rate limits
  (level 2) are configured here. *Export domain* writes a single-domain archive;
  *Import a domain* on the Backup page reads one back in.
- **Deliveries** (`/deliveries`) — searchable send log with server-side filters
  by domain and application. Each row shows status `queued` (accepted, not yet
  delivered), `sent` (handed off successfully), or `rejected` (refused — for
  example by a level-2 rate limit). Retention is controlled by
  `SEND_LOG_RETENTION_DAYS`.
- **Mail queue** (`/mail-queue`) — live view of messages Postfix is still
  trying to deliver or deferring.
- **System log** (`/system-log`) — tail of `/var/log/mail.log` (Postfix and
  related daemon lines). The log rotates daily (14 files kept) with a
  `postfix reload` after each rotation; a background loop checks every six
  hours.
- **Backup** (`/backup`) — download a full-server backup or import a
  single-domain export. See [Backup, restore, and moving a single domain](#backup-restore-and-moving-a-single-domain).
- **Account** (`/account`) — change the administrator username and/or password.
  Application SASL logins are separate and are not changed here.

**Sessions.** A login survives a container restart: sessions live in SQLite, not
in memory. Expiry is a sliding idle window (`PANEL_SESSION_IDLE_DAYS`, default
seven days) with no absolute lifetime cap — an admin who keeps using the panel
stays signed in indefinitely. HTMX polling on the monitoring screens
(Deliveries, Mail queue, System log, and the Status health fragment) does
**not** count as activity, so an auto-refreshing tab left open will not keep a
session alive forever. Changing the password signs out every other session but
leaves the current browser signed in.

**Upgrading.** Bump the pinned image tag in `docker-compose.yml` to the target
release, then `docker compose up -d`. The backup version check requires the
running image to match the version that created a full backup — see [Fixed image
tag](#fixed-image-tag).

## Rate limiting

SelfPost applies two independent limits; both can refuse a submission, but only
level 2 writes a `rejected` row in the send log.

**Level 1 (IP backstop)** — always on, configured via `.env`:

- `RATE_LIMIT_MESSAGES_PER_IP` → Postfix `smtpd_client_message_rate_limit`
- `RATE_LIMIT_WINDOW_SECONDS` → Postfix `anvil_rate_time_unit`

This is an anvil limit per connecting client IP. It keeps working even if the
journal-milter (level 2) is down.

**Level 2 (per domain / per application)** — optional, configured in the panel
on each domain's page or on an individual application. You set a message
ceiling, a time window, and optionally restrict the limit to specific client
IPs; an empty IP list means the differentiated limit does not apply. When
exceeded, Postfix returns a 4xx and the refusal is recorded in Deliveries as
`rejected`.

## Backup, restore, and moving a single domain

Two related but distinct operations — spec 7.5:

- **Full backup** (whole `/data`: SQLite, all domains' DKIM keys, all
  applications' SASL credentials, `manifest.json` with the version that
  created it): panel button (*Backup* → *Full backup*), or from the
  host:
  ```sh
  docker exec <container> selfpost-backup > selfpost-backup.tar.gz
  ```
  **Restore** means unpacking that archive into a fresh `/data` bind mount and
  starting a container of the **exact same image version** that created it —
  SelfPost refuses to start otherwise and tells you which tag to use. This is
  why the compose file below pins a fixed tag rather than `:latest`: without a
  known version, there'd be no way to tell which image restoring a given
  backup actually requires.

- **Export/import a single domain** (domain page → *Export domain* to write the
  file, *Backup* → *Import a domain* to read it back in): moves one domain — its DKIM key and its applications' **working**
  SASL passwords — to a different SelfPost instance without regenerating
  anything, so DNS (the DKIM TXT record) doesn't need to change. Unlike a full
  restore, this works across different hostnames/instances.

Both files are **secrets** — they contain the admin password hash (full
backup) or working application credentials (domain export) in the clear or in
directly reversible form. Treat them like any other credential material:
encrypt at rest, restrict who can read them, don't email them around.

## Fixed image tag

`deploy/docker-compose.yml` pins an explicit version (`ghcr.io/mixeme/selfpost:X.Y.Z`),
deliberately never `:latest`. This is a direct consequence of the backup
version check above: the panel binary's embedded version and the image tag
that produced it are the same value by construction (the release CI stamps
both from one git tag — see `.github/workflows/release.yml`), so pinning the
tag is what makes "restore into the same version" a checkable fact rather than
a guess. Upgrade by bumping the tag deliberately, not by riding a moving
target.

## Machine requirements

Rough guide, not a hard floor: **1 vCPU**, **512MB–1GB RAM** (the stack — three
processes plus SQLite — idles around 100–150MB; the rest is headroom for
backups, log-tailer/retention sweeps and concurrent TLS handshakes coinciding),
**8–10GB disk**. Disk usage grows mainly from the send log (bounded by
`SEND_LOG_RETENTION_DAYS`, default 90) and the rotated `mail.log` (kept 14 days
in-image), not from the application itself. On boxes with little RAM, a small
swap file is cheap insurance against those occasional coincident spikes.

## Repository

- Primary: <https://codeberg.org/mix/selfpost>
- Mirror: <https://github.com/mixeme/selfpost>

## License

[AGPL-3.0](LICENSE). The AGPL closes the "SaaS loophole": if you run a modified
version as a network-accessible service, you must make the modified source
available to its users — not only when you distribute copies of the code.
