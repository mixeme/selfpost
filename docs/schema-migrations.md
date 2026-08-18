# SQLite schema and migrations

**What this file is.** A living reference for the embedded SQLite migration chain,
historical legacy that still runs on fresh 1.x installs, and the planned 2.x
schema squash. When you add or review a migration, update this file in the same
change.

**Source of truth in code:** `internal/store/migrations/*.sql`, applied by
`internal/store/store.go` (`migrate()`).

**Related:** [architecture.md](architecture.md) § Persistence;
[roadmap.md](roadmap.md) § schema-squash; [development.md](development.md) §
Documentation.

---

## Current snapshot

| Field | Value |
|---|---|
| Chain head | `user_version = 9` |
| Files | `0001_init.sql` … `0009_application_auth_ips.sql` (9 files) |
| Database file | `/data/selfpost.db` (bind mount) |
| Compatibility | 1.x MINOR releases must boot a `1.0.0` data directory |

Last updated with release **1.9.0** (`0009_application_auth_ips`); **1.9.1**
had no schema change.

---

## How migrations run

1. Migrations are embedded at build time (`//go:embed migrations/*.sql`).
2. Filenames are sorted lexicographically; **file order = version number**.
3. `PRAGMA user_version` records progress: after file *i* (0-based), version is
   *i + 1*.
4. Each pending migration runs in its own transaction, then bumps `user_version`.
5. There is no down-migration; fixes ship as a new `00NN_*.sql` file.

```text
0001_init.sql              → user_version 1
0002_sessions.sql          → user_version 2
…
0009_application_auth_ips.sql → user_version 9
```

**Implication:** never delete, rename, or reorder migration files while 1.x must
stay compatible with `1.0.0`. Git history keeps old files; only a 2.x cut may
replace the embedded set (see [Planned 2.x squash](#planned-2x-squash)).

---

## Migration chain

| Ver | File | Shipped | Kind | Summary |
|-----|------|---------|------|---------|
| 1 | `0001_init.sql` | ≤ 1.0.0 | DDL | Core schema: `admin`, `settings`, `domains`, `applications`, `application_addresses`, `send_log`, `rate_limits` |
| 2 | `0002_sessions.sql` | ≤ 1.0.0 | DDL | `sessions` (token hash, sliding idle expiry) |
| 3 | `0003_logtail_state.sql` | 0.5.0 | DDL | `logtail_state` (log-tailer read offset + fingerprint) |
| 4 | `0004_dmarc_report_email.sql` | 1.1.0 | DDL | `admin.dmarc_report_email`, `domains.dmarc_rua` |
| 5 | `0005_panel_users.sql` | 1.2.0 | DDL + data | `users`, `user_domains`; migrate single `admin` row; copy profile `dmarc_report_email` to `settings`; **DROP `admin`** |
| 6 | `0006_inbound_relay.sql` | 1.4.0 | DDL | `inbound_domains`, `inbound_transports`, `inbound_recipients` |
| 7 | `0007_rate_limit_auto.sql` | 1.6.0 | DDL | `rate_limits.mode`, `auto_multiplier`, `auto_updated_at` |
| 8 | `0008_dmarc_reports.sql` | 1.7.0 | DDL | `dmarc_reports`, `dmarc_report_records` |
| 9 | `0009_application_auth_ips.sql` | 1.9.0 | DDL + data | `applications.auth_ip_restrict`, `auth_allowed_ips`; move legacy app `rate_limits.allowed_ips` into auth columns; clear those IPs on rate limits |

**Kind:** *DDL* — schema only; *data* — `INSERT`/`UPDATE` that must stay correct
for operators upgrading from older 1.x images.

Feature plans that introduced schema work: [inbound-relay](plans/inbound-relay.md),
[domain-stats-auto-ratelimit](plans/domain-stats-auto-ratelimit.md),
[dmarc-reports](plans/dmarc-reports.md).

---

## Schema after head (v9)

Tables present in a fully migrated database:

| Table | Introduced | Role |
|-------|------------|------|
| `settings` | 0001 | Key/value panel settings (retention, profile flags) |
| `domains` | 0001 (+ `dmarc_rua` in 0004) | Sending domains |
| `applications` | 0001 (+ auth IP cols in 0009) | SASL applications per domain |
| `application_addresses` | 0001 | Explicit From addresses (`list` mode) |
| `send_log` | 0001 | Delivery journal |
| `rate_limits` | 0001 (+ auto cols in 0007) | Level-2 limits per domain/application |
| `sessions` | 0002 | Panel login sessions |
| `logtail_state` | 0003 | Log-tailer persistence |
| `users` | 0005 | Panel users (`global`, `domain_admin`) |
| `user_domains` | 0005 | Domain-admin assignments |
| `inbound_domains` | 0006 | Inbound relay domains |
| `inbound_transports` | 0006 | Upstream host/port/TLS per inbound domain |
| `inbound_recipients` | 0006 | Allow-list when `recipient_mode = list` |
| `dmarc_reports` | 0008 | Parsed aggregate report summaries |
| `dmarc_report_records` | 0008 | Per-source rows inside a report |

**Not present after v9:** `admin` (dropped in 0005).

---

## Legacy and fresh-install artefacts

These are intentional in 1.x; they are the main motivation for
[schema-squash](roadmap.md#schema-squash) at 2.x.

### `admin` table (0001 → 0005)

On a **new** 1.x data directory the chain still:

1. Creates `admin` (0001),
2. Adds `admin.dmarc_report_email` (0004),
3. Copies the row into `users` and drops `admin` (0005).

Functionally harmless; confusing when reading migrations or inferring schema from
code. A 2.x baseline should define `users` directly and omit `admin`.

### Application trusted IPs (0009)

Before 1.9.0, client IP restriction for an application lived in
`rate_limits.allowed_ips` (`scope = application`). Migration 0009 copies non-empty
values into `applications.auth_allowed_ips`, sets `auth_ip_restrict = 1`, and
clears `rate_limits.allowed_ips` for application scope. Level-2 limits no longer
carry IP bindings; auth and rate limiting are separate concerns.

### Profile DMARC email (0004 → 0005)

`0004` adds `admin.dmarc_report_email`. `0005` copies it into the migrated global
`users` row and into `settings` key `dmarc_report_email`. Per-domain overrides
remain on `domains.dmarc_rua`.

### Documentation references to migration numbers

Other docs cite migrations by number (e.g. architecture § sessions → `0002`).
After a 2.x squash, those references remain valid for **upgrade history** and
git; fresh 2.x installs only run the baseline plus post-2.x files.

---

## Rules for 1.x changes

1. **Add** the next `00NN_descriptive_name.sql`; do not edit shipped migrations.
2. **Bump this file:** snapshot table, chain row, schema table if needed.
3. **CHANGELOG** under the release that ships the migration.
4. **Operator impact:** if upgrade behaviour matters, note it in [guide.md](guide.md).
5. **Backup manifest** is separate: restore requires matching binary version
   ([architecture.md](architecture.md) § Persistence); it does not replace
   running pending SQLite migrations.

### Checklist for a new migration

- [ ] File name is next integer, zero-padded four digits.
- [ ] SQL is idempotent in spirit (runs once per DB; guard with schema state, not
      “IF NOT EXISTS” everywhere unless needed).
- [ ] Data migrations handle empty/partial state (e.g. no `admin` row on re-run is
      impossible; mid-upgrade failure is recovered by re-running the same file
      only if the transaction failed before `user_version` bump).
- [ ] Row added to [Migration chain](#migration-chain) and [Current snapshot](#current-snapshot).
- [ ] `go test ./...` (store and dependents).

---

## Planned 2.x squash

Tracked as [schema-squash](roadmap.md#schema-squash). **Not** a reason to cut 2.x
on its own — only bundled with another breaking change or an explicit major.

**Goal:** embed one baseline SQL file equal to the v9 schema (plus any later 1.x
migrations if 2.x is cut later), instead of the full 1.x chain. Fresh 2.x
`/data` directories skip create-then-drop `admin`.

**Upgrade gate (required when squash ships):**

| `user_version` | 2.x behaviour |
|---|---|
| `0` (empty DB) | Apply baseline; set `user_version` to new chain head |
| `>= 9` (fully migrated 1.x) | Skip; schema already matches baseline |
| `1`…`8` (mid-chain 1.x) | **Refuse to start** — run the last 1.x image once, then 2.x |

Adjust the `>= N` and `1`…`N-1` thresholds to the chain head at cut time.

**Done when:** baseline embedded; gate tested; [guide.md](guide.md) states that
2.x will not open an unfinished 1.x database.

---

## Restore vs migrate

| Mechanism | What it checks |
|-----------|----------------|
| SQLite `user_version` | Which embedded migrations have run on this `selfpost.db` |
| Backup `manifest.json` | Binary/image version after full restore |

An operator can have a matching manifest after restore but still need migrations
if they restored an older DB snapshot with a newer binary — normal `migrate()`
applies pending files. The 2.x gate adds a **refusal** for half-upgraded 1.x DBs
when the old chain is no longer embedded.
