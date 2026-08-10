# Plan: domain-admin (domain administrator role)

**Status:** agreed  
**Version:** target bump **1.x** MINOR, given a compatible migration of the
current administrator into a global one.  
**Order:** recommended after [web-split](web-split.md) (done), before
[inbound-relay](inbound-relay.md).

---

## What this is

Today the panel has exactly one subject: `RequireAuth` is a boolean gate, not a
role ([web.go](../../internal/web/web.go) — the
`mux.Handle("/", s.auth.RequireAuth(authed))` wrapper), and the session carries
nothing beyond the fact of being signed in.

Two panel roles:

| Role | Scope |
|------|-------|
| **global** | Full panel except nothing new — same powers as today's single admin |
| **domain_admin** | Only **assigned** domains (one or several; list set by global admin) |

For each assigned domain, a domain-admin can:

- applications (create, sender mode, password regeneration, delete, L2 limit);
- DKIM/DNS status and recheck;
- per-domain DMARC `rua=` (inherit / none / custom) — full control on the
  domain page;
- send log filtered to assigned domains;
- domain export (encrypted `.spde` optional, same as today);
- domain-level L2 rate limit.

What stays **global-only** (domain-admin gets 404 or redirect):

- adding and removing domains;
- domain import;
- creating/editing/deleting panel users and assigning domains;
- `/reload`;
- full backup (`/backup` — all of `/data` including every domain's `sasldb2`);
- mail queue (`/mail-queue*`);
- system log tail (`/system-log*`);
- status page (`/status*`) — server-wide health, queue summary, reload, DNS
  recheck of the **hostname**; same treatment as queue and system log.

Domain-admin **self-service** on `/account`: username and password only (not
global DMARC report email).

## Why this extends v1.0

[product.md](../product.md) puts "multiple panel users, roles" out of scope
(one administrator). A second subject is a deliberate widening of the project's
boundary, as inbound-relay is.

The cost is phase-sized, not patch-sized:

- a users table and their binding to domains;
- the role in the session;
- authorisation in every handler (not only on the route — today `{id}`/`{aid}`
  are checked for nothing beyond existence);
- reworking first-run setup and password change for several users;
- accounting for the new subject in backup and domain export.

*(The earlier wording of this item — "2FA and multiple administrators" — has
been replaced: 2FA is off the table, and "multiple administrators" is narrowed
to one specific role, because what is needed is not a second all-powerful admin
but limited access for the owner of one or several domains, with the list set
by the global administrator.)*

---

## Schema and migration

**New migration** `0005_panel_users.sql`:

```sql
CREATE TABLE users (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    username            TEXT NOT NULL UNIQUE,
    password_hash       TEXT NOT NULL,
    role                TEXT NOT NULL CHECK (role IN ('global', 'domain_admin')),
    dmarc_report_email  TEXT NOT NULL DEFAULT '',
    created_at          TEXT NOT NULL
);

CREATE TABLE user_domains (
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, domain_id)
);

-- Migrate existing administrator → global user (idempotent guard via admin count).
INSERT INTO users (username, password_hash, role, dmarc_report_email, created_at)
SELECT username, password_hash, 'global', dmarc_report_email, created_at
FROM admin WHERE id = 1;

DROP TABLE admin;
```

**Sessions:** keep `sessions.username` (no schema change). On login and
`RequireAuth`, resolve username → `User` row (role + domain IDs). Stale session
after username change behaves as today (`Lookup` fails → redirect login).

**Backup/restore:** full backup already snapshots `selfpost.db` via
`VACUUM INTO`; users and bindings restore with the DB. No manifest format change
required (same `selfpost-full-backup`).

**DMARC two levels:**

- **Global** `users.dmarc_report_email` — only on global user's `/account`;
  default `rua=` when a domain uses *inherit*.
- **Per-domain** `domains.dmarc_rua` — domain-admin edits on the domain page
  (existing handler); domain-admin never sees the global default field.

---

## Principal model

Request context carries a `Principal` (in `internal/web/auth`):

```go
type Role string // "global" | "domain_admin"

type Principal struct {
    ID       int64
    Username string
    Role     Role
    Domains  []int64 // assigned domain IDs; empty for global (meaning "all")
}
```

Helpers:

- `CurrentPrincipal(r)` — from context;
- `IsGlobal(p)` — `p.Role == "global"`;
- `CanAccessDomain(p, domainID)` — global or `domainID` in `p.Domains`;
- `CanAccessApp(p, app)` — `CanAccessDomain(p, app.DomainID)`.

`lookupDomain` / `lookupApplication` in handlers call `CanAccess*` after
existence check; return 404 (not 403) to avoid leaking IDs.

---

## Route matrix

| Method | Path | global | domain_admin |
|--------|------|--------|--------------|
| GET | `/` | → `/status` | → `/domains` |
| GET | `/status`, `/status/fragment` | yes | **no** (404) |
| POST | `/status/recheck` | yes | **no** |
| GET | `/domains` | all domains | assigned only |
| POST | `/domains` | yes | **no** |
| POST | `/domains/import` | yes | **no** |
| GET | `/domains/{id}` | yes | assigned |
| POST | `/domains/{id}/dns-recheck` | yes | assigned |
| GET/POST | `/domains/{id}/delete` | yes | **no** |
| POST | `/domains/{id}/applications` | yes | assigned |
| POST | `/domains/{id}/ratelimit` | yes | assigned |
| POST | `/domains/{id}/dmarc` | yes | assigned |
| POST | `/domains/{id}/export` | yes | assigned |
| POST | `/applications/{aid}/*` | yes | if app in assigned domain |
| POST | `/reload` | yes | **no** |
| GET/POST | `/account` | username, password, global DMARC email | username, password only |
| GET/POST | `/backup` | yes | **no** |
| GET | `/deliveries*` | all (optional filter) | clamped to assigned domains |
| GET | `/mail-queue*` | yes | **no** |
| GET | `/system-log*` | yes | **no** |
| GET | `/users` | list users | **no** |
| GET/POST | `/users/new` | create user | **no** |
| GET/POST | `/users/{uid}` | edit/delete user | **no** |
| POST | `/logout` | yes | yes |

**Deliveries:** for domain-admin, `sendLogData` forces filter to assigned
domain set; dropdowns list only assigned domains/apps; reject `domain` query
param outside assignment; `HandleDelivery` checks log row's `domain` field.

---

## User management UI (global only)

New routes under `/users`:

- **List** — username, role, assigned domain names (or "all" for global).
- **Create** — username, password, role (`domain_admin` default), multi-select
  domains (required when role is `domain_admin`).
- **Edit** — change password (optional), reassign domains, delete user.
- **Guards:** cannot delete the last `global` user; cannot demote self to
  `domain_admin` without another global user; domain-admin role cannot access
  these routes.

Templates: `users.html`, `user_form.html`; nav link visible only for global
users.

---

## Auth / setup / sessions

- **Setup** (`/setup/{token}`): unchanged semantics — creates first **global**
  user via `CreateGlobalUser`; `AdminExists` → `UserExists`.
- **Login:** authenticate against `users` by username + bcrypt.
- **Password change:** per-user `UpdateUser`; domain-admin cannot change
  another user's password.
- **Session rename / destroy others:** unchanged behaviour keyed by username.

---

## Navigation

[layout.html](../../internal/web/view/templates/layout.html) `nav` template:

- **global:** all items today (status, domains, deliveries, mail queue, system
  log, backup, settings) + **Users**.
- **domain_admin:** domains, deliveries, settings only.

Pass `IsGlobal` (or `Principal`) into every rendered page.

---

## Security

- **CSRF:** keep origin-check-only for now ([security.md](../security.md) ADR);
  note in CHANGELOG that multi-user panel reopens the ADR — no CSRF tokens in
  this phase.
- **Export encryption:** optional password on domain export remains; full backup
  encryption trigger ("second administrator") is satisfied by domain-admin
  existing — no change required.
- **Authorization tests:** table-driven tests for global vs domain-admin on
  representative handlers; explicit `{aid}` cross-domain mutation blocked.

---

## Implementation order

1. Migration `0005_panel_users.sql` + `store/users.go` (CRUD, domain bindings).
2. Auth: login against `users`, `Principal` in context, setup creates global user.
3. `CanAccessDomain` / `CanAccessApp`; harden `lookupDomain` / `lookupApplication`.
4. Route guards: global-only middleware or per-handler checks.
5. Filter lists: dashboard, deliveries, domain detail DMARC inherit source.
6. User management handlers + templates.
7. Nav visibility + default redirect (`/`).
8. Tests + `go build` / `go vet` / `go test`; CHANGELOG `[Unreleased]`.

---

## Done when

- A global administrator and a domain-admin with different rights both work
  through the panel; the domain-admin cannot reach past the **assigned**
  domains;
- the current single admin migrates into a global one without losing access;
- backup/restore accounts for users and their bindings;
- `build`/`vet`/`test`/image green.

## Risks

- An incomplete `{id}`/`{aid}` check in a handler — access leaking to someone
  else's domain;
- breaking setup or backup — that would be a semver major, not 1.x.
