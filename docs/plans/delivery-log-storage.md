# Plan: delivery-log-storage

**Status:** agreed 2026-10-12 (owner: option 1 of the roadmap item, "store the lines")  
**Date:** 2026-10-12  
**Version:** `2.2.0` MINOR (one DDL migration; a 2.1.0 data directory boots unchanged).

---

## Goal

A message's page (`/outbound/log/{id}`, mockup `out-message`) shows what
Postfix wrote about the message for as long as its row is in the send log.
Today the *Delivery log* box is filled by a search of the current `mail.log`
on every page view, so it is empty for anything sent before the last daily
rotation, while the row itself lives for ninety days.

## Context (as-built)

- The log tailer ([internal/logtail](../../internal/logtail/logtail.go))
  already reads every `mail.log` line once, from a persisted offset, to
  advance delivery statuses.
- `logtail.QueueLines` searches the last 4 MB of the current file for one
  queue id; `Handlers.deliveryLog` calls it per page view.
- A message with several recipients is several send-log rows under one queue
  id; the page of each shows every line of the message, as the search does
  today.

## Decisions

### What is stored

Every `mail.log` line whose head is a queue id - after the timestamp, the host
name and `program[pid]:` - goes into a new table with that id, the line's own
time (`lineTime`, the same reading the inbound journal uses) and the line as
written, cut to 4096 bytes. The head is matched from the start of the line:
the tail is the far end's words. `NOQUEUE` and `warning` are not queue ids -
the pattern takes Postfix's short form (hexadecimal) and its long form (no
vowels) and nothing else.

The tailer does not ask whether the id belongs to a send-log row before it
writes. The first lines of a message (`client=`, `message-id=`) are logged
before the journal milter writes the row at end-of-message, so such a check
would drop exactly them. Lines of messages that never get a row - inbound
relay mail, bounces Postfix generates - are stored too and removed by the
prune below within the hour or two.

### Which lines are a row's

A line is shown on a row's page when the queue id is the same **and** the
line's time lies between one hour before the row was written and the queue
lifetime (the larger of `maximal_queue_lifetime` and `bounce_queue_lifetime`,
as `postconf` reports them) plus a day after it. Postfix reuses a short queue
id once the message has left the queue; a later message under the same id
falls outside that window. This is the bound the roadmap item asked for.

### Retention

Lines live as long as a send-log row with their queue id does. One rule covers
both ends: a line older than an hour whose queue id has no send-log row is
deleted. That removes the lines of a row the retention sweep has just deleted,
and the lines of messages that never had a row. It runs with the send-log
sweep and once an hour between.

There is no cap of its own: the table is bounded by what Postfix logs about
queued messages, which its own rate limits bound.

### After an unclean stop

The tailer saves its place every few seconds, so after a crash it reads some
lines twice. While it is catching up at start a line is written only if the
same line is not there already - the rule the inbound journal follows.

### The page

The page reads the table: the latest 200 lines of the row, oldest first. When
the table has none, it still searches the current `mail.log` as before - that
is what shows a message accepted before 2.2.0 while its lines are in today's
file. Lines in rotated files are not imported: the only installation is the
owner's test server.

Nothing in the page's layout changes; the `out-message` mockup already draws
the box as a `log_pane`. Two texts change, mockup first: the *Outbound log*
help topic (it says an old message may show no lines) and the note shown when
there are none.

### Size

Six to ten lines of about 200 bytes per message: 1-2 KB next to a row of a few
hundred bytes. The table becomes the larger part of what the send log costs in
`/data` and in a backup; the retention setting bounds both together.

## Schema (migration `0003_send_log_lines.sql`)

```sql
CREATE TABLE send_log_lines (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    queue_id  TEXT NOT NULL,
    logged_at TEXT NOT NULL,
    line      TEXT NOT NULL
);
CREATE INDEX idx_send_log_lines_queue_id ON send_log_lines (queue_id, logged_at);
```

No foreign key: a line is written before its row exists, and one queue id is
several rows.

## Scope

In: the table, the tailer writing and pruning it, the page reading it, the two
texts, docs.

Out: the inbound journal (it decides for itself, roadmap § delivery-log-storage);
importing rotated files; `enable_long_queue_ids`; any change to `mail.log`
rotation or to the Server › Mail log page.

## Security

- The lines carry the far end's text (replies, HELO names). They are shown
  today from the file through the same escaping; storing them changes where
  they are read from, not what is shown or to whom. A domain administrator
  sees the lines of messages of their own domains, as now.
- They now enter the database and so a full backup, which `log/` does not.
- A line is matched from its head, so a reply cannot make itself a line of
  another message; Postfix does not log raw line breaks.

## Done when

A message sent before the last rotation shows its lines on its page; the lines
disappear with the row; the page's wording no longer blames rotation for an
ordinary case.

## Model routing (this plan)

| Role | Model |
|------|-------|
| All implementation (code, tests, docs) | **Opus** |
| Technical review (reviewer ≠ author: a separate read-only agent) | **Opus** |
| Security review | **Fable** |

## Implementation checklist

Target version cut: **`2.2.0`** (MINOR). One commit per step;
[development.md](../development.md) § Plan checklists.

- [x] Migration `0003_send_log_lines.sql`, store methods (insert, insert once, lines of a row, prune) and their tests — **Opus**
- [ ] Log tailer: store queue-id lines, write once while catching up, prune with the send-log sweep and hourly — **Opus**
- [ ] The two texts, mockup first: the *Outbound log* help topic and the empty note (`design:` commit) — **Opus**
- [ ] Message page reads the table, falls back to the current `mail.log`; help text in `help.go` — **Opus**
- [ ] [architecture.md](../architecture.md), [guide.md](../guide.md), [schema-migrations.md](../schema-migrations.md) — **Opus**
- [ ] `go vet`, `go test`, e2e: a message's page still lists its lines after a forced rotation — **Opus**
- [ ] Technical review — **Opus**
- [ ] Security review — **Fable**
