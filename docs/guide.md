# SelfPost operator guide

Detailed install, configuration, and day-to-day operations. For a short
overview and quick start, see [README.md](../README.md).

This guide has three parts: **[Installation](#installation)** (getting a
container running with a working reverse proxy and TLS), **[Instance
administration](#instance-administration)** (running and maintaining the
SelfPost server itself — the panel's layout, health, backups, users,
upgrades), and **[Domain administration](#domain-administration)** (day-to-day
work on the sending domains hosted on that instance — DNS, the outbound log,
rate limits, applications).

SelfPost 2.0 starts from an empty data directory: a `/data` written by 1.x is
refused at start, not upgraded. A domain moves over with its
[export file](#exporting-and-importing-a-single-domain), and every panel path
changed (see the CHANGELOG, *Removed*).

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
  - [The panel](#the-panel)
  - [Help](#help)
  - [Overview and Health](#overview-and-health)
  - [Queue and System log](#queue-and-system-log)
  - [Account](#account)
  - [Settings](#settings)
  - [Notifications](#notifications)
  - [Users](#users)
  - [Sessions](#sessions)
  - [Upgrading](#upgrading)
  - [Container health](#container-health)
  - [Server-level DNS (PTR/rDNS)](#server-level-dns-ptrrdns)
  - [Rate limiting — level 1 (IP backstop)](#rate-limiting--level-1-ip-backstop)
  - [Full backup and restore](#full-backup-and-restore)
    - [Taking a full backup](#taking-a-full-backup)
    - [Restore](#restore)
    - [Encrypting a backup or export](#encrypting-a-backup-or-export)
  - [Inbound relay](#inbound-relay)
  - [DMARC reports](#dmarc-reports)
- [Domain administration](#domain-administration)
  - [Outbound domains](#outbound-domains)
  - [Domain-level DNS (SPF, DKIM, DMARC)](#domain-level-dns-spf-dkim-dmarc)
  - [IP warmup](#ip-warmup)
  - [Rate limiting — level 2 (domain and application)](#rate-limiting--level-2-domain-and-application)
  - [Outbound log](#outbound-log)
  - [Exporting and importing a single domain](#exporting-and-importing-a-single-domain)

## Installation

### Ports

`deploy/docker-compose.yml` maps **465**, **587**, and **25** to the host. Port
465 (smtps) is always active. Port **587** is published even when
`SUBMISSION_ENABLE=false`; nothing listens until you set it to `true`. Port
**25** is published even when `INBOUND_RELAY_ENABLE=false`; Postfix does not
listen on port 25 until you set **`INBOUND_RELAY_ENABLE=true`** and/or
**`DMARC_REPORTS_ENABLE=true`** (see [Inbound relay](#inbound-relay) and
[DMARC reports](#dmarc-reports)). A scan over IPv4 sees such a port closed -
the connection is refused. Over IPv6 the connection goes through Docker's
proxy (the host runs `docker-proxy` on `::` for every published port), which
accepts it and then closes it, so a scanner may report it open. Measured on a
live server, 2026-10-15, Docker 29.6.1, with a published port nothing
listened on: refused from outside over IPv4 and from the host's public IPv4
address; accepted and closed through `127.0.0.1`, the same proxy. IPv6 itself
was not probed for this.

Since `2.7.0` the image runs Postfix 3.10, whose defaults hold every
listener - 465 and 587 as well as 25 - to two rules of SMTP: a message ends
only at the standard `<CR><LF>.<CR><LF>` (a client that ends DATA with a
bare newline waits for a reply that does not come), and a client that sends
commands ahead of the server's replies where RFC 2920 does not allow it is
answered `554 5.5.0 Error: SMTP protocol synchronization` and disconnected.

Since `2.7.0` a message sent on 465 or 587 does not tell its recipients
where the application runs: the `Received:` header SelfPost adds reads
`Received: by mail.example.com (Postfix) id 4AbC12xyz for <...>; <date>`,
without the application's address, host name or login. To trace a message
someone complains about, find its row in *Outbound › Log* (by domain,
application and day; the message's page shows its queue id); mail.log keeps
the client address on smtpd's `client=` line for the same id.

### Local trial

The [README quick start](../README.md#quick-start) runs a single container
with `PANEL_COOKIE_SECURE=false` and port 8080 published on localhost. No
reverse proxy, no `./certs` bind mount — Postfix still starts, but Overview
and Health will report missing TLS material until you mount PEM files at
`/etc/postfix/tls/fullchain.pem` and `privkey.pem`.

The one-time setup link is always printed as
`https://<SELFPOST_HOSTNAME>/setup/<token>` (and written the same way to
`/data/setup-token`). For a local trial that means rewriting the host and
scheme to `http://127.0.0.1:8080/setup/<token>` — the path token is what
matters; the hostname in the printed URL is not reachable as written.

**What works:** the full panel — setup, domains, applications, outbound log,
queue, system log. **What does not:** reliable outbound delivery to the
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
administrator username and password — until then the panel has no login, and
its sign-in page says there is no administrator yet and points at the setup
link. If this host ships container logs to a central aggregator, prefer reading the
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

With Apache on the host, the panel sees every visitor at the Docker
network's gateway. Tell it that address is your proxy, so that each visitor
has a sign-in limit of their own, and recreate the container:

```sh
docker network inspect "$(basename "$PWD")_default" -f '{{(index .IPAM.Config 0).Gateway}}'
```

Put what it prints into `.env` as `TRUSTED_PROXY_CIDR=` and run `docker
compose up -d` again. Until then the panel's log says, after the first
requests, that every visitor shares one limit and what to set; Server ›
Health shows the same under *Your address*.

**Get the setup URL** — open it in a browser to create the admin account (see
[Initial setup](#initial-setup)):

```sh
docker compose logs selfpost 2>&1 | grep -m1 'http'
```

```sh
cat ./data/setup-token
```

**4. DNS and sending.** Before sending real mail:

1. Confirm PTR/rDNS for the server IP points at `SELFPOST_HOSTNAME` (Server ›
   Health → *Re-check DNS*) — see [Server-level DNS](#server-level-dns-ptrrdns).
2. For each domain you add in the panel, publish SPF, DKIM, and DMARC at the
   same time ([Domain-level DNS](#domain-level-dns-spf-dkim-dmarc)).
3. Warm up a new IP gradually ([IP warmup](#ip-warmup)).

#### Fixed image tag

`deploy/docker-compose.yml` pins an explicit version (`ghcr.io/mixeme/selfpost:X.Y.Z`),
deliberately never `:latest`. The current pin is `2.14.3`. **No 2.x image is
published on ghcr.io yet** (the newest there is `1.3.0`); until one is, build
it from the checkout under the pinned name, and Compose uses the local image
instead of pulling:

```sh
docker build -f build/Dockerfile --build-arg VERSION=2.14.3 \
  -t ghcr.io/mixeme/selfpost:2.14.3 .
```

`VERSION` must equal the tag: it is the version the backup check compares
(see below). Intermediate
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
| `INBOUND_RELAY_ENABLE` | When `true`, accept mail on port 25 for domains configured under *Inbound › Domains* in the panel and forward them to the upstream you set. Off by default — the outbound path is unchanged. See [Inbound relay](#inbound-relay). | `false` | `.env` |
| `DMARC_REPORTS_ENABLE` | When `true`, accept DMARC aggregate reports on port 25 only for report addresses configured in the panel, parse gzip, zip or plain XML, and show summaries under *Outbound › DMARC reports*. Off by default. See [DMARC reports](#dmarc-reports). | `false` | `.env` |
| `DMARC_RATE_LIMIT_MESSAGES_PER_IP` | Per-client-IP cap on port 25 when DMARC ingest is on. When inbound relay is also enabled, **`INBOUND_RATE_LIMIT_MESSAGES_PER_IP`** applies to the shared listener instead. | `20` | `.env` |
| `DMARC_MESSAGE_SIZE_LIMIT` | Maximum report message size in bytes when DMARC ingest is on. | `5242880` (5 MiB) | `.env` |
| `INBOUND_ANTISPAM_MILTER` | Optional milter on the inbound relay listener only (requires `INBOUND_RELAY_ENABLE=true`; not 465/587 or DMARC-only port 25). Empty = off. Format `inet:host:port` or `unix:/path`. Example with [deploy/antispam/docker-compose.antispam.yml](../deploy/antispam/docker-compose.antispam.yml): `inet:antispam:11332`. The panel reads it to report on the *Spam filter* box of the inbound pages and to check that the filter answers (Server › Health). | *(empty)* | `.env` |
| `INBOUND_ANTISPAM_MILTER_ACTION` | What Postfix does if that milter is down: `accept` (fail-open) or `tempfail` (refuse with a temporary error). The *Spam filter* box states which. | `accept` | `.env` |
| `INBOUND_SPAM_LOG_RETENTION_DAYS` | How long the inbound journal keeps a row, 7-365 days; it also never holds more than 10 000 rows. Seeds the setting on the first start with the inbound relay on. | `90` | `.env` |
| `INBOUND_RATE_LIMIT_MESSAGES_PER_IP` | Coarse per-client-IP cap on port 25: how many times one client IP may start a message (`MAIL FROM`) per window, accepted or not (`smtpd_client_message_rate_limit`). Uses the same window as `RATE_LIMIT_WINDOW_SECONDS`. | `20` | `.env` |
| `INBOUND_MESSAGE_SIZE_LIMIT` | Maximum message size in bytes on inbound smtpd (`message_size_limit`). When both inbound relay and DMARC ingest are on, the shared listener uses the **greater** of this and `DMARC_MESSAGE_SIZE_LIMIT`. This is port 25's whole limit; 465 and 587 keep Postfix's own, 10 240 000 bytes. | `26214400` (25 MiB) | `.env` |
| `RATE_LIMIT_MESSAGES_PER_IP` | Level-1 backstop: how many times one client IP may start a message (`MAIL FROM`) on one listener per window, accepted or not; 465 and 587 count separately (Postfix `smtpd_client_message_rate_limit`). See [Rate limiting — level 1](#rate-limiting--level-1-ip-backstop). | `100` | `.env` |
| `RATE_LIMIT_WINDOW_SECONDS` | Level-1 window length in seconds (Postfix `anvil_rate_time_unit`). | `3600` | `.env` |
| `SEND_LOG_RETENTION_DAYS` | Initial default for how many days of send-log history are kept before the background sweep deletes rows, and with them the `mail.log` lines kept for each message (1–2 KB a message) — the main driver of `/data` growth over time and of the size of a full backup. After the first panel start, change retention under **Server › Settings** (global role); the env value is only used to seed SQLite when the setting has never been saved. | `90` | `.env` |
| `PANEL_SESSION_IDLE_DAYS` | Sliding idle timeout for the panel login session, in days. There is no absolute cap: a user who keeps coming back stays signed in indefinitely. | `7` | `.env` |
| `SELFPOST_DNS_RESOLVERS` | Comma-separated recursive resolvers the panel's PTR/SPF/DKIM/DMARC checks query directly (so they report what the internet sees, not what this host's stub resolver synthesises). Each is given 2 seconds to answer before the next is asked, and the one that answered last is asked first; a lookup none of them answered shows as not known, never as a wrong record. | `1.1.1.1:53`, `8.8.8.8:53`, `9.9.9.9:53` when unset | `.env` |
| `TZ` | Time zone of the container, an IANA name such as `Europe/Moscow`: every time the panel shows, every day it counts (today on Health, the days of the statistics, the days of the log filters) and the stamps Postfix writes in `mail.log`, so the two agree. Stored times stay UTC, so changing it is only a restart. *Server › Settings* shows the zone in use, and says so when the image does not know the name (the times are then UTC). | UTC | `.env` |
| `TRUSTED_PROXY_CIDR` | Comma-separated CIDRs (bare IPs allowed) of reverse proxies allowed to supply `X-Forwarded-For` for sign-in and setup rate-limiting (account-change attempts are counted per user, whatever the address). **Leave unset unless you know the exact address of your reverse proxy.** A wrong value lets a client spoof its rate-limit key by sending a forged `X-Forwarded-For` header — the panel trusts the last hop only when the TCP peer matches one of these CIDRs. With Apache on the host (the default), the panel sees the proxy's connections come from the Docker network's gateway, e.g. `172.18.0.1` (`docker network inspect` shows it; measured 2026-10-15: the panel's `127.0.0.1:8080` is published through Docker's proxy, and a connection to such a port from the host arrives from the gateway). | *(empty — XFF ignored)* | `.env` |

`PANEL_COOKIE_SECURE` is not in the table: the session cookie is `Secure`
unless it is `false`, which only a trial over plain HTTP wants, and the
compose file does not pass it - the local trial in the README sets it on
`docker run` (`-e PANEL_COOKIE_SECURE=false`).

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
| **Apache** (default/recommended) | Host disk, via the certbot Apache plugin — `certs/` links to them, no extraction step. | [deploy/apache/selfpost-vhost.conf](../deploy/apache/selfpost-vhost.conf) |
| nginx | Host disk, via a certbot sidecar container — the same shape as Apache. | [deploy/nginx/](../deploy/nginx/) |
| Caddy | Automatic ACME, zero extra containers — simplest, but its on-disk cert path is versioned internal layout, not a stable API; verify it for the Caddy version you run. | [deploy/caddy/](../deploy/caddy/) |
| Traefik | Bundled inside `acme.json` — needs a small extraction script to produce standalone PEM files. | [deploy/traefik/](../deploy/traefik/) |

Apache is the recommended default because the certbot Apache plugin already
keeps `fullchain.pem`/`privkey.pem` at a predictable path with no extra moving
parts between "certificate issued" and "Postfix can read it."

**The proxy needs no security configuration of its own.** The panel emits its
own `Content-Security-Policy`, `Strict-Transport-Security`, `X-Frame-Options`,
`X-Content-Type-Options` and `Referrer-Policy` — deliberately, so the part
that's easy to get wrong lives in the service rather than in a config file
somebody edits under pressure. There is exactly one thing the proxy must do:
**pass the original `Host` header through**. All four fragments above already
do (Apache `ProxyPreserveHost On`, nginx `proxy_set_header Host $host`, Caddy
and Traefik by default). A proxy that rewrites `Host` instead breaks the panel in a
browser that does not send `Sec-Fetch-Site` (Safari before 16.4, or any
browser on a panel served over plain HTTP at a name): there the panel compares
`Origin` with `Host` and refuses every change as cross-origin - the log says
so explicitly, printing the `Sec-Fetch-Site`, `Origin` and `Host` it compared.
A request that carries neither header - a browser too old to send `Origin`,
or a script posting to the panel without one - is refused the same way
since 2.10.4.

In every case the proxy terminates HTTPS for the panel; the resulting
certificate must end up under `./certs` as `fullchain.pem` and `privkey.pem` —
the files, or links that lead to them inside the container.

**Apache (recommended, on the host).** Install Apache with `ssl`, `proxy`,
`proxy_http` and `rewrite` enabled. Copy
[deploy/apache/selfpost-vhost.conf](../deploy/apache/selfpost-vhost.conf) into your
vhost directory, replace `mail.example.com` with your hostname, enable the site,
then issue a certificate:

```sh
sudo certbot --apache -d mail.example.com
```

Link the two files into `./certs` and let the container see what the links
lead to. certbot's `live/` files are themselves relative links into
`archive/` (certbot's documentation, "Where are my certificates?", advises
pointing servers at them or linking to them), so the whole of
`/etc/letsencrypt` is mounted, read-only and at the same path — uncomment that
line in `docker-compose.yml`. Mounting `live/mail.example.com` alone, or
linking `certs` to it, gives the container links into a directory it does not
have: Postfix then starts without a certificate.

```sh
mkdir certs
ln -s /etc/letsencrypt/live/mail.example.com/fullchain.pem certs/fullchain.pem
ln -s /etc/letsencrypt/live/mail.example.com/privkey.pem certs/privkey.pem
```

The panel reads them as its own user (Health shows the certificate, a full
backup carries both files), through the container's group 10997. certbot's
documentation gives the way for a server that is not root, and certbot keeps
the key's group and mode at every renewal:

```sh
sudo chmod 0755 /etc/letsencrypt/live /etc/letsencrypt/archive
sudo chgrp 10997 /etc/letsencrypt/archive/mail.example.com/privkey*.pem
sudo chmod 0640 /etc/letsencrypt/archive/mail.example.com/privkey*.pem
```

**nginx (containerised).** Edit
[deploy/nginx/nginx.conf.example](../deploy/nginx/nginx.conf.example) and
replace `mail.example.com` first. Then, from the `deploy/` directory, issue
the first certificate - before nginx can serve HTTPS, so certbot answers the
challenge itself (`--standalone`), and port 80 must be free while it runs.
The `certbot` service's own entrypoint is its renewal loop, which ignores
any arguments, hence `--entrypoint certbot`:

```sh
docker compose -f docker-compose.yml -f nginx/docker-compose.nginx.yml \
  run --rm -p 80:80 --entrypoint certbot certbot certonly --standalone \
  -d mail.example.com --email you@example.com --agree-tos --no-eff-email
```

The fragment mounts certbot's directory into both nginx and SelfPost, at
`/etc/letsencrypt`; `./certs` holds two links to
`/etc/letsencrypt/live/mail.example.com/` — a path that exists inside the
container, not on the host — and the panel reads the key through group 10997,
as in the Apache case. Then start everything:

```sh
mkdir certs
ln -s /etc/letsencrypt/live/mail.example.com/fullchain.pem certs/fullchain.pem
ln -s /etc/letsencrypt/live/mail.example.com/privkey.pem certs/privkey.pem
sudo chmod 0755 nginx/certbot-etc/live nginx/certbot-etc/archive
sudo chgrp 10997 nginx/certbot-etc/archive/mail.example.com/privkey*.pem
sudo chmod 0640 nginx/certbot-etc/archive/mail.example.com/privkey*.pem

docker compose -f docker-compose.yml -f nginx/docker-compose.nginx.yml up -d
```

Renewals cannot use the standalone method: nginx holds port 80 by then. The
fragment's renewal loop runs `certbot renew --webroot -w /var/www/certbot`,
the directory nginx serves the challenges from; certbot renews with the
options it is given and keeps them for the certificate once a renewal with
them succeeds (certbot's documentation, "Renewing certificates").

**Caddy (containerised, automatic ACME).** Edit
[deploy/caddy/Caddyfile](../deploy/caddy/Caddyfile) and the `<hostname>` placeholders
in [deploy/caddy/docker-compose.caddy.yml](../deploy/caddy/docker-compose.caddy.yml),
then:

```sh
docker compose -f docker-compose.yml -f caddy/docker-compose.caddy.yml up -d
```

Verify Caddy's on-disk cert path for your version before relying on the
default mount — see the comment at the top of the Caddy compose fragment.
With Caddy the certificate lives in Caddy's own data, not in `certs/`, and
Caddy writes its keys readable by itself alone: the panel's full backup then
refuses with "certs/ holds no certificate". Back up `./caddy/caddy-data`
with the project directory instead, or keep a copy of the pair in `certs/`,
the key given to group 10997 (`chgrp 10997`, `chmod 640`), refreshed after
each renewal.

**Traefik (containerised).** Edit the `Host(...)` label and ACME email in
[deploy/traefik/docker-compose.traefik.yml](../deploy/traefik/docker-compose.traefik.yml),
start the stack, then extract PEM files for Postfix whenever Traefik issues or
renews a certificate:

```sh
docker compose -f docker-compose.yml -f traefik/docker-compose.traefik.yml up -d
./traefik/extract-cert.sh ./traefik/letsencrypt/acme.json mail.example.com ./traefik/extracted-certs
```

Schedule `extract-cert.sh` (cron or a timer) alongside Traefik's renewals.

Whatever the proxy, SelfPost notices a renewed certificate in the mount within
a day (`TLS_RELOAD_INTERVAL_SECONDS`) and starts serving it on 465 and 587
without a restart.

## Instance administration

After sign-in the global role opens on **Overview** — the place to answer "is
the service healthy and will mail be accepted?" A user with the *domain* role
opens on the first list they reach: Outbound › Domains, or Inbound › Domains
when that is all they are assigned.

### The panel

The menu is grouped by mail direction, and a path reads like the menu:
`/<group>/<page>`, an entity under its list, an action under the thing it
changes.

| Menu | Pages | Path |
|---|---|---|
| Overview | Overview | `/overview` |
| Outbound | Domains · Log · Queue · DMARC reports | `/outbound/domains`, `/outbound/log`, `/outbound/queue`, `/outbound/dmarc` |
| Inbound | Domains · Log · Filter lists (global role) | `/inbound/domains`, `/inbound/log`, `/inbound/filter-lists` |
| Server | Health · System log · Backup · Users · Settings | `/server/health`, `/server/log`, `/server/backup`, `/server/users`, `/server/settings` |
| User menu (your name) | Account · Help · Sign out | `/account`, `/help`, `POST /logout` |

A page a user cannot reach, or a feature that is off, leaves no gap in the
menu: a group with nothing in it is absent. *Inbound* appears only with
`INBOUND_RELAY_ENABLE=true` and *DMARC reports* only with
`DMARC_REPORTS_ENABLE=true`. Everything under `/server/`, and Overview and the
queue, answer 404 to a user with the *domain* role — the same answer a missing
page gets.

Two pages are outside the menu: `/license`, the AGPL-3.0 text the footer links
to, open to anyone; and `/server/components` (global role), the panel's
component kit - every building block of its pages on one page, a reference
for whoever works on the panel.

Pages that used to be one are now several. The old Status page is **Overview**
(one verdict per check) and **Server › Health** (the tables behind them). The
old Settings page is **Account** (your own name, e-mail and password) and
**Server › Settings** (the instance). A domain has its own page, **Domain
settings** under it, and an **application** form of its own under the domain.

### Help

**Help** (user menu) is a page of topics, one box per section of the panel —
Overview, Outbound, Inbound, Server, Account — and a user is shown the sections
their menu shows. The **?** in the head of a box opens the topic about that box
on the Help page, scrolled to it. The topics explain what a check
or control *means* — kernel counters, TLS on port 465, forward-confirmed PTR,
DNS publish hints, rate limits, DMARC report addresses, the spam filter, and
similar. They are not a second copy of this document: installation,
reverse-proxy setup, backup/restore, environment variables, and security
assumptions stay here in `docs/guide.md`.

### Overview and Health

**Overview** (`/overview`, global role) shows the stamp — the worst of its
checks — with a sentence beside it ("Mail is flowing. One thing needs
attention.", "Mail may not be flowing." when a check fails) and, under it,
each check that is not fine with its value; then one card per check: machine (processor and memory), supervised
processes (Postfix, the panel - whose roles include the DKIM signer - and
their helpers), TLS certificate, queue, milter sockets and reverse DNS. A
check the panel could not run shows as a warning, never as fine. Below the cards are the outbound domain list and, with the inbound relay
on, the inbound one. Each card leads to its table on **Server › Health**
(`/server/health`) — the queue card to the Outbound queue — and Health has the
readings, numbered in the order of what stops mail first:

- **Processes** and **Milter sockets**: the DKIM signer's two - `dkim-signer · applications` for what
  applications send on 465/587, `dkim-signer · notifications` for the
  panel's own mail - the sender check, the send log (the journal-milter),
  with the inbound relay on the Inbound log's (the inbound-journal-milter,
  fail-open like the send log's), and, with one configured, the spam filter. Postfix attaches the signer
  strictly, so either of its rows down means the mail that goes through it
  waits.
- **Sender check** — what the strict check of the `From:` line has cost
  today, in the last 30 days and in the last 12 months: for how long the
  panel did not answer while Postfix was running, how many connections
  Postfix turned away because of it, and how many messages it refused with a
  temporary error in the middle (see [Who an application may send
  as](#who-an-application-may-send-as)). A restart of the whole container is
  not counted: mail was stopped for another reason then. A connection turned
  away is refused before a message is offered, so a monitor that only
  connects during an outage is counted too; a message refused in the middle
  is one the panel stopped answering for after it had begun. A time marked ≈
  is at most what it shows: at that start supervisord could not say since
  when Postfix ran. Days are those of the container's zone (`TZ`). The card warns when
  a connection was turned away or a message refused in the last 24 hours. If these numbers do not stay
  at nothing, the panel is in the way of your mail more than it should be -
  report it.
- **TLS certificate** — validity and expiry, and whether its names cover
  `SELFPOST_HOSTNAME` (the tag beside *Names*); a certificate for another
  name fails, as a client that checks the name refuses it.
- **Hostname and reverse DNS** — compares `SELFPOST_HOSTNAME` against the PTR
  record the internet publishes for this server's IP (forward-confirmed
  reverse DNS), see [Server-level DNS](#server-level-dns-ptrrdns); use
  *Re-check DNS* after changing DNS (twenty re-checks in 15 minutes per user;
past that the page shows the last result). *Greeting* is the `220` line Postfix
  opens every connection with; it must name the hostname, and another name
  there is a warning.
- **Notification mail** — whether the DNS records the panel's own mail is
  signed and authorised with are in place (see [Notifications](#notifications)).
- **Machine** — processor (core and thread counts), memory and swap of the
  host (a memory limit set on the container is not reflected), and
  per-interface network throughput and totals of the container's own network
  interfaces, read from the kernel's counters; CPU and throughput are measured between refreshes, so they appear
  one refresh after the page opens. A fully busy processor or a machine out of
  memory is a warning, because both delay or kill the mail path; throughput is
  only reported.
- **Your address** (in the Configuration card) — the address the panel sees
  your browser at, and whether it took it from `X-Forwarded-For`. Behind a
  reverse proxy that the panel does not trust it is the proxy's own: every
  visitor then shares one sign-in limit, and the card says what to set in
  `TRUSTED_PROXY_CIDR`.
- **Configuration** — the **Reload configuration** button writes again, from
  the database, every file the panel keeps for the mail path - the DKIM
  selector table (`/data/dkim/selectors`), the Postfix sender map, the
  inbound relay maps and the filter lists (with `INBOUND_RELAY_ENABLE=true`),
  the DMARC report maps (with `DMARC_REPORTS_ENABLE=true`) - removes the keys
  of deleted domains, and has Postfix re-read them (a save elsewhere that
  changes no file asks no reload; this button always does); the signer needs no
  reload, it reads the table for every message. This is
  what the first start after a restore does (see [Restore](#restore)). Use it
  if the daemons drifted from what the panel shows after manual edits under
  `/data`. A step that fails is named on the page, and the panel's log says
  why.
- **Test message** — sends one message to an address you type, from
  `postmaster@` the hostname, the way notifications go (through Postfix,
  signed with the key of the server's own name). The page that opens is its
  row in **Outbound › Log**, which follows it to the receiving server: sent,
  put off or refused, with the lines of `mail.log`. It tests the server's
  own path - TLS, its address, the records of its name - not a domain's,
  which only an application's mail does; and whether it reached the inbox
  only the mailbox can say. Ten an hour, global role.

### Queue and System log

- **Outbound › Queue** (`/outbound/queue`, global role) — live view of
  messages Postfix is still trying to deliver, deferring or holding, as
  `postqueue -p` lists them. It only shows: nothing there deletes or forces a message. A box
  states this instance's retry policy — first retry delay, later backoff cap,
  how long a message stays in the queue — from `postconf -h`, read once when
  the panel starts. A `postconf -e` override inside the container is visible
  after the next panel (or container) restart. There is no maximum attempt
  count: Postfix retries until the message is delivered or the queue lifetime
  runs out.
- **Server › System log** (`/server/log`) — tail of `/data/log/mail.log`
  (Postfix and related daemon lines). The log rotates daily (14 files kept)
  with a `postfix reload` after each rotation; a background loop checks every
  six hours. It lives in the data volume, so it survives a container recreate
  along with the rest of the state — `./data/log/` on the host — but it is
  *not* included in backups: it is diagnostics, not state.
- **The container log** (`docker compose logs selfpost`) — what the panel
  itself says, its milters among it; none of it is in `mail.log`. A message
  the DKIM signer cannot sign is not sent unsigned, it waits: Postfix
  answers an application with a temporary error and the application sends
  it again later. Why is a line of the container log that begins `dkim-signer:` and
  ends `- deferring`, e.g. `dkim-signer: no signing key for example.com -
  deferring` (the selector table has no line for the domain: *Reload
  configuration* on Server › Health writes it again from the database) or
  `dkim-signer: no sending domain for login "shop" - deferring`. The sender
  check's lines begin `sender-check:`.

### Account

**Account** (`/account`, every role) is the signed-in user's own: **Profile**
(username and e-mail) and **Password**. Changing the username or the
notifications switch does not ask for the password; changing the e-mail does,
and so does changing the password, both on the limit of a sign-in's guesses
(ten in 15 minutes per user). The e-mail is the user's own address: it is where the panel
sends [notifications](#notifications) while *Send me notifications* is ticked
beside it, and what a domain's *Use my e-mail* button fills in (see
[Domain-level DNS](#domain-level-dns-spf-dkim-dmarc)). Application SASL logins
are separate and are not changed here.

**A confirmed e-mail and a forgotten password.** A new address - saved under
Account, or typed for a user by the global role on the user form - is sent a
link that confirms it (`https://<SELFPOST_HOSTNAME>/confirm/…`), which works
once, for 24 hours, opened in a browser signed in as that account; its page
has a button, and only the button confirms. The mail does not name the
account, so a mistyped address cannot be confirmed by whoever reads it.
Account tags the address *confirmed* or *not confirmed* and, while it is not,
offers *Send the link again* (at most five links an hour to one address).
Notifications go to the address either way. Only a confirmed address can be
sent a reset link: *Forgot your password?* on the sign-in page asks for the
username and always answers the same - whether the name exists, and whether
its address is confirmed, is not said; another link is not sent within ten
minutes of the last, and after five in a day the owner is mailed once that
further requests are ignored for 24 hours - and the link
(`https://<SELFPOST_HOSTNAME>/login/reset/…`) works once, for 30 minutes, and
only while the account's address is still the one it was sent to. Setting the
new password signs every session of the account out. The links name the
server by `SELFPOST_HOSTNAME`, so the panel must be served at that name. An
account without a confirmed address recovers as before: the global role sets
a new password on the user form, and the only global user from the container
(see [Users](#users)).

### Settings

**Server › Settings** (`/server/settings`, global role) holds what is true for
the whole instance: how many days the **Outbound log** keeps its rows before the
background sweep deletes them (7–365 days; takes effect on the next six-hour
prune cycle without a container restart), and, read-only, the level-1 rate
limit that comes from `.env` ([Rate limiting — level 1](#rate-limiting--level-1-ip-backstop)).
There is no panel-wide DMARC report address: each domain has its own. The page
also has the switch for [notifications](#notifications) and the two DNS records
that make them arrive, with when they were last checked; **Re-check DNS** at
the top of the page checks them again once they are published (results are
otherwise kept a few minutes).

The last box, **Time zone**, names the zone the panel is in: `TZ` in `.env`
([Environment variables](#environment-variables)), UTC when it is not set,
with its offset from UTC. Every time the panel shows is in that zone, and no
time on the screens says which - this box is where it is said. Every day it
counts is that zone's too: *today* on Health, the days of the statistics,
the days of the log filters. Postfix runs in the same zone, so the stamps of
the `mail.log` lines on Server › System log and on a message's page read the
same clock as the rest of the page. The database keeps its times in UTC, so
changing the zone is a restart and nothing more; the days counted before it
stay days of the old zone, a shift of a few hours in the statistics. A name
the image does not know leaves everything in UTC, and the box says so. One
thing keeps UTC on purpose: the day a DMARC report covers is its reporter's,
who counts days in UTC, so the lists and a report's title name that day; the
report's page gives its window's times in this zone and says which day it is.

### Notifications

The panel writes to its users when something needs them. Four things are
reported, each once:

| What | When a message is sent | A second message |
|---|---|---|
| A sending domain's DKIM, SPF or DMARC record stops matching | after two hourly checks in a row found it wrong. A resolver that did not answer is not a mismatch; a missing DMARC record is advice on the domain's page, not a notification | when two checks in a row find the records right again |
| A level-2 rate limit refuses mail of a sending domain's applications | within a minute of the first refusal | at most once a day for the same domain, while it goes on |
| The TLS certificate is about to expire | 14 days before | 3 days before, if it is still the same certificate |
| A message has waited in the queue for more than three days | at the next hourly check | when no message that old is left |

A user with the **global** role is told all of it; a user with the **domain**
role is told about the DNS records and rate limits of the outbound domains
assigned to them. A message goes to the e-mail on the user's
[Account](#account) page and only while *Send me notifications* is ticked
there; a user without an e-mail gets none. **Server › Settings** has one switch
that turns notifications off for everyone. With it off nothing is checked, so
what is wrong when it is turned on again is reported then. The first check runs
three minutes after the panel starts, then every hour; the panel sends at
most 60 notifications an hour, a ceiling nothing should come near.

Notifications are the panel's own mail. They are sent from
`postmaster@<SELFPOST_HOSTNAME>`, one message per recipient, are not rows of
the Outbound log and count against no rate limit. They name the page of the
panel to open and carry no link: the panel does not know the address its
reverse proxy serves it under.

**Two DNS records make them arrive.** The server's own name is not one of your
sending domains, so it needs records of its own, shown with the values to
publish under **Server › Settings › Notification mail**:

- a **DKIM** key for the hostname, which the panel generates and signs
  notifications with (`<selector>._domainkey.<hostname>`, TXT);
- an **SPF** record for the hostname: `v=spf1 a -all` (TXT).

**Server › Health** shows whether each is in place in the *Notification mail*
card. Without them notifications are still sent, and a receiver may file them
as spam or, if the parent domain publishes a strict DMARC policy, refuse them.
Mail of your sending domains depends on neither. If the hostname is itself one
of your sending domains, the DKIM record is that domain's and is already
published - and whoever administers that domain can then send mail that
looks like the panel's; a hostname that is nobody's sending domain is the
better choice.

The e-mail on Account may be at a public mail host (Gmail and the like); only
a domain's DMARC report address may not.

A relay that cannot send cannot say so: none of the four is "the relay is
down". Watch that from outside ([Container health](#container-health)).

### Users

**Server › Users** (`/server/users`, global role only) creates, edits, and
deletes panel users. Everyone in the panel is an administrator, so the two
roles name a *reach*, not a rank, and are called `global` and `domain` in the
list, the role select and the page headers:

- **global** — every page and every domain: Overview, Server (Health, System
  log, Backup, Users, Settings), the Outbound queue, and every outbound and
  inbound domain. The only role that adds or deletes a domain and that manages
  users.
- **domain** — only the domains assigned to the user, outbound and inbound
  **separately**: the user form has one list for each, and the same name on
  both lists is two assignments. Each list has **All**, which includes domains
  added later; with it ticked the rows below are ignored. A *domain* user needs
  at least one domain, or *All*, on either list. A group with nothing assigned
  is absent from that user's menu.

What the *domain* role reaches inside what is assigned:

| | `global` | `domain` |
|---|---|---|
| Overview, Server, the Outbound queue | yes | no (404) |
| Outbound: domain page, applications, Domain settings, log | every domain | assigned domains |
| Outbound › DMARC reports | every report, with the ingest statistics | reports of assigned domains, without the ingest box |
| Inbound: domain page, upstream, recipients, log | every domain | assigned inbound domains |
| Inbound › Filter lists | yes | no (404) |
| Add or delete a domain, either direction | yes | no |
| Export a domain (Domain settings) | yes | assigned domains |
| Import a domain (Server › Backup) | yes | no |
| Account, Help | yes | yes |

A domain that is not assigned, one that does not exist, and a user with no
reach in that direction all get the same 404.

The panel refuses to remove or demote the **last** global user, so it can never
end up with none. A user cannot delete the account they are signed in with.
The user form also sets the user's e-mail, which the users list shows.

Adding a user, setting a user's password, changing their e-mail or role -
your own account's included - and deleting a user ask for **your account
password**, the one you sign in with, as a backup does and under the same
limit on wrong guesses: a session left open is not enough to hand out an
account. Saving only the name or the domain assignments is not asked.

### Sessions

A login survives a container restart: sessions live in SQLite, not in
memory. A session belongs to the user it was created for, by id: deleting the
user ends their sessions, and renaming them keeps theirs. Expiry is a sliding
idle window (`PANEL_SESSION_IDLE_DAYS`, default seven days) with no absolute
lifetime cap — a user who keeps using the panel stays signed in indefinitely.
HTMX polling on the monitoring screens (Overview, Health, Outbound log and
queue, System log) does **not** count as activity, so an auto-refreshing tab
left open will not keep a session alive forever.

- Changing **your own** password on **Account** signs out every other session
  of yours but leaves the current browser signed in.
- A password that a global user sets for **another** user on the user form
  ends all of that user's sessions; set for oneself, it behaves like the
  previous item.
- Signing out (`POST /logout`) ends only the current session — other browsers or
  tabs for the same user keep working until their session rows expire.

### Upgrading

Bump the pinned image tag in `docker-compose.yml` to the target release, build
that tag as under [Fixed image tag](#fixed-image-tag) while no 2.x image is
published, then `docker compose up -d`. The backup version check requires the running image
to match the version that created a full backup — see [Fixed image
tag](#fixed-image-tag).

**Base-package fixes come with releases.** The image is Debian trixie, and a
security update of Postfix, Cyrus SASL, OpenSSL or Berkeley DB reaches you with
the next SelfPost release built after it: a published image tag is never
rebuilt. The project checks every week whether one of those has moved and cuts
a patch release when it has; a release whose notes say "base packages" is one
of those.

**To 2.8.0** there is nothing to do. From 2.8.0 the panel signs outgoing
mail itself, in a milter of its own, and OpenDKIM is no longer in the image;
the keys, the selectors and the DNS records stay as they are. The keys move
from `data/opendkim/keys` to `data/dkim/keys`: on the first start the
entrypoint copies them, as the panel's user, into `data/dkim.new`, renamed
into place when the copy is whole, and says so in the container log
(`entrypoint: the DKIM keys were copied from /data/opendkim to /data/dkim;
/data/opendkim is left for a rollback`). A copy that fails stops the start -
the panel would otherwise give the server's own name a new key, and its
published record would no longer match. `data/dkim` is the panel's alone
(directories 0700, files 0600); so is `data/opendkim`, which is left as it
was, for a rollback, until the next release, and which a full backup leaves
out. The selector table, `data/dkim/selectors`, is written from the database
at every start, before the signer serves.

**To 2.9.0** from 2.8.x there is nothing to do: the first start removes
`data/opendkim`, the key tree 2.7.x read, which 2.8.x kept for a rollback,
and says so (`entrypoint: removed /data/opendkim, the key tree of 2.7.x;
the keys are under /data/dkim`); the keys under `data/dkim` stay as they
are. From 2.7.x, start 2.8.x once first: an image of 2.9.0 on a `data`
whose keys are under `data/opendkim` only stops at the start and says so,
rather than give the server's own name a new key. Rolling back from 2.9.x
to 2.8.x needs nothing; to 2.7.x, the old tree is gone - only a full backup
of 2.7.x brings it back.

**Rolling back to 2.7.x** from 2.8.x (only): the 2.7.x image signs with OpenDKIM
from `data/opendkim`, which holds the keys as they were at the upgrade. A
domain added under 2.8.x has no key there, and 2.7.x sends its mail
unsigned; one deleted and added again under 2.8.x has its old key there,
which no longer matches its published record. Postfix's queue under
`data/postfix/queue` belongs to Postfix's users, which have other numbers
in the two images (postfix 100 in 2.8.0, 101 in 2.7.x); a 2.7.x start
reads the queue before it hands it over and stops at `postfix check`
("scan_dir_push: open directory defer: Permission denied"). With the
instance stopped, hand the queue over in the 2.7.x image itself, by name,
before its first start (the image is the one you roll back to):

```bash
docker run --rm -v ./data:/data --entrypoint sh ghcr.io/mixeme/selfpost:2.7.0 -c 'chown -R postfix:postfix /data/postfix/queue && chgrp postdrop /data/postfix/queue/public /data/postfix/queue/maildrop && chown root /data/postfix/queue /data/postfix/queue/pid'
```

(Measured on the test server, 2026-10-16, on a copy of its queue: 2.7.x's
`postsuper` failed before and passed after.) Going forward again needs
nothing for the queue - 2.8.0 hands it over itself. A domain added under
2.7.x has its key only in `data/opendkim/keys/<domain>/`, and the
entrypoint copies only while `data/dkim` holds no key at all: with the
instance stopped, copy that directory into `data/dkim/keys/` (the start
makes it the panel's). Removing `data/dkim` instead has the old tree copied
again whole and loses the keys of domains added under 2.8.x.

### Container health

The image declares a Docker `HEALTHCHECK` that probes `GET /healthz` on port
8080 (unauthenticated, and answered only to a request that did not come
through a reverse proxy - no `X-Forwarded-For` - from loopback or a private
address; anyone else gets `404`). It returns `200 ok` when the panel - whose roles
include the DKIM signer - and Postfix are both `RUNNING` under supervisord;
otherwise `503 unhealthy`. This
catches a dead mail path that would still leave the HTTP server up, but it
does **not** verify TLS certificates, DNS records, or end-to-end delivery —
use the authenticated [Overview and Health](#overview-and-health) pages for
that. External monitoring runs on the host - `curl -fsS
http://127.0.0.1:8080/healthz` - or polls `docker inspect` health state;
through the reverse proxy the endpoint is not found, so its answer tells a
stranger nothing.

### Server-level DNS (PTR/rDNS)

Once, for the machine itself: **PTR/rDNS** for the server's IP, pointing at
its mail hostname. Most receiving mail servers weigh this heavily; get it
from whoever assigns the IP (hosting provider's panel/support), not from
your own DNS zone.

**Server › Health** verifies the server's hostname against this record
(forward-confirmed reverse DNS), and Overview shows the verdict. Results are
cached for about one minute; use *Re-check DNS* right after publishing a
record.

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

This is an anvil limit per client IP and listener. It keeps working even if the
journal-milter (level 2) is down. There is no per-IP bypass except the
container's own loopback.

**The client IP is the real one over IPv4 only.** Docker's port publishing
keeps an IPv4 client's address, but the compose network has no IPv6, so a
client that reaches the server over IPv6 is carried by Docker's proxy and
arrives from the network's gateway (`172.18.0.1` by default) - and so does a
connection from the host to `127.0.0.1`. All such clients share one level-1
budget, an application's client IP list cannot name them, and the spam
filter sees the gateway. Measured on a live server, 2026-10-15, Docker
29.6.1: an outside IPv4 client and the host's own public IPv4 address logged
as themselves, the host's IPv6 address and `127.0.0.1` as `172.18.0.1`.

### Full backup and restore

Both downloads - the full backup on **Server › Backup** and a domain's
export on its settings - ask for your account password, the one you sign in
with, every time: a session left open is not enough to take them. Wrong
guesses count against the same limit as a password change (ten in fifteen
minutes).

**Full backup** is a self-contained project archive: `data/` (SQLite, all
domains' DKIM keys under `data/dkim/`, all applications' SASL credentials,
`manifest.json` with the version that created it), plus `docker-compose.yml`,
`.env`, and `certs/` from the operator directory next to `./data`. Delivery
logs under `data/log/` are excluded, and so is the Postfix queue: the archive is taken while the
instance runs, and a copy of a queue Postfix is working on can hold a message
twice or not at all. Mail still waiting for delivery is therefore not carried
over, and a restored instance starts with an empty queue; to keep the queue,
stop the instance and use `tar` (methods 3 and 4 below). The base compose file mounts the project
directory read-only at `/selfpost-deploy` so the panel and CLI can read those
deploy files — without that mount, *Full backup* refuses with an error.

#### Taking a full backup

There are four ways to capture a full backup. They differ in whether the
instance keeps running, what goes into the archive, and what happens on restore.

**1. Panel — while the container is running (no downtime).** *Server › Backup*
→ *Full backup*. *Encrypt with a password* is ticked; untick it for a plain
archive. Same archive as the CLI below. The panel runs as its own user, not as
root, so **the private key in `certs/` has to be readable by it**: give the
file to the container's `backupkey` group, which has the fixed id 10997 and
holds the panel alone - not Postfix, whose daemons face the internet (until
`2.14.4` the guide said group 10998, `selfpost`, which Postfix is in; a key
given to it then is to be given to 10997 now) —

```sh
chgrp 10997 certs/privkey.pem && chmod 640 certs/privkey.pem
```

— and have whatever renews the certificate do the same; certbot does it by
itself, keeping the group and mode of a key on renewal (with `certs/` as links
into `/etc/letsencrypt`, the commands of "Reverse proxy" above give certbot's
own files the group). A key readable by root alone is enough for Postfix, but
then the panel refuses to make a backup and names the file: it does not hand
over an archive without the key. Links under `certs/` are followed: the
archive holds the files they lead to. A `certs/` that is there with no file
in it is refused for the same reason; what a backup leaves out - a link to
a directory under `certs/`, a link under `data/` - it names in a warning (the
CLI's standard error, the panel's log).

**2. `selfpost-backup` CLI — while the container is running (no downtime).**
The panel button and this command call the same code path: a consistent SQLite
snapshot via `VACUUM INTO`, then a gzip tar of `data/` (minus diagnostics) plus
deploy files. `docker exec` runs the command as root, so it reads the private
key whatever its permissions.

```sh
docker exec <container> selfpost-backup > selfpost-backup.tar.gz
```

Use `-o /path/inside/container` to write inside the container instead of
stdout; the file is written beside that name and takes it only when it is
whole, so a backup that fails leaves no half-written file and whatever had
the name before. A database that is not where `SELFPOST_DB_PATH` points is
refused, rather than archived empty. Optional encryption: set
`SELFPOST_BACKUP_PASSWORD` or pass `-password-file` — see [Encrypting a
backup or export](#encrypting-a-backup-or-export).

**3. `tar` of the whole project directory — with the service stopped.** Stop
the instance so nothing is writing to SQLite, then archive the operator
directory (the folder that contains `docker-compose.yml`, `.env`, `data/`, and
usually `certs/`):

```sh
docker compose down
tar czf ../selfpost-backup.tar.gz .
docker compose up -d
```

Postfix delivery logs under `data/log/` are included and are often the bulk of
the archive; add `--exclude='./data/log'` if you only want state. Unlike the
panel/CLI backup, this archive has no `data/manifest.json`, so restore does not
run the version guard or the one-time post-restore *Resync* (see
[Restore](#restore) below) — a normal `docker compose up` is enough when the
files were captured cleanly while stopped.

**4. `tar` of `./data` only — with the service stopped.** Same as (3), but only
the data volume:

```sh
docker compose down
tar czf ../selfpost-data.tar.gz ./data
docker compose up -d
```

This is **not** self-contained: you must keep `docker-compose.yml`, `.env`,
and `certs/` separately (another backup, or unchanged on the same host).
Restore is `tar xzf selfpost-data.tar.gz` into an existing project directory,
not into an empty one. Also includes `data/log/` unless you exclude it.

**Do not `tar` while the container is running.** SQLite uses WAL mode; copying
or archiving `./data` (or the whole project directory) while the panel and
Postfix are writing can produce an inconsistent database. The panel and CLI
backup avoid this by snapshotting the database on a live instance; the `tar`
methods avoid it by stopping first.

| | Panel / `selfpost-backup` | `tar` project dir (stopped) | `tar` `./data` only (stopped) |
|--|---------------------------|------------------------------|-------------------------------|
| Downtime | None | Yes (`docker compose down`) | Yes |
| Self-contained archive | Yes | Yes | No — deploy files separate |
| SQLite consistency | `VACUUM INTO` snapshot | Safe when stopped | Safe when stopped |
| `data/log/` | Excluded | Included (optional exclude) | Included (optional exclude) |
| Postfix queue | No | Yes | Yes |
| `certs/` private key | Yes (the panel needs it readable by group 10997; links are followed) | Yes when `certs/` holds the files; links are stored as links unless `tar -h` | No — deploy files separate |
| `data/manifest.json` | Written (version stamp) | Absent | Absent |
| Restore version check | Yes — must match image tag | No | No |
| Post-restore *Resync* | Yes, on first boot | No | No |
| Password encryption (`.spbk`) | Yes | No — encrypt the `.tar.gz` yourself if needed | No |

Prefer the panel or CLI when you cannot afford downtime. Prefer a stopped `tar`
of the whole project directory when you already plan maintenance and want a
plain archive without going through the container. Use `tar` of `./data` only
when deploy files are managed elsewhere and never change.

#### Restore

**Panel and CLI archives** (with `data/manifest.json`) restore by unpacking into
an **empty project directory** (not into `./data` alone) and starting a
container of the **exact same image version** that created the backup — SelfPost
refuses to start otherwise and tells you which tag to use. On the first
successful start after restore, `data/manifest.json` from the archive is
**deleted** — it guards only that one boot, so a later in-place upgrade is not
blocked. On that same first boot the panel also runs one **Resync** — the DKIM
selector table and Postfix's sender map are re-derived from SQLite (inbound relay maps
when `INBOUND_RELAY_ENABLE=true`; DMARC maps when `DMARC_REPORTS_ENABLE=true`)
and Postfix is reloaded, healing any drift between the extracted files and
the database. The manifest is deleted only after that Resync is done: a first
start that fails on the way leaves it, and the next start is a restore again
and runs the Resync. The *Reload configuration* button on Server › Health runs the same Resync.
This is why the
compose file pins a fixed tag rather than `:latest`: without a known version,
there'd be no way to tell which image restoring a given backup actually requires
(see [Fixed image tag](#fixed-image-tag)).

**Stopped-`tar` archives** (methods 3 and 4 above) have no `manifest.json`, so
there is no version guard and no automatic post-restore *Resync*. Unpack a
self-contained project archive into an empty directory (or replace `./data` in
place for a data-only archive) and `docker compose up -d`. Use an image tag
compatible with the data on disk; when in doubt, match the tag that was running
when the archive was taken.

**Restoring in place** (same host — recovering from data loss, or rolling
back after a bad change):

```sh
# 1. Stop the instance being replaced
docker compose down

# 2. Move the current project aside rather than deleting it
cd .. && mv selfpost selfpost.before-restore
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
backup: this backup was created by SelfPost 2.14.2 but this image is 2.14.3 — restore into the matching image (selfpost:2.14.2)
```

Fix the tag in `docker-compose.yml` (build that tag as under [Fixed image
tag](#fixed-image-tag) if it is not published), then `docker compose up -d`
again — the manifest is still there because the failed boot never got to
delete it.

An archive unpacked **in part** — a download cut short, or a `tar` that
stopped at a damaged block (it says so and exits non-zero) — is refused at
step 5 as well, before the database is opened:

```
backup: the archive was not unpacked whole (data/contents.json, its last entry, is not there) - it was cut short or is damaged; nothing was opened. Unpack it again, from an intact copy, into an empty directory
```

The last entry of every panel and CLI archive, `data/contents.json`, records
each file under `data/` with its size and SHA-256, and the first start
compares `/data` with it; a file that differs is named in the message.
`docker-compose.yml`, `.env` and `certs/` are not compared — they are yours to
edit before that start. A journal file beside the database
(`selfpost.db-wal`, `-shm`, `-journal`) is refused too: no archive holds one,
so it is left from what the directory held before, and SQLite would take it
up. To unpack again, unpack into an **empty** directory.

The files are compared once: a first start that gets past the comparison
and then fails (its Resync, say) leaves the manifest, and the next
`docker compose up -d` runs the Resync without comparing again - that start
had the database open, so its files are no longer the archive's.

**Moving to a different host** is the same flow: create an empty project
directory, unpack the backup there, edit `.env` (and `docker-compose.yml` if
needed) for the new hostname or proxy, then `docker compose up -d`. The archive
carries `certs/` from the old host — re-issue certificates when the hostname
changes; a new IP needs only DNS (the A record and the PTR). Set up the reverse-proxy vhost separately (not in the backup).

The inbound log and the filter lists are in the database and so in the
backup, and so are the list files under `data/antispam/`; the panel writes
those files again from the database on the first start after a restore. What
the backup does not hold is the spam filter's own side: the `antispam/`
directory with the compose fragment and the rspamd configuration. Like the
reverse-proxy fragments it is part of the installation, not of its state -
copy it from [deploy/antispam/](../deploy/antispam/) again, or the restored
instance runs with no filter.

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

**Archives and data directories from 1.x cannot be restored into 2.0.** The
database is refused at start and left untouched — 2.0 starts from an empty data
directory. Carry each domain over with its
[export file](#exporting-and-importing-a-single-domain) instead.

Restoring an archive taken **before** a session row was removed can bring
that session back: session rows travel with the backup, and a browser that
still holds the matching cookie is signed in again on the next request if the
restored row's idle expiry has not passed. `POST /logout` removes only the
current session; there is no "logout everywhere" — see
[Sessions](#sessions) for what ends a user's sessions.

See also [Exporting and importing a single
domain](#exporting-and-importing-a-single-domain) — a different, domain-scoped
operation: *Export domain* is on the domain's settings page, *Import a domain*
on *Server › Backup* (`/server/backup`).

Both a full backup and a domain export are **secrets** — they contain the
password hashes of the panel's users (full backup), TLS private keys and `.env`
(full backup), or working application credentials (domain export) in the clear
or in directly reversible form. Treat them like any other credential material:
restrict who can read them, don't email them around — and encrypt them, which
SelfPost does unless you untick the box.

#### Encrypting a backup or export

Both download forms carry an **Encrypt with a password** checkbox, ticked by
default. Ticked, the file that comes down is an encrypted envelope instead of
the plain archive; unticked, it is the plain archive:

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
needs no password. An import is refused, and says why, for a DKIM key outside
1024-4096 bits, more than 500 applications, a domain named as the server
itself (its key signs the panel's own mail), and a report address on the
server's name other than the domain's own hosted one.

A full backup has to be turned back into a plain archive before it can be
unpacked into `/data`, which the CLI does with the same password:

```sh
docker exec -i <container> selfpost-backup -decrypt < backup.spbk > backup.tar.gz
```

The password is checked on the file's first block, before the command
writes anything - but the shell's `>` has already emptied `backup.tar.gz`
before the command starts, so a wrong password leaves it empty; `-o` keeps
a file that was there. A file cut short fails at its end, with the command's
exit status non-zero, after what came before has been written: an archive
cut so is refused at the restore when it lost any file (see
[Restore](#restore) - the record is the archive's last entry). With `-o` the
output takes its name only when it is whole; a command killed outright
leaves a `.NAME.tmp-*` file beside it, readable by its owner alone, to
remove by hand.

The CLI also *writes* encrypted backups for scripted/cron use. The password
must be at least 12 characters, as on the panel's forms (decrypting takes
any), and it comes from `SELFPOST_BACKUP_PASSWORD` or `-password-file <path>`
(first line), never from a command-line argument, which would be visible in
the process list. Its other flags: `-o <file>` writes the output to a file
instead of standard output, `-i <file>` reads the encrypted archive from a
file instead of standard input (with `-decrypt`), and `-version` prints the
version.
`-e` with the name only takes the value from your shell's environment, so it is
not written on the command line:

```sh
SELFPOST_BACKUP_PASSWORD="$PW" docker exec -e SELFPOST_BACKUP_PASSWORD <container> selfpost-backup > backup.spbk
```

With no password set, the CLI keeps writing the plain `.tar.gz` it always has.

### Inbound relay

Optional backup-MX / forwarder: Postfix accepts mail on port **25** for
domains you list under *Inbound › Domains* and hands each message to the
upstream host you configure. It is **not** mailboxes, IMAP, or webmail —
SelfPost never stores the message locally.

**Off by default.** Set `INBOUND_RELAY_ENABLE=true` in `.env` and recreate the
container. Until then there is no inbound-relay `smtp inet` listener, no
*Inbound* group in the menu, and `/inbound/…` is 404. (Port 25 can still
listen when only `DMARC_REPORTS_ENABLE=true` — see [DMARC
reports](#dmarc-reports).) Outbound 465/587 is unchanged. Port 25 ends a
message only at the standard `<CR><LF>.<CR><LF>` (the SMTP-smuggling
defence, since `2.6.1`); a sending server that ends DATA with a bare
newline waits for a reply that does not come, and there is no allow-list
for such a sender.

**Panel** (`/inbound/domains`): the global role adds and deletes domains. On a
domain's page, set the upstream host/port and TLS to that hop (opportunistic /
required / off), and choose recipients. *Required* is Postfix's `encrypt`
level: the hop is encrypted, but the upstream's certificate is not checked,
so it guards against someone who listens on the way, not against one who
answers in the upstream's place. An upstream has one TLS mode: Postfix
keeps the TLS policy per next hop, not per domain, so the mode saved for one
domain is the mode of every domain forwarded to the same host and port, and
they are changed with it; a *domain* user who does not manage all of them is
refused, and told the mode in use. Recipients are an allow-list, or any address at that
domain. Inbound domains are assigned to a user with the *domain* role
separately from outbound ones (see [Users](#users)); such a user sees and
changes the upstream and recipients of the inbound domains assigned to them,
and cannot add or delete one. The upstream may be any host and port, including
loopback and private addresses — see the accepted risks in
[security.md](security.md). A domain with an empty upstream is kept in the
database but is **not** published into Postfix maps, so mail is never accepted
with nowhere to send it.

**DNS.** Unlike sending domains, an inbound domain needs an **MX** record that
points at `SELFPOST_HOSTNAME`. The domain page shows the value to publish
(`10 <hostname>.`) and a check that succeeds when *any* MX host matches this
server — other MX targets (a primary mail server) are fine; this is how
backup-MX is meant to work. When SelfPost is the backup of another MX, give it
a larger number than the primary's: MX records of equal preference are tried
in no fixed order, so the mail would be shared, not backed up. Use *Re-check DNS* after publishing.

**Not an open relay.** The inbound smtpd offers no SASL. It accepts only
domains in `relay_domains` and only listed recipients (`relay_recipient_maps`);
everything else is `reject_unauth_destination` / `reject_unlisted_recipient`.
Prefer an explicit recipient list so unknown addresses are refused at RCPT
and never generate a bounce (backscatter).

**Anti-spam.** SelfPost does not ship a filter. To attach one, set
`INBOUND_ANTISPAM_MILTER` (requires inbound relay enabled; not on DMARC-only
port 25) and merge
[deploy/antispam/docker-compose.antispam.yml](../deploy/antispam/docker-compose.antispam.yml)
the same way as the nginx/Caddy fragments:

```sh
docker compose -f docker-compose.yml -f antispam/docker-compose.antispam.yml up -d
```

The milter sees the real client IP (over IPv4; see [Rate limiting — level
1](#rate-limiting--level-1-ip-backstop) for IPv6), HELO and PTR — unlike the
upstream, which only sees SelfPost. Default action is fail-open (`accept`) so a down sidecar
does not block backup-MX; set `INBOUND_ANTISPAM_MILTER_ACTION=tempfail` to
refuse with a temporary error instead, so the sending server tries again.

The fragment mounts one file into the sidecar,
[deploy/antispam/rspamd/milter_headers.conf](../deploy/antispam/rspamd/milter_headers.conf):
it makes rspamd write its verdict - the action and the score with its symbols
- into two headers of every message it lets through. SelfPost records them
with the message, and the upstream receives them with the mail. Copy the
`antispam/rspamd/` directory next to the fragment; without the file accepted
mail is still recorded, with no verdict.

The same file makes rspamd **sign** each message, in a third header,
`X-SelfPost-Filter`. The filter is attached so that mail still flows when it
is down, and then a sender could write a verdict of his own into those
headers; SelfPost therefore records a verdict only next to a signature it can
recompute. The key is `data/antispam/filter.key`: SelfPost creates it on its
first start with the inbound relay on, and the sidecar reads it from the
directory it already mounts - there is nothing to configure. If Inbound › Log
shows *selfpost* for mail the filter did judge, the sidecar is running without
this file: copy `antispam/rspamd/` again and restart the sidecar. Nothing of
rspamd's needs clearing, then or after a change of the lists or of the key: it
rereads the list files by itself and reads the key anew for every message. A
restart is needed only for a change of its own configuration - the files of
`antispam/rspamd/`.

The third file there, [worker-proxy.inc](../deploy/antispam/rspamd/worker-proxy.inc),
keeps rspamd's scanner and its controller - whose default password is public -
on the sidecar's own loopback and puts only the milter on the compose network;
the fragment runs rspamd with no Linux capability. Upgrading SelfPost to
`2.14.4` brings rspamd 4.2.2 with it: copy `antispam/` again, then
`docker compose ... up -d` recreates the sidecar.

The same directory holds
[multimap.conf](../deploy/antispam/rspamd/multimap.conf), the rules for the
**allow and deny lists** kept in the panel. SelfPost does not apply the lists
itself: it writes them as six files under `data/antispam/` (`allow_ip.map`,
`allow_from.map`, `allow_domain.map` and the three `deny_` ones), the fragment
mounts that directory into the sidecar, and rspamd rereads a changed file within
about a minute - no restart. An entry is a sender address, a sender
domain (that domain, not its subdomains) or a client address or network. A
denied sender is refused outright. An allowed one is given a weight that no
spam score outweighs, so it is accepted - unless a deny entry matches the same
message: **deny wins**, whichever entries they are. **A sender address or
domain on the allow list is not proof of anything**: it is the envelope
sender, which the sending server states and nobody checks, so whoever knows an
allowed address can send as it. Allow a client address or network where you
can. Only the envelope sender is looked at: a bounce, whose envelope sender
is empty, matches no sender entry, whatever its `From:` line says. The files are readable by
anyone on the host; they hold addresses, not secrets.

In the panel the lists are **Inbound › Filter lists** (global role), and what
port 25 decided about each message is **Inbound › Log**: accepted mail with the
filter's verdict, refused mail with the reply it was given. For accepted mail
its *Upstream* column says what the upstream made of it - *queued* (not tried
yet), *held* (in the queue by the filter's verdict, until released), *sent*,
*deferred* (a greylisting, an upstream that cannot be reached: Postfix tries
again) or *bounced* (refused for good, or expired in the queue; the sender
was sent a bounce) - and opens the last reply and the host that gave it. Rows logged before 2.10.0 have no upstream result. A row of the log
leads to the lists with its sender filled in. Beside each entry of a list is
how often it decided a message, counted from the log - so over the days the
log keeps - and *never* marks an entry that may have outlived its use. The
count depends on two things in
[multimap.conf](../deploy/antispam/rspamd/multimap.conf): the message of the
deny rules and the names of the allow symbols; change them and every entry
reads *never*. A user with the *domain* role
sees the log of the inbound domains assigned to them, and not the lists. How
long the log keeps a row is set under **Server › Settings**.

The panel only reports this setting; it does not change it. Every inbound
domain page has a **Spam filter** box — on or off, what happens to mail while
the filter is down, and that the filter belongs to the server, not to the
domain — and the domain list says in its head whether the filter is on. The
filter's address (`INBOUND_ANTISPAM_MILTER`) is shown to the global role only,
so a user with the *domain* role sees the state without the server's internals.
For the global role the filter is also one of the milter sockets on **Server ›
Health** and in the Overview card: the panel connects to the address (one
second at most, nothing is sent) and, if there is no answer, says whether
inbound mail is being refused with a temporary error (`tempfail`) or going through unfiltered
(`accept`).

**Inbound mail is not in the Outbound log.** Port 25 runs its own milter chain:
the optional anti-spam milter and the panel's inbound journal (Inbound › Log,
fail-open), not the signer, the sender check or the outbound journal. The DKIM signer does not sign relayed
mail (and a signing outage therefore cannot hold it up), and the journal-milter
does not file it — the [Outbound log](#outbound-log) stays a record of what
*you* sent.
This also keeps a forged `From:` on inbound mail from counting against a
sending domain's [level-2 rate limit](#rate-limiting--level-2-domain-and-application).

Inbound configuration lives in SQLite and `/data/postfix/` map files, so it
is included in a [full backup](#full-backup-and-restore). Single-domain
export/import is sending domains only.

### DMARC reports

Optional ingest of DMARC **aggregate** reports (`rua=`): Postfix accepts mail on
port **25** only for addresses you configure, pipes each message to the panel
ingest worker, and stores parsed summaries in SQLite. Forensic reports (`ruf=`)
are not stored. This is separate from [Inbound relay](#inbound-relay) — no
backup-MX, no forwarding upstream.

**Off by default.** Set `DMARC_REPORTS_ENABLE=true` in `.env` and recreate the
container. Until then there is no report ingest, no *DMARC reports* entry in
the Outbound menu, and `/outbound/dmarc` is 404. Outbound 465/587 is unchanged.

**Addresses.** A domain's report address (`rua=`) is one of three choices, set
in that domain's **Domain settings** (Outbound › Domains › the domain › Domain
settings) and nowhere else:

- **No reports** — the state of a new domain; `rua=` is left out of the DMARC
  record.
- **SelfPost hosted** — `dmarc-reports+<domain>@<hostname>`, an address on this
  server whose reports are parsed and shown under *DMARC reports*. Offered only
  while `DMARC_REPORTS_ENABLE=true`.
- **A specific address** — any mailbox, typed for this domain; it receives the
  raw XML. Not one on `SELFPOST_HOSTNAME`: to have this server receive the
  reports, choose *SelfPost hosted*. Beside the field, *Use my e-mail* fills it with the e-mail of your
  profile (Account); it only fills the field, and saving is the form's own
  button. The button is absent when your profile has no e-mail.

Changing the choice changes the DMARC record to publish; the domain page shows
the new value. There is no panel-wide or per-user default. Only addresses on
`SELFPOST_HOSTNAME` that a domain has chosen are accepted on port 25 - the
hosted address `dmarc-reports+<domain>@<hostname>` of a domain set to
*SelfPost hosted* - and a report is kept only when it is for the domain the
address is tagged with. There is no mailbox for the server as a whole.

**DNS.** Publish the usual `_dmarc` TXT on each sending domain with
`rua=mailto:…` pointing at the address the domain shows. Receivers deliver to the
address's domain: to its MX, or, without one, to its A record -
`SELFPOST_HOSTNAME` has that already; a report address on another domain needs
that domain's **MX** to point here. If a hub domain
authorises external destinations, publish `_report._dmarc` there too; the panel
checks it on the domain page.

**Panel.** *Outbound › DMARC reports* lists recent reports. The global role also
sees the *Ingest* box — whether ingest works, the last report received, reports
kept this week and parse failures. Open a domain's roll-up from the list or
from *DMARC reports* in the domain page's menu. A user with the *domain* role
sees the reports of the domains assigned to them, without the *Ingest* box.
Summaries are pruned (500 kept for each domain, 90 days max). A report's
page is `/outbound/dmarc/reports/<id>` and a domain's roll-up
`/outbound/dmarc/domains/<id>`. A report's day - in the lists and its title -
is the reporter's, counted in UTC as reports count their days; its page shows
the window's times in the panel's zone, with a line naming that day.

A message that is not a report the relay keeps — unreadable as an aggregate
report, about a domain not sent from here, or delivered to another domain's
report address — is dropped without a retry and without a bounce. It adds one
to *parse failures*, and the reason is on the message's delivery line in the mail log
(*Server › System log*, `dropped: …`).

**Not an open relay.** The inbound smtpd offers no SASL. With DMARC ingest
alone, `check_recipient_access` permits only configured report addresses;
everything else is rejected. With inbound relay enabled too, both allow-lists
apply.

**Reports are checked before they are stored.** The address form is
predictable, so the report XML is treated as untrusted input: a report is
accepted only when the domain it claims in `policy_published` is a sending
domain configured here, and — when it arrived at a per-domain hosted address —
only when that domain matches the address's `+tag`. Anything else is refused
and counted in *parse failures* on the DMARC reports page, so a stranger cannot file
reports under your domain or push genuine ones out through the retention cap.
Reports are accepted as gzip, zip, or plain XML.

Parsed report data lives in SQLite and is included in a
[full backup](#full-backup-and-restore).

## Domain administration

### Outbound domains

**Outbound › Domains** (`/outbound/domains`) lists sending domains with the
verdict of each DNS check, the DKIM selector, the number of applications and
recent mail, and hosts the add-domain form (**global role only**). A user with
the *domain* role sees only the domains assigned to them. Three pages belong
to a domain:

- **The domain page** (`/outbound/domains/{id}`) — the *DNS records* to
  publish and what is in DNS now, the *Applications* (SASL logins) with *New
  password* and *Delete* in their rows, and the *Connection* settings. *Re-check
  DNS* and *Add application* are in its head; its menu leads to the domain's
  log and DMARC reports.
- **The application form** (`…/applications/new`, `…/applications/{aid}`) —
  who the application may send as, its client IP allow-list and its rate limit
  are validated and saved together, or not at all, also when the application
  is created. A password is shown once, on the page that answers the creation
  or *New password*, and is never shown again. A new login is stored in lower
  case - Postfix does not tell `Ops` from `ops`, so the panel does not let two
  applications be told apart by case - and the page that shows the password
  shows the login as it was stored.
- **Domain settings** (`…/settings`) — the *DMARC report address* (see [DMARC
  reports](#dmarc-reports)), the domain's *Rate limit* (see [Rate limiting —
  level 2](#rate-limiting--level-2-domain-and-application)), *Export domain*
  and, for the global role, *Delete domain*, which asks once more.

#### Who an application may send as

An application sends as any address of its domain, or only as the addresses
listed for it. Since `2.4.0` that rule covers both senders a message has:

- the **envelope sender** (`MAIL FROM`), which Postfix checks;
- the **`From:` line** of the message - the one a mail program shows and the
  one DMARC holds the DKIM signature's domain to - which the panel checks.

An address with an extension counts as the address: an application listed as
`invoices@…` may also send as `invoices+march@…`, in the envelope and in
`From:` alike.

An address listed for an application is that application's alone: another
application of the same domain, set to any address, may not send as it -
Postfix asks who owns the address before it asks who owns the domain.

A message whose `From:` is anything else is refused with `550 5.7.1` and a
text naming the line: another of your domains, the server's own name, an
address at a domain that is not on this server, a line with two addresses,
or no `From:` at all. The line must be one address as the mail standard
(RFC 5322) writes it - the address a mail program shows - and only that
address counts: what a display name or a comment around it says is not
judged, and a line with more than one address is refused. A display name
with a comma or brackets has to be quoted (`"Shop, Inc." <orders@…>`), as
the standard asks and mail libraries do; unquoted, the line does not read
as one address and is refused. The address is held to the application's
list the way Postfix holds the envelope sender, a quoted local part as the
same address unquoted. The refusal
is permanent, so the application does not send the message again; it is a
row of the [Outbound log](#outbound-log) tagged `wrong From`, and the row's
page shows what the line said.

Since `2.8.0` a **`Sender:` line** - the one a mail program shows beside
`From:` as "on behalf of" - is held to the same rule when the message has
one: one address, one the application may send as, and no more than one
such line. Otherwise the message is refused the same way, `550 5.7.1
Sender: …`, tagged `wrong Sender`. Most mail libraries write no `Sender:`;
one that writes the application's own address passes. A system that writes
for people outside your domains - a ticket system, a CRM - puts its own
address in `From:` with the person's name (`"Jane via Support"
<support@…>`) and the person's address in `Reply-To:`: their domain in
`From:` would fail DMARC at the receiver anyway.

What this stops working when you upgrade from an earlier version:

- an application that sends with the `From:` of another of your domains -
  give that domain an application of its own;
- an application with a list whose envelope address and `From:` address
  differ - add the second address to the list;
- a contact form that puts its visitor's address into `From:` - put it into
  `Reply-To:` and send from an address of your own, which is also what
  DMARC requires;
- (`2.8.0`) a message with a `Sender:` of another domain, or of an address
  the application is not listed for - drop the line or write the
  application's own address there.

The check is strict: while the panel does not answer, Postfix answers with a
temporary error (`451`, and `454` to the login): an application that queues
its mail sends it again later, one that sends directly gets an error to retry;
nothing is accepted unchecked. **Server › Health** counts how often
that happens.

Since `2.8.0` the check also guards the signature: the panel signs an
application's message with the key of the domain its login belongs to, and
DMARC counts that signature only when its domain aligns with the domain of
`From:`. Without the check an application could send under this server's
signature as another of its domains, or as the server itself.

**Lines of more than 998 bytes.** The signer signs the message as the
application handed it over. Postfix's SMTP client breaks a line longer
than 998 bytes, in the body or in a header, when it delivers the message (`smtp_line_length_limit`),
after the signature was made, and the receiver's check of the signature
then fails. Such a message is still sent; the container log names its
domain and login (`dkim-signer: example.com (login "shop"): a body line
longer than 998 bytes - ...`, or `a line of the Subject header is longer
than 998 bytes - ...`). Mail libraries keep lines short with
quoted-printable or base64; an application that writes its own body has to
as well. A message whose header section is over 1 MiB is refused,
`552 5.3.4`.

*Export domain* writes a single-domain file; *Import a domain* on *Server ›
Backup* reads one back in (**global role only**) — see [Exporting and importing
a single domain](#exporting-and-importing-a-single-domain).

### Domain-level DNS (SPF, DKIM, DMARC)

For *every* sending domain you add in the panel (outbound). An inbound
forwarding domain is a different object — it needs an MX, not these TXT
records; see [Inbound relay](#inbound-relay).

- **SPF** — a TXT record on the domain authorizing this server to send on its
  behalf (e.g. `v=spf1 a mx ip4:<server IP> -all`, adjusted to your setup).
- **DKIM** — a TXT record with the exact value the panel shows on that
  domain's page (*DNS records* box), one selector per domain.
- **DMARC** — a `_dmarc` TXT record. The panel suggests `p=none` (monitoring
  only, safe to publish immediately). Set `rua=` to receive aggregate reports
  by choosing the domain's report address in *Domain settings*: with [DMARC
  reports](#dmarc-reports) enabled, the SelfPost-hosted address; otherwise an
  address typed for the domain — a mailbox elsewhere that receives mail (*Use
  my e-mail* fills in your profile's) — or no reports, which leaves `rua=` out.
  The record the page shows follows that choice. If `rua=` points at another
  domain, publish `_report._dmarc` on that hub domain too; the panel checks
  it. Public mail hosts (Gmail, Outlook, …) cannot be used as external
  report destinations.

Publish all three: receivers weigh SPF, DKIM and DMARC together, and large
receivers require them for bulk senders - a domain without them is a common
way for mail SelfPost delivered correctly to land in spam. **Whenever you add a new domain in the
panel, add its DNS records at the same time**, not later.

Each domain's page has a *DNS records* box comparing the published DKIM
record against the key this server signs with, plus the domain's SPF,
DMARC, and (when configured) DMARC report-authorisation records. Results are
cached for a few minutes; use *Re-check DNS* right after publishing a record.
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

Level 2 is optional, configured in each domain's settings (the domain) and
on each application's form (the application), and layers on top of
the always-on [level-1 IP backstop](#rate-limiting--level-1-ip-backstop).
Level-2 ceilings cannot exceed level 1 (the panel shows the level-1 values
and rejects higher numbers). When a level-2 ceiling is exceeded, Postfix
returns a 4xx and the refusal is recorded in the [Outbound log](#outbound-log)
as `rejected`.

**Level 2 — domain** — a message ceiling and window for **every** client IP
sending as that domain. When unset, only level 1 applies for non-privileged
senders.

**Client IP allow-list (application)** — optional restriction on an
application: when enabled, list one or more client IPs that may **submit** mail
as that application; a session from any other address is refused with a 4xx at
`MAIL FROM`. When disabled, any client IP is allowed. This is independent of
rate limits, and it takes exact addresses only — CIDR ranges are not parsed.
An application that connects over IPv6 is seen from the Docker network's
gateway, not its own address (see [Rate limiting — level
1](#rate-limiting--level-1-ip-backstop)).

It is **not an authentication boundary.** The SASL login itself still succeeds;
the check runs in the journal-milter and is fail-open like level 2 (below):
if that milter does not answer, or its lookup hits a store error, the
restriction is skipped rather than enforced. A message whose client address
Postfix did not pass is refused, not let through. Use it to narrow where an application may send from, not to contain a
leaked password — for that, regenerate the password.

**Level 2 — application** — optional override of the domain limit for one
application (≤ level 1). The ceiling may be **higher or lower** than the domain
limit. When active, it replaces the domain limit for that login; when unset,
the domain limit (or level 1 alone) applies.

**Manual and auto mode** — each domain and application limit can be
**Manual** (you set the ceiling and window) or **Auto**. Auto derives
`max_messages` from sending statistics: `ceil(average msg/h × multiplier)`
over the level-1 window (`RATE_LIMIT_WINDOW_SECONDS`), capped at level 1.
The panel shows 30-day statistics (total, peak and average msg/h) on the
domain page; level-1 refusals are not in the send log, so totals
under-count strict IP limits. When retention is below 30 days, statistics
use `min(30, retention)` days. With no traffic in the window, auto stays
inactive until messages are sent. Auto limits are recalculated every six
hours and on demand via **Recalculate now**.

**Level 2 is best-effort, not a guarantee.** It runs inside the
journal-milter and is deliberately fail-open: if the rate-limit lookup hits
a store error, level 2 is skipped and the message is accepted rather than
held up. The client address plays no part: a ceiling is on a domain or an
application, wherever its mail comes from.
The same applies if the journal-milter itself does not answer — Postfix is
configured to accept rather than refuse it when it cannot reach it. (Since
`2.4.0` a panel that is down altogether makes Postfix refuse mail with a
temporary error anyway, through the
[sender check](#who-an-application-may-send-as) of the same process.) Level 1
is the backstop that keeps working even when level 2 cannot run, and it is
the only one of the two that does not depend on the panel process.

### Outbound log

**Outbound › Log** (`/outbound/log`) is the send log, with server-side
filters by domain, application, status and day (from and to, days of the container's zone, as on
the Inbound log); the status *rejected* is the four kinds of refusal
together, which the table tells apart by their tags. A user with the *domain*
role sees only the rows of their domains. The filters are kept by the pages,
the refresh and the way back from a message, and the log refreshes its rows
without resetting a filter being chosen. A row identifies its message and nothing more — time,
sender, recipient, subject and status `queued` (accepted, not yet
delivered), `sent` (handed off successfully), `deferred` (Postfix is
retrying), `bounced` (final failure: refused for good by the receiving server, or given
up when the queue lifetime ran out), or refused before it was queued.
`bounced` is usually Postfix's word - it returns a message it gave up on to
the sender, as it does one refused for good; it is the panel's when Postfix no longer holds a message and `mail.log` never
said what became of it (the log rotated
away while the panel was down, or was not readable) - the panel then closes
the row as a failure rather than show a delivery it cannot evidence, and
counts that it did. A
refused row says why in its tag: `rate-limited` (a [level-2 rate
limit](#rate-limiting--level-2-domain-and-application)), `wrong client` (the
application's client IP list), `wrong From` or `wrong Sender` (see [Who an
application may send as](#who-an-application-may-send-as)); a row refused before `2.4.0`
says `rejected`. A refused message's page shows what Postfix wrote about the
refusal: for `wrong From` and `wrong Sender` the lines of the queue id the
message had, for the other two the one `NOQUEUE` line; a row refused before `2.5.1` says the line
was not kept, and one the panel wrote from `mail.log` after a restart - it
had stopped before writing the refusal itself - says so;
*Details* opens that row's own page (`/outbound/log/{id}`). That page carries
the sending domain, the application it was submitted under, the Postfix
queue id and the journal id, beside the message's history — when it was
accepted and what Postfix later reported for the recipient. A `deferred`
row includes this Postfix's retry intervals (first delay, backoff cap, queue
lifetime), a `bounced` one the queue lifetime, the same numbers the Outbound
queue shows; users
with the *domain* role see them here because they cannot open the queue. Under
both sit the `mail.log` lines for its queue id: the connection to the
receiving server, the server's reply, and the status that reply was filed
as. The lines are kept in the database with the row, so they stay on the
page after `mail.log` has rotated. They go at the next six-hourly prune after
the row is deleted by retention, and not while another row carries the same
queue id (Postfix reuses ids); a
message accepted before `2.2.0` shows them only while they are still in the
current file, and the page says so when there are none. Retention is set under **Server › Settings** (global
role; with the inbound relay on, the Inbound log's has a field of its own there); `SEND_LOG_RETENTION_DAYS` in `.env` is only the initial default until it
is changed there.

### Exporting and importing a single domain

Domain settings → *Export domain* to write the file, *Server › Backup* →
*Import a domain* to read it back in. This moves one domain — its DKIM key, its
applications' **working** SASL passwords, and configured **rate limits**
(mode, ceilings, multipliers) and client IP allow-lists — to a different SelfPost
instance without regenerating anything, so DNS (the DKIM TXT record)
doesn't need to change. Unlike a full restore (see [Full backup and
restore](#full-backup-and-restore)), this works across different
hostnames/instances. The file is checked like the forms before anything is
created - the report address, every rate limit, every client address - and
refused with the form's own message, naming the field and the application.
An application login keeps the case it had (new logins are created in lower
case), and one that differs from a login of this server only in case is
refused. *Import* is for the global role only; *export* is
available to any user who can access the domain, **including a user with the
*domain* role** for a domain assigned to them — so that user can walk away with
the domain's working SASL passwords, in the clear if they untick the encryption
box. Weigh that when deciding which domains to assign to a *domain* user.

A domain export is a secret in the same way a full backup is. The *Encrypt with
a password* box is ticked by default, and the export is sealed the same way —
see [Encrypting a backup or export](#encrypting-a-backup-or-export).
