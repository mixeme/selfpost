-- The send log's questions, each answered from an index (audit 2026-10-12,
-- M2; stage 3). Measured with EXPLAIN QUERY PLAN against the statements the
-- store runs (TestSendLogQueriesUseTheirIndexes); before this file each of
-- them walked the table or sorted a domain's whole log.

-- The rows still open - queued or deferred - older than a cutoff: the log
-- tailer asks every five minutes, and nearly every row is older than the
-- cutoff, so only an index of the open rows alone helps. The queries name
-- the two statuses as literals, which is what lets SQLite use it.
CREATE INDEX idx_send_log_open ON send_log (created_at) WHERE status IN ('queued', 'deferred');

-- A page of the log, newest first, of one domain (a domain administrator's
-- view) or one application (the filter): an index of the one column is
-- ordered by id within it, so a page reads its rows and stops. The indexes
-- with created_at after the column answer the counts over a time window and
-- cannot give that order.
CREATE INDEX idx_send_log_domain_id ON send_log (domain_id);
CREATE INDEX idx_send_log_app_login ON send_log (app_login);

-- Whether a line of Postfix's log is already on a row: asked for every
-- refusal line the tailer reads. Only refused rows carry one.
CREATE INDEX idx_send_log_refusal_line ON send_log (refusal_line) WHERE refusal_line <> '';
