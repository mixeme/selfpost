# SelfPost operator guide

Detailed install, configuration, and day-to-day operations. For a short
overview and quick start, see [README.md](../README.md).

## Table of contents

- [Reverse proxy (mandatory)](#reverse-proxy-mandatory)
- [Local trial](#local-trial)
- [Environment variables](#environment-variables)
- [DNS setup](#dns-setup)
- [IP warmup](#ip-warmup)
- [Operations](#operations)
- [Rate limiting](#rate-limiting)
- [Backup, restore, and moving a single domain](#backup-restore-and-moving-a-single-domain)
  - [Encrypting a backup or export](#encrypting-a-backup-or-export)
- [Published ports](#published-ports)
- [Fixed image tag](#fixed-image-tag)

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
| **Apache** (default/recommended) | Host disk, via the certbot Apache plugin — PEM files ready to bind-mount, no extraction step. | [deploy/apache/selfpost-vhost.conf](../deploy/apache/selfpost-vhost.conf) |
| nginx | Host disk, via a certbot sidecar container — same PEM-ready shape as Apache. | [deploy/nginx/](../deploy/nginx/) |
| Caddy | Automatic ACME, zero extra containers — simplest, but its on-disk cert path is versioned internal layout, not a stable API; verify it for the Caddy version you run. | [deploy/caddy/](../deploy/caddy/) |
| Traefik | Bundled inside `acme.json` — needs a small extraction script to produce standalone PEM files. | [deploy/traefik/](../deploy/traefik/) |

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

## Local trial

The [README quick start](../README.md#quick-start) runs a single container
with `PANEL_COOKIE_SECURE=false` and port 8080 published on localhost. No
reverse proxy, no `./certs` bind mount — Postfix still starts, but the Status
page will report missing TLS material until you mount PEM files at
`/etc/postfix/tls/fullchain.pem` and `privkey.pem`.

The one-time setup link is always printed as
`https://<SELFPOST_HOSTNAME>/setup/<token>` (and written the same way to
`/data/setup-token`). For a local trial that means rewriting the host and
scheme to `http://127.0.0.1:8080/setup/<token>` — the path token is what
matters; the hostname in the printed URL is not reachable as written.

**What works:** the full panel — setup, domains, applications, deliveries view,
mail queue, system log. **What does not:** reliable outbound delivery to the
public internet (no PTR, no real DNS for your domains, port 25 may be blocked
on your network, HELO does not match anything receivers trust).

To exercise SMTP locally as well, generate a throwaway self-signed certificate
and mount it before `docker run`:

```sh
mkdir -p /tmp/selfpost-certs
openssl req -x509 -newkey rsa:2048 \
  -keyout /tmp/selfpost-certs/privkey.pem \
  -out /tmp/selfpost-certs/fullchain.pem \
  -days 1 -nodes -subj '/CN=mail.local.test'
```

Add `-v /tmp/selfpost-certs:/etc/postfix/tls:ro` to the `docker run` command
(and keep `SELFPOST_HOSTNAME=mail.local.test` so it matches the certificate CN).
Clients must skip TLS verification — the cert is not from a public CA.

## Environment variables

Copy [deploy/.env.example](../deploy/.env.example) to `.env` next to your
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
[deploy/docker-compose.yml](../deploy/docker-compose.yml) to match the `./certs`
bind mount — configure the mount, not these variables.

**Internal variables (not part of the operator interface).** The following are
read by the panel or startup scripts but are not meant to be changed in a
normal deployment; documenting them here avoids treating accidental overrides as
supported configuration:

- **Panel paths and tuning:** `SELFPOST_DATA_DIR` (`/data`), `SELFPOST_DB_PATH`
  (`/data/selfpost.db`), `SELFPOST_SETUP_TOKEN_FILE`
  (`/data/setup-token`), `PANEL_HTTP_ADDR` (`:8080`),
  `JOURNAL_MILTER_SOCKET` (`/run/selfpost/journal.sock`), `MAIL_LOG`
  (`/data/log/mail.log` — read by the panel and written by Postfix, so a change
  here has to be matched in `build/postfix-config.sh`),
  `PANEL_COOKIE_SECURE` (`true`), `OPENDKIM_SOCKET`
  (`/run/opendkim/opendkim.sock`), `OPENDKIM_DIR` (`/data/opendkim`),
  `DKIM_SELECTOR_DEFAULT` (`selfpost`), `SASL_DB_PATH`
  (`/data/sasl/sasldb2`), `SASL_REALM` (defaults to `SELFPOST_HOSTNAME`),
  `POSTFIX_DIR` (`/data/postfix`), `POSTFIX_SENDER_LOGIN_MAPS`
  (`/data/postfix/sender_login_maps` — read by Postfix config only; the panel
  always writes `<POSTFIX_DIR>/sender_login_maps`, so overriding this env alone
  desyncs the map Postfix reads from the file the panel maintains).
- **Milter and Postfix startup:** `MILTER_CONNECT_TIMEOUT` (`15s`),
  `MILTER_COMMAND_TIMEOUT` (`15s`), `MILTER_CONTENT_TIMEOUT` (`30s`),
  `MILTER_WAIT_TIMEOUT` (`30` seconds).
- **Background maintenance:** `TLS_RELOAD_INTERVAL_SECONDS` (`86400` — daily
  `postfix reload` to pick up renewed certificates),
  `LOGROTATE_INTERVAL_SECONDS` (`21600` — check `mail.log` rotation every six
  hours; logrotate keeps 14 rotated files on a daily schedule, and each
  rotation triggers `postfix reload`).

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
- **DMARC** — a `_dmarc` TXT record. The panel suggests `p=none` (monitoring
  only, safe to publish immediately). On a send-only relay the sending domain
  often has no inbox, so `rua=` is optional — configure a default report address
  in *Settings* or per domain when you have a mailbox that receives inbound mail
  elsewhere. If `rua=` points at another domain, publish `_report._dmarc` on that
  hub domain too; the panel checks it. Public mail hosts (Gmail, Outlook, …)
  cannot be used as external report destinations.

Skipping any of the three per-domain records is the single most common reason
mail lands in spam even though SelfPost delivered it correctly — DKIM passing
doesn't help if SPF/DMARC are absent. **Whenever you add a new domain in the
panel, add its DNS records at the same time**, not later.

The panel checks both scopes for you and tells you what is actually published:
the *Status* page verifies the server's hostname and its reverse record
(forward-confirmed reverse DNS), and each domain's page shows a *DNS status*
card comparing the published DKIM record against the key this server signs with,
plus the domain's SPF, DMARC, and (when configured) DMARC report-authorisation
records. Results are cached for a few minutes;
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
  Postfix queue summary. The **Machine** card adds the resource usage of the
  host underneath — processor (with the load average), memory and swap, and
  per-interface network throughput and totals — read from the kernel's
  counters; CPU and throughput are measured between refreshes, so they appear
  one refresh after the page opens. A fully busy processor or a machine out of
  memory is a warning here, because both delay or kill the mail path;
  throughput is only reported. The hostname block compares `SELFPOST_HOSTNAME`
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
  by domain and application. A row identifies its message and nothing more —
  time, sender, recipient, subject and status `queued` (accepted, not yet
  delivered), `sent` (handed off successfully), `deferred` (Postfix is retrying),
  `bounced` (final failure), or `rejected` (refused — for example by a level-2
  rate limit); *Details* opens that row's own page
  (`/deliveries/{id}`). That page carries the sending domain, the application it
  was submitted under, the Postfix queue id and the journal id, beside the
  message's history — when it was accepted and what Postfix later reported for
  the recipient — and, under both, the `mail.log` lines for its queue id: the
  connection to the receiving server, the server's reply, and the status that
  reply was filed as. Rows outlive `mail.log`, so an older message's lines may
  have rotated away; the page says so. Retention is controlled by
  `SEND_LOG_RETENTION_DAYS`.
- **Mail queue** (`/mail-queue`) — live view of messages Postfix is still
  trying to deliver or deferring.
- **System log** (`/system-log`) — tail of `/data/log/mail.log` (Postfix and
  related daemon lines). The log rotates daily (14 files kept) with a
  `postfix reload` after each rotation; a background loop checks every six
  hours. It lives in the data volume, so it survives a container recreate along
  with the rest of the state — `./data/log/` on the host — but it is *not*
  included in backups: it is diagnostics, not state.
- **Backup** (`/backup`) — download a full-server backup; the same page hosts
  the domain-import form (`POST /domains/import`). See
  [Backup, restore, and moving a single domain](#backup-restore-and-moving-a-single-domain).
- **Settings** (`/account`) — change the administrator username and/or password.
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

**Container health.** The image declares a Docker `HEALTHCHECK` that probes
`GET /healthz` on port 8080 (unauthenticated). It returns `200 ok` when
OpenDKIM, the panel, and Postfix are all `RUNNING` under supervisord;
otherwise `503 unhealthy`. This catches a dead mail path that would still leave
the HTTP server up, but it does **not** verify TLS certificates, DNS records,
or end-to-end delivery — use the authenticated **Status** page for that. External
monitoring can use the same endpoint through the reverse proxy if you expose it,
or poll `docker inspect` health state on the host.

**First-time setup link.** On first start the one-time setup URL is printed in
the container log (`docker compose logs -f`) and written to `/data/setup-token`
inside the container — `./data/setup-token` on the host, mode `0600` — then
deleted when setup completes. The link is
`https://<SELFPOST_HOSTNAME>/setup/<token>` (path token, not a query string),
valid for ten minutes. If this host ships container logs to a central
aggregator, prefer reading the file:

```sh
docker compose exec selfpost cat /data/setup-token
```

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

Two related but distinct operations
([architecture.md](architecture.md) § Persistence):

- **Full backup** (whole `/data` except `log/`: SQLite, all domains' DKIM keys,
  all applications' SASL credentials, `manifest.json` with the version that
  created it): panel button (*Backup* → *Full backup*), or from the
  host:
  ```sh
  docker exec <container> selfpost-backup > selfpost-backup.tar.gz
  ```
  **Restore** means unpacking that archive into a fresh `/data` bind mount and
  starting a container of the **exact same image version** that created it —
  SelfPost refuses to start otherwise and tells you which tag to use. On the
  first successful start after restore, `manifest.json` from the archive is
  **deleted** — it guards only that one boot, so a later in-place upgrade is
  not blocked. This is why the compose file pins a fixed tag rather than
  `:latest`: without a known version, there'd be no way to tell which image
  restoring a given backup actually requires.

  **Alternative: archive `./data` while stopped.** If the service can be taken
  offline, `docker compose down` then `tar czf selfpost-data.tar.gz ./data` on
  the host is safe — nothing is writing to SQLite. Unlike the panel/CLI backup
  this sweeps in `./data/log/` too, which is Postfix's raw log and usually the
  bulk of the archive; add `--exclude=./data/log` if you only want the state.
  Do **not** tar `./data` while
  the container is running: the database uses WAL mode and a naive copy can
  capture an inconsistent snapshot. The panel/CLI backup remains preferable when
  you cannot afford downtime because it takes a consistent SQLite snapshot via
  the Backup API on a live container.

- **Export/import a single domain** (domain page → *Export domain* to write the
  file, *Backup* → *Import a domain* to read it back in): moves one domain — its DKIM key and its applications' **working**
  SASL passwords — to a different SelfPost instance without regenerating
  anything, so DNS (the DKIM TXT record) doesn't need to change. Unlike a full
  restore, this works across different hostnames/instances.

Both files are **secrets** — they contain the admin password hash (full
backup) or working application credentials (domain export) in the clear or in
directly reversible form. Treat them like any other credential material:
restrict who can read them, don't email them around — and encrypt them, which
SelfPost can do for you.

### Encrypting a backup or export

Both download forms carry an **Encrypt with a password** checkbox. Ticked, the
file that comes down is an encrypted envelope instead of the plain archive:

| Artefact | Plain | Encrypted |
|----------|-------|-----------|
| Full backup | `.tar.gz` | `.spbk` (**S**elf**P**ost **b**ac**k**up) |
| Domain export | `.json` | `.spde` (**S**elf**P**ost **d**omain **e**xport) |

The suffixes are for the operator only — the server detects an encrypted file
by its magic bytes (`SELFPOST1`), not by the extension.

The key is derived from the password with scrypt and the contents are sealed
with AES-256-GCM, in chunks, so a truncated or altered file fails to open rather
than restoring quietly. **SelfPost does not store the password** — lose it and
the file is unrecoverable, which is the entire point.

*Import a domain* takes an encrypted export directly: choose a `.spde` file and
the password field appears (driven by the file extension in the browser; the
server also detects the envelope by its magic bytes). A plain `.json` export
needs no password.

A full backup has to be turned back into a plain archive before it can be
unpacked into `/data`, which the CLI does with the same password:

```sh
docker exec -i <container> selfpost-backup -decrypt < backup.spbk > backup.tar.gz
```

The CLI also *writes* encrypted backups for scripted/cron use. The password
comes from `SELFPOST_BACKUP_PASSWORD` or `-password-file <path>` (first line),
never from a command-line argument, which would be visible in the process list:

```sh
docker exec -e SELFPOST_BACKUP_PASSWORD="$PW" <container> selfpost-backup > backup.spbk
```

With no password set, the CLI keeps writing the plain `.tar.gz` it always has.

## Published ports

`deploy/docker-compose.yml` maps **465** and **587** to the host. Port 465
(smtps) is always active. Port **587** is published even when
`SUBMISSION_ENABLE=false`; nothing listens until you set it to `true` — harmless,
but it can look like an open port in external scans.

## Fixed image tag

`deploy/docker-compose.yml` pins an explicit version (`ghcr.io/mixeme/selfpost:X.Y.Z`),
deliberately never `:latest`. The current pin is `1.1.0`. Intermediate
CHANGELOG sections (`0.2.0`…`0.6.0`) record development cuts from before that
image was published. Pinning matters because of the backup version check above:
the panel binary's embedded version and the image tag that produced it are the
same value by construction (the release CI stamps both from one git tag — see
`.github/workflows/release.yml`), so the pin is what makes "restore into the
same version" a checkable fact rather than a guess. Upgrade by bumping the tag
deliberately, not by riding a moving target.
