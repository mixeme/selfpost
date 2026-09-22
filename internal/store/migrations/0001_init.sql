-- Initial SelfPost schema (spec 9). One SQLite file under /data holds the whole
-- panel state so a single directory backup/restore is sufficient (spec 7.5.A).

-- Single administrator account (spec 7.6.1). Exactly one row is allowed; the
-- presence of that row is what marks primary setup as complete, which is why
-- the /setup route disappears once it exists.
CREATE TABLE admin (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    username      TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL
);

-- Free-form key/value panel settings (retention overrides, misc flags).
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Sending domains managed through the panel (spec 4.1). DKIM keys themselves
-- live on disk under /data; this row records the selector and metadata.
CREATE TABLE domains (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL UNIQUE,
    dkim_selector TEXT NOT NULL,
    created_at    TEXT NOT NULL
);

-- Applications bound to a domain (spec 4.1). address_mode is either the domain
-- wildcard or an explicit address list; the SASL login is globally unique.
CREATE TABLE applications (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id    INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    login        TEXT NOT NULL UNIQUE,
    address_mode TEXT NOT NULL CHECK (address_mode IN ('wildcard', 'list')),
    created_at   TEXT NOT NULL
);

-- Explicit sender addresses for applications in 'list' mode. Each address must
-- belong to the application's domain (validated in the panel, spec 7.6.2).
CREATE TABLE application_addresses (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    application_id INTEGER NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    address        TEXT NOT NULL,
    UNIQUE (application_id, address)
);

-- Structured send log (spec 7.3). One row per (queue-id, recipient); the
-- log-tailer advances status from queued to a final state.
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

-- Differentiated rate limits per domain/application (spec 7.4). Both the IP
-- binding and the message limit are optional.
CREATE TABLE rate_limits (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    scope          TEXT NOT NULL CHECK (scope IN ('domain', 'application')),
    ref_id         INTEGER NOT NULL,
    allowed_ips    TEXT,
    max_messages   INTEGER,
    window_seconds INTEGER,
    UNIQUE (scope, ref_id)
);
