# SQLite schema and migrations

**What this file is.** A living reference for the embedded SQLite schema: the
2.0 baseline, the migrations added after it, and how a database is opened.
When you add or review a migration, update this file in the same change.

**Source of truth in code:** `internal/store/migrations/*.sql`, applied by
`internal/store/store.go` (`migrate()`).

**Related:** [architecture.md](architecture.md) § Persistence;
[plans/panel-redesign.md](plans/panel-redesign.md) § No compatibility;
[development.md](development.md) § Documentation.

---

## Current snapshot

| Field | Value |
|---|---|
| Chain head | `user_version = 1` |
| Files | `0001_init.sql` (the 2.0 baseline) |
| File mark | `PRAGMA application_id = 0x53503230` ("SP20") |
| Database file | `/data/selfpost.db` (bind mount) |
| Compatibility | 2.x MINOR releases must boot a `2.0.0` data directory. **Nothing is carried over from 1.x.** |

---

## 2.0 starts from an empty data directory

Decided by the owner on 2026-09-21: the redesign is a breaking release, the
1.x migration chain (`0001` … `0010`, with its `admin` table created in the
first file and dropped in the fifth) is deleted from the binary, and there is
no upgrade path. The only live 1.x installation is the owner's and is set up
again on 2.0. The 1.x chain stays in git history (last present at `1.9.5`).

What that means in practice:

- A fresh `/data` gets the baseline and nothing else.
- **A 1.x database is refused, not migrated.** `store.Open` returns
  `ErrForeignSchema` for a file that has a schema version but not the 2.x
  `application_id`. Without that check a 1.x file at `user_version = 10`
  would read as "already migrated" and the panel would start on tables it
  does not know. The file is not modified. The same applies to restoring a
  1.x full backup into a 2.0 container.
- A database whose version is higher than the build knows is refused too.
- Domains can still be carried by hand: a 1.x **domain export** (JSON, with
  the DKIM key and applications) imports into 2.0 — the transfer format is
  not the database.

---

## How migrations run

1. Migrations are embedded at build time (`//go:embed migrations/*.sql`).
2. Filenames are sorted lexicographically; **file order = version number**.
3. `PRAGMA user_version` records progress: after file *i* (0-based), version is
   *i + 1*.
4. Each pending migration runs in its own transaction, then bumps `user_version`.
5. There is no down-migration; fixes ship as a new `00NN_*.sql` file.

**Implication:** once `2.0.0` is released, `0001_init.sql` is never edited
again and no file is deleted, renamed or reordered — every 2.x database has
already run it. Until that release the baseline is still being written:
later steps of the redesign and the features queued behind it (the next one
is `0002_inbound_spam_log.sql` of inbound-antispam-panel, `2.1.0`) add files
after it.

---

## Migration chain

| Ver | File | Shipped | Kind | Summary |
|-----|------|---------|------|---------|
| 1 | `0001_init.sql` | 2.0.0 | DDL | The whole 2.0 schema, below |

**Kind:** *DDL* — schema only; *data* — `INSERT`/`UPDATE` that must stay correct
for operators upgrading from older 2.x images.

---

## Schema at head (v1)

| Table | Role |
|-------|------|
| `settings` | Key/value settings of the instance — what is true whoever is signed in (send-log retention). Nothing that belongs to a user |
| `users` | Panel logins. `role` is `global` or `domain`; `email` is the user's own address; `all_domains` / `all_inbound_domains` widen a `domain` user to every domain of that direction |
| `sessions` | Panel login sessions (token hash, owning user by id and deleted with them, sliding idle expiry) |
| `domains` | Sending (outbound) domains. `dmarc_rua` is where aggregate reports go (`''` = no reports) |
| `user_domains` | Outbound domains assigned to a `domain` user |
| `applications` | SASL applications per domain, with the client-IP restriction (`auth_ip_restrict`, `auth_allowed_ips`) |
| `application_addresses` | Explicit From addresses (`list` mode) |
| `rate_limits` | Level-2 limits per domain / application, manual or auto |
| `send_log` | Delivery journal |
| `logtail_state` | Log-tailer read position |
| `inbound_domains` | Inbound relay domains |
| `inbound_transports` | Upstream host / port / TLS per inbound domain |
| `inbound_recipients` | Allow-list when `recipient_mode = list` |
| `user_inbound_domains` | Inbound domains assigned to a `domain` user — granted separately from the outbound ones |
| `dmarc_reports` | Parsed aggregate report summaries |
| `dmarc_report_records` | Per-source rows inside a report |

`TestBaselineSchema` (`internal/store/schema_test.go`) pins every table, column
and index of this list.

### What changed against the last 1.x schema

| 1.x | 2.0 | Why |
|---|---|---|
| `admin` created in 0001, dropped in 0005 | never exists | the reason for the squash |
| `users.role` = `global` / `domain_admin` | `global` / `domain` | the role is a reach, not a rank — everyone in the panel is an administrator |
| `users.dmarc_report_email` | `users.email` | a user's e-mail is theirs; it is what the panel writes to, and a domain's report address can be filled with it |
| instance setting `dmarc_report_email` (mirror of the global user's field); `domains.dmarc_rua` NULL = inherit the instance default | `domains.dmarc_rua` alone: the address, `''` = no reports | a domain's report address is one of three explicit choices — no reports, hosted, a typed address; nothing is inherited and no user's default exists (2026-10-10) |
| — | `user_inbound_domains` | inbound is delegated per user, separately from outbound |
| — | `users.all_domains`, `users.all_inbound_domains` | "All" includes domains added later |
| `rate_limits.allowed_ips` (unused since 1.9.0) | gone | client IPs belong to the application (`auth_allowed_ips`) |

### Where a domain's DMARC reports go

`domains.dmarc_rua` is the address itself and the only thing stored; `''` means
no reports, and it is what a new domain has. The choice shown in Domain
settings is derived from it: `''` is no reports, the address SelfPost derives
for the domain and the server's hostname (`dmarc.HostedReportAddress`, offered
while hosted reports are enabled) is hosted, anything else is an address typed
for the domain. Nothing refers to a user: deleting one changes no domain.

---

## Adding a migration

1. Add `internal/store/migrations/00NN_short_name.sql` (next number).
2. Update the table in [Migration chain](#migration-chain), the snapshot at the
   top, and the affected rows of [Schema at head](#schema-at-head-v1).
3. Update `TestBaselineSchema` if tables, columns or indexes change, and add a
   test that opens a database at the previous version and migrates it.
4. A *data* migration states in a comment which 2.x versions it is written for.
