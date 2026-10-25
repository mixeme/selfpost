<p align="center">
  <img src="docs/assets/selfpost-stamp.svg" alt="SelfPost" width="440">
</p>

# SelfPost

Self-hosted SMTP relay with a web panel — one Docker image, no provider.

Postfix and a small Go panel, which also signs outgoing mail with DKIM, run
together under `supervisord`; you configure domains, DKIM keys, and SASL
applications once, then point your apps at the SMTP endpoint.

SelfPost sends mail straight to the internet from **your own IP**, with
per-domain DKIM signing. No mailboxes or webmail. An optional inbound relay
(backup-MX / forwarder on port 25) forwards to an upstream; it does not store
mail.

**For:** operators who run their own VPS or home server and want a simple relay
they control, without a third-party SMTP provider.

**Key properties:** one container, multi-domain DKIM, SASL per application,
send log and DNS checks in the panel, encrypted backups.

## Features

- Outbound SMTP (465/smtps; optional 587 submission) with per-domain DKIM signing
- Optional inbound relay on port 25 (backup-MX / forwarder; off by default)
- Optional DMARC aggregate report ingest on port 25 (off by default)
- In-panel Help: a page of topics, opened from the `?` in the head of a box
- Per-application client IP allow-list for submitting mail (optional; refused at MAIL FROM, not an authentication boundary)
- Web panel grouped by mail direction — Overview, Outbound (domains, log, queue, DMARC reports), Inbound, Server (health, system log, backup, users, settings)
- Multi-domain relay — each SASL application is bound to one sending domain
- DNS status checks (PTR, SPF, DKIM, DMARC) with in-panel re-check
- Two-level rate limiting — IP backstop (Postfix), per-domain ceilings, and per-application overrides
- Full-server backup and single-domain export/import (optional password encryption)
- Single Docker image; production data in a `./data` bind mount (the quick start below uses a named Docker volume instead)

## Documentation

| Document | Contents |
|---|---|
| [**Operator guide**](docs/guide.md) | Reverse proxy, environment variables, DNS, IP warmup, panel operations, rate limiting, backup/restore, ports, image tag |
| [Product boundaries](docs/product.md) | Purpose, deployment assumptions, out-of-scope items, multi-domain model |
| [Architecture](docs/architecture.md) | As-built technical design |
| [Security design](docs/security.md) | Mandatory requirements, accepted risks, the CSRF ADR |
| [Development](docs/development.md) | Building, testing, docs rules, model routing, commits |
| [Roadmap](docs/roadmap.md) | Open work — direction, not commitments |
| [CHANGELOG](CHANGELOG.md) | Release history |

Found a vulnerability? Do not open an issue — [SECURITY.md](SECURITY.md) has
the private reporting channel and the scope.

Repository: <https://github.com/mixeme/selfpost> — source, issues, releases, and
the `ghcr.io/mixeme/selfpost` image.

## Requirements

Providing these is the operator's job — SelfPost cannot fix a blocked port or a
missing PTR record for you. Details: [Operator guide](docs/guide.md).

### Platform

- Docker + Compose v2 on the host
- A reverse proxy in front of the panel (SelfPost never terminates HTTPS itself)
- Machine, by what is switched on (measured; details and disk growth in the
  guide's [Machine requirements](docs/guide.md#machine-requirements)):

  | Switched on | Host memory, at least | Images on disk |
  |---|---|---|
  | Outbound only (465/587), with or without port 25 (inbound relay, DMARC reports) | 512 MB | 315 MB |
  | With the spam filter (rspamd sidecar), port-25 messages kept small | 768 MB | 626 MB |
  | With the spam filter and 25 MiB messages, several at once | 1 GB | 626 MB |

  rspamd takes about 100 MB more for each large message it scans; a reverse
  proxy in a container adds its own few megabytes.

  One vCPU is enough. **Building the image needs 1.5 GB of memory and about
  3 GB of free disk** (the Go compiler: a 1 GB build was killed) - on a
  smaller machine, build elsewhere and `docker load` the image. The send log
  is what grows: about 2-3 KB a message for the retention (90 days by
  default) - 10 000 messages a day keep about 2.5 GB.

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

See [Domain-level DNS](docs/guide.md#domain-level-dns-spf-dkim-dmarc) in the operator guide.

### Per inbound domain (optional)

Only if you turn on inbound relay (`INBOUND_RELAY_ENABLE=true`):

- [ ] MX record pointing at `SELFPOST_HOSTNAME` (keep any existing primary MX
      if this host is backup-MX)

See [Inbound relay](docs/guide.md#inbound-relay) in the operator guide.

## Quick start

> **First boot — create the admin account.** On a fresh container SelfPost prints
> a **one-time setup URL** (valid ten minutes). Open it in a browser to choose
> the administrator username and password. Until you do, the panel has no login.
> Production deploy: [Full deployment](docs/guide.md#full-deployment) in the
> operator guide.

One container, panel at `http://127.0.0.1:8080` — no reverse proxy, no TLS
files, no compose files. Good for clicking through the UI on your machine;
outbound mail will not reach the real internet without DNS, PTR, and port 25.

> **The 2.x line is not published on ghcr.io yet** (the newest image there is
> `1.3.0`). Until it is, build the image from this checkout under the name the
> commands below and `deploy/docker-compose.yml` use; Docker then takes the
> local image and pulls nothing:
>
> ```sh
> docker build -f build/Dockerfile --build-arg VERSION=2.16.2 \
>   -t ghcr.io/mixeme/selfpost:2.16.2 .
> ```

```sh
docker run --rm -d --name selfpost-try \
  -p 127.0.0.1:8080:8080 \
  -e SELFPOST_HOSTNAME=mail.local.test \
  -e PANEL_COOKIE_SECURE=false \
  -v selfpost-try-data:/data \
  ghcr.io/mixeme/selfpost:2.16.2
```

This trial runs with Docker's default capabilities and no
`no-new-privileges`; the compose file of a real deployment
(`deploy/docker-compose.yml`) drops every capability but the few it names,
sets `no-new-privileges` and a process ceiling.

**Get the setup URL** (pick one):

```sh
docker logs selfpost-try 2>&1 | grep -m1 'http'
```

```sh
docker exec selfpost-try cat /data/setup-token
```

The printed URL is `https://mail.local.test/setup/<token>`. For this local
trial rewrite it to `http://127.0.0.1:8080/setup/<token>` (same path token;
`PANEL_COOKIE_SECURE=false` so the cookie works over plain HTTP). Open it
before it expires.

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

Full walkthrough — fetching the base files, setting up a reverse proxy and
TLS (Apache/nginx/Caddy/Traefik), starting the container, and wiring up
DNS — lives in the operator guide's [Full
deployment](docs/guide.md#full-deployment) section, with proxy-specific
commands under [Reverse proxy](docs/guide.md#reverse-proxy-mandatory).

The compose file always publishes **465**, **587**, and **25**; Postfix listens
on 587 only when `SUBMISSION_ENABLE=true`, and on 25 when
`INBOUND_RELAY_ENABLE=true` and/or `DMARC_REPORTS_ENABLE=true` (see
[Ports](docs/guide.md#ports)). Bump the
pinned image tag deliberately when upgrading, never `:latest` ([why](docs/guide.md#fixed-image-tag)). Optional
variables (`TRUSTED_PROXY_CIDR`, rate limits, retention): see [Environment
variables](docs/guide.md#environment-variables).

## License

Copyright © 2026 Mikhail Yenuchenko.

[AGPL-3.0](LICENSE). The AGPL closes the "SaaS loophole": if you run a modified
version as a network-accessible service, you must make the modified source
available to its users — not only when you distribute copies of the code.
Third-party notices: [NOTICE](NOTICE).
