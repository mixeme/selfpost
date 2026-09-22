-- Optional inbound relay (backup-MX / forwarder). Separate from sending
-- domains: these rows exist even when INBOUND_RELAY_ENABLE is false, but the
-- listener, Postfix maps and panel UI are generated only when that flag is on.

CREATE TABLE inbound_domains (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL UNIQUE,
    recipient_mode  TEXT NOT NULL CHECK (recipient_mode IN ('list', 'any')),
    created_at      TEXT NOT NULL
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
