-- Notifications: the panel writes to its users when something needs them
-- (panel-notifications, 2.3.0). DDL only; nothing existing changes, and every
-- user starts with notifications on.

-- A user's own switch, on Account. It decides nothing by itself: a user is
-- written to when it is on, the profile has an e-mail and the instance switch
-- (settings key "notifications") is on.
ALTER TABLE users ADD COLUMN notify INTEGER NOT NULL DEFAULT 1 CHECK (notify IN (0, 1));

-- What the panel has already said, so that it says each thing once and a
-- restart announces nothing again. One row per subject:
--   dns:<domain>            a sending domain's records
--   cert                    the TLS certificate
--   queue                   a message older than three days
--   limit:<domain>          level-2 limits that refused mail of the domain
-- phase is where the subject stands ("pending": seen wrong, not yet said;
-- "open": said; for the certificate, the threshold last said). count is the
-- checks in a row that agree with the phase's direction, mark what the phase
-- refers to (the certificate's expiry; for DNS, when the records were first
-- seen right again), since when it began, sent_at when the last message about
-- it went out.
CREATE TABLE notify_state (
    key     TEXT PRIMARY KEY,
    phase   TEXT NOT NULL,
    count   INTEGER NOT NULL DEFAULT 0,
    mark    TEXT NOT NULL DEFAULT '',
    since   TEXT NOT NULL,
    sent_at TEXT NOT NULL DEFAULT ''
);
