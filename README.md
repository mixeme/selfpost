# SelfPost

Self-hosted outbound SMTP relay with a web control panel, shipped as a single
Docker image. Postfix + OpenDKIM + a small Go panel run together under
`supervisord`; the panel manages multiple sending domains, per-domain DKIM keys
and SASL-authenticated applications bound to their domain.

SelfPost sends mail straight to the internet from your own IP, with DKIM
signing, and is configured once through the panel. It is **outbound only** — it
does not receive mail, provide mailboxes, or offer webmail.

> **Status: under active development.** See [docs/specification.md](docs/specification.md)
> for the full requirements and [docs/implementation-plan.md](docs/implementation-plan.md)
> for the phased build plan.

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

This starts SelfPost alone; it assumes Apache is already installed on the host
as the reverse proxy (see below) and expects certificates at `./certs`. The
first log line (`docker compose logs -f`) prints the one-time setup link —
open it to create the admin account.

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

## IP warmup

A brand-new IP has no sending history, so receiving servers are cautious with
it regardless of how correct your DKIM/SPF/DMARC are. Start with low volume to
a domain, increase gradually over days/weeks rather than sending everything on
day one, and check the IP against major blocklists (Spamhaus and similar)
before and during warmup. This is inherent to how mail reputation works on the
public internet, not something SelfPost's configuration can shortcut.

## Backup, restore, and moving a single domain

Two related but distinct operations — spec 7.5:

- **Full backup** (whole `/data`: SQLite, all domains' DKIM keys, all
  applications' SASL credentials, `manifest.json` with the version that
  created it): panel button (dashboard → *Backup & migration*), or from the
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

- **Export/import a single domain** (dashboard → domain page → *Export
  domain*): moves one domain — its DKIM key and its applications' **working**
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
