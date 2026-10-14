-- What Postfix wrote about a refusal (audit 2026-10-12, stage 2b). A message
-- the journal milter refuses at MAIL FROM - a level-2 limit, a client address
-- off the application's list - has no queue id, and Postfix's one line about
-- it, "NOQUEUE: milter-reject: MAIL from ...", was nowhere the message's page
-- could find it. The log tailer puts it on the row it belongs to. A message
-- refused for its From: line has a queue id (cleanup gives one before the
-- headers are judged), and its lines are found by it like any other's.
-- DDL only; a row written earlier has '' and says so on its page.
ALTER TABLE send_log ADD COLUMN refusal_line TEXT NOT NULL DEFAULT '';

-- A refusal row the panel did not get to write - the sender check answers 550
-- and writes the row a moment later, and a panel stopped in between wrote
-- none - is written by the log tailer from Postfix's line about it, and marked
-- so: recovered = 1.
ALTER TABLE send_log ADD COLUMN recovered INTEGER NOT NULL DEFAULT 0;
