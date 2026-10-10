-- SelfPost 2.0 baseline schema. One SQLite file under /data holds the whole
-- panel state, so a single directory backup/restore is sufficient.
--
-- This file IS the 2.0 schema: the 1.x chain (0001..0010, with its admin table
-- created and dropped again) was deleted, not squashed with an upgrade path —
-- 2.0 starts from an empty data directory (docs/schema-migrations.md). Later
-- changes ship as 0002_*.sql and onwards; this file is never edited again once
-- 2.0.0 is released.

-- Marks the file as a 2.x SelfPost database ("SP20"). store.Open refuses a
-- database that has a schema version but not this mark, which is what a 1.x
-- file looks like: its user_version would otherwise read as "already migrated".
PRAGMA application_id = 1397764656;

-- Free-form key/value settings of the instance: what is true whoever is signed
-- in (send-log retention). Nothing that belongs to a user lives here.
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Panel logins (not application SASL accounts). The presence of a row is what
-- marks primary setup as complete, which is why /setup disappears once one
-- exists. role is a reach, not a rank: 'global' sees the whole instance,
-- 'domain' only what is assigned below.
--
-- email is the user's own address — what the panel writes to. The DMARC
-- default is a separate choice that may use it: dmarc_default_mode says where
-- aggregate reports go for a domain that follows this user
-- (domains.dmarc_rua_user_id): 'hosted' (SelfPost's own mailbox for that
-- domain), 'account' (email above), 'custom' (dmarc_default_address) or 'none'.
--
-- all_domains / all_inbound_domains widen a 'domain' user's list to every
-- domain of that direction, including the ones added later; with a flag set
-- the rows of the matching assignment table are ignored.
CREATE TABLE users (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    username              TEXT NOT NULL UNIQUE,
    password_hash         TEXT NOT NULL,
    role                  TEXT NOT NULL CHECK (role IN ('global', 'domain')),
    email                 TEXT NOT NULL DEFAULT '',
    dmarc_default_mode    TEXT NOT NULL DEFAULT 'none'
                          CHECK (dmarc_default_mode IN ('hosted', 'account', 'custom', 'none')),
    dmarc_default_address TEXT NOT NULL DEFAULT '',
    all_domains           INTEGER NOT NULL DEFAULT 0 CHECK (all_domains IN (0, 1)),
    all_inbound_domains   INTEGER NOT NULL DEFAULT 0 CHECK (all_inbound_domains IN (0, 1)),
    created_at            TEXT NOT NULL
);

-- Panel login sessions. Persisted so a login survives a container restart or
-- redeploy; only the SHA-256 of the session token is stored, never the token
-- itself, so a stolen database file cannot be used to sign in. expires_at
-- implements the sliding idle timeout: it is pushed forward on activity rather
-- than being fixed at creation time. A session belongs to a user by id, not by
-- name: deleting the user ends their sessions, and renaming them leaves the
-- sessions where they are.
CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE INDEX idx_sessions_expires_at ON sessions (expires_at);

-- Sending (outbound) domains. DKIM keys themselves live on disk under /data;
-- this row records the selector and metadata.
--
-- Where the domain's DMARC aggregate reports go (rua=): with dmarc_rua_user_id
-- set, the domain follows that user's default and dmarc_rua is ignored;
-- otherwise dmarc_rua is the address itself, '' meaning no reports. Deleting
-- the user leaves the domain at "none" rather than silently handing its
-- reports to someone else.
CREATE TABLE domains (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    name              TEXT NOT NULL UNIQUE,
    dkim_selector     TEXT NOT NULL,
    dmarc_rua         TEXT NOT NULL DEFAULT '',
    dmarc_rua_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at        TEXT NOT NULL
);

-- Outbound domains assigned to a 'domain' user.
CREATE TABLE user_domains (
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, domain_id)
);

-- Applications bound to a domain. address_mode is either the domain wildcard or
-- an explicit address list; the SASL login is globally unique.
--
-- auth_ip_restrict / auth_allowed_ips restrict which client IPs may submit as
-- the application at all (refused otherwise). That is independent of the
-- level-2 rate limits below.
CREATE TABLE applications (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id        INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    login            TEXT NOT NULL UNIQUE,
    address_mode     TEXT NOT NULL CHECK (address_mode IN ('wildcard', 'list')),
    auth_ip_restrict INTEGER NOT NULL DEFAULT 0,
    auth_allowed_ips TEXT,
    created_at       TEXT NOT NULL
);

-- Explicit sender addresses for applications in 'list' mode. Each address must
-- belong to the application's domain (validated in the panel).
CREATE TABLE application_addresses (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    application_id INTEGER NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    address        TEXT NOT NULL,
    UNIQUE (application_id, address)
);

-- Level-2 rate limits per domain or application. In 'auto' mode max_messages
-- is derived from 30-day send statistics times auto_multiplier.
CREATE TABLE rate_limits (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    scope           TEXT NOT NULL CHECK (scope IN ('domain', 'application')),
    ref_id          INTEGER NOT NULL,
    max_messages    INTEGER,
    window_seconds  INTEGER,
    mode            TEXT NOT NULL DEFAULT 'manual' CHECK (mode IN ('manual', 'auto')),
    auto_multiplier REAL,
    auto_updated_at TEXT,
    UNIQUE (scope, ref_id)
);

-- Structured send log. One row per (queue-id, recipient); the log-tailer
-- advances status from queued to a final state.
CREATE TABLE send_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    queue_id   TEXT,
    domain     TEXT,
    app_login  TEXT,
    from_addr  TEXT,
    to_addr    TEXT,
    subject    TEXT,
    status     TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_send_log_queue_id ON send_log (queue_id);
CREATE INDEX idx_send_log_domain ON send_log (domain);
CREATE INDEX idx_send_log_created_at ON send_log (created_at);
-- The journal milter counts messages for a level-2 limit on every MAIL FROM,
-- and the domain page computes 30-day statistics per application on every
-- render: equality on app_login, range on created_at.
CREATE INDEX idx_send_log_app_login_created_at ON send_log (app_login, created_at);

-- Log-tailer read position. Without it the tailer starts at end-of-file on
-- every start, so delivery lines written while the panel was down are never
-- parsed and their send-log rows stay "queued" forever. One row per followed
-- path; fingerprint identifies the file the offset belongs to (the head bytes
-- of the log), so a rotated or recreated mail.log is detected across a restart,
-- where os.SameFile cannot help.
CREATE TABLE logtail_state (
    path        TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL,
    read_offset INTEGER NOT NULL,
    updated_at  TEXT NOT NULL
);

-- Optional inbound relay (backup-MX / forwarder). A separate entity from the
-- sending domains, even under the same name: these rows exist while
-- INBOUND_RELAY_ENABLE is false, but the listener, the Postfix maps and the
-- panel pages are generated only when that flag is on.
CREATE TABLE inbound_domains (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT NOT NULL UNIQUE,
    recipient_mode TEXT NOT NULL CHECK (recipient_mode IN ('list', 'any')),
    created_at     TEXT NOT NULL
);

-- One upstream per inbound domain (host:port + TLS policy for the hand-off).
CREATE TABLE inbound_transports (
    inbound_domain_id INTEGER PRIMARY KEY REFERENCES inbound_domains(id) ON DELETE CASCADE,
    host              TEXT NOT NULL,
    port              INTEGER NOT NULL CHECK (port >= 1 AND port <= 65535),
    tls_mode          TEXT NOT NULL CHECK (tls_mode IN ('may', 'encrypt', 'none'))
);

-- Explicit recipients for recipient_mode = 'list'. Ignored when mode is 'any'.
CREATE TABLE inbound_recipients (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    inbound_domain_id INTEGER NOT NULL REFERENCES inbound_domains(id) ON DELETE CASCADE,
    address           TEXT NOT NULL,
    UNIQUE (inbound_domain_id, address)
);

-- Inbound domains assigned to a 'domain' user — granted separately from the
-- outbound ones above; the same name on both lists is two assignments.
CREATE TABLE user_inbound_domains (
    user_id           INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    inbound_domain_id INTEGER NOT NULL REFERENCES inbound_domains(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, inbound_domain_id)
);

-- DMARC aggregate report summaries. One row per parsed report; per-source rows
-- hang off it. Forensic (ruf=) payloads are not stored.
CREATE TABLE dmarc_reports (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    domain        TEXT NOT NULL,
    reporter      TEXT NOT NULL,
    report_id     TEXT NOT NULL,
    period_begin  TEXT NOT NULL,
    period_end    TEXT NOT NULL,
    received_at   TEXT NOT NULL,
    contact_email TEXT NOT NULL DEFAULT '',
    policy_p      TEXT NOT NULL DEFAULT '',
    policy_sp     TEXT NOT NULL DEFAULT '',
    policy_pct    INTEGER NOT NULL DEFAULT 100,
    policy_adkim  TEXT NOT NULL DEFAULT '',
    policy_aspf   TEXT NOT NULL DEFAULT '',
    pass_count    INTEGER NOT NULL DEFAULT 0,
    fail_count    INTEGER NOT NULL DEFAULT 0,
    recipient     TEXT NOT NULL DEFAULT '',
    UNIQUE (reporter, report_id, domain)
);
CREATE INDEX idx_dmarc_reports_domain ON dmarc_reports (domain);
CREATE INDEX idx_dmarc_reports_received ON dmarc_reports (received_at);

CREATE TABLE dmarc_report_records (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    report_row_id INTEGER NOT NULL REFERENCES dmarc_reports(id) ON DELETE CASCADE,
    source_ip     TEXT NOT NULL DEFAULT '',
    count         INTEGER NOT NULL,
    disposition   TEXT NOT NULL DEFAULT '',
    spf_result    TEXT NOT NULL DEFAULT '',
    dkim_result   TEXT NOT NULL DEFAULT '',
    header_from   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_dmarc_report_records_report ON dmarc_report_records (report_row_id);
