-- The notifier asks every minute for the mail refused in the last day
-- (panel-notifications, 2.3.0). Refusals are a sliver of the send log; without
-- an index of their own the question walks the whole retention period each
-- time. A partial index holds only the refused rows. The query names the
-- status as a literal, which is what lets SQLite use it.
CREATE INDEX idx_send_log_rejected ON send_log (created_at) WHERE status = 'rejected';
