# Plan: from-header-binding

**Status:** shipped in `2.4.0` (2026-10-12); agreed the same day - the design questions were closed with the owner one at a time and he took it into work ("Да")  
**Date:** 2026-10-12  
**Version:** `2.4.0` MINOR (one migration; it refuses mail that was accepted before).

---

## Goal

An application sends only as itself - in the `From:` line a mail program
shows and OpenDKIM chooses the key by, not only in the envelope.

Until now Postfix held the envelope sender against the SASL login and nothing
held the header. An application of one sending domain could write another
registered domain, or the server's own name, into `From:` and have the message
signed with that name's key (security review of panel-notifications,
2026-10-12).

## Decisions (owner, 2026-10-12)

### The rule

**`From:` is held to what the envelope is held to.** An application set to
any address of its domain may write any address of that domain; one with a
list of addresses, only those. Anything else is refused: another registered
domain, the server's name, an address at a domain this server knows nothing
of, no `From:` at all.

I proposed the narrower rule - refuse only a `From:` that names a domain this
server signs for, and let a stranger's address through unsigned as before -
and, for an application with a list, to check the domain alone. The owner
chose the wider one both times. What it stops working: a form that puts its
visitor's address into `From:`, and an application with a list whose envelope
address and header address differ, until the second is added to the list.

When the hostname is itself a sending domain its applications write
`…@<hostname>` by right, as the guide already says.

### Where it is checked, and what happens when it cannot be

**The panel checks, and the check is strict:** while the panel does not
answer, Postfix replies with a temporary error and accepts nothing unchecked.

I first proposed the opposite (fail-open, as the journal and the level-2 limit
are) and then a small process of its own. The owner's objection to the first:
a check that is absent while the panel is down does not close the hole, and
the panel only has to be slow - 15 seconds - for Postfix to go on without it.
His answer to the second: not yet; measure first.

So the check is a role of the panel process on a socket of its own, attached
with `default_action=tempfail`. The journal milter keeps its socket and stays
fail-open: a slow database still does not defer mail. The check reads no
database - it reads the file of sender bindings Postfix itself reads for the
envelope, so the two cannot disagree - and moving it into a process of its own
later is a change of where it is started.

### Statistics, to decide whether the check needs its own process

Per day, kept a year:

- **for how long the panel did not answer while Postfix was running.** The
  panel marks itself alive every few seconds; at start the gap since the last
  mark is its downtime, counted when Postfix was not restarted within it (a
  rollout or a reboot stops mail for another reason);
- **how many messages were deferred because the check did not answer**, read
  from `mail.log`. This one also sees a panel that is running and not
  answering, which the mark does not.

Shown on **Server › Health** in a card of its own, *Sender check*: today, the
last 30 days, the last 12 months. The card warns when a message was deferred
in the last 24 hours; downtime that deferred nothing warns of nothing.

### What a refusal looks like

- **To the application:** `550 5.7.1`, permanent, naming the address. A
  level-2 limit answers 4xx because the message will pass in an hour; a wrong
  `From:` will not, and a 4xx would have the application send it again for
  days.
- **In the Outbound log:** a row with the reason in the status tag -
  `wrong From`, beside `rate-limited` and the tag of a refused client address.
  The built panel wrote `rejected` for all of them; it follows the mockup now.
  (The mockup's status filter is not built - the log filters by domain and
  application - so there is nothing to keep in step with the tags.)
- **On the message's page:** a fact *From: line* with the address written in
  the header, and a History step that says which address was refused, for
  which application, what that application may write, and that it was told
  `550 5.7.1`.

### Health, re-laid

The owner on the first picture of the new card: too many holes on the screen,
the numbers should follow importance, and the boxes should read left to right
and top to bottom - with stacked columns the third box was below the fold and
the fourth was not.

- Order: 01 Processes, 02 Milter sockets, 03 Sender check, 04 TLS
  certificate, 05 Hostname and reverse DNS, 06 Notification mail, 07 Machine,
  08 Configuration.
- Layout `cards` (new, on the components page): two stacks, odd numbers on the
  left and even on the right, each closed up, so a short box leaves no gap
  beside a tall one and the eye still goes 01, 02, 03. On a phone the boxes
  come in the order of their numbers.

## Settled while building

- **The header is read twice.** One `From:` field holding one address, by
  Go's parser; then every `@name` of the raw value is held against OpenDKIM's
  own `SigningTable`. The first build took the names for the second look from
  the sender bindings, and the e2e stand let a line through that named a
  domain with a key and no application yet: a domain is a signing name from
  the moment it is added, not from its first application.
- **What Postfix writes when the check does not answer** (seen on the stand):
  a warning naming the socket, then `milter-reject: CONNECT … 451 4.7.1
  Service unavailable` and the same for `EHLO`; the client is told `454 4.3.0
  Try again later` at `AUTH` and leaves. No `MAIL` is ever offered, so what
  is counted is the connection turned away, once. The journal milter's
  level-2 refusal is written in the same words, which is why a reject line
  counts only after the warning.
- **When Postfix started** is asked of supervisord (`supervisorctl status`,
  the uptime of `postfix`).
- **The reply** is a milter reply code, `550 5.7.1 From: <line> - <why>`; what
  the client wrote is reduced to printable ASCII and cut first.
- **The Delivery log box stays on a refused message's page**, with the one
  sentence it already had for a message that never reached the queue; the
  picture shown to the owner had left the box out.

## To settle while building (as first written)

- How the header is read so that the check and OpenDKIM cannot disagree: one
  `From:` field, one address, and a cruder second look at the raw value.
- What Postfix writes in `mail.log` when a strict milter does not answer, and
  how a line is told to be this milter's and not OpenDKIM's.
- How the panel learns when Postfix started.
- Schema: the reason of a refusal and the header address on a send-log row;
  the table of days.
- That a refusal for `From:` is not taken for a refusal under a limit by the
  notifier.

## Implementation checklist

- [x] Mockups: Health card and layout, the log row, help topic; accepted by the owner 2026-10-12 (`design:` commit)
- [x] Migration and store: refusal reason and header address, days of the sender check (`0006_sender_check.sql`)
- [x] The check: a role of the panel on its own socket, bindings read from Postfix's file; Postfix attaches it strictly; start order
- [x] Statistics: the alive mark and downtime, deferred mail from `mail.log`
- [x] Panel: Health (card, sockets row, layout), Outbound log tags, the message's page, help
- [x] guide.md, architecture.md, security.md (the accepted risk goes), schema-migrations.md
- [x] `go vet`, `go test`, e2e: refused for another domain's `From:`, for the hostname's and for an address off the list; accepted for its own (on the test server: vet ok, 23 packages ok, e2e ok - `sender_check_holds_the_from_line`, `panel_down_defers_mail`)
- [x] Technical review — **Opus** (2026-10-12; one Critical, fixed: a folded `From:` line - what a mail library makes of a long display name - was refused for good. Also fixed: a display name in a charset Go does not convert was refused; the statistics ran before the check served, and a hung `supervisorctl` would have kept it from serving; the count of deferred mail missed a check that stops answering in the middle of a message, and is now taken at Postfix's warning; a card whose figures could not be read said ok; a killed container's stale socket satisfied the wrapper's wait; outside quoted strings the line may now hold one `@` only. Three were first left as found and fixed the same evening at the owner's question: downtime is cut at the UTC midnights it crossed; the row of a message with the null envelope sender takes the domain the login is bound to; a log read again after an unclean stop does not count again what it counted before)
- [x] Security review — **Fable** (2026-10-12; no Critical or High. The same folded line, as its one Medium. Fixed: another sender's name in an encoded display name passed the second look; a client could arm the deferred counter by writing the socket's name where Postfix quotes it in a warning; a refused message wrote a row per recipient, a thousand at most - now one. Recorded: what a display name looks like to a person is not promised. The review also found the check stricter than Postfix for an address with an extension (`invoices+x@`, allowed in the envelope and refused in the header); the owner asked for it to work, and the check now makes Postfix's second try, with `recipient_delimiter` set explicitly. Confirmed from the code: no path to the signer around the check, and every failure of the check defers)

## Not done

Written at the owner's word when he had the version cut ("Запиши все
недоделки в план", 2026-10-12). Nothing here was promised and dropped in
silence; each line says what is missing and what it would take.

### Decided to wait

- **A process of its own for the check.** The owner: measure first. Server ›
  Health now has the numbers; the decision is his when they have had time to
  say something. Moving the role out is a change of where it is started -
  a subcommand of the panel binary and a `[program:]` in supervisord - and
  the downtime figure then needs a new meaning.

### Shown to the owner one way and built another

- **The Delivery log box is on a refused message's page.** The picture he
  approved had left it out; the built page keeps it, with one sentence. And
  the sentence is less than the truth for a `From:` refusal: cleanup did
  write a line about it under a queue id (`milter-reject: END-OF-MESSAGE ...
  5.7.1 From: ...`), but the check answers before the queue id reaches it, so
  the row keeps none and the page cannot show that line. **Decided
  2026-10-13 (owner):** the box is required - he had overlooked its absence
  from the picture, it was never a decision to drop it. The tailer attaches
  the line to the row, for all three kinds of refusal; a step of
  [audit-2026-10-12](../audit-2026-10-12.md) stage 1.
- **The Outbound log has no status filter.** The mockup draws one; the built
  log filters by domain and application. With three kinds of refusal now
  told apart by tag, a filter by status and reason would be worth having.
  **Decided 2026-10-13 (owner):** a filter by status, without the split by
  reason, and a filter by day as the Inbound log has; a step of [audit-2026-10-12](../audit-2026-10-12.md) stage 4.
- **"Messages deferred" counts connections Postfix turned away.** Postfix
  refuses from the connection on, before a message is offered, so a monitor
  that only connects during an outage is counted. The guide says so; the
  card's column head and the help topic still say "messages".

### Known and small

- **"Today" on the card is the UTC day**, and the card does not say so.
- **A deferral in the very second of the last one counted before an unclean
  stop is passed over** at the restart: `mail.log` stamps whole seconds.
  Chosen over counting a stretch of the log twice.
- **Deferrals are lost when `mail.log` is rotated while the panel is down**:
  the tailer starts on the new file.
- **A start at which `supervisorctl` fails counts no downtime**; it is
  logged.
- **Logins are compared as written**, Postfix compares them without regard
  to case. The panel writes logins one way, so nothing differs today.
- **The phone order of the `cards` layout is written for four boxes to a
  stack** and takes every child of a stack for a box (said at the partial).
  The rule the technical review suggested instead - the order from the
  box's own number - changes the component and would be drawn first.
- **Health's socket check looks at the file, not at who listens**: a socket
  nobody answers on would read "ok". The start now removes stale sockets,
  which was the one way to get such a file.
- **The kit page's table of status tags does not list the new ones**
  (`wrong From`, `wrong client`, `rate-limited`), and `rate-limited` is grey
  beside two red tags - as the mockup has it, not as a decision.
- **Refusal rows still in the buffer when the panel stops are not written**;
  the refusals themselves were answered.

### Accepted, in security.md

- **The panel's downtime is mail's.** The price of the strict check.
- **What a display name looks like to a person is not promised.** A name of
  another sender written without an `@` the scan can find passes, signed
  with the application's own key.
- **A malformed packet on the check's socket ends the panel** (the milter
  library panics on an empty one). The socket is reachable only inside the
  container, by the group Postfix is in; the journal milter always had this,
  and what is new is that the price is deferred mail.

### Not verified

- **OpenDKIM's reading of a `From:` line** was reasoned about, not run: its
  parser is not in this repository. The two rules that do not depend on it -
  no other signing name after any `@`, one `@` outside quotes - are what the
  claim rests on. A test that feeds the stand's OpenDKIM the lines the check
  passes and reads which key it signs with would settle it.
- **The pages were not looked at in a browser on the deployed panel**: I
  cannot sign in there. They were rendered from the fixtures and
  screenshotted, the polling and the phone order read from the code and
  exercised by e2e only as far as the Health numbers.
- **No refusal has been seen on the test server itself**, only on the e2e
  stand: there is no application there I can send as.
- **A check that stops answering in the middle of a message** is counted
  from a warning composed from Postfix's source; the stand only produced the
  case of nobody listening.
