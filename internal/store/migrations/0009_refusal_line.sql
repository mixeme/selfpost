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

-- The Sender check card counts two things apart (the owner, 2026-10-14):
-- deferred stays the connections Postfix turned away while the check did not
-- answer, and messages are those deferred in the middle, when the check
-- stopped answering after a message had begun (cleanup's warning). estimated
-- marks a day whose downtime was taken from the alive mark alone, because
-- supervisord could not say at start since when Postfix ran: an upper bound.
ALTER TABLE sender_check_days ADD COLUMN messages INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sender_check_days ADD COLUMN estimated INTEGER NOT NULL DEFAULT 0;
