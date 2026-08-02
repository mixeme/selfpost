-- Panel login sessions (spec 7.6.6, plan B.1). Persisted so a login survives a
-- container restart or redeploy; only the SHA-256 of the session token is
-- stored, never the token itself, so a stolen database file cannot be used to
-- sign in. expires_at implements the sliding idle timeout: it is pushed
-- forward on activity rather than being fixed at creation time.
CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    username   TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE INDEX idx_sessions_expires_at ON sessions (expires_at);
