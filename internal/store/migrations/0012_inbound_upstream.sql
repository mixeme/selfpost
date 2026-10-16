-- What the upstream answered, on the inbound journal's rows
-- (inbound-upstream-result, 2.10.0; owner, 2026-10-16). A row of mail
-- accepted on port 25 keeps the message's queue id, which the inbound journal
-- milter reads at end-of-message, and starts "queued" ("held" when the filter
-- had it held in the queue); the log tailer puts
-- each delivery attempt's result on it - the status, the host that answered
-- ("" when Postfix had no session), its reply and when - from the postfix/smtp
-- line of that queue id and recipient. sent and bounced are final. A refused
-- message has no queue id and no result, and neither has a row written before
-- this file.
ALTER TABLE inbound_spam_log ADD COLUMN queue_id TEXT NOT NULL DEFAULT '';
ALTER TABLE inbound_spam_log ADD COLUMN upstream_status TEXT NOT NULL DEFAULT ''
    CHECK (upstream_status IN ('', 'queued', 'held', 'sent', 'deferred', 'bounced'));
ALTER TABLE inbound_spam_log ADD COLUMN upstream_relay TEXT NOT NULL DEFAULT '';
ALTER TABLE inbound_spam_log ADD COLUMN upstream_reply TEXT NOT NULL DEFAULT '';
ALTER TABLE inbound_spam_log ADD COLUMN upstream_at TEXT NOT NULL DEFAULT '';

-- The tailer finds a row by its queue id at every delivery line; only rows
-- of accepted mail have one.
CREATE INDEX idx_inbound_spam_log_queue_id ON inbound_spam_log(queue_id) WHERE queue_id <> '';
