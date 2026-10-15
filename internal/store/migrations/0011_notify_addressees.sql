-- Who a notification is owed to, per subject (audit 2026-10-12, stage 4;
-- owner, 2026-10-14). A subject used to count as said once one recipient's
-- copy was handed to Postfix: a recipient whose copy failed, or was held back
-- by the hourly ceiling, was never told. The recipients a subject's first
-- message was addressed to are kept here with whether each was handed it;
-- a later check sends to those not told yet, and "cleared" goes to those who
-- were told. Rows of a subject go with its notify_state row
-- (Store.DeleteNotifyState). A subject opened before this file has none, and
-- is told cleared to every recipient, as before.
CREATE TABLE notify_addressees (
    key     TEXT NOT NULL,
    address TEXT NOT NULL,
    told_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (key, address)
);
