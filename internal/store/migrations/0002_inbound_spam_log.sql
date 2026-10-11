-- Inbound antispam journal and filter lists (inbound-antispam-panel, 2.1.0).
-- The first migration after the 2.0 baseline: DDL only, so a 2.0.x database
-- gains two empty tables and nothing in it changes.

-- What happened to a message offered on port 25, one row per recipient.
-- Metadata only - SelfPost relays inbound mail and stores none of it. Accepted
-- messages are written by the inbound journal milter, refused ones by the
-- mail.log tailer, which is why a row can lack a subject (refused before DATA)
-- and why engine says who decided: the external filter, Postfix's own policy,
-- or nobody (selfpost: relayed with no filter attached).
CREATE TABLE inbound_spam_log (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    inbound_domain TEXT NOT NULL,
    client_ip      TEXT NOT NULL,
    from_addr      TEXT NOT NULL,
    to_addr        TEXT NOT NULL,
    subject        TEXT NOT NULL,
    decision       TEXT NOT NULL CHECK (decision IN ('accept', 'reject', 'tempfail', 'quarantine')),
    engine         TEXT NOT NULL CHECK (engine IN ('rspamd', 'postfix', 'selfpost')),
    detail         TEXT NOT NULL,
    created_at     TEXT NOT NULL
);
-- Retention prunes by age; the Log page lists newest first, for the whole
-- server or for one domain.
CREATE INDEX idx_inbound_spam_log_created_at ON inbound_spam_log (created_at);
CREATE INDEX idx_inbound_spam_log_domain_created_at ON inbound_spam_log (inbound_domain, created_at);

-- Allow / deny entries for the external filter, instance-wide. The panel
-- writes them out as map files under /data/antispam/; SelfPost itself does not
-- consult them. value is stored normalised, so the same entry cannot be added
-- twice to one list.
CREATE TABLE inbound_spam_list (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    list_type  TEXT NOT NULL CHECK (list_type IN ('allow', 'deny')),
    entry_type TEXT NOT NULL CHECK (entry_type IN ('address', 'domain', 'ip')),
    value      TEXT NOT NULL,
    note       TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (list_type, entry_type, value)
);
