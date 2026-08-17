# SelfPost operator guide

Detailed install, configuration, and day-to-day operations. For a short
overview and quick start, see [README.md](../README.md).

This guide has three parts: **[Installation](#installation)** (getting a
container running with a working reverse proxy and TLS), **[Instance
administration](#instance-administration)** (running and maintaining the
SelfPost server itself — status, backups, users, upgrades), and **[Domain
administration](#domain-administration)** (day-to-day work on the sending
domains hosted on that instance — DNS, deliveries, rate limits, applications).

## Table of contents

- [Installation](#installation)
  - [Ports](#ports)
  - [Local trial](#local-trial)
  - [Initial setup](#initial-setup)
  - [Full deployment](#full-deployment)
    - [Fixed image tag](#fixed-image-tag)
  - [Environment variables](#environment-variables)
  - [Reverse proxy (mandatory)](#reverse-proxy-mandatory)
- [Instance administration](#instance-administration)
  - [Status](#status)
  - [Mail queue and System log](#mail-queue-and-system-log)
  - [Settings](#settings)
  - [Users](#users)
  - [Sessions](#sessions)
  - [Upgrading](#upgrading)
  - [Container health](#container-health)
  - [Server-level DNS (PTR/rDNS)](#server-level-dns-ptrrdns)
  - [Rate limiting — level 1 (IP backstop)](#rate-limiting--level-1-ip-backstop)
  - [Full backup and restore](#full-backup-and-restore)
    - [Encrypting a backup or export](#encrypting-a-backup-or-export)
- [Domain administration](#domain-administration)
  - [Domains page](#domains-page)
  - [Domain-level DNS (SPF, DKIM, DMARC)](#domain-level-dns-spf-dkim-dmarc)
  - [IP warmup](#ip-warmup)
  - [Rate limiting — level 2 (domain and application)](#rate-limiting--level-2-domain-and-application)
  - [Deliveries](#deliveries)
  - [Exporting and importing a single domain](#exporting-and-importing-a-single-domain)

## Installation

### Ports

`deploy/docker-compose.yml` maps **465** and **587** to the host. Port 465
(smtps) is always active. Port **587** is published even when
`SUBMISSION_ENABLE=false`; nothing listens until you set it to `true` — harmless,
but it can look like an open port in external scans.

### Local trial

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

### Initial setup

On first start the one-time setup URL is printed in the container log
(`docker compose logs -f`) and written to `/data/setup-token` inside the
container — `./data/setup-token` on the host, mode `0600` — then deleted when
setup completes. The link is `https://<SELFPOST_HOSTNAME>/setup/<token>` (path
token, not a query string), valid for ten minutes. Open it to choose the
administrator username and password — until then the panel has no login. If
this host ships container logs to a central aggregator, prefer reading the
file:

```sh
docker compose exec selfpost cat /data/setup-token
```

### Full deployment

Production layout: one `docker-compose.yml`, a `.env`, persistent `./data`, and
TLS PEM files at `./certs` (read by Postfix on 465/587). The panel is reached
only through a [reverse proxy](#reverse-proxy-mandatory) on 443 — port 8080 is
bound to localhost in the default compose file.

| Artefact | Path |
|---|---|
| Compose file (fixed image tag) | [deploy/docker-compose.yml](../deploy/docker-compose.yml) |
| Environment template | [deploy/.env.example](../deploy/.env.example) |
| Apache vhost (recommended) | [deploy/apache/selfpost-vhost.conf](../deploy/apache/selfpost-vhost.conf) |
| nginx | [deploy/nginx/](../deploy/nginx/) |
| Caddy | [deploy/caddy/](../deploy/caddy/) |
| Traefik | [deploy/traefik/](../deploy/traefik/) |

**1. Fetch the base files.**

```sh
mkdir -p selfpost/data selfpost/certs && cd selfpost
curl -O https://raw.githubusercontent.com/mixeme/selfpost/main/deploy/docker-compose.yml
curl -O https://raw.githubusercontent.com/mixeme/selfpost/main/deploy/.env.example
cp .env.example .env
```

Edit `.env` — at minimum set `SELFPOST_HOSTNAME` to your mail hostname (bare
FQDN, e.g. `mail.example.com`). It must match the PTR record you request from
your provider and the certificate your proxy will obtain. See [Environment
variables](#environment-variables) for the full list.

**2. Reverse proxy and TLS.** Pick and set up one proxy — see [Reverse proxy
(mandatory)](#reverse-proxy-mandatory) for the per-proxy commands. The same
certificate must end up under `./certs` as `fullchain.pem` and `privkey.pem`
so Postfix can serve it on 465 (and 587 if enabled).

**3. Start SelfPost.** If you used Apache on the host (the recommended
option), start only the base compose file from your `selfpost/` directory:

```sh
docker compose up -d
```

The nginx/Caddy/Traefik fragments already include `docker compose up -d` —
skip this if you ran one of those.

**Get the setup URL** — open it in a browser to create the admin account (see
[Initial setup](#initial-setup)):

```sh
docker compose logs selfpost 2>&1 | grep -m1 'http'
```

```sh
cat ./data/setup-token
```

**4. DNS and sending.** Before sending real mail:

1. Confirm PTR/rDNS for the server IP points at `SELFPOST_HOSTNAME` (Status
   page → *Re-check*) — see [Server-level DNS](#server-level-dns-ptrrdns).
2. For each domain you add in the panel, publish SPF, DKIM, and DMARC at the
   same time ([Domain-level DNS](#domain-level-dns-spf-dkim-dmarc)).
3. Warm up a new IP gradually ([IP warmup](#ip-warmup)).

#### Fixed image tag

`deploy/docker-compose.yml` pins an explicit version (`ghcr.io/mixeme/selfpost:X.Y.Z`),
deliberately never `:latest`. The current pin is `1.3.0`. Intermediate
CHANGELOG sections (`0.2.0`…`0.6.0`) record development cuts from before that
image was published. Pinning matters because of the backup version check (see
[Full backup and restore](#full-backup-and-restore)): the panel binary's
embedded version and the image tag that produced it are the same value by
construction (the release CI stamps both from one git tag — see
`.github/workflows/release.yml`), so the pin is what makes "restore into the
same version" a checkable fact rather than a guess. Upgrade by bumping the tag
deliberately, not by riding a moving target — see [Upgrading](#upgrading).

### Environment variables

Copy [deploy/.env.example](../deploy/.env.example) to `.env` next to your
`docker-compose.yml`. The table below lists every variable an operator is
expected to set; defaults match the code exactly.

| Variable | Purpose | Default | Set in |
|---|---|---|---|
| `SELFPOST_HOSTNAME` | Mail-server identity: Postfix HELO/EHLO, SASL realm, certificate CN/SAN, and the hostname the PTR check expects. Bare FQDN only — no scheme or port. | *(required)* | `.env` |
| `SUBMISSION_ENABLE` | When `true`, also listen on port 587 with STARTTLS (RFC 6409 submission) alongside the primary 465/smtps listener. | `false` | `.env` |
| `RATE_LIMIT_MESSAGES_PER_IP` | Level-1 backstop: maximum messages one client IP may submit per window (Postfix `smtpd_client_message_rate_limit`). See [Rate limiting — level 1](#rate-limiting--level-1-ip-backstop). | `100` | `.env` |
| `RATE_LIMIT_WINDOW_SECONDS` | Level-1 window length in seconds (Postfix `anvil_rate_time_unit`). | `3600` | `.env` |
| `SEND_LOG_RETENTION_DAYS` | Days of send-log history kept before the background sweep deletes rows — the main driver of `/data` growth over time. | `90` | `.env` |
| `PANEL_SESSION_IDLE_DAYS` | Sliding idle timeout for the panel login session, in days. There is no absolute cap: an admin who keeps coming back stays signed in indefinitely. | `7` | `.env` |
| `SELFPOST_DNS_RESOLVERS` | Comma-separated recursive resolvers the panel's PTR/SPF/DKIM/DMARC checks query directly (so they report what the internet sees, not what this host's stub resolver synthesises). | `1.1.1.1:53`, `8.8.8.8:53`, `9.9.9.9:53` when unset | `.env` |
| `TRUSTED_PROXY_CIDR` | Comma-separated CIDRs (bare IPs allowed) of reverse proxies allowed to supply `X-Forwarded-For` for login, setup, and account-change rate-limiting. **Leave unset unless you know the exact address of your reverse proxy.** A wrong value lets a client spoof its rate-limit key by sending a forged `X-Forwarded-For` header — the panel trusts the last hop only when the TCP peer matches one of these CIDRs. Behind the default Apache host-network setup this is typically the Docker bridge gateway, e.g. `172.18.0.1`. | *(empty — XFF ignored)* | `.env` |

TLS certificate paths (`TLS_CERT_FILE`, `TLS_KEY_FILE`) are fixed in
[deploy/docker-compose.yml](../deploy/docker-compose.yml) to match the `./certs`
bind mount — configure the mount, not these variables.

The image also reads a number of internal, non-operator env vars (paths,
timeouts, tuning) — not part of this interface; see
[architecture.md § Configuration](architecture.md#configuration) if you need
them.

### Reverse proxy (mandatory)

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

In every case the proxy terminates HTTPS for the panel; the resulting
certificate must end up under `./certs` as `fullchain.pem` and `privkey.pem`.

**Apache (recommended, on the host).** Install Apache with `ssl`, `proxy`, and
`proxy_http` enabled. Copy
[deploy/apache/selfpost-vhost.conf](../deploy/apache/selfpost-vhost.conf) into your
vhost directory, replace `mail.example.com` with your hostname, enable the site,
then issue a certificate:

```sh
sudo certbot --apache -d mail.example.com
```

Point `./certs` at the PEM files certbot wrote (symlink is fine):

```sh
ln -s /etc/letsencrypt/live/mail.example.com certs
```

**nginx (containerised).** From the `deploy/` directory, merge the nginx
fragment and issue the first certificate before nginx can serve HTTPS:

```sh
docker compose -f docker-compose.yml -f nginx/docker-compose.nginx.yml \
  run --rm certbot certonly --webroot -w /var/www/certbot \
  -d mail.example.com --email you@example.com --agree-tos --no-eff-email

docker compose -f docker-compose.yml -f nginx/docker-compose.nginx.yml up -d
```

Edit [deploy/nginx/nginx.conf.example](../deploy/nginx/nginx.conf.example) and
replace `mail.example.com` first. The fragment bind-mounts certbot's output into
both nginx and SelfPost.

**Caddy (containerised, automatic ACME).** Edit
[deploy/caddy/Caddyfile](../deploy/caddy/Caddyfile) and the `<hostname>` placeholders
in [deploy/caddy/docker-compose.caddy.yml](../deploy/caddy/docker-compose.caddy.yml),
then:

```sh
docker compose -f docker-compose.yml -f caddy/docker-compose.caddy.yml up -d
```

Verify Caddy's on-disk cert path for your version before relying on the
default mount — see the comment at the top of the Caddy compose fragment.

**Traefik (containerised).** Edit the `Host(...)` label and ACME email in
[deploy/traefik/docker-compose.traefik.yml](../deploy/traefik/docker-compose.traefik.yml),
start the stack, then extract PEM files for Postfix whenever Traefik issues or
renews a certificate:

```sh
docker compose -f docker-compose.yml -f traefik/docker-compose.traefik.yml up -d
./traefik/extract-cert.sh ./traefik/letsencrypt/acme.json mail.example.com ./traefik/extracted-certs
```

Schedule `extract-cert.sh` (cron or a timer) alongside Traefik's renewals.

## Instance administration

After sign-in the panel opens on **Status** — the place to answer "is the
service healthy and will mail be accepted?"

### Status

`/status` shows supervised processes (Postfix, OpenDKIM, panel), TLS
certificate validity and expiry, milter socket presence, and a short Postfix
queue summary. The **Machine** card adds the resource usage of the host
underneath — processor (core and thread counts), memory and swap, and
per-interface network throughput and totals — read from the kernel's
counters; CPU and throughput are measured between refreshes, so they appear
one refresh after the page opens. A fully busy processor or a machine out of
memory is a warning here, because both delay or kill the mail path;
throughput is only reported. The hostname block compares `SELFPOST_HOSTNAME`
against the PTR record the internet publishes for this server's IP
(forward-confirmed reverse DNS) — see
[Server-level DNS](#server-level-dns-ptrrdns); use *Re-check* after changing
DNS. The **Reload configuration** button re-applies OpenDKIM tables and the
Postfix sender map from the database — use it if daemons drifted from what
the panel shows after manual edits under `/data`.

### Mail queue and System log

- **Mail queue** (`/mail-queue`) — live view of messages Postfix is still
  trying to deliver or deferring. A card at the top states this instance's
  retry policy — first retry delay, later backoff cap, how long a message
  stays in the queue — from `postconf -h`, read once when the panel starts.
  A `postconf -e` override inside the container is visible after the next
  panel (or container) restart. There is no maximum attempt count: Postfix
  retries until the message is delivered or the queue lifetime runs out.
- **System log** (`/system-log`) — tail of `/data/log/mail.log` (Postfix and
  related daemon lines). The log rotates daily (14 files kept) with a
  `postfix reload` after each rotation; a background loop checks every six
  hours. It lives in the data volume, so it survives a container recreate along
  with the rest of the state — `./data/log/` on the host — but it is *not*
  included in backups: it is diagnostics, not state.

### Settings

`/settings` changes the signed-in user's username and/or password. **Global
administrators** also set the panel-wide default DMARC report address (`rua=`)
offered when a domain doesn't set its own — see
[Domain-level DNS](#domain-level-dns-spf-dkim-dmarc). Application SASL logins
are separate and are not changed here.

### Users

`/users` (global administrator only) creates, edits, and deletes panel users.
There are two roles:

- **Global administrator** — full access to every page and every domain,
  including Users, Backup, Status, Mail queue, and System log.
- **Domain-admin** — scoped to one or more domains assigned by a global
  administrator. Sees only those domains' pages, applications, and
  Deliveries rows; cannot add or delete domains. `/users`, `/backup`,
  `/status`, `/mail-queue`, `/system-log`, and `POST /reload` are not
  reachable (404). A domain-admin can *export* the
  domains assigned to them — see
  [Exporting and importing a single domain](#exporting-and-importing-a-single-domain).

The panel refuses to remove or demote the **last** global administrator, so
it can never end up with none.

### Sessions

A login survives a container restart: sessions live in SQLite, not in
memory. Expiry is a sliding idle window (`PANEL_SESSION_IDLE_DAYS`, default
seven days) with no absolute lifetime cap — an admin who keeps using the panel
stays signed in indefinitely. HTMX polling on the monitoring screens
(Deliveries, Mail queue, System log, and the Status health fragment) does
**not** count as activity, so an auto-refreshing tab left open will not keep a
session alive forever. Changing **your own** password on `/settings` signs out
every other session for that user but leaves the current browser signed in.
Signing out (`POST /logout`) ends only the current session — other browsers or
tabs for the same user keep working until their session rows expire.

### Upgrading

Bump the pinned image tag in `docker-compose.yml` to the target release, then
`docker compose up -d`. The backup version check requires the running image
to match the version that created a full backup — see [Fixed image
tag](#fixed-image-tag).

### Container health

The image declares a Docker `HEALTHCHECK` that probes `GET /healthz` on port
8080 (unauthenticated). It returns `200 ok` when OpenDKIM, the panel, and
Postfix are all `RUNNING` under supervisord; otherwise `503 unhealthy`. This
catches a dead mail path that would still leave the HTTP server up, but it
does **not** verify TLS certificates, DNS records, or end-to-end delivery —
use the authenticated [Status](#status) page for that. External monitoring
can use the same endpoint through the reverse proxy if you expose it, or poll
`docker inspect` health state on the host.

### Server-level DNS (PTR/rDNS)

Once, for the machine itself: **PTR/rDNS** for the server's IP, pointing at
its mail hostname. Most receiving mail servers weigh this heavily; get it
from whoever assigns the IP (hosting provider's panel/support), not from
your own DNS zone.

The [Status](#status) page verifies the server's hostname against this
record (forward-confirmed reverse DNS). Results are cached for about one
minute; use *Re-check* right after publishing a record.

Per-domain DNS (SPF, DKIM, DMARC) is a separate scope — see
[Domain-level DNS](#domain-level-dns-spf-dkim-dmarc).

### Rate limiting — level 1 (IP backstop)

SelfPost applies two independent layers of rate limiting; both can refuse a
submission, but only level 2 (domain/application, see
[Domain administration](#rate-limiting--level-2-domain-and-application))
writes a `rejected` row in the send log. Level-2 ceilings set in the panel
cannot exceed level 1 (the panel shows the level-1 values and rejects higher
numbers).

Level 1 is always on, configured via `.env`:

- `RATE_LIMIT_MESSAGES_PER_IP` → Postfix `smtpd_client_message_rate_limit`
- `RATE_LIMIT_WINDOW_SECONDS` → Postfix `anvil_rate_time_unit`

This is an anvil limit per connecting client IP. It keeps working even if the
journal-milter (level 2) is down. There is no per-IP bypass.

### Full backup and restore

**Full backup** is a self-contained project archive: `data/` (SQLite, all
domains' DKIM keys, all applications' SASL credentials, the Postfix queue,
`manifest.json` with the version that created it), plus `docker-compose.yml`,
`.env`, and `certs/` from the operator directory next to `./data`. Delivery
logs under `data/log/` are excluded. The base compose file mounts the project
directory read-only at `/selfpost-deploy` so the panel and CLI can read those
deploy files — without that mount, *Full backup* refuses with an error.

Take a backup from the panel (*Backup* → *Full backup*) or from the host:

```sh
docker exec <container> selfpost-backup > selfpost-backup.tar.gz
```

**Restore** means unpacking that archive into an **empty project directory**
(not into `./data` alone) and starting a container of the **exact same image
version** that created it — SelfPost refuses to start otherwise and tells you
which tag to use. On the first successful start after restore, `data/manifest.json`
from the archive is **deleted** — it guards only that one boot, so a later
in-place upgrade is not blocked. On that same first boot the panel also runs
one **Resync** — OpenDKIM's tables and Postfix's sender map are re-derived from
SQLite and both daemons are reloaded, healing any drift between the extracted
files and the database (the Status page's *Reload configuration* button runs
the same step on demand). This is why the compose file pins a fixed tag rather
than `:latest`: without a known version, there'd be no way to tell which image
restoring a given backup actually requires (see [Fixed image
tag](#fixed-image-tag)).

**Restoring in place** (same host — recovering from data loss, or rolling
back after a bad change):

```sh
# 1. Stop the instance being replaced
docker compose down

# 2. Move the current project aside rather than deleting it
mv . ../selfpost.before-restore
mkdir selfpost && cd selfpost

# 3. Unpack the backup into the fresh directory
tar xzf ../selfpost-backup.tar.gz

# 4. docker-compose.yml in the archive must pin the exact tag the backup was
#    made with — check if unsure:
tar xzf ../selfpost-backup.tar.gz -O data/manifest.json

# 5. Start it and watch the boot
docker compose up -d
docker compose logs -f selfpost
```

A version mismatch at step 5 refuses to start and leaves `/data` untouched —
the panel exits with a message naming the tag to use, e.g.:

```
backup: this backup was created by SelfPost 1.2.3 but this image is 1.3.0 — restore into the matching image (selfpost:1.2.3)
```

Fix the tag in `docker-compose.yml`, `docker compose pull && docker compose up
-d` again — the manifest is still there because the failed boot never got to
delete it.

**Moving to a different host** is the same flow: create an empty project
directory, unpack the backup there, edit `.env` (and `docker-compose.yml` if
needed) for the new hostname or proxy, then `docker compose up -d`. The archive
carries `certs/` from the old host — re-issue certificates when the hostname or
IP changes. Set up the reverse-proxy vhost separately (not in the backup).

**Restoring an encrypted (`.spbk`) backup** needs a running container to
decrypt it first — any container with the `selfpost-backup` CLI works; decryption
does not read `/data` and performs no version check. Start one normally
(step 5, but on an empty project you have not unpacked yet), then:

```sh
docker exec -i <container> selfpost-backup -decrypt < backup.spbk > selfpost-backup.tar.gz
```

Stop it, wipe the project directory again, and continue from step 2 above with
the resulting `.tar.gz` — see [Encrypting a backup or
export](#encrypting-a-backup-or-export) for the decrypt command's password
options.

**Archives from older SelfPost versions** (flat layout: `manifest.json` and
`selfpost.db` at the archive root, no `data/` prefix, no deploy files) restore
with the previous procedure: `tar xzf backup.tar.gz -C ./data` into a project
that already has `docker-compose.yml` and `.env`.

Restoring an archive taken **before** a session row was removed can bring
that session back: session rows travel with the backup, and a browser that
still holds the matching cookie is signed in again on the next request if the
restored row's idle expiry has not passed. `POST /logout` removes only the
current session; there is no "logout everywhere". Changing your own password
on `/settings` deletes your other sessions, but a global administrator
resetting another user's password on `/users` does not invalidate that user's
existing sessions.

**Alternative: archive `./data` while stopped.** If the service can be taken
offline, `docker compose down` then `tar czf selfpost-data.tar.gz ./data` on
the host is safe — nothing is writing to SQLite. Unlike the panel/CLI backup
this sweeps in `./data/log/` too, which is Postfix's raw log and usually the
bulk of the archive; add `--exclude=./data/log` if you only want the state.
Do **not** tar `./data` while the container is running: the database uses
WAL mode and a naive copy can capture an inconsistent snapshot. The
panel/CLI backup remains preferable when you cannot afford downtime because
it takes a consistent SQLite snapshot via the Backup API on a live
container.

See also [Exporting and importing a single
domain](#exporting-and-importing-a-single-domain) — a different, domain-scoped
operation that also lives on the *Backup* page (`/backup`).

Both a full backup and a domain export are **secrets** — they contain the
admin password hash (full backup), TLS private keys and `.env` (full backup),
or working application credentials (domain export) in the clear or in
directly reversible form. Treat them like any other credential material: restrict who can read them, don't email them
around — and encrypt them, which SelfPost can do for you.

#### Encrypting a backup or export

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

## Domain administration

### Domains page

`/domains` lists sending domains and hosts the add-domain form (**global
administrator only**). Domain administrators see only domains assigned to
them. Each row shows its DKIM TXT value, SPF/DMARC checks, and SASL
applications. Per-domain rate limits (level 2) and trusted-IP application
overrides are configured here — see [Rate limiting —
level 2](#rate-limiting--level-2-domain-and-application). *Export domain*
writes a single-domain archive; *Import a domain* on the Backup page reads
one back in (**global administrator only**) — see [Exporting and importing a single
domain](#exporting-and-importing-a-single-domain).

### Domain-level DNS (SPF, DKIM, DMARC)

For *every* sending domain you add in the panel:

- **SPF** — a TXT record on the domain authorizing this server to send on its
  behalf (e.g. `v=spf1 a mx ip4:<server IP> -all`, adjusted to your setup).
- **DKIM** — a TXT record with the exact value the panel shows on that
  domain's page (`domain page → DKIM TXT record`), one selector per domain.
- **DMARC** — a `_dmarc` TXT record. The panel suggests `p=none` (monitoring
  only, safe to publish immediately). On a send-only relay the sending domain
  often has no inbox, so `rua=` is optional — configure a default report address
  in *Settings* (see [Settings](#settings)) or per domain when you have a
  mailbox that receives inbound mail elsewhere. If `rua=` points at another
  domain, publish `_report._dmarc` on that hub domain too; the panel checks
  it. Public mail hosts (Gmail, Outlook, …) cannot be used as external
  report destinations.

Skipping any of the three records is the single most common reason mail
lands in spam even though SelfPost delivered it correctly — DKIM passing
doesn't help if SPF/DMARC are absent. **Whenever you add a new domain in the
panel, add its DNS records at the same time**, not later.

Each domain's page shows a *DNS status* card comparing the published DKIM
record against the key this server signs with, plus the domain's SPF,
DMARC, and (when configured) DMARC report-authorisation records. Results are
cached for a few minutes; use *Re-check* right after publishing a record.
The SPF check is deliberately shallow — it looks for a mechanism that
literally covers this server's address and does not follow `include:` or
`redirect=`, so a record that authorizes the server through an include is
reported as "cannot tell" rather than as a failure.

Server-level DNS (the PTR/rDNS record) is a separate, once-per-machine scope
— see [Server-level DNS](#server-level-dns-ptrrdns).

### IP warmup

A brand-new IP has no sending history, so receiving servers are cautious with
it regardless of how correct your DKIM/SPF/DMARC are. Start with low volume to
a domain, increase gradually over days/weeks rather than sending everything on
day one, and check the IP against major blocklists (Spamhaus and similar)
before and during warmup. This is inherent to how mail reputation works on the
public internet, not something SelfPost's configuration can shortcut.

### Rate limiting — level 2 (domain and application)

Level 2 is optional, configured on each domain's page, and layers on top of
the always-on [level-1 IP backstop](#rate-limiting--level-1-ip-backstop).
Level-2 ceilings cannot exceed level 1 (the panel shows the level-1 values
and rejects higher numbers). When a level-2 ceiling is exceeded, Postfix
returns a 4xx and the refusal is recorded in [Deliveries](#deliveries) as
`rejected`.

**Level 2 — domain** — a message ceiling and window for **every** client IP
sending as that domain. When unset, only level 1 applies for non-privileged
senders.

**Level 2 — application (trusted IPs)** — optional override on an
application: list one or more client IPs and a ceiling **strictly above**
the domain limit (still ≤ level 1). Connections from those IPs use the
application ceiling and skip the domain check. Other IPs stay under the
domain limit (or level 1 alone). An application override without trusted
IPs is inactive.

**Level 2 is best-effort, not a guarantee.** It runs inside the
journal-milter and is deliberately fail-open: if the rate-limit lookup hits
a store error, or the connecting client's IP is not available to the
milter, level 2 is skipped and the message is accepted rather than held up.
Level 1 is the backstop that keeps working even when level 2 cannot run.

### Deliveries

`/deliveries` is a searchable send log with server-side filters by domain
and application. A row identifies its message and nothing more — time,
sender, recipient, subject and status `queued` (accepted, not yet
delivered), `sent` (handed off successfully), `deferred` (Postfix is
retrying), `bounced` (final failure), or `rejected` (refused — for example
by a [level-2 rate limit](#rate-limiting--level-2-domain-and-application));
*Details* opens that row's own page (`/deliveries/{id}`). That page carries
the sending domain, the application it was submitted under, the Postfix
queue id and the journal id, beside the message's history — when it was
accepted and what Postfix later reported for the recipient. A `deferred`
or `bounced` row includes this Postfix's retry intervals (first delay,
backoff cap, queue lifetime), the same numbers Mail queue shows; domain
administrators see them here because they cannot open Mail queue. Under
both sit the `mail.log` lines for its queue id: the connection to the
receiving server, the server's reply, and the status that reply was filed
as. Rows outlive `mail.log`, so an older message's lines may have rotated
away; the page says so. Retention is controlled by
`SEND_LOG_RETENTION_DAYS`.

### Exporting and importing a single domain

Domain page → *Export domain* to write the file, *Backup* → *Import a
domain* to read it back in. This moves one domain — its DKIM key and its
applications' **working** SASL passwords — to a different SelfPost instance
without regenerating anything, so DNS (the DKIM TXT record) doesn't need to
change. Unlike a full restore (see [Full backup and
restore](#full-backup-and-restore)), this works across different
hostnames/instances. *Import* is global-administrator only; *export* is
available to any user who can access the domain, **including a domain-admin**
for a domain assigned to them — so a domain-admin can walk away with that
domain's working SASL passwords in the clear. Weigh that when deciding which
domains to assign to a domain-admin account.

A domain export is a secret in the same way a full backup is, and can be
encrypted the same way — see [Encrypting a backup or
export](#encrypting-a-backup-or-export).
