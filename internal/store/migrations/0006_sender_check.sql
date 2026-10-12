-- The sender check: a message's From: line is held against the application
-- that sends it (from-header-binding, 2.4.0). DDL only; nothing existing
-- changes.

-- Why a refused row was refused. Until now "rejected" was one status for
-- three things, and the row did not say which:
--   limit    a level-2 rate limit
--   client   the application's list of client addresses
--   from     the From: line is not the application's
-- A row from before this migration has '' and is shown as plainly rejected.
ALTER TABLE send_log ADD COLUMN reason TEXT NOT NULL DEFAULT '';

-- For a row refused for its From: line, what the header said - cut and
-- reduced to printable characters by the check before it is stored. from_addr
-- stays the envelope sender, the address the application had the right to.
ALTER TABLE send_log ADD COLUMN header_from TEXT NOT NULL DEFAULT '';

-- What the strict check costs, a row per UTC day that has something to say:
-- for how many seconds the panel did not answer while Postfix was running,
-- and how many messages Postfix deferred because the check did not answer.
-- Kept a year. The first day counted is in settings, "sender_check_since".
CREATE TABLE sender_check_days (
    day          TEXT PRIMARY KEY,
    down_seconds INTEGER NOT NULL DEFAULT 0,
    deferred     INTEGER NOT NULL DEFAULT 0
);
