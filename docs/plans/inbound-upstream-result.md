# inbound-upstream-result

**Status:** agreed 2026-10-16; one stage, `2.10.0`.

## Problem

The owner, 2026-10-16: "Нам не хватает в журнале для Inbound ответа
upstream. Не понятна судьба письма: принято, отклонено, хост не доступен и
пр."

Inbound › Log shows what port 25 decided - accept, reject, tempfail - and
nothing after it. Its help says so ("Accept says what this server did, not
what the upstream did with the message afterwards"). The answer is in
`mail.log`: Postfix's `smtp` client logs every attempt to hand a message to
its upstream as `<id>: to=<…>, relay=<host>[ip]:port, …, dsn=…, status=sent
(250 …)` - or `deferred (451 4.7.1 Greylisted …)`, `deferred (connect to …:
Connection refused)` with `relay=none`, `bounced (… said: 550 …)` (Postfix
3.10.13, `src/global/log_adhoc.c`; `relay=none` when there was no session,
`src/smtp/smtp_trouble.c:273, 421`). The tailer reads those lines for the
outbound log and, finding no send-log row for a port-25 queue id, drops
them; the journal row does not even hold the queue id.

## Decision

The owner, asked whether the result goes in the row or on a page of its own
per message: "Пока только результат. Отдельная страница в план в случае
появления потребности." So: the row carries the result and the last reply;
no stored `mail.log` lines for port-25 mail (delivery-log-storage left them
out on purpose: a stranger sets the rate). A page per inbound message, with
every line, is for later, if the result is not enough.

## Design

- **Schema, migration 0012:** `inbound_spam_log` gains `queue_id`,
  `upstream_status` (`''`, `queued`, `sent`, `deferred`, `bounced`),
  `upstream_relay`, `upstream_reply` and `upstream_at`; an index on
  `queue_id` for rows that have one. Rows written before have none and show
  "—".
- **The milter** writes an accepted row with the queue id it already reads
  at end-of-message (`{i}`, for the filter's signature) and `queued`. The
  row is in the database before the message is queued, so before any
  delivery line.
- **The tailer:** a delivery line whose queue id has no send-log row is
  offered to the journal: the rows of that queue id and recipient take the
  status, the relay (empty for `none`), the reply (printable, at most 255
  bytes) and the line's time. One update per attempt, no new row; the
  journal's caps stand. qmgr's `status=expired, returned to sender` makes a
  queued or deferred row `bounced`, its reply saying it expired after the
  last one.
- **The page:** an Upstream column after Decision (mockup `5348330`): a
  plain `queued` tag, or a tag opening an "Upstream" sheet - host, reply,
  when, and what follows for deferred and bounced. Refused rows "—".
- **Docs:** help (word for word with the mockup), guide § Inbound log,
  architecture (journal and tailer), security (what is stored from the
  upstream and at what rate), CHANGELOG.

## Steps

- [ ] Migration 0012, store: write the queue id, update by queue id and
  recipient, read the result back.
- [ ] Milter: the queue id and `queued` on accepted rows.
- [ ] Tailer: delivery and expiry lines of port-25 mail to the journal.
- [ ] View, template, help; tests (store, milter, tailer, view).
- [ ] e2e: a relayed message's row shows `sent` with the sink's `250`; an
  upstream that refuses connections shows `deferred` with the reason.
- [ ] Docs; roll out, read the server's journal; cut `2.10.0`.
