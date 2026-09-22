-- Panel users and domain-admin assignments. Migrates the single admin row into
-- a global user; drops the admin table.

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

INSERT INTO users (username, password_hash, role, dmarc_report_email, created_at)
SELECT username, password_hash, 'global', dmarc_report_email, created_at
FROM admin WHERE id = 1;

INSERT OR REPLACE INTO settings (key, value)
SELECT 'dmarc_report_email', dmarc_report_email FROM admin WHERE id = 1;

DROP TABLE admin;
