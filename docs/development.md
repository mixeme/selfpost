# SelfPost — development

**What this file is.** How to build, test, document, and ship changes. Open
work after 1.0 (1.x+) lives in [roadmap.md](roadmap.md) and linked
[plans/](plans/). Product boundaries: [product.md](product.md). As-built layout:
[architecture.md](architecture.md).

---

## Resuming work

After `/clear` or a fresh chat:

1. Read this file (process, docs rules, model routing).
2. Open [roadmap.md](roadmap.md) for the index of open work; follow the linked
   plan file for the active item. Accepted risks — [security.md](security.md);
   as-built — [architecture.md](architecture.md).
3. Skim [product.md](product.md) if scope is in doubt.
4. Continue from the next unchecked step in the **active** plan file (not the
   roadmap index).

History of closed phases is in `git log` and [CHANGELOG.md](../CHANGELOG.md),
not duplicated here.

---

## Authorship and disclosure

**Decided, not open for re-litigation.** SelfPost is written by AI agents under
a maintainer's direction, and the project says so rather than hiding it.

Concretely, this is what "says so" means, and none of it is an oversight to be
tidied away later:

- `Co-Authored-By: Claude <model>` trailers stay in commit messages, including
  the ~140 commits that predate v1.0.
- The model routing table below is public, in a file the README links to.
- [.cursor/rules/agent-rules.mdc](../.cursor/rules/agent-rules.mdc) ships in
  the repository.
- Process notes written for an agent — "after a context reset, pick an item
  marked `agreed`" in [roadmap.md](roadmap.md) — stay as they are.

**Why not quietly drop it.** Once the trailers are in the history, removing the
routing table or the rules file would not conceal authorship, it would only
make the project look like it was trying to. Partial concealment reads worse
than the plain statement, and the plain statement costs nothing: the code is
reviewed, tested, and shipped under the same rules either way, and the
[security design](security.md) records what was audited and what was accepted.

**Revisit if:** the disclosure ever conflicts with the licence or a downstream
obligation — not because the convention around AI authorship shifts.

---

## Model routing

| Kind of work | Model | Examples |
|---|---|---|
| Security, infra, file permissions, Postfix/`postqueue`, open-relay risk | **Opus** | `mail.log` under `/data`, entrypoint permissions, queue reconcile |
| UI / JS / CSS, templates, documentation (English), README | **Sonnet** | adaptive polling, this file's Documentation section |
| Trivial mechanics: retarget links, grep, compose bump, CHANGELOG cut | **Haiku** | Makefile / release.yml comment fixes, deleting closed plan files |
| Security **review** (not authorship) | **Fable** | pre-release checklist (CHANGELOG `[0.5.0]` Security / [security.md](security.md) — done) |

Default rule: risk-critical → Opus; UI / docs / boilerplate → Sonnet; trivial
mechanics → Haiku. Reviewers must not be the author of the code under review.

---

## Technology stack and tools

| Component | Version / notes |
|---|---|
| **Go** | 1.26+ (`go.mod`); `CGO_ENABLED=0` — pure Go, static linking |
| **SQLite** | `modernc.org/sqlite` (pure Go, no cgo) |
| **Build** | [Makefile](../Makefile): primary targets `vet`, `test`, `build`, `e2e` (also `all`, `clean`) |
| **Container** | Docker + Compose v2 on the dev host and in CI |
| **Image (build stage)** | `golang:1.26-bookworm` — [build/Dockerfile](../build/Dockerfile) |
| **Image (runtime)** | `debian:bookworm-slim` + Postfix, OpenDKIM, supervisord, SASL, logrotate |
| **CI** | GitHub Actions — [.github/workflows/](../.github/workflows/) |
| **Image registry** | `ghcr.io/mixeme/selfpost` |

**Repository layout** (brief; process details in [architecture.md](architecture.md)):

- `cmd/panel` — HTTP panel + journal-milter + log-tailer
- `cmd/selfpost-backup` — backup CLI (`docker exec … selfpost-backup`)
- `internal/` — domain logic, store, web, health
- `build/` — Dockerfile, supervisord, Postfix/OpenDKIM, entrypoint
- `deploy/` — `docker-compose.yml`, proxy examples, `.env.example`
- `test/e2e/` — **separate Go module**; container integration tests

---

## External libraries

The project is **AGPL-3.0** ([LICENSE](../LICENSE)). Copyright holder and
third-party notices: [NOTICE](../NOTICE). The tree does not use per-file
`SPDX-License-Identifier` headers; AGPL-3.0 does not require them. New Go
dependencies must be permissive or GPL-family (see
[.cursor/rules/agent-rules.mdc](../.cursor/rules/agent-rules.mdc)).

### Main module (`go.mod`)

| Package | Version | Repository | License |
|---|---|---|---|
| `github.com/emersion/go-milter` | v0.4.1 | <https://github.com/emersion/go-milter> | BSD-2-Clause |
| `golang.org/x/crypto` | v0.54.0 | <https://github.com/golang/crypto> | BSD-3-Clause |
| `modernc.org/sqlite` | v1.53.0 | <https://gitlab.com/cznic/sqlite> (mirror: <https://github.com/modernc-org/sqlite>) | BSD-3-Clause |

Transitive dependencies — `go mod graph` / `go.sum`; all indirect packages in
the tree are AGPL-3.0-compatible.

### Vendored front-end

| Asset | Version | Repository | License |
|---|---|---|---|
| `internal/web/view/static/htmx.min.js` | 2.0.4 | <https://github.com/bigskysoftware/htmx> | 0BSD |
| `internal/web/view/static/ibm-plex-*.woff2` | latin subset | <https://github.com/IBM/plex> | SIL OFL 1.1 (`OFL.txt` beside the files) |

### E2e module (`test/e2e/go.mod`)

| Package | Version | Repository | License |
|---|---|---|---|
| `github.com/emersion/go-msgauth` | v0.6.8 | <https://github.com/emersion/go-msgauth> | BSD-2-Clause |

The test module is not part of the main `go build` graph and is not shipped in
the image.

### Debian packages in the runtime image

Postfix, OpenDKIM, `supervisord`, `sasl2-bin`, `logrotate`, and others come
from Debian bookworm repositories; licenses are in each package's `copyright`
file on <https://packages.debian.org/bookworm/>.
The image also ships [LICENSE](../LICENSE), [NOTICE](../NOTICE), and the IBM
Plex [OFL.txt](../internal/web/view/static/OFL.txt) under
`/usr/share/doc/selfpost/`. The panel serves the AGPL text at `/license` and
the OFL text at `/static/OFL.txt`.

---

## Building binaries and the image

### Local binaries

Requires Go 1.26+ and `CGO_ENABLED=0`.

```sh
make build        # bin/panel, bin/selfpost-backup (VERSION=dev by default)
make build VERSION=1.2.3
```

Or directly:

```sh
go build -trimpath -ldflags "-X github.com/mixeme/selfpost/internal/buildinfo.Version=dev" -o bin/panel ./cmd/panel
```

The version is stamped into both binaries via `-ldflags` and **must match the
Docker image tag** — restore checks backup version compatibility.

### Docker image

From the repository root:

```sh
docker build -f build/Dockerfile -t selfpost:dev --build-arg VERSION=dev .
```

The Dockerfile has a build stage (`go vet`, `go build` with `VERSION`) and a
runtime stage (Debian + mail stack). Runtime config and scripts use `COPY
--chmod` so file modes in the image do not depend on how the build context was
synced (e.g. a Windows checkout widening permissions on `logrotate-mail.conf`).
See [architecture.md](architecture.md) § Image and processes.

---

## Commits and release build

Commit on every meaningful step (a working sub-feature, a green build, end of a
phase) — not every file save, and not only at phase end. Minimum: one commit per
closed phase, plus intermediate commits for coherent sub-steps. Branch `main`
unless a separate branch is requested. Push / PR only on explicit request.

Commit messages end with
`Co-Authored-By: Claude <model> <noreply@anthropic.com>` for the model that did
the step (e.g. `Claude Sonnet 4.6`).

Every such step also updates [CHANGELOG.md](../CHANGELOG.md) under
`[Unreleased]` (Keep a Changelog). On an explicit version cut, rename
`[Unreleased]` to `[X.Y.Z] - date` and open a fresh empty `[Unreleased]`. Image
tag / push only on explicit request (see `release.yml`).

### Release image

The release image is published **only** for a SemVer version `X.Y.Z`: a
**published** GitHub Release whose tag is `vX.Y.Z`, or a `workflow_dispatch`
that supplies that version. Pushing a git tag alone does not publish. Ordinary
commits, and a dispatch from `main` without a version input, do not publish.
The version is the single source that drives the image tag and `-ldflags` in
the binaries so they cannot drift apart.

**Steps (on explicit request):**

1. Close `[Unreleased]` in [CHANGELOG.md](../CHANGELOG.md) and bump the pinned
   tag in [deploy/docker-compose.yml](../deploy/docker-compose.yml) (and any
   local-trial image references) in the **same** release commit.
2. Create and push git tag `vX.Y.Z` on that commit.
3. Publish the GitHub Release for `vX.Y.Z` (not a draft).
4. Workflow [release.yml](../.github/workflows/release.yml) builds, e2e-gates,
   and publishes `ghcr.io/mixeme/selfpost:X.Y.Z`.

**GitHub Release vs GHCR.** The public [Releases](https://github.com/mixeme/selfpost/releases)
page lists only **published** releases. A draft is visible to maintainers only —
it looks like “no releases” to everyone else. CI does not create or publish the
GitHub Release; you do that in the UI. Deleting a release’s git tag on GitHub
(or re-pushing tags while cleaning the registry) converts a published release
back into a **draft** — that matches “I published three times and it keeps
disappearing”. After publish, leave the tag on GitHub; clean up only unwanted
GHCR package versions, not the git tag.

Push workflow and source changes to **github.com/mixeme/selfpost** before
publishing — Actions reads that repo, not Gitea.

Ordinary commits **do not** publish an image. The compose pin and the git tag
must match (`1.0.0` / `v1.0.0` for the first published release). Intermediate
CHANGELOG sections (`0.2.0`…`0.6.0`) record development history before that cut.

---

## Phase closure

Before `/clear` at the end of a finished step:

1. Update [roadmap.md](roadmap.md) (and the active plan checklist, if any): what
   changed, what is next.
2. Check the applicable «Done when…» criteria.
3. Append [CHANGELOG.md](../CHANGELOG.md) under `[Unreleased]`.
4. Make the final commit for the step (when the user asks for a commit).

---

## Testing

### Static analysis and unit tests

Main module (`go test ./...`); e2e is a separate module — see below.

```sh
make vet          # go vet ./...
make test         # go test ./...
```

Or directly:

```sh
gofmt -l .        # in CI — fails on drift
go vet ./...
go test ./...
```

### Env documentation regression

`go test ./cmd/panel -run 'TestLoadConfigKeysDocumented|TestBuildScriptKeysDocumented|TestDocumentedKeysAreRead'`
— every new `loadConfig` key must appear in the env lists in [guide.md](guide.md)
([cmd/panel/envdoc_test.go](../cmd/panel/envdoc_test.go)).

### End-to-end (container suite)

Separate Go module `test/e2e/`; **not** included in the main module's
`go test ./...`.

```sh
make e2e
# same as: cd test/e2e && go test -v -timeout 20m ./...
```

**Stack:** [deploy/docker-compose.yml](../deploy/docker-compose.yml) +
[test/e2e/compose.override.yml](../test/e2e/compose.override.yml) — same
`cap_drop`/`cap_add`/`no-new-privileges` as production. Override: high ports
(`20465`/`20587`/`20080`), test hostname, self-signed TLS,
`PANEL_COOKIE_SECURE=false`, isolated compose project. Mail is hermetic:
CoreDNS (fake zone) + Postfix `smtp-sink` as sink-MX; DKIM TXT is scraped from
the panel and published into the zone — the test verifies the records an
operator would actually use.

**Coverage (summary):** bootstrap → SMTP AUTH → delivery → DKIM verify →
send-log `queued → sent`; negatives (no AUTH, relay, sender/login mismatch,
L1/L2 limits, milter fail-open, bad `SELFPOST_HOSTNAME`, session survives
`docker restart`); startup checks that supervisord actually brought up
OpenDKIM, the panel, and Postfix (`checkSupervisorProcesses`), plus logrotate
config-mode and forced-rotation checks (`checkLogrotateConfigMode`,
`checkLogrotateRotation` — [test/e2e/logrotate_check.go](../test/e2e/logrotate_check.go)).
Polling with timeouts only — no fixed `sleep`.

Requires **Docker + Compose v2** on the machine running the suite.

---

## CI

Workflows in [.github/workflows/](../.github/workflows/). What each job runs —
[§ Testing](#testing) above.

### `test.yml` — every push and PR to `main`

`gofmt -l` → `go vet ./...` → `go test ./...` (main module, no e2e).

### `release.yml` — published GitHub Release, or `workflow_dispatch` with SemVer

`prepare` takes the version from the published release tag (`v1.2.5` → `1.2.5`)
or from the `workflow_dispatch` `version` input. A bare git tag push does not
run this workflow. A dispatch whose input is missing or not `X.Y.Z` fails in
`prepare` — it must not publish `ghcr.io/...:main`.

```
prepare (version from published release tag or workflow_dispatch input)
  → build [matrix: ubuntu-latest / ubuntu-24.04-arm]
      → docker build --load (VERSION from prepare)
      → e2e (test/e2e)
      → push ghcr.io/...:X.Y.Z-amd64 | X.Y.Z-arm64
  → merge
      → docker buildx imagetools create → unified manifest X.Y.Z
```

Native per-arch matrix (no QEMU): running the full Postfix/OpenDKIM stack under
emulation for e2e is impractical. E2e first, then push — the registry receives
the bytes that passed the gate.

A failed e2e **blocks** image publication.

---

## Documentation

History of deleted plans lives in git and [CHANGELOG.md](../CHANGELOG.md).
There is no `docs/archive/` directory.

### Documentation map

| Home | File |
|---|---|
| Operator install / quick start | [README.md](../README.md) |
| Operator guide | [guide.md](guide.md) |
| Product boundaries | [product.md](product.md) |
| As-built design | [architecture.md](architecture.md) |
| Development process (this file) | [development.md](development.md) |
| Security requirements and accepted risks | [security.md](security.md) |
| Roadmap (1.x+) | [roadmap.md](roadmap.md) |
| Active design plans | [plans/](plans/) |
| Release history | [CHANGELOG.md](../CHANGELOG.md) |

### User-facing deliverables

| Artefact | Role |
|---|---|
| [README.md](../README.md) | Overview, requirements, quick start, docs index, reference deploy, licence |
| [guide.md](guide.md) | Proxy, env, DNS, IP warmup, operations, rate limiting, backup, ports, image tag |
| [SECURITY.md](../SECURITY.md) | Private reporting channel, supported versions, scope |
| [LICENSE](../LICENSE) | AGPL-3.0 full text |
| [NOTICE](../NOTICE) | Copyright holder and third-party attributions |
| [deploy/docker-compose.yml](../deploy/docker-compose.yml) + proxies | Apache + nginx/Caddy/Traefik under [deploy/](../deploy/) |
| [deploy/.env.example](../deploy/.env.example) | Public env template; full reference in [guide.md](guide.md) |
| [CHANGELOG.md](../CHANGELOG.md) | Keep a Changelog |

Out of scope for v1.x: `CONTRIBUTING.md`, man pages, a separate docs site
(candidates in [roadmap.md](roadmap.md)).

### Maintaining documentation

1. **Step rule:** a new or renamed env key, panel route, or observable mail-path
   behaviour ships together with [guide.md](guide.md) / `.env.example` and a
   CHANGELOG entry (see [§ Commits and release build](#commits-and-release-build)).
2. **Env regression:** [cmd/panel/envdoc_test.go](../cmd/panel/envdoc_test.go)
   fails on an undocumented `loadConfig` or build-script key.
3. **New gaps** go into [roadmap.md](roadmap.md) (or the active plan file), not
   silent drive-by edits.

### Verifying docs against code

Every claim in the docs has a **source of truth in the tree**; verify from code
to prose.

| Claim class | Source of truth |
|---|---|
| Env keys and defaults | `loadConfig` — [cmd/panel/main.go](../cmd/panel/main.go); `${VAR:-…}` in [build/](../build/) |
| Mail path | [build/postfix-config.sh](../build/postfix-config.sh) |
| Panel routes | [internal/web/web.go](../internal/web/web.go) |
| Backup / restore, domain export | [internal/backup/](../internal/backup/), [cmd/selfpost-backup/](../cmd/selfpost-backup/) |
| Sessions | [internal/store/sessions.go](../internal/store/sessions.go), [internal/web/auth/session.go](../internal/web/auth/session.go) |
| Log rotation, reload | [build/logrotate-mail.conf](../build/logrotate-mail.conf), [build/logrotate-loop.sh](../build/logrotate-loop.sh), [build/postfix-cert-reload.sh](../build/postfix-cert-reload.sh) |
| Deploy | [deploy/docker-compose.yml](../deploy/docker-compose.yml), [build/Dockerfile](../build/Dockerfile) |
| Operator checklist | [§ User-facing deliverables](#user-facing-deliverables); detail — [guide.md](guide.md) |
| Product / out of scope | [product.md](product.md) |
| As-built | [architecture.md](architecture.md) |
| Mandatory security | [security.md](security.md) |

Order: list what the code actually does → find it in [guide.md](guide.md) /
`architecture.md`. Before every tag, a short pass over this table — not a full
prose rewrite.
