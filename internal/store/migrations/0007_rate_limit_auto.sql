-- Level-2 rate limit auto mode: operator sets a multiplier; max_messages is
-- derived from 30-day send stats (plan domain-stats-auto-ratelimit).

ALTER TABLE rate_limits ADD COLUMN mode TEXT NOT NULL DEFAULT 'manual'
  CHECK (mode IN ('manual', 'auto'));
ALTER TABLE rate_limits ADD COLUMN auto_multiplier REAL;
ALTER TABLE rate_limits ADD COLUMN auto_updated_at TEXT;
