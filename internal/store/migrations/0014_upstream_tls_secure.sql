-- The upstream's TLS mode may be 'secure' (upstream-tls-verify, 2.15.0; owner,
-- 2026-10-18; the security audit of 2026-10-17, M7): Postfix's level that
-- encrypts and checks the upstream's certificate against its name. The CHECK
-- of a column cannot be altered in SQLite, so the table is made anew with the
-- wider one and its rows copied (https://sqlite.org/lang_altertable.html § 7).
-- Nothing references inbound_transports, so it can be dropped with foreign
-- keys on; its own reference to inbound_domains goes over with the new table.
CREATE TABLE inbound_transports_new (
    inbound_domain_id INTEGER PRIMARY KEY REFERENCES inbound_domains(id) ON DELETE CASCADE,
    host              TEXT NOT NULL,
    port              INTEGER NOT NULL CHECK (port >= 1 AND port <= 65535),
    tls_mode          TEXT NOT NULL CHECK (tls_mode IN ('may', 'encrypt', 'secure', 'none'))
);
INSERT INTO inbound_transports_new (inbound_domain_id, host, port, tls_mode)
    SELECT inbound_domain_id, host, port, tls_mode FROM inbound_transports;
DROP TABLE inbound_transports;
ALTER TABLE inbound_transports_new RENAME TO inbound_transports;
