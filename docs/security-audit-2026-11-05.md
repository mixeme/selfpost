# Security audit — 2026-11-05

> **No findings above low.** The one low (E1) is a documentation gap, not a
> code change; E2 and E3 are confirmations. The file stays as the record of
> what an independent reader checked at `5867f97` (release `2.16.13`).

**What this is.** An external single-pass security review of SelfPost at
`5867f97` (release `2.16.13`), run at the owner's request: the code tree, the
deployment fragments and CI, with [security.md](security.md) as the yardstick.
It is narrower than the 2026-10-17 audit
([security-audit-2026-10-17.md](security-audit-2026-10-17.md)): one reader —
Cline, an AI agent outside this project's model routing
([development.md](development.md) § Model routing) and not the author of any
code in the tree — the tree only: no live server, no probes, no e2e run.
Nothing in the tree was changed by the review itself; this file, and the
history paragraph it adds to [security.md](security.md), are the only output.

**Method.** The security-relevant paths were read in full: authentication,
sessions, setup and reset links, and the login limiters
([internal/web/auth](../internal/web/auth)); the origin check and the
security headers ([internal/web/security.go](../internal/web/security.go));
the SQL layer ([internal/store](../internal/store)); template escaping and
static serving ([internal/web/view](../internal/web/view)); every
`exec.Command` call site in the product ([internal/app/sasl.go](../internal/app/sasl.go),
[internal/postfix](../internal/postfix), [internal/notify/mailer.go](../internal/notify/mailer.go));
file writes and permissions ([internal/atomicfile](../internal/atomicfile),
[internal/secretfile](../internal/secretfile)); validation
([internal/web/validate](../internal/web/validate)); RBAC and the
secret-download re-auth ([internal/web/handlers](../internal/web/handlers));
the DMARC ingest ([internal/dmarc](../internal/dmarc)); the DKIM signer
([internal/milter/sign.go](../internal/milter/sign.go)); the deployment
([deploy/docker-compose.yml](../deploy/docker-compose.yml),
[build/Dockerfile](../build/Dockerfile)) and CI
([.github/workflows](../.github/workflows)). Pattern searches over the tree
and the tracked files for secret shapes, weak hashes (`md5`, `sha1`, `rc4`,
`des`), `math/rand`, `InsecureSkipVerify`, and SQL built by concatenation.

**Machine results.** `go vet ./internal/... ./cmd/...` ran clean (go1.27.0,
windows/amd64). No private key or token outside test fixtures in the tracked
files; `dev/` untracked; `.claude/` and `.cursor/` carry agent configuration
only, no secrets. No `math/rand`, no weak hash, no `InsecureSkipVerify` in
product code (test/e2e only). `staticcheck` and `govulncheck` did not run in
this session — the local toolchain is 1.27 and staticcheck 2026.1 targets
1.26; CI runs both on every push and pull request
([test.yml](../.github/workflows/test.yml)), which is the standing gate.

**Severity** follows the 2026-10-17 audit's scale: **critical** — the
security boundary is crossed or mail/data lost today, by a stranger;
**high** — a documented guarantee is false in a way an attacker will use;
**medium** — a tenant or stranger gets something the design denies them, or a
stated guard is not there; **low** — unlikely condition, cost without a
boundary, or hygiene with a security flavour; **nit** — wording. Confidence
is *proven* (a test, a probe, or the running system), *likely* (unambiguous
reading) or *possible*. IDs are lettered **E** — external review.

**The short version.** Nothing above low. Every check the review made agrees
with [security.md](security.md): 256-bit `crypto/rand` session tokens stored
as SHA-256, rotation at sign-in, fail-closed creation, the `__Host-` prefix
with `Secure` on by default; the origin-strict CSRF defence sitting outside
the auth middleware; parameterized SQL throughout, with the two column-name
interpolations whitelist-gated
([deliverylog.go](../internal/store/deliverylog.go),
[sendercheck.go](../internal/store/sendercheck.go)); contextual
auto-escaping with the one deliberate `template.HTML` escaped before its
`<wbr>` ([components.go](../internal/web/view/components.go)); fixed-argv
external commands with `--` before any address, no shell; validation
whitelists whose output is safe as path segments; embedded static assets
behind an ETag whitelist that 404s everything else
([static.go](../internal/web/view/static.go)); re-auth on backup, export and
user-form secrets; a hardened image (dropped capabilities,
`no-new-privileges`, pids limit, fixed uids) and a CI that pins its actions
by digest and runs govulncheck at every push.

---

## Findings

### Low

**E1. The absent absolute session cap is stated in the guide but is not
recorded under Accepted risks.** *Likely (by reading).* A session lives as
long as its sliding idle window is renewed
([session.go](../internal/web/auth/session.go)); the guide says so plainly —
`PANEL_SESSION_IDLE_DAYS`, "There is no absolute cap: a user who keeps coming
back stays signed in indefinitely" — but [security.md](security.md)'s
Accepted risks does not carry the decision, so a reader of that register
alone does not learn it or its trigger. **Fix:** one bullet under Accepted
risks ("no absolute session cap; revisit when more than one person
administers a server"), or leave as is — a wording change either way, for the
owner to place or decline.

## Confirmed as written (no action)

**E2. The named accepted risks hold.** The review re-read the three the
owner would name as residual risk — no session-bound CSRF tokens (the origin
check is the boundary; the ADR's revisit triggers stand), backup encryption
switchable off (a single-operator trade-off with its trigger), and
unauthenticated DMARC ingest (domain-tagged addresses, size- and count-bound,
advisory data) — and found nothing that makes any of them easier to exploit
than the register claims.

**E3. Dependencies and supply chain.** `golang.org/x/crypto v0.58.0`,
`modernc.org/sqlite v1.60.1`, `emersion/go-msgauth v0.7.0` — current at the
date of this review; `go-milter` is the locally patched copy under
`third_party/`, with the patch's purpose recorded there. CI pins its actions
by commit digest, asks least-privilege permissions, and runs `govulncheck`
on every push and pull request; the release workflow refuses to publish an
image whose stamped version differs from the tag.

## Scope and limits

- One reader, one pass: the paths named under Method were read in full; the
  rest of the tree was covered by targeted search, not line by line. This is
  not the 2026-10-17 audit's four-zone read of every file and its tests.
- No running system was touched: no probes, no e2e (the suite needs a Linux
  Docker host), no inspection of a live installation.
- `staticcheck` and `govulncheck` were not re-run locally; CI's gate on every
  push is the standing check.
