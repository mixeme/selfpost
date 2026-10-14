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
