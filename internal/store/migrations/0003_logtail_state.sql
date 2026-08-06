-- Log-tailer read position. Without it the tailer starts at end-of-file on
-- every start, so delivery lines written while the panel was down are never
-- parsed and their send-log rows stay "queued" forever. One row per followed
-- path; fingerprint identifies the file the offset belongs to (the head bytes
-- of the log), so a rotated or recreated mail.log is detected across a restart,
-- where os.SameFile cannot help.
CREATE TABLE logtail_state (
    path        TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL,
    read_offset INTEGER NOT NULL,
    updated_at  TEXT NOT NULL
);
