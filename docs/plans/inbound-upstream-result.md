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

## Reviews (2026-10-16)

A technical and a security review of the tree before the commit; what each
finding became:

- *A quoted RCPT TO* (both): the milter kept `"owner"@x` as given, every line
  says `owner@x` - the row never matched. Now `recipientAsLogged`, as the
  outbound milter does.
- *A forged line through a quoted local part* (security, medium): `to=<…>`
  was read up to its first `>`; a local part in quotes may hold `>`, `,`,
  `status=`. Port 25 refuses the one that would reach another row (an `@` in
  the local part is a routed address: `src/trivial-rewrite/resolve.c:467`,
  `src/smtpd/smtpd_check.c:1724`, `allow_untrusted_routing=no`), and the
  pattern now reads addresses by Postfix's quoting all the same
  (`loggedAddress`).
- *Held mail* (technical): a message the filter had held showed `queued`,
  "not tried yet", for as long as it stayed held. Now `held`, a status of its
  own (the CHECK of 0012, not shipped yet).
- *Rows that never close, and queue ids that repeat* (both): before writing
  a message's rows the milter detaches older rows of its queue id; cleanup's
  `removed (canceled)` detaches rows of a message never queued; an empty
  `{i}` writes no result.
- *A bounce of null-sender mail* (technical): "The sender was sent a
  bounce" is wrong there - Postfix does not bounce a bounce. The sheet says
  so.
- Accepted, said in security.md: a row whose insert waited past the milter's
  30 s timeout can miss its first line and stay `queued`. Not of this row:
  an old send-log row of a reused queue id and the same recipient takes the
  line first (sendlog.go, as before).

## Steps

- [x] Migration 0012, store: write the queue id, update by queue id and
  recipient, read the result back.
- [x] Milter: the queue id and `queued` on accepted rows.
- [x] Tailer: delivery and expiry lines of port-25 mail to the journal.
- [x] View, template, help; tests (store, milter, tailer, view).
- [x] e2e: a relayed message's row shows `sent` with the sink's `250`; an
  upstream that refuses connections shows `deferred` with the reason.
- [x] Docs. *Done (2026-10-16), with the reviews' findings above.*
- [ ] Roll out, read the server's journal; cut `2.10.0`.
