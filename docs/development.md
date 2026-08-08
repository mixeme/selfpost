# SelfPost — development

**What this file is.** How to build, test, and ship changes. Current sprint
state lives in [progress.md](progress.md) — read that first after `/clear`.

Product boundaries: [product.md](product.md). As-built layout:
[architecture.md](architecture.md).

---

## Repository layout

- `cmd/panel` — panel HTTP server + milter + log-tailer
- `cmd/selfpost-backup` — CLI backup (`docker exec … selfpost-backup`)
- `internal/` — domain logic, store, web handlers, health checks
- `build/` — Dockerfile, supervisord, Postfix/OpenDKIM wiring, entrypoint
- `deploy/` — `docker-compose.yml`, proxy examples, `.env.example`
- `test/e2e/` — **separate Go module**; container integration tests

---

## Local Go workflow

Requires Go 1.26+ and `CGO_ENABLED=0` (pure Go SQLite).

```sh
make vet          # go vet ./...
make test         # go test ./...
make build        # bin/panel, bin/selfpost-backup (VERSION=dev by default)
make build VERSION=1.0.0
```

Or directly:

```sh
go vet ./...
go test ./...
go build -trimpath -ldflags "-X github.com/mixeme/selfpost/internal/buildinfo.Version=dev" -o bin/panel ./cmd/panel
```

**Env documentation regression:** `go test ./cmd/panel -run TestLoadConfig` —
new `loadConfig` keys must appear in guide.md env lists
([cmd/panel/envdoc_test.go](../cmd/panel/envdoc_test.go)).

---

## End-to-end tests

Hermetic container suite (implemented as `test/e2e/`, separate Go module):

```sh
make e2e
# equivalent: cd test/e2e && go test -v -timeout 20m ./...
```

**Stack:** shipped [deploy/docker-compose.yml](../deploy/docker-compose.yml) +
[test/e2e/compose.override.yml](../test/e2e/compose.override.yml) — same
`cap_drop`/`cap_add`/`no-new-privileges` as production. Override uses high
ports (`20465`/`20587`/`20080`), test hostname, self-signed TLS,
`PANEL_COOKIE_SECURE=false`, isolated compose project. Mail is hermetic: CoreDNS
fake zone + Postfix `smtp-sink` as sink-MX; DKIM TXT is scraped from the panel
and published into the zone so the test verifies the record the operator would
use.

**Coverage (summary):** full bootstrap → SMTP AUTH → delivery → DKIM verify →
send-log `queued → sent`; negatives (no AUTH, relay, sender/login mismatch, L1/L2
limits, milter fail-open, bad `SELFPOST_HOSTNAME`, session survives
`docker restart`). Polling with timeouts only — no fixed `sleep`.

Requires **Docker + Compose v2** on the test host. Not included in `go test ./...`
of the main module.

**CI** ([release.yml](../.github/workflows/release.yml)): tag `vX.Y.Z` triggers
`prepare` (version from tag) → native matrix `[ubuntu-latest, ubuntu-24.04-arm]`
— build `--load`, e2e, push per-arch tag → `merge` publishes unified
`ghcr.io/...:X.Y.Z` via `docker buildx imagetools create`. Failed e2e blocks the
image. Ordinary pushes still run only `vet`/`test` in [test.yml](../.github/workflows/test.yml).

---

## Dev server (full container)

When unit tests are not enough — Postfix, OpenDKIM, supervisord, real SMTP:

**Typical setup:** edit locally → sync tree to dev host → build/test there.

Documented dev host: `selfpost.mixfed.ru` (Debian 12). Sync example from
[progress.md](progress.md):

```sh
tar -czf - --exclude=.git . | ssh root@selfpost.mixfed.ru \
  'rm -rf /root/selfpost-src && mkdir -p /root/selfpost-src && tar -xzf - -C /root/selfpost-src'
```

On the server (Go in `/usr/local/go/bin` if not in PATH):

```sh
cd /root/selfpost-src
/usr/local/go/bin/go vet ./...
/usr/local/go/bin/go test ./...
docker build -f build/Dockerfile -t selfpost:dev --build-arg VERSION=dev .
```

Manual smoke on production-like host: panel at `https://selfpost.mixfed.ru`,
real LE cert, live deliverability — not replaceable by e2e alone (no outbound
25 on CI runners, no real PTR/reputation).

---

## Commit and changelog protocol

From [progress.md](progress.md):

1. Meaningful step → entry under `[Unreleased]` in [CHANGELOG.md](../CHANGELOG.md)
   (Keep a Changelog format).
2. Commit on `main` unless asked for a branch. **Do not commit unless the user
   asks.**
3. Before `/clear` at end of a phase: update `progress.md`, verify acceptance
   criteria, CHANGELOG, final commit.

Documentation changes that add/rename env vars, panel routes, or observable
mail behaviour ship in the **same commit** as the code change.

Release tagging and image push — only on explicit request.

---

## Agent rules (formerly spec §12)

1. **No git commits** without explicit instruction in the prompt.
2. After Go changes: `go build`, `go vet`; fix all issues. Run `go test` when
   tests exist.
3. Before calling a container task done: image builds and container starts.
4. Iterate: minimal skeleton first, then features.
5. Security requirements in [security.md](security.md) — implement with the feature,
   not deferred.
6. Do not implement out-of-scope items ([product.md](product.md)) or change
   fixed assumptions without agreement.
7. For large tasks: propose a plan before coding unless the user already approved
   one.
8. Check licence compatibility of new Go dependencies (permissive or
   GPL-family for AGPL-3.0 project).

Model routing (from progress): security/infra → Opus; UI/docs → Sonnet;
trivial mechanics → Haiku. Pre-release security **review** (not authorship) →
Fable.
