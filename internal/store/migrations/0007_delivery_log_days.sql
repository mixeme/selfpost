-- What the delivery log's own machinery did, a row per UTC day that has
-- something to say, kept a year (audit 2026-10-12, stage 1). These are the
-- numbers the reconcile sweep's fate is decided by; no page reads them, they
-- are read off a copy of the database. DDL only; nothing existing changes.
--   reconcile_closed        rows the sweep closed as bounced (reason 'reconcile')
--   reconcile_contradicted  rows closed by the sweep that a delivery line later
--                           rewrote - the log wins, and is counted here
--   tailer_blind_seconds    seconds the log tailer could not read mail.log
--                           (the file gone, reopening it failing)
--   queued_over_lifetime    rows still queued past Postfix's queue lifetime,
--                           as measured by the day's last sweep (a level, not
--                           a sum)
CREATE TABLE delivery_log_days (
    day                    TEXT PRIMARY KEY,
    reconcile_closed       INTEGER NOT NULL DEFAULT 0,
    reconcile_contradicted INTEGER NOT NULL DEFAULT 0,
    tailer_blind_seconds   INTEGER NOT NULL DEFAULT 0,
    queued_over_lifetime   INTEGER NOT NULL DEFAULT 0
);
