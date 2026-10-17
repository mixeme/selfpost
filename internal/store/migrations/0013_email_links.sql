-- A confirmed account e-mail, and the one-time links that confirm one and
-- reset a forgotten password (password-reset, 2.14.0; owner, 2026-10-13: "the
-- e-mail must be confirmed"; 2026-10-17: notifications go to it confirmed or
-- not). email_confirmed_at is when the address in email was confirmed, ''
-- while it is not; changing the address clears it. Every address saved before
-- this file is unconfirmed.
ALTER TABLE users ADD COLUMN email_confirmed_at TEXT NOT NULL DEFAULT '';

-- A link's token is in a mail and nowhere else: only its SHA-256 is kept, as a
-- session's. email is the address a confirmation link confirms - it fails
-- once the account's address is another - and the one a reset link was sent
-- to. A link is taken once; a new one of the same purpose replaces the old.
CREATE TABLE user_links (
    token_hash TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose    TEXT NOT NULL CHECK (purpose IN ('confirm', 'reset')),
    email      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE INDEX idx_user_links_user ON user_links(user_id, purpose);
