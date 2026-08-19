-- Send-log lookups by application login run on two hot paths: the journal
-- milter counts messages for a level-2 rate limit on every MAIL FROM, and the
-- domain page computes 30-day statistics per application on every render.
-- 0001 indexed queue_id, domain and created_at, but not app_login, so both
-- fell back to scanning the created_at range. The composite matches the shape
-- of those queries (equality on app_login, range on created_at).

CREATE INDEX IF NOT EXISTS idx_send_log_app_login_created_at
    ON send_log (app_login, created_at);
