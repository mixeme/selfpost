-- The tenant boundary (audit 2026-10-12, stage 2).

-- An application login is unique without regard to case. Postfix compares
-- the SASL login with the sender map's without regard to case, so "Ops" and
-- "ops" were two applications to the panel and one sender to Postfix: a
-- tenant could take another's envelope sender (audit H4). New logins are
-- folded to lower case by the panel; a login stored earlier keeps its case,
-- since its SASL account and its client use it. The column's own UNIQUE
-- (case-sensitive) stays: SQLite cannot change a column's collation without
-- rebuilding the table, and this index is the stricter rule.
CREATE UNIQUE INDEX idx_applications_login_nocase ON applications (login COLLATE NOCASE);

-- A send-log row belongs to a domain by its id, not its name (audit L12; the
-- owner, 2026-10-12). A domain deleted and added again under its old name
-- used to show the earlier rows to whoever it was assigned to next. The id is
-- the domain of the login the row was written for; a row of a domain deleted
-- since - and every row older than this migration whose domain is gone - is
-- NULL and nobody's but the global role's. The name stays for display and for
-- the global view.
--
-- Filled once, by the rule new rows follow: the domain of the row's
-- application, then - for a row whose application is gone - the domain of the
-- row's name. Either only if it already existed when the row was written: a
-- domain deleted and added again before this migration must not inherit the
-- rows of the one before (review of audit stage 2). Times are RFC3339 UTC
-- text, so they compare as times.
ALTER TABLE send_log ADD COLUMN domain_id INTEGER REFERENCES domains(id) ON DELETE SET NULL;
UPDATE send_log SET domain_id = COALESCE(
    (SELECT a.domain_id FROM applications a JOIN domains d ON d.id = a.domain_id
      WHERE a.login = send_log.app_login
        AND a.created_at <= send_log.created_at AND d.created_at <= send_log.created_at),
    (SELECT d.id FROM domains d
      WHERE d.name = send_log.domain AND d.created_at <= send_log.created_at));
CREATE INDEX idx_send_log_domain_id_created_at ON send_log (domain_id, created_at);

-- The inbound journal follows the same rule, so the two logs do not follow two
-- (security.md's accepted risk of the inbound log, struck).
ALTER TABLE inbound_spam_log ADD COLUMN inbound_domain_id INTEGER REFERENCES inbound_domains(id) ON DELETE SET NULL;
UPDATE inbound_spam_log SET inbound_domain_id = (SELECT d.id FROM inbound_domains d
    WHERE d.name = inbound_spam_log.inbound_domain AND d.created_at <= inbound_spam_log.created_at);
CREATE INDEX idx_inbound_spam_log_domain_id_created_at ON inbound_spam_log (inbound_domain_id, created_at);
