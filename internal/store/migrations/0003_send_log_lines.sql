-- What Postfix wrote about a message, kept with its send-log rows
-- (delivery-log-storage, 2.2.0). DDL only: a 2.1.0 database gains one empty
-- table and nothing in it changes.

-- One row per mail.log line headed by a queue id, written by the log tailer as
-- it reads the log. mail.log is rotated daily and fourteen files are kept; a
-- send-log row lives for the retention window, ninety days by default, and
-- its page shows these lines for as long.
--
-- There is no reference to send_log: the first lines of a message are logged
-- before the journal milter writes its row, and a message with several
-- recipients is several rows under one queue id. A line belongs to a row by
-- queue id and by time (Postfix reuses queue ids), and a line whose queue id
-- has no send-log row is deleted by the tailer's prune.
CREATE TABLE send_log_lines (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    queue_id  TEXT NOT NULL,
    logged_at TEXT NOT NULL,
    line      TEXT NOT NULL
);
-- A message's page reads the lines of one queue id within a span of time.
CREATE INDEX idx_send_log_lines_queue_id ON send_log_lines (queue_id, logged_at);
