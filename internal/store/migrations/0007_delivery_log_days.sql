-- What the delivery log's own machinery did, a row per UTC day that has
-- something to say, kept a year (audit 2026-10-12, stage 1). These are the
-- numbers the reconcile sweep's fate is decided by; no page reads them, they
-- are read off a copy of the database. DDL only; nothing existing changes.
--   reconcile_closed        rows the sweep closed as bounced (reason 'reconcile')
--   reconcile_missed        of those, rows whose delivery line was in mail.log
--                           (or mail.log.1) when the sweep closed them: the
--                           tailer had not read it
--   reconcile_contradicted  rows closed by the sweep that a delivery line read
--                           later gave a status (reason becomes
--                           'reconcile-contradicted'). closed - missed -
--                           contradicted is what the log never resolved: what
--                           removing the sweep would leave queued
--   tailer_blind_seconds    seconds the log tailer could not read mail.log
--                           (the file gone, reopening it failing)
--   queued_over_lifetime    rows still queued past Postfix's queue lifetime
--                           and a backoff, as the day's last sweep saw them -
--                           rows stuck although the sweep runs (a level, not
--                           a sum)
CREATE TABLE delivery_log_days (
    day                    TEXT PRIMARY KEY,
    reconcile_closed       INTEGER NOT NULL DEFAULT 0,
    reconcile_missed       INTEGER NOT NULL DEFAULT 0,
    reconcile_contradicted INTEGER NOT NULL DEFAULT 0,
    tailer_blind_seconds   INTEGER NOT NULL DEFAULT 0,
    queued_over_lifetime   INTEGER NOT NULL DEFAULT 0
);
