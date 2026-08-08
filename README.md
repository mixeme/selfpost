<p align="center">
  <img src="docs/assets/selfpost-stamp.svg" alt="SelfPost" width="440">
</p>

# SelfPost

Self-hosted outbound SMTP relay with a web control panel, shipped as a single
Docker image. Postfix, OpenDKIM, and a small Go panel run together under
`supervisord`; you configure domains, DKIM keys, and SASL applications once,
then point your apps at the SMTP endpoint.

SelfPost sends mail straight to the internet from **your own IP**, with per-domain
DKIM signing. It is **outbound only** — no inbound mail, mailboxes, or webmail.

**For:** operators who run their own VPS or home server and want a simple relay
they control, without a third-party SMTP provider.

**Key properties:** one container, multi-domain DKIM, SASL per application,
send log and DNS checks in the panel, encrypted backups.

## Features

- Outbound SMTP (465/smtps; optional 587 submission) with per-domain DKIM signing
- Web panel — domains, applications, deliveries, mail queue, system log, backup
- Multi-domain relay — each SASL application is bound to one sending domain
- DNS status checks (PTR, SPF, DKIM, DMARC) with in-panel re-check
- Two-level rate limiting — IP backstop (Postfix) and per-domain/per-app limits
- Full-server backup and single-domain export/import (optional password encryption)
- Single Docker image; data in a `./data` bind mount

## Documentation

| Document | Contents |
|---|---|
| [**Operator guide**](docs/guide.md) | Reverse proxy, environment variables, DNS, IP warmup, panel operations, rate limiting, backup/restore, ports, image tag |
| [Product boundaries](docs/product.md) | Purpose, deployment assumptions, out-of-scope items, multi-domain model |
| [Architecture](docs/architecture.md) | As-built technical design |
| [Security](docs/security.md) | Accepted security trade-offs and requirements |
| [Development](docs/development.md) | Building, testing, and contributing |
| [CHANGELOG](CHANGELOG.md) | Release history |

Repository: <https://github.com/mixeme/selfpost> — source, issues, releases, and
the `ghcr.io/mixeme/selfpost` image.

## Requirements

Providing these is the operator's job — SelfPost cannot fix a blocked port or a
missing PTR record for you. Details: [Operator guide](docs/guide.md).

### Platform

- Docker + Compose v2 on the host
- A reverse proxy in front of the panel (SelfPost never terminates HTTPS itself)
- Rough sizing: **1 vCPU**, **512 MB–1 GB RAM**, **8–10 GB disk** (send log and
  rotated `mail.log` are the main growth drivers)

### Network and IP

- [ ] Static IP address
- [ ] Outbound TCP port 25 unblocked (many consumer/cloud hosts block it by
      default)
- [ ] PTR/rDNS for that IP pointing at your mail hostname (`SELFPOST_HOSTNAME`)
- [ ] Reasonable starting IP reputation — a fresh IP still needs
      [warmup](docs/guide.md#ip-warmup)

### Per sending domain

For every domain you add in the panel:

- [ ] SPF TXT record authorizing this server
- [ ] DKIM TXT record (value shown on the domain page)
- [ ] DMARC `_dmarc` TXT record

See [DNS setup](docs/guide.md#dns-setup) in the operator guide.

## Quick start

> **First boot — create the admin account.** On a fresh container SelfPost prints
> a **one-time setup URL** (valid ten minutes). Open it in a browser to choose
> the administrator username and password. Until you do, the panel has no login.
> Production deploy: [step 3](#3-start-selfpost).

One container, panel at `http://127.0.0.1:8080` — no reverse proxy, no TLS
files, no compose files. Good for clicking through the UI on your machine;
outbound mail will not reach the real internet without DNS, PTR, and port 25.

```sh
docker run --rm -d --name selfpost-try \
  -p 127.0.0.1:8080:8080 \
  -e SELFPOST_HOSTNAME=mail.local.test \
  -e PANEL_COOKIE_SECURE=false \
  -v selfpost-try-data:/data \
  ghcr.io/mixeme/selfpost:0.1.0
```

**Get the setup URL** (pick one):

```sh
docker logs selfpost-try 2>&1 | grep -m1 'http'
```

```sh
docker exec selfpost-try cat /data/setup-token
```

Open the printed `http://…/setup?token=…` link before it expires.

When finished:

```sh
docker rm -f selfpost-try && docker volume rm selfpost-try-data
```

More detail (limitations, optional throwaway TLS for local SMTP): [Local
trial](docs/guide.md#local-trial) in the operator guide.

## Reference deploy

Production layout: one `docker-compose.yml`, a `.env`, persistent `./data`, and
TLS PEM files at `./certs` (read by Postfix on 465/587). The panel is reached
only through a reverse proxy on 443 — port 8080 is bound to localhost in the
default compose file.

| Artefact | Path |
|---|---|
| Compose file (fixed image tag) | [deploy/docker-compose.yml](deploy/docker-compose.yml) |
| Environment template | [deploy/.env.example](deploy/.env.example) |
| Apache vhost (recommended) | [deploy/apache/selfpost-vhost.conf](deploy/apache/selfpost-vhost.conf) |
| nginx | [deploy/nginx/](deploy/nginx/) |
| Caddy | [deploy/caddy/](deploy/caddy/) |
| Traefik | [deploy/traefik/](deploy/traefik/) |

### 1. Fetch the base files

```sh
mkdir -p selfpost/data selfpost/certs && cd selfpost
curl -O https://raw.githubusercontent.com/mixeme/selfpost/main/deploy/docker-compose.yml
curl -O https://raw.githubusercontent.com/mixeme/selfpost/main/deploy/.env.example
cp .env.example .env
```

Edit `.env` — at minimum set `SELFPOST_HOSTNAME` to your mail hostname (bare
FQDN, e.g. `mail.example.com`). It must match the PTR record you request from
your provider and the certificate your proxy will obtain.

### 2. Reverse proxy and TLS

Pick one proxy. In every case the proxy terminates HTTPS for the panel; the
same certificate must end up under `./certs` as `fullchain.pem` and
`privkey.pem` so Postfix can serve it on 465 (and 587 if enabled). The proxy
must **pass the original `Host` header** — details and rationale:
[Reverse proxy](docs/guide.md#reverse-proxy-mandatory).

**Apache (recommended, on the host).** Install Apache with `ssl`, `proxy`, and
`proxy_http` enabled. Copy
[deploy/apache/selfpost-vhost.conf](deploy/apache/selfpost-vhost.conf) into your
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

Edit [deploy/nginx/nginx.conf.example](deploy/nginx/nginx.conf.example) and
replace `mail.example.com` first. The fragment bind-mounts certbot's output into
both nginx and SelfPost.

**Caddy (containerised, automatic ACME).** Edit
[deploy/caddy/Caddyfile](deploy/caddy/Caddyfile) and the `<hostname>` placeholders
in [deploy/caddy/docker-compose.caddy.yml](deploy/caddy/docker-compose.caddy.yml),
then:

```sh
docker compose -f docker-compose.yml -f caddy/docker-compose.caddy.yml up -d
```

Verify Caddy's on-disk cert path for your version before relying on the
default mount — see the comment at the top of the Caddy compose fragment.

**Traefik (containerised).** Edit the `Host(...)` label and ACME email in
[deploy/traefik/docker-compose.traefik.yml](deploy/traefik/docker-compose.traefik.yml),
start the stack, then extract PEM files for Postfix whenever Traefik issues or
renews a certificate:

```sh
docker compose -f docker-compose.yml -f traefik/docker-compose.traefik.yml up -d
./traefik/extract-cert.sh ./traefik/letsencrypt/acme.json mail.example.com ./traefik/extracted-certs
```

Schedule `extract-cert.sh` (cron or a timer) alongside Traefik's renewals.

### 3. Start SelfPost

If you used Apache on the host (step 2, first option), start only the base
compose file from your `selfpost/` directory:

```sh
docker compose up -d
```

The nginx/Caddy/Traefik fragments from step 2 already include `docker compose up
-d` — skip this if you ran one of those.

**Get the setup URL** — open it in a browser to create the admin account
([first boot](#quick-start)):

```sh
docker compose logs selfpost 2>&1 | grep -m1 'http'
```

```sh
cat ./data/setup-token
```

The file is deleted as soon as setup completes. If logs are shipped to a
central aggregator, prefer `cat ./data/setup-token` so the bearer token does
not enter the log pipeline.

### 4. DNS and sending

Before sending real mail:

1. Confirm PTR/rDNS for the server IP points at `SELFPOST_HOSTNAME` (Status
   page → *Re-check*).
2. For each domain you add in the panel, publish SPF, DKIM, and DMARC at the
   same time ([DNS setup](docs/guide.md#dns-setup)).
3. Warm up a new IP gradually ([IP warmup](docs/guide.md#ip-warmup)).

### Ports and upgrades

The compose file maps **465** (always) and **587** (when
`SUBMISSION_ENABLE=true`). Bump the pinned image tag deliberately when
upgrading — never use `:latest` ([why](docs/guide.md#fixed-image-tag)).

Optional variables (`TRUSTED_PROXY_CIDR`, rate limits, retention): see
[Environment variables](docs/guide.md#environment-variables).

## License

[AGPL-3.0](LICENSE). The AGPL closes the "SaaS loophole": if you run a modified
version as a network-accessible service, you must make the modified source
available to its users — not only when you distribute copies of the code.
