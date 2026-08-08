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

```sh
mkdir -p selfpost && cd selfpost
curl -O https://raw.githubusercontent.com/mixeme/selfpost/main/deploy/docker-compose.yml
curl -O https://raw.githubusercontent.com/mixeme/selfpost/main/deploy/.env.example
mv .env.example .env   # then edit SELFPOST_HOSTNAME etc.
docker compose up -d
```

`SELFPOST_HOSTNAME` is required — bare FQDN only (e.g. `mail.example.com`). It
is the Postfix HELO name, the SASL realm, and must match the PTR record and
certificate CN/SAN.

This starts SelfPost only. You still need a reverse proxy with TLS certificates
bind-mounted at `./certs` — see [Reference deploy](#reference-deploy) and the
[reverse-proxy section](docs/guide.md#reverse-proxy-mandatory) in the operator
guide.

On first start, `docker compose logs -f` prints a one-time setup link to create
the admin account. The same link is also in `./data/setup-token` on the host —
see [Operations → First-time setup link](docs/guide.md#operations).

## Reference deploy

| Artefact | Path |
|---|---|
| Compose file (fixed image tag) | [deploy/docker-compose.yml](deploy/docker-compose.yml) |
| Environment template | [deploy/.env.example](deploy/.env.example) |
| Apache vhost (recommended) | [deploy/apache/selfpost-vhost.conf](deploy/apache/selfpost-vhost.conf) |
| nginx | [deploy/nginx/](deploy/nginx/) |
| Caddy | [deploy/caddy/](deploy/caddy/) |
| Traefik | [deploy/traefik/](deploy/traefik/) |

The compose file maps ports **465** (always) and **587** (when
`SUBMISSION_ENABLE=true`). TLS certificate paths inside the container are fixed
to match the `./certs` bind mount. Bump the pinned image tag deliberately when
upgrading — never use `:latest` ([why](docs/guide.md#fixed-image-tag)).

## License

[AGPL-3.0](LICENSE). The AGPL closes the "SaaS loophole": if you run a modified
version as a network-accessible service, you must make the modified source
available to its users — not only when you distribute copies of the code.
