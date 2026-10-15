# What SelfPost assumes of other programs - the register of 2026-10-14

The heuristics review of audit-2026-10-12, stage 2c. The owner's rule
(development.md § "Code that relies on another component's behaviour"):
what Postfix, OpenDKIM, Cyrus SASL, rspamd, supervisord, logrotate, Docker,
SQLite/modernc, go-milter, htmx or a browser does is read in its source or
documentation for the version the image runs - never guessed - and the code
that relies on it cites where.

This register is the review's work list. It was made in two passes:

1. **Inventory** (this commit): four read-only readings of the tree at
   `0f984ff`, one per area - A: the log tailer, Health and `cmd/panel`;
   B: the milters, `internal/postfix`, `internal/app`, `internal/domain`;
   C: `build/`, `deploy/`, the inbound relay, DMARC, backup, the e2e harness;
   D: the store, the web layer, and the claims architecture.md, security.md
   and guide.md state as fact. Each entry gives the place, what is assumed,
   what justifies it in the tree today, where it can be checked, and a
   rating of that justification: **CITED**, **MEASURED**, **GUESSED**, or
   **WRONG?** (a reason to think it false). Where a quick grep already
   answered a question the entry says so, but the rating stays what the
   tree earns.
2. **Answers**: each entry is settled in the source of the version the image
   runs and gets a verdict (holds / does not hold / measured, with date), the
   citation written into the code, and, where the source contradicts the
   code or a document, the fix. The answers follow the inventory, part by
   part (§ Answers).

Entries are named by part and number: **A-8** is entry 8 of part A, **B-PS1**
entry PS1 of part B, **D-S2** entry S2 of part D.

The sources read are the Debian bookworm packages the image installs,
fetched with `apt-get source` (development.md § Code that relies on another
component's behaviour): postfix 3.7.11-0+deb12u1, opendkim
2.11.0~beta2-8+deb12u1, cyrus-sasl2 2.1.28+dfsg-10, supervisor 4.2.5-1,
logrotate 3.21.0-1, with Debian's patches applied; Go modules at the
versions `go.mod` pins (go-milter v0.4.1, modernc.org/sqlite v1.60.1 with
SQLite 3.53.4); htmx 2.0.4 as vendored. Upstream paths below are relative
to the package's source tree.

Since base-trixie (`2.7.0`) the image runs Debian trixie (Postfix 3.10.13,
opendkim 2.11.0~beta2-9.1+b1, cyrus-sasl2 2.1.28+dfsg1-9, supervisor
4.2.5-3, logrotate 3.22.0-1; development.md § Code that relies on another
component's behaviour), and the citations in the code were re-read against
the trixie sources. This register is left as it was made: its line numbers
and versions are those of the bookworm sources above, which are now kept
under `dev/upstream-bookworm/` (where it says `dev/upstream/`).

With panel-dkim-signing (`2.8.0`) OpenDKIM left the image: the panel signs
outgoing mail itself (`internal/milter/sign.go`), and `build/opendkim.conf`,
the syslog relay (`internal/syslogrelay`, `/dev/log`), the KeyTable and
SigningTable, the USR1 reload through supervisord and the port of
OpenDKIM's From: parser (`internal/milter/mailparse.go`) went with it. The
rows about OpenDKIM, the syslog relay and those pieces describe code that
is gone; they are left as they were, like the rest of this register.

Counts at the inventory, by the justification the tree had:

| Part | Entries | CITED | MEASURED | GUESSED | WRONG? |
|---|---|---|---|---|---|
| A - log tailer, Health, cmd/panel | 65 | 0 | 7 | 50 | 8 |
| B - milters, postfix, app, domain | 79 | 0 | 3 | 65 | 11 |
| C - build, deploy, inbound, DMARC, backup, e2e | 139 | 2 | 11 | 114 | 12 |
| D - store, web | 71 | 0 | 0 | 62 | 9 |
| D - architecture.md, security.md, guide.md | 267 | 1 | 4 | 244 | 18 |

## Part A - the log tailer, Health and cmd/panel

Repository HEAD `0f984ff` (2026-10-14). Scope: `internal/logtail/**`, `internal/health/**`, `cmd/panel/*.go` and the test fixtures in those packages.
Upstream paths are relative to `dev/upstream/` (Debian sources with the Debian patches applied): `postfix-3.7.11`, `supervisor-4.2.5`, `logrotate-3.21.0`, `opendkim-2.11.0~beta2`, `cyrus-sasl2-2.1.28+dfsg`.

Ratings: **CITED** means a comment cites the source or the docs. **MEASURED** means a comment or a labelled fixture says the line or behaviour was seen on the stand or the live relay. **GUESSED** means neither. **WRONG?** means there is reason to think the assumption is false. A "(src: ...)" note means I checked the point with a quick grep of the upstream tree. It is not a full verification.

None of the code in this area carries a comment that cites a component's source file and function. The heuristics review (`0f984ff`) wrote the rule into `development.md` but has not yet reached these files.

---

### 1. Postfix 3.7.11: the shape of mail.log lines

| # | file:line (func) | Assumed | Justified now by | Check in | Rating |
|---|---|---|---|---|---|
| 1 | internal/logtail/lines.go:26-30 (`queueLineRe`) | A queue id is `[0-9A-F]{6,24}` (short) or `[0-9B-DF-HJ-NP-TV-Zb-df-hj-np-tv-z]{10,24}` (long ids). The words `NOQUEUE` and `warning` can stand in its place but never match. | Prose comment, no source. Short ids are 5 hex digits of usec plus the inode in hex, so `{6,`} is plausible. The long-id length bounds are not justified. | src/global/mail_queue.h (`MQID_SH_USEC_PAD`=5, `MQID_LG_*`), src/global/mail_queue.c `mail_queue_enter()`; postconf(5) `enable_long_queue_ids` | GUESSED |
| 2 | lines.go:32-39 (`smtpdProgramRe`, `port25Program`), inbound.go:41-43, refusals.go:285/310 | The log tag is `syslog_name + "/" + process name`. Port 25 (`syslog_name=postfix/inbound`) logs as `postfix/inbound/smtpd`. The other listeners log `postfix/smtpd`, or `postfix/<x>/smtpd` with an override. | Own config (postfix-config.sh:454-459). Tag construction not cited. The e2e test only checks the parameter, never a log line. | src/util/msg_logger.c (record format), src/global/mail_task.c `mail_task()`; postconf(5) `syslog_name` | GUESSED |
| 3 | lines.go:98 (`lineKeeper.add`) | smtpd writes `<id>: client=...` when it opens a queue id, so that line tells which listener a message came in on. | None | src/smtpd/smtpd.c:2355-2370 (`msg_info("%s: client=%s...")` in the queue-file open path) | GUESSED |
| 4 | lines.go:106 (`add`); lines.go:67-71 | qmgr writes `<id>: removed` when the message leaves the queue. | None | src/qmgr/qmgr_active.c:582 `msg_info("%s: removed", ...)` (src: confirmed) | GUESSED |
| 5 | lines.go:70-71, inbound.go:62-64, logtail.go:498-502 | Postfix reuses queue ids, and one id can be the tail of a longer one. | None | postconf(5) `enable_long_queue_ids` (short ids are inode-based); src/global/mail_queue.c | GUESSED |
| 6 | logtail.go:85-109 (`deliveryRe`) | A delivery agent's line is `<id>: to=<addr>, [orig_to=<addr>, ]relay=..., ... status=<word> (...)`. The first `status=` after `relay=` is the real one, and qmgr/smtpd/cleanup lines never match. | Prose comment (review of stage 1, audit M12), no source. e2e `send_verify_dkim_and_status` drives a row to `sent` through it, but no comment cites that. | src/global/log_adhoc.c:111-117, 214 (`to=<%s>`, `, orig_to=<%s>`, `, relay=%s`, `, dsn=%s, status=%s (%s)`) | GUESSED |
| 7 | logtail.go:104-108 | The remote reply is appended verbatim after `status=<word>`, so a lazy match is enough. | Comment, no source | src/global/log_adhoc.c:214 `" (%s)"` | GUESSED |
| 8 | logtail.go:126-128 (`parseDelivery`) | When the queue lifetime runs out, Postfix writes a per-recipient line `to=<...>, relay=..., status=expired`, and the code maps it to bounced. | Comment "Postfix gave up after the queue lifetime". Composed fixture logtail_test.go:50. | src/qmgr/qmgr_active.c:450-460 (`qmgr_active_done_25_generic`): expiry is logged once per message as `<id>: from=<...>, status=expired, returned to sender`. That line has no `to=` or `relay=`, so it never matches `deliveryRe`. The recipients keep their last `status=deferred`. The sweep only closes rows that are still `queued` (store/sendlog.go:188-193), so an expired message's rows stay `deferred` forever. **(src: confirmed)** | WRONG? |
| 9 | logtail.go:93-95, 109; logtail_test.go "orig_to is ignored in favour of to" | The send-log row's recipient equals the `to=` field. | Test comment "the address the message was actually delivered to" | src/global/log_adhoc.c:111-116: `orig_to=` is logged whenever cleanup rewrote the address (canonical/virtual maps, append_at_myorigin). The milter's row holds the RCPT TO as sent, which is `orig_to`, not `to`. In those cases the row never matches and the sweep later closes it as bounced. Edge case under SelfPost's config. | WRONG? |
| 10 | logtail.go:498-517 (`mentionsQueueID`) | Postfix writes the id followed by `:` and it is not preceded by an id character. | Comment, no source | as #3 and #6 | GUESSED |
| 11 | logtail.go:523-539 (`syslogStampRe`), inbound.go:251-262 (`lineTime`) | Postfix 3.7's maillog_file stamp is syslog-style `Mon dd hh:mm:ss`, with no year, zone or fraction, and the day may be zero-padded. | Fixture logtail_test.go "Copied off the live relay (Postfix 3.7 ...)", line `Aug 08 07:26:41 selfpost postfix/master[231]...` (no date given) | src/util/msg_logger.c:198-200 `strftime(..."%b %d %H:%M:%S ")` (src: confirmed) | MEASURED |
| 12 | logtail.go:526-535; logtail_test.go TestSplitTimestamp name | "maillog_file_format arrived in Postfix 3.9" and selects RFC 3339. That is why `isoStampRe` is matched first. | Comment states it as fact, no source. | Not in the 3.7.11 tree (expected). Needs the 3.9 postconf(5) or RELEASE_NOTES-3.9. | GUESSED |
| 13 | inbound.go:236-242 (`lineTime`) | The stamp is in the container's zone, "which is what postlogd wrote it in". | Comment. Misattributed: the stamp is formatted by the logging process (msg_logger.c `localtime`), not by postlogd. Harmless because `*/*/chroot=n`. | src/util/msg_logger.c:190-200 | GUESSED |
| 14 | inbound.go:34-35, 59-60 (`inboundRejectRe`, kind `reject`) | smtpd's refusal is `<id\|NOQUEUE>: reject: RCPT from name[addr]: <reply>; from=<..> to=<..> proto=.. helo=<..>`. HELO is the last field, and the client address has no `IPv6:` prefix. | Comment and composed fixtures (example.net) | src/smtpd/smtpd.c:1700-1722 `smtpd_whatsup()`: after `helo=<>` it appends ` sasl_username=<..>` when the session authenticated. That cannot happen on port 25 (SASL off), so "HELO is last" holds there only. Also src/smtpd/smtpd_check.c `log_whatsup()`. | GUESSED |
| 15 | inbound.go:36, 41-46, 59-60 (cleanup branch); refusals.go:297-306, 382 (`fromRefusalRe`, recovered row `To: m[5]`) | cleanup's `milter-reject` line names the message's recipient in `to=`. | Comment. The From-refusal line itself was seen (#22). | src/cleanup/cleanup_milter.c:2113-2121 logs ` to=<state->recip>`. src/cleanup/cleanup_addr.c:231-233 overwrites `state->recip` for every recipient. So `to=` is the **last** recipient only. A multi-recipient message gets one inbound-journal row (one domain), and a recovered From-refusal row has one recipient. **(src: confirmed)** | WRONG? |
| 16 | inbound.go:41-46 | cleanup writes the filter verdict under one name (`postfix/cleanup`) for every listener. | Own config (shared `cleanup` service), not cited | postconf(5) `cleanup_service_name`; master.cf | GUESSED |
| 17 | inbound.go:61-65 (`inboundClientRe`, `inboundForgetRe`) | qmgr's `<id>: from=<..>, size=.., nrcpt=..` means the message was queued (the filter let it through). | None | src/qmgr/qmgr_message.c (from= logging when entering active) | GUESSED |
| 18 | inbound.go:180-184 | A Postfix refusal reply starts `4xx `. A milter reply as cleanup logs it starts `4.x.x` (code stripped). Either way a leading `4` means tempfail. | Comment, no source. A seen fixture (sendercheck_test, cleanup 1339) shows `4.7.1 ...`. | src/cleanup/cleanup_milter.c:2100-2112 (`text = resp + 4`) | GUESSED |
| 19 | inbound.go:133-139 | A quoted local part in `from=<>` may contain `> to=<`, so addresses with `"<> ` are dropped. | Comment (security review), no source | src/global/info_log_addr_form.c; log_whatsup quoting | GUESSED |
| 20 | refusals.go:275-285 (`refusalLineRe`) | The journal milter's refusal at MAIL FROM is logged as `NOQUEUE: milter-reject: MAIL from name[addr]: 451 4.7.1 ...; from=<..> proto=ESMTP helo=<..>`. | "As the stand writes it:" with a quoted line (no date). The fixtures of the same event (refusals_test.go:39, sendercheck_test.go:48) end with ` sasl_username=<l2app>`, which the quoted line lacks, so one of them is not as seen. | src/smtpd/smtpd.c `smtpd_whatsup()` | MEASURED |
| 21 | refusals.go:278 ("the refusal has no queue id and the line no login") and the matching by client address, sender and time | A smtpd `milter-reject` line carries no login. | Comment | src/smtpd/smtpd.c:1718-1721: `smtpd_whatsup()` appends ` sasl_username=<%s>` whenever `state->sasl_username` is set. That is always the case on 465/587. The login is on the line and could be matched on. **(src: confirmed)** | WRONG? |
| 22 | refusals.go:297-306 (`fromRefusalRe`) | cleanup's From-refusal is `<id>: milter-reject: END-OF-MESSAGE from name[addr]: 5.7.1 From: <..> - <reason>; from=<..> to=<..>`, and the `;` is Postfix's. | "as the stand writes it". Dated in sendercheck_test.go:159-161 (2026-10-12). Also own milter text (sender.go printable). | src/cleanup/cleanup_milter.c:2113-2125 | MEASURED |
| 23 | refusals.go:308-310 (`clientLoginRe`, `sasl_username=(\S+)`) | `sasl_username=` is the last field of smtpd's `client=` line. | Comment shows the format, no source | src/smtpd/smtpd.c:2355-2370: the field order is `client=`, `sasl_method`, `sasl_username`, then `, sasl_sender=` (if the client gave `AUTH=` on MAIL FROM) and `, orig_queue_id=` / `, orig_client=` (XFORWARD). `\S+` then captures `login,` with a trailing comma. **(src: confirmed)** | WRONG? |
| 24 | refusals.go:393 (`TrimPrefix(m[2],"IPv6:")`) | The client address in `name[addr]` may carry `IPv6:`. | None. Harmless. | src/smtpd/smtpd_peer.c (`namaddr` uses `addr`, not `rfc_addr`) | GUESSED |
| 25 | logtail/sendercheck.go:316-321, 363, 403 (`deferredCounter`) | With nobody on the socket, smtpd writes `warning: connect to Milter service unix:<path>: <errno>` once per connection and then the generic `milter-reject` lines. | "seen on the e2e stand, 2026-10-12", with quoted lines | src/milter/milter8.c `milter8_connect()` msg_warn | MEASURED |
| 26 | logtail/sendercheck.go:332-341, 404, 418-423 | A milter that hangs after DATA is met by cleanup: `warning: milter unix:<path>: can't read SMFIC_HEADER reply packet header: ...`. One warning is one message. | "Seen on the e2e stand ... SIGSTOP ... (2026-10-14)" | src/milter/milter8.c `milter8_read_resp()` | MEASURED |
| 27 | logtail/sendercheck.go:426-440 | smtpd writes `connect from ...` at each new client, and one smtpd pid serves one connection at a time, so per-pid state resets there. | Seen fixtures (pid 1183). The process model is not cited. | src/smtpd/smtpd.c `smtpd_service()` (`connect from`); single_server model (src/master/single_server.c) | MEASURED |
| 28 | logtail/sendercheck.go:323-326 | A client turned away gets `454 4.3.0` at AUTH and leaves without a line of its own. | "seen on the e2e stand, 2026-10-12" | src/smtpd/smtpd_sasl_proto.c | MEASURED |
| 29 | logtail/sendercheck.go:328-330 | The `milter-reject ... Service unavailable` lines read the same whether the sender check, OpenDKIM or a level-2 refusal is behind them, so only the warning names the socket. | Comment. The OpenDKIM fixture lines (sendercheck_test.go:193-195, 207) are not labelled seen. | src/smtpd/smtpd.c milter reply path; src/milter/milter.c `milter_default_action` text | GUESSED |

### 2. Postfix 3.7.11: behaviour (queue, milters, pipe, postlogd)

| # | file:line (func) | Assumed | Justified now by | Check in | Rating |
|---|---|---|---|---|---|
| 30 | logtail.go:213-216 (`reconcileLoop`) | A message is still worked on until its first retry after the lifetime, at most `maximal_backoff_time` later. So "stuck" means older than lifetime plus maximal backoff. | Comment, no source | src/qmgr/qmgr_active.c:450-456 (expiry is checked when the message is done with an attempt); postconf(5) `maximal_queue_lifetime`, `bounce_queue_lifetime`, `maximal_backoff_time` | GUESSED |
| 31 | logtail.go:245-262 (`reconcile`) | Every recipient that leaves the queue gets a `to=..., status=` line. A queued row whose id is gone therefore lost its line to rotation or deletion. | Comment, no source | Exceptions to check: `postsuper -d` (only `removed`), header/milter DISCARD (src/cleanup `discard:` lines), expiry (#8), milter `smfi_delrcpt`. src/qmgr/qmgr_active.c, src/postsuper/postsuper.c | GUESSED |
| 32 | logtail.go:63-65, 273-279 → internal/postfix/queue.go:92-125 | `postqueue -p` lists every message Postfix holds (maildrop, incoming, active, deferred, hold), and its entry line is `<id>[*!] <size> <date> <sender>`. | Comment in internal/postfix (outside this area), no source | src/showq/showq.c, src/postqueue/postqueue.c | GUESSED |
| 33 | logtail.go:684-687 (`follow`); build/logrotate-loop.sh:2-10 | postlogd keeps writing to the renamed inode until `postfix reload`. | Comment, no source | src/postlogd/postlogd.c:212 (file opened once in `pre_jail_init`), :233 (`var_use_limit=0`, but it may exit on idle and reopen then); README_FILES/MAILLOG_README | GUESSED |
| 34 | lines.go:41-46 (`lineGrace`) | A message's first lines come at arrival and its row at end-of-message, never more than an hour apart. | Comment ("an hour is a message body sent slowly") | postconf(5) `smtpd_timeout` (per read, not per message) | GUESSED |
| 35 | inbound.go:78-83 (`inboundPruneEvery`) | "Refusals are written at the rate strangers send RCPT TO, which no limit of Postfix's counts". | Comment | postconf(5) `smtpd_client_recipient_rate_limit` counts recipients "regardless of whether or not Postfix actually accepts" them. It exists but is unset here, so the claim is too strong (minor). | WRONG? |
| 36 | cmd/panel/sendercheck.go:21-25; health/socket.go:160-163; handlers_status.go:435-437 | The sender-check and OpenDKIM sockets are attached with `default_action=tempfail` (mail deferred while they are down). The journal milter uses `default_action=accept`. | Own config (postfix-config.sh:249-251). Effect not cited. | README_FILES/MILTER_README "default_action"; src/milter/milter8.c | GUESSED |
| 37 | health/socket.go:160-163 (`CheckSocket` doc) | "OpenDKIM runs with default_action=tempfail". | Comment. Misattributed: `default_action` is Postfix's per-milter setting, not OpenDKIM's. | as #36 | GUESSED |
| 38 | health/socket.go:171-182 (`CheckSocket`) | A socket file that exists means "Listening". | None | A crashed OpenDKIM (libmilter) or a SIGKILLed panel leaves its socket file. A frozen panel keeps one that accepts and never answers (the case sendercheck.go itself counts). The detail overclaims. opendkim-2.11.0~beta2/opendkim/opendkim.c (socket removal only at start/clean exit); libmilter `smfi_opensocket` | WRONG? |
| 39 | health/milter.go:102-150 (`CheckMilter`) | Postfix milter addresses come in two forms, `unix:/path` and `inet:host:port`. | Comment, no source | src/milter/milter8.c `milter8_connect()` (check whether `local:` is also accepted) | GUESSED |
| 40 | cmd/panel/main.go:37 (`os.Exit(75)`) | Exit 75 (EX_TEMPFAIL) from the pipe(8) command makes Postfix defer. | Comment "ask Postfix to defer" | src/global/pipe_command.c:471 and the status mapping near :620-655; pipe(8) | GUESSED |
| 41 | cmd/panel/dmarc_ingest.go:35-37 | Exit 0 means delivered. Deferring retries for days, and another failure bounces to the envelope sender. | Comment | src/global/pipe_command.c:620-655 (5.3.0/4.3.0 mapping); src/pipe/pipe.c | GUESSED |
| 42 | dmarc_ingest.go:46-49 (`logRejected`) | Postfix puts "about a hundred bytes" of the command's output into its `status=sent (delivered via ... service (...))` line. | Comment, no source | src/pipe/pipe.c:1055-1065 `" (%.100s)"`. The claim is right; the comment could cite this. | GUESSED |
| 43 | dmarc_ingest.go:50-51 | "postlog is no use here: pipe(8) starts the command without the group that may write to Postfix's log socket". | Comment, no source | conf/postfix-files:135 installs `postlog` set-gid `$setgid_group` (2755). src/postlogd/postlogd.c:28-30 man text: set-gid is allowed on "(Postfix >= 3.7) postlog(1)". postlog should therefore reach postlogd whatever group pipe runs with. Check `ls -l /usr/sbin/postlog` in the image. | WRONG? |
| 44 | dmarc_ingest.go:73-77 (`ingestRecipient`) | pipe(8) passes the recipient only via `${recipient}` in argv and sets no `RECIPIENT` variable (local(8) does). | Comment naming pipe(8) and local(8), with no section | src/pipe/pipe.c (macro expansion, environment); pipe(8) "COMMAND ATTRIBUTE SYNTAX" | GUESSED |
| 45 | cmd/panel/httpserver.go:100-103 → internal/postfix.LoadRetryPolicy | `postconf -h` gives the effective config, including manual overrides. | Comment (internal/postfix is outside this area) | src/postconf/postconf.c; postconf(1) `-h` | GUESSED |
| 46 | cmd/panel/sendercheck.go:129-157 (`postfixStarted`) | supervisord's uptime of program `postfix` (the wrapper) is the time since Postfix started. | Comment | build/postfix-wrapper.sh; supervisor-4.2.5/supervisor/rpcinterface.py:608-619 (`start` is the spawn time) | GUESSED |

### 3. OpenDKIM 2.11.0~beta2

| # | file:line | Assumed | Justified now by | Check in | Rating |
|---|---|---|---|---|---|
| 47 | cmd/panel/notifier.go:116-118 | With no SigningTable line for the server's name, OpenDKIM passes the mail unsigned: "a failure here costs the signature, not the message". | Comment | opendkim-2.11.0~beta2/opendkim/opendkim.c `mlfi_eoh` (no signing match → accept); opendkim.conf(5) SigningTable | GUESSED |

### 4. Cyrus SASL 2.1.28 / Postfix SASL glue

| # | file:line | Assumed | Justified now by | Check in | Rating |
|---|---|---|---|---|---|
| 48 | cmd/panel/main.go:201-211 (`saslRealm`) | With `smtpd_sasl_local_domain` empty, a bare login resolves in sasldb under the server hostname (`SELFPOST_HOSTNAME`). | Comment "mirrors the hostname Postfix's SASL layer uses" | postfix-3.7.11/src/xsasl/xsasl_cyrus_server.c (`sasl_server_new` serverFQDN/user_realm args); cyrus-sasl2-2.1.28+dfsg/lib/server.c and plugins/sasldb.c (realm default) | GUESSED |

### 5. logrotate 3.21.0 (with build/logrotate-mail.conf)

| # | file:line | Assumed | Justified now by | Check in | Rating |
|---|---|---|---|---|---|
| 49 | offset.go:129-137 (`readRotated`), logtail.go:308-316 (`deliveryInLog`) | The previous log is `mail.log.1`, uncompressed, because of `delaycompress`. | Comment, no source | logrotate-3.21.0/logrotate.8:532-537 (`delaycompress`); logrotate.c rotation naming | GUESSED |
| 50 | logtail.go:561-565, 683 (`follow`) | Rotation shows as an inode change (`create`) or as truncation (`copytruncate`). | Comment | logrotate.8 `create`, `copytruncate`; logrotate.c | GUESSED |
| 51 | logtail.go:704-710, 717 (`blindness`, `blindMin`) | Between rename and create the name is missing for well under 3 s. | Comment | logrotate.c (rename then create in one run) | GUESSED |
| 52 | offset.go:18-24 (`fingerprintSize`) | The first 512 bytes of a fresh mail.log are unique per generation, since Postfix stamps every line. | Comment | src/util/msg_logger.c (stamp to the second); the inode is now part of the fingerprint | GUESSED |
| 53 | fileid_unix.go:171-176 (`fileID`) | The inode survives a rename. The device number may change across a reboot (LVM, btrfs, NFS). | Comment ("review of stage 2b") | Linux stat(2), rename(2) | GUESSED |

### 6. supervisor 4.2.5

| # | file:line | Assumed | Justified now by | Check in | Rating |
|---|---|---|---|---|---|
| 54 | health/processes.go:38-52 (`Processes`) | `supervisorctl status` exits non-zero when some program is not running, so the output is parsed first. | Comment, no source | supervisor/supervisorctl.py:657-700 `do_status` (`LSBStatusExitStatuses.NOT_RUNNING`=3) (src: confirmed) | GUESSED |
| 55 | processes.go:55-67 (`supervisorStates`) | The state words are STOPPED, STARTING, RUNNING, BACKOFF, STOPPING, EXITED, FATAL, UNKNOWN. | Comment, no source | supervisor/states.py:4-12 `ProcessStates` (src: confirmed) | GUESSED |
| 56 | processes.go:69-88 (`parseProcesses`) | A status line is `<name> <STATE> <desc>`, separated by whitespace. Error lines have a non-state second field. | Comment. Fixture health_test.go:36 "Real supervisorctl output", with no where or when. | supervisor/supervisorctl.py:643-650 `_show_statuses` (template `%(namespec)-{maxlen+3}s%(state)-10s%(desc)s`), :549/:613 `'%s: ERROR (%s)'` | GUESSED |
| 57 | processes.go:90-121 (`Uptime`) | The desc is `pid N, uptime H:MM:SS`, or `N day(s), H:MM:SS` (Python `str(timedelta)`). | Comment shows the format, no source | supervisor/rpcinterface.py:608-619 `_interpretProcessInfo` | GUESSED |

### 7. Linux kernel and libc (/proc, unix sockets)

| # | file:line | Assumed | Justified now by | Check in | Rating |
|---|---|---|---|---|---|
| 58 | health/machine.go:36-40 | In a container, /proc/stat and /proc/meminfo show the host (not namespaced). /proc/net/dev shows the container's network namespace. | Comment, no source | proc(5); network_namespaces(7) | GUESSED |
| 59 | machine.go:382-420 (`readCPUTimes`) | The `cpu` line holds user nice system idle iowait irq softirq steal, then guest fields already counted in user/nice. `cpuN` lines are per CPU. | Comment, no source | proc_stat(5); kernel/sched/cputime.c `account_guest_time()` | GUESSED |
| 60 | machine.go:275-279, 524-555 (`readMemory`, `readMeminfo`) | MemAvailable is the kernel's estimate of available memory, and "kB" means 1024 bytes. | Comment, no source | proc_meminfo(5); kernel commit 34e431b0ae39 (MemAvailable) | GUESSED |
| 61 | machine.go:557-585 (`readNetDev`) | In `/proc/net/dev`, field 0 after the colon is rx bytes and field 8 is tx bytes. The colon may have no space after it. | Comment, no source | proc_net(5); net/core/net-procfs.c `dev_seq_printf_stats` | GUESSED |
| 62 | machine.go:429-495 (`readCPUTopology`) | /proc/cpuinfo has `processor`, `physical id` and `core id` keys (x86). Without them, each processor counts as a core. | Comment, no source | arch/x86/kernel/cpu/proc.c `show_cpuinfo` | GUESSED |
| 63 | cmd/panel/journal.go:243-260 (`listenMilter`) | A stale socket file must be removed before bind. A socket made in a setgid directory takes its group. Connecting needs write permission on the socket node. | Comment, no source | unix(7) ("connecting ... requires write permission"); bind(2); inode(7) setgid directories | GUESSED |

### 8. TLS certificate and ACME

| # | file:line | Assumed | Justified now by | Check in | Rating |
|---|---|---|---|---|---|
| 64 | health/cert.go:11-14 (`certWarnDays`) | Let's Encrypt clients renew at 30 days left. | Comment | certbot docs `renew_before_expiry`; LE's lifetime changes (shorter certificates planned) | GUESSED |
| 65 | health/cert.go:16-18, 28-66 | The file at TLS_CERT_FILE is the certificate Postfix serves now. | Comment | build/postfix-cert-reload.sh; postfix-3.7.11 src/smtpd/smtpd.c TLS init (keys load per process at `pre_jail_init`) | GUESSED |

---

### 9. Test fixtures: is each one labelled seen or composed, and is the label true?

The rule (development.md:237-238): a fixture copied from a real log says where and when; a composed one says that it was composed.

| File | Fixtures | Label | Assessment |
|---|---|---|---|
| internal/logtail/sendercheck_test.go:159-164 | smtpd pid 1183 and cleanup 1339/1195 lines | "as the e2e stand wrote them ... (2026-10-12) ... (2026-10-14); the stamps are put in ISO form" | Seen, dated, honest about the stamp change. The surrounding lines (pid 1149 level-2 refusal, pid 1190 "Illegal address syntax" with the socket name, OpenDKIM lines on 1201 and cleanup :207) are **not labelled**, so they read as composed. The 1149 line carries `sasl_username=<l2app>` while refusals.go:282 quotes "as the stand writes it" without it. One of the two is not what the stand wrote. Source (smtpd.c:1718-1721) sides with the fixture. |
| internal/logtail/refusals_test.go:39-43, 76-77, 104-105 | NOQUEUE milter-reject, client= with sasl, cleanup From-refusal | none | Unlabelled. They use the stand's addresses (`e2e.test`, `10.77.0.1`), so they look copied, but nothing says seen or composed. `postfix/inbound/smtpd[372]` with `stranger.example` is composed. |
| internal/logtail/logtail_test.go:17-140 (TestParseDelivery) | delivery lines (example.net) | none | Composed, not labelled. :50 composes `to=<..>, relay=none, status=expired (...)`, a shape Postfix 3.7.11 does not write (#8). :26 uses an ISO stamp with no zone, which Postfix 3.7 never writes. |
| logtail_test.go TestSplitTimestamp | `Aug 08 07:26:41 selfpost postfix/master[231]: daemon started -- version 3.7.11 ...` | "Copied off the live relay (Postfix 3.7...)" | Labelled seen; where is given, **when is not**. The other cases (ISO with microseconds, `opendkim[30]` in mail.log) are composed and unlabelled. OpenDKIM logs with `Syslog no` (build/opendkim.conf:12), so it never writes to mail.log. |
| internal/logtail/lines_test.go | queue-id lines; tags `postfix/submission/smtpd`, `postfix/smtps/smtpd` | none | Composed, unlabelled. This config sets no `syslog_name` on smtps/submission, so those tags never occur in this image. That is harmless for the regexps, but the fixtures are not real. |
| internal/logtail/inbound_test.go | port-25 rejects, cleanup verdicts; head `2026-10-11T12:00:05.123456+00:00 mail ` | none | Composed, unlabelled. ISO stamps with microseconds are a format Postfix 3.7 does not write. |
| internal/logtail/retention_test.go | no Postfix lines | n/a | n/a |
| internal/health/health_test.go:36-41, 168-182 | supervisorctl status output | ":36 Real supervisorctl output" | Labelled real, with no where or when. The second set (:168-182) is unlabelled. The format matches supervisorctl.py `_show_statuses`. |
| internal/health/health_test.go:194-203 | uptime strings | none | Composed; consistent with `str(timedelta)`. `127:00:01` cannot come from supervisor (timedelta prints days), and the test only shows the parser tolerates it. |
| internal/health/machine_test.go | /proc files | "fakeProc writes a /proc-shaped directory" | Composed, and says so in effect. |
| internal/health/milter_test.go, cmd/panel/sendercheck_test.go | no external program output | n/a | n/a |

---

### Counts (code entries 1-65)

- CITED: 0
- MEASURED: 7 (#11, 20, 22, 25, 26, 27, 28)
- GUESSED: 50
- WRONG?: 8 (#8, 9, 15, 21, 23, 35, 38, 43). Of these, #8, #15, #21 and #23 were confirmed by a grep of the 3.7.11 source. #43 is backed by postfix-files plus the postlogd man text, and still needs `ls -l /usr/sbin/postlog` on the stand.

## Part B - internal/milter, internal/postfix, internal/app, internal/domain

Tree at `0f984ff` (2026-10-14). Read-only inventory under docs/development.md, "Code that relies on another component's behaviour".

Ratings:

- **CITED**: a comment names the component, the version, the file and the function (or a document's URL and section).
- **MEASURED**: a comment or fixture says it was seen on the stand or in a trial, with a date.
- **GUESSED**: there is no such justification. Internal cross-references such as "architecture.md", "internal/postfix renderSenderLoginMaps" or "build/postfix-config.sh" do not count.
- **WRONG?**: I believe the assumption is false, and the entry says why.

Where a quick look at the unpacked source (dev/upstream/, the go-milter v0.4.1 module cache) settled a question, the entry is marked **[checked: ...]**. The rating stays what the code's own justification earns, so a checked-true GUESSED entry still needs its comment.

Upstream paths are relative to dev/upstream/<package>/. "go-milter" means `$GOMODCACHE/github.com/emersion/go-milter@v0.4.1/`.

**Totals: 79 entries. CITED 0, MEASURED 3, GUESSED 65, WRONG? 11.**

No code in these four packages carries a citation in the form the rule requires. The only dated measurements are the ones noted under MEASURED.

---

### 1. go-milter v0.4.1 (the library's session lifecycle and argument forms)

**G1. internal/milter/milter.go:41-47** (`session` doc comment)
- Assumes: macros arrive per stage and do not accumulate, and go-milter creates one session per connection.
- Component: go-milter v0.4.1.
- Justified by: only a pointer to architecture.md.
- Check in: go-milter session.go `Process`, `case CodeMacro` (`m.macros = make(map[string]string)`); server.go `Serve` (`backend: s.NewMilter()` for each connection).
- **[checked: true]**
- **GUESSED**

**G2. milter.go:86-94** (`MailFrom` comment), **sender.go:231-233**, **inbound.go:46-48**, **milter_test.go:414-417**, **inbound_test.go:248-249**
- Assumes: the library replaces the session after every reply that is not "continue", and a replacement session never sees Connect.
- Component: go-milter v0.4.1.
- Justified by: "(Found on 2026-10-12; test/e2e sends two MAIL FROM on one connection.)"
- Check in: go-milter session.go `HandleMilterCommands` (`if !resp.Continue() { m.backend = m.server.NewMilter() }`); response.go `SimpleResponse.Continue` (true only for ActContinue) and `CustomResponse.Continue` (false only for accept, discard, reject and tempfail).
- **[checked: true]** Note: the sender check's `NewResponseStr('y', "550 ...")` is a CustomResponse with code 'y', so it does **not** replace the session. Its RespTempFail at end-of-headers does.
- **MEASURED**

**G3. milter.go:130-135** (`Body`), **inbound.go:130-133**, **sender.go:259** (`Headers`)
- Assumes: `Body` fires once at end-of-message and `Headers` fires at end-of-headers.
- Component: go-milter v0.4.1.
- Justified by: nothing.
- Check in: go-milter session.go `Process`, `case CodeEOB` and `case CodeEOH`.
- **[checked: true]**
- **GUESSED**

**G4. milter.go:153-162** (`macro`)
- Assumes: "go-milter stores whatever name Postfix sends verbatim", so both `name` and `{name}` must be tried.
- Component: go-milter v0.4.1.
- Justified by: nothing.
- Check in: go-milter session.go `case CodeMacro` (`m.macros[data[i]] = data[i+1]`).
- **[checked: true]**
- **GUESSED**

**G5. milter.go:205-236** (`cleanAddress`), used by MailFrom and RcptTo in all three milters
- Assumes: the address may arrive as `<addr>` followed by ESMTP parameters, and a quoted local part may contain a space or a `>`.
- Component: go-milter v0.4.1 (with Postfix 3.7.11's SMFIC_MAIL/RCPT arguments).
- Justified by: "review of audit stage 1" only. address_test.go feeds bracketed forms only.
- Check in: go-milter session.go `case CodeMail` / `case CodeRcpt`: `readCString(msg.Data)` takes only the first argument (ESMTP parameters are separate strings and are dropped), then `strings.Trim(from, "<>")`.
- Why it is wrong: the input never starts with `<`, so the quoted-local-part branch never runs in production. The fallback `IndexByte(' ')` then cuts `"a b"@example.net` to `"a`. That is exactly the case the stage-1 fix was for, and its test passes only because it feeds the bracketed form. The parameter-stripping is also dead code.
- **WRONG?**

**G6. milter.go:253-257, sender.go:497-503, inbound.go:272-276** (`Actions: 0`, `Protocol: OptNoBody`)
- Assumes: with SMFIP_NOBODY negotiated, Postfix still sends headers, EOH and EOB and skips only body chunks; with Actions 0 the milter may still send a reply code ('y').
- Components: go-milter v0.4.1 and Postfix 3.7.11.
- Justified by: nothing.
- Check in: go-milter session.go `case CodeOptNeg`; Postfix src/milter/milter8.c `milter8_negotiate` and `milter8_message` (SMFIP_NOBODY handling).
- **GUESSED**

**G7. inflight.go:298-305** (`reservationTTL`)
- Assumes: "go-milter has no connection-close hook", and a client that drops after MAIL FROM produces neither EOM nor ABORT.
- Components: go-milter v0.4.1 and Postfix 3.7.11.
- Justified by: nothing.
- Check in: go-milter server.go `type Milter interface` (no Close callback; `CodeQuit` returns errCloseSession without calling the backend), **[checked: true]**. Postfix src/smtpd/smtpd.c `mail_reset()` → src/milter/milter.c `milter_abort()` would show whether ABORT is in fact sent on disconnect (open).
- **GUESSED**

**G8. milter.go:259-269, sender.go:505-515, inbound.go:278-288**
- Assumes: `srv.Close()` unblocks `Serve`, which then returns an error that is treated as a clean shutdown.
- Component: go-milter v0.4.1.
- Justified by: nothing.
- Check in: go-milter server.go `Serve` / `Close` (returns `ErrServerClosed`).
- **[checked: true]**
- **GUESSED**

**G9. milter.go:71-76, inbound.go:60-65** (`Connect`)
- Assumes: Connect's `addr` is the client IP, and may be nil.
- Components: go-milter v0.4.1 and Postfix 3.7.11.
- Justified by: nothing.
- Check in: go-milter session.go `case CodeConn` (`net.ParseIP(address)`, which gives nil for an "IPv6:"-prefixed or unknown address); Postfix src/milter/milter8.c `milter8_conn_event` for the form of the IPv6 address it sends (open).
- **GUESSED**

### 2. Postfix 3.7.11: the milter protocol, macros and reply handling

**PM1. milter.go:56-59, 97-99, 164-177** (`clientAddr`), **inbound.go:69-72**
- Assumes: `{client_addr}` comes with MAIL FROM because build/postfix-config.sh adds it to milter_mail_macros. Its value is a bare IP, or "IPv6:..." in "Sendmail's form".
- Component: Postfix 3.7.11.
- Justified by: a reference to build/postfix-config.sh only.
- Check in: src/smtpd/smtpd_milter.c `smtpd_milter_eval` (`S8_MAC_CLIENT_ADDR`); postconf(5) milter_mail_macros.
- **GUESSED**

**PM2. milter.go:79-81, 101; sender.go:236** (`{auth_authen}`)
- Assumes: the SASL login arrives in the MAIL-stage macros, is the bare login as written in sender_login_maps (no @realm), and is empty when the client is not authenticated.
- Components: Postfix 3.7.11 and Cyrus SASL 2.1.28.
- Justified by: nothing in the code; build/postfix-config.sh:189-194 explains it without a source.
- Check in: src/global/mail_params.h:3473 `DEF_MILT_MAIL_MACROS` (includes `{auth_authen}`) **[checked]**; src/smtpd/smtpd_milter.c (`S8_MAC_AUTH_AUTHEN` → `state->sasl_username`); src/xsasl/xsasl_cyrus_server.c; cyrus lib/canonusr.c `_canonuser_internal`.
- **GUESSED**

**PM3. milter.go:130-136, inbound.go:134-137** (queue id `{i}` at end-of-message)
- Assumes: the queue-id macro `i` is set at end-of-message.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/global/mail_params.h:3496 `DEF_MILT_EOD_MACROS "i"` **[checked]**; src/cleanup/cleanup_milter.c `cleanup_milter_eval`.
- **GUESSED**

**PM4. sender.go:271-274** (`Headers` reads `i`)
- Assumes: "cleanup has given the message a queue id by the end of its headers (milter_end_of_header_macros has {i})".
- Component: Postfix 3.7.11.
- Justified by: nothing. The comment writes `{i}`, but the default is a bare `i`; `macro()` reads both.
- Check in: src/global/mail_params.h:3492 `DEF_MILT_EOH_MACROS "i"` **[checked]**; src/cleanup/cleanup_milter.c.
- **GUESSED**

**PM5. milter.go:130-134, inbound.go:130-132**
- Assumes: "continue" at end-of-message lets the message through just as "accept" does.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/milter/milter8.c `milter8_event` (`SMFIR_CONTINUE` at the EOM stage) and `milter8_message`.
- **GUESSED**

**PM6. milter.go:80-83, 104-111** (rate limit and client-IP refusal)
- Assumes: RespTempFail at MAIL FROM makes Postfix refuse that transaction with a 4xx and nothing more. The connection goes on, and the next MAIL FROM reaches the milter again.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/milter/milter8.c `milter8_event` (`SMFIR_TEMPFAIL`); src/smtpd/smtpd.c `mail_cmd` (`milter_mail_event`).
- **GUESSED**

**PM7. milter.go:8-11, inbound.go:33-35, sender.go:29-31**
- Assumes: with `default_action=accept`, a crash or hang of the journal milters fails open; with `default_action=tempfail`, the sender check defers mail while it does not answer.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: postconf(5) milter_default_action; MILTER_README "Different settings for different Milter applications"; src/milter/milter8.c `milter8_comm_error` / `milter8_conf_error`.
- **GUESSED**

**PM8. milter.go:115-116** (`RcptTo`)
- Assumes: Postfix calls the milter once per recipient.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/milter/milter8.c `milter8_rcpt_event`; src/smtpd/smtpd.c `rcpt_cmd`.
- **GUESSED**

**PM9. milter.go:143-145** (`Abort`)
- Assumes: Abort fires on a client RSET, or when Postfix rejects the message for its own reasons.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/smtpd/smtpd.c `mail_reset` → src/milter/milter.c `milter_abort`; milter8.c `milter8_abort`.
- **GUESSED**

**PM10. sender.go:275-277** (`NewResponseStr('y', "550 5.7.1 ...")`), **sender.go:464-465** (`printable` turns `%` into `?`)
- Assumes: a reply code at end-of-headers makes Postfix reject the message permanently with this text, and the client is told the text.
- Component: Postfix 3.7.11.
- Justified by: nothing. No reason is given for the `%` handling.
- Check in: src/milter/milter8.c `milter8_event` `case SMFIR_REPLYCODE` (validates the code and enhanced status; lines ~1272-1300 halve `%%` and drop single `%`) **[checked: the `%` part]**; src/cleanup/cleanup_milter.c `cleanup_milter_apply`.
- **GUESSED**

**PM11. sender.go:466-470** (`printable` turns `;` into `,`)
- Assumes: Postfix logs the reply followed by its own `; from=<...> to=<...>`.
- Component: Postfix 3.7.11.
- Justified by: nothing here (internal/logtail may hold a sighting).
- Check in: src/cleanup/cleanup_milter.c `cleanup_milter_apply` and the log_adhoc / `msg_info` "milter-reject" format.
- **GUESSED**

**PM12. sender.go:301-303, 393-397** (`unfold`)
- Assumes: "Postfix hands a milter a header as it came, folded lines and all", with line breaks as CRLF, LF or CR.
- Component: Postfix 3.7.11.
- Justified by: nothing. The e2e with testdata/from-lines-passed.txt exercises two folded lines.
- Check in: src/milter/milter8.c `milter8_header` (~2361-2369; also strips one leading space unless SMFIP_HDR_LEADSPC).
- **GUESSED**

**PM13. sender.go:247** (header name trimmed: "the obsolete syntax allows blanks before the colon")
- Assumes: Postfix passes a name such as `From ` with its blanks, as a header.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/global/is_header.c `is_header_buf`; src/milter/milter8.c `milter8_header` ("XXX Following matches is_header.c").
- **GUESSED**

**PM14. inbound.go:82-87**
- Assumes: Postfix hands the message to its milters one after another at end-of-message, each seeing the headers the one before it added.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/milter/milter.c `milter_message`; src/cleanup/cleanup_milter.c `cleanup_milter_inspect` / `cleanup_milter_apply`.
- **GUESSED**

**PM15. inbound.go:28-31**
- Assumes: refusals by the filter "never get this far", because Postfix stops the milter chain at the first reject.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/milter/milter.c `milter_message` (the loop over `milter_list` returns on the first non-null reply).
- **GUESSED**

**PM16. sender.go:417-421** (`domain()`)
- Assumes: Postfix holds the null sender against no login.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/smtpd/smtpd_check.c ~4561-4565 (`if (state->sender && *state->sender) status = reject_auth_sender_login_mismatch(...)`).
- **[checked: true]**
- **GUESSED**

**PM17. sender.go:289-292**
- Assumes: an unauthenticated message never reaches the end-of-headers judgement while the submission ports demand authentication.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/smtpd/smtpd.c (milter events against smtpd_relay_restrictions at RCPT); master.cf overrides in build/postfix-config.sh.
- **GUESSED**

### 3. Postfix 3.7.11: smtpd_sender_login_maps and texthash

**PS1. sender.go:23-37, 117-140** (`Bindings.Allowed`)
- Assumes: the check "go[es] by one table, by one lookup" as Postfix does for the envelope: the address, the address without its extension, the domain. It accepts when **any** of the three keys lists the login.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/smtpd/smtpd_check.c `reject_auth_sender_login_mismatch` (~3928-3947); src/global/mail_addr_find.c `mail_addr_find_opt` (~253-423: each step runs only `if (result == 0 ...)`, so the first key found wins); src/global/mail_addr_find.h:39 `MA_FIND_DEFAULT` (also includes `MA_FIND_LOCALPART_IF_LOCAL`).
- Why it is wrong: Postfix takes the owner list of the **first** key found and checks the login against that list only. With `billing@example.org billing` and `@example.org shop` (a list-mode and a wildcard-mode application on one domain), Postfix refuses shop as envelope sender billing@example.org, but the check lets shop use From: billing@example.org. The local-part-only keys for local domains are not modelled either.
- **[checked]**
- **WRONG?**

**PS2. sender.go:35-36**
- Assumes: "Postfix compares logins without regard to case".
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/smtpd/smtpd_check.c:3936 `strcasecmp_utf8(state->sasl_username, name)`.
- **[checked: true]**
- **GUESSED**

**PS3. internal/app/validate.go:27-36** (`foldLogin`)
- Assumes: the same claim as PS2, which is why new logins are lower-cased.
- Component: Postfix 3.7.11.
- Justified by: "audit 2026-10-12, H4" only.
- Check in: as PS2.
- **[checked: true]**
- **GUESSED**

**PS4. sender.go:91-112** (`Bindings.fresh` parsing)
- Assumes: texthash lines are `<key><whitespace><value>`, `#` starts a comment, and the value is a comma list.
- Component: Postfix 3.7.11.
- Justified by: a reference to internal/postfix renderSenderLoginMaps only.
- Check in: src/util/dict_thash.c `dict_thash_open` (uses `readllines`, so a line that starts with whitespace continues the previous one); src/smtpd/smtpd_check.c:3934 (owners split on `CHARS_COMMA_SP`).
- Note: this file joins whitespace-separated logins into one ("a b" becomes "ab"). That is harmless for the file the panel writes.
- **GUESSED**

**PS5. sender.go:98, 126**
- Assumes: keys and the looked-up address compare in lower case.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/util/dict_thash.c (`DICT_FLAG_FOLD_FIX`); src/global/mail_addr_find.c (key lower-casing; lookups use the externalised, quoted form).
- **GUESSED**

**PS6. sender.go:130-158** (`withoutExtension`, `extensionDelimiter`)
- Assumes: Postfix's second try is the address without its extension, cut at the first `+`, and an address whose local part begins with the delimiter has no extension.
- Component: Postfix 3.7.11.
- Justified by: a reference to build/postfix-config.sh `recipient_delimiter=+` only.
- Check in: src/global/split_addr.c `split_addr` / `split_addr_internal` (special local parts such as postmaster and MAILER-DAEMON); src/global/mail_addr_find.c:362.
- **GUESSED**

**PS7. internal/postfix/postfix.go:58-61, 111-113**
- Assumes: a comma-separated list of logins on one line "is how Postfix expects multiple owners".
- Component: Postfix 3.7.11.
- Justified by: architecture.md only.
- Check in: src/smtpd/smtpd_check.c:3934 `mystrtok(&cp, CHARS_COMMA_SP)`; postconf(5) smtpd_sender_login_maps.
- **[checked: true]**
- **GUESSED**

**PS8. postfix.go:127-134, inbound.go:120-123** (forbidden characters)
- Assumes: whitespace, comma, backslash and "the colon a lookup key cannot carry" would break a map line.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: postmap(1) "INPUT FILE FORMAT"; src/util/dict_thash.c.
- Note: the colon claim has no basis I know of in texthash (it is special only to postalias). Over-strict, but harmless.
- **GUESSED**

**PS9. postfix.go:152-166, RebuildSenderLoginMaps / RebuildInboundMaps / RebuildDMARCMaps**
- Assumes: `postfix reload` makes Postfix re-read main.cf, master.cf and the texthash tables. texthash is read into memory, so a rewrite needs a reload.
- Component: Postfix 3.7.11.
- Justified by: nothing (architecture.md only).
- Check in: DATABASE_README "texthash"; postfix(1) reload; src/master/master_sig.c and master_avail; src/util/dict_thash.c.
- Note: smtpd processes busy with a client keep the old table until they exit; the audit records this as "measured, not understood".
- **GUESSED**

**PS10. postfix.go:155-158**
- Assumes: "`postfix start-fg` forks a separate master whose PID supervisord does not track", so a SIGHUP to the supervised process would not reach it.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: conf/postfix-script, the `start-fg` branch (`master -i` only when PID 1); src/postfix/postfix.c.
- **GUESSED**

### 4. Postfix 3.7.11: postqueue, postconf and the retry parameters

**PQ1. internal/postfix/queue.go:115-125** (`queueEntryRe` = `` ^([0-9A-Za-z]+)[*!]?\s+\d+\s ``)
- Assumes: a `postqueue -p` entry line is the id with an optional `*` (active) or `!` (hold) flag, then the size.
- Component: Postfix 3.7.11.
- Justified by: an example line only.
- Check in: src/postqueue/showq_compat.c:128-134.
- Why it is wrong: a third flag exists. `if (forced_expire) vstring_strcat(id_status, "#")` gives `ID#`, `ID*#` or `ID!#` after `postsuper -e`. The regex does not match those, so QueueIDs omits a message that is still queued, and the reconcile sweep can close its row as bounced.
- **[checked]**
- **WRONG?**

**PQ2. internal/postfix/queue_test.go:75-95** (fixture "a real listing"; the comment says the reason line "starts at the left margin just as an entry does")
- Assumes: the fixture is a real listing, and the reason line starts at the left margin.
- Component: Postfix 3.7.11.
- Justified by: the fixture says "real" without saying where or when it was seen. Its reason line sits at column 0.
- Check in: src/postqueue/showq_compat.c:161-167 (`padding = 76 - strlen(reason)`).
- Why it is wrong: the reason is right-aligned to column 76 and starts at the margin only when it is 76 characters or longer. The fixture is evidently composed, but it is not labelled so.
- **WRONG?** (the fixture label and comment; the parser itself copes)

**PQ3. queue.go:92-97**
- Assumes: `postqueue -p` covers the maildrop, incoming, active, deferred and hold queues.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/showq/showq.c `showq_service` (queue list).
- **GUESSED**

**PQ4. queue.go:44-73**
- Assumes: the unprivileged panel may run `postqueue -p`; failure is a non-zero exit with the reason in the output; "Mail queue is empty" is printed for an empty queue.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/postqueue/postqueue.c `show_queue` (authorized_mailq_users); src/postqueue/showq_compat.c:214-216.
- **GUESSED**

**PQ5. internal/postfix/queuejson.go:202-241**
- Assumes: `postqueue -j` writes one JSON object per line with `queue_name`, `queue_id`, `arrival_time` (epoch seconds), `sender` and `recipients[].address`.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/postqueue/showq_json.c `showq_message` (~128-170); postqueue(1) "-j".
- **GUESSED**

**PQ6. queuejson.go:152**
- Assumes: the queue names are "deferred", "active", "hold", "incoming" and "maildrop".
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: postqueue(1) "-j"; src/global/mail_queue.h.
- **GUESSED**

**PQ7. queuejson_test.go:10-15** (fixture)
- Assumes: the JSON lines are the shape `postqueue -j` writes.
- Component: Postfix 3.7.11.
- Justified by: nothing. The fixture is not labelled as seen or composed; its field set looks taken from postqueue(1).
- Check in: src/postqueue/showq_json.c.
- **GUESSED**

**PQ8. internal/postfix/retry.go:28-41** (`DefaultRetryPolicy`)
- Assumes: the compiled-in values are 300s, 300s, 4000s, 5d, 5d and 0.
- Component: Postfix 3.7.11.
- Justified by: "Postfix 3.x compiled-in values", with no source.
- Check in: src/global/mail_params.h:770-793 (`DEF_QUEUE_RUN_DELAY "300s"`, `DEF_MAX_BACKOFF_TIME "4000s"`, `DEF_MAX_QUEUE_TIME "5d"`, `DEF_DSN_QUEUE_TIME "5d"`, `DEF_DELAY_WARN_TIME "0h"`).
- **[checked: true]**
- **GUESSED**

**PQ9. retry.go:58-80**
- Assumes: `postconf -h k1 ... k6` prints one value per line, in argument order.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/postconf/postconf_main.c `pcf_show_main_params` / `pcf_print_parameter`.
- **GUESSED**

**PQ10. internal/postfix/duration.go:157-158** (`ParseDuration`: "A bare number is seconds")
- Assumes: a time value without a unit is in seconds.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/util/conv_time.c `conv_time` (`switch (*end ? *end : def_unit)`); src/global/mail_conf_time.c:156-161 (`get_def_time_unit(name, defval)`, which takes the unit from the default value).
- Why it is wrong: the default unit is per parameter. It is d for maximal_queue_lifetime and bounce_queue_lifetime ("5d") and h for delay_warning_time ("0h"). `postconf -h` prints the value as written in main.cf, so `maximal_queue_lifetime = 3` (3 days) would read as 3 seconds.
- **[checked]**
- **WRONG?**

**PQ11. duration.go:158-159** ("Values may concatenate units (`1h7m`), matching Postfix's own conv_time")
- Assumes: Postfix accepts concatenated units.
- Component: Postfix 3.7.11.
- Justified by: the comment names conv_time, but without version, file or function location, and the claim is false.
- Check in: src/util/conv_time.c `conv_time`.
- Why it is wrong: conv_time reads one number and one unit letter. The trailing-text check is commented out (`/* || (*end != 0 && end[1] != 0) */`), so Postfix reads "1h7m" as 1 hour while the panel reads 1h7m. duration_test.go:21 pins the wrong reading.
- **[checked]**
- **WRONG?**

**PQ12. retry.go:132-141** (`LongestLifetime`)
- Assumes: bounce_queue_lifetime applies to any mail from the null sender, including mail an application submits itself.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/qmgr/qmgr_message.c / qmgr_active.c (choice between var_dsn_queue_time and var_max_queue_time); postconf(5) bounce_queue_lifetime.
- **GUESSED**

**PQ13. retry_test.go:12, 58, 69** (fixture values "300s", ..., "0")
- Assumes: these are values `postconf -h` prints.
- Component: Postfix 3.7.11.
- Justified by: nothing. They are not labelled; the stock delay_warning_time prints "0h", not "0", so they are evidently composed.
- Check in: src/global/mail_params.h:793.
- **GUESSED**

### 5. Postfix 3.7.11: the inbound relay and DMARC maps

**PI1. internal/postfix/inbound.go:58-85** (`renderInboundMaps`)
- Assumes: relay_domains takes `domain OK`; transport takes `domain smtp:[host]:port`; relay_recipient_maps takes `addr OK` and `@domain OK` as a catch-all.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: transport(5); postconf(5) relay_domains and relay_recipient_maps; src/global/mail_addr_find.c (the `@domain` step).
- **GUESSED**

**PI2. inbound.go:87-92** (`inboundNexthop`)
- Assumes: brackets disable the MX lookup and also delimit a bare IPv6 address.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: src/util/host_port.c `host_port`; transport(5) and smtp(8) next-hop syntax.
- Note: whether a bare `[2001:db8::1]` is accepted, or `[ipv6:...]` is needed, is open.
- **GUESSED**

**PI3. inbound.go:72** (tls_policy keyed by `[host]:port`; levels may, encrypt, none)
- Assumes: smtp_tls_policy_maps looks up the next hop exactly as written in transport, and these three levels exist.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: TLS_README "TLS policy table"; src/smtp/smtp_tls_policy.c `policy_lookup`.
- **GUESSED**

**PI4. internal/postfix/dmarc.go:224-251**
- Assumes: `addr OK` works for check_recipient_access; `addr dmarc-ingest:` (empty next hop) works in a transport map with address keys; `domain OK` works in relay_domains.
- Component: Postfix 3.7.11.
- Justified by: nothing.
- Check in: transport(5) ("transport:" with an empty next hop); access(5).
- **GUESSED**

### 6. Cyrus SASL 2.1.28 (saslpasswd2, sasldb2) and Berkeley DB db_dump

**S1. internal/app/sasl.go:36-60** (`Set`: `-p -c -f path -u realm -- login`)
- Assumes: `-p` reads the password from stdin without prompting; `-c` creates or overwrites an existing entry in place; `--` ends options.
- Component: Cyrus SASL 2.1.28 (Debian bookworm).
- Justified by: flag glosses only, with no source.
- Check in: utils/saslpasswd.c `main` (`getopt(argc, argv, "vpcdnf:u:a:h?")` at :306, **[checked]**, so glibc getopt handles `--`) and the `sasl_setpass(..., SASL_SET_CREATE)` call at :419-425; lib/server.c `sasl_setpass` for what CREATE does to an existing user (open).
- **GUESSED**

**S2. sasl.go:63-75** (`Delete`: "A missing account is not treated as an error, so deletion is idempotent")
- Assumes: deleting an account that does not exist succeeds.
- Component: Cyrus SASL 2.1.28.
- Justified by: nothing, and no code makes it so.
- Check in: utils/saslpasswd.c:419-457 (`if (result != SASL_OK) exit_sasl(result, errstr)`); sasldb/db_berkeley.c `_sasldb_putdata` ~:340-345 (delete of a missing key gives DB_NOTFOUND → SASL_NOUSER).
- Why it is likely wrong: `saslpasswd2 -d` on a missing user then exits non-zero. Service.Delete would refuse to delete the application, and PurgeDomainSASL would fail the domain delete.
- **WRONG?**

**S3. sasl.go:81-88, 110-112, 146-152** (`Secret`, `parseSASLSecret`)
- Assumes: sasldb2 keeps the plaintext `userPassword`, keyed by `login\0realm\0userPassword`, and the value carries no NUL terminator.
- Component: Cyrus SASL 2.1.28.
- Justified by: nothing.
- Check in: sasldb/allockey.c `_sasldb_alloc_key`; plugins/sasldb.c `sasldb_auxprop_store`; sasldb/db_berkeley.c `_sasldb_putdata`.
- **GUESSED**

**S4. internal/app/password.go:207-209** ("sasldb2 keeps only the hashed form")
- Assumes: sasldb2 stores a hash of the password.
- Component: Cyrus SASL 2.1.28.
- Justified by: nothing.
- Check in: as S3.
- Why it is wrong: it contradicts sasl.go:83-85 and `Secret()`, which reads the plaintext password back from sasldb2 for exports.
- **WRONG?**

**S5. sasl.go:18, 29-31**
- Assumes: the realm given to `-u` (SELFPOST_HOSTNAME) is the realm Cyrus looks the account up under when Postfix authenticates with smtpd_sasl_local_domain empty.
- Components: Cyrus SASL 2.1.28 and Postfix 3.7.11.
- Justified by: nothing here; build/postfix-config.sh:189-194 explains it without a source.
- Check in: Postfix src/xsasl/xsasl_cyrus_server.c `xsasl_cyrus_server_create` (`sasl_server_new` arguments); cyrus lib/canonusr.c `_canonuser_internal`; lib/server.c `sasl_server_new` (serverFQDN as the default realm).
- **GUESSED**

**S6. validate.go:38-42**
- Assumes: an `@` in a login would change the account's identity, because sasldb2 separates user from realm with it.
- Component: Cyrus SASL 2.1.28.
- Justified by: nothing.
- Check in: lib/canonusr.c `_canonuser_internal` (splits user@realm).
- **GUESSED**

**S7. validate.go:61-66** ("saslpasswd2 reads the passphrase from stdin and a newline would silently truncate it")
- Assumes: a newline in the password would cut it short.
- Component: Cyrus SASL 2.1.28.
- Justified by: nothing. service_test.go:319 repeats the claim.
- Check in: utils/saslpasswd.c `read_password` (:129-226).
- Why it is wrong: saslpasswd2 does one `read()` of up to PW_BUF_SIZE (2048) bytes and strips only a single trailing `\n`. An embedded newline is kept in the password, not truncated; only a trailing one is lost. Rejecting control characters is still sensible, but the stated reason is false, and the 1024 limit has no stated basis.
- **[checked]**
- **WRONG?**

**S8. sasl.go:110-153, 175-185** (db_dump output)
- Assumes: db_dump prints a header ending in `HEADER=END`, then alternating key and value lines, each " <hex>", then `DATA=END`; and Debian's sasldb2 is a Berkeley DB file.
- Components: Berkeley DB 5.3 db_dump (Debian db-util) and Cyrus SASL 2.1.28.
- Justified by: nothing.
- Check in: Berkeley DB db_dump(1) "dump format"; cyrus-sasl2 debian/rules:43-44, 189 (built with Berkeley DB, **[checked]**); sasldb/db_berkeley.c.
- **GUESSED**

**S9. sasl_test.go:69-80** (`makeDump`: "the same shape `db_dump <sasldb2>` emits")
- Assumes: the composed document has the shape of real db_dump output.
- Component: Berkeley DB 5.3 db_dump.
- Justified by: nothing. It is composed, but presented as "the same shape", not labelled composed and not seen; real output also has `db_pagesize=` and similar header lines.
- Check in: db_dump(1).
- **GUESSED**

### 7. OpenDKIM 2.11.0~beta2

**D1. internal/domain/opendkim.go:233-234** (KeyTable line)
- Assumes: a KeyTable line is `<keyname> <domain>:<selector>:<keypath>`.
- Component: OpenDKIM 2.11.0~beta2.
- Justified by: a format gloss only.
- Check in: opendkim/opendkim.conf.5 "KeyTable"; opendkim/opendkim.c `dkimf_add_signrequest` (key lookup and the colon split).
- **GUESSED**

**D2. opendkim.go:235-236, sender.go:197** (SigningTable `refile:` line)
- Assumes: a SigningTable line is `*@<domain> <keyname>`, and the pattern matches exactly that domain.
- Component: OpenDKIM 2.11.0~beta2.
- Justified by: a format gloss only.
- Check in: opendkim/opendkim-db.c `dkimf_db_open` `case DKIMF_DB_TYPE_REFILE` (~2300-2400, `REG_ICASE` when the ICASE flag is set); opendkim/util.c:1605 `dkimf_mkregexp` (prepends `^`, turns `*` into `.*`).
- **GUESSED**

**D3. sender.go:186-209** (`Signers.Signs`), **opendkim.go:172-199** (`signs`)
- Assumes: OpenDKIM signs mail whose From: is at domain d exactly when the SigningTable has a `*@d` line. Subdomains and the case of the address do not change that.
- Component: OpenDKIM 2.11.0~beta2.
- Justified by: nothing.
- Check in: opendkim/opendkim.c `dkimf_apply_signtable` (how the address is lower-cased and looked up); opendkim/util.c `dkimf_mkregexp` (whether `$` is appended).
- **GUESSED**

**D4. sender.go:23-27, 318-323**
- Assumes: OpenDKIM chooses the signing key from the From: line, using "a parser of its own".
- Component: OpenDKIM 2.11.0~beta2.
- Justified by: nothing.
- Check in: opendkim.conf(5) SenderHeaders (default From); opendkim/opendkim.c `mlfi_eoh` (picks the sender header); libopendkim/dkim-mailparse.c `dkim_mail_parse`.
- **GUESSED**

**D5. internal/milter/testdata/from-lines-passed.txt** (header lines 23-32)
- Assumes: OpenDKIM signs each of these From: lines for {domain} and nothing else.
- Component: OpenDKIM 2.11.0~beta2.
- Justified by: "which key OpenDKIM signs with, seen and not reasoned", backed by the e2e test/e2e/dkim_from_test.go, which re-measures on every run. The lines themselves are composed inputs; the file says what was seen, but not that the lines are composed.
- Check in: libopendkim/dkim-mailparse.c `dkim_mail_parse`.
- **MEASURED**

**D6. internal/milter/testdata/from-lines-other.txt** (header lines 1-8)
- Assumes: the e2e signs none of these lines for {other}.
- Component: OpenDKIM 2.11.0~beta2.
- Justified by: the e2e checks that nothing is signed for {other}. The file states the expectation but says neither "seen" nor "composed".
- Check in: libopendkim/dkim-mailparse.c.
- **GUESSED** (label missing)

**D7. opendkim.go:263-273** (`reloadViaSupervisor`)
- Assumes: SIGUSR1 makes OpenDKIM re-read KeyTable and SigningTable; "opendkim's documented reload signal".
- Component: OpenDKIM 2.11.0~beta2.
- Justified by: nothing beyond the claim; no version or page.
- Check in: opendkim/opendkim.c:5706-5718 `dkimf_sighandler` (USR1 sets `reload` only `if (conffile != NULL)`; build passes `-x`) and the `dkimf_reloader` thread; opendkim.8 "SIGNALS".
- **[checked: true]**
- **GUESSED**

**D8. internal/domain/dkim.go:307-338, opendkim.go:118-135**
- Assumes: OpenDKIM loads a PKCS#1 "RSA PRIVATE KEY" PEM from a group-readable 0640 file.
- Component: OpenDKIM 2.11.0~beta2.
- Justified by: a reference to build/opendkim.conf and entrypoint.sh only.
- Check in: opendkim/opendkim.c (key load into `dkim_sign`); libopendkim/dkim.c `dkim_sig_load_privkey` / PEM read; opendkim.conf(5) RequireSafeKeys (set to no in build/opendkim.conf).
- **GUESSED**

**D9. dkim.go:348-361** (`dkimRecord`: "mirrors what opendkim-genkey emits: v=DKIM1, sha256, RSA")
- Assumes: opendkim-genkey writes `v=DKIM1; h=sha256; k=rsa; p=...`.
- Component: OpenDKIM 2.11.0~beta2.
- Justified by: nothing.
- Check in: opendkim/opendkim-genkey.in:242-247, 276.
- Why it is wrong (minor): `h=` is emitted only with `--hash-algorithms`, so the default output is `v=DKIM1; k=rsa; p=...`. The record the panel builds is still valid, but it does not mirror genkey.
- **[checked]**
- **WRONG?**

**D10. opendkim.go:241-244**
- Assumes: the colon and the path separators carry meaning inside a table entry.
- Component: OpenDKIM 2.11.0~beta2.
- Justified by: nothing.
- Check in: opendkim.conf(5) KeyTable; opendkim/opendkim-db.c.
- **GUESSED**

**D11. internal/milter/sender_test.go:16-33** (`senderMaps`, `signingTable` fixtures)
- Assumes: `senderMaps` is "a sender_login_maps file as internal/postfix writes it", and `signingTable` is "OpenDKIM's SigningTable for the same server".
- Components: OpenDKIM 2.11.0~beta2 and Postfix 3.7.11 (table formats).
- Justified by: the descriptions quoted above.
- Check in: internal/postfix renderSenderLoginMaps; internal/domain renderTables.
- Why it is wrong: senderMaps is not sorted as renderSenderLoginMaps sorts it (billing@ comes after invoices@). signingTable's key-name column is `selfpost._domainkey.<d>`, while renderTables writes `<d>`. Both are composed but presented as the real format. Harmless to what the tests check.
- **WRONG?** (labels)

### 8. supervisord 4.2.5

**U1. opendkim.go:263-273**
- Assumes: `supervisor.Signal("USR1", "opendkim")` reaches the opendkim process itself.
- Component: supervisord 4.2.5.
- Justified by: nothing.
- Check in: supervisor/rpcinterface.py `signalProcess`; build/supervisord.conf `command=/usr/sbin/opendkim -f ...` (foreground, so the PID is opendkim's).
- **GUESSED**

**U2. postfix.go:152-166**
- Assumes: `supervisor.Start("postfix-reload")` runs `postfix reload` as root, and the start call reports its exit.
- Component: supervisord 4.2.5.
- Justified by: nothing.
- Check in: supervisor/rpcinterface.py `startProcess` (with `startsecs=0`, `exitcodes=0` in build/supervisord.conf).
- **GUESSED**

### 9. rspamd sidecar (deploy/antispam: image rspamd/rspamd:3.11, not a bookworm package)

**R1. internal/milter/inbound.go:82-87**
- Assumes: "The filter replaces headers of these names, so the last one read is its own".
- Component: rspamd 3.11.
- Justified by: nothing here; deploy/antispam/rspamd/milter_headers.conf:8 states it without a source.
- Check in: rspamd src/plugins/lua/milter_headers.lua (the `remove` handling of x-spamd-result and x-rspamd-action).
- **GUESSED**

**R2. inbound.go:199-204** (score and symbol formats)
- Assumes: X-Spamd-Result has the form `default: False [7.20 / 15.00]` and each symbol reads `SYMBOL(weight)[options]`.
- Component: rspamd 3.11.
- Justified by: nothing. The fixture at inbound_test.go:37-38 ("A header of the kind the documented rspamd sidecar adds") is not labelled seen or composed.
- Check in: rspamd src/plugins/lua/milter_headers.lua, the `x-spamd-result` routine.
- **GUESSED**

**R3. inbound.go:232-235, inbound_test.go:236-237**
- Assumes: "rspamd writes symbols heaviest first".
- Component: rspamd 3.11.
- Justified by: nothing.
- Check in: as R2 (the sort order).
- **GUESSED**

**R4. inbound.go:110-119** (`FilterSignature`), **inbound_test.go:344-349**
- Assumes: rspamd computes the same HMAC-SHA256 signature of the queue id as the panel.
- Component: rspamd 3.11.
- Justified by: "What rspamd 3.11 wrote for this key and queue id in a throwaway container (dev trial, 2026-10-12)". The deployed image tag is also 3.11.
- Check in: deploy/antispam/rspamd/milter_headers.conf:25-42.
- **MEASURED**

## Part C - build/, deploy/, internal/inbound, internal/dmarc, internal/backup, test/e2e

Tree at `0f984ff` (2026-10-14). Read-only inventory. Upstream paths in "Check" refer to the
Debian sources unpacked under `dev/upstream/` (postfix-3.7.11, opendkim-2.11.0~beta2,
cyrus-sasl2-2.1.28+dfsg, supervisor-4.2.5, logrotate-3.21.0). Where a quick grep there already
answered the question, the Check line says "grep: consistent" or "grep: contradicts".
That is a pointer for the next stage, not a full verification.

Ratings (about the **justification in the tree**, not about whether the claim is true):
- **CITED**: the comment names the source or document.
- **MEASURED**: the comment says it was seen, measured or observed in an incident. "(undated)" means the rule's date is missing.
- **GUESSED**: no citation and no measurement stated. The claim may still be true. "grep: consistent" marks the ones that a source line already supports.
- **WRONG?**: there is a reason to think the claim, or the behaviour that depends on it, is false.

"e2e:" says whether the suite exercises the behaviour on every run. That is not a citation, but it tells you how risky the claim is.

The internal/milter/inbound.go entries (the inbound journal milter) and cmd/panel/dmarc_ingest.go
(the far end of the dmarc-ingest pipe) are included because the brief names "the inbound journal
milter's assumptions" and "the dmarc-ingest pipe from Postfix".

---

### 1. Postfix 3.7.11 (Debian bookworm package)

**P1. build/postfix-config.sh:18-25.** The short-form `-o name=value` in master.cf cannot carry whitespace: master splits on it, smtpd dies with "fatal: unexpected command-line argument", and `postconf -P` refuses such values. Restriction lists accept commas instead.
- Justified by: nothing cited. The error text is quoted, from the 1.9.1 crash-loop history (test/e2e/port25_config_check.go:110-114).
- Check: man/man5/master.5 §"Command name + arguments", NOTE 1/NOTE 2 (~l.194-210). NOTE 2 recommends "use the long form ... or use commas instead of spaces". The long form `-o { name = value }` (Postfix ≥ 3.0) preserves whitespace, and the comment never mentions it. grep: consistent for the short form.
- Rating: **GUESSED** (true for the short form; incomplete).
- e2e: port25_config_check.go compares the file with `postconf -Ph`.

**P2. postfix-config.sh:35-57 (`master_cf_set_smtp_inet_o`, awk).** The stanza starts at a line `smtp<ws>inet<ws>` and continues over lines that begin with whitespace. Inserting `    -o p=v` right after the header line is valid. A later `postconf -F/-P` keeps the inserted lines.
- Justified by: nothing.
- Check: man/man5/master.5 l.38-42 ("A line that starts with whitespace continues a logical line"); src/postconf/postconf_master.c (how -F/-P rewrite the file).
- Rating: **GUESSED**. e2e: yes (port25_config_check.go).

**P3. postfix-config.sh:61-65, 189-193; entrypoint.sh:10-19.** myhostname is the Cyrus realm. With `smtpd_sasl_local_domain` empty, Postfix hands Cyrus `$myhostname` as serverFQDN and no user realm, and Cyrus then looks accounts up under serverFQDN.
- Justified by: nothing.
- Check: src/xsasl/xsasl_cyrus_server.c `xsasl_cyrus_server_create()` l.338 `SASL_SERVER_NEW(args->service, var_myhostname, args->user_realm ? args->user_realm : NO_AUTH_REALM, ...)` (grep: consistent). Cyrus lib/server.c `sasl_server_new()` for the default realm.
- Rating: **GUESSED**. e2e: AUTH succeeds on every run.

**P4. postfix-config.sh:71-74; postfix-cert-reload.sh:5-8.** "Postfix insists the private key is root-owned and mode 0600." This is the stated reason for copying into /etc/postfix/tls-internal.
- Justified by: nothing.
- Check: src/tls/tls_certkey.c, src/tls/tls_server.c, src/smtpd/smtpd.c `pre_jail_init()` (the key is loaded as root). grep for `S_IRGRP|S_IROTH|readable by` in src/tls, src/smtpd and conf/postfix-script found no mode check. TLS_README only recommends such modes.
- Rating: **WRONG?** No enforcement found. The copy may still be worth keeping (a root-owned 0600 copy is good hygiene), but the reason given is not in the source.

**P5. postfix-config.sh:67-69.** With cert or key missing, master starts and TLS handshakes on 465 fail until the files appear.
- Check: src/smtpd/smtpd.c `pre_jail_init()` (TLS_SERVER_INIT failure) and the wrappermode connection path.
- Rating: **GUESSED**. e2e: no.

**P6. postfix-config.sh:150-157.** The default `maillog_file_prefixes` is `/var, /dev/stdout`, and without `/data` `postfix check` rejects the path.
- Check: src/global/mail_params.h:4336 `DEF_MAILLOG_FILE_PFXS "/var, /dev/stdout"` (grep: consistent); the check is in src/global/maillog_client.c / conf/postfix-script.
- Rating: **GUESSED**.

**P7. postfix-config.sh:163-166.** "postlogd is mandatory whenever maillog_file is set (MAILLOG_README)". The postlog service line follows that README.
- Justified by: names MAILLOG_README, without section or version.
- Check: html/MAILLOG_README.html §"Configuring logging to file".
- Rating: **CITED** (weak: no section or version).

**P8. postfix-config.sh:154-161, 168-173.** With `mydestination=` and `local_recipient_maps=`, `alias_maps=` and `alias_database=` empty, nothing is ever delivered locally. `relayhost=` means direct MX delivery.
- Check: postconf(5) mydestination, local_recipient_maps; src/trivial-rewrite/resolve.c.
- Rating: **GUESSED**.

**P9. postfix-config.sh:180-187.** "auth_only guarantees credentials are never accepted before TLS is up on any port." On 465 it rests on wrappermode; on 587 on encrypt plus auth_only. On 25 it is overridden to `no`, but SASL is off there (`-o smtpd_sasl_auth_enable=no`).
- Check: postconf(5) smtpd_tls_auth_only; src/smtpd/smtpd.c `ehlo_cmd()` (AUTH advertised only when `smtpd_tls_auth_only` permits).
- Rating: **GUESSED**.

**P10. postfix-config.sh:194-201, 490-500.** `smtpd_sasl_type=cyrus` with `smtpd_sasl_path=smtpd` makes Cyrus read **/etc/postfix/sasl/smtpd.conf**.
- Justified by: nothing. Upstream Postfix leaves the path to Cyrus. This only works because of a Debian patch.
- Check: debian/patches/07_sasl_config.diff (`xsasl_getconfpath`, SASL_CB_GETCONFPATH in src/xsasl/xsasl_cyrus_server.c and xsasl_cyrus_log.c); debian/README.Debian:63 "SASL configuration goes in /etc/postfix/sasl".
- Rating: **GUESSED** (Debian-specific dependency, unstated). e2e: AUTH.

**P11. postfix-config.sh:203-205.** "texthash resolves the full address first, then the '@domain' wildcard."
- Justified by: nothing. The order belongs to smtpd's sender-login lookup (`mail_addr_find`), not to texthash.
- Check: src/smtpd/smtpd_check.c `check_sender_login()`; src/global/mail_addr_find.c (user+ext@domain, user@domain, user+ext, user, @domain).
- Rating: **GUESSED** (wording misattributes the behaviour). e2e: wildcard and list applications.

**P12. postfix-config.sh:207-214.** `recipient_delimiter=+` lets `invoices@` in the sender map cover `invoices+march@`, and the panel's milter makes the same second try.
- Check: src/global/mail_addr_find.c (extension stripping by recipient_delimiter); src/smtpd/smtpd_check.c `check_sender_login()`.
- Rating: **GUESSED**. e2e: sender_check_test.go "the listed address with an extension".

**P13. postfix-config.sh:104-105.** texthash needs no postmap, so the unprivileged panel can rewrite the file and ask for a reload. Daemons read the file when they start.
- Check: html/DATABASE_README.html "texthash"; src/util/dict_thash.c.
- Rating: **GUESSED**. Related: smtp_client.go:231-238 (E19).

**P14. postfix-config.sh:216-224.** With no `permit_mynetworks`, no open relay is possible. `reject_sender_login_mismatch` refuses an unowned sender, and (negative_test.go:92-94) `smtpd_delay_reject=yes` by default, so the refusal comes at RCPT.
- Check: src/global/mail_params.h:2420 `DEF_SMTPD_DELAY_REJECT 1` (grep: consistent); src/smtpd/smtpd_check.c.
- Rating: **GUESSED**. e2e: negative checks 1-3.

**P15. postfix-config.sh:226-230.** `smtpd_client_message_rate_limit` per client IP is the level-1 backstop. Not said: anvil exempts `smtpd_client_event_limit_exceptions` (default `$mynetworks`, which is loopback in Debian's main.cf), and `anvil_rate_time_unit` is global to every anvil limit.
- Check: postconf(5) smtpd_client_event_limit_exceptions, anvil_rate_time_unit; mail_params.h (DEF_SMTPD_CEVENT_EXCL).
- Rating: **GUESSED**. e2e: testLevel1RateLimit.

**P16. postfix-config.sh:232-251.** Per-milter `{ socket, default_action=... }` is "Postfix 3.0+ brace syntax". Milters run in order, and a tempfail from the first stops the chain. `non_smtpd_milters` covers sendmail(1) mail. "Bounces Postfix generates itself do not pass it (internal_mail_filter_classes is empty)."
- Justified by: names the Postfix version only.
- Check: html/MILTER_README.html §"Different settings for different Milter applications"; src/milter/milter.c `milter_*_event()` loops; mail_params.h:3544 `DEF_INT_FILT_CLASSES INT_FILT_CLASS_NONE` (grep: consistent).
- Rating: **GUESSED**. e2e: panel_down_defers_mail.

**P17. postfix-config.sh:253-259.** `milter_mail_macros` = "its defaults plus `{client_addr}`".
- Check: mail_params.h:3473 `DEF_MILT_MAIL_MACROS "i {auth_type} {auth_authen} {auth_author} {mail_addr} {mail_host} {mail_mailer}"` (grep: consistent). Note: `i` is empty at MAIL FROM because `smtpd_delay_open_until_valid_rcpt=yes` (mail_params.h:1427). This is harmless: the inbound milter reads `i` at end-of-message (P40).
- Rating: **GUESSED**.

**P18. postfix-config.sh:261-271.** The Postfix defaults are 300 s for content (and 30 s for connect and command).
- Check: mail_params.h:3500-3508 `DEF_MILT_CONN_TIME "30s"`, `DEF_MILT_CMD_TIME "30s"`, `DEF_MILT_MSG_TIME "300s"` (grep: consistent).
- Rating: **GUESSED**.

**P19. postfix-config.sh:277, 288, 457.** The service names `smtps`, `submission` and `smtp` resolve to 465, 587 and 25 without relying on /etc/services, whose presence in bookworm-slim (netbase) is unknown.
- Justified by: nothing (unstated).
- Check: mail_params.h:4376 `DEF_KNOWN_TCP_PORTS "lmtp=24, smtp=25, smtps=submissions=465, submission=587"` (Postfix ≥ 3.6; grep: consistent).
- Rating: **GUESSED**.

**P20. postfix-config.sh:274-276, 481-488.** "Debian ships the smtp delivery agent and others chrooted ... cannot read /etc/resolv.conf", so chroot is turned off for every service. Not said: `cap_drop: ALL` in deploy/docker-compose.yml removes CAP_SYS_CHROOT, so chroot=y services could not start at all.
- Check: Debian master.cf (debian/ and conf/master.cf in the source package); capabilities(7) CAP_SYS_CHROOT.
- Rating: **GUESSED**.

**P21. postfix-config.sh:283-295.** On 587, `smtpd_tls_security_level=encrypt` forces STARTTLS before AUTH, and `postconf -MX` removes a disabled service so the port stops listening after a restart.
- Check: postconf(1) -M/-X.
- Rating: **GUESSED**.

**P22. postfix-config.sh:298-300; test/e2e/process_check.go:260-262.** Debian's stock master.cf enables `smtp/inet`.
- Check: Debian master.cf in the source package.
- Rating: **GUESSED**. e2e: checkInboundRelayOff.

**P23. postfix-config.sh:319-322.** `flags=`, `user=` and `argv=` are pipe(8) arguments. `postconf -P` refuses them ("unmatched service_name/type"), and a bare pipe dies with "fatal: missing user= command-line attribute".
- Justified by: the quoted messages are history from 1.7.0-1.9.5 (port25_features_test.go:17-19, 40-42; port25_config_check.go:34-37), with no date.
- Check: man/man8/pipe.8; src/pipe/pipe.c `get_service_attr()`.
- Rating: **MEASURED** (incident, undated). e2e: dmarc_report_reaches_the_panel.

**P24. postfix-config.sh:323-330.** `${recipient}` is expanded by pipe(8). With `dmarc-ingest_destination_recipient_limit=1` it is a single argument. Process limit 1.
- Check: man/man8/pipe.8 l.258 "expands to as many command-line arguments as there are recipients" (grep: consistent).
- Rating: **GUESSED**.

**P25. postfix-config.sh:331-345; cmd/panel/dmarc_ingest.go (checkIngestService comment in port25_config_check.go:178-182).** pipe(8) gives the command only `export_environment`. The parameter is split at whitespace and `$` is expanded, and its default is `TZ MAIL_CONFIG LANG`. Not said: export_environment is global, so every Postfix daemon gets the DMARC_* variables.
- Check: mail_params.h:2634 `DEF_EXPORT_ENVIRON "TZ MAIL_CONFIG LANG"` (grep: consistent); man/man8/pipe.8 l.409; src/global/clean_env.c.
- Rating: **GUESSED**. e2e: yes.

**P26. postfix-config.sh:353-355, 423-424.** A comma rather than a space between `check_recipient_access` and its table, and between milter groups.
- Check: man/man5/master.5 NOTE 2 ("or use commas instead of spaces").
- Rating: **GUESSED** (grep: consistent).

**P27. postfix-config.sh:364-371; port25_config_check.go:81-84.** `smtpd_reject_unlisted_recipient` is applied after the restriction list "whatever check_recipient_access answered".
- Justified by: nothing. It was learnt from the bug.
- Check: src/smtpd/smtpd_check.c `smtpd_check_rcpt()` l.5175-5179: applied unless `status == SMTPD_CHECK_REJECT`, so an OK does not skip it (grep: consistent).
- Rating: **GUESSED**. e2e: config read-back plus dmarc_report_reaches_the_panel with both features on.

**P28. postfix-config.sh:385-392, 469.** A per-service `-o smtpd_milters=` replaces main.cf's chain for port 25, so OpenDKIM and the journal milter never see inbound mail.
- Check: master.5 `-o`; postconf(5) smtpd_milters.
- Rating: **GUESSED**. e2e: config read-back.

**P29. postfix-config.sh:418-424; internal/milter/inbound.go:82-87.** "Postfix hands a message to its milters one after another at end-of-message", so the last milter sees the headers the filter added, and the last header of a name it reads is the filter's own.
- Justified by: nothing.
- Check: src/milter/milter.c `milter_message()` loop; src/cleanup/cleanup_milter.c (header edits written to the queue file before the next milter reads it).
- Rating: **GUESSED**. e2e: **no** (no filter on the stand).

**P30. postfix-config.sh:454-460.** `-o syslog_name=postfix/inbound` makes this listener log as `postfix/inbound/smtpd[...]`.
- Check: postconf(5) syslog_name; src/global/mail_task.c.
- Rating: **GUESSED**. e2e: the inbound refusal is read into the journal (port25_features_test.go:361-363).

**P31. postfix-config.sh:439-452, 466-467.** `-o message_size_limit=${PORT25_SIZE}` on smtp/inet sets the size limit for port 25 (inbound default 26214400).
- Justified by: nothing.
- Check: src/cleanup/cleanup_init.c:440-451 `cleanup_post_jail()` calls `set_file_limit(var_message_limit)`. cleanup runs with **main.cf's** message_size_limit, which postfix-config.sh never sets, so it is `DEF_MESSAGE_LIMIT 10240000` (mail_params.h:1997). smtpd's own checks (smtpd.c:1995, 3657, 4047) use the -o value. Grep: contradicts.
- Rating: **WRONG?** The EHLO SIZE advertises 25 MiB, but queue files above ~10 MB fail in cleanup. DMARC's 5 MiB is unaffected. e2e: no large message.

**P32. postfix-config.sh:502-512.** `postfix check` exits non-zero on a bad configuration. Not said: it also runs `post-install create-missing`, which builds the queue tree (P34).
- Check: conf/postfix-script l.238-242 `check)`, l.259 `post-install create-missing`.
- Rating: **GUESSED**.

**P33. postfix-wrapper.sh:206-231; supervisord.conf:71-79.** `postfix start-fg` does not exec master when it is not PID 1. It runs master as a child of the shell script, so a signal to the supervised process never reaches master.
- Justified by: nothing.
- Check: conf/postfix-script l.153-161: `1) exec $daemon_directory/master -i ... *) $daemon_directory/master -s` (grep: consistent).
- Rating: **GUESSED**.

**P34. entrypoint.sh:171-175.** "Initialise the persistent queue tree on first start or after restore": `if [ ! -d /data/postfix/queue/active ]; then postfix set-permissions; fi`.
- Justified by: nothing.
- Check: postfix-config.sh, which runs earlier, ends with `postfix check`, and that runs `post-install create-missing` (conf/postfix-script:240, 259; conf/postfix-files:55 `active:d:$mail_owner:-:700`). By the time this test runs, `active/` already exists.
- Rating: **WRONG?** The branch is dead and the queue is built elsewhere. Harmless, but the comment describes a mechanism that does not run. backup.go:92-93 "a start without a queue directory builds an empty one" is true by this other path.

**P35. entrypoint.sh:176-187.** Postfix keeps `public/` and `maildrop/` in group postdrop, and the queue directory and `pid/` owned by root. It warns "not owned by root" otherwise.
- Check: conf/postfix-files:51 `$queue_directory:d:root:-:755`, :64-66 `maildrop ... $setgid_group:730`, `public ... $setgid_group:710`, `pid:d:root:-:755` (grep: consistent).
- Rating: **GUESSED**. e2e: queue_readable_by_the_panel.

**P36. entrypoint.sh:132-137.** "postlogd creates a missing log itself, but at 0600."
- Justified by: nothing here. logrotate-loop.sh:93-95 says "confirmed on a live container" (undated).
- Check: src/util/logwriter.c:85 `safe_open(path, O_CREAT|O_WRONLY|O_APPEND, 0644, ...)` under master's `umask(077)` (src/master/master.c:297) gives 0600 (grep: consistent).
- Rating: **GUESSED** at entrypoint.sh. See L4 for the measured copy.

**P37. logrotate-loop.sh:86-92; logrotate-mail.conf:165-167.** Rotation by rename plus `postfix reload` is "the same mechanism `postfix logrotate` uses". postlogd keeps writing to the renamed inode until the reload.
- Check: conf/postfix-script `logrotate)` case (`mv "$maillog_file" ...`, `kill -HUP` master; grep: consistent). src/postlogd/postlogd.c:212 opens the file once (`logwriter_open_or_die`). The reopen depends on master restarting postlogd on reload, which was not looked at.
- Rating: **GUESSED**. e2e: delivery_log_survives_rotation.

**P38. postfix-cert-reload.sh:46-55, 29-36.** `postfix reload` makes newly started smtpd processes load the renewed certificate. `postconf -h smtpd_tls_cert_file` tells whether the copy is in use.
- Check: src/smtpd/smtpd.c `pre_jail_init()` (the certificate is loaded per process); src/master/master_sig.c / master_conf.c (reload).
- Rating: **GUESSED**. e2e: renewed_certificate_picked_up.

**P39. postfix-config.sh:161 `inet_protocols=all`.** Not said: in a Docker bridge without IPv6, outbound AAAA attempts fail and Postfix falls back to IPv4.
- Check: postconf(5) inet_protocols, smtp_address_preference.
- Rating: **GUESSED** (implicit).

**P40. internal/milter/inbound.go:130-138 (Body reads macro `i`).** The queue id is available at end-of-message for smtpd_milters.
- Check: mail_params.h:3496 `DEF_MILT_EOD_MACROS "i"` (grep: consistent); content and end-of-message events come from cleanup (src/cleanup/cleanup_milter.c), as E17 measured.
- Rating: **GUESSED**. e2e: the inbound journal row is written; the signature path is not tested.

**P41. postfix-config.sh:300-301, 362; internal/postfix (adjacent).** The tls_policy key `[host]:port` matches transport's verbatim nexthop `smtp:[host]:port`.
- Check: html/TLS_README.html l.1246-1256: "the policy table lookup key is the verbatim next-hop ... ':smtp' or ':25' or no port suffix result in different ... keys". Grep: consistent, because both tables write the same `[host]:port`.
- Rating: **GUESSED** (true; noted because it looked suspicious).

**P42. supervisord.conf:60-69 ([program:postfix], no `stopasgroup`/`killasgroup`).** Stopping or restarting the program stops Postfix.
- Justified by: nothing (implicit).
- Check: by P33 the supervised pid is postfix-script's shell and master is its child. supervisor sends SIGTERM to that pid only (supervisor/process.py `kill()` signals the pid unless `killasgroup`). master would be orphaned and a new start-fg would hit "already running" (conf/postfix-script l.127 `master -t`).
- Rating: **WRONG?** (unverified). It matters only for `supervisorctl stop|restart postfix`; nothing in the tree does that.

### 2. Cyrus SASL 2.1.28

**S1. postfix-config.sh:490-500.** smtpd.conf: `pwcheck_method: auxprop`, `auxprop_plugin: sasldb`, `sasldb_path`, `mech_list: PLAIN LOGIN`.
- Check: cyrus-sasl2-2.1.28+dfsg/doc/legacy/options.html (pwcheck_method, auxprop_plugin l.34, sasldb_path l.149).
- Rating: **GUESSED**. e2e: AUTH.

**S2. postfix-config.sh:492-493.** PLAIN and LOGIN are safe "because TLS is mandatory before auth on every port". Rests on P9.
- Rating: **GUESSED**.

**S3. entrypoint.sh:92-100.** smtpd, running as postfix, reads sasldb2 through group selfpost while the panel's saslpasswd2 writes it in place (Berkeley DB, no lock environment). Nothing says what a reader sees during a write.
- Check: cyrus-sasl2 plugins/sasldb.c, sasldb/db_berkeley.c (open flags, locking).
- Rating: **GUESSED** (unaddressed).

**S4. Dockerfile:46-48.** sasl2-bin provides saslpasswd2, libsasl2-modules provides PLAIN/LOGIN, the sasldb auxprop plugin comes from libsasl2-modules-db (a dependency of libsasl2-2), and db-util provides db_dump.
- Check: Debian package lists for bookworm.
- Rating: **GUESSED**.

### 3. OpenDKIM 2.11.0~beta2

**O1. opendkim.conf:175-180.** SIGUSR1, sent through supervisord, reloads KeyTable and SigningTable.
- Check: opendkim/opendkim.8.in:501 "Upon receiving SIGUSR1 ... configuration file ... will be re-read" (grep: consistent for the configuration). The tables being re-opened as part of that: opendkim/opendkim.c around l.5714.
- Rating: **GUESSED**.

**O2. opendkim.conf:190-193.** `refile:` SigningTable lets `*@example.com` patterns match any local part.
- Check: opendkim/opendkim.conf.5 SigningTable, "refile".
- Rating: **GUESSED**. e2e: DKIM verifies.

**O3. opendkim.conf:195-199.** Group-readable keys trip the default safe-key check, so it is disabled.
- Check: opendkim/opendkim.c `dkimf_securefile()` l.4698; `RequireSafeKeys` l.6291.
- Rating: **GUESSED**.

**O4. opendkim.conf:187 `UMask 007` plus entrypoint.sh:144-155.** The socket comes out group read-write and inherits group selfpost from the setgid directory, so Postfix can connect.
- Check: opendkim.conf.5 UMask; Linux setgid-directory semantics (K3).
- Rating: **GUESSED**. e2e: signing works.

**O5. entrypoint.sh:81-86; opendkim.conf:182-184.** OpenDKIM starts cleanly with empty KeyTable and SigningTable.
- Rating: **GUESSED**. e2e: startup on an empty /data.

**O6. supervisord.conf:38; opendkim.conf:203-204.** `-f -x conf` plus `UserID opendkim` and `Background no`: the supervised pid is OpenDKIM itself, now running as opendkim.
- Check: opendkim.8 -f, -x; opendkim.conf.5 UserID.
- Rating: **GUESSED**.

### 4. supervisor 4.2.5

**V1. supervisord.conf:3-6.** `priority=` enforces start order. It does not wait for readiness, which is why the wrapper waits.
- Check: docs `[program:x]` priority; supervisor/supervisord.py (startup order).
- Rating: **GUESSED**.

**V2. supervisord.conf:20-35.** `[unix_http_server] chown=root:selfpost chmod=0770` with no auth is enough to gate access.
- Check: docs `[unix_http_server]`; supervisor/http.py.
- Rating: **GUESSED**.

**V3. supervisord.conf:48-52 (`user=panel`).** Unstated: the panel gets its supplementary groups selfpost and postdrop. The Dockerfile's postdrop membership (Dockerfile:73-84) depends on it.
- Check: supervisor/options.py `drop_privileges()` l.1382-1388: `os.setgroups` from `grp.getgrall()` membership (grep: consistent).
- Rating: **GUESSED**. e2e: queue_readable_by_the_panel.

**V4. supervisord.conf:71-90 ([program:postfix-reload]).** With `startsecs=0` and `exitcodes=0`, a run "never trips the crashexit listener".
- Check: supervisor/process.py `finish()` l.551-612: with startsecs=0, `too_quickly` is never true, so the state becomes EXITED, never BACKOFF or FATAL, even on a non-zero exit (grep: consistent).
- Rating: **GUESSED**.

**V5. supervisord.conf:108-111 ([program:logrotate]).** "Exits non-zero when the config is group/other-writable ...; autorestart surfaces BACKOFF on the Status page."
- Check: same `finish()`. With `startsecs=0`, an exit right after start is RUNNING to EXITED, and `autorestart=true` respawns at once. The program never reaches BACKOFF or FATAL; supervisorctl shows it flapping between RUNNING/STARTING and EXITED.
- Rating: **WRONG?** (grep: contradicts).

**V6. postfix-wrapper.sh:197-199; supervisord.conf:8-10.** When the milter sockets are not ready within 30 s, the wrapper "exits non-zero WITHOUT starting Postfix, so supervisord/Docker see an explicit startup failure". "If any managed process exhausts its restart retries (FATAL), the crashexit event listener brings the whole container down."
- Check: process.py `finish()`. The default `startsecs=1` has long passed after a 30 s wait, so the exit is RUNNING to EXITED and autorestart retries forever. It never becomes FATAL, crashexit never fires, and Docker sees nothing. FATAL only happens for exits within one second, three times in a row.
- Rating: **WRONG?** (grep: contradicts).

**V7. crashexit.py:136-170.** The event listener protocol: write `READY\n`; read one header line of `key:value` tokens; read `len` bytes of payload; answer `RESULT 2\nOK`.
- Check: supervisor/childutils.py (listener helpers, RESULT token l.74); docs events.html §"Event Listener Notification Protocol".
- Rating: **GUESSED**.

**V8. crashexit.py:131-134, 168.** The listener's parent is supervisord. SIGTERM shuts supervisord down; it is PID 1 (entrypoint.sh:189 exec), so the container exits and "Docker's restart policy can recreate it cleanly". Docker restarts the same container; it does not recreate it. The stale sockets are handled by entrypoint.sh:163.
- Rating: **GUESSED** (inexact wording).

**V9. supervisord.conf:123-125; sender_check_test.go:113-116.** "crashexit fires on PROCESS_STATE_FATAL only; a supervisor stop is STOPPED."
- Check: supervisor/process.py state transitions; supervisor/events.py.
- Rating: **GUESSED**. e2e: panel_down_defers_mail checks the container is alive.

**V10. test/e2e/process_check.go:203-206, 236-239.** "postfix-reload is deliberately excluded: autostart=false, its healthy resting state is STOPPED."
- Check: after any run, process.py `finish()` l.606 changes the state to EXITED (expected). STOPPED holds only until the first start. The check passes because it runs before anything asks for a reload.
- Rating: **WRONG?** (claim false; the test passes because of ordering).

**V11. process_check.go:217-219.** `supervisorctl status` exits non-zero when any program is not RUNNING. The output is parsed as `name STATE ...`.
- Check: supervisor/supervisorctl.py `LSBStatusExitStatuses` (l.56) and `do_status`.
- Rating: **GUESSED** (harmless: the exit status is ignored).

### 5. logrotate 3.21.0

**L1. logrotate-loop.sh:97-99.** "daily" is tracked in /var/lib/logrotate/status, so polling more often is safe. Not said: the state file is on the container's ephemeral filesystem, so a recreate forgets the last rotation.
- Check: debian/rules:11 `--with-state-file-path=/var/lib/logrotate/status` (grep: consistent); logrotate.c `findState()` (behaviour for an unknown file).
- Rating: **GUESSED**.

**L2. logrotate-loop.sh:105-106, 131-136; Dockerfile:101-104.** logrotate ignores a configuration writable by group or others and exits 0. Its messages contain "Ignoring" and "Potentially dangerous mode".
- Check: config.c:1054-1066: "Potentially dangerous mode" is only a warning, given when `(mode & 07533) != 0400`, so an executable bit also triggers it and the loop treats that as fatal. "Ignoring ... writable by group or others" leads to `return 0`. The config must also be owned by root (config.c:1069). Grep: consistent.
- Rating: **GUESSED**. e2e: TestImageBuildPreservesLogrotateMode, checkLogrotateConfigMode.

**L3. entrypoint.sh:134-137.** logrotate refuses a log whose directory is writable by a non-root group, so the directory is 2750.
- Check: logrotate.c:1390-1392 `(sb.st_gid != 0 && (sb.st_mode & S_IWGRP)) || (sb.st_mode & S_IWOTH)` gives "skipping ... parent directory has insecure permissions" (grep: consistent).
- Rating: **GUESSED**.

**L4. logrotate-loop.sh:92-95.** `create` instead of `nocreate`, because "a postlogd-triggered recreate lands the file at 0600 owned by postfix ... confirmed on a live container".
- Check: as for P36.
- Rating: **MEASURED** (undated).

**L5. logrotate-mail.conf:157-167.** `delaycompress` keeps mail.log.1 uncompressed, and `create 0640 postfix selfpost` recreates the file.
- Check: logrotate(8) delaycompress and create.
- Rating: **GUESSED**. e2e: logrotate_check.go asserts both.

**L6. delivery_log_test.go:56-57.** "The rotation ends with `postfix reload`, which fails while Postfix is still starting." `postfix status` exits non-zero while Postfix is not running.
- Check: conf/postfix-script `reload)` and `status)` cases.
- Rating: **GUESSED**.

### 6. Debian packaging and base image

**D1. Dockerfile:33, 41-53.** The versions everything cites (Postfix 3.7.11, OpenDKIM 2.11.0~beta2, SASL 2.1.28, supervisor 4.2.5, logrotate 3.21.0) are what the image runs. Nothing pins them: the `debian:bookworm-slim` tag and `apt-get install` without versions take whatever the current point release ships.
- Rating: **GUESSED**. This underlies every citation in this file.

**D2. Dockerfile:37-40.** The debconf keys `postfix/mailname` and `postfix/main_mailer_type` give a non-interactive install.
- Check: postfix-3.7.11/debian/templates.
- Rating: **GUESSED**.

**D3. crashexit.py `#!/usr/bin/env python3`.** python3 is not installed explicitly; it arrives through the supervisor package's Depends.
- Rating: **GUESSED** (implicit).

**D4. postfix-cert-reload.sh:41-42.** The openssl CLI is not installed explicitly; it arrives through ca-certificates' Depends.
- Rating: **GUESSED** (implicit).

**D5. Dockerfile:65-71.** gid 998 is free after the package installs.
- Rating: **GUESSED** (the build would fail loudly if not).

**D6. Dockerfile:73-84.** postqueue and postdrop are setgid postdrop; `no-new-privileges` disables the setgid transition on exec; membership in postdrop gives the same access.
- Check: kernel Documentation/userspace-api/no_new_privs.rst; conf/postfix-files (the postqueue and postdrop entries).
- Rating: **GUESSED**. e2e: queue page and notifications.

### 7. Linux kernel, Docker, Compose

**K1. deploy/docker-compose.yml:93-131.** The capability set: NET_BIND_SERVICE; CHOWN; FOWNER (chmod of panel-owned files as root); FSETID (the setgid bit when not a member of the file's group); SETUID/SETGID; DAC_OVERRIDE; KILL ("cross-uid kill() still consults CAP_KILL"). Unlisted: SYS_CHROOT is dropped (P20). pipe(8) switching to user=panel and supervisord switching users need SETUID/SETGID.
- Check: capabilities(7); kernel/signal.c `kill_ok_by_cred()`; fs/attr.c (`setattr_prepare` and the S_ISGID clearing).
- Rating: **GUESSED**. e2e: the stand runs the shipped compose file.

**K2. docker-compose.yml:119-120.** `no-new-privileges` does not stop root from calling setuid() in supervisord and pipe; it only affects exec-time escalation.
- Check: no_new_privs.rst.
- Rating: **GUESSED** (implicit).

**K3. entrypoint.sh:78-80, 95-96, 144-151.** Files, directories and unix sockets created in a setgid directory inherit its group.
- Check: Linux `inode_init_owner()` (fs/inode.c).
- Rating: **GUESSED**.

**K4. entrypoint.sh:156-163.** A stale socket file left by a killed container satisfies postfix-wrapper's `-S` test.
- Rating: **GUESSED** (true by POSIX).

**K5. deploy/antispam/docker-compose.antispam.yml:140-141.** "The milter sees the real client IP, HELO and PTR." Postfix's per-IP limits (P15) and the inbound journal's client address rest on the same assumption. It holds with iptables DNAT; it does not hold where docker-proxy relays the connection (IPv6 without ip6tables, rootless Docker, hairpin), because then the source is the bridge gateway.
- Check: Docker docs "Packet filtering and firewalls" / userland-proxy.
- Rating: **GUESSED**. Related: mail_helpers.go:67-72 says the observed address "depends on Docker's NAT".

**K6. test/e2e/compose.override.yml:12-15 and the deploy fragments (`ports: !override`).** The `!override` merge tag needs Compose ≥ 2.24.4.
- Check: Compose docs "Merge, !override".
- Rating: **GUESSED**.

**K7. deploy/caddy, nginx and traefik fragment headers.** "Compose resolves every relative path in both files against the directory of the FIRST -f file."
- Check: Compose docs "project directory".
- Rating: **GUESSED**.

**K8. compose.override.yml:24-27.** "compose build.context paths follow the project directory, unlike bind-mount volume paths".
- Check: the same file's `./dns-stage` and `./mail-stage` volumes resolve into `.stage`, which is where stage.go:30 creates them. Volumes follow the project directory too, and this contradicts K7.
- Rating: **WRONG?** (comment only).

**K9. test/e2e/stage.go:57-59 "compose reads neither"; stack.go:190-195 ("interpolates ... straight from the process environment").** Compose loads `.env` from the project directory (`--project-directory .stage`) for interpolation. It is harmless here because the value is the same.
- Check: Compose docs "Set environment variables with .env".
- Rating: **WRONG?** (comment only).

**K10. smtp_client.go:157-164; port25_client.go:60-62.** Docker's port proxy accepts on the host side before anything listens in the container, then resets the connection.
- Justified by: "how the level-1 rate-limit case once took a reset for its limit".
- Rating: **MEASURED** (incident, undated).

**K11. entrypoint.sh:62-64; hostname_gate_test.go:19-20.** Go's testing.TempDir is 0700.
- Check: Go `os.MkdirTemp` docs.
- Rating: **GUESSED**.

### 8. rspamd 3.11 (sidecar) and the inbound journal milter's view of it

**R1. multimap.conf:216-217; internal/inbound/lists.go:30-35.** "rspamd rereads a file when it changes, no restart." The map files are replaced by atomic rename. Not said: file maps are polled (map_watch_interval), so a change takes effect with a delay, not at once.
- Check: rspamd docs configuration/maps.html; configuration/options.html `map_watch_interval`.
- Rating: **GUESSED**. e2e: no rspamd.

**R2. lists.go:158-162, 174.** The map file syntax: a newline ends an entry, whitespace starts the entry's value, and `#` starts a comment, so a comment header line is allowed.
- Check: rspamd docs configuration/maps.html "Map formats".
- Rating: **GUESSED**.

**R3. lists.go:248-274.** An `ip` map takes bare v4/v6 addresses and CIDR networks (radix).
- Check: docs modules/multimap.html `type = "ip"`.
- Rating: **GUESSED**.

**R4. multimap.conf:225, 239-275; lists.go:56-58, 384-392.** `type = "from"` matches the sender address whole and lower-cased; `filter = "email:domain"` matches the exact domain, not subdomains. Not said: rspamd's `from` is the envelope sender with a fallback to MIME From when the envelope is empty, while the journal's From is the envelope only.
- Check: docs modules/multimap.html (types and filters); src/plugins/lua/multimap.lua (3.11).
- Rating: **GUESSED**.

**R5. multimap.conf:219-223.** "Deny wins": `prefilter = true` with `action = "reject"` sets a pre-result that an allow score of -100 cannot override.
- Check: multimap.lua (`task:set_pre_result`); docs "prefilter".
- Rating: **GUESSED**.

**R6. multimap.conf:227-229; lists.go:295-303.** The deny `message` reaches Postfix's `milter-reject` reply in mail.log verbatim, and the allow symbols appear as `SELFPOST_ALLOW_X(` in X-Spamd-Result.
- Check: rspamd src/rspamd_proxy.c (milter reply from the pre-result message); milter_headers.lua x-spamd-result.
- Rating: **GUESSED**. e2e: no.

**R7. milter_headers.conf:170-187.** The routines `x-spamd-result` and `x-rspamd-action` exist and remove headers of the same name that the sender wrote before adding their own.
- Check: docs modules/milter_headers.html; src/plugins/lua/milter_headers.lua.
- Rating: **GUESSED**.

**R8. milter_headers.conf:189-211.** The custom routine signature `function(task, common_meta)` returns `(err, add, remove, metric)`, and `remove = { ['X-SelfPost-Filter'] = 0 }` removes all instances.
- Check: milter_headers.lua (3.11) custom routines; docs "Custom routines".
- Rating: **GUESSED**.

**R9. milter_headers.conf:207-208 vs internal/milter/inbound.go:110-119.** `rspamd_cryptobox_hash.create_specific_keyed(key, 'sha256', qid)` is HMAC-SHA256 keyed by the hex text.
- Justified by: inbound_test.go:344-345 "What rspamd 3.11 wrote for this key and queue id in a throwaway container (dev trial, 2026-10-12)".
- Rating: **MEASURED** (dated; the conf file itself does not point to it).

**R10. milter_headers.conf:203.** `task:get_queue_id()` returns the Postfix queue id that the milter sees as `{i}` at end-of-data.
- Check: rspamd src/libserver/milter.c (macro "i"); Postfix mail_params.h:3496.
- Rating: **GUESSED**. The R9 trial measured the HMAC for a given id, not where rspamd gets the id from.

**R11. milter_headers.conf:193-202; lists.go:166-168, 421-424.** rspamd's worker user can read the 0644 files in the 0755 directory mounted read-only.
- Rating: **GUESSED**. e2e: modes are checked (port25_features_test.go:268-288); no rspamd on the stand.

**R12. antispam compose:152, 158.** The `rspamd/rspamd:3.11` image runs a proxy worker with milter mode on 11332 by default. "3.11" is a series tag that moves across 3.11.x, despite "pin ... the same way the SelfPost image is pinned".
- Check: rspamd docker docs; conf/worker-proxy.inc.
- Rating: **GUESSED**.

**R13. internal/milter/inbound.go:199-203.** The formats `default: False [7.20 / 15.00]` and `SYM(5.10)[...]`. Fixture inbound_test.go:37 says "A header of the kind the documented rspamd sidecar adds": composed, but it does not say so.
- Check: milter_headers.lua `x-spamd-result`.
- Rating: **GUESSED**.

**R14. inbound.go:232-235; inbound_test.go:236.** "rspamd writes symbols heaviest first, which puts an allow entry's -100 last." Doubtful; the code keeps the SELFPOST_ symbols wherever they appear, so it does not depend on the order.
- Check: the sort in milter_headers.lua `x-spamd-result`.
- Rating: **GUESSED**.

**R15. internal/inbound/lists_test.go:321, 335-339.** "v4 (as Postfix may log it, mapped)" `::ffff:198.51.100.77`, and the fixtures "5.7.1 Spam message rejected" and "550 5.1.1 ...". Composed, but not said; whether Postfix logs v4-mapped addresses with `inet_protocols=all` is not established.
- Rating: **GUESSED**.

**R16. inbound.go:46-49; negative_test.go:55-61.** After a non-continue reply, go-milter gives a fresh session and Postfix does not send connect again, so the client address must come from `{client_addr}` at MAIL.
- Justified by: "found by the technical review of 2026-10-12"; asserted by mailTwiceOnOneConnection.
- Rating: **MEASURED** (dated).

**R17. inbound.go:271-276.** With `Protocol: OptNoBody`, end-of-message (Body) is still called; with `Actions: 0` nothing is modified.
- Check: github.com/emersion/go-milter (version in go.mod), server.go/session.go.
- Rating: **GUESSED**.

### 9. Postfix pipe(8) and the dmarc-ingest command (cmd/panel)

**M1. cmd/panel/dmarc_ingest.go:73-77.** pipe(8) hands over `${recipient}` as an argument and "sets no RECIPIENT variable, that is local(8)". With `flags=q` the address may come quoted, and `TaggedDomain` does not unquote it (harmless for domain tags).
- Check: man/man8/pipe.8 (`q` flag, macros); src/pipe/pipe.c.
- Rating: **GUESSED**.

**M2. cmd/panel/main.go:35-37.** Exit 75 (EX_TEMPFAIL) makes pipe(8) defer.
- Check: man/man8/pipe.8 DIAGNOSTICS; src/global/sys_exits.c.
- Rating: **GUESSED**.

**M3. dmarc_ingest.go:46-51.** "Postfix puts the start of what a delivery command wrote - about a hundred bytes - into its own line." postlog cannot be used because pipe(8) starts the command without the group allowed to write to the log socket.
- Check: src/pipe/pipe.c (command output into the DSN text); src/global/pipe_command.c; src/util/set_ugid.c:54 `setgroups(1, &gid)`: primary group only (grep: consistent for the group half).
- Rating: **GUESSED**. e2e: port25_features_test.go:166 asserts the `status=sent (delivered via dmarc-ingest service (dropped: ...` form, but not the length.

### 10. DMARC aggregate reports (RFC 7489) and reporters

**M4. internal/dmarc/ingest.go:110-160.** The report travels as an attachment (or the whole body) whose file name or Content-Type contains xml, gzip or zip, in base64 or quoted-printable.
- Check: RFC 7489 §7.2.1.1 (gzip and the file-name convention).
- Rating: **GUESSED**.

**M5. parse.go:171-173.** "Both are seen in the wild: most senders gzip the XML, some (notably several Microsoft and Yahoo endpoints) ship it as a single-entry zip."
- Justified by: claims a sighting, with no place or date.
- Rating: **MEASURED** (undated, unsourced).

**M6. parse.go:43-76.** The XML element names (feedback, report_metadata, policy_published, record/row/policy_evaluated, identifiers) are unnamespaced; Go's matching by local name would also accept a namespaced (DMARCbis) report.
- Check: RFC 7489 Appendix C.
- Rating: **GUESSED**.

**M7. parse.go:134-136, 163-165.** A missing pct means 100. A record passes when the evaluated SPF or DKIM is pass, those being the aligned results.
- Check: RFC 7489 §6.3 (pct default), Appendix C (policy_evaluated).
- Rating: **GUESSED**.

**M8. parse.go:78-87.** "A real report has tens of records, a few thousand from the largest receivers."
- Rating: **GUESSED**.

**M9. Fixtures: dmarc/parse_test.go:14 (`sampleXML`, org "google.com"); test/e2e/port25_features_test.go:60 "as a receiver sends it", :90 "one gzip attachment, base64 in 76-column lines".** All composed by hand, and none says so (the rule requires it).
- Rating: **GUESSED**.

### 11. SQLite (modernc.org/sqlite) and what a running instance tolerates during backup

**Q1. internal/backup/backup.go:380-409.** `VACUUM INTO` yields a consistent snapshot while the panel writes under WAL, and `_pragma=busy_timeout(5000)` in the DSN sets the busy timeout.
- Check: sqlite.org/lang_vacuum.html §"VACUUM INTO"; modernc.org/sqlite docs (`_pragma` DSN parameter).
- Rating: **GUESSED**.

**Q2. backup.go:85-95.** Postfix moves messages between queue directories, so a copy can hold a message twice or not at all, and queue files are readable by Postfix's own user only ("which is why the panel's backup failed outright from 1.3.0 to 2.0.0").
- Check: conf/postfix-files:55 (`active ... 700`, likewise for the other queues).
- Rating: **MEASURED** (incident, undated).

**Q3. backup.go:161 (addTree over /data).** Files that may be rewritten during the copy (texthash maps, OpenDKIM tables, antispam maps, filter.key) are consistent because their writers rename atomically. sasldb2 is not written that way (S3).
- Rating: **GUESSED** (unstated).

**Q4. backup.go:271-276, 268.** Symlinks are skipped silently. A `certs/` made of certbot's `live/` symlinks would be left out of the archive with no warning.
- Rating: **GUESSED** (unstated; see A1).

### 12. ACME clients and reverse proxies

**A1. deploy/apache/selfpost-vhost.conf:94-103; deploy/nginx/docker-compose.nginx.yml:32-35.** Bind-mounting certbot's `/etc/letsencrypt/live/<domain>` (or `./nginx/certbot-etc/live/mail.example.com`) gives Postfix the PEM files. In certbot's layout, `live/<domain>/*.pem` are relative symlinks to `../../archive/<domain>/*N.pem`. With only `live/<domain>` mounted they dangle inside the container: postfix-config.sh's `-f` test fails and Postfix gets no certificate.
- Check: certbot user guide §"Where are my certificates?".
- Rating: **WRONG?** (the base compose with `./certs` works when the operator copies the files).

**A2. nginx compose:55-57.** "Renewal twice a day is certbot's own recommended cadence."
- Check: certbot docs §"Automated Renewals".
- Rating: **GUESSED**.

**A3. apache vhost:94-97, 108-111.** `certbot --apache` edits this vhost in place to add the SSL directives and a redirect.
- Check: certbot apache plugin docs.
- Rating: **GUESSED**.

**A4. deploy/caddy/docker-compose.caddy.yml:10-16, 31-35, 41.** Caddy stores certificates at `certificates/acme-v02.api.letsencrypt.org-directory/<host>/<host>.crt|.key`.
- Justified by: honestly declared "not a documented public API — VERIFY". Not said: Caddy falls back to ZeroSSL, which stores under another issuer directory.
- Rating: **GUESSED** (declared).

**A5. deploy/traefik/extract-cert.sh:125-133.** acme.json is `.le.Certificates[] | .domain.main, .certificate, .key`, with base64 PEM and the resolver name "le" hard-coded.
- Check: Traefik v3.1 docs, ACME storage.
- Rating: **GUESSED**.

**A6. traefik compose:79-82.** The docker provider reads routing labels from the routed container.
- Check: Traefik docs, Docker provider.
- Rating: **GUESSED**.

**A7. nginx.conf.example:77 `http2 on;`.** Needs nginx ≥ 1.25.1 (the image is 1.27). The `caddy:2` and `certbot/certbot:latest` tags float.
- Rating: **GUESSED**.

### 13. OpenSSL CLI (OpenSSL 3.0, bookworm)

**X1. postfix-cert-reload.sh:38-44.** `openssl x509 -noout -pubkey` and `openssl pkey -pubout` print byte-identical SubjectPublicKeyInfo PEM for a matching pair.
- Check: openssl-x509(1), openssl-pkey(1).
- Rating: **GUESSED**. e2e: renewed_certificate_picked_up (matching pair only).

### 14. e2e harness: CoreDNS, smtp-sink, and the replies and log lines it asserts

**E1. test/e2e/sink/Dockerfile:105-130.** smtp-sink with `-d` writes one file per transaction with a random suffix, and it refuses to run as root without `-u`.
- Check: postfix-3.7.11 man/man1/smtp-sink.1 (-d, -u).
- Rating: **GUESSED**.

**E2. test/e2e/dns/Corefile:131-139; dns_zone.go:39-42.** "The file plugin's own reload ... polls db.zone's mtime/serial", and dns_zone.go says "CoreDNS's `reload 2s` ... picks up the new mtime". The Corefile has `reload 1s`. CoreDNS's file plugin reloads when the SOA serial changes. writeZone sets the serial to `time.Now().Unix()`, so two writes in the same second keep the serial, and the second is not served (publishTXT then fails after 20 s).
- Check: coredns.io/plugins/file §reload.
- Rating: **WRONG?** (stale interval; the mechanism is described as mtime).

**E3. dns_zone.go:49-51.** An implicit MX falls back to the A record "(RFC 5321)".
- Check: RFC 5321 §5.1.
- Rating: **CITED** (no section).

**E4. dns_zone.go:59-62.** TXT strings are limited to 255 bytes and concatenated by the resolver.
- Check: RFC 1035 §3.3; RFC 6376 §3.6.2.2.
- Rating: **GUESSED**.

**E5. port25_config_check.go:260-273.** postconf rewraps the whole master.cf at a fixed column when writing and can split a value, citing "smtpd_recipient_restrictions's two space-separated tokens". That example is stale: values are comma-joined now.
- Check: src/postconf/postconf_master.c / postconf_edit.c (line folding on write).
- Rating: **GUESSED**.

**E6. port25_config_check.go:116-119.** "Postfix is not reloaded, so the running master keeps the config it started with." True for master.cf, but each newly spawned daemon reads main.cf, which for the length of this step holds the port-25 combinations (relay_domains, export_environment and the rest).
- Check: postconf(5) introduction ("changes ... picked up automatically, as Postfix processes are restarted on a regular basis").
- Rating: **WRONG?** (partly; harmless, no mail at that point).

**E7. port25_config_check.go:144-148.** A value with a space is cut and "port 25 dead while `postfix check` stays silent".
- Justified by: the 1.9.1 history, undated.
- Rating: **MEASURED** (undated).

**E8. process_check.go:263-272.** `postconf -M smtp/inet` prints nothing, or a warning or fatal, when the service is absent.
- Rating: **GUESSED**.

**E9. negative_test.go:50, 92-94, 110, 197.** Reply texts: milter tempfail at MAIL is `451 4.7.1 Service unavailable`; the mismatch is `553 ... Sender address rejected: not owned by user` at RCPT; no AUTH is `554 ... Client host rejected: Access denied`; anvil is `450 4.7.1 Error: too much mail from`.
- Check: src/milter/milter8.c; src/smtpd/smtpd_check.c; src/smtpd/smtpd.c (anvil).
- Rating: **GUESSED**. Each is asserted on every run.

**E10. sender_check_test.go:141-143.** "Postfix answers 451 from the connection on, and '454 4.3.0 Try again later' at AUTH." Only 4xx is asserted.
- Rating: **GUESSED** (looks observed, but is not said to be).

**E11. sender_check_test.go:219-225.** "Past the DATA command Postfix's cleanup, not smtpd, talks to the milters."
- Justified by: audit 2026-10-12, M13; asserted.
- Rating: **MEASURED** (dated). Check: src/cleanup/cleanup_milter.c.

**E12. sender_check_test.go:53-55.** A folded header reaches the milter with both of its lines.
- Rating: **GUESSED** (asserted).

**E13. smtp_client.go:231-238.** An smtpd that was idle from before the reload answers 553 for about a second ("seen on the stand; two seconds after the save it no longer happened ...").
- Rating: **MEASURED** (undated).

**E14. negative_test.go:179-184.** The level-1 case once passed on a reset during startup, and its first message had always been refused for another reason.
- Rating: **MEASURED** (incident, undated).

**E15. mail_helpers.go:67-72.** smtpd logs `connect from host[ADDR]`.
- Check: src/smtpd/smtpd.c.
- Rating: **GUESSED** (asserted).

**E16. logrotate_check.go:27-55; restore_test.go:78-80; cert_reload_check.go:148-153.** These rest on L5, P34 and P38 respectively and assert them. No additional claims.

---

### Tally

139 rated entries (E16 is a pointer, not rated).

| Rating | Count | Entries |
|---|---|---|
| CITED | 2 | P7, E3 (both weak: the document is named, the section is not) |
| MEASURED | 11 | P23, L4, K10, Q2, M5, E7, E13, E14 (undated); R9, R16, E11 (dated) |
| GUESSED | 114 | everything else; about 25 of them are marked "grep: consistent" against dev/upstream |
| WRONG? | 12 | P4, P31, P34, P42, V5, V6, V10, K8, K9, A1, E2, E6 |

## Part D - internal/store, internal/web, and the claims of architecture.md, security.md, guide.md

Made 2026-10-14 against `0f984ff` (main), read-only. Rule: docs/development.md
§ "Code that relies on another component's behaviour" (lines 218-240).

Ratings say what justifies the claim **today, in the repository**:

- **CITED** - a comment/doc names the component's source or documentation for the version in use.
- **MEASURED** - a comment/doc states a measurement (with a date or "on the stand").
- **GUESSED** - neither. Where this inventory happened to confirm or doubt it with a quick grep, that is said under "Inventory note"; the rating stays GUESSED, because the code still carries no citation.
- **WRONG?** - there is a concrete reason to think the claim (or the code built on it) is false.

"Test-covered" means a test in the repository runs the real driver/browser-less code path and would fail if the behaviour differed - evidence, but not a citation under the rule.

Versions used for "Check at":

- SQLite **3.53.4**, as bundled by modernc.org/sqlite **v1.60.1** (`$GOMODCACHE/modernc.org/sqlite@v1.60.1/lib/sqlite.go:4556 SQLITE_VERSION`).
- htmx **2.0.4** (`internal/web/view/static/htmx.min.js`, `version:"2.0.4"`; docs/development.md:193). Docs at https://v2.htmx.org (2.x line), source tag v2.0.4 `src/htmx.js`.
- Bulma 1.0.4, Tabler icons 3.49.0 (docs/development.md:194-197).
- Postfix 3.7.11, Cyrus SASL 2.1.28 - sources under `dev/upstream/`.
- Go html/template: the Go toolchain of go.mod (1.26+, docs/development.md:248).

---

### 1. SQLite / modernc.org/sqlite (internal/store/**)

#### 1.1 Connection set-up and pool

**S1. internal/store/store.go:30-32** - DSN `_pragma=` parameters (`journal_mode(WAL)`, `busy_timeout(5000)`, `foreign_keys(ON)`) are applied by the driver on every new pooled connection, so FK enforcement and the busy timeout (both per-connection) hold on every connection.
- Component: modernc.org/sqlite v1.60.1; SQLite 3.53.4 (foreign_keys and busy_timeout are per-connection, journal_mode=WAL is persistent in the file).
- Justification now: comment "_pragma parameters are applied on every pooled connection by the driver, so foreign-key enforcement and WAL survive connection churn." No source named.
- Check at: modernc `driver.go:94-153` (DSN documentation: "_pragma: Each value is run with the PRAGMA keyword prepended"), `sqlite.go:432-470` (apply phase, called from `newConn`); sqlite.org/pragma.html#pragma_foreign_keys, #pragma_busy_timeout, #pragma_journal_mode.
- Rating: **GUESSED**.
- Inventory note: confirmed by grep - modernc applies the `_pragma` list on each new connection and sorts `busy_timeout` first (`sqlite.go:452-462`, "Push 'busy_timeout' first, the rest in lexicographic order"), so the WAL pragma does not run before the timeout is set. The citation is ready to be written.

**S2. internal/store/store.go:37-39** - "modernc's driver serializes writes anyway; a small pool avoids 'database is locked' surprises under WAL."
- Component: modernc.org/sqlite v1.60.1; SQLite 3.53.4 locking.
- Justification now: the comment; nothing cited.
- Check at: modernc `conn.go`/`stmt.go` (no write mutex across connections); sqlite.org/wal.html §2.2 "Concurrency" (one writer at a time; other writers get SQLITE_BUSY or wait out busy_timeout), sqlite.org/lockingv3.html.
- Rating: **WRONG?** - the driver does not serialise writes across connections; SQLite's write lock does, and a second connection waits through busy_timeout or gets SQLITE_BUSY. In this process it is `SetMaxOpenConns(1)` itself, that is, database/sql's pool, that serialises everything. The conclusion holds, but the premise it gives does not. Also: with one connection, every reader in the panel waits behind any open transaction (see S24).

**S3. internal/store/store.go:62-64, 77-125** - `PRAGMA user_version` decides which migrations run, "so each migration runs at most once".
- Component: SQLite 3.53.4 (deferred transactions, user_version in the file header); the panel and the `dmarc-ingest` process both call `store.Open` (cmd/panel/main.go:290, cmd/panel/dmarc_ingest.go:24).
- Justification now: comment only.
- Check at: sqlite.org/pragma.html#pragma_user_version; sqlite.org/lang_transaction.html ("BEGIN DEFERRED ... no transaction is actually started until the database is first accessed").
- Rating: **WRONG?** - the version is read outside the transaction (store.go:78) and the transaction is a deferred `BEGIN` (modernc `tx.go:22-25`: plain "begin" unless `_txlock` is set). Two processes opening the file at the same moment after an upgrade (panel start and a DMARC report piped to `dmarc-ingest`) can both read N and both run migration N+1. The second waits for the write lock and then fails ("duplicate column name" / "table ... already exists"). Impact is small (one failed ingest that Postfix retries, or one failed panel start that supervisord restarts), but "at most once" is not what the code guarantees. Fix: re-read user_version inside the transaction after a first write, or use `BEGIN IMMEDIATE` for migrations.

**S4. internal/store/store.go:104-115** - a whole migration file (several statements) is executed by a single `tx.Exec`, and every statement in it runs, not just the first.
- Component: modernc.org/sqlite v1.60.1.
- Justification now: none.
- Check at: modernc `stmt.go:189-190` ("FALLBACK PATH: Multi-statement script", the loop over `prepareV2(&psql)` tails).
- Rating: **GUESSED** (test-covered: schema_test.go TestBaselineSchema / TestOpenMigratesA20Database would fail otherwise).
- Inventory note: confirmed by grep.

**S5. internal/store/store.go:116-121; migrations/0001_init.sql:13** - `PRAGMA user_version = N` and `PRAGMA application_id = ...` run inside the migration's transaction and are rolled back with it, and a PRAGMA cannot take a bound parameter.
- Component: SQLite 3.53.4.
- Justification now: comment "PRAGMA does not accept a bound parameter, and target is a trusted loop index". Transactionality is not mentioned.
- Check at: sqlite.org/pragma.html (header: "PRAGMA ... value" syntax; #pragma_user_version, #pragma_application_id: both are integers at fixed offsets of the database header, written under the write transaction).
- Rating: **GUESSED**.

**S6. internal/store/store.go:54-56, 82-94** - a database without the SP20 mark reads `application_id` 0, so a 1.x file (user_version > 0, no mark) is recognised and refused.
- Component: SQLite 3.53.4.
- Justification now: comment; docs/schema-migrations.md.
- Check at: sqlite.org/pragma.html#pragma_application_id; sqlite.org/fileformat2.html §1.3 (offset 68, default 0).
- Rating: **GUESSED** (test-covered: schema_test.go TestOpenRefusesA1xDatabase).

**S7. internal/store/store.go:32 (busy_timeout 5000)** - five seconds is long enough for any write of another connection (the dmarc-ingest process, the backup tool) to finish.
- Component: SQLite 3.53.4 busy handler.
- Justification now: none. There is no statement of how long the longest write holds the lock (DeleteSendLogBefore over a retention window, PruneSendLogLines, migration 0008's UPDATE of all of send_log).
- Check at: sqlite.org/c3ref/busy_timeout.html; a measurement on the stand of the longest write transaction.
- Rating: **GUESSED**.

#### 1.2 Transactions and locking

**S8. internal/store/users.go:83-99 (CreateGlobalUser)** - `INSERT ... SELECT ... WHERE NOT EXISTS (SELECT 1 FROM users)` is one statement, so two setup requests at once cannot both insert.
- The same pattern: sendloglines.go:56-60 (`once`), inboundspam.go:76-93 (InsertInboundSpamOnce), sendlog.go:283-293 (RecoverRefusal), sendlog.go:251-259 (AttachRefusalLine's `NOT EXISTS`).
- Component: SQLite 3.53.4 (a write statement starts its write transaction before reading: the VDBE `OP_Transaction` with the write flag at the start of the program).
- Justification now: comment "The check that no user exists yet and the insert are one statement, so two setup requests at once cannot both create one (audit 2026-10-12, M7)." Not cited.
- Check at: sqlite.org/lang_transaction.html ("An implicit transaction ... is started automatically"; read vs write transactions); sqlite.org/opcode.html#Transaction.
- Rating: **GUESSED**. In this process `SetMaxOpenConns(1)` serialises the two requests anyway; the SQLite property only matters for a second process.

**S9. internal/store/users.go:299-323 (SaveUser), 404-427 (DeleteUser)** - "A write first: it takes the write lock, so what is counted below is what is committed": the first UPDATE/DELETE of a deferred transaction takes the write lock, no other connection can commit until this transaction ends, and the count inside it sees its own write.
- Component: SQLite 3.53.4 (WAL write lock); modernc v1.60.1 `Begin` = deferred `BEGIN` (`tx.go:22-25`).
- Justification now: the comment; not cited. Test users_tx_test.go TestTheLastGlobalSurvivesTwoAtOnce runs both goroutines through the single pooled connection (`SetMaxOpenConns(1)`), so it exercises database/sql's pool, not SQLite's lock.
- Check at: sqlite.org/lang_transaction.html §"Read transactions versus write transactions"; sqlite.org/wal.html §2.2.
- Rating: **GUESSED**.

**S10. internal/store/sendlog.go:107-111 (UpdateStatus)** - "A transaction that reads first and then writes fails at once with 'database is locked' when another connection - the dmarc-ingest process opens the same file - commits in between ...; a write first takes the write lock, and the busy timeout waits for it."
- Component: SQLite 3.53.4 in WAL mode (SQLITE_BUSY_SNAPSHOT: a read transaction whose snapshot is stale cannot be upgraded, and the busy handler is not invoked for it).
- Justification now: the comment; not cited, not measured.
- Check at: sqlite.org/lang_transaction.html (the upgrade paragraph: "If some other database connection has already modified the database ... the upgrade attempt fails with SQLITE_BUSY"); sqlite.org/rescode.html#busy_snapshot; sqlite.org/c3ref/busy_handler.html (handler not invoked when it could deadlock).
- Rating: **GUESSED** (the substance matches SQLite's documentation as I know it; the comment has to name it).

**S11. All other multi-statement transactions begin with a write, without saying why** - applications.go:77-101 (INSERT first), 113-139 (UPDATE first); inbound.go:51-79, 192-225; users.go:252-280, 505-515 / 521-552 (writeUserReach starts with UPDATE); dmarc.go:85-131 (DELETE first); sendercheck.go:103-139 (INSERT first); sendloglines.go:62-84 (`tx.Prepare` and then INSERTs; preparing does not open a read transaction); store.go:108-124.
- Component: as S9/S10.
- Justification now: the rule is stated only at users.go:304 and sendlog.go:107-111.
- Check at: as S10.
- Rating: **GUESSED** (consistent today, but a future read-first transaction would break it silently. A one-line reference to S10's comment, or `_txlock=immediate` in the DSN, would make it structural).

**S12. internal/store/sendloglines.go:42-46** - "a commit per line would be a disk sync per line".
- Component: SQLite 3.53.4, WAL with the default `synchronous=FULL` (modernc sets nothing else).
- Justification now: none.
- Check at: sqlite.org/pragma.html#pragma_synchronous ("FULL ... WAL mode: the WAL file is synchronized before each transaction commit").
- Rating: **GUESSED**.

**S13. internal/store/sessions.go:31-33** - LookupSession does not delete expired rows itself, "keeping the read side lock-free".
- Component: SQLite 3.53.4 WAL (readers do not block, and are not blocked by, the writer).
- Justification now: none.
- Check at: sqlite.org/wal.html §2.2.
- Rating: **GUESSED**. True of SQLite, but with `SetMaxOpenConns(1)` the read still queues behind any transaction open on the panel's only connection.

#### 1.3 Foreign keys, cascades, ALTER TABLE

**S14. ON DELETE CASCADE / SET NULL** - domains.go:129-131 (applications, addresses go with the domain), inbound.go:228-229, applications.go:356-358, dmarc.go:91-96 (deleting a prior report deletes its records), migrations/0001_init.sql:48-53 (deleting a user ends their sessions), 0008:27,38 (`ON DELETE SET NULL` keeps log rows).
- Component: SQLite 3.53.4 foreign keys, which act only on connections with `foreign_keys=ON` (S1).
- Justification now: none beyond naming the clause.
- Check at: sqlite.org/foreignkeys.html §2 (enabling, per connection), §4.3 (ON DELETE actions).
- Rating: **GUESSED** (test-covered: domains_test.go TestDeleteDomainCascadesApplications).

**S15. internal/store/users.go:534-549** - inserting an assignment for a domain that is gone fails at that statement with a foreign-key error (constraints are immediate), which becomes ErrNoSuchDomain.
- Component: SQLite 3.53.4.
- Justification now: ErrNoSuchDomain's doc comment (users.go:19-21) describes the case; the mechanism is not cited.
- Check at: sqlite.org/foreignkeys.html §4.2 (immediate vs deferred constraints).
- Rating: **GUESSED**.

**S16. migrations/0008_tenant_boundary.sql:27, 38** - `ALTER TABLE ... ADD COLUMN ... REFERENCES ... ON DELETE SET NULL` is allowed inside a migration run with `foreign_keys=ON` because the new column's default is NULL.
- Component: SQLite 3.53.4.
- Justification now: none (not commented).
- Check at: sqlite.org/lang_altertable.html §"ALTER TABLE ADD COLUMN" ("If foreign key constraints are enabled, a column with a REFERENCES clause must have a default value of NULL").
- Rating: **GUESSED** (test-covered: the migration runs in TestOpenMigratesA20Database).

**S17. migrations/0008_tenant_boundary.sql:8-10** - "SQLite cannot change a column's collation without rebuilding the table".
- Component: SQLite 3.53.4.
- Justification now: the sentence, uncited.
- Check at: sqlite.org/lang_altertable.html §7 "Making Other Kinds Of Table Schema Changes".
- Rating: **GUESSED** (correct as far as I know).

#### 1.4 Collation, comparison, text functions

**S18. migrations/0008_tenant_boundary.sql:3-6** (also internal/app/validate.go:28-33, docs/development.md:230-231) - "Postfix compares the SASL login with the sender map's without regard to case".
- Component: Postfix 3.7.11 smtpd.
- Justification now: none in the migration. docs/development.md:230-231 uses this as its model citation: "Postfix 3.7.11, src/smtpd/smtpd_check.c, check_sender_login(): the SASL login is compared with the sender map's owners by strcasecmp."
- Check at: `dev/upstream/postfix-3.7.11/src/smtpd/smtpd_check.c:3913` `reject_auth_sender_login_mismatch()`, line 3936 `strcasecmp_utf8(state->sasl_username, name)`.
- Rating: **GUESSED** for the migration. **WRONG?** for development.md's example: in 3.7.11 the function is `reject_auth_sender_login_mismatch()` and the comparison is `strcasecmp_utf8`, not `check_sender_login()` and `strcasecmp`. Harmless for the code, because logins are ASCII-only (internal/app/validate.go:46-58), so NOCASE (ASCII) and `strcasecmp_utf8` agree. But the template every future citation is copied from names the wrong function.

**S19. COLLATE NOCASE, ASCII-only** - migrations/0008:11 (`UNIQUE INDEX ... (login COLLATE NOCASE)`), domains.go:204 (`name = ? COLLATE NOCASE`), sendlog.go:121 (`to_addr = ? COLLATE NOCASE`), sendlog.go:255 (`from_addr = ?5 COLLATE NOCASE`).
- Component: SQLite 3.53.4 built-in NOCASE (folds ASCII A-Z only).
- Justification now: none anywhere.
- Check at: sqlite.org/datatype3.html §7.1 "Collating Sequences" ("NOCASE ... only works for ASCII characters").
- Rating: **GUESSED**. Equivalent to a full case fold for logins and domain names (validated ASCII). For `to_addr`/`from_addr` an SMTPUTF8 address whose case differs outside ASCII would not match; whether Postfix ever changes it is S20.

**S20. internal/store/sendlog.go:91-92** - "Postfix may normalise address case between the milter (envelope) and the delivery log", which is why the recipient is matched with NOCASE.
- Component: Postfix 3.7.11 (smtpd milter macros `{rcpt_addr}` vs the `to=<...>` of smtp/lmtp/qmgr delivery lines; cleanup/trivial-rewrite).
- Justification now: none ("may").
- Check at: `dev/upstream/postfix-3.7.11/src/cleanup/cleanup_addr.c` (recipient rewriting), `src/global/log_adhoc.c` (`to=<%s>`), `src/smtpd/smtpd_milter.c` (`rcpt_addr` macro).
- Rating: **GUESSED** (a hedge in place of an answer the source can give).

**S21. internal/store/sendlog.go:50, stats.go:88, notify.go:189** - `applications.login = ?` uses BINARY comparison although logins are unique without regard to case. This assumes the SASL login reaches the milter (`{auth_authen}`) and the log in the case it is stored in. That holds only if Cyrus SASL/sasldb authenticates the login case-sensitively, so that "OPS" cannot log in as "ops".
- Component: Cyrus SASL 2.1.28 (sasldb auxprop, canonuser), Postfix 3.7.11 `{auth_authen}`.
- Justification now: none (not commented).
- Check at: `dev/upstream/cyrus-sasl2-2.1.28+dfsg/lib/canonusr.c` (internal canon_user), `plugins/sasldb.c`, `sasldb/db_berkeley.c` (key built from user\0realm\0prop as given).
- Rating: **GUESSED**.

**S22. RFC3339 text compared as time** - stated at sendlog.go:186-187, 435-436; ratelimits.go:188-189; deliverylog.go:73; inboundspam.go:226-228; migrations/0008:25-26. Relied on without comment at sessions.go:103; dmarc.go:140-141, 154, 258, 272, 311; sendloglines.go:112, 150; notify.go:190; stats.go:98, 109; sendlog.go:256, 289 (BETWEEN).
- Component: SQLite 3.53.4 (TEXT compared with BINARY collation = memcmp); Go `time.RFC3339` on a UTC time gives fixed-width `YYYY-MM-DDTHH:MM:SSZ` (stdlib; sendercheck.go:119-122 uses its own fixed-width fractional format and compares it only with itself, as its comment says).
- Justification now: the comments assert it; nothing cited.
- Check at: sqlite.org/datatype3.html §4.1-4.2 (comparison of TEXT values) and §7 (BINARY collation: memcmp).
- Rating: **GUESSED** (correct as long as every writer formats UTC with `time.RFC3339`, which all store writers do).

**S23. internal/store/sendlog.go:257** - `julianday(created_at)` parses `YYYY-MM-DDTHH:MM:SSZ` (the `T` separator and the `Z` suffix).
- Component: SQLite 3.53.4 date functions.
- Justification now: none.
- Check at: sqlite.org/lang_datefunc.html §2 "Time Values" (formats 5-7 accept `T`; a timezone suffix "[+-]HH:MM or just Z").
- Rating: **GUESSED** (refusal_test.go covers the nearest-in-time choice; if julianday returned NULL the ORDER BY would fall back to id silently, so a test that distinguishes the two is what proves it).

**S24. internal/store/stats.go:110** - `GROUP BY substr(created_at, 1, 13)` buckets rows by UTC hour: SQLite's substr is 1-based and counts characters.
- Component: SQLite 3.53.4.
- Justification now: none.
- Check at: sqlite.org/lang_corefunc.html#substr.
- Rating: **GUESSED**.

**S25. internal/store/notify.go:140-142** - `substr(key, 1, ?) = ?` with Go's `len(prefix)` (bytes) as the length, while SQLite's substr counts characters for TEXT. The two are the same only for ASCII prefixes; the callers pass "dns:" and "limit:" (internal/notify/notify.go:297, 490).
- Component: SQLite 3.53.4.
- Justification now: none.
- Check at: sqlite.org/lang_corefunc.html#substr ("characters, not bytes" for TEXT).
- Rating: **GUESSED** (correct for today's callers).

#### 1.5 Statements, parameters, results, errors

**S26. Upsert** - deliverylog.go:30-31, 62-63; sendlog.go:139-140; sendercheck.go:80-82, 113-114, 124-128 (`... DO UPDATE SET value = excluded.value WHERE excluded.value > settings.value`: a false WHERE leaves the row untouched and raises nothing); settings.go:162; logtail.go:48-53; notify.go:118-120; ratelimits.go:95-102.
- Component: SQLite 3.53.4 UPSERT (`excluded.`, WHERE on DO UPDATE; the INSERT...SELECT parsing ambiguity does not arise here, as all use VALUES).
- Justification now: none.
- Check at: sqlite.org/lang_upsert.html.
- Rating: **GUESSED** (test-covered for the mark that must not move back: sendercheck_test.go).

**S27. RowsAffected semantics** - users.go:96, 318, 417; applications.go:123, 368; inbound.go:179, 202, 235; domains.go:137, 188; notify.go:39; inboundspam.go:92, 337; sendlog.go:129, 149, 167, 268, 297. Assumed: an UPDATE counts every row its WHERE matched even if no value changed (SaveUser's "n == 0 means no such user"); an `INSERT ... SELECT ... WHERE NOT EXISTS` that inserts nothing reports 0; rows removed by an FK cascade are not counted.
- Component: SQLite 3.53.4 `sqlite3_changes`; modernc v1.60.1 `RowsAffected`.
- Justification now: none.
- Check at: sqlite.org/c3ref/changes.html ("Only changes made directly by the INSERT, UPDATE or DELETE statement are considered - auxiliary changes caused by triggers, foreign key actions or REPLACE constraint resolution are not counted").
- Rating: **GUESSED**.

**S28. LastInsertId** - applications.go:93, domains.go:52, users.go:268, inbound.go:67, dmarc.go:114, inboundspam.go:302 - the id of the row just inserted on this connection. No `RETURNING` is used anywhere in internal/store.
- Component: SQLite `sqlite3_last_insert_rowid` via modernc.
- Justification now: none.
- Check at: sqlite.org/c3ref/last_insert_rowid.html.
- Rating: **GUESSED** (ordinary use, test-covered everywhere).

**S29. internal/store/domains.go:164-179** - a constraint violation comes back as `*sqlite.Error` whose `Code()` is the extended result code (`SQLITE_CONSTRAINT_UNIQUE` 2067, `_PRIMARYKEY` 1555, `_FOREIGNKEY` 787). A violation of the NOCASE unique index (0008) is also `SQLITE_CONSTRAINT_UNIQUE`.
- Component: modernc.org/sqlite v1.60.1; SQLite 3.53.4 result codes.
- Justification now: none.
- Check at: modernc `conn.go:113` (`c.extendedResultCodes(true)` in `newConn`), `conn.go:833-839`, `error.go:21` (`func (e *Error) Code() int`); sqlite.org/rescode.html#constraint_unique, #constraint_foreignkey.
- Rating: **GUESSED** (test-covered: domains_test.go TestAddDomainDuplicate, user/app duplicate tests).
- Inventory note: confirmed by grep - modernc turns extended result codes on for every connection.

**S30. internal/store/stats.go:84-88; sendlog.go:251-263; sendloglines.go:56-60** - numbered parameters: "?1 twice; a plain ? after it takes the next number", and database/sql's positional arguments bind to `?NNN` by number.
- Component: SQLite 3.53.4 parameter numbering; modernc v1.60.1 binding.
- Justification now: the stats.go comment states the rule; not cited.
- Check at: sqlite.org/lang_expr.html §"Parameters" ("?" takes "a number one greater than the largest parameter number already assigned"); modernc `stmt.go` bind (`sqlite3_bind_parameter_name`/index).
- Rating: **GUESSED** (test-covered: sendlog_test.go TestALoginTakenAgainStartsWithNoNumbers).

**S31. internal/store/inboundspam.go:107-108** - `LIMIT -1 OFFSET ?` means "no upper bound, skip the newest keep rows".
- Component: SQLite 3.53.4.
- Justification now: none.
- Check at: sqlite.org/lang_select.html §"The LIMIT clause" ("If the LIMIT expression evaluates to a negative value, then there is no upper bound").
- Rating: **GUESSED** (test-covered: inboundspam_test.go prune test).

**S32. migrations/0005_send_log_rejected_index.sql:1-6; notify.go:183-184** - "The query names the status as a literal, which is what lets SQLite use [the partial index]"; a bound `status = ?` would not.
- Component: SQLite 3.53.4 query planner.
- Justification now: none. No EXPLAIN test; audit-2026-10-12.md stage 3 plans "EXPLAIN tests against the real query strings".
- Check at: sqlite.org/partialindex.html §2 "Queries Using Partial Indexes" (the query's WHERE must imply the index's WHERE; the rules for constants and bound parameters).
- Rating: **GUESSED**.

---

### 2. htmx 2.0.4 (internal/web/**)

**H1. internal/web/view/templates/layout.html:31-36** - unless `includeIndicatorStyles` is false, htmx injects its own `<style>` element into the head, which would need a CSP exception. `<meta name="htmx-config">` turns it off before that happens.
- Component: htmx 2.0.4.
- Justification now: template comment; no citation.
- Check at: https://v2.htmx.org/reference/#config (`htmx.config.includeIndicatorStyles`, the `htmx-config` meta tag); v2.0.4 `src/htmx.js` `insertIndicatorStyles()` / `mergeMetaConfig()`.
- Rating: **GUESSED**.
- Inventory note: confirmed in the vendored file - the ready handler runs `$n();_n();` (meta config first, then `if(Q.config.includeIndicatorStyles!==false){... insertAdjacentHTML("beforeend","<style"...`).

**H2. internal/web/view/static/panel.js:178-184** - `hx-trigger="every Ns [expr]"`: the filter is evaluated with `new Function`, which the CSP (no 'unsafe-eval') "would silently break".
- Component: htmx 2.0.4 (`maybeEval`, `htmx.config.allowEval`).
- Justification now: comment; no citation.
- Check at: https://v2.htmx.org/attributes/hx-trigger/ ("Trigger Filters"), https://v2.htmx.org/docs/#security (allowEval / CSP); `src/htmx.js` `maybeEval()`.
- Rating: **GUESSED**. Not entirely silent: maybeEval catches the EvalError and logs it to the console.

**H3. panel.js:172-176** - `htmx:load` fires for every element htmx puts into the page, with the element in `ev.detail.elt`.
- Component: htmx 2.0.4.
- Justification now: comment; no citation.
- Check at: https://v2.htmx.org/events/#htmx:load; `src/htmx.js` `makeAjaxLoadTask()`.
- Rating: **GUESSED**.

**H4. panel.js:235-247** - `htmx:afterSwap` fires on the new element (detail.elt), before htmx processes that element's `hx-trigger="load"`, so stripping `hx-trigger` in the handler prevents a second GET.
- Component: htmx 2.0.4 swap/settle order: `swap()` fires afterSwap on `settleInfo.elts`, and only the settle tasks (`makeAjaxLoadTask` -> `processNode`, `htmx:load`) process the new node.
- Justification now: comment ("Swapped-in markup still carries hx-trigger="load"; strip it so htmx does not issue a duplicate GET"); ordering not cited.
- Check at: v2.0.4 `src/htmx.js` `swap()`, `swapOuterHTML()`, `insertNodesBefore()`, `makeAjaxLoadTask()`, `doSettle()`.
- Rating: **GUESSED**.
- Inventory note: the vendored file shows afterSwap fired on the settle elements (`se(l.elts,function(e){...he(e,"htmx:afterSwap",o.eventInfo)})`) before the settle tasks run. It looks right, but deserves the citation, since it is the least obvious ordering in the panel.

**H5. panel.js:202-207** - the scheduled poll `htmx.ajax("GET", url, {target, swap})` without `source` belongs to the polled box, so its events reach the box.
- Component: htmx 2.0.4 `htmx.ajax` / `issueAjaxRequest`.
- Justification now: none.
- Check at: https://v2.htmx.org/api/#ajax; vendored file: `if(r==null){r=ne().body}` in `issueAjaxRequest`.
- Rating: **WRONG** - known: audit-2026-10-12.md:223-232 (M11), "the scheduled poll is `htmx.ajax(...)` without `source`, so htmx attributes it to `document.body`". The fix is scheduled in stage 4 (audit-2026-10-12.md:931-933). The default to `document.body` is visible in the vendored file.

**H6. panel.js:249-251** - `htmx:responseError` reaches the polled element, so a failed refresh is rescheduled; `htmx:sendError`/`htmx:timeout` are not handled.
- Component: htmx 2.0.4 events.
- Justification now: none.
- Check at: https://v2.htmx.org/events/#htmx:responseError, #htmx:sendError, #htmx:timeout.
- Rating: **WRONG** - known (M11): the event reaches `document.body`, which has no `data-poll`, so polling stops for good after one failure.

**H7. panel.js:253-257** - `htmx:beforeRequest`'s `ev.target` is the polled element, so a poll can be cancelled while the tab is hidden.
- Component: htmx 2.0.4.
- Justification now: none.
- Check at: as H5.
- Rating: **WRONG?** - same cause as M11 (the request's element is `document.body`), so this guard never matches. The audit names only responseError. Harmless, because `schedulePoll` already skips hidden tabs, but it is dead code built on the same false belief.

**H8. internal/web/auth/middleware.go:37-39** - every request htmx makes carries `HX-Request` (so a GET with it is a background poll and does not renew the session).
- Component: htmx 2.0.4 request headers.
- Justification now: none.
- Check at: https://v2.htmx.org/reference/#request_headers ("HX-Request: always true").
- Rating: **GUESSED**.

**H9. internal/web/auth/middleware.go:12-25** (with H8) - an htmx request whose session has expired is answered with `303 /login`. Implicitly, htmx is assumed to handle that sensibly.
- Component: htmx 2.0.4 + XMLHttpRequest (redirects are followed transparently; the sign-in page is swapped into the box).
- Justification now: none.
- Check at: https://v2.htmx.org/reference/#response_headers (`HX-Redirect`, `HX-Location`); XHR spec (redirects followed).
- Rating: **WRONG** - known: audit-2026-10-12.md:229-232 ("the XHR follows it and the sign-in document is swapped into the box - no `HX-Redirect`"). Stage 4.

**H10. internal/web/view/templates/health_body.html:1-7; overview_cards.html:1-6; out_log_rows.html:1-5; view/components.go:194-199, 284-286, 331-342** - `hx-swap-oob="true"` swaps the element with the same id by outerHTML. OOB elements are taken out of the polled fragment, nested ones included, and "a fragment can replace only the element that asked for it".
- Component: htmx 2.0.4 (`hx-swap-oob`, `htmx.config.allowNestedOobSwaps`).
- Justification now: template comments; no citation.
- Check at: https://v2.htmx.org/attributes/hx-swap-oob/; https://v2.htmx.org/reference/#config (allowNestedOobSwaps, default true); `src/htmx.js` `oobSwap()`, `findAndSwapOobElements()`.
- Rating: **GUESSED**.

**H11. components.html:87; health_body.html:15; out_log_rows.html:6** - `hx-trigger="load"` fires once, when htmx processes the element, giving the first fetch only (panel.js:179-180).
- Component: htmx 2.0.4.
- Justification now: panel.js comment; no citation.
- Check at: https://v2.htmx.org/attributes/hx-trigger/ ("load - triggered on load (useful for lazy-loading something)").
- Rating: **GUESSED**.

---

### 3. Browsers (internal/web/**)

**B1. internal/web/security.go:8-28** - CSP semantics: `default-src 'self'` covers scripts, styles, images, fonts and the XHR of htmx polling. `base-uri` and `form-action` do not fall back to default-src, so they are set explicitly. `object-src`/`frame-ancestors 'none'` stop embedding and framing. With no inline script, inline handler or `style=` (enforced by the guard test), no exceptions are needed.
- Component: browsers implementing CSP Level 3.
- Justification now: comment; no citation.
- Check at: https://www.w3.org/TR/CSP3/ §6.1 (fetch directives and default-src fallback), §6.3 (base-uri, form-action do not fall back), §6.4.2 frame-ancestors; MDN Content-Security-Policy.
- Rating: **GUESSED**.

**B2. security.go:52-54** - `X-Frame-Options: DENY` for browsers that predate `frame-ancestors`; "the two never disagree".
- Component: browsers.
- Justification now: comment.
- Check at: MDN X-Frame-Options; CSP3 §6.4.2 (frame-ancestors takes precedence where supported); RFC 7034.
- Rating: **GUESSED**.

**B3. security.go:55-58** - `Referrer-Policy: no-referrer`: "The panel's behaviour never depends on the referrer, so the strictest value is free".
- Component: browsers / Fetch Standard.
- Justification now: comment.
- Check at: Fetch Standard §3.1, "append a request `Origin` header": for a request whose method is neither GET nor HEAD and whose mode is not "cors", referrer policy "no-referrer" sets the serialized Origin to `null`. MDN Origin ("null ... when the referrer policy is no-referrer").
- Rating: **WRONG?** - the origin check depends on it. Under `no-referrer`, a same-origin form POST is sent with `Origin: null`; `originAllowed` (security.go:119-123) refuses "null". Wherever `Sec-Fetch-Site` is absent, every POST is then refused: browsers without Fetch Metadata (e.g. Safari before 16.4), and any browser on plain HTTP to a non-localhost host (B5). Django's CSRF origin check runs into the same thing ("Origin checking failed - null does not match"). Behind HTTPS with a current browser, Sec-Fetch-Site answers first, so production is unaffected today. Settle it with one POST from a browser on the stand without Sec-Fetch-Site (or a plain-HTTP non-localhost host).

**B4. security.go:83-89, 100-106** - SameSite is judged per site (registrable domain), so a neighbouring subdomain's forged POST carries the Lax cookie. `Sec-Fetch-Site` is per origin and is a forbidden header (page script cannot set it). Its values include `none` for a navigation with no initiator.
- Component: browsers - RFC 6265bis; W3C Fetch Metadata.
- Justification now: comment; no citation.
- Check at: draft-ietf-httpbis-rfc6265bis §5.2 ("same-site"), §5.6.7 (SameSite=Lax); https://www.w3.org/TR/fetch-metadata/ §2.4 (Sec-Fetch-Site values); Fetch Standard "forbidden request-header" (`Sec-` prefix).
- Rating: **GUESSED**.

**B5. security.go:108-110** - "Older browsers send only Origin."
- Component: browsers - Fetch Metadata.
- Justification now: comment.
- Check at: https://www.w3.org/TR/fetch-metadata/ §2 (the headers are sent only to potentially trustworthy URLs: HTTPS, localhost).
- Rating: **WRONG?** (incomplete) - current browsers also send only Origin when the panel is reached over plain HTTP at a non-localhost name: Fetch Metadata goes only to potentially trustworthy origins. The code path handles it, but together with B3 the Origin it then gets is "null".

**B6. security.go:111-118** - a request with neither header comes only from a very old client (accepted risk).
- Component: browsers.
- Justification now: comment (accepted risk).
- Check at: Fetch Standard §3.1 (Origin on non-GET/HEAD requests); MDN Origin, browser compatibility table.
- Rating: **GUESSED**.

**B7. security.go:119-123** - the opaque origin is serialised as "null" (sandboxed iframe, a cross-origin POST after a redirect), and url.Parse("null") has no host.
- Component: browsers - Fetch/HTML origin serialisation.
- Justification now: comment.
- Check at: Fetch Standard "serializing a request origin"; HTML §7.1.1 (opaque origins serialise to "null").
- Rating: **GUESSED** - true, but incomplete in the same way as B3: the panel's own POSTs are also sent with "null" under its own Referrer-Policy.

**B8. security.go:63-71** - reverse proxies preserve Host: Apache with `ProxyPreserveHost On`, nginx with `proxy_set_header Host $host`, "Caddy and Traefik by default".
- Component: Apache httpd 2.4 mod_proxy, nginx, Caddy 2, Traefik 2/3.
- Justification now: comment; no citation, no versions.
- Check at: https://httpd.apache.org/docs/2.4/mod/mod_proxy.html#proxypreservehost; https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_set_header (default `Host $proxy_host`); https://caddyserver.com/docs/caddyfile/directives/reverse_proxy#headers (Host passed through by default); https://doc.traefik.io/traefik/routing/services/#pass-host-header (passHostHeader default true).
- Rating: **GUESSED**.

**B9. security.go:30-39** - HSTS sent over plain HTTP "would be meaningless at best"; without includeSubDomains it does not affect siblings.
- Component: browsers - RFC 6797.
- Justification now: comment.
- Check at: RFC 6797 §8.1 (UAs ignore STS received over non-secure transport), §6.1.2 (includeSubDomains).
- Rating: **GUESSED**.

**B10. internal/web/auth/handlers.go:15-16, 26-31, 56-66** - the `__Host-` prefix makes the browser accept the cookie only with `Secure`, `Path=/`, no `Domain`, from a secure origin, so another host on the domain cannot plant or overwrite it. Clearing the prefixed cookie needs `Secure` (line 64).
- Component: browsers - RFC 6265bis cookie prefixes.
- Justification now: none (the constant is not commented).
- Check at: draft-ietf-httpbis-rfc6265bis §4.1.3.2 "The __Host- Prefix"; MDN Set-Cookie §Cookie prefixes.
- Rating: **GUESSED**.

**B11. auth/handlers.go:33-53** - two cookies of the same name in one request mean "another host on this domain is overwriting the session cookie"; a parent-domain cookie is sent alongside the host-only one.
- Component: browsers - RFC 6265 §5.4.
- Justification now: log text only.
- Check at: RFC 6265 §5.4 (Cookie header: all matching cookies, same name allowed; order by path length, then creation time).
- Rating: **GUESSED**.

**B12. auth/handlers.go:58-66, 134-142; auth/session.go:32-34** - `SameSite=Lax` withholds the cookie on cross-site POSTs. `Max-Age` makes the browser drop the cookie when the idle window elapses, and re-setting it on renewal slides it.
- Component: browsers - RFC 6265bis, RFC 6265 §5.2.2.
- Justification now: session.go comment ("so the browser drops the cookie no later than the server would have expired it anyway"); no citation.
- Check at: RFC 6265 §5.2.2 (Max-Age); rfc6265bis §5.6.7 (SameSite enforcement).
- Rating: **GUESSED**.

**B13. internal/web/handlers/handlers_apps.go:396-401, 528-531** - `Cache-Control: no-store` (with `Pragma: no-cache`) keeps the page that shows a password "out of every cache and out of the browser's history copy", and as a POST response it "is never stored by the browser".
- Component: browsers - RFC 9111; HTML history / back-forward cache.
- Justification now: comments; no citation.
- Check at: RFC 9111 §5.2.2.5 (no-store applies to HTTP caches) and §5.4 (Pragma is deprecated, has no defined response meaning); HTML Living Standard §7.4 (session history); web.dev "bfcache" / Chrome's rollout of bfcache for Cache-Control: no-store pages.
- Rating: **GUESSED**. Leaning WRONG? for "never stored": no-store governs HTTP caches, not the session history's live document, and Chrome has been allowing bfcache for no-store pages under conditions. `Pragma` in a response does nothing.

**B14. internal/web/view/static.go:11-19, 42-52** - without a validator or explicit freshness the browser may guess a lifetime (heuristic freshness). `Cache-Control: no-cache` lets it store the copy but revalidate on every use, answered by a 304 from the ETag.
- Component: browsers - RFC 9111.
- Justification now: comments; no citation.
- Check at: RFC 9111 §4.2.2 (heuristic freshness), §5.2.2.4 (no-cache), RFC 9110 §13.1.2 (If-None-Match).
- Rating: **GUESSED**.

**B15. internal/web/handlers/handlers_account.go:94; handlers_settings.go:104** - "An unticked checkbox is not sent at all".
- Component: browsers - HTML form submission.
- Justification now: comment.
- Check at: HTML Living Standard §4.10.21.4 "Constructing the entry list" (checkbox skipped unless checked).
- Rating: **GUESSED**.

**B16. internal/web/view/templates/layout.html:21-27** - "a browser takes the last icon whose type it understands", so the SVG declared second wins wherever SVG favicons are supported.
- Component: browsers - HTML `link rel=icon`.
- Justification now: comment.
- Check at: HTML Living Standard §4.6.7.8 "Link type icon" ("If multiple icons are equally appropriate, user agents must use the last one declared"; `sizes` and `type` decide what is appropriate).
- Rating: **GUESSED** (the PNG declares `sizes="32x32"` and the SVG none, so "equally appropriate" is browser-dependent).

**B17. layout.html:37-38; panel.js:168-170, 245-257** - both scripts are `defer`: they run in order after parsing, before `DOMContentLoaded`, so panel.js can use `htmx` and `document.body` at once and still catch `DOMContentLoaded`.
- Component: browsers - HTML script processing.
- Justification now: none.
- Check at: HTML Living Standard §4.12.1.1 (defer: "executed when the document has finished parsing", in order, before DOMContentLoaded).
- Rating: **GUESSED**.

**B18. panel.js:8-13, 24-28** - the Clipboard API is absent on plain HTTP outside localhost (secure contexts only).
- Component: browsers - Clipboard API.
- Justification now: comment.
- Check at: MDN Navigator.clipboard ("Secure context"); W3C Clipboard API §8.
- Rating: **GUESSED**.

**B19. panel.js:65-77** - the `submit` event bubbles to `document`, so a delegated listener covers forms swapped in by htmx; `window.confirm` blocks the submission when it returns false.
- Component: browsers - HTML form submission.
- Justification now: comment.
- Check at: HTML Living Standard §4.10.21.3 (fire `submit` with bubbles: true; cancelable).
- Rating: **GUESSED**.

**B20. panel.js:141-144** - `hidden` alone does not hide an element whose stylesheet sets `display` (Bulma's `.box`), so Bulma's `is-hidden` is toggled with it.
- Component: browsers (UA stylesheet `[hidden]{display:none}` loses to author CSS); Bulma 1.0.4 `.is-hidden` (`display: none !important`).
- Justification now: comment.
- Check at: HTML Living Standard §15.3.1 (hidden attribute rendering); Bulma 1.0.4 `sass/helpers/visibility.scss`.
- Rating: **GUESSED**.

**B21. internal/web/view/components.go:108-111; templates/components.html:31-35** - the Inbound log's sheet is "the browser's own popover" (`popover`, `popovertarget`, `popovertargetaction="hide"`), working with no script.
- Component: browsers - HTML popover API (Baseline 2024: Chrome 114, Firefox 125, Safari 17).
- Justification now: comment; no browser floor stated anywhere.
- Check at: HTML Living Standard §6.12 "The popover attribute"; MDN Popover API compatibility.
- Rating: **GUESSED** (in older browsers the sheet simply does not open).

### 4. Go html/template (internal/web/**)

**T1. internal/web/security.go:22-23; view/components.go:9-12** - "XSS is already prevented by html/template's contextual auto-escaping".
- Component: Go html/template (toolchain of go.mod).
- Justification now: refers to security.md, which states the same; no citation of html/template's documentation.
- Check at: https://pkg.go.dev/html/template ("Contexts", "Security Model"); `go/src/html/template/attr.go` `attrType`.
- Rating: **GUESSED**. Implicit caveat: html/template treats attributes it does not know as plain text. That covers `hx-get` (components.html:87, out_log_rows.html:6), `hx-swap` and `popovertarget`: their values are escaped as text, but a `javascript:` URL in `hx-get` would not be filtered (only names containing "src"/"uri"/"url" or starting with "on" get URL/JS treatment). Today all `hx-get` values are server-built paths.

**T2. internal/web/view/components.go:164-168** - `template.HTMLEscapeString` escapes every character that matters (`< > & ' "` and NUL), so marking the result `template.HTML` after inserting `<wbr>` is safe.
- Component: Go html/template.
- Justification now: comment "wbrAt escapes s".
- Check at: `go/src/html/template/escape.go` `HTMLEscapeString` -> `text/template` `HTMLEscape` (escapes `<`, `>`, `&`, `'`, `"`, NUL).
- Rating: **GUESSED** (correct).

### 5. Postfix claims in panel help/health text (internal/web/view)

These are user-facing statements; the Postfix behaviour itself probably sits in another inventory's area as well (milter, logtail).

**P1. internal/web/view/help.go:174; view/health.go:178** - while the sender check (strict milter) does not answer, Postfix replies with a temporary error. A connection is refused "from the connection on, before a message is offered"; a check that stops mid-message defers that message (cleanup's warning).
- Component: Postfix 3.7.11 smtpd/cleanup milter client (`milter_default_action`, smtpd_milters connect-time handling).
- Justification now: none in the text. architecture.md/guide.md may cover it (see §6).
- Check at: `dev/upstream/postfix-3.7.11/src/milter/milter.c` / `milter8.c` (`milter8_conn_event`, default action), `src/smtpd/smtpd.c` (milter connect at session start); https://www.postfix.org/MILTER_README.html §"Error handling"; postconf.5 `milter_default_action`.
- Rating: **GUESSED**.

**P2. view/help.go:184** - a deferred message is retried after the first retry delay, then at gaps that double up to a cap, until it is taken or the queue lifetime runs out; there is no attempt counter.
- Component: Postfix 3.7.11 qmgr (`minimal_backoff_time`, `maximal_backoff_time`, `maximal_queue_lifetime`).
- Justification now: none.
- Check at: postconf.5 #minimal_backoff_time / #maximal_backoff_time / #maximal_queue_lifetime; `src/qmgr/qmgr_active.c` `qmgr_active_defer()` (the doubling).
- Rating: **GUESSED**.

**P3. view/help.go:199** - TLS modes of the inbound hop (Opportunistic = `may`, Required = `encrypt`, Off = `none`); the upstream host "used exactly as written, with no MX lookup"; while the upstream does not answer, Postfix keeps the mail and retries.
- Component: Postfix 3.7.11 smtp client (`smtp_tls_security_level`, transport `[host]:port` syntax).
- Justification now: none.
- Check at: TLS_README §"Client-side TLS security levels"; transport.5 (square brackets suppress MX lookups).
- Rating: **GUESSED** (the generator does write `[host]:port`, internal/postfix/inbound.go:91; the Postfix semantics are uncited).

**P4. view/help.go:224** - "rspamd rereads a file within seconds of a change" (the filter-list map files).
- Component: rspamd (external, version not pinned by SelfPost).
- Justification now: none.
- Check at: https://rspamd.com/doc/configuration/maps.html (file maps are monitored; reload interval).
- Rating: **GUESSED** (no rspamd version is named, so the claim cannot be pinned to one).

**P5. view/help.go:151** - clients that verify certificates refuse to connect when the certificate expired.
- Component: SMTP clients.
- Justification now: none.
- Rating: **GUESSED** (generic; low value to cite).

---

### 6. docs/architecture.md (read in full, lines 1-1015; helper agent's inventory, condensed)

Overall: architecture.md cites **no** upstream source or doc for any claim. Its one measurement is line 295 ("seen on the e2e stand", no date). Line 124 says "seen on rspamd 3.11" without a date or setup, so it is rated GUESSED. Line 924 ("The panel is the only writer") contradicts line 455 (dmarc-ingest commits) and S3/S10. Paths are under dev/upstream/. All entries are GUESSED unless marked.

#### Postfix 3.7.11
- :20-21 - with no hostname set Postfix uses the container id, the SASL realm splits, AUTH fails with 535. Check: postconf.5 myhostname; src/xsasl/xsasl_cyrus_server.c xsasl_cyrus_server_create.
- :37-38 - `postfix start-fg` runs master in the foreground. Check: conf/postfix-script.
- :67-69 - `postconf -MX smtp/inet` removes the stock smtpd. Check: postconf.1 -M/-X.
- :74-75 - smtpd_tls_security_level=encrypt makes STARTTLS mandatory. Check: postconf.5.
- :77-81 - a master.cf `-o smtpd_milters=` replaces main.cf's chain on port 25. Check: master.5.
- :83-84, 139-141 - per-milter default_action=accept is fail-open; Postfix's default is tempfail (src/global/mail_params.h:3460). Line 140 reads ambiguously.
- :85-88, 97-99 - milters are called in turn at end-of-message, each seeing the previous one's headers, and a refusal stops the chain. Check: src/milter/milter.c milter_message.
- :100-104 - `syslog_name` gives postfix/inbound/smtpd, while cleanup stays postfix/cleanup; smtpd logs `reject:`, cleanup logs milter-reject/-hold/-discard under the queue id. Check: src/cleanup/cleanup_milter.c ~2070-2121.
- :106-108 - a cleanup refusal line names one recipient (the last: cleanup_milter.c ~2119, cleanup_addr.c:233).
- :133-135, 141-143 - relay_domains/relay_recipient_maps/reject_unlisted_recipient; check_recipient_access + pipe to dmarc-ingest. Check: postconf.5, pipe.8.
- :147-149 - sender_login_maps hold the envelope sender only. Check: src/smtpd/smtpd_check.c:3913.
- :151-152 - the anvil limit is independent of the milters. Check: postconf.5 (note smtpd_client_event_limit_exceptions = $mynetworks by default).
- :153 - chroot off for DNS/TLS. Check: master.5.
- :154-156 - "Postfix wants the key root-owned and 0600": TLS_README l.76-82 only advises it, and there is no check in src/tls. The wording should say advice.
- :157-159, 941-942, 1013-1014 - `postfix reload` brings in a renewed certificate, rewritten maps and a new log file. Check: postfix(1), src/master/master_sig.c. (Running smtpd processes keep the old certificate until they exit.)
- :185-192 - the six retry parameters and their defaults (300s/4000s/5d/0). Check: mail_params.h:770-781.
- :218-223 - sendmail(1) mail passes only non_smtpd_milters; Postfix's own bounces pass none. Check: mail_params.h:3544 DEF_INT_FILT_CLASSES.
- :235-243 - the login-map lookup order. Incomplete: the default also tries the localpart for local domains (src/global/mail_addr_find.h:26-39). Harmless with empty mydestination.
- :243-244 - logins are compared without regard to case: smtpd_check.c:3936 strcasecmp_utf8.
- :259-266 - a milter's reply code reaches the client; a milter that does not answer means a deferral under tempfail. Check: milter8.c.
- :284-290 - socket down: 451 4.7.1 at MAIL and 454 4.3.0 at AUTH (src/smtpd/smtpd_sasl_proto.c:170-180); the line does not name the milter.
- :291-295 - cleanup's content-timeout warning `can't read SMFIC_HEADER reply packet header` (milter8.c:593). **MEASURED** ("seen on the e2e stand with the panel frozen", no date).
- :296-297 - one cleanup per message, so one warning is one message. Check: cleanup.8.
- :323-327, 437-439 - `postqueue -j` arrival_time, and `-p` lists all ids. Check: src/showq/showq.c:169.
- :329-331 - a MAIL FROM refusal comes before the login check. Check: smtpd.c mail_cmd; mail_params.h:2420.
- :352-354 - postmaster@hostname gets no mail (build/postfix-config.sh:158).
- :365-367 - postdrop membership without setgid. Check: conf/postfix-files.
- :376, 387-393 - postlogd writes as postfix, recreates the log lazily with mode 0600 (src/util/logwriter.c:85 under master's umask 077, src/master/master.c:297), and keeps writing to the renamed file until reload (src/postlogd/postlogd.c:212).
- :397-400, 493-498, 541-543 - log line layout; short ids are capital hex and long ids vowel-less; a short id is the tail of a longer one. Check: src/util/msg_logger.c, src/global/mail_queue.h:95-100.
- :420-426, 431-432 - the queue id is given before the headers are judged, freed at once on refusal; NOQUEUE milter-reject format; `client=` carries the login; the END-OF-MESSAGE From line. Check: smtpd.c:2136, cleanup_milter.c ~2100-2121.
- :442-444, 452-454, 472-475, 501-506, 519-523 - nothing is reported after a message leaves; short ids are reused; a message may stay past the lifetime by up to a backoff; `removed` line; client=/message-id= logged before end-of-message; lifetime = max(maximal_, bounce_queue_lifetime). Check: src/qmgr/qmgr_active.c, postconf.5.
- :532 - 6-10 lines of about 200 bytes per message: an estimate; measure.

#### OpenDKIM 2.11.0~beta2
- :29 - starts as root and drops to UserID. :169-172, 230-231 - SigningTable chooses by From:. :247-249 - its own From parser (libopendkim/dkim-mailparse.c:365). :941-942 - a reload re-reads the tables (opendkim/opendkim.c:5714, SIGUSR1).

#### Cyrus SASL 2.1.28
- :19-22, 989 - realm = hostname, or sasldb lookups fail with 535. Check: lib/server.c sasl_server_new; plugins/sasldb.c.

#### rspamd (version unpinned in docs) / milter macros
- :88, 92-96, 121-130 - header names; HMAC routine; deny = prefilter reject, allow = -100; with two prefilters the first decides ("seen on rspamd 3.11", undated); deny beats any score; Lists.Usage matches as multimap. Check: rspamd milter_headers / multimap docs.
- :195-196 - {auth_authen} at MAIL (mail_params.h:3473). :206 - ABORT on a transaction without end-of-message (milter8.c milter8_abort).

#### supervisor 4.2.5
- :40-44, 279-281 - crashexit on FATAL (only after start retries are exhausted); RUNNING means live; the start time is the wrapper's. Check: supervisor/process.py:153,402; rpcinterface.py getProcessInfo.

#### logrotate 3.21.0
- :384-388, 410, 470-471, 488-489, 930-931 - copytruncate can lose lines; create 0640 before postrotate; delaycompress; the rename-create gap loses nothing; daily checked every 6 h means once a day, 14 kept. Check: logrotate.8, logrotate.c.

#### Docker
- :40-41 - "Docker restart: unless-stopped recreates a broken instance" - **WRONG?**: a restart policy restarts the same container (same writable layer), it does not recreate it. build/supervisord.conf:8-10 repeats it. The .alive mark is cleared by build/entrypoint.sh:163, not by Docker.
- :276-278 - "a new container starts without [the mark]": /run is not a tmpfs; the guarantee is entrypoint.sh:163.
- :366-367 - no-new-privileges disables setgid. :374-376, 876 - the bind mount survives a recreate.

#### SQLite (see section 1)
- :454-456 (= S10), :511-512 (= S12), :516-518 (a second per million lines, undated: measure), :887-888 (= S5/S6), :754-755 (= S14).
- :924-926 - VACUUM INTO for a live backup. "The panel is the only writer" (:924) contradicts :455.

#### Browser / htmx (see sections 2-3)
- :645-649 (= H2, H8), :724-730 (= B1, H1).
- :732-734 - browsers send Sec-Fetch-Site and/or Origin on state-changing requests: see B3/B5 (Origin is "null" under the panel's own Referrer-Policy).
- :253-254 - MUA display of an encoded word: measure.

#### Other
- :19 - the hostname must match PTR and the certificate. :257-258 - Go leaves some charsets undecoded. :288-289 - a client brings a message per connection.

Counts (architecture.md): CITED 0, MEASURED 1, GUESSED 91, WRONG? 1.

---

### 7. docs/guide.md (read in full, 1320 lines; helper agent's inventory, condensed)

Overall: the only citation is "RFC 6409 submission" (:217); there are no measurements. Entries are GUESSED unless marked. Paths are under dev/upstream/.

#### WRONG? (13)
- **:227** - INBOUND_MESSAGE_SIZE_LIMIT (25 MiB) caps port 25 via message_size_limit. build/postfix-config.sh:467 sets it only as the smtpd `-o smtp/inet/message_size_limit`, but cleanup enforces main.cf's message_size_limit (src/cleanup/cleanup_init.c:450-451 `set_file_limit(var_message_limit)`, default 10240000 at src/global/mail_params.h:1997). Mail over about 9.8 MB is refused by cleanup.
- **:228, 621, 624** - level 1 is "messages one client IP may submit per window". anvil keys on service + address (src/global/anvil_clnt.c:170 `ANVIL_IDENT(service, addr)`), so 465 and 587 count separately, and the counter counts MAIL FROM requests, not accepted messages.
- **:301-303** - `docker compose run --rm certbot certonly --webroot ...` - the args are appended to the service's `sh -c` entrypoint (deploy/nginx/docker-compose.nginx.yml:57) and ignored, so this needs `--entrypoint certbot`. Also, nothing serves port 80 for the HTTP-01 challenge at that point.
- **:578-579** - upgrade with `docker compose up -d`: no 2.x tag exists on GHCR (per :187), so the pull fails unless the image was built locally.
- **:788-790** - `docker compose pull && up -d` fails the same way. The sample message shows a 1.x restore, which :821 says is impossible.
- **:877-881** - `docker exec -e SELFPOST_BACKUP_PASSWORD="$PW"` puts the password in the host's argv (visible in `ps`). Use `-e SELFPOST_BACKUP_PASSWORD` (name only).
- **:258, 263-265** - certbot's `live/` files are "plain PEM ready to bind-mount". They are relative symlinks into `../../archive/` and dangle inside the container.
- **:291-295, 309-310** - linking `certs` to `/etc/letsencrypt/live/<host>` "is fine". It breaks for the same reason (deploy/nginx/docker-compose.nginx.yml:35, deploy/docker-compose.yml:77).
- **:722** - a stopped `tar` of the project contains the private key. With the recommended symlink, tar stores the link (no `-h`).
- **:795-796** - certificates must be re-issued when the IP changes. DV certificates name the host, not the IP.
- **:1150-1151** - after a 4xx "applications send again later". Only MTAs queue; application libraries surface the error.
- **:1179-1181** - "DKIM passing doesn't help if SPF/DMARC are absent" / "single most common reason". An aligned DKIM pass alone satisfies DMARC (RFC 7489 §4.2); there is no source for "most common".

#### Postfix 3.7.11 (GUESSED)
- :74-76 (starts without TLS material; smtpd.c pre_jail_init); :223, 934-936, 1251-1252 (accept vs tempfail milters; milter8.c:554, MILTER_README); :226, 229, 622 (anvil_rate_time_unit window); :625 "no per-IP bypass" - not literally true, since smtpd_client_event_limit_exceptions defaults to $mynetworks (mail_params.h:3206), and with Docker's userland proxy every client may share the gateway IP; :336-338 (a renewed certificate after reload; running smtpd keep the old one); :424-427, 741-745 (Resync/reload re-applies maps); :432-434 (Queue = postqueue -p; showq also lists hold `!`); :435-440, 1285-1286 (retry parameters; no attempt counter; null-sender mail uses bounce_queue_lifetime; qmgr_active.c:454-455); :442-443 (reload after rotation - supported by README_FILES/MAILLOG_README:69); :633-637, 721 (queue copy consistency; postsuper repairs names at start, conf/postfix-script:224-226,287); :658-659 (a root-only key is enough); :917-921 (inbound relay restrictions, no backscatter); :1061-1065 (pipe `delivered via ... service` text, src/pipe/pipe.c:1065); :1067-1070 (check_recipient_access); :1104-1106 ("does not tell Ops from ops" - also depends on sasldb case, see S21); :1117, 1121-1123 (envelope check; `+extension` via mail_addr_find); :1125 (milter 550 text relayed; smtpd.c:1739); :411-414, 1149-1151 (451/454 when the check is down; smtpd.c:5776-5806, smtpd_sasl_proto.c:171-180 - "turned away" means a 220 greeting followed by 451/454); :1211-1222 (4xx at MAIL FROM); :1264-1267 ("bounced" - Postfix logs `status=expired` on lifetime expiry, qmgr_active.c:455); :1277-1279 (NOQUEUE vs queue-id lines); :1288-1290 (delivery line contents).

#### OpenDKIM 2.11.0~beta2 (GUESSED)
- :424-425, 741-745 (reload picks up the tables); :1118-1119 (signature chosen by From:, opendkim.c:7351-7370 SenderHeaders default); :1128-1134 (own From parser).

#### Cyrus SASL 2.1.28 (GUESSED)
- :216 (hostname is the realm); :1225 (login succeeds, and the IP check comes later in the milter).

#### rspamd / milter (GUESSED)
- :222 (inet:/unix: syntax); :933-934 (the milter sees the real client); :940-958 (milter_headers, the signing routine, maps reread, restart only for config); :965-966 "rereads within seconds" - if map_watch_interval 5 min × map_file_watch_multiplier 0.1 are the defaults, that is about 30 s; :966-970 (domain entry excludes subdomains; deny refuses, allow outweighs, deny wins); :984-986 (counts depend on rule message/symbol names).

#### supervisord / logrotate (GUESSED)
- :403, 586-587 (RUNNING); :415-416 (≈ start time); :442-444 (daily, 14 kept, checked every 6 h; `notifempty` skips an empty log).

#### Docker (GUESSED)
- :61-68 (published ports look open); :187-190 (no 2.x on GHCR, undated; compose uses the local image, pull_policy missing); :233 (the peer is the bridge gateway - "host-network setup" is misleading wording); :585-593 (HEALTHCHECK via inspect); :665-666 (exec as root - no USER in build/Dockerfile); :683-686, 717-719 (down then tar); :813, 873 (exec -i streams binary); :893, 1022 (up -d recreates on .env change).

#### SQLite (GUESSED)
- :663-664, 711-713, 719 (VACUUM INTO is consistent on a live database); :709-711 (copying a WAL database while it is written can be inconsistent; sqlite.org/howtocorrupt.html §1); :719 (a stopped copy is consistent).

#### Browser / reverse proxy / certificates (GUESSED)
- :260 (Caddy's path is internal - hedge "verify it for the Caddy version you run"); :261 (Traefik acme.json); :268-271 (panel headers suffice); :272-276 (Host preservation by proxy = B8; "every form submission fails the Origin check" assumes Origin on every POST - see B3); :233 (last XFF hop); :281 (Apache modules); :657-658 (re-apply chgrp/chmod after renewal; certbot keeps the previous key's mode/group).

#### DNS / mail standards
- :217 - RFC 6409 submission on 587 - **CITED**.
- GUESSED: :85-87 (local delivery unreliable); :146-147, 216, 420-422, 597-603 (PTR/FCrDNS); :232 (public resolvers); :500-510 (notification DKIM/SPF, strict parent DMARC, the hostname as a sending domain); :513-514, 1176-1177 (no public mail hosts as external rua); :911-915 (backup MX; `10 <hostname>.` may load-share rather than back up); :971-973 (envelope sender unchecked); :1016-1019, 1047-1052 (rua/ruf; MX for the rua domain; implicit MX makes it not strictly required); :219 vs :1079 (gzip/XML vs gzip, zip, XML - inconsistent); :1128-1135 (RFC 5322 quoting; 5xx is final); :1145-1147 (visitor From breaks DMARC); :1164-1169 (SPF example; selector-keyed DKIM; p=none); :1198-1203 (warm-up).

#### Other (GUESSED)
- :102 (self-signed: clients skip verification); :398-402 (/proc counters - in a container meminfo shows the host's totals, net/dev only the container's interfaces); :877-878 (argv visible in the process list).

Counts (guide.md): CITED 1, MEASURED 0, GUESSED 84, WRONG? 13 (98 entries).

---

### 8. docs/security.md (read in full, 627 lines; helper agent's inventory, condensed)

Overall: no claim names a source for the version in use, so nothing is CITED. Three rest on a measurement (e2e / test stand). Entries are GUESSED unless marked. Paths are under dev/upstream/.

#### WRONG? (4)
- **:91-92** - sasldb2 stores the password "in the form SASL requires (not plaintext held by the panel)", so a lost password can only be reissued. sasldb keeps `userPassword` in cleartext: the panel itself reads it back with `db_dump` (internal/app/sasl.go:89-137 parseSASLSecret), and domain exports carry working passwords (:292). Check: cyrus-sasl2-2.1.28+dfsg/plugins/sasldb.c.
- **:272-273** - the HELO name is "logged as written". smtpd stores it through `printable()` (postfix-3.7.11/src/smtpd/smtpd.c:1844, :1929), so non-printables become `?`. The conclusion (client-chosen text) still holds.
- **:472-473** - mail.log holds "neither message bodies nor headers". cleanup logs Message-ID (src/cleanup/cleanup_message.c:630-632; :296 itself says so), and smtpd's milter-reject line carries the sender check's reply, which quotes the client's From: text (:234).
- **:509-511** - "Supervisord restarts the panel at once and takes the container down after three failed starts." With startretries=3, FATAL comes after the 4th failed start; a crash within startsecs backs off 1/2/3 s (supervisor-4.2.5/supervisor/process.py change_state :171, transition :680/:701; build/supervisord.conf:54); and it is SelfPost's crashexit listener (supervisord.conf:123) that takes the container down.

#### MEASURED (3)
- :41-43 - before the fix, limits applied to the first message of a connection only: "reproduced on the test stand and fixed" (2026-10-12 review).
- :258 - "test/e2e sends two MAIL FROM on one connection" (both milters judge every message).
- :213-223 - the panel's From parser agrees with OpenDKIM's: the e2e over from-lines-passed.txt / from-lines-other.txt.

#### Postfix 3.7.11 (GUESSED)
- :31-34 (pipe could run a hundred at once - default_process_limit, mail_params.h DEF_PROC_LIMIT; check `<transport>_destination_concurrency_limit`); :199-203, 239-244 (not an open relay; envelope only; refused at RCPT; no backscatter in list mode - smtpd_check.c:3913/:3954, check_rcpt_maps); :210-212, 259-260 (milter order and tempfail stops the chain - src/milter/milter.c); :236-237, 495-497 (local submission meets only non_smtpd_milters and no anvil); :245-248, 406-409 (per-milter default_action; SMFIC_CONNECT once per connection - milter8.c:1909; AUTH always succeeds, no milter event covers it); :255 ({client_addr} in milter_mail_macros - mail_params.h:3464/:3473); :270-273 (the reply ends at `; from=<`, then helo=); :282-283 (TLS before AUTH; smtpd.c:6356 `smtpd_tls_auth_only || enforce_tls`, :5823 - source supports it); :294-297 (what is logged per message; log_adhoc.c); :379-380 (absent from postqueue -p = left the queue; showq also scans hold); :394-401 (recipient in the pipe argv; dropped-not-bounced by exit status - pipe.c ~:1097); :470-471 (postlogd as postfix; conf/master.cf:79); :489-490 (MAIL FROM milter refusal comes before the login check); :493-495 (postdrop setgid under no-new-privileges); :503-507, 519-521 (tempfail + 15 s/30 s timeouts - milter8.c:1737, :2615, :2521/:2580); :518-519 (0660 sockets reached via group); :528-532, 537-538 (short queue id = µs + inode, repeats; lifetime about 6 days; long ids end collisions - mail_queue.h:89-150); :542-548 (transport to any host:port); :102 (escaped writes cannot inject directives).

#### OpenDKIM 2.11.0~beta2 (GUESSED)
- :201-206 (key by From: via SigningTable - opendkim.c mlfi_eoh :11753, dkimf_apply_signtable :9746, SenderHeaders :7351); :230-233 (display name without a findable @ signs with the application's key - dkim-mailparse.c); :495-496, 523-525 (notifications signed with the hostname's key; the hostname as a sending domain).

#### Cyrus SASL 2.1.28 (GUESSED)
- :6-7 (`--` ends saslpasswd2 option parsing - utils/saslpasswd.c getopt).

#### go-milter v0.4.1 / rspamd (GUESSED)
- :248-250 (session replaced after a non-continue reply); :461-464 (no close callback, so a reservation lives until TTL - and whether Postfix sends SMFIC_ABORT on disconnect, milter8.c milter8_abort / smtpd.c mail_reset); :515-517 (an empty packet panics the library - "(technical review, 2026-10-12)", a review, not a source citation); :262-268, 51-53 (rspamd strips verdict headers when up; HMAC of the queue id equals the one the milter recomputes - `{i}` macros); :277-281 (map syntax: whitespace, newline, #); :430-435 (envelope sender; allow outweighs; deny wins).

#### supervisord / logrotate (GUESSED)
- :194 (user=panel drops root - supervisor/options.py drop_privileges); :378 (rotate 14 loses older lines).

#### Docker / Linux (GUESSED)
- :312-313 (argv readable in the container; hidepid); :374-375 (mail.log survives recreation); :424-425, 440-442, 266-267 (the filter's uid; 0755/0644); :493-495 (no-new-privileges); :518-522 (0660 unix sockets, unix(7)).

#### SQLite (GUESSED; see section 1)
- :155-159 (write first, count after = S9); :159-160 (single-statement insert-if-empty = S8); :389 (dmarc_reports "upsert is delete-then-insert" - the code does an explicit DELETE then INSERT in one transaction, dmarc.go:85-131; S14 for the cascade).

#### Browser / htmx (GUESSED; see sections 2-3)
- :58 (__Host- = B10); :107-108, 177 (cookie flags as CSRF protection = B12); :122 (HX-Request = H8); :174-177, 578-581 (Sec-Fetch-Site/Origin identify the panel - see B3/B5: over plain HTTP only Origin, and under no-referrer it is "null"); :324-326, 600-602 (only old browsers send neither header); :335-337 (the origin check closes the sibling-subdomain case); :341-343, 597-598 (CSP + html/template = B1/T1); :340-341, 596-597 (same-origin script can read tokens); :353-354 (data-confirm without JS submits at once = B19).
- security.md says nothing specific about HSTS, X-Frame-Options, Referrer-Policy or caching (:58 says only "security headers"). B3's Referrer-Policy question is therefore not discussed in security.md at all.

#### Go stdlib / crypto (GUESSED)
- :192-193 (html/template = T1); :75 (crypto/rand token ≥128 bits); :79 (subtle.ConstantTimeCompare - returns at once on length mismatch, harmless for fixed-length); :100-101 (os/exec has no shell); :301-304 (scrypt + AES-256-GCM chunked STREAM); :265, 269-270 (HMAC-SHA256 unforgeable, RFC 2104).

#### Reverse proxy (GUESSED)
- :284-285 (only trusted proxies supply XFF, assuming the proxy appends the real client); :176 (Origin must name the panel's host - relies on Host preservation, B8).

#### Other
- :390-391 (rua reporters are arbitrary third parties - RFC 7489 §7.1).

Counts (security.md): CITED 0, MEASURED 3, GUESSED 69, WRONG? 4 (76 entries).

# Answers

Each entry settled in the source of the version the image runs (2026-10-14).
Verdicts: **HOLDS** (the citation is now in the code beside it), **PARTLY**
(which part fails), **DOES NOT HOLD** (what the source says, the consequence,
the fix), **NEEDS A MEASUREMENT** (what to measure on the stand). Part D's
document claims are named by document and line: D-arch:40, D-guide:227,
D-sec:91. The fixes are lines of audit-2026-10-12, stage 2c.

## Part A - answers (2026-10-14)

Sources read: dev/upstream/ (postfix-3.7.11, opendkim-2.11.0~beta2, cyrus-sasl2-2.1.28+dfsg,
supervisor-4.2.5, logrotate-3.21.0, Debian patches applied). Where the 3.7.11 tree could not
answer: the Postfix 3.12 snapshot of 2026-09-25 (github.com/vdukhovni/postfix, master:
proto/postconf.proto, src/util/msg_logger.c), Linux torvalds/linux master read 2026-10-14
(kernel/sched/cputime.c, fs/proc/meminfo.c, fs/proc/stat.c, arch/x86/kernel/cpu/proc.c,
net/core/net-procfs.c, net/core/dev.c), man pages proc(5), unix(7), bind(2), inode(7),
rename(2), network_namespaces(7), and certbot/docs/using.rst (main). Line numbers are of the
working tree after the edits. Nothing in the code's behaviour was changed; only comments and
fixture labels.

### Entries

A-1: HOLDS - Postfix 3.7.11, src/global/mail_queue.c mail_queue_enter(); src/global/mail_queue.h MQID_SH_* / MQID_LG_*; src/global/safe_ultostr.c safe_chars - short id "%05X" usec + "%lX" inode = 6..21 chars; long id 6+ sec, 4 usec, 'z', inode base 51 = 12..23 chars, alphabet "0-9 B-Z b-z" minus vowels, which is exactly the regexp's class. The lower bound {10, is looser than needed (12), harmless. NOQUEUE and warning contain vowels/non-hex. - comment at internal/logtail/lines.go:31.

A-2: HOLDS - src/global/mail_task.c mail_task() ("%s/%s", syslog_name, process name); src/global/mail_params.c mail_params_init() exports the (-o overridden) syslog_name as MAIL_LOGTAG; src/master/single_server.c single_server_main() calls maillog_client_init(mail_task(...)) again after the -o options. Port 25 is "smtp/inet/syslog_name=postfix/inbound" (postfix-config.sh); the other listeners get the default "postfix". - comment at lines.go:41.

A-3: HOLDS - src/smtpd/smtpd.c mail_open_stream(): msg_info("%s: client=%s...") when the queue file is opened (at MAIL FROM or the first accepted RCPT, smtpd_delay_open_until_valid_rcpt), before any other line of that id; the program tag is the listener's. - comment at lines.go:86.

A-4: HOLDS - src/qmgr/qmgr_active.c qmgr_active_done_3_generic(): msg_info("%s: removed") after mail_queue_remove(); postsuper writes the same text (src/postsuper/postsuper.c). - comment at lines.go:86.

A-5: HOLDS - postconf(5) enable_long_queue_ids ("long, non-repeating, queue IDs"; default no); mail_queue.h: short id = microseconds + inode, so ids repeat, and their length varies with the inode, so one can be the tail of another. - comment at lines.go:86 (and mentionsQueueID's existing text).

A-6: PARTLY - src/global/log_adhoc.c log_adhoc(): "%s: to=<%s>", ", orig_to=<%s>" (only when strcasecmp_utf8(address, orig_addr) != 0), ", relay=%s", [", conn_use=%d"], delays, ", dsn=%s, status=%s (%s)". The shape holds. What fails: "qmgr/smtpd/cleanup lines do not match" - qmgr writes this very line for recipients it defers or bounces itself (src/qmgr/qmgr_defer.c defer_append(), src/qmgr/qmgr_bounce.c bounce_append(); log_adhoc() runs in the calling process, src/global/defer.c defer_append_intern(), bounce.c). Consequence: none harmful - those are genuine per-recipient results and the regexp (which does not restrict the program) takes them correctly; only the comment was wrong. - comment rewritten at internal/logtail/logtail.go:93 (cites log_adhoc and the qmgr case). Fix: none needed beyond the comment.

A-7: HOLDS - log_adhoc(): the reply is the trailing " (%s)" after status=; between relay= and status= come only conn_use=, delay=, delays=, dsn= (Postfix's own fields), so the first status= after relay= is the real one. - comment at logtail.go:117.

A-8: DOES NOT HOLD - src/qmgr/qmgr_active.c qmgr_active_done_25_generic():
    msg_info("%s: from=<%s>, status=%s, returned to sender", message->queue_id, ..., expire_status);
  with expire_status "expired" (or "force-expired" after postsuper -e). That line is once per message, has no to= and no relay=, so deliveryRe never matches it; adefer_flush() then has the bounce daemon send the notice ("sender non-delivery notification", bounce_notify_service.c) without any per-recipient status line. No Postfix 3.7.11 code writes "to=<..>, ..., status=expired". Consequence: an app sends to an address whose MX keeps answering 4xx for 5 days: each attempt writes "status=deferred", the row becomes "deferred"; on expiry Postfix returns the mail and removes the queue file, but the row stays "deferred" for ever - the sweep (store CloseUnreported / ListQueuedOlderThan) only touches rows still "queued". The panel shows "deferred" for a message that bounced days ago. The "expired" case in parseDelivery is dead code and the fixture at logtail_test.go ("expired maps to bounced") is a shape Postfix never writes.
  Fix: in logtail, match qmgr's "^\S+ postfix/qmgr\[\d+\]: (<id>): from=<.*>, status=(expired|force-expired), returned to sender$" (anchored at the head like deliveryRe) and call a new store method, e.g. ExpireQueueID(queueID) = UPDATE send_log SET status='bounced', reason='expired' WHERE queue_id=? AND status IN ('queued','deferred'). Drop the "expired" case from parseDelivery. Tests: TestParseDelivery loses the composed expired case; a new test feeds a deferred line then the qmgr expiry line and asserts the row is bounced; a store test for ExpireQueueID leaving sent/bounced rows alone; a test that a sender address quoting "status=expired, returned to sender" in from=<"..."> does not trigger (anchor + external quoting).

A-9: PARTLY - log_adhoc(): to= is the address after cleanup rewrote it (src/cleanup/cleanup_addr.c cleanup_addr_recipient(): cleanup_rewrite_internal(MAIL_ATTR_RWR_LOCAL, ...) then canonical maps), orig_to= the RCPT TO as given, shown only when they differ case-insensitively. The send-log row holds the RCPT TO the milter saw (internal/milter RcptTo). Holds whenever cleanup does not rewrite the recipient: this config sets no canonical/virtual maps, and case differences are hidden by strcasecmp + COLLATE NOCASE. Fails when trivial-rewrite qualifies the address: an app sending "RCPT TO:<ops>" (no domain; permit_sasl_authenticated lets it through) is delivered and logged as to=<ops@$myorigin>, orig_to=<ops>; UpdateStatus(queueID, "ops@...") matches no row, the row stays queued and the sweep closes it as bounced although it was delivered. - comment (log_adhoc citation, "only when the address was rewritten") at logtail.go:93.
  Fix: capture orig_to in deliveryRe (group 3, optional) and, when present, try UpdateStatus with orig_to first, then to. Tests: TestParseDelivery case with orig_to returns both; a Run-level test with a row whose To is the bare RCPT and a line carrying orig_to=<that> resolves it; the existing "orig_to is ignored in favour of to" case changes to expect orig_to as the row key.

A-10: HOLDS - every queue-id line is msg_info("%s: ...", queue_id) (log_adhoc.c, smtpd.c mail_open_stream(), qmgr_active.c "%s: removed", opened.c); the id is preceded by "]: " - comment at logtail.go:522.

A-11: HOLDS (MEASURED, now also cited) - src/util/msg_logger.c msg_logger_print(): strftime("%b %d %H:%M:%S ") of localtime(), then the first host label; postlogd writes the record as received (src/postlogd/postlogd.c postlogd_service() -> logwriter_write). The live-relay fixture is now labelled "date not recorded". - comment at logtail.go:549; fixture label in logtail_test.go TestSplitTimestamp.

A-12: DOES NOT HOLD - Postfix 3.7.11 proto/postconf.proto has no maillog_file_format; neither does the Postfix 3.12 snapshot of 2026-09-25 (github.com/vdukhovni/postfix master, mail_version.h "3.12", RELEASE_DATE 20260925): its postconf.proto has maillog_file, maillog_file_prefixes, maillog_file_compressor, maillog_file_rotate_suffix only, and its src/util/msg_logger.c still has
    strftime(..., "%b %d %H:%M:%S ", lt)
  So "maillog_file_format arrived in Postfix 3.9 and selects RFC 3339" is false as far as these sources show. Consequence: none at run time (isoStampRe matches no Postfix line and falls through); but most logtail test fixtures (inbound_test, refusals_test, sendercheck_test, parts of logtail_test) use ISO stamps, so the path production actually takes - syslog stamp, year inference in lineTime() - is exercised by only a handful of cases, and an upgrade plan built on this parameter would find nothing. - comment rewritten at logtail.go:557 (states the fact, cites both trees); no citation blessing the claim.
  Fix: (a) convert the fixtures' heads to the syslog form ("Oct 12 16:22:13 mail ..."), keeping the measured lines' text; (b) either delete isoStampRe or keep it labelled "no Postfix writes this" - recommendation: delete it and the RFC 3339 TestSplitTimestamp cases, since nothing produces that format; (c) rename the TestSplitTimestamp case "RFC 3339, which maillog_file_format selects on Postfix 3.9 and up". Tests: TestSplitTimestamp/TestLineTime with syslog stamps only; the inbound/refusal/sendercheck tests pass unchanged after the stamp conversion (lineTime then dates them with `now`'s year).

A-13: PARTLY - msg_logger.c msg_logger_print(): the stamp is made by the logging process with localtime(); postlogd only writes it (postlogd_service()). The effect holds (the zone is the container's: TZ is in import_environment's default, src/global/mail_params.h DEF_IMPORT_ENVIRON, and */*/chroot=n); the attribution "what postlogd wrote it in" was wrong. - comment corrected at internal/logtail/inbound.go:269. Fix: none beyond the comment.

A-14: HOLDS - src/smtpd/smtpd_check.c log_whatsup(): "%s: %s: %s from %s: %s;" + " from=<..>" + " to=<..>" + " proto=" + " helo=<..>", nothing after; smtpd_check_reject() builds the "%d %s ..." reply. A milter refusal at smtpd (smtpd.c check_milter_reply()) uses smtpd_whatsup(), which appends " sasl_username=<..>" only when authenticated - SASL is off on port 25 (smtp/inet/smtpd_sasl_auth_enable=no). namaddr is state->addr without "IPv6:" (smtpd.h SMTPD_BUILD_NAMADDRPORT, smtpd_peer.c). - comment at inbound.go:48.

A-15: DOES NOT HOLD - src/cleanup/cleanup_milter.c cleanup_milter_apply():
    if (state->recip) vstring_sprintf_append(state->temp1, " to=<%s>", ...state->recip);
  and src/cleanup/cleanup_addr.c cleanup_addr_recipient():
    state->recip = mystrdup(STR(clean_addr));	/* Used by Milter client */
  for every recipient, so to= is the LAST recipient (after rewriting), not the message's recipient. Consequences: (1) inbound journal: rspamd rejects a message to a@in1.example and b@in2.example at end-of-message - one row is written, for in2.example only; in1.example's journal never shows it. (2) From-refusal recovery (refusals.go): a refused message to three recipients that the panel did not record is recovered as one row with the last To; the page shows one recipient where the panel would have written three. - honest note (no blessing) at inbound.go:59 and refusals.go:52.
  Fix: no log line names all recipients (smtpd logs none per accepted RCPT), so the fix is to say so rather than to guess: (1) inbound journal - keep one row but derive the domain only from to= and add "(last recipient of the message)" to Detail when it came from cleanup; (2) recovered From-refusal - mark the recovered row (e.g. Reason detail "recovered from mail.log: last recipient only"). Tests: inbound_test with a cleanup verdict line carrying the last of two recipients asserts one row and the marked Detail; refusals_test asserts the recovered row carries the mark.

A-16: HOLDS - src/smtpd/smtpd.c mail_open_stream() connects to var_cleanup_service (cleanup_service_name, default "cleanup", postconf.proto), not overridden in postfix-config.sh; cleanup's own tag is mail_task("cleanup") with the default syslog_name. - comment at inbound.go:48.

A-17: HOLDS - src/qmgr/qmgr_message.c qmgr_message_read(): opened() (src/global/opened.c "%s: from=<%s>, size=%ld, nrcpt=%d") when it reads a committed queue file into the active queue (every time it re-enters, which is harmless here). - comment at inbound.go:78.

A-18: PARTLY - smtpd: smtpd_check_reject() logs "%d %s ..." (code first) via log_whatsup(); check_milter_reply() logs the milter's whole reply. cleanup: cleanup_milter_apply() logs text = resp + 4 (code dropped). Holds when the milter's reply has an enhanced status code. Fails: src/milter/milter8.c (SMFIR_REPLYCODE in milter8_event()) accepts a reply without one ("451 Try again later": buf[4] not a digit passes the check), and cleanup then logs "Try again later" - no leading 4, so the journal records a reject for what was a tempfail. Postfix's own default replies always carry one ("451 4.7.1 ..."). Whether rspamd 3.11 ever replies without an enhanced code is rspamd's source (Part B/C), not read here. - citation for the holding part at inbound.go:206.
  Fix: in inboundJournal.handle, for program=="cleanup" decide tempfail/reject only when the text starts with [45]\.\d; otherwise keep Decision reject but put "(reply class not logged)" in Detail. Tests: inbound_test milter-reject with "Try again later" text gets the marked detail; "4.7.1 ..." still tempfail.

A-19: HOLDS - postconf(5) info_log_address_format (default external: a local part with spaces or specials is quoted); src/global/info_log_addr_form.c info_log_addr_form_sender/recipient(). A quoted local part may contain "> to=<". - comment at inbound.go:155.

A-20: PARTLY - src/smtpd/smtpd.c check_milter_reply(): msg_info("%s: %s: %s from %s: %s;%s", queue_id ("NOQUEUE"), action, where, namaddr, reply, smtpd_whatsup()), and smtpd_whatsup() starts with " from=<..>" - the regexp holds. The quoted "as the stand writes it" line cannot be verbatim: smtpd_whatsup() appends " sasl_username=<%s>" whenever the session authenticated, and a journal-milter level-2 refusal is of an authenticated application; the fixtures of the same event (refusals_test.go, sendercheck_test.go pid 1149) do carry "sasl_username=<l2app>". Date of the quote not recorded. Consequence: none (the regexp is not anchored at the end). - comment at internal/logtail/refusals.go:22 (cites the source, says the quote lacks the field, "date not recorded").

A-21: DOES NOT HOLD - smtpd.c smtpd_whatsup():
    if (state->sasl_username) vstring_sprintf_append(buf, " sasl_username=<%s>", state->sasl_username);
  The line does carry the login on 465/587. Consequence: AttachRefusalLine matches by client address, sender and time only. Two applications behind one NAT address that may both send as billing@ (sender_login_maps "billing@x app1,app2" or a shared @domain) are refused at MAIL FROM in the same 30 s window: the line can go onto the other application's row, and that row's page shows a refusal that was not its own. - comment corrected at refusals.go:22 (no longer says "the line no login").
  Fix: capture `sasl_username=<([^>]*)>$` in refusalLineRe (optional group), pass it to AttachRefusalLine, and match app_login too when present. Tests: refusals_test with two waiting rows of the same IP and sender but different logins - the line goes to the right one; a line without sasl_username still matches as now.

A-22: HOLDS (MEASURED, now cited) - src/cleanup/cleanup_milter.c cleanup_milter_apply(): "%s: %s: %s from %s[%s]: %s;" with text = resp + 4 ("5.7.1 From: ..."), then " from=<..>", " to=<..>" (last recipient, A-15), " proto=", " helo=<..>"; the ";" is Postfix's. The quoted line is dated: e2e stand 2026-10-12 (sendercheck_test.go fixture). - comment at refusals.go:43 and :52.

A-23: DOES NOT HOLD - src/smtpd/smtpd.c mail_open_stream():
    msg_info("%s: client=%s%s%s%s%s%s%s%s%s%s%s", ..., ", sasl_method=", ..., ", sasl_username=", state->sasl_username, ", sasl_sender=", state->sasl_sender, ", orig_queue_id=", ..., ", orig_client=", ...)
  sasl_sender is set by src/smtpd/smtpd_sasl_proto.c smtpd_sasl_mail_opt() when the client sends MAIL FROM:<..> AUTH=<addr> (any non-"<>" value). Then `sasl_username=(\S+)` captures "app1," with the comma. Consequence: an app whose library sends AUTH= on MAIL FROM (several do) has a From-refusal the panel did not record recovered with AppLogin "app1," - a login no application has, so the recovered row is filed under no application. - honest note at refusals.go:61.
  Fix: `sasl_username=([^,\s]+)` (panel logins hold no comma or space - check the login validator in internal/app before relying on it; if one could, use `sasl_username=(.*?)(?:, sasl_sender=|, orig_queue_id=|, orig_client=|$)`). Tests: refusals_test with "client=..., sasl_method=PLAIN, sasl_username=app1, sasl_sender=a@x" recovers AppLogin "app1".

A-24: DOES NOT HOLD (harmless) - smtpd.c SMTPD_BUILD_NAMADDRPORT uses state->addr (smtpd_peer.c: rfc_addr is the one with "IPv6:"); cleanup's "[%s]" is MAIL_ATTR_ACT_CLIENT_ADDR, written from state->addr ("XXX Note: state->addr, not state->rfc_addr", smtpd.c mail_open_stream()). The prefix never appears in these lines, so strings.TrimPrefix(m[2], "IPv6:") at refusals.go handle() is a no-op. Consequence: none. - nothing written (would bless a false premise).
  Fix: delete the TrimPrefix (or keep it with a comment "never present: smtpd.c namaddr uses state->addr"). Tests: existing refusal tests; add an IPv6 client line "unknown[2001:db8::1]" that attaches with clientIP "2001:db8::1".

A-25: HOLDS (MEASURED, now cited) - src/milter/milter8.c milter8_connect(): msg_warn("connect to Milter service %s: %m", milter->m.name), then milter8_comm_error(): tempfail -> "451 4.7.1 Service unavailable - try again later"; smtpd connects to the milters per client after "connect from" (smtpd.c smtpd_service(): msg_info("connect from %s") then setup_milters() -> milter_create() -> milter8_create() -> milter8_connect()), and the CONNECT/EHLO events repeat the stored reply. Measured on the e2e stand 2026-10-12. - comment at internal/logtail/sendercheck.go:32.

A-26: HOLDS (MEASURED 2026-10-14, now cited) - milter8.c milter8_read_resp(): msg_warn("milter %s: can't read %s reply packet header: %m", ..., "SMFIC_HEADER"), then milter8_comm_error() closes the stream; cleanup gets the milters per message, so one warning ends one message. - comment at sendercheck.go:53.

A-27: HOLDS (MEASURED, now cited) - smtpd.c smtpd_service() msg_info("connect from %s"); src/master/single_server.c (one client at a time per process). - comment at sendercheck.go:32.

A-28: HOLDS (MEASURED, now cited) - src/smtpd/smtpd_sasl_proto.c smtpd_sasl_auth_cmd(): a 4xx from milter_other_event() becomes smtpd_chat_reply("454 4.3.0 Try again later"), no msg_info. - comment at sendercheck.go:32.

A-29: HOLDS - milter8.c milter8_event(): SMFIR_TEMPFAIL -> "451 4.7.1 Service unavailable - try again later"; milter8_comm_error() with tempfail -> the same text. The journal milter's level-2 refusal is RespTempFail (internal/milter/milter.go). So the reject lines are identical for all three. - comment at sendercheck.go:32. (The OpenDKIM fixture lines are now labelled composed.)

A-30: PARTLY - src/qmgr/qmgr_active.c qmgr_active_done_25_generic() (expiry checked when an attempt ends) and qmgr_active_done_3_generic() (delay capped at var_max_backoff_time) - holds. Left out: a deferred message is moved back to active only by the deferred-queue scan, every queue_run_delay (src/qmgr/qmgr.c qmgr_deferred_run_event timer; qmgr_active.c qmgr_active_feed() skips files whose mtime is in the future), plus the attempt's own duration. Consequence: countStuck can count as "past the lifetime" a row Postfix is still about to try, for up to queue_run_delay (300 s default) - a transient overcount in queued_over_lifetime. - citation + note at logtail.go:228.
  Fix: overLifetime := policy.LongestLifetime() + policy.MaximalBackoff + policy.QueueRunDelay (the field already exists in internal/postfix.RetryPolicy). Test: a reconcileLoop/countStuck unit test with a stub retryPolicy asserting the cutoff includes QueueRunDelay.

A-31: PARTLY - holds for normal delivery (every recipient ends in sent()/defer_append()/bounce_append() -> log_adhoc()). Exceptions read in source: postsuper -d (src/postsuper/postsuper.c: only "%s: removed"); cleanup drops a duplicate recipient silently (src/cleanup/cleanup_out_recipient.c been_here()); a milter's smfi_delrcpt; expiry (A-8 - rows already deferred). Concrete consequence of the duplicate case: an app sends RCPT TO:<a@x> twice; the journal milter writes two rows (internal/milter RcptTo appends, no dedupe; no unique key on send_log), Postfix delivers once, UpdateStatus updates only the newest row, the older stays queued, the sweep closes it as bounced and - since deliveryInLog finds the sent line - counts it as "missed", a false alarm about the tailer. - note at logtail.go:274.
  Fix: dedupe recipients case-insensitively in the journal milter's RcptTo (internal/milter, Part B's area), or have UpdateStatus update all rows of (queue_id, to) rather than the newest. Tests: milter test with a repeated RCPT writes one row; or store test that UpdateStatus resolves both duplicates.

A-32: PARTLY - src/showq/showq.c showq_service() scans maildrop, active, incoming, deferred, hold - holds. Entry format: src/postqueue/showq_compat.c showq_message(): id, then "*" (active) or "!" (hold), then "#" when forced_expire (postsuper -e), then size. internal/postfix/queue.go queueEntryRe `^([0-9A-Za-z]+)[*!]?\s+\d+\s` does not accept "#". Consequence: after `postsuper -e <id>`, until the message actually expires, it is missing from QueueIDs and its still-"queued" rows (message never attempted) can be closed by the sweep as bounced while Postfix holds it. Narrow. - citation (the five queues) at logtail.go:65; parser outside my area, not edited.
  Fix: internal/postfix/queue.go: `^([0-9A-Za-z]+)[*!]?#?\s+\d+\s`. Test: parseQueueIDs with "ABC123*#   446 Thu ..." and "ABC124#  ..." lines.

A-33: PARTLY - src/postlogd/postlogd.c pre_jail_init(): logwriter_open_or_die(var_maillog_file) once per process; post_jail_init() sets var_use_limit=0, but src/master/dgram_server.c dgram_server_timeout() still ends it after max_idle; README_FILES/MAILLOG_README "Reload Postfix so that postlogd(8) immediately closes the old logfile." So postlogd keeps writing to the renamed inode until reload (or an idle exit) - holds. What fails is the comment's "nothing in that gap is lost": logrotate renames, creates, and only then runs postrotate `postfix reload` (logrotate.c rotateSingleLog() then postrotateSingleLog()). If the tailer's tick falls between create and postlogd's exit, it drains the old inode once, switches, and whatever postlogd writes to the old inode afterwards is never read. Consequence: a delivery line in that sub-second window is lost; the row is closed by the sweep as bounced and counted "missed" (deliveryInLog finds it in mail.log.1). - citation + note at logtail.go:722.
  Fix: after a rotation keep the old descriptor and drain it on each tick until it has not grown for some seconds (e.g. 10 s, well past `postfix reload`) before closing it; record Places against the old file's fingerprint. Test: a follow test (unix-only, beside TestFollowTailsAndRotates) that renames the log, creates a new one, then appends a delivery line to the OLD file after the tailer has switched, and asserts UpdateStatus is called for it.

A-34: PARTLY - postconf(5) (3.7.11) smtpd_timeout is per read/write; smtpd_per_request_deadline "normal: no" bounds a request, not a message; nothing bounds the time from client= (first RCPT) to end-of-message. Holds for any client sending at a sane rate. Consequence when it fails: a message whose DATA phase (or the gap RCPT->DATA, NOOPs keep it alive) exceeds an hour, with a prune falling in between, loses its first stored lines (client=, cleanup's message-id) from its page - cosmetic. - note at internal/logtail/lines.go:58.
  Fix: raise lineGrace to 24 h (the cost is storage of lines without a row for a day), or prune only ids qmgr has "removed". Test: TestRetentionLoopPrunesLines/PruneSendLogLines cutoff with the new grace.

A-35: DOES NOT HOLD (comment only) - postconf(5) smtpd_client_recipient_rate_limit: "The maximal number of recipient addresses that any client is allowed to send to this service per time unit, regardless of whether or not Postfix actually accepts those recipients", default 0 (off); SelfPost does not set it. So a Postfix limit that counts them exists; it is off, and per client address only. Consequence: none - the prune-every-500 logic is right either way. - comment corrected at internal/logtail/inbound.go:97. Fix: done (comment); optionally set smtp/inet/smtpd_client_recipient_rate_limit in postfix-config.sh (Part C).

A-36: HOLDS - build/postfix-config.sh smtpd_milters (sender, OpenDKIM default_action=tempfail; journal accept); src/milter/milter8.c milter8_comm_error(): tempfail -> "451 4.7.1 Service unavailable - try again later", accept -> no reply; README_FILES/MILTER_README "Milter error handling". - comments at cmd/panel/sendercheck.go:24 and internal/health/socket.go:25.

A-37: PARTLY - effect holds (as A-36); "OpenDKIM runs with default_action=tempfail" misattributed - default_action is Postfix's per-milter setting (MILTER_README "Different settings for different Milter applications"). - comment corrected at internal/health/socket.go:25.

A-38: DOES NOT HOLD - unix(7): a socket bound to a filename "creates a socket in the filesystem that must be deleted by the caller when it is no longer needed (using unlink(2))"; a SIGKILLed or crashed process cannot unlink it, and connect() to it then fails with ECONNREFUSED. opendkim.c calls smfi_opensocket(FALSE) (no removal of an existing socket by OpenDKIM; libmilter's own cleanup is in the sendmail package, not read). CheckSocket says "Listening" from os.Stat alone. Consequence: OpenDKIM crashes, supervisord restarts it with backoff or gives up (FATAL): Health > sockets shows OpenDKIM "Listening / OK" while Postfix defers every submission with 451; only the Processes table tells. Same for a SIGKILLed panel (sender/journal sockets). - note (no blessing) at socket.go:31.
  Fix: for unix: sockets, dial (net.DialTimeout("unix", path, 1s)) and close, as checkMilter already does for inet:; ECONNREFUSED -> fail("Nothing is listening on the socket."), EACCES -> StatusUnknown "cannot be checked by the panel". Check before relying on it that the panel can connect to /run/opendkim/opendkim.sock (opendkim.conf UMask 007, directory opendkim:selfpost 2750 - the panel must be in group selfpost). A frozen process still accepts (kernel backlog) - that case stays with the sender-check deferral count. Tests: health_test/milter_test - a listener closed with SetUnlinkOnClose(false) leaves a socket file and must grade as missing; a live listener grades OK.

A-39: PARTLY - src/milter/milter8.c milter8_connect(): transports "inet", "unix" and "local" (LOCAL_CONNECT). "Postfix takes two forms" is false; but build/postfix-config.sh refuses anything but inet:/unix: for INBOUND_ANTISPAM_MILTER, so a local: address never reaches CheckMilter. - comment at internal/health/milter.go:21. Fix: treat "local:" like "unix:" in checkMilter (one case), test in milter_test.

A-40: HOLDS - src/global/pipe_command.c pipe_command(): a non-zero exit with no leading "D.S.N text" in the output and SYS_EXITS_CODE(75) -> sys_exits_detail(75) = "4.3.0" (src/global/sys_exits.c) -> PIPE_STAT_DEFER; src/pipe/pipe.c eval_command_status() -> defer_append(). The panel's output starts with the log prefix "dmarc-ingest: ", so dsn_valid() cannot take over. - comment at cmd/panel/main.go:39.

A-41: HOLDS - exit 0 -> PIPE_STAT_OK -> sent(), "delivered via %s service" (pipe.c eval_command_status()); 4.x -> defer_append() until maximal_queue_lifetime, then qmgr_active.c returns it to the sender; 5.x -> bounce_append() and a non-delivery notice (src/global/bounce.c, bounce daemon). - comment at cmd/panel/dmarc_ingest.go:38.

A-42: HOLDS - src/pipe/pipe.c eval_command_status(): " (%.100s)" of why->reason; src/global/pipe_command.c reads stdout+stderr (dup2 of both onto one pipe), up to VSTREAM_BUFSIZE, newlines/tabs to spaces, printable(). - comment at dmarc_ingest.go:56.

A-43: NEEDS A MEASUREMENT - Read: src/util/set_ugid.c set_ugid() does setgroups(1, &gid): pipe(8) runs the command with the user's primary group only, so the half "pipe(8) starts the command without the group" holds. Upstream conf/postfix-files:135 installs postlog "2755 $setgid_group", and postlogd(8) allows set-gid on "(Postfix >= 3.7) postlog(1)"; if /usr/sbin/postlog is set-gid postdrop in the image, postlog gets the group by itself and the claim "postlog is no use here" is false. Debian's packaging (debian/postfix.postinst) sets dpkg-statoverride only for postdrop and postqueue; whether the .deb ships postlog set-gid (debhelper's dh_fixperms is not in dev/upstream) cannot be read here. Measure on the stand: `docker exec <selfpost> stat -c '%a %U:%G' /usr/sbin/postlog /var/spool/postfix/public` and `docker exec -u panel <selfpost> /usr/sbin/postlog -t dmarc-ingest probe-$(date +%s)` then grep the probe in /data/log/mail.log. Consequence if postlog works: the reason could be logged in full instead of the 100-byte window. - citation for the set_ugid half + "not recorded" at dmarc_ingest.go:61.

A-44: HOLDS - src/pipe/pipe.c deliver_message(): pipe_command(... CA_PIPE_CMD_EXPORT(export_env->argv) ...) - no CA_PIPE_CMD_ENV; src/local/command.c sets "RECIPIENT" for local(8). - comment at dmarc_ingest.go:86.

A-45: HOLDS - postconf(1) (3.7.11 man/man1/postconf.1): "By default, the postconf(1) command displays the values of main.cf configuration parameters" (default when unset), -h "Show parameter ... values without the "name = " label", -x expands $name (not used; the six retry parameters default to literal values). - comment at cmd/panel/httpserver.go:103.

A-46: PARTLY - supervisor 4.2.5 supervisor/rpcinterface.py _interpretProcessInfo(): uptime = now - info['start'], the spawn of program "postfix" = build/postfix-wrapper.sh, which waits for the milter sockets (up to MILTER_WAIT_TIMEOUT, default 30 s) before `exec postfix start-fg`. So "uptime of postfix = time since Postfix started" is early by up to that wait. Consequence: after supervisorctl restart of panel and postfix together, senderCheckDowntime can count up to ~30 s during which Postfix was not running yet (the wrapper was waiting for the very sender socket). At container start the .alive mark is removed (entrypoint.sh), so no downtime is counted there. - citation + bound at cmd/panel/sendercheck.go:142.
  Fix (cheap): take Postfix's start from master's pid file mtime (`$queue_directory/pid/master.pid`, written by master at start) instead of the supervisord uptime, falling back to supervisord. Test: senderCheckDowntime with a stub ask() returning the later start.

A-47: PARTLY - OpenDKIM 2.11.0~beta2 opendkim/opendkim.c mlfi_eoh(): no SigningTable match (dkimf_apply_signtable() == 0) leaves no signing request, and with Mode s (build/opendkim.conf) "verify request with verify mode disabled" returns SMFIS_ACCEPT - unsigned, delivered: holds. Fails: dkimf_apply_signtable() < 0 ("signing table references unknown key", "error loading key") returns SMFIS_TEMPFAIL - a line written whose key the KeyTable lacks or that fails to load defers the message (notifications, and everything for that domain). - citation + caveat at cmd/panel/notifier.go:26.
  Fix: make sure internal/domain resync writes KeyTable and the key file before SigningTable (or both atomically by rename), and that EnsureServerSigned only adds the server's line once its key file exists and parses. Test (internal/domain, Part B): resync with a missing key file does not emit the SigningTable line.

A-48: PARTLY - Postfix 3.7.11 src/xsasl/xsasl_cyrus_server.c xsasl_cyrus_server_create(): sasl_server_new(service, var_myhostname, user_realm=NULL (smtpd_sasl_local_domain empty, smtpd_sasl_glue.c REALM_OR_NULL), ...); Cyrus SASL 2.1.28 plugins/sasldb.c sasldb_auxprop_lookup(): user_realm NULL -> serverFQDN; common/plugin_common.c _plug_parseuser(): no "@" -> realm = serverFQDN. myhostname = SELFPOST_HOSTNAME (postfix-config.sh). Holds with SASL_REALM unset. Fails: SASL_REALM set to anything else (it is read in main.go and documented in architecture.md) - the panel creates accounts in that realm, Postfix looks them up in myhostname's, and every login fails; and with SELFPOST_HOSTNAME unset the panel uses "localhost" while Postfix uses `hostname -f`. - citation at cmd/panel/main.go:209.
  Fix: drop the SASL_REALM override (realm := SELFPOST_HOSTNAME) or refuse to start when SASL_REALM != SELFPOST_HOSTNAME; setting smtpd_sasl_local_domain instead would change the authenticated name to login@realm and break sender_login_maps. Test: cmd/panel test that loadConfig with a differing SASL_REALM is rejected (or ignored); envdoc_test updated.

A-49: HOLDS - logrotate 3.21.0 logrotate.c prerotateSingleLog(): firstRotated = "%s/%s.%d%s%s" with logStart (default 1) and "" instead of compext under LOG_FLAG_DELAYCOMPRESS; logrotate.8 delaycompress. logrotate-loop.sh runs only /etc/logrotate.d/mail (no global dateext). - comments at internal/logtail/logtail.go:329 and offset.go:134.

A-50: HOLDS - logrotate.c rotateSingleLog(): without copytruncate, rename() then, with create, createOutputFile(); with copytruncate, copyTruncate(). - comment at logtail.go:592.

A-51: HOLDS - logrotate.c rotateSingleLog(): rename() and createOutputFile() in the same pass, no script between (postrotate is postrotateSingleLog(), after). - comment at logtail.go (blindness doc, "rename() and createOutputFile() follow each other").

A-52: HOLDS - msg_logger.c msg_logger_print(): every record starts "%b %d %H:%M:%S "; with the inode in the fingerprint two generations are told apart. - comment at internal/logtail/offset.go:21.

A-53: HOLDS for what the code relies on - rename(2) moves the file (same inode; "other hard links ... unaffected"), stat(2) st_ino. The device-number caution (may change across reboots on LVM/btrfs/NFS) is not a claim the code relies on - leaving st_dev out only removes a way to fail; not settled by a man page and not needed. - comment at internal/logtail/fileid_unix.go:12.

A-54: HOLDS - supervisor 4.2.5 supervisor/supervisorctl.py do_status(): exitstatus = LSBStatusExitStatuses.NOT_RUNNING (3) when any info state is in states.STOPPED_STATES (STOPPED, EXITED, FATAL, UNKNOWN); UNKNOWN (4) when upcheck fails or a name is unknown. - comment at internal/health/processes.go:42.

A-55: HOLDS - supervisor/states.py ProcessStates: STOPPED, STARTING, RUNNING, BACKOFF, STOPPING, EXITED, FATAL, UNKNOWN. - comment at processes.go:60.

A-56: HOLDS - supervisorctl.py _show_statuses(): template '%(namespec)-' + str(maxlen+3) + 's%(state)-10s%(desc)s', maxlen >= 30; state words <= 8 chars, so whitespace always separates; errors "%s: ERROR (no such process)". - comment at processes.go:74.

A-57: HOLDS - supervisor/rpcinterface.py _interpretProcessInfo(): 'pid %s, uptime %s' % (pid, timedelta); Python's str(timedelta) gives "H:MM:SS" or "N day(s), H:MM:SS". - comment at processes.go:100.

A-58: HOLDS - proc(5): /proc/stat "kernel/system statistics", /proc/meminfo statistics "on the system" (no namespace virtualises them; only an overlay like LXCFS would); network_namespaces(7): network namespaces isolate "the /proc/net directory". The code comment's "unless it was started with its own namespaces" was imprecise and is reworded. - comment at internal/health/machine.go:41.

A-59: HOLDS - proc(5) /proc/stat field order (user nice system idle iowait irq softirq steal guest guest_nice); Linux kernel/sched/cputime.c account_guest_time(): guest time is added to CPUTIME_USER (or CPUTIME_NICE) as well as CPUTIME_GUEST (torvalds/linux master read 2026-10-14; the host's kernel may be older, and older versions of the function were not read). fs/proc/stat.c prints one cpuN line per online logical CPU, so the code's "one line per core" comment was corrected to "logical CPU" (the value is used as the thread fallback, which is right). - comment at machine.go:412.

A-60: HOLDS - proc(5) /proc/meminfo MemAvailable (since 3.14) "An estimate of how much memory is available for starting new applications, without swapping"; Linux fs/proc/meminfo.c show_val_kb(): pages << (PAGE_SHIFT - 10), i.e. KiB. - comments at machine.go:282 and :539.

A-61: HOLDS - proc(5) /proc/net/dev: 8 receive columns (bytes first) then transmit (bytes first) -> fields[0], fields[8]; net/core/net-procfs.c dev_seq_printf_stats() "%6s: %7llu ..." (master); net/core/dev.c dev_valid_name() refuses ':' in a device name, so the first colon ends the name whatever the spacing. - comment at machine.go:577.

A-62: HOLDS - Linux arch/x86/kernel/cpu/proc.c show_cpuinfo() "processor\t: %u", show_cpuinfo_core() (CONFIG_SMP) "physical id\t: %d", "core id\t\t: %d"; proc(5) says /proc/cpuinfo items are architecture dependent - the fallback covers the rest. - comment at machine.go:441.

A-63: HOLDS - unix(7) (the socket file "must be deleted by the caller"; "connecting to a stream socket object requires write permission on that socket"), bind(2) EADDRINUSE, inode(7) set-group-ID directory: new files take the directory's group. - comments at cmd/panel/journal.go:59 and :73.

A-64: PARTLY - certbot/docs/using.rst "Renewing certificates": "As of Certbot 4.0.0, a certificate is considered ready for renewal when less than 1/3rd of its lifetime remains ... Prior to Certbot 4.0.0 the threshold was a fixed 30 days." Holds for 90-day certificates (30 days). With the shorter Let's Encrypt lifetimes (45-day certificates: renewal at 15 days left), the 14-day warning fires one day after the renewal point, not "comfortably past" it; with other ACME clients (Caddy, acme.sh, Traefik) the rule is theirs (Part C § 12). - comment rewritten at internal/health/cert.go:12.
  Fix: grade by the certificate's own lifetime: warn when DaysLeft < max(3, lifetime/6) where lifetime = NotAfter - NotBefore (90 d -> 15 d, 45 d -> 7.5 d), i.e. well after a client renewing at 1/3. Test: TestCheckCertificate with 90-day and 45-day certificates at various remaining days.

A-65: DOES NOT HOLD - build/postfix-config.sh copies TLS_CERT_FILE to /etc/postfix/tls-internal/fullchain.pem and sets smtpd_tls_cert_file to the copy; build/postfix-cert-reload.sh refreshes the copy and reloads only every TLS_RELOAD_INTERVAL_SECONDS (default 86400); each smtpd loads the certificate when it starts (Postfix 3.7.11 src/smtpd/smtpd.c pre_jail_init(), TLS_SERVER_INIT). CheckCertificate reads the mounted TLS_CERT_FILE. Consequence: the proxy renews a certificate 2 days before expiry; Health and the notifier read the new file ("valid for 89 days", OK) while Postfix serves the old copy for up to a day longer - and if the reload fails (cert-reload logs and retries), Postfix can serve an expired certificate while the panel shows OK. - note at cert.go:27 (no blessing).
  Fix: CheckCertificate(path) reads the file Postfix serves - `postconf -h smtpd_tls_cert_file` (readable 0644) - and, when the mounted file differs, adds "A renewed certificate is waiting; Postfix picks it up at the next refresh" (warn if the served one is within certWarnDays). Tests: health_test with a served file expiring in 2 days and a mounted one valid 89 days -> warn with that detail; identical files -> as today.

### Fixtures (Part A § 9)

- internal/logtail/sendercheck_test.go TestDeferredCounter: seen lines (smtpd 1183 first client, cleanup 1339 sender-check pair, cleanup 1195 From refusal) were already labelled with dates; the others (smtpd 1149, 1190, 1201, 1300, the second client of 1183, the OpenDKIM warning of cleanup 1339) are now labelled composed. Note: smtpd 1149's "sasl_username=<l2app>" agrees with smtpd_whatsup() - the refusals.go quote (no field) is the one that cannot be verbatim (A-20). The ISO stamps are not Postfix's format (A-12) - the label already says "the stamps are put in ISO form".
- internal/logtail/refusals_test.go: labelled composed (file-level comment); ISO stamps noted as not Postfix's. `postfix/inbound/smtpd[372]` with stranger.example composed.
- internal/logtail/logtail_test.go: file-level comment before TestParseDelivery says all lines are composed unless marked; names the shapes Postfix does not write: "to=..., status=expired" (A-8), ISO stamps incl. the zone-less one in the first case (A-12), opendkim lines in mail.log (build/opendkim.conf Syslog no). TestSplitTimestamp's live-relay line now says "Seen on the live relay, date not recorded"; the other cases composed. Not rewritten: the "RFC 3339, which maillog_file_format selects on Postfix 3.9 and up" case name repeats the false claim of A-12.
- internal/logtail/lines_test.go: labelled composed; notes "postfix/submission/smtpd" is never written by this image (no syslog_name on smtps/submission).
- internal/logtail/inbound_test.go: labelled composed; ISO stamps with microseconds noted as not Postfix's (A-12).
- internal/health/health_test.go: TestParseProcesses "Real supervisorctl output" now says where/when not recorded (columns and FATAL text match supervisorctl.py _show_statuses() and process.py spawnerr); TestLivenessFromParsedProcesses labelled composed; TestUptime labelled composed, "127:00:01" noted as not a form supervisor prints.
- internal/health/machine_test.go: labelled composed (proc(5) layouts).
- milter_test.go, cmd/panel/*_test.go, retention_test.go: no external program output.

### Counts

HOLDS 39 (A-1..5, 7, 10, 11, 14, 16, 17, 19, 22, 25..29, 36, 40..42, 44, 45, 49..63)
PARTLY 16 (A-6, 9, 13, 18, 20, 30, 31, 32, 33, 34, 37, 39, 46, 47, 48, 64)
DOES NOT HOLD 9 (A-8, 12, 15, 21, 23, 24, 35, 38, 65)
NEEDS A MEASUREMENT 1 (A-43)

### DOES NOT HOLD

- A-8  "to=..., status=expired" is never written; expiry is one qmgr "from=<..>, status=expired, returned to sender" line - expired messages' rows stay "deferred" for ever.
- A-12 maillog_file_format does not exist (3.7.11, nor the 3.12 snapshot); isoStampRe matches nothing Postfix writes; most fixtures use that format.
- A-15 cleanup's milter-reject to= is the last recipient only - inbound journal and recovered From-refusal rows miss the others.
- A-21 smtpd's milter-reject line does carry sasl_username=<login> - refusal lines are matched without it.
- A-23 sasl_username=(\S+) takes "login," when sasl_sender follows (client used AUTH= on MAIL FROM).
- A-24 "IPv6:" never appears in name[addr] (harmless no-op TrimPrefix).
- A-35 Postfix has smtpd_client_recipient_rate_limit (off by default, per client) - comment corrected, no behaviour issue.
- A-38 a socket file that exists is not a listening milter - Health shows a crashed OpenDKIM as "Listening".
- A-65 TLS_CERT_FILE is not what Postfix serves (it serves the tls-internal copy, refreshed daily) - Health can show OK while an older or expired certificate is served.

### NEEDS A MEASUREMENT

- A-43 Is /usr/sbin/postlog set-gid in the image, and can user panel log through it? On the stand: `docker exec <selfpost> stat -c '%a %U:%G' /usr/sbin/postlog /var/spool/postfix/public`; `docker exec -u panel <selfpost> /usr/sbin/postlog -t dmarc-ingest probe-<n>`, then grep the probe in /data/log/mail.log.

## Part B answers (and the Part C entries placed in internal/milter/inbound.go)

Read in: dev/upstream/postfix-3.7.11, cyrus-sasl2-2.1.28+dfsg, opendkim-2.11.0~beta2, supervisor-4.2.5 (Debian sources, patches applied); go-milter v0.4.1 in the module cache; rspamd 3.11.0 fetched from GitHub at tag 3.11.0 (src/plugins/lua/milter_headers.lua, lualib/lua_mime.lua, src/libserver/milter.c, src/libutil/addr.c, src/lua/lua_ip.c, conf/options.inc); Berkeley DB 5.3 Programmer's Reference "Dump output formats" (docs.oracle.com/cd/E17076_02/html/programmer_reference/dumpload_format.html, "Library Version 11.2.5.3").

The inventory's line numbers are approximate (some files are shorter than the numbers it gives); entries were matched by content.

Code comments were added only where the entry holds (or to the part that holds). Code that is about to be replaced by the dkim_mail_parse port (the From: parsing in sender.go: judge()'s unfold/ParseList/mentionedDomains/unquoted "cruder second look") got no comments; entries about it are answered here only (PM12, D4's second place, D5/D6 are fixture labels and were done because the files stay with the e2e).

### 1. go-milter v0.4.1

B-G1: HOLDS - go-milter v0.4.1 session.go Process() case CodeMacro (`m.macros = make(...)` per macro packet), server.go Serve() (NewMilter() per accepted connection); Postfix 3.7.11 src/milter/milter8.c milter8_event() sends a macro packet before every event, milter.c milter_macro_lookup() never returns null (so even an empty list resets the map) - citation added to the `session` doc comment in milter.go.

B-G2: HOLDS (MEASURED 2026-10-12, now also cited) - go-milter session.go HandleMilterCommands() (`if !resp.Continue() { m.backend = m.server.NewMilter() }`), response.go SimpleResponse/CustomResponse.Continue(); Postfix milter8.c milter8_conn_event() sends CONNECT only in state READY, once per milter connection. Confirmed the note: the sender check's 'y' CustomResponse keeps the session, its RespTempFail replaces it - citations added at session.MailFrom (milter.go) and senderSession.MailFrom (sender.go); the measurement sentence stays as it was (it has where and when).

B-G3: HOLDS - go-milter session.go case CodeEOB -> Body(), case CodeEOH -> Headers(); Postfix milter8.c milter8_eob() sends one SMFIC_BODYEOB per message - cited at session.Body, senderSession.Headers.

B-G4: HOLDS - go-milter session.go case CodeMacro `m.macros[data[i]] = data[i+1]`; Postfix milter.c milter_macro_lookup() `argv_add(argv, name, value)` with the name as milter_*_macros spells it (defaults in mail_params.h brace the long names, "i" bare; build sets `{client_addr}` braced) - cited at macro(). The test comment of TestBracedMacros ("{i} for some versions") corrected: Postfix sends the name as configured; macros there marked composed.

B-G5: DOES NOT HOLD - go-milter v0.4.1 session.go case CodeMail/CodeRcpt: `from := readCString(msg.Data); m.backend.MailFrom(strings.Trim(from, "<>"), ...)`; Postfix src/smtpd/smtpd.c mail_cmd()/rcpt_cmd() pass `milter_argv(state, argc - 2, argv + 2)` (the address token as the client wrote it, `<...>`, then each ESMTP parameter as a separate string; smtpd_token.c keeps `<"a b"@x>` as one token), milter8.c milter8_mail_event() sends them as MILTER8_DATA_ARGV. So cleanAddress always receives the address without brackets and without parameters: the `<` branch never runs, and the fallback `strings.IndexByte(a, ' ')` cuts `"a b"@example.net` to `"a`. No comment added.
  - Consequence: an application that sends to (or from) a quoted local part with a space gets a journal row whose To (From) is `"a`; the delivery line `to=<"a b"@example.net>` matches no row and the reconcile sweep closes the row as bounced. Also the parameter-stripping is dead code. Rare, but exactly the case the stage-1 fix meant to cover.
  - Fix: in cleanAddress drop the `IndexByte(' ')` cut (go-milter never passes parameters; it reads only the first argument) and keep the bracket loop only as a tolerance; comment the go-milter/Postfix forms with the citation above. Tests: address_test.go gains the forms go-milter really delivers (`"a b"@example.net` -> unchanged, `"a>b"@example.net` -> unchanged); a milter_test that drives a session through go-milter's own client (client.go in the module) with `MAIL FROM:<"a b"@example.net> SIZE=10` and checks the row.

B-G6: HOLDS - Postfix milter8.c milter8_message(): mime_state_alloc() passes milter8_body only without SMFIP_NOBODY, header/EOH callbacks unless SMFIP_NOHDRS/NOEOH, milter8_eob always; milter8_event() handles SMFIR_REPLYCODE without looking at rq_mask; milter8_connect() accepts rq_mask 0. go-milter session.go case CodeOptNeg returns (version 2, s.Actions, s.Protocol) - cited at Serve (milter.go); ServeInbound/ServeSender point there. Note: go-milter answers protocol version 2; Postfix (milter_protocol=6) then marks v3+ events as non-protocol (milter8_connect(), `milter->version < my_version`), which only removes UNKNOWN/DATA events these milters do not use.

B-G7: PARTLY - "go-milter has no connection-close hook" HOLDS (go-milter server.go type Milter has no close callback; session.go HandleMilterCommands() returns on CodeQuit/EOF without calling the backend) - cited at reservationTTL. "A client that drops after MAIL FROM produces neither EOM nor ABORT" DOES NOT HOLD: Postfix src/smtpd/smtpd.c smtpd_proto(), on SMTP_ERR_EOF ("lost connection") falls through to the cleanup `helo_reset(state); ... mail_reset(state); rcpt_reset(state); if (state->milters) milter_disc_event(...)`; mail_reset() calls `milter_abort(state->milters)` when SMTPD_FLAG_NEED_MILTER_ABORT is set (set in mail_cmd before milter_mail_event), and milter8.c milter8_abort() writes SMFIC_ABORT in state ENVELOPE/MESSAGE. So Abort() does arrive, then QUIT.
  - Consequence: none for behaviour - the TTL is still the right backstop for what really sends nothing: an smtpd killed mid-transaction, or Postfix closing the milter socket after an error/timeout (milter8_comm_error() shuts the socket down without ABORT). Only the comment's reason is wrong.
  - Fix: rewrite the reservationTTL comment: ABORT arrives on client disconnect (cite smtpd.c smtpd_proto()/mail_reset()); the TTL covers a killed smtpd and a milter socket Postfix abandons (milter8.c milter8_comm_error()). No test needed (comment only).

B-G8: HOLDS - go-milter server.go Serve(): `if s.closed { return ErrServerClosed }` after Close() closed the listener - cited at Serve.

B-G9: HOLDS - Postfix smtpd.c smtpd_proto() calls `milter_conn_event(state->milters, state->name, state->addr, ...)` (state->addr has no "IPv6:"; a V4-mapped address is rewritten to IPv4 in smtpd_peer.c); go-milter case CodeConn: net.ParseIP(address), nil for "unknown" or for family 'U'/'L' with no address - cited at Connect.

### 2. Postfix 3.7.11: milter protocol, macros, replies

B-PM1: HOLDS - build sets milter_mail_macros with {client_addr}; src/smtpd/smtpd_milter.c smtpd_milter_eval(): S8_MAC_CLIENT_ADDR -> state->rfc_addr; src/smtpd/smtpd_peer.c smtpd_peer_sockaddr_to_hostaddr(): rfc_addr = dotted quad, `concatenate(IPV6_COL, client_addr.buf)` for IPv6, V4-mapped rewritten to IPv4; smtpd_peer_no_client(): "unknown" - cited at the clientIP field and clientAddr().

B-PM2: PARTLY - holds: the login comes in the MAIL macros (build milter_mail_macros has {auth_authen}; mail_params.h DEF_MILT_MAIL_MACROS has it too) and is absent when not authenticated (smtpd_milter.c smtpd_milter_eval(): `IF_SASL_ENABLED(state->sasl_username)` returns 0, milter.c milter_macro_lookup() then adds nothing, so macro() gives "") - cited at session.MailFrom. Fails: "the bare login, no @realm" is not guaranteed. Postfix src/xsasl/xsasl_cyrus_server.c xsasl_cyrus_server_get_username() returns Cyrus SASL_USERNAME; with smtpd_sasl_local_domain empty (smtpd_sasl_glue.c REALM_OR_NULL -> no user_realm) Cyrus lib/canonusr.c _canonuser_internal() appends nothing, so the name is what the client typed; and plugins/sasldb.c sasldb_auxprop_lookup() -> common/plugin_common.c _plug_parseuser() accepts `login@<myhostname>` (splits at the '@', realm = the typed one). A client that authenticates as `shop@<SELFPOST_HOSTNAME>` is therefore authenticated and {auth_authen} = "shop@host".
  - Consequence: Postfix's reject_sender_login_mismatch compares "shop@host" with owner "shop" (strcasecmp_utf8) and refuses every non-null envelope sender (553 not owned by user). For MAIL FROM:<> Postfix skips that check (PM16); the journal milter then finds no application for "shop@host" (ApplicationByLogin not found -> client-address list and app rate limit fail open), but the sender check refuses the message ("not an address this application sends as", the login is not in sender_login_maps). No mail leaves; only the journal/limit bookkeeping sees an unknown login.
  - Fix (recommended, cheap): document it in session.MailFrom's comment with the Cyrus citation. Optional hardening (~10 lines + test): treat a {auth_authen} containing "@" as no application match that fails closed in authIPAllowed/overLimit. I recommend the comment only; the sender check already closes the door.

B-PM3: HOLDS - mail_params.h DEF_MILT_EOD_MACROS "i" (build does not override it); src/cleanup/cleanup_milter.c cleanup_milter_receive() installs cleanup_milter_eval(), which answers S8_MAC_I with state->queue_id; content events of smtpd_milters run in cleanup (cleanup_milter_inspect() -> milter_message()) - cited at session.Body and inboundSession.Body.

B-PM4: HOLDS - mail_params.h DEF_MILT_EOH_MACROS "i"; header and EOH events also carry eoh_macros (milter8.c milter8_header()/milter8_eoh()); cleanup_milter_eval() as PM3; content inspection runs after the whole message is in the queue file, so the id exists - cited at the comment in senderSession.Headers (merged under the existing prose; the prose's "{i}" is harmless since macro() reads both).

B-PM5: HOLDS - milter8.c milter8_event() `case SMFIR_CONTINUE: ... MILTER8_EVENT_BREAK(milter->def_reply)` (null unless a quarantine set "H"); milter.c milter_message() goes on to the next milter / returns null; cleanup accepts - cited at session.Body.

B-PM6: HOLDS - milter8.c milter8_event() SMFIR_TEMPFAIL (not connect/helo) -> "451 4.7.1 Service unavailable - try again later", no state change to REJECT_CON; smtpd.c mail_cmd(): `err = check_milter_reply(...)`, `mail_reset(state); smtpd_chat_reply(state, "%s", err); return (-1);` - the connection stays; the next MAIL FROM goes to the milters again - cited at session.MailFrom.

B-PM7: HOLDS - milter8.c milter8_comm_error(): default_action "accept" -> reply 0, "tempfail" -> "451 4.7.1 Service unavailable - try again later"; a hang ends in a read timeout (milter_command_timeout/content_timeout via vstream timeouts) and goes through milter8_comm_error(); a failed connect through milter8_comm_error() (FREE_TRANSPORT_AND_BAIL_OUT in milter8_connect()); build sets default_action per milter (postfix-config.sh smtpd_milters) - cited in the package comment (milter.go), the inboundSession comment and the sender check's package prose.

B-PM8: HOLDS - smtpd.c rcpt_cmd(): one milter_rcpt_event() per RCPT; a recipient smtpd refused goes only to milters with MILTER_FLAG_WANT_RCPT_REJ (SMFIP_RCPT_REJ, which these milters do not negotiate); milter.c milter_rcpt_event() stops at the first milter that refuses, so the journal (last in the chain) never sees a recipient an earlier milter refused - cited at session.RcptTo.

B-PM9: HOLDS (and more than it says) - smtpd.c rset_cmd() -> mail_reset() -> milter_abort() (milter8.c milter8_abort(): SMFIC_ABORT); mail_reset() also runs on every refusal in mail_cmd(), at the end of every transaction (common_post_message_handling()) and at session end (smtpd_proto()) - cited at session.Abort. (So Abort also follows a completed message; the code is idempotent there.)

B-PM10: HOLDS - milter8.c milter8_event() case SMFIR_REPLYCODE: validates `[45]dd[ -]` and an enhanced code starting with the same digit, halves "%%" and drops a single "%" (`if ((ch = *cp++) == '%') ch = *cp++;`); milter_message() returns it; cleanup_milter.c cleanup_milter_apply(): '5' -> CLEANUP_STAT_CONT + CLEANUP_MILTER_SET_SMTP_REPLY; smtpd.c common_post_message_handling() replies `STR(why)` when IS_SMTP_REJECT - cited at senderSession.Headers and printable().

B-PM11: HOLDS - cleanup_milter.c cleanup_milter_apply(): `"%s: %s: %s from %s[%s]: %s;"` then ` from=<%s>`, ` to=<%s>`, ` proto=`, ` helo=<...>`; the event string is "END-OF-MESSAGE" also for a refusal at end-of-headers (cleanup_milter_inspect()) - cited at printable(). (For the log tailer: the refusal line reads `<id>: milter-reject: END-OF-MESSAGE from host[addr]: 5.7.1 From: ...; from=<..> to=<..> proto=ESMTP helo=<..>`.)

B-PM12: PARTLY (answer only - unfold() belongs to the From: parsing being replaced) - holds: Postfix hands a folded header whole: src/global/mime_state.c joins a continuation line to the saved header with `vstring_strcat(state->output_buffer, "\n")` and milter8.c milter8_header() sends it as one SMFIC_HEADER ("Sendmail 8 sends multi-line headers as text separated by newline"). Fails: the line breaks are LF only, never CRLF or CR (smtpd strips CRLF into records); milter8_header() also eats one space after the colon unless SMFIP_HDR_LEADSPC (go-milter does not ask for it). unfold()'s CRLF/CR handling is harmless. For the dkim_mail_parse port: the value it gets has "\n" + the folding white space, and one leading space removed.

B-PM13: DOES NOT HOLD - src/global/is_header.c is_header_buf() returns the length of the name without the blanks before ':'; milter8.c milter8_header(): `cp = STR(buf) + (header_info ? strlen(header_info->name) : is_header(STR(buf))); while (*cp == ' ' || *cp == '\t') *cp++ = 0;` - the blanks are overwritten with NUL, so the name sent is never "From ". No comment added.
  - Consequence: none (the TrimSpace is a no-op); the comment states a Postfix behaviour that does not exist.
  - Fix: replace the comment by the citation ("Postfix strips the blanks before the colon: milter8.c milter8_header()") and drop or keep the TrimSpace (keeping it costs nothing). If the port rewrites senderSession.Header, do it there.

B-PM14: HOLDS (= C-P29) - milter.c milter_message(): `for (resp = 0, m = milters->milter_list; resp == 0 && m != 0; m = m->next) resp = m->message(m, fp, data_offset, ...)`; milter8.c milter8_message() seeks to data_offset and reads with rec_get() (record.h: REC_FLAG_DEFAULT includes REC_FLAG_FOLLOW_PTR, REC_FLAG_SKIP_DTXT); edits of an earlier milter are applied during its EOB reply (milter8_event() -> parent->add_header = cleanup_milter.c cleanup_add_header(), which writes the header behind the "header append" pointer record after the last header) - cited at inboundSession.Header.

B-PM15: HOLDS - milter.c milter_message() loop stops at the first non-null reply (reject, tempfail, discard "D", and also quarantine "H"); so a message the filter refuses is never shown to the journal - cited at the inboundSession comment. (Edge: a filter "quarantine" would also not reach the journal.)

B-PM16: HOLDS - src/smtpd/smtpd_check.c generic_checks(): REJECT_AUTH_SENDER_LOGIN_MISMATCH and REJECT_UNAUTH_... run only `if (state->sender && *state->sender)`; reject_sender_login_mismatch is defined as both (smtpd_check_init()) - cited at senderSession.domain().

B-PM17: HOLDS - build: smtps/submission `smtpd_client_restrictions=permit_sasl_authenticated,reject`; smtpd_check.c smtpd_check_rcpt(): with var_smtpd_delay_reject (DEF_SMTPD_DELAY_REJECT 1) the client restrictions run at RCPT and refuse every recipient; smtpd.c data_cmd(): "554 5.5.1 Error: no valid recipients"; port 25 overrides smtpd_milters; non_smtpd_milters has only OpenDKIM - cited in judge() under the existing prose.

### 3. sender_login_maps and texthash

B-PS1: DOES NOT HOLD - src/smtpd/smtpd_check.c reject_auth_sender_login_mismatch(): `owners = check_mail_addr_find(state, sender, smtpd_sender_login_maps, STR(reply->recipient), 0)` then the login is compared with those owners only; src/global/mail_addr_find.c mail_addr_find_opt(): each further key is tried only `if (result == 0 && path->error == 0 ...)` - the first key found (full address, address without extension, local part if the domain is local, "@domain") decides. No comment added.
  - Consequence: with `billing@example.org billing` and `@example.org shop` (a list-mode and a wildcard-mode application on one domain - both modes exist in the store), Postfix refuses shop the envelope sender billing@example.org, but Bindings.Allowed("shop", "billing@example.org") is true (the "@example.org" key lists shop), so shop may put From: billing@example.org on mail it sends with its own envelope - and OpenDKIM signs it for example.org. Same for invoices+x@example.org when invoices@ is listed.
  - Fix: in Bindings, keep "key exists" (the parsed table already has it) and in Allowed take the owners of the first key present in this order: full lower-cased address, address without extension (if different), "@domain"; check the login against that set only. The local-part-only keys (MA_FIND_LOCALPART_IF_LOCAL) need no modelling: the panel never writes such keys (cite that). Tests in sender_test.go: table `billing@example.org billing` + `@example.org shop`: shop refused for billing@ and for billing+x@, allowed for other@example.org; billing allowed for billing@ and refused for other@example.org. Then the package prose "by one lookup" becomes true and can be cited.

B-PS2: HOLDS - smtpd_check.c reject_auth_sender_login_mismatch(): `strcasecmp_utf8(state->sasl_username, name)` - cited in the sender check's package prose (the prose already says the check compares as written; that stays correct because sasldb2 authentication is byte-exact - see PS3).

B-PS3: HOLDS - same citation, added at foldLogin (validate.go). Note: Cyrus sasldb keys are compared byte for byte (sasldb/db_berkeley.c, the Berkeley DB key), so a client can only authenticate with the login's exact spelling; folding at creation keeps Postfix's case-blind comparison and the panel's view in agreement.

B-PS4: PARTLY - holds for the file the panel writes: src/util/dict_thash.c dict_thash_open() splits the key at the first unquoted blank and trims the value; src/util/readlline.c readllines() drops a line whose first non-blank is '#'; owners are split by smtpd_check.c `mystrtok(&cp, CHARS_COMMA_SP)` - cited (key/comment/folding parts) in Bindings.fresh. Fails: (1) readllines() joins a line that starts with white space to the previous one (fresh() reads it as its own line); (2) Postfix splits owners on comma AND blank, fresh() joins blank-separated owners ("a b" -> "ab").
  - Consequence: none today - renderSenderLoginMaps never writes continuation lines or blanks inside the value (assertMapSafe forbids them).
  - Fix (cheap, recommended): split the value with strings.FieldsFunc on ',' and white space, and append a line that starts with white space to the previous logical line. Test: fresh() over "a@x one two\n b@y three\n  ,four" gives a@x -> {one,two}, b@y -> {three,four}.

B-PS5: HOLDS - smtpd_check.c opens smtpd_sender_login_maps with DICT_FLAG_FOLD_FIX (maps_create(..., DICT_FLAG_LOCK | DICT_FLAG_FOLD_FIX | DICT_FLAG_UTF8_REQUEST)); src/util/dict_ht.c folds the key on update and on lookup (`lowercase()`); mail_addr_find is queried with the external (quoted) form first, then the internal one (MA_FIND_FORM_LEGACY) - cited in Bindings.fresh. (Go's ToLower is Unicode, Postfix's lowercase() ASCII; immaterial for the whitelisted addresses.)

B-PS6: HOLDS - src/global/strip_addr.c strip_addr_internal() cuts at the last '@' and calls split_addr(); split_addr.c split_addr_internal(): `strcspn(localpart, delimiter_set)` - first delimiter; no split when it is the first character (`len == 0`); the special local parts (postmaster, MAILER-DAEMON, double-bounce, owner-/-request with '-') cannot contain '+' anyway - cited at withoutExtension.

B-PS7: HOLDS - smtpd_check.c reject_auth_sender_login_mismatch() `mystrtok(&cp, CHARS_COMMA_SP)` - cited at RebuildSenderLoginMaps.

B-PS8: PARTLY - holds: blank (dict_thash_open() key ends at the first unquoted blank), newline (readllines()), comma (owner separator), backslash (dict_thash_open() treats '\\' as an escape in the key scan) - cited at the forbidden-character constants. Fails: the colon "a lookup key cannot carry" has no basis - dict_thash_open() only warns when a key ENDS with ':' ("is this an alias file?"). Not listed though meaningful: '"' (toggles quoting in dict_thash_open(); an unbalanced one makes the line ignored) and a leading '#' (comment).
  - Consequence: none (addresses and logins are whitelisted upstream; the colon ban is merely over-strict).
  - Fix: correct the comment (drop the colon reason; keeping the colon forbidden is harmless) and add '"' and '#' to both forbidden sets as defence in depth. Test in postfix_test.go: assertMapSafe refuses `a"b@x` and `#a@x`.

B-PS9: HOLDS - README_FILES/DATABASE_README: "If you change a regexp:, pcre:, cidr: or texthash: file then Postfix may not pick up the file changes immediately... reads the entire file into memory once"; conf/postfix-script reload: `kill -HUP` of the master; src/master/master.c on SIGHUP calls master_refresh(), and master_conf.c master_config(): "Make the current generation of child processes commit suicide whenever it is convenient. The next generation of child processes will run with the new configuration settings." That is also the source of the audit's "measured, not understood": a busy smtpd keeps the old table until it has served its client - cited at reloadViaSupervisor (postfix.go).

B-PS10: HOLDS - conf/postfix-script start-fg: `case $$ in 1) exec $daemon_directory/master -i ;; *) $daemon_directory/master -s;; esac` - under supervisord the script's shell (postfix(1) execvp()s the script) is the supervised PID and master is its child; src/master/master.c puts it in its own session (`setsid()` unless init mode/debug/-d). A SIGHUP to the supervised PID hits the shell - cited at reloadViaSupervisor.

### 4. postqueue, postconf, retry parameters

B-PQ1: DOES NOT HOLD - src/postqueue/showq_compat.c showq_message(): `if (active) strcat("*") else if (hold) strcat("!"); if (forced_expire) vstring_strcat(id_status, "#");` - ids may end in "#", "*#" or "!#" (after `postsuper -e`/`-f`). queueEntryRe `^([0-9A-Za-z]+)[*!]?\s+\d+\s` misses them. No comment added.
  - Consequence: QueueIDs() omits a force-expired message that is still queued; the reconcile sweep can close its row as bounced before Postfix has bounced it (it will bounce it at its next delivery attempt, "status=expired, returned to sender", so the outcome is usually right but early and without the reason).
  - Fix: `^([0-9A-Za-z]+)[*!]?#?\s+\d+\s` and list "#" in the comment with the citation. Tests: TestParseQueueIDs entries `ABC123#`, `ABC124*#`, `ABC125!#`.

B-PQ2: DOES NOT HOLD (fixture comment) - showq_compat.c showq_message(): `if ((padding = 76 - (int) strlen(show_reason)) < 0) padding = 0; vstream_printf("%*s(%s)\n", padding, "", show_reason);` - a reason reaches the left margin only when 76 characters or longer. The fixture is composed. Fixed (comment, not data): queue_test.go now says the listing is composed and why the reason is put at the margin (the hard case). Also labelled TestFakePostqueue's entries as composed.

B-PQ3: HOLDS - src/showq/showq.c showq_service(): queue_info = maildrop, active, incoming, deferred, hold - cited at QueueIDs.

B-PQ4: HOLDS - src/postqueue/postqueue.c show_queue(): users other than root/mail_owner pass check_user_acl_byuid(VAR_SHOWQ_ACL) with DEF_SHOWQ_ACL static:anyone (build does not set authorized_mailq_users); failures are msg_fatal_status() (non-zero exit; msg_vstream_init() writes to stderr); showq_compat.c "Mail queue is empty" - cited at listQueue.

B-PQ5: HOLDS - src/postqueue/showq_json.c format_json(): one `{...}\n` per message with "queue_name", "queue_id", "arrival_time" (%ld epoch), "message_size", "forced_expire", "sender", "recipients" [{"address", "delay_reason"}]; json_quote() escapes - cited at QueuedMessage.

B-PQ6: HOLDS - src/global/mail_queue.h names; showq.c showq_service() scans exactly those five - cited at QueuedMessage.

B-PQ7: HOLDS (fixture relabelled) - shape matches showq_json.c format_json(); the fixture is composed (its first line in format_json()'s field order, the rest broken on purpose) - label added in queuejson_test.go.

B-PQ8: HOLDS - src/global/mail_params.h DEF_QUEUE_RUN_DELAY "300s", DEF_MIN_BACKOFF_TIME DEF_QUEUE_RUN_DELAY, DEF_MAX_BACKOFF_TIME "4000s", DEF_MAX_QUEUE_TIME "5d", DEF_DSN_QUEUE_TIME "5d", DEF_DELAY_WARN_TIME "0h" - cited at DefaultRetryPolicy.

B-PQ9: HOLDS - src/postconf/postconf_main.c pcf_show_parameters(): named parameters in argument order; pcf_print_parameter() with PCF_HIDE_NAME prints `"%s\n"`, the value unexpanded unless -x; unknown names only warn - cited at postconfRetryValues. (A main.cf value written as "$other_param" would come back unexpanded and fail ParseDuration -> defaults with the muted note; acceptable.)

B-PQ10: DOES NOT HOLD - src/global/conv_time.c conv_time(): `switch (*end ? *end : def_unit)`; src/global/mail_conf_time.c get_mail_conf_time() takes def_unit from the default value (get_def_time_unit(name, defval): the unit letter of "5d" -> 'd', of "0h" -> 'h', of "300s"/"4000s" -> 's'). `postconf -h` prints the value as written. No comment added.
  - Consequence: `maximal_queue_lifetime = 3` (3 days for Postfix) reads as 3 seconds: the Mail queue card shows "3 seconds" and LongestLifetime() (which bounds store.SendLogLines) becomes 3s, so a row's later log lines fall outside the window. Same for bounce_queue_lifetime (d) and delay_warning_time (h). Only when the owner edits main.cf with bare numbers (build/postfix-config.sh sets none of the six).
  - Fix: ParseDuration(s, defUnit byte); parseRetryPolicy passes 's','s','s','d','d','h' in retryConfKeys order (cite mail_conf_time.c get_def_time_unit and the DEF_* values). Tests: parseRetryPolicy with "3" for maximal_queue_lifetime -> 72h, "4" for delay_warning_time -> 4h, "300" for queue_run_delay -> 300s.

B-PQ11: DOES NOT HOLD - conv_time.c conv_time(): one strtol() and one unit character; the trailing-text check is commented out (`/* || (*end != 0 && end[1] != 0) */`), so "1h7m" is 1 hour; only lower-case w/d/h/m/s are units (an upper-case letter is an invalid value, a fatal error for Postfix). duration_test.go:21 pins the panel's different reading. No comment added.
  - Consequence: a main.cf value "1h7m" shows as 1h7m in the panel while Postfix runs 1h; "5D" parses in the panel but stops Postfix.
  - Fix: mirror conv_time(): read one number, take the next character as the unit (default per PQ10), ignore the rest; accept only lower-case units. Tests: {"1h7m", 1h}, {"5D", error}, {"10", per-key default}; update the comment with the citation.

B-PQ12: HOLDS - src/qmgr/qmgr_active.c qmgr_active_done(): expiry at `message->create_time + (*message->sender ? var_max_queue_time : var_dsn_queue_time)` - any null-sender message, whoever submitted it; qmgr.c caps var_dsn_queue_time at var_max_queue_time (so the max() is always max_queue_time, harmlessly) - cited at LongestLifetime.

B-PQ13: HOLDS (fixture relabelled) - the values are legal postconf -h output (it prints main.cf's text); stock prints "0h" for delay_warning_time (mail_params.h DEF_DELAY_WARN_TIME). Label added in retry_test.go ("composed by hand").

### 5. Inbound relay and DMARC maps

B-PI1: HOLDS - man/man5/postconf.5 relay_domains ("a type:table lookup table is matched when a (parent) domain appears as lookup key"), relay_recipient_maps ("used as lists... Specify @domain as a wild-card"); mail_addr_find.c MA_FIND_AT_DOMAIN (exact domain, no PDMS in MA_FIND_DEFAULT); man/man5/transport.5 "domain transport:nexthop" - cited at renderInboundMaps. Note: relay_domains also matches subdomains (parent_domain_matches_subdomains default lists relay_domains), but relay_recipient_maps then refuses them (neither the address list nor "@domain" matches a subdomain) and transport has no ".domain" entry.

B-PI2: HOLDS - src/smtp/smtp_connect.c: `lookup_mx = (... && *dest != '[')`; src/util/host_port.c host_port(): "[host]:port" and "[ipv6:addr]:port", a bare IPv6 inside brackets accepted (`split_at(cp, ']')`, then valid_hostaddr() -> valid_ipv6_hostaddr()) - cited at inboundNexthop. The inventory's open question: a bare `[2001:db8::1]` is accepted; "ipv6:" is optional.

B-PI3: HOLDS - README_FILES/TLS_README: "the policy table lookup key is the verbatim next-hop specification from the recipient domain, transport(5) table..."; src/smtp/smtp_tls_policy.c tls_policy_lookup() ("Only one lookup with [nexthop]:port..."); src/tls/tls_level.c names "none", "may", "encrypt" - cited at renderInboundMaps.

B-PI4: HOLDS - man/man5/access.5 "OK  Accept the address etc. that matches the pattern"; transport.5 "user@domain transport:nexthop" and "A non-null transport field with a null nexthop field resets the nexthop information to the recipient domain" (pipe(8) ignores the nexthop); relay_domains as PI1 - cited at renderDMARCMaps.

### 6. Cyrus SASL 2.1.28, sasldb2, db_dump

B-S1: HOLDS - utils/saslpasswd.c main(): `getopt(argc, argv, "vpcdnf:u:a:h?")` (glibc getopt ends options at "--"); -p sets flag_pipe (and stdin not a tty sets it anyway), read_password() does one read() without prompt; -c -> SASL_SET_CREATE, which lib/server.c sasl_setpass() checks only against SASL_SET_DISABLE - nothing refuses an existing user; sasldb/db_berkeley.c _sasldb_putdata() `mbdb->put(..., 0)` overwrites - cited in Set().

B-S2: HOLDS (the inventory's WRONG? is refuted) - plugins/sasldb.c sasldb_auxprop_store(): `if (value == NULL && ret == SASL_NOUSER) { /* Deleting something which is not there is not an error */ ret = SASL_OK; }` - the SASL_NOUSER that db_berkeley.c _sasldb_putdata() returns for DB_NOTFOUND becomes OK; the SCRAM plugin's setpass (plugins/scram.c scram_setpass(), the only setpass among libsasl2-modules' plugins) deletes through the same store; lib/server.c sasl_setpass() returns SASL_OK; utils/saslpasswd.c main() exits 0 - cited at Delete(). (A measurement on the stand would confirm it cheaply - `saslpasswd2 -d -f /tmp/x -u h -- nobody; echo $?` - but the source is unambiguous.)

B-S3: HOLDS - sasldb/allockey.c _sasldb_alloc_key(): key_len = len(user)+len(realm)+len(prop)+2, no trailing NUL; plugins/sasldb.c sasldb_auxprop_store() stores `value ? strlen(value) : 0` bytes; lib/server.c sasl_setpass() stores SASL_AUX_PASSWORD_PROP ("userPassword", include/sasl.h) in plaintext unless SASL_SET_NOPLAIN (-n, which Set does not pass) - cited at parseSASLSecret.

B-S4: DOES NOT HOLD - lib/server.c sasl_setpass(): `if ((flags & SASL_SET_DISABLE || !(flags & SASL_SET_NOPLAIN)) && ...) { ... prop_set(..., SASL_AUX_PASSWORD_PROP, pass, passlen) ... sasl_auxprop_store(...) }` - without -n the password is kept in plaintext as userPassword (SCRAM's setpass adds derived secrets besides). password.go's "sasldb2 keeps only the hashed form" is false and contradicts Secret(). No comment added.
  - Consequence: the security reasoning is wrong: sasldb2 (and every backup that carries it) holds usable application passwords; they must be treated as secrets of the same weight as the passwords themselves.
  - Fix: password.go comment: "sasldb2 keeps it in plaintext (userPassword, Cyrus 2.1.28 lib/server.c sasl_setpass() without SASL_SET_NOPLAIN) - that is what Secret() exports"; check security.md / architecture.md for the same claim (not in my files). No test (comment).

B-S5: HOLDS - Postfix src/smtpd/smtpd_sasl_glue.c `user_realm = REALM_OR_NULL(var_smtpd_sasl_realm)` (empty smtpd_sasl_local_domain -> null), src/xsasl/xsasl_cyrus_server.c xsasl_cyrus_server_create(): `SASL_SERVER_NEW(args->service, var_myhostname, user_realm ...)`; Cyrus plugins/sasldb.c sasldb_auxprop_lookup(): `user_realm = sparams->user_realm ? ... : sparams->serverFQDN` -> common/plugin_common.c _plug_parseuser() takes that realm for a login without '@'; build sets myhostname = SELFPOST_HOSTNAME - cited at NewSASLDB.

B-S6: HOLDS - common/plugin_common.c _plug_parseuser(): `r = strchr(input, '@')` - user and realm split at the first '@', both in saslpasswd2's store (sasldb_auxprop_store()) and the lookup - cited at validateLogin.

B-S7: DOES NOT HOLD - utils/saslpasswd.c read_password(): `n_read = read(STDIN_FILENO, buf, PW_BUF_SIZE)` (PW_BUF_SIZE 2048), then `if (0 < n_read && buf[n_read - 1] == '\n') n_read--;` - an embedded newline is kept in the password; only one trailing newline is dropped; more than 2048 bytes would be cut. No comment added.
  - Consequence: none for behaviour (control characters stay refused, 1024 < 2048); the stated reason is false (also repeated in service_test.go:319).
  - Fix: validate.go comment: "saslpasswd2 reads the password with one read() of at most 2048 bytes and drops one trailing newline (utils/saslpasswd.c read_password()): a trailing newline would be lost, a longer password cut; control characters are refused as no client could type them"; same correction in service_test.go:319's comment. No new test.

B-S8: HOLDS - Berkeley DB 5.3 Programmer's Reference "Dump output formats": "VERSION=N", name=value header pairs, "HEADER=END"; for Btree/Hash "paired lines of text where the first line of the pair is the key item, and the second line ... its corresponding data item... Each of these lines is preceded by a single space"; without -p "paired hexadecimal values"; "DATA=END". Debian's sasldb2 is Berkeley DB: cyrus-sasl2 m4/sasldb.m4 (--with-dblib default auto_detect, berkeley first) with libdb in Build-Depends, debian/rules recording BDB_VERSION into berkeley_db.txt; build/Dockerfile installs db-util - cited at parseSASLSecret. (A file with several databases repeats the header; sasldb2 has one.)

B-S9: PARTLY (fixture relabelled) - the data section matches the format above; the header does not ("the same shape" was too strong: a real dump carries more name=value lines before HEADER=END, which the parser skips). Comment corrected in sasl_test.go: composed by hand in the documented shape.

### 7. OpenDKIM 2.11.0~beta2

B-D1: HOLDS - KeyTable has no type prefix, so a file: table: opendkim/opendkim-db.c dkimf_db_open() case DKIMF_DB_TYPE_FILE (key up to the first blank, '#' comment); opendkim/opendkim.c dkimf_add_signrequest() reads domain, selector, key data from the value through dkimf_db_get() with three fields, split at ':' (opendkim-db.c dkimf_db_datasplit()); `keydata[0] == '/'` -> a file - cited in renderTables.

B-D2: HOLDS - opendkim-db.c dkimf_db_open() case DKIMF_DB_TYPE_REFILE: pattern up to the first blank, '#'/newline end the line, the rest trimmed is the data (key name); opendkim/util.c dkimf_mkregexp() - cited in renderTables and Signers.Signs.

B-D3: HOLDS - opendkim/util.c dkimf_mkregexp(): `dst[0] = '^'`, '*' -> ".*", '.' -> "\.", `*q++ = '$'`; opendkim.c opens SigningTable with DKIMF_DB_FLAG_ICASE (-> REG_ICASE); dkimf_apply_signtable() matches "user@domain" against it. So "*@d" matches d in any case and no subdomain (the '@' is literal before d) - cited at Signers.Signs and OpenDKIM.signs. (Signing itself further needs originok etc. - irrelevant to "which names have a key".)

B-D4: HOLDS - opendkim/opendkim.c mlfi_eoh(): SenderHeaders unset -> `from = dkimf_findheader(dfc, "from", 0)` (the first From:), then `dkim_mail_parse(...)` (libopendkim/dkim-mailparse.c) - cited in the sender check's package prose (sender.go 23-27, which stays); not at sender.go 318-323 (the "cruder second look", to be replaced by the port).

B-D5: HOLDS (MEASURED) - the e2e re-measures on every run; the lines are composed - label added to testdata/from-lines-passed.txt ("composed by hand; what OpenDKIM signs each with is measured, by the e2e on every run").

B-D6: HOLDS (label) - same, label added to testdata/from-lines-other.txt.

B-D7: HOLDS - opendkim/opendkim.c dkimf_reloader() / dkimf_sighandler(): SIGUSR1 sets `reload = TRUE` only `if (conffile != NULL)` (build passes -x); dkimf_config_reload(), called from mlfi_negotiate() at the next milter connection, reloads the configuration and reopens the tables; opendkim.8 OPERATION ("Upon receiving SIGUSR1, if the filter was started with a configuration file, it will be re-read") - cited at reloadViaSupervisor (internal/domain). Note: the reload is lazy - it takes effect at the next milter connection, not at the signal.

B-D8: HOLDS - libopendkim/dkim.c dkim_privkey_load(): key data starting "-----" goes to OpenSSL PEM_read_bio_PrivateKey() (Debian builds against libssl-dev), which reads the traditional "RSA PRIVATE KEY" form; opendkim.c dkimf_loadkey() -> dkimf_securefile() may call a group-readable file insecure, and dkimf_add_signrequest() then only logs it unless RequireSafeKeys (build: no) - cited at writePrivateKeyPEM. (That the opendkim user can read 0640 through the selfpost group is deployment, measured by the e2e signing.)

B-D9: DOES NOT HOLD - opendkim/opendkim-genkey.in: `$hashout = ""; if (defined($hashalgs)) { $hashout = " h=$hashalgs;"; }` and `"v=DKIM1;" . $noteout . $hashout . " k=rsa; " . $flags` - by default genkey writes `v=DKIM1; k=rsa; p=...`, no h=. No comment added.
  - Consequence: none functional (h=sha256 only restricts verifiers to sha256, which OpenDKIM signs with by default); the comment's "mirrors" is false.
  - Fix: comment: "the form opendkim-genkey writes (v=DKIM1; k=rsa; p=...) plus h=sha256, which genkey adds only with --hash-algorithms"; do NOT change the value (published records match it). No test.

B-D10: HOLDS - opendkim-db.c dkimf_db_datasplit() splits the KeyTable value at ':'; the domain and selector are also path components of the key file (SelfPost's path.Join and opendkim's `keydata[0] == '/'`) - cited at opendkimForbidden.

B-D11: DOES NOT HOLD (labels) - senderMaps is not in renderSenderLoginMaps' sorted order; signingTable's key-name column (`selfpost._domainkey.<d>`) is not renderTables' (`<d>`). Comments corrected in sender_test.go (composed by hand, in the format but not the order / key names; the check reads only the pattern). Data unchanged.

### 8. supervisord 4.2.5

B-U1: HOLDS - supervisor/rpcinterface.py signalProcess() -> process.py signal(): `options.kill(self.pid, sig)`; process.py get_execv_args()/_spawn_as_child(): shlex.split + execve, no shell; command `/usr/sbin/opendkim -f -x ...` (-f: no fork) so the pid is opendkim's - cited at reloadViaSupervisor (internal/domain).

B-U2: PARTLY - holds: supervisord (PID 1, root) spawns `/usr/sbin/postfix reload` with no user= (build/supervisord.conf). Fails: "the start call reports its exit": supervisor/rpcinterface.py startProcess(): after spawn() and reap(), `process.transition()` moves STARTING -> RUNNING as soon as `now - self.laststart > self.config.startsecs` (startsecs=0), and then `return True` - the call returns while `postfix reload` is still running; its exit status is never reported. Two side cases: if transition() runs in the same clock tick, the deferred onwait() later sees EXITED and raises ABNORMAL_TERMINATION for a successful reload; and internal/supervisor.Start maps "already started" to success, so a rebuild whose reload is asked while the previous reload process still runs is not reloaded by a second HUP. No comment about exit added.
  - Consequence: a failing `postfix reload` (master not running, postsuper error) is invisible: RebuildSenderLoginMaps / the panel's reload button report success. Impact is limited because smtpd and cleanup are short-lived and reread texthash anyway (PS9), but long-running trivial-rewrite/qmgr keep stale tables.
  - Fix (internal/supervisor, not my file, + postfix.go): a RunOnce(program) that starts the program, then polls `supervisorctl status <program>` (or XML-RPC getProcessInfo) until EXITED/FATAL/BACKOFF and fails unless EXITED with exitstatus 0; on "already started", wait for that run to finish and start again. Test with a fake supervisorctl (the pattern queue_test.go uses for postqueue): exit 0 -> nil, exit 1 -> error, already-started -> second start.

### 9. rspamd sidecar

B-R1: PARTLY - holds when the routines run: rspamd 3.11.0 src/plugins/lua/milter_headers.lua routines x-spamd-result and x-rspamd-action set `remove[header] = 0` and add with order -1; lualib/lua_mime.lua modify_headers() sends them in the milter reply; src/libserver/milter.c rspamd_milter_remove_header_safe() with nhdr == 0 sends CHGHEADER (delete) for every instance, rspamd_milter_extract_single_header() with order -1 sends ADDHEADER (Postfix appends after the last header, PM14) - cited at inboundSession.Header. Fails: both routines start with `if skip_wanted(...) then return end`; skip_wanted() is true for `settings.skip_local` (default true) when `task:get_ip():is_local()` - src/lua/lua_ip.c lua_ip_is_local(): loopback, IPv6 link/site-local, or local_addrs (conf/options.inc: 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12, fd00::/8, 169.254.0.0/16, fe80::/10) - and for skip_authenticated when task:get_user() is set. deploy/antispam/rspamd/milter_headers.conf sets neither. The custom selfpost-signature routine is called without skip_wanted() (custom_routines branch), so it still signs.
  - Consequence: mail from a client rspamd counts as local (a LAN host, or every client when Docker's userland proxy makes port-25 connections appear from the bridge gateway 172.x) keeps the sender's own X-Spamd-Result / X-Rspamd-Action, and rspamd still adds a valid X-SelfPost-Filter signature: the journal shows the sender's forged verdict as rspamd's (and "quarantine" if he wrote it). Journal display only - delivery is unaffected.
  - Fix (deploy/, not my file): in milter_headers.conf set `skip_local = false;` and `skip_authenticated = false;` (port 25 has no SASL, but it costs nothing). To confirm on a stand: run rspamd 3.11 + Postfix, send from 127.0.0.1/172.x a message carrying `X-Spamd-Result: default: False [-50.00 / 15.00]`, check the journal shows rspamd's score, not -50. Also worth measuring once: which client address Postfix sees on the stand's port 25 (Docker NAT vs userland proxy).

B-R2: HOLDS (= C-R13) - milter_headers.lua routine x-spamd-result: `string.format('default: %s [%.2f / %.2f]', (action == 'reject') and 'True' or 'False', score, required)`, symbols `string.format('%s(%.2f)[%s]', s.name, s.score, table.concat(s.options or {}, ','))`, joined `'; '`, folded at ';' (lua_util.fold_header) - cited at the rspamdScore/rspamdSymbol regexps. Fixture inbound_test.go:37 relabelled: composed by hand in that format, its symbol order not rspamd's.

B-R3: DOES NOT HOLD (= C-R14) - milter_headers.lua x-spamd-result: `sort_by = 'score'` (default) -> `res = math.abs(s1.score) > math.abs(s2.score)` ("inverse order to show important symbols first"): sorted by absolute weight, so an allow entry's -100.00 comes FIRST, not last. No comment added to inbound.go.
  - Consequence: none - rspamdVerdict keeps SELFPOST_ symbols wherever they are; the comment's reason is wrong (and with sort_by = 'name' the order would be alphabetical, which the code also survives).
  - Fix: inbound.go comment: "rspamd 3.11 sorts symbols by absolute weight (milter_headers.lua, sort_by = 'score'), which puts -100 first; the list symbols are kept wherever they stand, so the order does not matter". The test's comment (inbound_test.go TestRspamdVerdictKeepsTheListSymbols) is corrected already (fixture claim; the -100-last order is now described as the composed hard case).

B-R4: HOLDS (MEASURED, dev trial in a throwaway container, 2026-10-12; = C-R9) - the measurement stays a measurement (inbound_test.go TestFilterSignatureIsWhatTheFilterWrites says where and when); a pointer to it added at FilterSignature.

### Part C entries placed in internal/milter/inbound.go

C-P29: HOLDS - as B-PM14 (milter.c milter_message(), milter8.c milter8_message() rec_get following PTR, cleanup_milter.c cleanup_add_header()); with B-R1's caveat for "the last one read is its own" - cited at inboundSession.Header.

C-P40: HOLDS - as B-PM3 (mail_params.h DEF_MILT_EOD_MACROS "i"; cleanup_milter.c cleanup_milter_receive() installs cleanup_milter_eval(), S8_MAC_I -> state->queue_id) - cited at inboundSession.Body. Related C-R10 (place milter_headers.conf, not edited): HOLDS - rspamd src/libserver/milter.c takes the queue id from macro "{i}", else "i", for the request it sends to the scanner.

C-R9: HOLDS (MEASURED 2026-10-12) - as B-R4.

C-R13: HOLDS - as B-R2; fixture relabelled.

C-R14: DOES NOT HOLD - as B-R3.

C-R16: HOLDS (MEASURED 2026-10-12, now cited) - as B-G2 (go-milter HandleMilterCommands(); milter8_conn_event() once per milter connection) - the citation is at session.MailFrom, which inboundSession's clientIP comment points to.

C-R17: HOLDS - as B-G3/B-G6: with OptNoBody Postfix still sends EOB (milter8_eob()), go-milter calls Body(); Actions 0 and no Modifier calls - nothing is modified (Postfix milter8_connect() accepts rq_mask 0) - ServeInbound points to the citation at Serve.

### Counts

Part B (79): HOLDS 59, PARTLY 8, DOES NOT HOLD 12, NEEDS A MEASUREMENT 0.
Part C inbound.go (7): HOLDS 6, DOES NOT HOLD 1.

### DOES NOT HOLD

- B-G5 cleanAddress: go-milter passes the address unbracketed and without parameters; the blank cut breaks quoted local parts. Fix: drop the cut; test through go-milter's form.
- B-PM13 sender Header trim: Postfix already strips blanks before ':'. Fix: comment (trim harmless).
- B-PS1 Bindings.Allowed: Postfix uses the first key found, not any. Fix: first-key-wins lookup; tests with list+wildcard on one domain.
- B-PQ1 queueEntryRe: misses the forced-expire "#" flag. Fix: `[*!]?#?`; tests.
- B-PQ2 queue_test fixture comment: reason lines are right-aligned to column 76. Fixed (comment).
- B-PQ10 ParseDuration: default unit is per parameter (d, h), not s. Fix: per-key default unit; tests.
- B-PQ11 ParseDuration: Postfix reads one number and one unit; "1h7m" is 1h. Fix: mirror conv_time; tests.
- B-S4 password.go: sasldb2 keeps the plaintext userPassword. Fix: comment (and check security.md).
- B-S7 validate.go: saslpasswd2 keeps embedded newlines, drops one trailing one, reads at most 2048 bytes. Fix: comment (+ service_test.go:319).
- B-D9 dkimRecord: genkey writes no h= by default. Fix: comment only, value unchanged.
- B-D11 sender_test fixtures: not internal/postfix's order nor renderTables' key names. Fixed (comments).
- B-R3 / C-R14 rspamd symbol order: sorted by absolute weight, -100 first. Fix: comment in inbound.go (test comment fixed).

PARTLY (failing part): B-G7 (ABORT does arrive on disconnect - comment), B-PM2 (login may carry @realm - comment), B-PM12 (LF only - for the port), B-PS4 (continuation lines, blank-separated owners - small parser fix), B-PS8 (colon claim; '"' and '#' missing - comment + two characters), B-S9 (fixture header - fixed), B-U2 (startProcess does not report the reload's exit - RunOnce in internal/supervisor), B-R1 (skip_local/skip_authenticated skip the header routines but not the signature - deploy config).

### NEEDS A MEASUREMENT

None: every entry was settled in source or documentation. Optional confirmations on the stand: B-S2 (`saslpasswd2 -d` on a missing login exits 0) and, for B-R1, which client address Postfix sees on the stand's port 25 and that a forged X-Spamd-Result from a local address survives today.

## Part C - answers (2026-10-14)

Sources: dev/upstream (postfix 3.7.11-0+deb12u1, opendkim 2.11.0~beta2, cyrus-sasl2 2.1.28+dfsg, supervisor 4.2.5, logrotate 3.21.0, Debian patches applied); rspamd 3.11.1 source (github rspamd/rspamd tag 3.11.1) + rspamd-docker tag v3.11.1+3 + docs.rspamd.com (unversioned, read 2026-10-14); modernc.org/sqlite v1.60.1 and go-milter v0.4.1 from the module cache; Go stdlib (local go1.27 GOROOT); CoreDNS v1.14.6 source; Traefik v3.1.0 source; caddy/certmagic master (the `caddy:2` tag floats); docs.docker.com, eff-certbot.readthedocs.io, nginx.org, docs.openssl.org/3.0, sqlite.org, RFC 7489/5321/1035/6376.

"Comment written" = citation added in the file at the place that relies on it. Files outside my list (internal/milter/inbound.go, inbound_test.go, cmd/panel/*) were not touched.

### 1. Postfix 3.7.11

C-P1: HOLDS (short form) - master.5 "-o name=value (short form)" NOTE 2; postconf_edit.c pcf_edit_master() "-Pe does not accept whitespace in parameter value"; smtpd_check.c smtpd_check_parse() and milter.c milter_new() split on CHARS_COMMA_SP. The long form `-o { name = value }` (NOTE 1) would keep whitespace; now said in the comment - comment written, postfix-config.sh master_cf_set_smtp_inet_o.
C-P2: HOLDS - master.5 SYNTAX (continuation lines); postconf_edit.c pcf_edit_master() + pcf_gobble_cf_line() re-parse a matched entry with its continuation lines and print it whole (so the awk-inserted -o survive the later -F) - comment written.
C-P3: HOLDS - xsasl_cyrus_server.c xsasl_cyrus_server_create() SASL_SERVER_NEW(service, var_myhostname, user_realm or NULL); smtpd_sasl_glue.c smtpd_sasl_activate() REALM_OR_NULL; Cyrus plugins/sasldb.c sasldb_auxprop_lookup(): no user realm -> serverFQDN; common/plugin_common.c _plug_parseuser() - comments written (HOSTNAME_VALUE and SASL block).
C-P4: DOES NOT HOLD - see detail below. No edit.
C-P5: HOLDS - smtpd.c pre_jail_init() only warns when TLS init fails; smtpd_proto() "Wrapper-mode request dropped from %s for service %s. TLS context initialization failed"; each new smtpd re-runs pre_jail_init() - comment written.
C-P6: HOLDS - mail_params.h DEF_MAILLOG_FILE_PFXS "/var, /dev/stdout"; maillog_client.c maillog_client_init(): msg_fatal "%s value '%s' does not match any prefix in %s" (postfix.c calls it, so `postfix check` dies) - comment written.
C-P7: HOLDS - MAILLOG_README "Configuring logging to file"; postfix-script check-fatal: "missing 'postlog' service in master.cf" - citation completed.
C-P8: PARTLY - relayhost= is direct MX delivery (holds). "Nothing is ever delivered locally" fails: resolve_local.c resolve_local() still treats an address literal of one of the container's own interfaces as local ("Compare the destination against the list of interface addresses that we are supposed to listen on"; inet_interfaces=all) - see detail. No edit.
C-P9: HOLDS - smtpd_sasl_proto.c smtpd_sasl_auth_cmd(): "504 5.5.4 Encryption required for requested authentication mechanism" while auth_only and no TLS; smtpd.c pre_jail_init(): wrappermode -> enforce_tls -> auth_only - comment written.
C-P10: HOLDS (Debian-specific) - debian/patches/07_sasl_config.diff: xsasl_getconfpath() returns "$config_directory/sasl:/usr/lib/sasl" (xsasl_cyrus_log.c), SASL_CB_GETCONFPATH in xsasl_cyrus_server.c; debian/README.Debian "SASL configuration goes in /etc/postfix/sasl" - comment written at the smtpd.conf heredoc.
C-P11: PARTLY - the order holds but is smtpd's, not texthash's: smtpd_check.c reject_auth_sender_login_mismatch() -> check_mail_addr_find() -> mail_addr_find.c mail_addr_find_opt(MA_FIND_DEFAULT): full, without extension, local part if local, @domain. Comment corrected and cited (no behaviour change).
C-P12: HOLDS - mail_addr_find_opt() strip_addr_internal(int_full_key, &saved_ext, var_rcpt_delim) - comment written.
C-P13: HOLDS - DATABASE_README "texthash" (no postmap; read once per process; short-running processes need no reload) - comment written.
C-P14: HOLDS - mail_params.h DEF_SMTPD_DELAY_REJECT 1; smtpd_check.c smtpd_check_rcpt() - comments written (postfix-config.sh, negative_test.go).
C-P15: HOLDS - smtpd.c mail_cmd(): anvil check skips namadr_list_match(hogger_list) = smtpd_client_event_limit_exceptions (default $mynetworks; Debian debconf default debian/templates postfix/mynetworks "127.0.0.0/8 [::ffff:127.0.0.0]/104 [::1]/128"); anvil_rate_time_unit is global - the unstated parts now stated, comment written.
C-P16: HOLDS - MILTER_README "Different settings for different Milter applications" (3.0+) and "Signing internally-generated bounce messages"; milter.c milter_*_event()/milter_message() loop "resp == 0 && m != 0"; mail_params.h DEF_INT_FILT_CLASSES INT_FILT_CLASS_NONE - comment written.
C-P17: HOLDS - mail_params.h DEF_MILT_MAIL_MACROS; `i` empty at MAIL (DEF_SMTPD_DELAY_OPEN 1) is harmless - comment written.
C-P18: HOLDS - mail_params.h DEF_MILT_CONN_TIME/CMD_TIME "30s", MSG_TIME "300s" - comment written.
C-P19: HOLDS - mail_params.h DEF_KNOWN_TCP_PORTS; find_inet.c find_inet_port() -> filter_known_tcp_port() - comment written.
C-P20: HOLDS - debian/patches/40_chroot_by_default.diff (chroot y); debian/configure-instance.sh copies etc/resolv.conf into the chroot (not run in the container); chroot(2) needs CAP_SYS_CHROOT, dropped by cap_drop: ALL - comment written (postfix-config.sh, deploy/docker-compose.yml).
C-P21: HOLDS - smtpd.c pre_jail_init() (encrypt -> enforce -> auth_only); postconf_edit.c pcf_edit_master() -MX drops the entry - comment written.
C-P22: HOLDS - 40_chroot_by_default.diff: "smtp inet n - y - - smtpd" uncommented - comment written.
C-P23: PARTLY - holds: pipe.c get_service_attr() "missing user= command-line attribute"; these are pipe(8) attributes. Inexact: "postconf -P refuses them" - postconf_master.c pcf_edit_master_param() only ever writes "-o name=value"; "unmatched service_name/type" is postconf_edit.c's warning for a service not in master.cf. The measurement (1.7.0-1.9.5) stays, now "date not recorded" - comment extended (postfix-config.sh, port25_config_check.go).
C-P24: HOLDS - pipe.8 ${recipient}; dmarc-ingest_destination_recipient_limit=1 - comment written.
C-P25: HOLDS - pipe.8 export_environment; mail_params.h DEF_EXPORT_ENVIRON "TZ MAIL_CONFIG LANG"; clean_env.c. Unstated part (main.cf-global) now stated - comment written.
C-P26: HOLDS - master.5 NOTE 2 "or use commas instead of spaces" - comment written.
C-P27: HOLDS - smtpd_check.c smtpd_check_rcpt(): check_recipient_rcpt_maps() unless status == REJECT - comment written.
C-P28: HOLDS - master.5 "-o: Override the named main.cf configuration parameter"; smtpd.c milter_send() hands smtpd's list to cleanup, cleanup_milter.c cleanup_milter_receive() - comment written.
C-P29: HOLDS - milter.c milter_message() list order; milter8.c milter8_event() applies SMFIR_ADDHEADER at once via cleanup; next milter8_message() reads with rec_get() (record.h REC_FLAG_DEFAULT follows PTR) - comment written in postfix-config.sh (inbound.go not touched).
C-P30: HOLDS - mail_task.c mail_task(): "%s/%s" syslog_name/program - comment written.
C-P31: DOES NOT HOLD - see detail. No edit.
C-P32: HOLDS - postfix-script "check": check-fatal || exit 1; check-fatal runs post-install create-missing - comment written.
C-P33: HOLDS - postfix.c execvp(postfix-script); postfix-script start-fg: `1) exec master -i ... *) master -s`; master.c setsid() - comment written (postfix-wrapper.sh, supervisord.conf postfix-reload).
C-P34: DOES NOT HOLD - see detail. No edit.
C-P35: HOLDS - conf/postfix-files ($queue_directory root 755, maildrop 730/public 710 $setgid_group, pid root 755); postfix-script check-warn "not owned by root" - comment written.
C-P36: HOLDS - logwriter.c logwriter_open_or_die() safe_open(..., 0644) under master.c umask(077) -> 0600 - comment written (entrypoint.sh, logrotate-loop.sh).
C-P37: HOLDS - postfix-script "logrotate" (mv, kill -HUP); master_conf.c master_config() -> master_restart_service() -> master_status_cleanup(); dgram_server.c dgram_server_abort() on MASTER_STATUS_FD; postlogd.c logwriter_open_or_die() on the path - comment written (logrotate-loop.sh).
C-P38: HOLDS - smtpd.c pre_jail_init() per process; multi_server.c multi_server_abort() on MASTER_STATUS_FD after reload; postconf -h reads main.cf - comment written (postfix-cert-reload.sh).
C-P39: HOLDS - inet_proto.c inet_proto_init() keeps IPv6 when socket(PF_INET6) works; smtp_connect.c smtp_connect_inet() loops on to the next address after a failed connect. The listener side ([::] bind with the container's IPv6 disabled) is shown by every e2e run, not read - comment written.
C-P40: HOLDS - mail_params.h DEF_MILT_EOD_MACROS "i" - place is inbound.go (not mine): no edit; same fact cited in milter_headers.conf.
C-P41: HOLDS - README_FILES/TLS_README "TLS policy table" note: the lookup key is the verbatim next-hop; ":smtp", ":25", no port are different keys - comment written at smtp_tls_policy_maps.
C-P42: DOES NOT HOLD - see detail. No edit.

### 2. Cyrus SASL 2.1.28

C-S1: HOLDS - doc/legacy/options.html (pwcheck_method, auxprop_plugin, sasldb_path, mech_list) - comment written.
C-S2: HOLDS - rests on P9 (holds).
C-S3: DOES NOT HOLD - see detail. No edit.
C-S4: HOLDS - cyrus-sasl2 debian/control (libsasl2-2 Depends libsasl2-modules-db), sasl2-bin.install (usr/sbin), libsasl2-modules.install (libplain, liblogin), libsasl2-modules-db.install (libsasldb); packages.debian.org/bookworm/amd64/db-util/filelist (/usr/bin/db_dump) - comment written in Dockerfile.

### 3. OpenDKIM 2.11.0~beta2

C-O1: HOLDS - opendkim.8.in "Upon receiving SIGUSR1 ... re-read"; opendkim.c dkimf_config_reload() -> dkimf_config_load() -> dkimf_db_open() for SigningTable and KeyTable - comment written.
C-O2: HOLDS - opendkim.conf.5.in SigningTable: refile keys are wildcard patterns against the From: address, in order - comment written.
C-O3: HOLDS - opendkim.c dkimf_checkfsnode(): key must be owned by root or the executing uid ("is not owned by the executing uid"), group-readable only in a single-user group ("which has multiple users"). Note the man page (RequireSafeKeys) is looser than the code; the code decides - comment written.
C-O4: HOLDS - opendkim.conf.5.in UMask (applies to the socket); inode(7) setgid directory - comment written.
C-O5: HOLDS - opendkim-db.c dkimf_db_open() (file/refile read line by line; empty file = empty table) - comment written.
C-O6: HOLDS - opendkim.8.in -f, -x; opendkim.conf.5.in UserID, Background - comment written.

### 4. supervisor 4.2.5

C-V1: HOLDS - docs/configuration.rst [program:x] priority "the start and shutdown ordering"; supervisord.py runforever() transitions all groups in one pass - comment written.
C-V2: HOLDS - docs/configuration.rst [unix_http_server] chmod/chown; username "Default: No username required" - comment written.
C-V3: HOLDS - options.py drop_privileges(): os.setgroups() from grp.getgrall() - comment written.
C-V4: HOLDS - process.py Subprocess.finish(): startsecs=0 -> too_quickly never true -> EXITED, never BACKOFF/FATAL - comment written.
C-V5: DOES NOT HOLD - see detail. No edit.
C-V6: DOES NOT HOLD - see detail. No edit.
C-V7: HOLDS - docs/events.rst "Event Listener Notification Protocol" (READY\n, header line, len, "RESULT 2\nOK") - comment written in crashexit.py.
C-V8: PARTLY - holds: the listener is supervisord's child; SIGTERM shuts supervisord down (docs/running.rst "Signals"); it is PID 1. Fails: Docker restarts the same container, it does not recreate it (docs.docker.com/engine/containers/start-containers-automatically/, "Use a restart policy": "Docker restarts the container if it exits"). Mechanism cited in crashexit.py; the "recreate" wording left (fix: change "recreate it cleanly" to "restart it" in supervisord.conf and crashexit.py).
C-V9: HOLDS - process.py finish(): killing -> STOPPED; give_up(): FATAL only from BACKOFF - comment written (supervisord.conf, sender_check_test.go).
C-V10: DOES NOT HOLD - see detail. No edit.
C-V11: PARTLY - supervisorctl.py do_status(): exit 3 (LSBStatusExitStatuses.NOT_RUNNING) only for states in STOPPED_STATES (STOPPED, EXITED, FATAL, UNKNOWN), not for STARTING/BACKOFF/STOPPING; output "%(namespec)-Ns%(state)-10s%(desc)s" so fields[0], fields[1] parse holds. Harmless (exit ignored). Fix: comment in process_check.go: "exits 3 when a program is STOPPED, EXITED, FATAL or UNKNOWN". No edit.

### 5. logrotate 3.21.0

C-L1: HOLDS - debian/rules --with-state-file-path=/var/lib/logrotate/status; unstated part: logrotate.c newState() records an unknown log as rotated today, so after a recreate the first rotation comes a day late - comment written.
C-L2: HOLDS - config.c readConfigFile(): as root, mode & 0022 -> "Ignoring ... writable by group or others." return 0 (exit 0); owner not root ignored the same way; "Potentially dangerous mode" (mode & 07533 != 0400) is only a warning - the loop's choice to treat it as fatal is now said - comment written.
C-L3: HOLDS - logrotate.c findNeedRotating(): "parent directory has insecure permissions" - comment written (entrypoint.sh).
C-L4: HOLDS - the measurement ("confirmed on a live container") kept, now "date not recorded", and the source added (P36) - comment written.
C-L5: HOLDS - logrotate.8 create, delaycompress - comment written.
C-L6: HOLDS - postfix-script reload/status: master -t, exit 1 while not running - comment written (delivery_log_test.go).

### 6. Debian packaging and base image

C-D1: DOES NOT HOLD - see detail. No edit.
C-D2: HOLDS - postfix debian/templates postfix/main_mailer_type, postfix/mailname - comment written.
C-D3: HOLDS - supervisor debian/control Depends ${python3:Depends} - comment written.
C-D4: HOLDS - packages.debian.org/bookworm/ca-certificates "Depends: openssl (>= 1.1.1)" - comment written.
C-D5: HOLDS - groupadd(8) refuses a --gid in use (the build is the check) - comment written.
C-D6: HOLDS - conf/postfix-files postdrop/postqueue root:$setgid_group 2755; Linux no_new_privs.rst - comment written.

### 7. Linux, Docker, Compose

C-K1: HOLDS - capabilities(7) "Capabilities list"; kill(2) for CAP_KILL; SETUID/SETGID also used by pipe(8) user=panel; SYS_CHROOT dropped - comment written (deploy/docker-compose.yml).
C-K2: HOLDS - no_new_privs.rst (execve only) - comment written.
C-K3: HOLDS - inode(7) "The set-group-ID bit" - comment written (entrypoint.sh, opendkim.conf).
C-K4: HOLDS - test(1) -S - comment written.
C-K5: NEEDS A MEASUREMENT - Docker's docs (engine/network/port-publishing, packet-filtering-firewalls) do not say what source address the container sees when docker-proxy relays (IPv6 host address to IPv4 container, hairpin, rootless). Measure on the live host: connect to 25 and 465 from an outside host over IPv4 and over IPv6 and read "connect from ...[ADDR]" in mail.log; record `docker info` (userland-proxy, ip6tables). Stakes: P15 anvil per-IP, the inbound journal, and R7 (a 172.16/12 gateway address is "local" to rspamd).
C-K6: HOLDS - docs.docker.com/reference/compose-file/merge/ "!override" ("fully replace an attribute"). No minimum version is documented or claimed in the tree - comment written (compose.override.yml).
C-K7: HOLDS - docs.docker.com/compose/how-tos/multiple-compose-files/merge/ "Paths are evaluated relative to the base file" - comment written (caddy, nginx, traefik fragments).
C-K8: DOES NOT HOLD (comment only) - see detail.
C-K9: DOES NOT HOLD (comment only) - see detail.
C-K10: HOLDS as a measurement - Docker docs do not describe it; kept, "seen on the stand, date not recorded" - comment written (smtp_client.go, port25_client.go).
C-K11: HOLDS - Go src/os/tempfile.go MkdirTemp "created with mode 0o700"; testing.TempDir uses os.MkdirTemp - comment written (entrypoint.sh, hostname_gate_test.go).

### 8. rspamd 3.11 (rspamd/rspamd:3.11 = 3.11.1 at rspamd-docker v3.11.1+3)

C-R1: HOLDS - map.c ev_stat watcher; conf/options.inc map_watch_interval 5min x map_file_watch_multiplier 0.1 (30 s poll without inotify); docs.rspamd.com/configuration/maps asks for atomic rename. The delay is now stated - comment written (multimap.conf, lists.go).
C-R2: HOLDS - map_helpers.c rspamd_parse_kv_list(): '#' comment, whitespace ends key - comment written (lists.go).
C-R3: HOLDS - docs.rspamd.com/configuration/maps (IP maps: v4/v6, CIDR, mixed) - comment written (lists.go).
C-R4: PARTLY - holds: hash map lookup is whole-key, case-insensitive (map_helpers.c rspamd_icase_hash/rspamd_lc_cmp); email:domain = domain part (multimap.lua apply_addr_filter()), no subdomains. Fails (unstated): multimap.lua `from` default algorithm uses the SMTP sender, and the MIME From: only when the envelope sender is absent (task:has_from('smtp') else mime) - while the journal's From is the envelope only. Also: a `from` rule without filter matches the map against addr, domain and user (match_addr()); harmless because SelfPost's from-maps hold full addresses only. Holding part cited in multimap.conf. Fix in detail.
C-R5: PARTLY - "deny wins" holds: multimap.lua calls task:set_pre_result(rule.action, message) and registers the symbol with 'passthrough' augmentation; a pre-result action stands whatever the score. Fails: `prefilter = true` is not read by 3.11.1's multimap.lua (no reference) - docs.rspamd.com/modules/multimap: prefilter "not used since Rspamd 3.3"; the comment's "(a prefilter)" mechanism is wrong. Harmless. Holding part cited in multimap.conf. Fix: drop the three `prefilter = true;` lines and the "(a prefilter)" wording.
C-R6: HOLDS - protocol.c rspamd_scan_result_ucl() puts the pre-result message into messages.smtp_message; milter.c rspamd_milter_send_task_results() replies "554 5.7.1 <message>"; milter_headers.lua x-spamd-result writes "%s(%.2f)[%s]" - comment written (multimap.conf, lists.go). Caveat: the allow symbol is visible only when x-spamd-result is written (R7).
C-R7: PARTLY (security-relevant) - see detail. No citation written for the removal claim.
C-R8: HOLDS - milter_headers.lua: pcall(custom_routines[n], task, common) -> ok, err, to_add, to_remove, common_in; remove value 0 = all (docs.rspamd.com/modules/milter_headers) - comment written.
C-R9: HOLDS - lua_cryptobox.c rspamd_lua_hash_create(): "sha256" with key -> HMAC over EVP_sha256(), key bytes as given (the hex text). Also measured (inbound_test.go, 2026-10-12) - comment written in milter_headers.conf.
C-R10: HOLDS - milter.c rspamd_milter_macro_http(): macro "{i}" or "i" -> Queue-Id header -> protocol.c task->queue_id; Postfix DEF_MILT_EOD_MACROS "i" - comment written.
C-R11: HOLDS - rspamd-docker v3.11.1+3 Dockerfile USER 11333:11333; 0644 files in a 0755 dir readable by any uid - comment written (lists.go, antispam compose).
C-R12: PARTLY - holds: conf/worker-proxy.inc "milter = yes"; rspamd-docker patches/rspamd-docker-config.diff binds rspamd_proxy to "*:11332" (upstream conf binds localhost) - comment written. Fails: "3.11" is a moving series tag (rspamd-docker README "Tags": "3.9 | latest stable release in 3.9 series"), so "pin ... the same way the SelfPost image is pinned" is false. Fix: `rspamd/rspamd:3.11.1` (or a digest).
C-R13: HOLDS - milter_headers.lua x-spamd-result: 'default: %s [%.2f / %.2f]' and '%s(%.2f)[%s]' joined by '; '. inbound.go/inbound_test.go not mine: the fixture at inbound_test.go:37 should say "composed by hand".
C-R14: DOES NOT HOLD (comment in inbound.go, not mine) - milter_headers.lua x-spamd-result default sort_by='score': `math.abs(s1.score) > math.abs(s2.score)` - heaviest |score| FIRST, so an allow entry's -100 comes first, not last. No consequence (code does not depend on order). Fix: correct the inbound.go comment.
C-R15: DOES NOT HOLD (fixture comment) - smtpd_peer.c (smtpd_peer_sockaddr_to_hostaddr): with IPv4 enabled Postfix rewrites a v4-mapped peer to plain IPv4 ("rewriting V4-mapped address"); and inet_listen.c sets IPV6_V6ONLY. So Postfix 3.7.11 with inet_protocols=all never logs "::ffff:198.51.100.77". The case stays as a defensive one. Did: labelled the lists_test.go rows "composed by hand" (+ reject_message source). Fix: reword "(as Postfix may log it, mapped)" to "(mapped - Postfix 3.7.11 never logs it so with IPv4 on, smtpd_peer.c; kept as a guard)".
C-R16: HOLDS - measured, dated (2026-10-12); Postfix half cited: smtpd.c smtpd_proto() calls milter_conn_event() once per connection - comment written (negative_test.go). go-milter half belongs to the inbound.go agent.
C-R17: HOLDS - go-milter v0.4.1 session.go: CodeEOB -> m.backend.Body(); Postfix milter8.c sends EOB even with SMFIP_NOBODY (milter8_eob always installed) - inbound.go not mine, no edit.

### 9. pipe(8) and dmarc-ingest (cmd/panel - not mine, no edits)

C-M1: HOLDS - pipe.8 (`q` flag quotes, ${recipient}); pipe(8) exports only export_environment, RECIPIENT is local(8)'s.
C-M2: HOLDS - pipe.8 DIAGNOSTICS (sysexits.h); sys_exits.c EX_TEMPFAIL "4.3.0" temporary.
C-M3: HOLDS - pipe.c deliver_message(): success text " (%.100s)" - 100 bytes; pipe_command.c captures up to VSTREAM_BUFSIZE (4096) for the failure DSN; set_ugid.c setgroups(1, &gid).

### 10. DMARC

C-M4: HOLDS - RFC 7489 § 7.2.1.1 (application/gzip if compressed, text/xml otherwise; extension "xml" / "xml.gz"); zip is outside the RFC (M5) - comment written (ingest.go looksLikeAggregate).
C-M5: HOLDS as a measurement - unsourced sighting kept, now "where and when not recorded" + RFC note - comment written.
C-M6: HOLDS - RFC 7489 Appendix C targetNamespace "http://dmarc.org/dmarc-xml/0.1"; Go src/encoding/xml/read.go compares the namespace only when the tag names one - comment written.
C-M7: PARTLY - holds: pct default 100 (§ 6.3); dkim/spf in policy_evaluated are "The DMARC-aligned authentication result" (Appendix C), pass if either (§ 4.2) - cited at dmarcRecordPasses. Fails: an explicit <pct>0</pct> (allowed: "between 0 and 100, inclusive") parses to 0 and is turned into 100 (parse.go `if out.PolicyPct == 0`). See detail.
C-M8: NEEDS A MEASUREMENT - no source can say how many records real reports carry. Measure on the live installation: the largest record count of the reports received so far (per report, from the DMARC tables), and keep maxReportRecords at least 10x above it.
C-M9: HOLDS (rule now met) - fixtures labelled composed by hand: internal/dmarc/parse_test.go (sampleXML and messages), test/e2e/port25_features_test.go (aggregateXML; reportMessage with RFC 2045 § 6.8 for 76 columns).

### 11. SQLite / backup

C-Q1: HOLDS - sqlite.org/lang_vacuum.html "VACUUM INTO": "transactional in the sense that the generated output database is a consistent snapshot"; modernc.org/sqlite v1.60.1 sqlite.go applyQueryParams(): each _pragma run as "pragma <v>", busy_timeout first - comment written.
C-Q2: HOLDS - conf/postfix-files queue dirs $mail_owner 700; the incident (1.3.0-2.0.0) kept, "date not recorded" - comment written.
C-Q3: PARTLY - the atomic-rename writers hold for SelfPost's own files (atomicfile.Write uses os.Rename). sasldb2 does not: see S3 (Berkeley DB written in place, no lock environment). A backup taken during a password change can archive a torn sasldb2. Fix in S3.
C-Q4: DOES NOT HOLD - see detail. No edit.

### 12. ACME / proxies

C-A1: DOES NOT HOLD - see detail. No edit.
C-A2: HOLDS - eff-certbot.readthedocs.io User Guide "Automated Renewals": sample cron "0 0,12 * * *"; renew acts only near expiry - comment written (nginx fragment).
C-A3: NEEDS A MEASUREMENT - certbot docs describe only that the apache plugin "modifies Apache ... configuration" and an SSL vhost extension "-le-ssl.conf" (a new file); what it does with this *:443 vhost that has no SSLEngine is not documented. Measure: on a throwaway host with Apache, install deploy/apache/selfpost-vhost.conf, run `certbot --apache --staging -d <name>`, diff /etc/apache2 before/after. Note also: the shipped :80 vhost already has the redirect, so "certbot adds ... redirect" is moot.
C-A4: HOLDS - certmagic storage.go KeyBuilder.SiteCert() "certificates/<issuerKey>/<domain>/<domain>.crt", acmeissuer.go issuerKey() (CA host+path -> "acme-v02.api.letsencrypt.org-directory"); caddy automation.go DefaultIssuers(): ZeroSSL only with an email, the Caddyfile sets none. Read at master 2026-10-14 (caddy:2 floats) - comment written.
C-A5: HOLDS - Traefik v3.1.0 pkg/provider/acme/local_store.go map[resolver]*StoredData; store.go StoredData{Account, Certificates}; provider.go Certificate json "domain"/"certificate"/"key" ([]byte -> base64); resolver "le" from the fragment - comment written (extract-cert.sh). Docs do not document the layout.
C-A6: HOLDS - doc.traefik.io/traefik/v3.1/providers/docker/ "Routing Configuration": "Traefik uses container labels"; exposedByDefault=false needs traefik.enable=true - comment written.
C-A7: HOLDS - nginx.org ngx_http_v2_module "http2": "This directive appeared in version 1.25.1" - comment written. The floating `caddy:2` / `certbot/certbot:latest` tags are a fact of the fragments (noted in the caddy citation).

### 13. OpenSSL

C-X1: NEEDS A MEASUREMENT - openssl-x509(1) 3.0: -pubkey "Prints the certificate's SubjectPublicKeyInfo block in PEM format"; openssl-pkey(1) 3.0: -pubout "restricts the output to the public components" - the structure is not named, and byte-identity is not stated. The e2e stand shows it for its RSA pair on every run. Measure on the stand (the image's OpenSSL 3.0): `openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -keyout k.pem -out c.pem -subj /CN=x`, then compare `openssl x509 -noout -pubkey -in c.pem` with `openssl pkey -pubout -in k.pem` (certbot >= 2.0 issues ECDSA keys by default). No edit.

### 14. e2e harness

C-E1: HOLDS - smtp-sink.c: -d "single-message file ... appending a pseudo-random hexadecimal number"; main(): "-u option is required if running as root" - comment written (sink/Dockerfile).
C-E2: DOES NOT HOLD - see detail. No edit.
C-E3: HOLDS - RFC 5321 § 5.1 implicit MX - section added.
C-E4: HOLDS - RFC 1035 § 3.3 (<character-string> 255); RFC 6376 § 3.6.2.2 (concatenate) - comment written.
C-E5: PARTLY - holds: any -F/-P/-M rewrites each matched entry with PCF_FOLD_LINE (postconf_edit.c pcf_edit_master() -> pcf_print_master_entry()), and */*/chroot=n matches every entry. Fails: postconf_master.c pcf_print_master_entry() forces a line break before each "-o" and "Keep[s] option and value on the same line", and a value has no whitespace (P1) - so it never splits an option's value nor re-flows -o lines into one paragraph; the example (two space-separated tokens) is stale. Harmless (the reader is defensive). Fix: correct the comment in port25_config_check.go parseSMTPInetOptions.
C-E6: PARTLY - holds for master.cf (master_config() only on SIGHUP). Fails: every daemon started meanwhile reads main.cf afresh (multi_server.c multi_server_main(): mail_conf_suck()), so for the length of the step new smtpd/cleanup/qmgr processes run the port-25 combinations' main.cf. Harmless (no mail at that point). Fix: say so in the comment.
C-E7: HOLDS - measurement (1.9.1) kept, "date not recorded", + master.5 NOTE 2 - comment written.
C-E8: HOLDS - postconf_master.c pcf_show_master_entries(): nothing on stdout, msg_warn "unmatched request" - comment written.
C-E9: HOLDS - milter8.c milter8_event() SMFIR_TEMPFAIL "451 4.7.1 Service unavailable - try again later"; smtpd_check.c reject_auth_sender_login_mismatch() 553 5.7.1 "Sender address rejected: not owned by user"; generic_checks() REJECT_ALL var_reject_code (DEF_REJECT_CODE 554) "%s rejected: Access denied"; smtpd.c mail_cmd() "450 4.7.1 Error: too much mail from" - comments written (negative_test.go).
C-E10: PARTLY - holds: "454 4.3.0 Try again later" at AUTH (smtpd_sasl_proto.c smtpd_sasl_auth_cmd(): milter 4xx -> 454). Fails: "451 from the connection on" - smtpd.c smtpd_proto() changes the greeting only for a 5xx or 421 milter reply (otherwise "220"), and ehlo_cmd() answers only a 421; the 451 comes at MAIL. Test asserts only 4xx: harmless. Fix: reword the comment in sender_check_test.go. No edit.
C-E11: HOLDS - measured (dated) + smtpd.c milter_send(), cleanup_milter.c - comment written.
C-E12: HOLDS - milter8.c milter8_header(): the value is the whole collected header incl. continuation - comment written.
C-E13: HOLDS as a measurement - kept, "date not recorded"; mechanism cited: DATABASE_README texthash (read once per process), multi_server.c multi_server_abort() - comment written.
C-E14: HOLDS as a measurement - incident kept, "date not recorded" - comment written.
C-E15: HOLDS - smtpd.c smtpd_service(): msg_info("connect from %s", state.namaddr) - comment written.
C-E16: pointer - logrotate_check.go rests on L5 (holds), restore_test.go on P34 (the queue IS rebuilt on start, by `postfix check` - not by the branch P34 names), cert_reload_check.go on P38 (holds). No edit.

Also labelled: internal/backup/backup_test.go mail.log line "composed by hand".

---

### Step-3 detail (DOES NOT HOLD, and the failing parts of PARTLY)

**P4** postfix-config.sh:71-74, postfix-cert-reload.sh header: "Postfix insists the private key is root-owned and mode 0600."
Source: no mode/owner check anywhere in src/tls, src/smtpd or postfix-script; smtpd loads the key as root in pre_jail_init() (`if (getuid() == 0 || getuid() == var_owner_uid)`), so it could read the mount as it is. TLS_README only recommends: the key file "should be owned by "root" and not be readable by any other user".
Consequence: none for behaviour - the copy is still worth having (cert-reload validates a renewal on the copy before Postfix sees it). The stated reason is false, and someone may "simplify" the copy away for the wrong reasons or keep it for the wrong ones.
Fix: reword both comments: "Postfix does not check the key's mode (Postfix 3.7.11: no check in src/tls; smtpd reads it as root, smtpd.c pre_jail_init()); TLS_README recommends root-owned, unreadable by others. The copy gives that, and lets cert-reload check a renewed pair before Postfix uses it." Check: none needed (comment).

**P8** postfix-config.sh:168-169 "Empty these so a misfiled recipient never gets delivered locally."
Source: resolve_local.c resolve_local(): after mydestination, "Compare the destination against the list of interface addresses that we are supposed to listen on" - with inet_interfaces=all an address literal of the container's own address (`x@[127.0.0.1]`, `x@[172.18.0.2]`) is local, and trivial-rewrite gives it to local(8). STANDARD_CONFIGURATION_README l.209 (null client): `local_transport = error:local mail delivery is disabled`.
Consequence: an authenticated application sending to `root@[127.0.0.1]` gets local delivery (or a local(8) bounce) inside the container instead of a refusal; with alias_maps empty `root` lands in /var/mail/root of an ephemeral container.
Fix: `postconf -e "local_transport=error:local mail delivery is disabled"`. Check: e2e - an authenticated send to `postmaster@[127.0.0.1]` is bounced with "local mail delivery is disabled" (mail.log status=bounced), nothing under /var/mail.

**P31** postfix-config.sh:439-452, 466-467: `-o message_size_limit=${PORT25_SIZE}` sets port 25's limit (inbound default 26214400).
Source: cleanup_init.c cleanup_post_jail(): `if (ENFORCING_SIZE_LIMIT(var_message_limit)) set_file_limit((off_t) var_message_limit)` - cleanup runs with main.cf's message_size_limit, which is never set, so DEF_MESSAGE_LIMIT 10240000 (mail_params.h:1997). smtpd's own checks (smtpd.c:1995, 3657, 4047) use the -o value. A write past the rlimit is EFBIG -> cleanup_strerror.c CLEANUP_STAT_SIZE "552 5.3.4 message file too big".
Consequence: EHLO on 25 advertises SIZE 26214400, a 12 MB message to a relay domain is accepted by smtpd through DATA and then refused 552 by cleanup - every message between ~10 MB and 25 MiB to the inbound relay bounces at the sender. DMARC (5 MiB) is unaffected.
Fix: give cleanup the largest limit any listener uses, e.g. `postconf -P "cleanup/unix/message_size_limit=${max(PORT25_SIZE, 10240000)}"` when the inbound relay is on (submission keeps main.cf's limit in smtpd). Check: e2e - deliverPort25 a 12 MB message to the inbound domain, expect 250 and arrival at the sink; and the read-back of `postconf -Ph cleanup/unix/message_size_limit`.

**P34** entrypoint.sh:171-175 "Initialise the persistent queue tree on first start or after restore" (`if [ ! -d .../active ]; then postfix set-permissions`).
Source: postfix-config.sh, which runs first, ends with `postfix check`; postfix-script "check" -> check-fatal -> `post-install create-missing`, which builds every queue directory from conf/postfix-files (`$queue_directory/active:d:$mail_owner:-:700:ucr`). By the time the test runs, active/ exists.
Consequence: the branch is dead; harmless. The comment describes a mechanism that never runs (the queue is built by `postfix check`).
Fix: delete the branch and say "postfix-config.sh's `postfix check` builds a missing queue tree (post-install create-missing)". Check: e2e restore_test (start over a /data without queue) still passes.

**P42** supervisord.conf [program:postfix] (no stopasgroup): stopping the program stops Postfix.
Source: P33 - the supervised pid is postfix-script's shell; master.c calls setsid() (own session and process group). supervisor process.py kill(): `options.kill(pid, sig)` to the pid only (to -pid only with stopasgroup/killasgroup, and master is not in that group anyway).
Consequence: (a) `supervisorctl restart postfix`: the shell dies, master lives on orphaned; the new start-fg hits `master -t` -> "the Postfix mail system is already running", exit 1 within a second, BACKOFF x3 -> FATAL -> crashexit takes the container down. Nothing in the tree runs it today. (b) Every `docker stop`/restart: supervisord stops the program (the shell), master is never told; when supervisord (PID 1) exits, the kernel SIGKILLs the namespace - Postfix never gets its graceful SIGTERM, sessions in progress are cut. The queue is crash-safe, so no mail is lost; clients mid-DATA retry.
Fix: postfix-wrapper.sh does not exec start-fg; it traps TERM/INT and runs `postfix stop` (then waits), e.g. `trap 'postfix stop; exit 0' TERM INT; postfix start-fg & wait`. Check: e2e - `supervisorctl restart postfix` leaves one master and the container up; after `docker compose stop`, mail.log has "postfix/master[...]: terminating on signal 15".

**S3** entrypoint.sh:92-100: smtpd reads sasldb2 while the panel's saslpasswd2 writes it.
Source: cyrus-sasl2 sasldb/db_berkeley.c berkeleydb_open(): `db_create(mbdb, NULL, 0)` and `->open(*mbdb, NULL, path, NULL, DB_HASH, flags, 0660)` - no DB_ENV, so no Berkeley DB locking; writers use flags DB_CREATE and put()/del() in place (_sasldb_putdata()).
Consequence: an AUTH during a password change can read a page mid-write - a spurious 535 or a DB error for that login; a backup (Q3) taken then archives a torn sasldb2, and a restore of it breaks AUTH for every application.
Fix: the panel writes a copy (`saslpasswd2 -f <tmp copy>` on a copy of sasldb2) and renames it over; and the backup reads sasl/ under the same panel mutex. Check: unit test in the panel's SASL writer (the file is replaced by rename: inode changes); e2e - change a password in a loop while authenticating in another loop, no 535 for the unchanged login.

**V5** supervisord.conf [program:logrotate]: "Exits non-zero when the config is group/other-writable ...; autorestart surfaces BACKOFF on the Status page."
Source: process.py Subprocess.finish(): with startsecs=0 `too_quickly` is never true, so the exit is RUNNING -> EXITED; transition(): EXITED + autorestart -> spawn() at once. BACKOFF and FATAL are never reached.
Consequence: a group-writable /etc/logrotate.d/mail makes the program respawn about once a second forever (supervisord's 1 s loop), each run logging the refusal; the Status page shows it flapping RUNNING/EXITED, not BACKOFF; nothing rotates.
Fix (recommended): `startsecs=1` for [program:logrotate], so a refusal (immediate exit) is "exited too quickly" -> BACKOFF -> FATAL after startretries; and teach crashexit.py to ignore processname:logrotate (it is not on the mail path) so FATAL shows on Status instead of killing the container. Check: e2e TestImageBuildPreservesLogrotateMode sibling - chmod g+w the config in a running stand, restart the program, expect FATAL and the container still up.

**V6** postfix-wrapper.sh:8-10, supervisord.conf:8-10: a milter-socket timeout "exits non-zero ... so supervisord/Docker see an explicit startup failure"; FATAL -> crashexit.
Source: process.py finish(): after a 30 s wait the default startsecs=1 has long passed -> RUNNING -> EXITED (unexpected), autorestart=true -> respawn at once. FATAL only for three exits within startsecs. Docker sees nothing.
Consequence: if the panel never opens its sockets, the container stays "up" forever without Postfix (only the healthcheck turns unhealthy; Docker does not restart unhealthy containers on its own) - the opposite of what both comments promise.
Fix: `startsecs=` above MILTER_WAIT_TIMEOUT for [program:postfix] (e.g. 40): a timeout exit then counts as "too quickly" -> BACKOFF -> FATAL after 3 -> crashexit -> container exits -> restart policy. Check: e2e with the panel's sender socket path pointed elsewhere (env), expect the container to exit within ~4 x 30 s.

**V10** process_check.go:9-12, 42-44: "postfix-reload ... its healthy resting state is STOPPED".
Source: process.py finish(): after any run the state is EXITED (expected), not STOPPED; STOPPED holds only until the first start.
Consequence: the check passes only because it runs before anything asks for a reload; called after one (a later re-check) it would fail on a healthy container.
Fix: accept STOPPED or EXITED for postfix-reload. Check: run checkSupervisorProcesses after a panel action that reloads Postfix.

**V8 (failing part)**: "Docker's restart policy can recreate it" - Docker restarts the same container. Fix: wording in supervisord.conf header and crashexit.py docstring.

**D1** Dockerfile:33, 41-53: the versions every citation names are what the image runs.
Source: `FROM debian:bookworm-slim` (a moving tag) and `apt-get install` without versions take the current point release; nothing records or checks the installed versions.
Consequence: after a Debian point/security update (e.g. a postfix 3.7.x-0+deb12uN), a rebuild silently runs a version the citations were not read against.
Fix (recommended): a release-gate check, not a pin (pinned Debian versions vanish from the mirrors): the e2e stand runs `dpkg-query -W -f '${Package} ${Version}\n' postfix opendkim libsasl2-2 supervisor logrotate` and compares with the versions development.md names; a difference fails the gate and sends someone to re-read. Check: that e2e test itself.

**K8** compose.override.yml:24-27: "compose build.context paths follow the project directory, unlike bind-mount volume paths".
Source: docs.docker.com/compose/how-tos/multiple-compose-files/merge/ "Paths are evaluated relative to the base file"; with --project-directory both resolve against it (the same file's ./dns-stage and ./mail-stage volumes land in .stage).
Consequence: none (comment only); it contradicts the deploy fragments' (true) statement.
Fix: "build.context and volume paths both resolve against --project-directory (.stage)".

**K9** stage.go:57-59 "compose reads neither"; stack.go:65-69 "straight from the process environment".
Source: docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/: without --env-file Compose reads .env from the project directory (--project-directory first); shell variables take precedence over it.
Consequence: none - the .env written to .stage holds the same values, and the shell's SELFPOST_HOSTNAME wins anyway.
Fix: "compose reads .env from --project-directory too; the shell value set here takes precedence".

**R4 (failing part)**: multimap.lua `from` default: envelope sender, MIME From: when the envelope is empty. A bounce (<>) whose From: header is on the deny list is refused, and one whose From: is on the allow list is weighted -100, while the journal records sender "" - the Filter lists page cannot attribute it (Usage matches the envelope). Fix: `extract_from = "smtp";` on the four from-rules in multimap.conf. Check: dev trial in a throwaway rspamd 3.11.1 container (`rspamc --from '' --header 'From: x@deny.example'`) expecting no SELFPOST_ symbol.

**R7** milter_headers.conf:5-8 "rspamd removes headers of these names that a sender wrote himself before it adds its own."
Source: milter_headers.lua (3.11.1): settings skip_local = true, skip_authenticated = true; skip_wanted() returns true when `task:get_ip():is_local()` (lua_ip.c lua_ip_is_local(): loopback, or within local_addrs - conf/options.inc `local_addrs = [192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12, fd00::/8, 169.254.0.0/16, fe80::/10]`) or the task has a user; then routines x-spamd-result and x-rspamd-action return before setting `remove[...]` - nothing added, nothing removed. The custom routine selfpost-signature has no such skip and always signs.
Consequence: a client whose address is in a private range - any LAN host, or every client if Docker's proxy shows the bridge gateway 172.x.0.1 (K5) - can write its own "X-Spamd-Result: default: False [-5.00 / 15.00]" and "X-Rspamd-Action: no action"; rspamd leaves them, adds a valid X-SelfPost-Filter signature, and the inbound journal records the sender's verdict as rspamd's - the very forgery the signature was introduced to stop.
Fix: in milter_headers.conf set `skip_local = false;` and `skip_authenticated = false;` (documented module options). Check: dev trial in a throwaway rspamd 3.11.1 container: scan a message with a forged X-Spamd-Result from IP 10.0.0.5 (`rspamc --ip 10.0.0.5 --mime`), expect the forged header removed and rspamd's own added.

**R12 (failing part)**: `rspamd/rspamd:3.11` floats across 3.11.x. Fix: `image: rspamd/rspamd:3.11.1` (or @sha256 digest), and say "pinned" only then.

**M7 (failing part)**: parse.go `if out.PolicyPct == 0 { out.PolicyPct = 100 }` also turns an explicit `<pct>0</pct>` (RFC 7489 § 6.3: "between 0 and 100, inclusive") into 100. Consequence: a domain published with pct=0 (monitoring-only rollout) shows as 100 %. Fix: Pct as *int in feedbackXML (nil -> 100). Check: parse_test with `<pct>0</pct>` expects 0, without `<pct>` expects 100.

**Q4** backup.go:271-276: symlinks skipped silently.
Source: addTree() skips non-regular files. eff-certbot docs "Where are my certificates?": "point your (web) server configuration directly to those files (or create symlinks)"; "--force-renewal": "symbolic links (the "live" reference) will be updated" - live/ entries are symlinks.
Consequence: an operator who follows certbot's own advice and makes certs/ a set of symlinks gets a full backup with no certificate and no warning; after a restore TLS on 465 is dead (P5).
Fix: in addDeployFiles/addTree for certs/, follow a symlink whose target is a regular file (archive the content under the link's name), or at least OnWarn("certs/<name> is a symlink and was not archived"). Check: backup_test with certs/fullchain.pem a symlink - the archive holds the PEM (or the warning is emitted).

**A1** apache vhost:9-14, deploy/nginx/docker-compose.nginx.yml:35: bind-mounting certbot's `live/<domain>` gives Postfix the PEM files.
Source: certbot docs: live/ holds symbolic links to the current files ("--force-renewal": "symbolic links (the "live" reference) will be updated to point to the new certificate"); they point into archive/, which is not mounted. Inside the container the links dangle: postfix-config.sh's `[ -f "$TLS_CERT_SRC" ]` is false, no copy is made, smtpd_tls_cert_file points at a dangling path -> "Wrapper-mode request dropped ... TLS context initialization failed" (P5) on every 465 connection.
Consequence: an operator following the Apache or nginx scenario as written gets a relay with no working TLS on 465 from the first start.
Fix: mount the whole letsencrypt directory read-only (`/etc/letsencrypt:/etc/letsencrypt:ro`, nginx: `./nginx/certbot-etc:/etc/letsencrypt:ro`) and set TLS_CERT_FILE/TLS_KEY_FILE to `/etc/letsencrypt/live/<domain>/{fullchain,privkey}.pem`; fix the vhost comment. Check: e2e stand variant with a certbot-shaped tree (live/<d>/*.pem symlinks into archive/<d>/*1.pem), mount the parent, expect a TLS greeting on 465.

**E2** dns/Corefile:5-9, dns_zone.go:39-42: reload "polls db.zone's mtime/serial"; "CoreDNS's `reload 2s` ... picks up the new mtime".
Source: CoreDNS v1.14.6 plugin/file/reload.go Reload(): without reload_by_mtime it re-parses with the current serial, and file.go Parse() returns serialErr "no change in SOA serial" when the serial is unchanged - reload is by SOA serial only. coredns.io/plugins/file: reload "if the SOA version changes"; default one minute. The Corefile has `reload 1s`, not 2s. writeZone sets the serial to time.Now().Unix().
Consequence: two writeZone calls within one second keep the serial; CoreDNS never serves the second (publishTXT's poll fails after its timeout). Today the writes happen to be seconds apart.
Fix: writeZone keeps a strictly increasing serial (max(last+1, now)); comments say "SOA serial" and "reload 1s". Check: a unit test in test/e2e - two writeZone calls in a row yield increasing serials (parse the SOA line).

### DOES NOT HOLD
P4, P31, P34, P42, S3, V5, V6, V10, D1, K8, K9, R14 (inbound.go, not mine), R15 (fixture comment), Q4, A1, E2.

### PARTLY (the failing part needs a fix)
P8, P11 (comment corrected), P23, V8, V11, R4, R5, R7 (security-relevant), R12, M7, Q3, E5, E6, E10.

### NEEDS A MEASUREMENT
K5 (client address through Docker's proxy, IPv4 and IPv6 - live host), M8 (largest record count in received reports - live DB), A3 (certbot --apache on the shipped vhost - throwaway host), X1 (x509 -pubkey vs pkey -pubout for an ECDSA P-256 pair - on the stand's OpenSSL 3.0).

## Answers, Part D sections 1-5 (store, htmx, browsers, html/template, Postfix claims in help/health text)

Sources read: modernc.org/sqlite v1.60.1 in the module cache (driver.go, conn.go, sqlite.go, stmt.go, tx.go, error.go,
result.go) and its bundled SQLite 3.53.4 (lib/, transpiled C with the C comments kept; SQLITE_VERSION "3.53.4");
sqlite.org pages fetched 2026-10-14 (lang_transaction, wal, pragma, foreignkeys, datatype3, lang_upsert, lang_altertable,
lang_datefunc, lang_corefunc, lang_expr, lang_select, partialindex, rescode, fileformat2, c3ref/changes,
last_insert_rowid, busy_handler, prepare); htmx v2.0.4 src/htmx.js (github tag v2.0.4); Fetch Standard, W3C Fetch
Metadata, CSP Level 3, HTML Living Standard (multipage), W3C Clipboard API, RFC 6265, draft-ietf-httpbis-rfc6265bis
(editor's copy of 2026-10-14), RFC 6797, RFC 9111, MDN browser-compat-data 6.0.0; Go html/template, text/template,
net/http fs.go of release-branch.go1.26 (identical to the local 1.27 for the functions cited); Postfix 3.7.11 and Cyrus
SASL 2.1.28 in dev/upstream; rspamd 3.11.1 (conf/options.inc, src/libserver/cfg_utils.cxx, src/libserver/maps/map.c).

Code changes are comments only: no behaviour changed. Migrations were not touched (docs/schema-migrations.md: a shipped
migration is never edited), so the citations for what they rely on sit at store.go migrate(). help.go and every visible
wording were left alone.

### 1. SQLite / modernc.org/sqlite

D-S1: HOLDS - modernc v1.60.1 conn.go newConn() -> sqlite.go applyQueryParams() (the _pragma list, busy_timeout sorted
first, on every new connection); foreignkeys.html § 2 ("must be enabled separately for each database connection");
wal.html § 3.3 (WAL is persistent) - citation at store.go Open (DSN).

D-S2: PARTLY - the conclusion holds (one pooled connection: database/sql serialises this process, so no in-process
SQLITE_BUSY), the premise does not: modernc does not serialise writes across connections (conn.go: `sync.Mutex` is
embedded per conn; nothing process-wide). Between processes it is SQLite's single write transaction
(lang_transaction.html § 2.1) plus busy_timeout. - Comment at store.go SetMaxOpenConns rewritten to the true reason, with
the citations.

D-S3: DOES NOT HOLD - lang_transaction.html § 2.2: "DEFERRED means that the transaction does not actually start until the
database is first accessed"; modernc tx.go newTx(): plain "begin" unless `_txlock` is set. migrate() reads user_version
with s.db.QueryRow outside the transaction. Concrete case: after an image upgrade the panel starts and, at the same
moment, Postfix pipes a DMARC report to `panel dmarc-ingest`; both read user_version = 8, both Begin; the first migration
statement of one waits for the other's write lock (busy_timeout), then runs 0009 again on the migrated file: "duplicate
column name: refusal_line" -> Open fails (the ingest exits, Postfix retries; or the panel start fails, supervisord
restarts). Nothing is corrupted (the failing transaction rolls back), but "each migration runs at most once" is not what
the code guarantees - it is "at most once successfully". No citation written. Fix (~15 lines): add `_txlock=immediate` to
the DSN (modernc sqlite.go: sets conn.beginMode, tx.go then issues "begin immediate"; a BEGIN IMMEDIATE waits through the
busy handler because the connection holds no transaction yet - btree.c btreeBeginTrans()), and in migrate() re-read
user_version inside each migration's transaction and skip (commit nothing) when it is already >= target. Test: create a
WAL file, hold `BEGIN IMMEDIATE` on a raw *sql.DB, start two goroutines calling store.Open on the path, let both read
user_version, release the lock; today one Open fails with "already exists"/"duplicate column", after the fix both succeed
and user_version == len(migrations). `_txlock=immediate` also makes D-S11 structural.

D-S4: HOLDS - modernc v1.60.1 stmt.go stmt.exec(): "FALLBACK PATH: Multi-statement script" loops prepareV2() over the
tail and steps every statement - citation at store.go migrate().

D-S5: HOLDS - SQLite 3.53.4 pragma.c sqlite3Pragma(), case PragTyp_HEADER_VALUE: the value is the literal's text
(sqlite3Atoi(zRight)), written by the setCookie program = OP_Transaction (write) + OP_SetCookie, so it is part of the
write transaction and a rollback undoes it - citation at store.go (the PRAGMA user_version exec).

D-S6: HOLDS - fileformat2.html § 1.3.15 (application id at offset 68); btree.c newDatabase(): memset(data+24, 0, 76), so a
file reads 0 until it is set - citation at store.go migrate().

D-S7: NEEDS A MEASUREMENT - busy_timeout semantics are documented (c3ref/busy_timeout), but whether 5000 ms outlasts the
longest write is a property of this data. Measure on the stand, on a database filled to the retention limit (90 days at
the highest traffic the owner expects): the duration of (a) migration 0008's UPDATE of all of send_log (one-off),
(b) DeleteSendLogBefore after a long gap, (c) PruneSendLogLines, (d) InsertSendLogLines with one pass's worth of lines,
(e) the backup tool's read/VACUUM INTO if it writes; and whether dmarc-ingest ever logs SQLITE_BUSY while the panel runs
(c). Anything above ~2 s is the signal to raise the timeout or batch the delete.

D-S8: HOLDS - lang_transaction.html § 2.1: a write transaction is started by INSERT/UPDATE/DELETE, and there is only one at
a time; the INSERT ... SELECT ... WHERE NOT EXISTS is one statement, so its read happens inside the write transaction
(in WAL, on the newest snapshot). c3ref/changes.html: an insert of no row sets changes() to 0. - citations at
users.go CreateGlobalUser (canonical), sendlog.go AttachRefusalLine and RecoverRefusal, inboundspam.go
InsertInboundSpamOnce, sendloglines.go (`once`). In this process SetMaxOpenConns(1) serialises the two requests anyway.

D-S9: HOLDS - lang_transaction.html § 2.2 ("If the first statement after BEGIN DEFERRED is a write statement, then a write
transaction is started") + § 2.1 (one at a time); a write transaction begun with no earlier read in it reads the newest
snapshot - citation at users.go SaveUser (merged into "A write first"), DeleteUser points at it.
(users_tx_test exercises database/sql's pool, as the inventory says; the SQLite property is what the citation covers.)

D-S10: HOLDS (and is stronger than the comment says) - SQLite 3.53.4 btree.c btreeBeginTrans(): the busy-handler retry
loop runs only while `pBt->inTransaction == TRANS_NONE`, so upgrading a read transaction returns SQLITE_BUSY at once,
not only after the other connection committed (SQLITE_BUSY_SNAPSHOT, rescode.html#busy_snapshot) but also while it merely
holds the write lock; c3ref/busy_handler.html says the handler is skipped where it could deadlock. - citation at
sendlog.go UpdateStatus.

D-S11: HOLDS - every multi-statement transaction in internal/store opens with a write: applications.go AddApplication
(INSERT), UpdateApplication (UPDATE); inbound.go x2; dmarc.go (DELETE); users.go AddUser, SaveUser, DeleteUser,
setUserReach/writeUserReach (UPDATE); sendlog.go UpdateStatus; sendercheck.go (INSERT); sendloglines.go (Prepare, then
INSERT - prepare.c sqlite3InitOne() commits the read transaction it opens to load the schema, so Prepare leaves none
open). - a one-line "The first statement is a write (see UpdateStatus)." at each Begin, the rule stated once at
UpdateStatus. (Structural option: `_txlock=immediate`, see D-S3.)

D-S12: HOLDS - modernc builds SQLite with SQLITE_DEFAULT_SYNCHRONOUS = SQLITE_DEFAULT_WAL_SYNCHRONOUS = 2 (lib/, FULL) and
the DSN sets no synchronous; wal.html § 2.3: "Writers sync the WAL on every transaction commit if PRAGMA synchronous is
set to FULL" - citation at sendloglines.go InsertSendLogLines.

D-S13: HOLDS - wal.html § 2.2 / § 1: "readers do not block writers and a writer does not block readers". Within the
panel the read still waits for the single pooled connection - citation at sessions.go LookupSession, with that note.

D-S14: HOLDS - foreignkeys.html § 2 (per connection) and § 4.3 (CASCADE, SET NULL), with foreign_keys ON on every
connection (D-S1) - citation at domains.go DeleteDomain; applications.go DeleteApplication and dmarc.go
InsertDMARCReport point at it. (Migrations 0001/0008 not edited.)

D-S15: HOLDS - foreignkeys.html § 4.2: "Foreign key constraints are immediate by default"; a violation of an immediate
constraint fails the statement - citation at users.go writeUserReach.

D-S16: HOLDS - lang_altertable.html § 4: "If foreign key constraints are enabled and a column with a REFERENCES clause is
added, the column must have a default value of NULL"; pragma.html#pragma_foreign_keys: "This pragma is a no-op within a
transaction" (so a migration cannot switch FKs off) - citation at store.go migrate().

D-S17: HOLDS - lang_altertable.html § 8: the only direct schema changes are rename table/column, add/drop column; anything
else is the rebuild procedure - citation at store.go migrate().

D-S18: HOLDS for the migration's claim - Postfix 3.7.11 src/smtpd/smtpd_check.c:3913 reject_auth_sender_login_mismatch(),
:3936 strcasecmp_utf8(state->sasl_username, name). NOCASE (ASCII) and strcasecmp_utf8 agree because logins are ASCII
only. development.md's example was already corrected in 811055d. - citation at applications.go ErrLoginExists.

D-S19: HOLDS - datatype3.html § 7: "NOCASE ... only ASCII characters are case folded" - citation at domains.go
DomainExists (names validated ASCII by validate.Domain) and sendlog.go AttachRefusalLine. For to_addr/from_addr see S20.

D-S20: DOES NOT HOLD (the hedge) - Postfix 3.7.11 does not change the case of a recipient between RCPT TO and the delivery
line: smtpd.c rcpt_cmd() passes the client's argument to the milter (milter_argv(), :2499-2518, :3088), the queue file
holds that address after cleanup's rewriting, and no code on the way lowercases it (grep lowercase/casefold over
cleanup/, trivial-rewrite/, qmgr/: the only hit is qmgr_message.c:1309, which lowercases the *queue name* used for
scheduling, not the recipient); global/log_adhoc.c:111 logs `to=<%s>` from recipient->address through
info_log_addr_form_recipient(), which only quotes (quote_822_local) and never folds case. log_adhoc.c:114 even compares
address and orig_addr with strcasecmp_utf8 - Postfix treats them as one, but prints the address as stored. The journal
milter (internal/milter/milter.go cleanAddress) keeps the case too. So the row and the line carry the same text. Consequence
of matching NOCASE anyway: for one message to "User@x" and "user@x" (distinct mailboxes by RFC 5321 § 2.4), both delivery
lines update the newest of the two rows, and the other stays "queued" until the reconcile sweep closes it as bounced on a
guess. Rare, but a wrong result. No citation written. Fix: replace "Postfix may normalise address case ..." with the source
answer above and match the recipient exactly (drop COLLATE NOCASE in UpdateStatus' `newest`, and the EqualFold in
internal/logtail/logtail.go:321, which belongs to another agent). Test: a queue id with recipients "User@x" and "user@x",
one delivery line each -> each row gets its own status. If the owner prefers to keep NOCASE as harmless, the comment must
still lose the "may".

D-S21: HOLDS - Cyrus SASL 2.1.28 lib/canonusr.c _canonuser_internal() only trims whitespace and appends the realm;
sasldb/allockey.c _sasl_alloc_key() copies the identity byte for byte into the user\0realm\0prop key; db_berkeley.c opens a
DB_HASH (exact keys). So "OPS" cannot authenticate as "ops", and the login reaches {auth_authen} and the log in its stored
case - citation at sendlog.go loginDomainID (used by stats.go and notify.go too).

D-S22: HOLDS - datatype3.html § 7 ("BINARY - Compares string data using memcmp()"); every writer formats UTC with
time.RFC3339 (checked all Format( calls in internal/store: the non-UTC-looking ones all take `now := time.Now().UTC()`;
sendercheck.go's fractional stamp is compared only with itself; dayLayout is fixed-width) and no migration uses
datetime()/CURRENT_TIMESTAMP - citation at sendlog.go ListQueuedOlderThan, pointed to from DeleteSendLogBefore and
sessions.go DeleteExpiredSessions.

D-S23: HOLDS - lang_datefunc.html § 2 "Time Values": formats 5-7 take the literal "T", and formats 2-10 "may be optionally
followed by a timezone indicator of the form [+-]HH:MM or just Z" - citation at sendlog.go AttachRefusalLine. (A test that
tells nearest-by-julianday from fallback-by-id is still the evidence the inventory asks for.)

D-S24: HOLDS - lang_corefunc.html#substr: "begins with the Y-th character ... The left-most character of X is number 1 ...
If X is a string then indices count UTF codepoints" - citation at stats.go sendStats.

D-S25: HOLDS for today's callers - same page: substr counts codepoints of TEXT; len() counts bytes; equal for "dns:" and
"limit:" - citation at notify.go ListNotifyStateKeys.

D-S26: HOLDS - lang_upsert.html § 2: excluded. is the would-be inserted value; "The only use for the WHERE clause at the
end of the DO UPDATE is to optionally change the DO UPDATE into a no-op"; all upserts use VALUES (no § 2.2 parsing
ambiguity) - citations at sendercheck.go (the WHERE one) and settings.go SetSetting.

D-S27: HOLDS - c3ref/changes.html ("Only changes made directly by the INSERT, UPDATE or DELETE statement are considered -
auxiliary changes caused by triggers, foreign key actions or REPLACE constraint resolution are not counted"); for an
UPDATE that changes no value: update.c sqlite3Update() writes every row its WHERE finds through
sqlite3CompleteInsertion(), which sets OPFLAG_NCHANGE (insert.c), so it is counted; modernc result.go newResult() takes
sqlite3_changes - citations at users.go SaveUser / DeleteUser / CreateGlobalUser, domains.go DeleteDomain.

D-S28: HOLDS - c3ref/last_insert_rowid.html (per connection, the most recent successful INSERT); modernc conn.go
lastInsertID() = sqlite3_last_insert_rowid - citation at applications.go AddApplication.

D-S29: HOLDS - modernc conn.go newConn(): c.extendedResultCodes(true) on every connection; error.go Error.Code() returns
the rc of the step, i.e. the extended code; rescode.html: SQLITE_CONSTRAINT_FOREIGNKEY 787, _PRIMARYKEY 1555, _UNIQUE 2067
(a unique index, NOCASE included, fails as UNIQUE) - citation at domains.go isForeignKeyViolation/isUniqueViolation.

D-S30: HOLDS - lang_expr.html § 4: a plain "?" gets "a number one greater than the largest parameter number already
assigned"; modernc conn.go bind() + argFinder.find(): ?NNN binds argument NNN, a plain ? the argument of its index -
citation at stats.go appByLogin (sendlog.go AttachRefusalLine mentions ?1 reused).

D-S31: HOLDS - lang_select.html § 5: "If the LIMIT expression evaluates to a negative value, then there is no upper bound"
- citation at inboundspam.go PruneInboundSpam.

D-S32: PARTLY - holds: the literal makes the partial index usable at the first prepare (partialindex.html § 3: the query's
term must match the index's WHERE term exactly) - citation at notify.go LimitRefusalsSince. Does not hold: migration
0005's implication that a bound `status = ?` would not let SQLite use it. SQLite 3.53.4 expr.c sqlite3ExprCompare():
"If pParse is not NULL and SQLITE_EnableQPSG is off then TK_VARIABLE terms in pA with bindings in pParse->pReprepare can
be matched against literals in pB"; where.c whereUsablePartialIndex() passes pParse; exprCompareVariable() sets the
variable's expmask bit, so binding it expires the statement and c3ref/prepare.html: "the statement will be automatically
recompiled ... on the first sqlite3_step() call following any change to the bindings" - with the value 'rejected' the
re-prepared plan uses the index. Consequence: none for behaviour (the literal is the cheaper form - no re-prepare per
call); only the migration's sentence overstates. Fix: none in code (the migration is never edited); the notify.go comment
now states only what holds. An EXPLAIN QUERY PLAN test over the real query string (audit stage 3) is what would pin it.

### 2. htmx 2.0.4

D-H1: HOLDS - src/htmx.js, the document ready handler runs mergeMetaConfig() then insertIndicatorStyles(), which inserts
the <style> only `if (htmx.config.includeIndicatorStyles !== false)` - citation in layout.html's existing template
comment.

D-H2: PARTLY - holds that the filter needs eval: maybeGenerateConditional() compiles it with Function() inside maybeEval().
Does not hold: "silently". Under the CSP the EvalError is caught, triggerErrorEvent(body, 'htmx:syntax:error') logs it
(logError), the function returns null and the trigger is pushed without its filter - it would fire on every tick (plus
the browser's own CSP console report). - panel.js comment corrected and cited (comment only).

D-H3: PARTLY - htmx:load fires once per top-level element of the swapped content (insertNodesBefore() queues
makeAjaxLoadTask(child) for each non-text child; the task triggers htmx:load on it, detail.elt = it - triggerEvent sets
detail.elt), and once on body after init, not "for every element". initShowWhen(root) looks at the descendants with
querySelectorAll, which covers what it needs (a top-level swapped element is a box, never a matching control). - panel.js
comment made exact and cited.

D-H4: HOLDS - swap(): `forEach(settleInfo.elts, ... triggerEvent(elt, 'htmx:afterSwap', ...))` runs before doSettle(),
and only the settle tasks (makeAjaxLoadTask -> processNode -> addTriggerHandler 'load') act on hx-trigger; for outerHTML,
swapOuterHTML() puts the new elements in settleInfo.elts - citation in panel.js onPollElementReady.

D-H5: DOES NOT HOLD (known, audit M11, stage 4) - ajaxHelper() passes resolveTarget(context.source) = null as elt;
issueAjaxRequest(): `if (elt == null) { elt = getDocument().body }`. No citation.

D-H6: DOES NOT HOLD (known, M11) - config.responseHandling `{ code: '[45]..', swap: false, error: true }` and
handleAjaxResponse() fires htmx:responseError on elt = body (and sendError/timeout likewise, src/htmx.js:4499-4514), so
detail.elt has no data-poll and polling stops. No citation.

D-H7: DOES NOT HOLD - same cause: htmx:beforeRequest is triggered on elt = body (src/htmx.js:4518), so ev.target is body
and the hidden-tab guard never matches. Harmless today (schedulePoll skips hidden tabs) but dead code. Fix: part of M11 -
pass `source: el` to htmx.ajax in triggerPoll (then beforeRequest, responseError, sendError and timeout all fire on the
box and bubble to body with ev.target/detail.elt = the box) and handle htmx:sendError and htmx:timeout like
responseError. Test: a browser test (or the e2e stand) that answers one poll with 500 and sees the next poll scheduled;
none exists for panel.js today.

D-H8: HOLDS - getHeaders(): `'HX-Request': 'true'` for every request issueAjaxRequest makes; loadHistoryFromServer() sets
it too. Every hx-get of the panel is a load/poll (components.html:87, health_body.html:15, out_log_rows.html:6) -
citation at auth/middleware.go isSessionActivity.

D-H9: DOES NOT HOLD (known, M11) - the XHR follows the 303 (Fetch: a request's redirect mode defaults to "follow");
handleAjaxResponse() acts only on HX-Redirect/HX-Location headers (src/htmx.js:4692, 4711); the sign-in page answers 200,
which responseHandling swaps into the box. No citation.

D-H10: HOLDS - findAndSwapOobElements() takes every [hx-swap-oob] out of the fragment, nested ones while
allowNestedOobSwaps (default true); oobSwap() with "true" swaps outerHTML onto `#id`; the main content replaces only the
target - citation at view/components.go Box.OOB.

D-H11: HOLDS - addTriggerHandler(): `!nodeData.firstInitCompleted && triggerSpec.trigger === 'load'` -> fires once when
the element is first processed - citation in panel.js (polling block).

### 3. Browsers

D-B1: HOLDS - CSP3 § 6.8.3 "Get fetch directive fallback list" (script/style/img/font/connect/object fall back to
default-src); base-uri § 6.3.1, form-action § 6.4.1, frame-ancestors § 6.4.2 are in no list ("frame-ancestors will not
fall back to the default-src directive") - citation at security.go.

D-B2: HOLDS - CSP3 § 6.4.2.2: "'none' ... roughly equivalent to that header's DENY", and with an enforced frame-ancestors
"the X-Frame-Options header will be ignored" - citation at security.go.

D-B3: DOES NOT HOLD - "The panel's behaviour never depends on the referrer". Fetch Standard § 3.2 "append a request
`Origin` header": for a request whose method is neither GET nor HEAD and whose mode is not "cors", referrer policy
"no-referrer" -> "Set serializedOrigin to `null`". A form POST is a navigation (mode "navigate"), so under the panel's own
`Referrer-Policy: no-referrer` every form POST carries `Origin: null` (htmx XHRs are mode "cors" and keep their origin).
originAllowed() turns "null" into a refusal whenever Sec-Fetch-Site is absent, and Fetch Metadata § 3 appends
Sec-Fetch-Site only for a potentially trustworthy URL; MDN bcd 6.0.0: Sec-Fetch-Site from Chrome 76, Firefox 90,
Safari 16.4. Concrete cases: (a) Safari/iOS 16.3 or older over HTTPS - every POST, /login included, is "403
cross-origin request rejected"; (b) any current browser on a plain-HTTP deployment at a name other than localhost (the
CookieSecure=false setup the code supports) - the same. Production over HTTPS with a current browser is unaffected.
No citation written (comment left as it was). Fix (1 line + test): `Referrer-Policy: same-origin` - Fetch § 3.2 then
nulls Origin only when the request is not same origin with its URL, and no referrer leaves for another site, which is
what the comment wants. Tests: a unit test on the header value; a handler test that a POST with only `Origin: null` is
still refused (it must stay so); and on the stand one form POST over plain HTTP at a non-localhost name (or from Safari
16.3) answers 303, not 403. (The citations at B5/B7 point at this entry.)

D-B4: HOLDS - rfc6265bis § 5.2 (same-site per [SAMESITE] "same site"; registrable domain § 2/§ 8.9), Fetch Metadata § 2.3
(the value algorithm; "none" for a user-initiated navigation), Fetch § 2.2.2 (a `Sec-` name is a forbidden
request-header) - citations at security.go originAllowed.

D-B5: PARTLY - "Older browsers send only Origin" holds but is incomplete: Fetch Metadata § 3 "If r's current URL is not a
potentially trustworthy URL, return" - a current browser over plain HTTP at a non-localhost name sends only Origin too.
- comment completed and cited at security.go; the Origin value it then carries is D-B3.

D-B6: HOLDS - Fetch § 3.2: Origin "is used for all HTTP fetches whose request's response tainting is "cors", as well as
those where request's method is neither GET nor HEAD"; MDN bcd 6.0.0 http.headers.Origin: Firefox before 70 is the
browser that omitted it on a same-origin form POST - citation at security.go (the accepted risk unchanged).

D-B7: PARTLY - holds: Fetch § 2.2.5 "serializing a request origin" returns "null" when the redirect-taint is not
same-origin; HTML § 7.1.1: an opaque origin serialises to "null"; url.Parse("null") has an empty Host. Incomplete: the
panel's own form POSTs are sent with "null" under its Referrer-Policy (D-B3). - cited at security.go with the pointer.

D-B8: HOLDS (for the configurations shipped; proxy versions are not pinned by SelfPost) - httpd 2.4 mod_proxy
ProxyPreserveHost; nginx proxy_set_header (default `Host $proxy_host`, the fragment sets `$host`); Caddy 2 reverse_proxy
§ Headers > Defaults: "By default, Caddy passes through incoming headers—including Host—to the backend without
modifications" (v2.11 rewrites Host only for HTTPS upstreams; the Caddyfile proxies to http selfpost:8080); Traefik
service load balancer: "By default, passHostHeader is true" - citation at security.go.

D-B9: HOLDS - RFC 6797 § 8.1 ("If an HTTP response is received over insecure transport, the UA MUST ignore any present
STS header field(s)"), § 6.1.2 includeSubDomains - citation at security.go.

D-B10: HOLDS - rfc6265bis § 4.1.3.2: a __Host- cookie "will have been set with a Secure attribute, a Path attribute with a
value of /, and no Domain attribute" (examples: without Secure it is rejected - so clearing it needs Secure, as
clearSessionCookies does) - citation at auth/handlers.go.

D-B11: HOLDS - RFC 6265 § 5.4: the Cookie header lists every matching cookie, a parent-domain one of the same name
included; for the __Host- name another host cannot plant one (B10), so the case arises only with the unprefixed name -
citation at auth/handlers.go sessionToken.

D-B12: HOLDS - rfc6265bis § 5.8.3 (a cross-site retrieval excludes a Lax cookie unless the request uses a "safe" method
and is a top-level navigation) and § 5.6.7.1; RFC 6265 § 5.2.2 (Max-Age -> expiry now + seconds) and § 5.3 step 11 (same
name/domain/path replaces) - citations at auth/handlers.go setSessionCookie and auth/session.go MaxAge.

D-B13: PARTLY - holds: RFC 9111 § 5.2.2.5, no-store keeps the response out of every HTTP cache, private and shared. Does
not hold: "out of the browser's history copy" / "never stored by the browser" - RFC 9111 § 6: "a history mechanism can
display a previous representation even if it has expired ... it might honor cache directives (e.g., Cache-Control:
no-store)" - might, not must; the HTML back/forward cache is not barred by no-store, and Chrome has been admitting no-store
pages to it. `Pragma: no-cache` in a response: § 5.4, "the meaning of 'Pragma: no-cache' in responses was never
specified". Consequence: after creating an application (or New password), going to another panel page and pressing Back
may show the page again from the tab's memory, password included - same tab, same operator, no disk copy. Fix: comment
corrected (done, comment only: cites what holds, points here); in panel.js, on `pagehide`, blank inputs marked
`data-secret` on the shown-once page so a restored page shows an empty field (about 10 lines + the attribute in the
template); drop `Pragma`. Whether a given browser restores it is a measurement: Chrome/Firefox/Safari current, create an
app, navigate away, Back.

D-B14: HOLDS - RFC 9111 § 4.2.2 (a cache "MAY assign a heuristic expiration time" absent explicit freshness), § 5.2.2.4
(no-cache: "MUST NOT be used to satisfy any other request without forwarding it for validation"); Go 1.26 net/http fs.go
ServeContent(): Last-Modified only for a non-zero modtime, and "If the caller has set w's ETag header ... ServeContent uses
it to handle requests using If-Match, If-None-Match, or If-Range" - citations at view/static.go.

D-B15: HOLDS - HTML form-control-infrastructure.html#constructing-entry-list: a field that is "an input element whose type
attribute is in the Checkbox state and whose checkedness is false" is skipped - citations at handlers_account.go and
handlers_settings.go.

D-B16: NEEDS A MEASUREMENT - HTML links.html#rel-icon: "the user agent must select the most appropriate icon according to
the type, media, and sizes attributes. If there are multiple equally appropriate icons, user agents must use the last one
declared". Whether a 32x32 PNG and a sizeless SVG are "equally appropriate" is the UA's call, so the spec does not settle
"the SVG wins". Measure: open the panel in current Chrome, Firefox and Safari and see which of /static/favicon.svg and
/static/favicon.png is requested and shown (access log or devtools network). If any picks the PNG, adding sizes="any"
to the SVG link makes it the appropriate one for every size.

D-B17: HOLDS - HTML parsing.html#the-end: the deferred scripts execute in list order, then a task fires DOMContentLoaded;
htmx's own ready() therefore runs after panel.js has added its listeners - citation in layout.html's comment.

D-B18: HOLDS - W3C Clipboard API, § Navigator interface: `[SecureContext, SameObject] readonly attribute Clipboard
clipboard` - citation in panel.js.

D-B19: HOLDS - HTML #form-submission-algorithm: submit is fired "with ... the bubbles attribute initialized to true, and
the cancelable attribute initialized to true", and a cancelled event ends the submission (not fired for form.submit()) -
citation in panel.js.

D-B20: HOLDS - HTML rendering.html#hidden-elements: `[hidden]:not([hidden=until-found i]):not(embed) { display: none; }` in
the UA sheet, overridden by author `.box{display:block}` (vendored bulma.min.css 1.0.4); `.is-hidden{display:none!important}`
- citation in panel.js.

D-B21: HOLDS - HTML popover.html (popovertarget, popovertargetaction="hide", no script); MDN bcd 6.0.0
html.global_attributes.popover: Chrome 114, Firefox 125, Safari 17 - citation at view/components.go Sheet, with that
floor stated.

### 4. Go html/template

D-T1: HOLDS (within the package's security model) - Go 1.26 html/template doc.go "Security Model": template authors are
trusted, Execute's data is not; attr.go attrType(): an unknown attribute that does not start with "on" and does not
contain src/uri/url is contentTypePlain, so hx-get, hx-swap, popovertarget values are escaped as attribute text but not
URL-filtered. All hx-get values are server-built paths (.Poll, .PollURL, a literal) - citation at security.go, with the
caveat stated.

D-T2: HOLDS - Go 1.26 text/template funcs.go HTMLEscape(): escapes NUL, ", ', &, <, > (html/template.HTMLEscapeString
calls it) - citation at view/components.go wbrAt.

### 5. Postfix (and rspamd) claims in help/health text - help.go not edited

D-P1: PARTLY - holds: with default_action=tempfail (build/postfix-config.sh:250), a sender check that cannot be reached
makes milter8.c milter8_comm_error() set the persistent reply "451 4.7.1 Service unavailable - try again later" (state
MILTER8_STAT_ERROR); smtpd.c :5789 logs the CONNECT event as milter-reject (one line per connection, which is what Health
counts) but still greets 220 (:5796-5809: only 5xx and 421 change the banner); thereafter STARTTLS and AUTH get "454 4.3.0
Try again later" (smtpd.c starttls_cmd, smtpd_sasl_proto.c:171-182, via milter_other_event) and MAIL FROM gets the 451
(milter8_mail_event returns def_reply). So help.go:174's "refuses from the connection on, before a message is offered"
and "replies with a temporary error" are right. Not right: health.go:178 "Postfix defers mail instead of accepting it
unchecked" - in Postfix's vocabulary "deferred" is the deferred queue, and the message is never queued; the client keeps
it. Proposed wording (health.go WithSenderCheck note): "While the panel does not answer, Postfix answers the sending
application with a temporary error instead of accepting mail unchecked, and the application tries again later." Same for
help.go:174 "how many messages were deferred in the middle" -> "how many messages were refused with a temporary error in
the middle". (Not edited: visible wording.)

D-P2: HOLDS - qmgr/qmgr_active.c qmgr_active_done_3_generic() :556-566: delay = time already in the queue, clamped to
[minimal_backoff_time, maximal_backoff_time], so the first retry comes after the minimum and each gap equals the message's
age - it doubles up to the cap; qmgr_active_defer() only moves the file's time stamp; no attempt count is kept; the queue
lifetime bounds it. (Retries also wait for the next deferred-queue scan, queue_run_delay.) Citation belongs in help.go -
not mine; recorded here.

D-P3: PARTLY - holds: smtp/smtp_connect.c:888 `lookup_mx = (... && *dest != '[')` (no MX lookup for [host]:port);
tls/tls_level.c: none/may/encrypt; with encrypt and no STARTTLS offered, smtp_proto.c:817-831 fails the site with 4.7.4 /
4.7.5 / 4.7.0, i.e. defers; an unanswering upstream defers as any mail. Does not hold in one case: an upstream that offers
STARTTLS and answers it with a 5xx makes smtp_proto.c:801-805 fail the site with the server's own 5xx reply, and
smtp_trouble.c smtp_bulk_fail() bounces (soft_error is false) - "Required holds the mail until a TLS session is possible"
is not true then. Proposed wording (help.go:199): "Required sends only over TLS: while the upstream does not offer it or
the TLS handshake fails, the mail waits in the queue; an upstream that refuses STARTTLS outright with a permanent error
bounces it." (Not edited.)

D-P4: HOLDS for the image deploy/antispam pins (rspamd/rspamd:3.11, today 3.11.1) - conf/options.inc:
`map_watch_interval = 5min; map_file_watch_multiplier = 0.1;` (the compiled default of the multiplier, cfg_utils.cxx:80, is
1 - the shipped config is what makes it fast); src/libserver/maps/map.c: a file map's poll_timeout = map_timeout x
multiplier (30 s), and rspamd_map_watch() puts an ev_stat on the file with interval poll_timeout x multiplier (3 s);
rspamd_map_on_stat() reschedules with RSPAMD_MAP_SCHEDULE_INIT, whose timeout is 0 - the file is read at once. So "within
seconds" (about 3). Caveat: "3.11" is a moving tag (Part C R12). Citation belongs in help.go/deploy - recorded here.

D-P5: HOLDS - RFC 5280 § 6.1.3 (a)(2): path validation checks that the current time is within the certificate's
validity period; a client that verifies refuses an expired certificate. Help text, not edited.

### Lists

DOES NOT HOLD (whole or in part):
- D-S2 (PARTLY): premise "modernc serialises writes" false; comment corrected to the real reason (done).
- D-S3: migrations can run twice across processes -> `_txlock=immediate` + re-read user_version inside the transaction;
  test with two concurrent Opens behind a held write lock.
- D-S20: Postfix does not change recipient case -> drop the hedge; match the recipient exactly (UpdateStatus, and
  logtail.go:321 EqualFold); test with User@x / user@x in one message.
- D-S32 (PARTLY): a bound status would also use the partial index after SQLite's automatic re-prepare; no code change, the
  notify.go citation states only what holds; EXPLAIN test in audit stage 3.
- D-H2 (PARTLY): not silent - logged and the filter dropped; comment corrected (done).
- D-H3 (PARTLY): htmx:load per top-level swapped element; comment corrected (done).
- D-H5, D-H6, D-H9: known WRONG, audit M11 stage 4 (confirmed in src/htmx.js).
- D-H7: beforeRequest guard never matches (same cause) -> fold into M11: `source: el` in htmx.ajax, handle
  sendError/timeout too.
- D-B3: Referrer-Policy no-referrer makes every form POST carry Origin: null -> 403 wherever Sec-Fetch-Site is absent
  (Safari < 16.4, plain HTTP at a non-localhost name) -> `Referrer-Policy: same-origin`; header test + one POST on the
  stand.
- D-B5 (PARTLY), D-B7 (PARTLY): comments completed and pointed at D-B3 (done).
- D-B13 (PARTLY): no-store does not govern the back/forward history; Pragma has no meaning -> comment corrected (done);
  blank `data-secret` fields on pagehide, drop Pragma.
- D-P1 (PARTLY): health.go "Postfix defers mail" -> proposed wording above.
- D-P3 (PARTLY): a 5xx answer to STARTTLS bounces under Required -> proposed wording above.

NEEDS A MEASUREMENT:
- D-S7: the longest write transaction against busy_timeout 5000 ms, on a database at the retention limit (migration 0008's
  UPDATE, DeleteSendLogBefore, PruneSendLogLines, InsertSendLogLines; SQLITE_BUSY in dmarc-ingest).
- D-B16: which favicon current Chrome, Firefox and Safari pick (PNG sizes=32x32 vs SVG without sizes).
- (D-B13's history part, for the record: whether each browser restores the shown-once page from the back/forward cache.)

## Answers E - the claims of architecture.md, guide.md, security.md (register Part D sections 6, 7, 8)

Read 2026-10-14 against the tree at `a8b6276` (the three documents are unchanged since the inventory at `0f984ff`, so the register's line numbers hold).
Sources: `dev/upstream/` (postfix 3.7.11-0+deb12u1, opendkim 2.11.0~beta2-8+deb12u1, cyrus-sasl2 2.1.28+dfsg-10, supervisor 4.2.5-1, logrotate 3.21.0-1), go-milter v0.4.1 and modernc.org/sqlite v1.60.1 from the module cache, rspamd **3.11.1** source (github.com/rspamd/rspamd tag 3.11.1; the image tag `rspamd/rspamd:3.11` floats inside 3.11.x), docs.rspamd.com, certbot source (`certbot/src/certbot/_internal/storage.py`) and eff-certbot.readthedocs.io, MDN, the Fetch / Fetch Metadata standards, Docker docs, RFCs.
Postfix paths are relative to `postfix-3.7.11/`. A condensed register bullet that names several lines gets the id of its first line, with the other lines in brackets.

Verdicts: **HOLDS**, **DOES NOT HOLD**, **PARTLY**, **NEEDS A MEASUREMENT**. "Fault: doc" means only the document is wrong; "Fault: code/config" means SelfPost's code or shipped config must change too.

---

### architecture.md

#### Postfix 3.7.11

- **D-arch:19** (hostname must match PTR and certificate) - HOLDS. FCrDNS and the TLS name are both checked against `SELFPOST_HOSTNAME`; the realm part is D-arch:20. Cite beside it: "(SASL realm: Cyrus SASL 2.1.28, common/plugin_common.c `_plug_parseuser()` - no realm given means serverFQDN, which Postfix passes as `$myhostname`)".
- **D-arch:20** (20-21; no hostname -> container id, realms split, 535) - HOLDS. With `SELFPOST_HOSTNAME` empty, `build/postfix-config.sh:65` sets `myhostname` to `hostname -f || hostname` (the container id). Postfix hands Cyrus `var_myhostname` as serverFQDN with no user realm (`smtpd_sasl_local_domain=`; src/xsasl/xsasl_cyrus_server.c:338-339 `SASL_SERVER_NEW(..., var_myhostname, NO_AUTH_REALM ...)`). Cyrus then uses serverFQDN as the realm (cyrus-sasl2 common/plugin_common.c:739-744 "Default to serverFQDN"). sasldb keys carry the realm, so a login made under another realm is not found: 535. Cite: "(Postfix 3.7.11, src/xsasl/xsasl_cyrus_server.c; Cyrus SASL 2.1.28, common/plugin_common.c `_plug_parseuser()`)".
- **D-arch:37** (37-38; `postfix start-fg` runs master in the foreground) - HOLDS. conf/postfix-script:151-166: not PID 1, so it runs `$daemon_directory/master -s`, which does not fork (src/master/master.c:440, setsid only). Cite: "(Postfix 3.7.11, conf/postfix-script `start-fg`)".
- **D-arch:67** (67-69; `postconf -MX smtp/inet` removes the stock smtpd) - HOLDS. man/man1/postconf.1 `-X`: "With -M, edit the master.cf configuration file, and remove one or more service entries". Cite: "(postconf(1) -MX)".
- **D-arch:74** (74-75; `smtpd_tls_security_level=encrypt` makes STARTTLS mandatory) - HOLDS. proto/postconf.proto `smtpd_tls_security_level`: encrypt = "Mandatory TLS encryption". Cite: "(postconf(5) smtpd_tls_security_level)".
- **D-arch:77** (77-81; master.cf `-o smtpd_milters=` replaces main.cf's chain on port 25) - HOLDS. master(5) `-o name=value` overrides a main.cf parameter for that service. The chain is set at `build/postfix-config.sh:469`. Cite: "(master(5) -o)".
- **D-arch:83** (83-84, 139-141; per-milter `default_action=accept` is fail-open; "default `milter_default_action` is fail-open (`accept`)") - PARTLY. The per-milter setting holds (README_FILES/MILTER_README "Different settings for different Milter applications"; src/milter/milter8.c:548-549 `accept` -> no reply). Line 140 is wrong as worded: Postfix's `milter_default_action` defaults to `tempfail` (src/global/mail_params.h:3460), and SelfPost sets main.cf to `tempfail` (`build/postfix-config.sh:249`). What defaults to accept is SelfPost's `INBOUND_ANTISPAM_MILTER_ACTION`, written as the milter's own `default_action` (`build/postfix-config.sh:417`). Proposed for 139-141: "Optional `INBOUND_ANTISPAM_MILTER` only when inbound relay is enabled (not on DMARC-only port 25); it is attached with its own `default_action`, `INBOUND_ANTISPAM_MILTER_ACTION`, which is `accept` (fail-open) unless set to `tempfail`." Fault: doc.
- **D-arch:85** (85-88, 97-99; milters are called in turn at end-of-message, each sees what the one before added, a refusal stops the chain) - HOLDS. src/milter/milter.c:576 `milter_message()`: `for (resp = 0, m = milters->milter_list; resp == 0 && m != 0; m = m->next)`. Each milter reads the queue file from `data_offset`, which cleanup has already edited for the milter before (src/cleanup/cleanup_milter.c edit callbacks). A refusal is a non-null `resp` and ends the loop. MILTER_README: "applied in the order as specified, and the first Milter application that rejects a command will override the responses from other Milter applications". Cite: "(Postfix 3.7.11, src/milter/milter.c `milter_message()`)".
- **D-arch:100** (100-104; `syslog_name` gives `postfix/inbound/smtpd` and cleanup stays `postfix/cleanup`; smtpd logs `reject:`, cleanup logs milter-reject/-hold/-discard under the queue id) - HOLDS. The tag is `syslog_name` + "/" + the program (src/global/mail_task.c `mail_task()`). The `-o syslog_name` belongs to the smtp/inet service only, and cleanup is its own master.cf service. cleanup's line is `"%s: %s: %s from %s[%s]: %s;"` with queue id and action (src/cleanup/cleanup_milter.c:2113-2114). smtpd's `reject:` comes from src/smtpd/smtpd_check.c `log_whatsup()`. Cite: "(Postfix 3.7.11, src/global/mail_task.c; src/cleanup/cleanup_milter.c:2113)".
- **D-arch:106** (106-108; a cleanup refusal line names one recipient) - HOLDS. src/cleanup/cleanup_milter.c:2119-2121 appends ` to=<state->recip>`, and src/cleanup/cleanup_addr.c:231-233 overwrites `state->recip` for each recipient, so the line names the last one. Cite: "(Postfix 3.7.11, src/cleanup/cleanup_milter.c:2119, cleanup_addr.c:233 - the last recipient)". Optional precision: "names one recipient (the last)".
- **D-arch:133** (133-135, 141-143; relay_domains / relay_recipient_maps / reject_unlisted_recipient; check_recipient_access + pipe to dmarc-ingest) - HOLDS. See postconf(5) `relay_recipient_maps`, `smtpd_reject_unlisted_recipient`, `check_recipient_access`, and pipe(8). The config is `build/postfix-config.sh:304-371`. Cite: "(postconf(5) relay_recipient_maps; pipe(8))".
- **D-arch:147** (147-149; sender_login_maps cover the envelope sender only) - HOLDS. src/smtpd/smtpd_check.c:3911 `reject_auth_sender_login_mismatch(state, sender, ...)` is called with the MAIL FROM address. No header is consulted. Cite: "(Postfix 3.7.11, src/smtpd/smtpd_check.c `reject_auth_sender_login_mismatch()`)".
- **D-arch:151** (151-152; the anvil limit is independent of the milters) - HOLDS. src/smtpd/smtpd.c:2576 `anvil_clnt_mail()` runs in `mail_cmd()` before `milter_mail_event()` at :2735, and no milter state enters it. Clients in `smtpd_client_event_limit_exceptions` (default `$mynetworks`, proto/postconf.proto:4956) are exempt. Cite: "(Postfix 3.7.11, src/smtpd/smtpd.c `mail_cmd()`)".
- **D-arch:153** (chroot off for DNS/TLS) - HOLDS. `build/postfix-config.sh:488` `postconf -F "*/*/chroot=n"`, per postconf(1) `-F`. Cite: "(postconf(1) -F)".
- **D-arch:154** (154-156; "Postfix wants the key root-owned and 0600, which the mount cannot give") - PARTLY. README_FILES/TLS_README:80-83 only advises a key "owned by root and not be readable by any other user". Nothing in Postfix enforces it. smtpd loads the key in `pre_jail_init()` (src/smtpd/smtpd.c:6390-6430), as root, so any mode can be read. The copy is still worth having, because it is what gives the advised mode. Proposed: "copied at start into `/etc/postfix/tls-internal`, owned by root and `0600` as Postfix's TLS_README advises (Postfix itself reads the key as root and does not check its mode), which the mount does not give, and Postfix reads the copy." Fault: doc, plus the comment at `build/postfix-config.sh:71` ("Postfix insists...").
- **D-arch:157** (157-159, 941-942, 1013-1014; `postfix reload` brings in a renewed certificate, rewritten maps and a new log file) - HOLDS. postfix(1) `reload`: "Re-read configuration files. Running processes terminate at their earliest convenience". New smtpd processes load the certificate in `pre_jail_init()`, and texthash tables are read when a process starts. MAILLOG_README "Rotating logs" says to "Reload Postfix so that postlogd(8) immediately closes the old logfile". An smtpd already running keeps its old certificate until it exits. Cite: "(postfix(1) reload; README_FILES/MAILLOG_README)".
- **D-arch:185** (185-192; six retry parameters, defaults 300s/4000s/5d/0) - HOLDS. src/global/mail_params.h: `DEF_QUEUE_RUN_DELAY "300s"` (:770), `DEF_MIN_BACKOFF_TIME` = it (:773), `DEF_MAX_BACKOFF_TIME "4000s"` (:777), `DEF_MAX_QUEUE_TIME "5d"` (:781), `DEF_DSN_QUEUE_TIME "5d"` (:789), `DEF_DELAY_WARN_TIME "0h"` (:793). These match `internal/postfix/retry.go:31-40`. Cite: "(Postfix 3.7.11, src/global/mail_params.h:770-793)".
- **D-arch:218** (218-223; `sendmail(1)` mail passes only non_smtpd_milters, Postfix's own bounces pass none) - HOLDS. MILTER_README "Non-SMTP Milter applications": `non_smtpd_milters` applies to sendmail(1) and qmqpd mail. `DEF_INT_FILT_CLASSES` is empty (src/global/mail_params.h:3544), so internally generated mail (bounces) is not filtered. Cite: "(postconf(5) internal_mail_filter_classes, default empty)".
- **D-arch:235** (235-243; the login-map lookup order: address, address without +extension, domain) - HOLDS for SelfPost's config. `MA_FIND_DEFAULT` = FULL | NOEXT | LOCALPART_IF_LOCAL | AT_DOMAIN (src/global/mail_addr_find.h:39-41). The localpart step only applies to local domains, and `mydestination=` is empty (`build/postfix-config.sh:158`). Cite: "(Postfix 3.7.11, src/global/mail_addr_find.h `MA_FIND_DEFAULT`; the localpart-only step needs a local domain, and mydestination is empty)".
- **D-arch:243** (243-244; logins compared without regard to case) - HOLDS. src/smtpd/smtpd_check.c:3934 `strcasecmp_utf8(state->sasl_username, name)`. Cite: the development.md example comment.
- **D-arch:259** (259-266; a milter's reply code reaches the client; a milter that does not answer means a deferral under tempfail) - HOLDS. src/smtpd/smtpd.c `check_milter_reply()` relays the milter's reply. On a timeout or broken socket, src/milter/milter8.c:553-554 answers `451 4.7.1 Service unavailable - try again later` for `tempfail`. Cite: "(Postfix 3.7.11, src/milter/milter8.c `milter8_comm_error()`)".
- **D-arch:284** (284-290; socket down: a warning, then `milter-reject: … 451 4.7.1` at MAIL, `454 4.3.0` at AUTH; the reject line does not name the milter) - HOLDS. A failed connect leaves the milter in `MILTER8_STAT_ERROR` with the default reply (milter8.c:548-561). At CONNECT a 4xx that is not 421 still gets `220` (smtpd.c:5796-5807). Each later event returns the default reply (milter8.c:1936-1939). MAIL then gets 451 and logs `NOQUEUE: milter-reject: MAIL from …` (smtpd.c:2735-2748 -> `check_milter_reply()`, action "milter-reject" at :1771). AUTH maps a 4xx to `454 4.3.0 Try again later` (src/smtpd/smtpd_sasl_proto.c:171-181). Cite: "(Postfix 3.7.11, src/milter/milter8.c `milter8_comm_error()`, src/smtpd/smtpd_sasl_proto.c:171-181)".
- **D-arch:291** (291-295; cleanup's content-timeout warning `can't read SMFIC_HEADER reply packet header`) - HOLDS. It is MEASURED (undated) and now also read in the source: src/milter/milter8.c:589-593 `msg_warn("milter %s: can't read %s reply packet header: %m", ...)`, then `milter8_comm_error()` gives `451 4.7.1`. Past DATA, smtpd hands the milter connections to cleanup (smtpd.c `mail_open_stream()` -> `milter_send()`). Cite: "(Postfix 3.7.11, src/milter/milter8.c `milter8_read_resp()`; seen on the e2e stand, <date>)". The measurement still needs its date.
- **D-arch:296** (296-297; one cleanup serves message after message, so one warning is one message) - HOLDS. cleanup is a reusable server (cleanup(8), `max_use`). Its milter state arrives per message from smtpd (`milter_receive()`), and a failed milter is marked in error for that message only, so it warns once per message. Cite: "(Postfix 3.7.11, src/milter/milter8.c - one warning per failed exchange, the milter state is the message's)".
- **D-arch:323** (323-327, 437-439; `postqueue -j` arrival_time, `-p` lists every id) - HOLDS. src/showq/showq.c:169-242 reads `arrival_time`. showq scans maildrop, active, incoming, deferred and hold (:349-353). Cite: "(Postfix 3.7.11, src/showq/showq.c; postqueue(1) -j)".
- **D-arch:329** (329-331; a MAIL FROM refusal by the milter comes before the login check) - HOLDS. `smtpd_delay_reject` defaults to yes (mail_params.h:2420), so `smtpd_sender_restrictions` (`reject_sender_login_mismatch`) run at RCPT. In `mail_cmd()` the sender check only runs when delay_reject is off (smtpd.c:2724-2725), and the milter MAIL event follows at :2735. Cite: "(Postfix 3.7.11, src/smtpd/smtpd.c `mail_cmd()`; smtpd_delay_reject=yes)".
- **D-arch:352** (352-354; postmaster@hostname receives no mail) - HOLDS. SelfPost config: `mydestination=` and no local delivery (`build/postfix-config.sh:158`). Cite: "(build/postfix-config.sh: mydestination is empty)".
- **D-arch:365** (365-367; postdrop membership without the setgid bit) - HOLDS. conf/postfix-files:64 `$queue_directory/maildrop:d:$mail_owner:$setgid_group:730`, so a member of postdrop may write. Docker's `no-new-privileges` stops setgid from taking effect (kernel no_new_privs: execve does not grant set-user/group-ID). Cite: "(Postfix 3.7.11, conf/postfix-files: maildrop 0730 postfix:postdrop)".
- **D-arch:376** (376, 387-393; postlogd writes as postfix, recreates the log lazily as 0600, writes to the renamed file until reload) - HOLDS. postlogd is unprivileged in master.cf (conf/master.cf:79, runs as `mail_owner`). It opens the file in `pre_jail_init()` (src/postlogd/postlogd.c:212) with `O_CREAT|O_APPEND, 0644` (src/util/logwriter.c:85) under master's `umask(077)` (src/master/master.c:297), giving 0600. Precision: `pre_init` runs **before** the privilege drop (src/master/dgram_server.c:615-623), so a file postlogd creates is owned by root (group selfpost from the setgid directory), not by postfix. "Lazily" holds: a new postlogd is started on the first datagram after a reload. Proposed precision for :387-388: "(postlogd recreates the file on its first line after the reload, as root and mode `0600`, which the unprivileged panel user cannot read)". Cite: "(Postfix 3.7.11, src/postlogd/postlogd.c `pre_jail_init()`, src/util/logwriter.c)".
- **D-arch:397** (397-400, 493-498, 541-543; log line layout; short ids capital hex, long ids without vowels; a short id can be the tail of a longer one) - HOLDS. src/util/msg_logger.c:198-200 `"%b %d %H:%M:%S "` + host + `program[pid]: `. src/global/mail_queue.h:81-100: long ids use an "alphabet ... made safe by removing all the vowels", short ids are hex. A short id is 5 hex digits of microseconds plus the inode in hex, so one can end another. Cite: "(Postfix 3.7.11, src/global/mail_queue.h MQID_*)". "regularly" (:543) is "can": optional wording "a shorter one can be the tail of a longer one".
- **D-arch:420** (420-426, 431-432; the queue id is given before the headers are judged and freed at once on refusal; the NOQUEUE milter-reject format; `client=` carries the login; the END-OF-MESSAGE From line) - HOLDS. smtpd.c:2354-2361 `"%s: client=%s, sasl_method=…, sasl_username=…"`. cleanup_milter.c:2113-2125 is the END-OF-MESSAGE line, and for a 5xx it logs `resp + 4` (the text without the code). The NOQUEUE line comes from smtpd.c `check_milter_reply()`. A refused message's queue file is removed by cleanup. Cite: "(Postfix 3.7.11, src/smtpd/smtpd.c:2354; src/cleanup/cleanup_milter.c:2113)".
- **D-arch:442** (442-444, 452-454, 472-475, 501-506, 519-523; nothing is reported after the message leaves; short ids are reused; a message may stay past the lifetime by up to a backoff; the `removed` line; client=/message-id= are logged before end-of-message; lifetime = max(maximal_, bounce_queue_lifetime)) - HOLDS. src/qmgr/qmgr_active.c:450-460: expiry is tested after a delivery attempt (`event_time() >= create_time + (*sender ? var_max_queue_time : var_dsn_queue_time)`), which is up to one backoff late. Null-sender mail uses `bounce_queue_lifetime`. message-id is logged by cleanup while the headers arrive (src/cleanup/cleanup_message.c:630, 728). Cite: "(Postfix 3.7.11, src/qmgr/qmgr_active.c `qmgr_active_done_25_generic()`)". Related, not this doc's wording: on expiry Postfix writes one `<id>: from=<…>, status=expired, returned to sender` line with no `to=`. See D-guide:1264 and register A-8.
- **D-arch:532** ("six to ten lines of about 200 bytes per message") - NEEDS A MEASUREMENT. On a copy of the live database: `SELECT count(*)*1.0/count(DISTINCT queue_id), avg(length(line)) FROM send_log_lines;`.

#### OpenDKIM 2.11.0~beta2

- **D-arch:29** (starts as root, drops to `UserID`) - HOLDS. `build/opendkim.conf` `UserID opendkim`. opendkim.conf(5) UserID: it becomes that user after opening its socket. Cite: "(opendkim.conf(5) UserID)".
- **D-arch:169** (169-172, 230-231; the SigningTable chooses by `From:`) - HOLDS. opendkim/opendkim.c:7348-7372: `SenderHeaders` unset leaves the default of the From header, and the signing table is applied to the From address (`dkimf_apply_signtable`). Cite: "(OpenDKIM 2.11.0~beta2, opendkim/opendkim.c `dkimf_apply_signtable()`)".
- **D-arch:247** (247-249; OpenDKIM has its own From parser) - HOLDS. libopendkim/dkim-mailparse.c `dkim_mail_parse()`. Cite: "(OpenDKIM 2.11.0~beta2, libopendkim/dkim-mailparse.c)".
- **D-arch:941** (a reload re-reads the tables) - HOLDS. opendkim/opendkim.c:5714-5717: SIGUSR1 sets `reload` when started with a config file (`-x`, build/supervisord.conf), and `dkimf_config_reload()` (:8800) reopens KeyTable and SigningTable. Cite: "(OpenDKIM 2.11.0~beta2, opendkim/opendkim.c SIGUSR1 -> `dkimf_config_reload()`)".

#### Cyrus SASL 2.1.28

- **D-arch:989** (19-22, 989; realm = hostname, or sasldb lookups fail with 535) - HOLDS, see D-arch:20. Cite: "(Cyrus SASL 2.1.28, common/plugin_common.c:739-744)".

#### rspamd / milter macros

- **D-arch:88** (88, 92-96; header names `X-Rspamd-Action`, `X-Spamd-Result`; the HMAC routine) - PARTLY.
  - The names hold: rspamd 3.11.1 src/plugins/lua/milter_headers.lua:55-77.
  - HMAC-SHA256 holds: `rspamd_cryptobox_hash.create_specific_keyed(key,'sha256',qid)` is `HMAC_Init_ex(…, EVP_sha256())` (src/lua/lua_cryptobox.c `rspamd_lua_hash_create()`).
  - What fails, found reading milter_headers.lua: both verdict routines start with `skip_wanted()` (:183-194). With the defaults `skip_local = true` and `skip_authenticated = true` (:37-38), they neither add **nor remove** their headers for a client that `ip:is_local()` calls local. That check (src/lua/lua_ip.c `lua_ip_is_local`) includes `local_addrs` = 192.168/16, 10/8, 172.16/12, fd00::/8, 169.254/16, fe80::/10 (conf/options.inc:65). The custom `selfpost-signature` routine runs regardless. So mail from such a client keeps verdict headers **the sender wrote** and gets a **valid** signature. The same applies to a client that docker-proxy turns into the bridge gateway (see D-guide:933).
  - Proposed wording, after the fix: "rspamd writes and signs these headers for every message it lets through - the module's `skip_local`/`skip_authenticated` are turned off, so a sender's own headers are removed whatever his address".
  - Fault: config. Set `skip_local = false;` and `skip_authenticated = false;` in `deploy/antispam/rspamd/milter_headers.conf`, or have the custom routine also remove `X-Rspamd-Action`/`X-Spamd-Result` and sign only when it wrote them. An e2e case from a 172.16/12 client should pin it.
- **D-arch:121** (121-130; a deny entry is a prefilter `reject`, allow is -100, with two prefilters the first decides "seen on rspamd 3.11", deny beats any score, Lists.Usage matches as multimap)
  - **"a prefilter `reject`"** - PARTLY. docs.rspamd.com/modules/multimap, *Map Attributes*: `prefilter` is "not used since Rspamd 3.3", so `prefilter = true;` in multimap.conf is ignored in 3.11. What makes deny a refusal is `action = "reject"`, which "is set as a passthrough result, and the remaining filters are not processed" (*Pre-filter maps*). Proposed: "a deny entry forces the action `reject` (a passthrough result, which rspamd applies whatever the score), an allow entry a symbol weighing -100". Fault: doc. Optionally config too: drop the dead `prefilter = true;` lines from `deploy/antispam/rspamd/multimap.conf`.
  - **"with two prefilters the first decides … (seen on rspamd 3.11)"** - NEEDS A MEASUREMENT. The docs do not say. On the stand: one client matched by both an allow and a deny entry, with allow written as `action = "accept"`, under both rule orders. Record the rspamd 3.11.x patch level and the date.
  - **"deny wins over any score"** - HOLDS. multimap docs *Troubleshooting*: the forced action applies "regardless of the score".
  - **allow -100** - HOLDS (multimap.conf `score = -100.0`).
  - **"Lists.Usage matches as rspamd matches them"** - PARTLY. A `from` map matches "envelope from (or header From if envelope from is absent)" (multimap docs *Map types*). Lists.Usage must take the header From for a null-sender message, or it under-counts. Fault: code, if `internal/inbound` uses the envelope sender only. Check `Lists.Usage`.
- **D-arch:195** (195-196; `{auth_authen}` at MAIL) - HOLDS. `DEF_MILT_MAIL_MACROS` (mail_params.h:3473) has `{auth_authen}`, and SelfPost keeps it (`build/postfix-config.sh:259`). Cite: "(Postfix 3.7.11, src/global/mail_params.h:3473)".
- **D-arch:206** (ABORT on a transaction without end-of-message) - HOLDS. smtpd.c `mail_reset()` -> `milter_abort()` (:2852-2855), which runs on RSET, on a new MAIL after a refusal, and in the session teardown after a dropped connection (smtpd.c:6026-6037). See D-sec:461. Cite: "(Postfix 3.7.11, src/smtpd/smtpd.c `mail_reset()` -> `milter_abort()`)".

#### supervisor 4.2.5

- **D-arch:40** (40-44, 279-281; crashexit on FATAL; RUNNING means live; the start time is the wrapper's) - see D-arch:40-docker for the Docker half. Supervisor half: HOLDS.
  - FATAL comes only after the retries are spent (supervisor/process.py:701-707, `backoff > startretries` -> `give_up()`).
  - RUNNING means the process stayed up `startsecs` (:680-690). It is not a check that the process answers. /healthz says only that.
  - `getProcessInfo` `start` is the supervised process's `laststart`, which is the wrapper's.
  - Cite: "(supervisor 4.2.5, supervisor/process.py `transition()`)".

#### logrotate 3.21.0

- **D-arch:384** (384-388, 410, 470-471, 488-489, 930-931) - HOLDS.
  - copytruncate can lose lines: logrotate.8:445 "there is a very small time slice between copying the file and truncating it, so some logging data might be lost".
  - `create` comes before `postrotate`: logrotate.c `rotateSingleLog()` renames (:2161-2174), then `createOutputFile()` (:2226). `postrotate` runs afterwards in `rotateLogSet()` (:2511-2527).
  - delaycompress keeps `.1` plain (logrotate.8).
  - Daily means a calendar day: logrotate.c:1498-1501 compares `tm_mday/tm_mon/tm_year`. Checked every 6 h, that is once a day, 14 kept.
  - The rename-create tick itself loses nothing: postlogd writes through its descriptor. The loss window is the one :391-395 names.
  - Precision: the state file is `/var/lib/logrotate/status` inside the container. After a recreate a log is first seen as "rotated today" (logrotate.c:466-470), so the first rotation slips a day.
  - Cite: "(logrotate 3.21.0, logrotate.c `rotateSingleLog()`; logrotate(8) copytruncate)".

#### Docker

- **D-arch:40-docker** (40-41; "Docker `restart: unless-stopped` recreates a broken instance") - DOES NOT HOLD as worded. A restart policy restarts **the same container**: same writable layer, same `/run` (docs.docker.com/engine/containers/start-containers-automatically/, *Use a restart policy*). Nothing is recreated. Proposed: "`crashexit` event listener exits the container on any managed process FATAL, so Docker's `restart: unless-stopped` starts it again - the same container: the entrypoint clears what the last run left in `/run` (sockets, the alive mark)." Fault: doc, plus the comments in `build/supervisord.conf:8-10` and `build/crashexit.py` docstring ("recreate it cleanly"). No behaviour change: `build/entrypoint.sh:163` already removes the stale sockets and `.alive`.
- **D-arch:276** (276-278; "under `/run`, so a new container starts without one") - PARTLY. A recreated container starts without the mark. A restarted one (D-arch:40-docker) keeps `/run`, which is not a tmpfs (no `tmpfs:` in deploy/docker-compose.yml). The guarantee is `build/entrypoint.sh:163` `rm -f … /run/selfpost/*.alive`. Proposed: "(the socket's path with `.alive`, under `/run`; the entrypoint removes it at every container start, so a restart begins without one)". Fault: doc.
- **D-arch:366** (no-new-privileges disables setgid) - HOLDS. Docker docs `docker run --security-opt no-new-privileges`, and kernel Documentation/userspace-api/no_new_privs.rst: execve "will not grant privileges", so set-user/group-ID bits are not honoured. Cite: "(Linux no_new_privs; Docker --security-opt no-new-privileges)".
- **D-arch:374** (374-376, 876; the bind mount survives a recreate) - HOLDS. A bind mount is the host directory (docs.docker.com/engine/storage/bind-mounts/). Cite: "(Docker: bind mounts)".

#### SQLite

- **D-arch:454** (454-456; = S10, write-first transaction) - HOLDS. sqlite.org/lang_transaction.html §2.1 says a read transaction that later writes fails with SQLITE_BUSY if another connection wrote in between. In WAL that is SQLITE_BUSY_SNAPSHOT, which the busy handler does not retry (sqlite.org/rescode.html#busy_snapshot). A transaction that writes first waits under `busy_timeout`. Cite: "(sqlite.org/lang_transaction.html; rescode.html#busy_snapshot)".
- **D-arch:511** (511-512; = S12, a commit per line would be a disk sync per line) - HOLDS. modernc sqlite v1.60.1 lib/sqlite.go:3424-3428 `SQLITE_DEFAULT_SYNCHRONOUS = 2`, `SQLITE_DEFAULT_WAL_SYNCHRONOUS = 2`, and the DSN does not change it (internal/store/store.go:32). sqlite.org/pragma.html#pragma_synchronous: in WAL mode FULL syncs the WAL at every commit. Cite: "(SQLite synchronous=FULL in WAL: one sync per commit; pragma.html#pragma_synchronous)".
- **D-arch:516** (516-518; about a second per million lines) - NEEDS A MEASUREMENT. On a copy of the database with about 1M `send_log_lines` rows, time `PruneSendLogLines` (or its DELETE) and record the date and host.
- **D-arch:887** (887-888; = S5/S6, user_version/application_id) - HOLDS. sqlite.org/pragma.html#pragma_application_id: a 32-bit integer, 0 when never set. user_version is stored in the header and transactional. Cite: "(sqlite.org/pragma.html#pragma_application_id)".
- **D-arch:754** (754-755; = S14, `ON DELETE CASCADE`) - HOLDS, with `foreign_keys(ON)` on every connection (store.go:32). sqlite.org/foreignkeys.html §4.3. Cite: "(sqlite.org/foreignkeys.html; enforced because the DSN turns foreign_keys on)".
- **D-arch:924** (924-926; "The panel is the only writer; … VACUUM INTO for a live backup") - PARTLY.
  - VACUUM INTO holds: sqlite.org/lang_vacuum.html#vacuuminto, "the generated output database is a consistent snapshot of the original database".
  - "The panel is the only writer" does not hold: `dmarc-ingest`, a separate process Postfix starts through pipe(8) (`build/postfix-config.sh:328`), writes the same file. :455 of the same document says so.
  - Proposed: "Two processes write: the panel, on one connection, and `dmarc-ingest`, which Postfix starts per report; SQLite runs in WAL mode and a transaction that must not fail starts with a write (§ Log tailer). A full backup snapshots with `VACUUM INTO`, which gives a consistent copy of a live database, instead of copying the file."
  - Fault: doc.

#### Browser / htmx

- **D-arch:645** (645-649; = H2, H8) - HOLDS.
  - htmx 2 evaluates a trigger filter with `Function` (htmx docs, *Configuration* `htmx.config.allowEval`: disabling eval disables "trigger filters"), and `default-src 'self'` without `'unsafe-eval'` blocks that (CSP3 §script-src).
  - htmx sends `HX-Request: true` on every request it makes (htmx docs, *Request Headers*).
  - Cite: "(htmx 2.0.4: trigger filters are eval'd - config.allowEval; every htmx request carries HX-Request)".
- **D-arch:724** (724-730; = B1, H1) - HOLDS. `htmx.config.includeIndicatorStyles` (htmx docs *Configuration*) is turned off through the `htmx-config` meta. CSP3: `base-uri` and `form-action` do not fall back to `default-src`. Cite: "(CSP Level 3: base-uri/form-action have no default-src fallback; htmx config.includeIndicatorStyles)".
- **D-arch:732** (732-734; browsers send Sec-Fetch-Site and/or Origin on state-changing requests; Origin must name the panel's host) - PARTLY.
  - The rule describes `originAllowed` correctly, and Sec-Fetch-Site holds for current browsers over HTTPS or localhost. Fetch Metadata only sets it for a potentially trustworthy URL (w3c.github.io/webappsec-fetch-metadata §5.1).
  - The Origin fallback holds only for htmx requests (XHR in `cors` mode). A native form POST under the panel's own `Referrer-Policy: no-referrer` carries `Origin: null` (Fetch Standard, "append a request `Origin` header": for a non-GET/HEAD request not in cors mode, `no-referrer` -> serializedOrigin = `null`; MDN *Origin*: "Certain Referrer-Policy values, for requests that use neither GET nor HEAD and aren't made in cors mode (e.g., basic form posts)"). `originAllowed` refuses that (security.go:119-123).
  - So in a browser without Sec-Fetch-Site, a form posted with scripts off is refused.
  - Proposed: "…`Sec-Fetch-Site` must say `same-origin`; without it, `Origin` must name the panel's host - which a browser without `Sec-Fetch-Site` sends for htmx requests only: a plain form post under the panel's `Referrer-Policy: no-referrer` says `Origin: null` and is refused. A request with neither header is let through (an accepted risk)."
  - Fault: doc. Whether code changes too belongs to register B3/B5.
- **D-arch:253** (253-254; a mail program shows `=?utf-8?q?director=40example.org?=` as an address) - HOLDS. RFC 2047 §5(3) allows an encoded-word in a `phrase` (the display name), and its decoded form is `director@example.org`. Displaying the display name is what MUAs do. Cite: "(RFC 2047 §5: an encoded-word may stand in a display name)".

#### Other

- **D-arch:257** (257-258; Go leaves some charsets undecoded) - HOLDS. pkg.go.dev/mime#WordDecoder: with `CharsetReader` nil, only UTF-8, ISO-8859-1 and US-ASCII decode. Others return an error. Cite: "(Go mime.WordDecoder: CharsetReader nil)".
- **D-arch:288** (288-289; "a client brings a message when it connects") - PARTLY. This is a counting convention, not a property of clients. A client may bring several messages per connection or reconnect for the same message. Proposed: "so what is counted is a connection turned away, once - the count of messages it held back is not known: a client may bring one or several, or come back for the same one." Fault: doc.

Counts (architecture.md, 60 verdicts covering the register's 93 line-claims; cross-references not counted): HOLDS 49 · PARTLY 8 · DOES NOT HOLD 1 · NEEDS A MEASUREMENT 2 (the "first prefilter decides" part of :121 is counted under PARTLY there and listed in (c); D-arch:376 HOLDS, with a precision offered in (a)).

---

### guide.md

#### The register's WRONG? list

- **D-guide:227** (`INBOUND_MESSAGE_SIZE_LIMIT` 25 MiB caps port 25 via `message_size_limit`) - DOES NOT HOLD.
  - `build/postfix-config.sh:467` sets it as `-o message_size_limit` on smtp/inet only, which binds smtpd.
  - cleanup is a separate service and keeps main.cf's `message_size_limit`, which SelfPost never sets: default `DEF_MESSAGE_LIMIT 10240000` (src/global/mail_params.h:1997). It calls `set_file_limit(var_message_limit)` (src/cleanup/cleanup_init.c:450-451) and refuses a larger queue file.
  - Port-25 mail between about 9.8 MB and 25 MiB is refused with 552. The same ceiling caps 465/587, which is unstated.
  - Fault: **config**. In `build/postfix-config.sh` set main.cf `message_size_limit` to the largest per-listener limit in use (`max(PORT25_SIZE, 10240000)`), and keep explicit `-o message_size_limit=10240000` (or a new knob) on smtps/submission so their limit is unchanged. Add an e2e check with a 12 MB message on port 25.
  - The doc sentence can stay once fixed.
- **D-guide:228** (228, 621, 624, also 226; "maximum messages one client IP may submit per window"; "an anvil limit per connecting client IP") - PARTLY.
  - proto/postconf.proto:5019-5025 `smtpd_client_message_rate_limit`: "message delivery requests that any client is allowed to make to this service per time unit, regardless of whether or not Postfix actually accepts those messages".
  - anvil keys on service + address (src/global/anvil_clnt.c:170 `ANVIL_IDENT(service, addr)`, called with `state->service`, smtpd.c:2576), so 465 and 587 count apart.
  - Proposed :228: "Level-1 backstop: how many times one client IP may start a message (`MAIL FROM`) on one listener per window, accepted or not; 465 and 587 count separately (Postfix `smtpd_client_message_rate_limit`)."
  - Proposed :624: "This is an anvil limit per client IP and listener."
  - Same correction for :226 ("per-client-IP cap on MAIL FROM requests").
  - Fault: doc.
- **D-guide:301** (301-303; `docker compose … run --rm certbot certonly --webroot …`) - DOES NOT HOLD.
  - The certbot service's `entrypoint: sh -c '… certbot renew …'` (deploy/nginx/docker-compose.nginx.yml:57) receives the run arguments as `$0 $1 …` of `sh -c`, which ignores them. The command runs the renew loop and never issues anything (docs.docker.com/reference/compose-file/services/#entrypoint: command args go to the entrypoint).
  - Also, `--webroot` needs nginx serving `/.well-known/acme-challenge` on port 80, and at this point nginx is not running.
  - Proposed: `docker compose -f docker-compose.yml -f nginx/docker-compose.nginx.yml run --rm -p 80:80 --entrypoint certbot certbot certonly --standalone -d mail.example.com --email you@example.com --agree-tos --no-eff-email`, with the sentence "port 80 must be free while it runs".
  - Fault: doc. A config fix is possible too: a one-shot service, or document `--entrypoint` in the fragment's comment at :18.
- **D-guide:578** (578-579; upgrade = bump the tag, `docker compose up -d`) - PARTLY.
  - Compose pulls a missing image (`pull_policy` default `missing`, docs.docker.com/reference/compose-file/services/#pull_policy).
  - GHCR has no 2.x tag. Measured 2026-10-14: `ghcr.io/v2/mixeme/selfpost/tags/list` -> 1.0.0, 1.3.0 (with -amd64/-arm64).
  - So `up -d` works only after building the new tag locally.
  - Proposed: "Bump the pinned image tag in `docker-compose.yml` to the target release, build that tag as under [Fixed image tag](#fixed-image-tag) while no 2.x image is published, then `docker compose up -d`."
  - Fault: doc.
- **D-guide:788** (788-790; `docker compose pull && docker compose up -d`; sample message with 1.2.3/1.5.0) - DOES NOT HOLD. `pull` of a tag GHCR does not have fails, and `&&` stops there (measured 2026-10-14, as above). The sample shows a 1.x restore, which :821 says cannot happen in 2.x. Proposed: "Fix the tag in `docker-compose.yml` (build that tag if it is not published), then `docker compose up -d` again". Proposed sample: `backup: this backup was created by SelfPost 2.5.0 but this image is 2.5.1 — restore into the matching image (selfpost:2.5.0)`, taken from the code's actual format string. Fault: doc.
- **D-guide:877** (877-881; `docker exec -e SELFPOST_BACKUP_PASSWORD="$PW"`) - DOES NOT HOLD. The host shell expands `$PW` into the docker CLI's argv, which every user on the host can read in `ps` (proc(5) `/proc/[pid]/cmdline`). That is exactly what :877-878 says must not happen. docs.docker.com/reference/cli/docker/container/exec/ `-e`: a bare `-e NAME` takes the value from the client's environment. Proposed: `SELFPOST_BACKUP_PASSWORD="$PW" docker exec -e SELFPOST_BACKUP_PASSWORD <container> selfpost-backup > backup.spbk` ("`-e` with the name only: the value is taken from your shell's environment, not written on the command line"). Fault: doc.
- **D-guide:258** (258, 263-265; certbot's files are "PEM files ready to bind-mount, no extraction step") - DOES NOT HOLD. certbot writes `live/<name>/*.pem` as **relative** symlinks into `../../archive/<name>/` (certbot `_internal/storage.py` `_relpath_from_file()` / `os.symlink(_relpath_from_file(...))`). Bind-mounted alone, they dangle inside the container. Then `build/postfix-config.sh:81` finds no file and Postfix runs without TLS. Proposed: "Host disk, via the certbot Apache plugin - mount `/etc/letsencrypt` read-only and point `TLS_CERT_FILE`/`TLS_KEY_FILE` at `live/<host>/`, or copy the files with `cp -L` from a certbot deploy hook into `./certs`." Fault: **config/doc** (see D-guide:291).
- **D-guide:291** (291-295, 309-310; `ln -s /etc/letsencrypt/live/<host> certs` "is fine"; the nginx fragment bind-mounts certbot's output) - DOES NOT HOLD, for the same reason as D-guide:258.
  - Docker resolves the `./certs` symlink on the host. The files inside are `../../archive/...` links, resolved inside the container against `/etc/postfix/tls`.
  - The panel's full backup reads `certs/` through `/selfpost-deploy`, where the absolute link points at nothing. `os.Stat` fails (internal/backup/backup.go:258-262), and the backup goes out **without** the TLS material, with only a warning.
  - `deploy/nginx/docker-compose.nginx.yml:35` (`./nginx/certbot-etc/live/mail.example.com:/etc/postfix/tls:ro`) has the same fault.
  - Fault: **config**. Mount the whole letsencrypt directory (`./nginx/certbot-etc:/etc/letsencrypt:ro`) and set `TLS_CERT_FILE=/etc/letsencrypt/live/<host>/fullchain.pem`, `TLS_KEY_FILE=…/privkey.pem` in the fragments. Or add a deploy hook that copies with `cp -L` into `./certs` (then the backup holds real files). Make the guide's Apache step the copy hook instead of the symlink.
  - Measure it once on the stand after the change.
- **D-guide:722** (the stopped `tar` of the project "contains the private key") - DOES NOT HOLD with the recommended symlink. GNU tar stores a symbolic link as a link unless `-h`/`--dereference` is given (GNU tar manual §6.3.4 "Symbolic Links"). Proposed cell: "Yes when `certs/` holds the files; a `certs` symlink is stored as a link (add `-h` to follow it)". Fault: doc. It goes away with the D-guide:291 config fix (copied files).
- **D-guide:795** (795-796; re-issue certificates "when the hostname or IP changes") - PARTLY. A DV certificate names DNS names (RFC 5280 §4.2.1.6 subjectAltName dNSName; the CA/B Baseline Requirements validate the name, not the address). An IP change needs DNS (A/PTR) only. Proposed: "re-issue certificates when the hostname changes; a new IP needs only DNS (A record and PTR)." Fault: doc.
- **D-guide:1150** (1150-1151; after a 4xx "applications send again later") - PARTLY. RFC 5321 §4.2.1 says 4yz means the SMTP client SHOULD try again. A program that hands mail to a queue (an MTA, a mail library with a spool) does. One that talks SMTP directly gets an error and must retry itself. Proposed: "Postfix answers with a temporary error (`451`, and `454` to the login): an application that queues its mail sends it again later, one that sends directly gets an error to retry; nothing is accepted unchecked." Fault: doc.
- **D-guide:1179** (1179-1181; "the single most common reason … DKIM passing doesn't help if SPF/DMARC are absent") - DOES NOT HOLD.
  - An aligned DKIM pass alone satisfies DMARC (RFC 7489 §4.2: "DMARC passes if either SPF or DKIM authenticates an aligned identifier"). Without a DMARC record, DMARC is not evaluated at all, and DKIM still counts for reputation.
  - "Single most common" has no source.
  - Proposed: "Publish all three: receivers weigh SPF, DKIM and DMARC together, and large receivers (Gmail, Yahoo) require them for bulk senders - a domain without them is a common way for mail SelfPost delivered correctly to land in spam. **Whenever you add a new domain in the panel, add its DNS records at the same time**, not later."
  - Fault: doc.

#### Postfix 3.7.11

- **D-guide:74** (74-76; Postfix starts without TLS material) - HOLDS. src/smtpd/smtpd.c:6406-6412: "Some TLS configuration errors are not show stoppers… After a show-stopper error, reply with 454 to STARTTLS". master starts.
- **D-guide:223** (223, 934-936, 1251-1252; accept vs tempfail when a milter is down) - HOLDS. src/milter/milter8.c:548-554 and MILTER_README "Milter error handling".
- **D-guide:229** (226, 229, 622; the window is `anvil_rate_time_unit`) - HOLDS. postconf(5) `smtpd_client_message_rate_limit`: "The time unit is specified with the anvil_rate_time_unit configuration parameter". (The "messages" part of :226 is in D-guide:228.)
- **D-guide:625** ("There is no per-IP bypass") - HOLDS for remote clients. `smtpd_client_event_limit_exceptions` defaults to `$mynetworks` (proto/postconf.proto:4956), which SelfPost leaves at Debian's loopback-only value. The debconf default `127.0.0.0/8 [::ffff:127.0.0.0]/104 [::1]/128` is set by debian/postfix.postinst:355-367. Optional precision: "no bypass except the container's own loopback". Clients that docker-proxy maps to the gateway share one key: see D-guide:933.
- **D-guide:336** (336-338; a renewed certificate is served after reload) - HOLDS. postfix(1) reload. smtpd loads the certificate per process. An already running smtpd keeps the old one until it exits.
- **D-guide:424** (424-427, 741-745; Resync/reload re-applies maps) - HOLDS. texthash is read when a process starts. `postfix reload` replaces the processes (postfix(1)).
- **D-guide:432** (432-434; the Queue lists what `postqueue -p` lists) - HOLDS. showq also lists the hold queue (marked `!`) and maildrop (src/showq/showq.c:349-353). Optional wording: "…still trying to deliver, deferring, or holding".
- **D-guide:435** (435-440, 1285-1286; retry parameters; no attempt counter; lifetime) - HOLDS. src/qmgr/qmgr_active.c:450-455 expires on time only, and null-sender mail uses `bounce_queue_lifetime`.
- **D-guide:442** (442-443; `postfix reload` after rotation) - HOLDS. README_FILES/MAILLOG_README "Rotating logs".
- **D-guide:633** (633-637, 721; a live queue copy may hold a message twice or not at all; a stopped copy is safe) - HOLDS. Queue files move between directories while Postfix runs (qmgr(8)). At start, `postfix check` runs postsuper, which repairs queue file names (conf/postfix-script `check`).
- **D-guide:658** (658-659; a root-only key is enough for Postfix) - HOLDS. smtpd reads the key in `pre_jail_init()` as root (smtpd.c:6390-6430), and postfix-config.sh copies it as root.
- **D-guide:917** (917-921; not an open relay, list mode refuses at RCPT, no backscatter) - HOLDS. postconf(5) `relay_recipient_maps` + `smtpd_reject_unlisted_recipient`: the RCPT is refused with 550 before DATA.
- **D-guide:1061** (1061-1065; reason on the delivery line, `dropped: …`) - HOLDS. src/pipe/pipe.c:1051-1065: on success the command's output is appended as `delivered via %s service (%.100s)`. Precision: the text is cut at 100 characters.
- **D-guide:1067** (1067-1070; with DMARC alone `check_recipient_access` permits only report addresses) - HOLDS. `build/postfix-config.sh:353`: `check_recipient_access,texthash:dmarc_recipients,reject`.
- **D-guide:1104** (1104-1106; "Postfix does not tell `Ops` from `ops`") - HOLDS. smtpd_check.c:3934 `strcasecmp_utf8` in the sender-login check. Note: Cyrus sasldb keys are compared byte for byte, so the **login** itself is case-sensitive. Register S21 is about that; this sentence is about the map and holds.
- **D-guide:1117** (1117, 1121-1123; the envelope is checked; `+extension` counts as the address) - HOLDS. `MA_FIND_NOEXT` in `MA_FIND_DEFAULT` (src/global/mail_addr_find.h:39-41) with `recipient_delimiter=+` (`build/postfix-config.sh:213`).
- **D-guide:1125** (the milter's 550 text reaches the client) - HOLDS. src/smtpd/smtpd.c `check_milter_reply()`; cleanup relays the reply (`CLEANUP_MILTER_SET_SMTP_REPLY`, cleanup_milter.c:2103).
- **D-guide:411** (411-414; "turned away … refused before a message is offered") - HOLDS. The client gets `220`, then `451 4.7.1` at MAIL (smtpd.c:5796-5807, 2735-2748), or `454 4.3.0` at AUTH (smtpd_sasl_proto.c:171-181). (The 1149-1151 half is D-guide:1150.)
- **D-guide:1211** (1211-1222; a 4xx at MAIL FROM) - HOLDS. smtpd.c `mail_cmd()` relays the milter's MAIL reply (:2735-2748).
- **D-guide:1264** (1264-1267; "`bounced` (final failure)" … "usually Postfix's word") - PARTLY.
  - A permanent failure is `status=bounced` per recipient (src/global/log_adhoc.c).
  - Expiry of the queue lifetime is **not**: Postfix writes one `<id>: from=<…>, status=expired, returned to sender` line without `to=` (src/qmgr/qmgr_active.c:456-459). Today such a row stays `deferred` (register A-8).
  - Fault: **code** (A-8: map the per-message `status=expired` line to the message's rows). After that fix, the doc sentence can read: "`bounced` (final failure: refused by the receiving server, or given up when the queue lifetime ran out)".
- **D-guide:1277** (1277-1279; NOQUEUE vs queue-id lines) - HOLDS. smtpd.c:2354-2355 `state->queue_id ? … : "NOQUEUE"`.
- **D-guide:1288** (1288-1290; a delivery line gives the receiving server, its reply and the status) - HOLDS. src/global/log_adhoc.c:111-117, 214 (`to=`, `relay=`, `dsn=`, `status=%s (%s)`).

#### OpenDKIM 2.11.0~beta2

- **D-guide:424-dkim** (424-425, 741-745; reload picks up the tables) - HOLDS. opendkim.c:5714-5717 SIGUSR1 -> `dkimf_config_reload()`.
- **D-guide:1118** (1118-1119; the signature is chosen by `From:`) - HOLDS. opendkim.c:7348-7372 (SenderHeaders unset: From), with `SigningTable refile:` (build/opendkim.conf).
- **D-guide:1128** (1128-1134; two programs read the line) - HOLDS. libopendkim/dkim-mailparse.c `dkim_mail_parse()` is OpenDKIM's own parser.

#### Cyrus SASL 2.1.28

- **D-guide:216** (the hostname is the SASL realm) - HOLDS. common/plugin_common.c:739-744, with Postfix passing `$myhostname` (D-arch:20).
- **D-guide:1225** (the login succeeds; the IP check comes later in the milter) - HOLDS. AUTH completes in smtpd (smtpd_sasl_proto.c), and the milter's first look at the sender is the MAIL event (smtpd.c:2735). No milter command covers AUTH: `milter_other_event()` only replays a stored state.

#### rspamd / milter

- **D-guide:222** (`inet:host:port` / `unix:/path`) - HOLDS. MILTER_README "general syntax for listening sockets".
- **D-guide:933** (933-934; the milter sees the real client IP, HELO and PTR) - NEEDS A MEASUREMENT for one case. For IPv4 with Docker's default iptables DNAT the source address is kept, and Postfix passes it in the CONNECT event. A connection carried by docker-proxy (IPv6 to an IPv4-only bridge, per docs.docker.com/engine/network/port-publishing/ "host IPv6 addresses map to the container's IPv4 address … `--userland-proxy=true` (default)") arrives from the bridge gateway. Docker's docs do not say what source the container sees. On the stand: send to port 25 over IPv6 and read `client=` in mail.log and the IP rspamd logs. If it is the gateway, the sentence needs "over IPv4", and anvil (D-guide:625) and rspamd `skip_local` (D-arch:88) are affected too.
- **D-guide:940** (940-958; rspamd writes the verdict into two headers of every message it lets through; it reads the key anew per message; a restart is needed only for its own configuration) - PARTLY.
  - "Every message" fails for clients rspamd calls local (RFC1918, ULA, link-local) or authenticated: `skip_local`/`skip_authenticated` default true (milter_headers.lua:37-38, 183-194), while the signature is still added. See D-arch:88. Fault: config.
  - "Key anew per message" holds: the custom routine does `io.open` on each call (milter_headers.conf).
  - "Restart only for its own configuration" holds.
  - Proposed after the config fix: unchanged. Before it: "… into two headers of every message it lets through from a public address".
- **D-guide:965** (965-966; "rspamd rereads a file within seconds of a change") - PARTLY. rspamd 3.11.1 conf/options.inc:20-22 has `map_watch_interval = 5min; map_file_watch_multiplier = 0.1;`, so a file map is polled every 30 s (src/libserver/maps/map.c:2902 `poll_timeout = map_timeout * map_file_watch_multiplier`), with jitter (docs.rspamd.com/configuration/options: "from this value up to 2x"). Proposed: "rspamd rereads a changed file within about a minute - no restart." Fault: doc.
- **D-guide:966** (966-970; a domain entry excludes subdomains; deny refuses, allow outweighs, deny wins) - HOLDS. `filter = "email:domain"` yields the whole domain and the map lookup is a key match: multimap docs, *From, rcpt and header filters*, where only `email:domain:tld` reduces `user@foo.example.com` to `example.com`. Deny is a forced `reject` "regardless of the score" (*Troubleshooting*), and allow is `score = -100`.
- **D-guide:971** (971-973; "it is the envelope sender, which the sending server states and nobody checks") - PARTLY. A `from` map matches "envelope from (or header From if envelope from is absent)" (multimap docs *Map types*). Proposed: "it is the envelope sender (for a bounce, which has none, the `From:` line) - what the sending server states and nobody checks". Fault: doc. The same precision goes in security.md :430-432.
- **D-guide:984** (984-986; counts depend on the deny message and the allow symbol names) - HOLDS (SelfPost's own matching against `multimap.conf`).

#### supervisord / logrotate

- **D-guide:403** (403, 586-587; RUNNING) - HOLDS. supervisor/process.py:680-690: RUNNING after `startsecs`.
- **D-guide:415** (415-416; ≈ when supervisord cannot say since when Postfix ran) - HOLDS. `getProcessInfo` `start` is the wrapper's `laststart`.
- **D-guide:442-rot** (442-444; daily, 14 kept, checked every 6 h; `notifempty`) - HOLDS. logrotate.c:1498-1501 (calendar day), `rotate 14`, logrotate.8 `notifempty`. See the state-file precision under D-arch:384.

#### Docker

- **D-guide:61** (61-68; extra publishes "can look like open ports in external scans") - NEEDS A MEASUREMENT. With iptables DNAT a SYN to a published port where nothing listens is answered by the container with RST, which looks closed. docker-proxy, however, binds the host port and accepts. On the stand with `SUBMISSION_ENABLE=false`: `nmap -p 587,25 <host>` from outside, and `ss -ltnp` on the host.
- **D-guide:187** (187-190; no 2.x image on GHCR; Compose uses the local image) - HOLDS. Measured 2026-10-14: GHCR tags 1.0.0 and 1.3.0 only. Compose `pull_policy` default `missing` (Compose file reference, services `pull_policy`).
- **D-guide:233** (`TRUSTED_PROXY_CIDR` … "Behind the default Apache host-network setup this is typically the Docker bridge gateway") - PARTLY, wording. Apache runs on the host and reaches the published `127.0.0.1:8080`. The container sees the bridge network's gateway. There is no `network_mode: host`. Proposed: "With Apache on the host (the default), the panel sees the proxy's connections come from the Docker network's gateway, e.g. `172.18.0.1`; check it in the panel's log." Fault: doc. That the address is the gateway is the stand's observation: confirm once.
- **D-guide:233-xff** (the panel trusts the last hop when the TCP peer matches) - HOLDS. Apache mod_proxy appends the client address to `X-Forwarded-For` (httpd.apache.org/docs/2.4/mod/mod_proxy.html, *Reverse Proxy Request Headers*), and the panel takes the last hop.
- **D-guide:585** (585-593; HEALTHCHECK, `docker inspect` health) - HOLDS. build/Dockerfile:122-123 `curl -fsS http://127.0.0.1:8080/healthz`. Dockerfile reference, *HEALTHCHECK*.
- **D-guide:665** (665-666; `docker exec` runs as root) - HOLDS. build/Dockerfile has no `USER`, and `docker exec` defaults to the image's user.
- **D-guide:683** (683-686, 717-719; `down`, then `tar`) - HOLDS. `docker compose down` stops and removes the containers and leaves the bind-mounted `./data`.
- **D-guide:813** (813, 873; `docker exec -i` streams binary) - HOLDS. Without `-t` no TTY is allocated, and stdin and stdout are plain streams (docker exec reference `-i`/`-t`).
- **D-guide:893** (893, 1022; `up -d` recreates on a `.env` change) - HOLDS. Compose recreates a container whose configuration changed (docker compose up reference: "If there are existing containers for a service, and the service's configuration or image was changed after the container's creation, docker compose up picks up the changes by stopping and recreating the containers").

#### SQLite

- **D-guide:663** (663-664, 711-713, 719; `VACUUM INTO` is consistent on a live database) - HOLDS. sqlite.org/lang_vacuum.html#vacuuminto.
- **D-guide:709** (709-711; copying a WAL database while it is written can be inconsistent) - HOLDS. sqlite.org/howtocorrupt.html §1.2-1.4 (a backup copy taken in the middle of a transaction, and a database copied without its `-wal`). Minor: "while the panel and Postfix are writing" - Postfix writes the queue and the log, not SQLite. Harmless.
- **D-guide:719** (a stopped copy is consistent) - HOLDS. After a clean close, or with the `-wal` file beside it, the copy is a valid database (howtocorrupt §1.4: the WAL travels with the file).

#### Browser / reverse proxy / certificates

- **D-guide:260** (Caddy's on-disk path is internal; verify it) - HOLDS. caddyserver.com/docs/conventions#data-directory documents the data directory, and it gives no promise about the certificate subpaths. The guide already hedges.
- **D-guide:261** (Traefik keeps certificates in `acme.json`) - HOLDS. doc.traefik.io/traefik/https/acme/ `storage`.
- **D-guide:268** (268-271; the panel's own headers suffice) - HOLDS. The code sets them (internal/web/security.go). HSTS is only sent with a Secure cookie, which is the default.
- **D-guide:272** (272-276; a proxy that rewrites `Host` "makes the panel reject every form submission as cross-origin") - PARTLY.
  - `originAllowed` does not look at Host when `Sec-Fetch-Site` is present (internal/web/security.go:104-106). Every browser that sends it (MDN *Sec-Fetch-Site* compatibility: Chrome 76, Firefox 90, Safari 16.4) keeps working behind such a proxy, over HTTPS.
  - Host only matters in a browser without Sec-Fetch-Site, for htmx requests: a plain form post there carries `Origin: null` and is refused anyway (D-arch:732).
  - Proposed: "There is one thing the proxy should do: **pass the original `Host` header through**. … A proxy that rewrites `Host` instead breaks the panel in a browser too old to send `Sec-Fetch-Site`: it refuses every change as cross-origin, and the log prints the `Origin` and `Host` it compared."
  - Fault: doc.
- **D-guide:281** (Apache needs `ssl`, `proxy`, `proxy_http`) - PARTLY. `deploy/apache/selfpost-vhost.conf` uses `RewriteEngine`/`RewriteRule` in the `:80` vhost, which is mod_rewrite (httpd docs mod_rewrite). Proposed: "Install Apache with `ssl`, `proxy`, `proxy_http` and `rewrite` enabled." Fault: doc.
- **D-guide:657** (657-658; have whatever renews the certificate re-apply chgrp/chmod, "a certbot deploy hook, for one") - PARTLY. certbot keeps them itself: "Any changes to the group mode or group owner (gid) of this file will be preserved on renewals" (eff-certbot.readthedocs.io, *Where are my certificates?*). Proposed: "certbot keeps the group and mode on renewal; another client (Caddy, Traefik's extraction) has to set them again after each renewal." Fault: doc. The copy hook of D-guide:291 would set them anyway.

#### DNS / mail standards

- **D-guide:217** (RFC 6409 submission on 587) - HOLDS. RFC 6409 §3.1, port 587 (CITED already).
- **D-guide:85** (85-87; local delivery unreliable) - HOLDS. A general fact, hedged ("may be blocked").
- **D-guide:146** (146-147, 216, 420-422, 597-603; PTR/FCrDNS weighed by receivers) - HOLDS. Google *Email sender guidelines*: "Sending domains or IPs must have valid forward and reverse DNS records"; RFC 8601 `iprev`.
- **D-guide:232** (public resolvers report what the internet sees) - HOLDS (the code's default resolver list).
- **D-guide:500** (500-510; notification DKIM/SPF; a strict parent DMARC can refuse them; the hostname as a sending domain) - HOLDS. RFC 7489 §6.6.3 (policy discovery falls back to the organizational domain, `sp=`/`p=`) and §3.1 (alignment).
- **D-guide:513** (513-514, 1176-1177; public mail hosts cannot be external `rua` destinations) - HOLDS. RFC 7489 §7.1: an external destination must publish `<domain>._report._dmarc.<dest-domain>`, which those hosts do not.
- **D-guide:911** (911-915; publish `10 <hostname>.`) - PARTLY. RFC 5321 §5.1: MX records of equal preference are tried in random order. As a **backup** MX beside a primary at 10, mail is load-shared, not backed up. Proposed: "The domain page shows the value to publish (`10 <hostname>.`) - when SelfPost is the backup of another MX, give it a larger number than the primary's." Fault: doc. The panel could show `20` when other MX exist (optional code change).
- **D-guide:971-dns** - see D-guide:971.
- **D-guide:1016** (1016-1019, 1047-1052; publish MX "so reports reach this server") - PARTLY. RFC 5321 §5.1: without an MX the domain's A/AAAA record is the implicit MX, and `SELFPOST_HOSTNAME` has one (the PTR check needs it). Proposed: "Receivers deliver to the address's domain: to its MX, or, without one, to its A record - `SELFPOST_HOSTNAME` has that already; a report address on another domain needs that domain's MX to point here." Fault: doc.
- **D-guide:219** (219 vs 1079; "parse gzip/XML" vs "gzip, zip, or plain XML") - PARTLY, internal inconsistency. The code also reads zip (internal/dmarc/parse.go uses archive/zip), and RFC 7489 §7.2.1.1 names gzip and zip. Proposed :219: "…parse gzip, zip or plain XML…". Fault: doc.
- **D-guide:1128-rfc** (1128-1135; a display name with a comma must be quoted; 5xx is final) - HOLDS. RFC 5322 §3.4 (display-name = phrase; "," is not atext) and RFC 5321 §4.2.1 (5yz permanent).
- **D-guide:1145** (1145-1147; a visitor's address in `From:` breaks DMARC) - HOLDS. RFC 7489 §3.1: the From domain is not aligned with your DKIM `d=` or SPF.
- **D-guide:1164** (1164-1169; SPF example, one selector per domain, `p=none` is monitoring) - HOLDS. RFC 7208 §5 (`a`, `mx`, `ip4`) and RFC 7489 §6.3 (`p=none`: "no specific action").
- **D-guide:1198** (1198-1203; IP warm-up) - HOLDS. Advice, consistent with Google's sender guidelines ("Increase sending volume slowly").

#### Other

- **D-guide:102** (self-signed: clients must skip verification) - HOLDS. No public CA trusts it, unless the client is given the certificate.
- **D-guide:398** (398-402; machine readings "from the kernel's counters") - PARTLY. Inside a container `/proc/meminfo` and `/proc/stat` are the host's, not namespaced, so a container's memory limit is not shown. `/proc/net/dev` is per network namespace, so only the container's interfaces (network_namespaces(7): `/proc/net` shows the namespace's devices). Proposed: "processor and memory of the host (a memory limit set on the container is not reflected), and the container's own network interfaces…". Fault: doc. If the owner wants the limit shown, read cgroup `memory.max` (code).
- **D-guide:877-argv** (877-878; an argument would be visible in the process list) - HOLDS (proc(5) `/proc/[pid]/cmdline`). The example under it is D-guide:877.

Counts (guide.md, 98 register entries -> 83 verdicts here; some register lines share one verdict, cross-references not counted): HOLDS 57 · PARTLY 16 · DOES NOT HOLD 8 · NEEDS A MEASUREMENT 2 (plus D-guide:233's gateway confirmation, listed in (c)).

---

### security.md

#### The register's WRONG? list

- **D-sec:91** (91-92; sasldb2 stores the password "in the form SASL requires (not plaintext held by the panel)"; a lost password "can only be reissued") - DOES NOT HOLD.
  - `saslpasswd2` without `-n` stores the plaintext `userPassword` property: cyrus-sasl2 lib/server.c:161-186 `sasl_setpass()`, "Do we want to store SASL_AUX_PASSWORD_PROP (plain text)?", taken unless `SASL_SET_NOPLAIN`, which utils/saslpasswd.c:323/426 sets only for `-n`. SelfPost passes `-p -c -f … -u … --` (internal/app/sasl.go:56).
  - The panel reads it back with `db_dump` for exports (sasl.go:84-153).
  - Proposed: "In `sasldb2` it is stored in plaintext - Cyrus keeps `userPassword` as given (Cyrus SASL 2.1.28, lib/server.c `sasl_setpass()`), and a domain export reads it back from there. The panel never shows it again: a lost password is reissued. `sasldb2` is therefore as secret as the exports (§ Backup and domain export)."
  - Fault: doc. Storing without plaintext (`-n`) would break exports and PLAIN/LOGIN against sasldb.
- **D-sec:272** (272-273; the HELO name after `; from=<` is "the client's text, logged as written") - PARTLY. smtpd stores the HELO argument as `printable(argv[1].strval, '?')` and then `neuter(…, NEUTER_CHARACTERS, '?')`, with `NEUTER_CHARACTERS " <>()\\\";@"` (src/smtpd/smtpd.c:1599, 1844-1845, 1929). It cannot carry a space, `<`, `>`, `;` or `@`. The conclusion (cut the reply at Postfix's own `; from=<`) still holds. Proposed: "so that the HELO name after it - the client's text, which Postfix logs with non-printable characters and ` <>()\";@` turned into `?` (Postfix 3.7.11, src/smtpd/smtpd.c `helo_cmd()`) - cannot stand in…". Fault: doc. The same correction applies to :44-46's "a client's HELO name could supply the sender, recipient and reply". That was found and fixed. Whether it was exploitable given neutering is for the review's history, not a change now.
- **D-sec:472** (472-473; mail.log holds "neither message bodies nor headers") - DOES NOT HOLD.
  - cleanup logs the `Message-ID` header (src/cleanup/cleanup_message.c:630, 728) and `resent-message-id` (:632).
  - Milter refusal lines carry the milter's reply (cleanup_milter.c:2113-2114, smtpd.c `check_milter_reply()`). The sender check's reply quotes the `From:` line, in printable ASCII and cut.
  - :296 of this document says the message id is logged.
  - Proposed: "The log holds envelope addresses, client IPs and SASL logins, the `Message-ID` of each message and the replies given, which for a refused `From:` line quote it (printable ASCII, cut) - no message bodies and no other headers;".
  - Fault: doc.
- **D-sec:509** (509-511; "Supervisord restarts the panel at once and takes the container down after three failed starts") - PARTLY.
  - A panel that ran past `startsecs` (1 s by default) and then died is restarted at once (supervisor/process.py transition: EXITED -> `spawn()` with `autorestart=true`).
  - One that dies within a second goes to BACKOFF with waits of 1, 2, 3 s (process.py:169-172). With `startretries=3`, FATAL comes after the **fourth** such start (:701 `backoff > startretries`).
  - It is SelfPost's `crashexit` listener (build/supervisord.conf `[eventlistener:crashexit]`), not supervisord, that ends the container.
  - Proposed: "Supervisord starts a panel that died again at once; one that dies within a second of starting is tried four times in a few seconds, and then SelfPost's `crashexit` listener takes the container down (supervisor 4.2.5, `startretries=3`), so the outage is seconds or the whole instance;".
  - Fault: doc.

#### MEASURED

- **D-sec:41** (41-43; limits held for the first message only, reproduced and fixed) - HOLDS. A measurement dated in the review of 2026-10-12. The mechanism is read in source: Postfix sends CONNECT once per connection (smtpd.c:5789), and go-milter v0.4.1 replaces the backend after a non-continue reply (session.go:263-266).
- **D-sec:258** (test/e2e sends two MAIL FROM on one connection) - HOLDS (the e2e test).
- **D-sec:213** (213-223; the check's parser agrees with OpenDKIM's, tested by e2e over from-lines-passed/other) - HOLDS, as a measurement. It needs a date in the doc when the doc cites it.

#### Postfix 3.7.11

- **D-sec:31** (31-34; the delivery command could run a hundred at once, now one) - HOLDS. `DEF_PROC_LIMIT 100` (mail_params.h:917), and master.cf maxproc `1` on the dmarc-ingest line (build/postfix-config.sh:328). Cite: "(Postfix 3.7.11: default_process_limit 100; master.cf maxproc 1)".
- **D-sec:199** (199-203, 239-244; not an open relay; envelope only; refused at RCPT; no backscatter in list mode) - HOLDS. smtpd_check.c:3911-3945; postconf(5) `relay_recipient_maps`, `smtpd_reject_unlisted_recipient`. Cite: "(postconf(5) relay_recipient_maps)".
- **D-sec:210** (210-212, 259-260; milter order; tempfail stops the chain) - HOLDS. src/milter/milter.c:576 `milter_message()`; MILTER_README. Cite: "(Postfix 3.7.11, src/milter/milter.c `milter_message()`)".
- **D-sec:236** (236-237, 495-497; local submission meets only non_smtpd_milters and no anvil) - HOLDS. MILTER_README "Non-SMTP Milter applications". anvil is called only by smtpd (smtpd.c:2576, 5728). Cite: "(MILTER_README; anvil is smtpd's)".
- **D-sec:245** (245-248, 406-409; per-milter `default_action`; CONNECT once per connection; AUTH always succeeds, no milter event covers it) - HOLDS. milter8.c:548-554; smtpd.c:5789 (CONNECT at session start). AUTH calls only `milter_other_event()` (smtpd_sasl_proto.c:171), which replays a stored state and sends nothing. Cite: "(Postfix 3.7.11, src/smtpd/smtpd_sasl_proto.c:171 - no milter command for AUTH)".
- **D-sec:255** (`{client_addr}` in `milter_mail_macros`) - HOLDS. SelfPost adds it to the defaults (build/postfix-config.sh:259; mail_params.h:3473). Cite: "(postconf(5) milter_mail_macros)".
- **D-sec:270** (270-273; the reply ends at `; from=<`, then `helo=`) - HOLDS. smtpd_check.c `log_whatsup()` / smtpd.c:1700-1722 `smtpd_whatsup()`: `…: <reply>; from=<…> to=<…> proto=… helo=<…>`, with ` sasl_username=` after helo only for an authenticated session (never on port 25). Cite: "(Postfix 3.7.11, src/smtpd/smtpd.c `smtpd_whatsup()`)". For "as written", see D-sec:272.
- **D-sec:282** (282-283; TLS before AUTH) - HOLDS. 465 is `smtpd_tls_wrappermode=yes`, and 587 has `smtpd_tls_security_level=encrypt` with `smtpd_tls_auth_only=yes`. AUTH is not offered before TLS (smtpd.c ehlo: `smtpd_tls_auth_only || enforce_tls`). Cite: "(postconf(5) smtpd_tls_auth_only)".
- **D-sec:294** (294-297; what is logged per message: client address, login, message id, receiving server, reply) - HOLDS. smtpd.c:2354-2361; cleanup_message.c:630/728; log_adhoc.c:111-117, 214. Cite: "(Postfix 3.7.11, src/global/log_adhoc.c)".
- **D-sec:379** (379-380; absent from `postqueue -p` = left the queue) - HOLDS. showq scans maildrop, active, incoming, deferred, hold (src/showq/showq.c:349-353). Cite: "(Postfix 3.7.11, src/showq/showq.c)".
- **D-sec:394** (394-401; the recipient is in the pipe argv; a dropped message is not bounced because of the exit status) - HOLDS. pipe(8) `${recipient}` and the exit status (0 = delivered); the output goes on the line (src/pipe/pipe.c:1051-1065). Cite: "(pipe(8): exit status 0 is a delivery)".
- **D-sec:470** (470-471; postlogd (user postfix) writes) - HOLDS. conf/master.cf:79 is unprivileged. Precision as in D-arch:376: the file is opened as root at process start (dgram_server.c:615-623). Cite: "(Postfix 3.7.11, conf/master.cf postlog; src/postlogd/postlogd.c)".
- **D-sec:489** (489-490; the MAIL FROM milter refusal comes before the login check) - HOLDS, as D-arch:329.
- **D-sec:493** (493-495; postdrop's setgid does not take under no-new-privileges; only root, postfix and the panel can submit) - HOLDS. conf/postfix-files:64 (maildrop 0730 postfix:postdrop), :139 (postdrop 2755), and no_new_privs. Cite: "(Postfix 3.7.11, conf/postfix-files: maildrop 0730)".
- **D-sec:503** (503-507, 519-521; tempfail with 15 s / 30 s timeouts) - HOLDS. postconf(5) `milter_connect_timeout`, `milter_command_timeout`, and `milter_content_timeout` ("for sending message content … and for receiving the response"). Values from build/postfix-config.sh:268-271. Cite: "(postconf(5) milter_*_timeout)".
- **D-sec:518** (518-519, 518-522; 0660 sockets reached through the group) - HOLDS. unix(7): connecting needs write permission on the socket file. Postfix daemons keep their supplementary groups: src/util/chroot_uid.c `chroot_uid()` calls `initgroups(user_name, gid)`, and postfix is in `selfpost` (build/Dockerfile:71). Cite: "(unix(7); Postfix 3.7.11 src/util/chroot_uid.c keeps supplementary groups)".
- **D-sec:528** (528-532, 537-538; a short id is microseconds + inode and repeats; lifetime about six days; long ids end collisions) - HOLDS. src/global/mail_queue.h:81-100, src/global/mail_queue.c `mail_queue_enter()`, postconf(5) `enable_long_queue_ids`. Six days = 5d lifetime + 1 day (SendLogLines). Cite: "(Postfix 3.7.11, src/global/mail_queue.h MQID_SH_*)".
- **D-sec:542** (542-548; a transport to any host:port) - HOLDS. transport(5) `smtp:[host]:port`, and smtp(8) connects where told. Cite: "(transport(5))".
- **D-sec:102** (escaped writes cannot inject directives) - HOLDS for texthash. Each line is `key whitespace value` (postconf(5) texthash / DATABASE_README), and the validators allow no whitespace or newline (internal/postfix writers). Cite: "(texthash: one key and value per line - the writers refuse whitespace)".

#### OpenDKIM 2.11.0~beta2

- **D-sec:201** (201-206; the key is chosen by `From:` through the SigningTable) - HOLDS. opendkim.c `mlfi_eoh` -> `dkimf_apply_signtable`, SenderHeaders default From (:7348-7372). Cite: "(OpenDKIM 2.11.0~beta2, opendkim/opendkim.c `dkimf_apply_signtable()`)".
- **D-sec:230** (230-233; a display name without a findable `@` signs with the application's key) - HOLDS. `dkim_mail_parse()` (libopendkim/dkim-mailparse.c) takes the angle-addr, and the display name is not looked up. Cite: "(libopendkim/dkim-mailparse.c `dkim_mail_parse()`)".
- **D-sec:495** (495-496, 523-525; notifications signed with the hostname's key; the hostname as a sending domain) - HOLDS. The SigningTable line written by `EnsureServerSigned`; the From address is `postmaster@<hostname>`.

#### Cyrus SASL 2.1.28

- **D-sec:6** (6-7; `--` ends saslpasswd2's option parsing) - HOLDS. utils/saslpasswd.c:306 uses libc `getopt()`, and POSIX/glibc getopt(3): "The special argument "--" forces an end of option-scanning". Cite: "(Cyrus SASL 2.1.28, utils/saslpasswd.c getopt; getopt(3) '--')".

#### go-milter v0.4.1 / rspamd

- **D-sec:248** (248-250; the library replaces the milter's session after a non-continue reply) - HOLDS. go-milter v0.4.1 session.go:263-266 `if !resp.Continue() { m.backend = m.server.NewMilter() }`. Cite: "(go-milter v0.4.1, session.go `HandleMilterCommands()`)".
- **D-sec:461** (461-464; no close callback, so a reservation lives until TTL "if a client disconnects without an explicit Abort") - PARTLY.
  - go-milter has no close callback: the `Milter` interface (server.go:20-60) has none, and `CodeQuit` only ends the session (session.go:211-213).
  - But Postfix itself sends ABORT when a client drops mid-transaction. smtpd's session teardown runs `helo_reset()` and `mail_reset()`, both of which call `milter_abort()` (src/smtpd/smtpd.c:2084-2092, 2852-2855, 6026-6037). go-milter passes SMFIC_ABORT on as `Abort` (session.go:101-107).
  - What is left is an ABORT that never arrives: smtpd killed, the milter connection broken, or a message whose session cleanup took over.
  - Proposed: "**Level-2 in-flight reservations can outlive a broken SMTP session until TTL.** Postfix tells the milters ABORT when a client drops the connection mid-transaction (Postfix 3.7.11, src/smtpd/smtpd.c `mail_reset()` -> `milter_abort()`), and the journal milter releases the reservation then; when that never arrives - smtpd killed, the milter connection lost - go-milter has no close callback, and the reservation is released by TTL…"
  - Fault: doc.
- **D-sec:515** (515-517; an empty packet panics the library) - HOLDS, now read in source. go-milter v0.4.1 session.go:34-56 `readPacket()`: `data := make([]byte, length)` … `Code: data[0]` with `length` 0. The goroutine `session.HandleMilterCommands()` (server.go:130) has no `recover`. Cite: "(go-milter v0.4.1, session.go `readPacket()`: `data[0]` on a zero-length packet)".
- **D-sec:262** (262-268, 51-53; rspamd strips verdict headers when up; the HMAC of the queue id equals the one the milter recomputes) - PARTLY.
  - HMAC-SHA256 holds (rspamd 3.11.1 src/lua/lua_cryptobox.c `rspamd_lua_hash_create()` -> `HMAC_Init_ex(…, EVP_sha256())`; queue id from `task:get_queue_id()`).
  - "Rspamd removes a sender's verdict headers" holds only for a client that is not "local" (D-arch:88): with `skip_local`/`skip_authenticated` at their default true, the routines skip both adding and removing, while the signature routine signs.
  - Fault: **config** (`deploy/antispam/rspamd/milter_headers.conf`: `skip_local = false; skip_authenticated = false;`, or remove the two headers in the custom routine).
  - Proposed after the fix: unchanged, plus "(rspamd's milter_headers with `skip_local`/`skip_authenticated` off, so a sender's own headers are removed whatever his address)".
- **D-sec:277** (277-281; map syntax: whitespace, newline, `#`) - HOLDS. docs.rspamd.com/modules/multimap *Maps content*: `key1 value1`, `# comment`. Cite: "(rspamd multimap, Maps content)".
- **D-sec:430** (430-435; the filter matches the envelope sender; allow outweighs; deny wins) - PARTLY. A `from` map matches "envelope from (or header From if envelope from is absent)" (multimap docs *Map types*), so a bounce is matched by its `From:` line. "Allow outweighs" and "deny wins" hold (*Troubleshooting*: the forced action applies "regardless of the score"). Proposed: "The filter matches the envelope sender (for a bounce, which has none, the `From:` line), which is not authenticated…". Fault: doc.

#### supervisord / logrotate

- **D-sec:194** (`user=panel` drops root) - HOLDS. supervisor/options.py `drop_privileges()`: setuid with the user's supplementary groups, set via `os.setgroups` (:1382-1390). That is how `panel` keeps `selfpost` and `postdrop`. Cite: "(supervisor 4.2.5, supervisor/options.py `drop_privileges()`)".
- **D-sec:378** (`rotate 14` loses older lines) - HOLDS. logrotate.8 `rotate count`: "rotated count times before being removed". Cite: "(logrotate(8) rotate)".

#### Docker / Linux

- **D-sec:312** (312-313; argv is readable by any process in the container) - HOLDS. proc(5) `/proc/[pid]/cmdline`, unless `/proc` is mounted with `hidepid`, which Docker does not do. Cite: "(proc(5) /proc/pid/cmdline; hidepid)".
- **D-sec:374** (374-375; mail.log survives recreation) - HOLDS (bind mount).
- **D-sec:424** (424-425, 440-442, 266-267; the filter is another user in another container; 0755/0644) - NEEDS A MEASUREMENT for the uid. The modes are the code's own. That rspamd runs as a non-root user in `rspamd/rspamd:3.11` is not in SelfPost's docs. On the stand: `docker compose exec antispam id`, and a note of the image digest.
- **D-sec:493-nnp** - see D-sec:493.
- **D-sec:518-unix** - see D-sec:518.

#### SQLite

- **D-sec:155** (155-159; the write comes first and the count after; = S9) - HOLDS. sqlite.org/lang_transaction.html: the first write takes the RESERVED/write lock, which only one connection holds until commit. Cite: "(sqlite.org/lang_transaction.html)".
- **D-sec:159** (159-160; first-user setup is one insert-if-empty statement; = S8) - HOLDS. A single statement is atomic, and writers are serialized (sqlite.org/atomiccommit.html).
- **D-sec:389** (the upsert is delete-then-insert) - HOLDS. The code does DELETE then INSERT in one transaction (internal/store/dmarc.go:85-131). The cascade is per S14.

#### Browser / htmx

- **D-sec:58** (`__Host-` cookie) - HOLDS. RFC 6265bis §4.1.3.2: `Secure`, `Path=/`, no `Domain`. Cite: "(RFC 6265bis §4.1.3.2)".
- **D-sec:107** (107-108, 177; `SameSite`) - HOLDS. RFC 6265bis §5.5.7.1: Lax withholds the cookie on cross-site POST. Note it does not cover a sibling subdomain (same-site), which is the origin check's job (D-sec:335).
- **D-sec:122** (polling GETs carry `HX-Request`) - HOLDS (htmx docs, *Request Headers*).
- **D-sec:174** (174-177, 578-581; `Sec-Fetch-Site` same-origin, or `Origin` names the panel's host) - PARTLY, as D-arch:732. Under the panel's own `Referrer-Policy: no-referrer`, a native form POST carries `Origin: null` (Fetch Standard, "append a request `Origin` header"; MDN *Origin*). So the Origin fallback admits only htmx requests. Proposed for :176 and :580: "…`Sec-Fetch-Site` must be `same-origin`; where a browser does not send it, `Origin` must name the panel's own host - which it does for htmx requests, while a plain form post says `Origin: null` under the panel's `Referrer-Policy: no-referrer` and is refused;". Fault: doc. Code: register B3/B5.
- **D-sec:324** (324-326, 600-602; only old browsers send neither header) - HOLDS. MDN *Origin*: same-origin POSTs carry Origin, "Baseline … since July 2020". Clients that are not browsers (curl) send neither, but they carry no victim's cookie.
- **D-sec:335** (335-337; the origin check closes the neighbouring-subdomain case) - HOLDS. A sibling subdomain is `same-site`, not `same-origin` (Fetch Metadata §2.1, `Sec-Fetch-Site`), so it is refused, and an Origin naming another host fails the host compare. Cite: "(Fetch Metadata: same-site is not same-origin)".
- **D-sec:340** (340-341, 596-597; script in the panel's origin can read a token) - HOLDS (same-origin policy, HTML §7.5).
- **D-sec:341** (341-343, 597-598; CSP + html/template) - HOLDS (CSP3; Go html/template docs, *Contexts*).
- **D-sec:353** (353-354; without JS `data-confirm` forms submit at once) - HOLDS (HTML form submission; nothing intercepts it without script).

#### Go stdlib / crypto

- **D-sec:192** (192-193; html/template auto-escapes) - HOLDS. pkg.go.dev/html/template: "contextually autoescapes".
- **D-sec:75** (crypto/rand token ≥128 bits) - HOLDS. internal/web/auth/setup.go:85 `randomToken(16)`, and pkg.go.dev/crypto/rand: cryptographically secure.
- **D-sec:79** (`subtle.ConstantTimeCompare`) - HOLDS. pkg.go.dev/crypto/subtle: time independent of contents; returns at once on a length mismatch, harmless for a fixed-length token.
- **D-sec:100** (100-101; os/exec runs no shell) - HOLDS. pkg.go.dev/os/exec: "intentionally does not invoke the system shell".
- **D-sec:301** (301-304; scrypt + AES-256-GCM chunked) - HOLDS (SelfPost's own format, internal/secretfile; golang.org/x/crypto/scrypt, crypto/cipher GCM).
- **D-sec:265** (265, 269-270; HMAC-SHA256 cannot be forged without the key) - HOLDS. RFC 2104 §6; it is a PRF under the key.

#### Reverse proxy

- **D-sec:284** (284-285; only trusted proxies supply XFF) - HOLDS. Apache mod_proxy appends the client address (mod_proxy docs, *Reverse Proxy Request Headers*), and the panel takes the last hop only from a trusted peer.
- **D-sec:176** (Origin must name the panel's host, which relies on Host preservation, B8) - HOLDS for the fragments shipped: Apache `ProxyPreserveHost On`, nginx `proxy_set_header Host $host`, Caddy and Traefik pass Host by default (Caddy docs reverse_proxy *Headers*; Traefik docs `passHostHeader` default true). See D-sec:174 for when Host matters at all.

#### Other

- **D-sec:390** (390-391; `rua` reporters are arbitrary third parties) - HOLDS. RFC 7489 §7.1, §12.

Counts (security.md, 76 register entries -> 62 verdicts; cross-references not counted): HOLDS 53 · PARTLY 6 · DOES NOT HOLD 2 · NEEDS A MEASUREMENT 1.

---

### (a) DOES NOT HOLD / PARTLY, with proposed wording

(Precision offered for claims that HOLD, not counted here: D-arch:376 "(postlogd recreates the file on its first line after the reload, as root and mode `0600`…)"; D-arch:106 "names one recipient (the last)"; D-guide:432 "…or holding"; D-guide:625 "no bypass except the container's own loopback".)

DOES NOT HOLD:
- D-arch:40-docker - "…so Docker's `restart: unless-stopped` starts it again - the same container: the entrypoint clears what the last run left in `/run` (sockets, the alive mark)." Also fix the comments in build/supervisord.conf:8-10 and build/crashexit.py.
- D-guide:227 - the doc sentence is right once the config is fixed (see b). Until then: "…the shared listener uses the greater of this and `DMARC_MESSAGE_SIZE_LIMIT`; Postfix's cleanup still caps every message at 10 240 000 bytes" (better fixed than documented).
- D-guide:301 - `… run --rm -p 80:80 --entrypoint certbot certbot certonly --standalone -d mail.example.com --email you@example.com --agree-tos --no-eff-email` ("port 80 must be free while it runs").
- D-guide:788 - "Fix the tag in `docker-compose.yml` (build that tag if it is not published), then `docker compose up -d` again"; the sample message with 2.x versions.
- D-guide:877 - `SELFPOST_BACKUP_PASSWORD="$PW" docker exec -e SELFPOST_BACKUP_PASSWORD <container> selfpost-backup > backup.spbk`.
- D-guide:258 - "Host disk, via the certbot Apache plugin - certbot's `live/` files are links into `archive/`: copy them (`cp -L`) into `./certs` from a deploy hook, or mount `/etc/letsencrypt` and point `TLS_CERT_FILE`/`TLS_KEY_FILE` at `live/<host>/`."
- D-guide:291 - replace the `ln -s` step with the copy hook (or the `/etc/letsencrypt` mount); same for the nginx fragment.
- D-guide:722 - "Yes when `certs/` holds the files; a `certs` symlink is stored as a link (add `-h`)" - moot after the copy hook.
- D-guide:1179 - "Publish all three: receivers weigh SPF, DKIM and DMARC together, and large receivers require them for bulk senders - a domain without them is a common way for mail SelfPost delivered correctly to land in spam."
- D-sec:91 - "In `sasldb2` it is stored in plaintext - Cyrus keeps `userPassword` as given (Cyrus SASL 2.1.28, lib/server.c `sasl_setpass()`), and a domain export reads it back from there. The panel never shows it again: a lost password is reissued. `sasldb2` is as secret as the exports."
- D-sec:472 - "The log holds envelope addresses, client IPs and SASL logins, the `Message-ID` of each message and the replies given, which for a refused `From:` line quote it (printable ASCII, cut) - no message bodies and no other headers;"

PARTLY:
- D-arch:83 (:139-141) - "…attached with its own `default_action`, `INBOUND_ANTISPAM_MILTER_ACTION`, which is `accept` (fail-open) unless set to `tempfail`."
- D-arch:154 - "…owned by root and `0600` as Postfix's TLS_README advises (Postfix itself reads the key as root and does not check its mode), which the mount does not give…"; same for the comment at build/postfix-config.sh:71.
- D-arch:88 - after the config fix: "rspamd writes and signs these headers for every message it lets through - `skip_local`/`skip_authenticated` are off, so a sender's own headers are removed whatever his address."
- D-arch:121 - "a deny entry forces the action `reject` (a passthrough result rspamd applies whatever the score), an allow entry a symbol weighing -100"; Lists.Usage must take the `From:` line for a null-sender message.
- D-arch:276 - "(…under `/run`; the entrypoint removes it at every container start, so a restart begins without one)".
- D-arch:288 - "…a connection turned away, once - how many messages it held back is not known: a client may bring one or several, or come back for the same one."
- D-arch:732 - "…without it, `Origin` must name the panel's host - which a browser without `Sec-Fetch-Site` sends for htmx requests only: a plain form post under the panel's `Referrer-Policy: no-referrer` says `Origin: null` and is refused."
- D-arch:924 - "Two processes write: the panel, on one connection, and `dmarc-ingest`, which Postfix starts per report; … A full backup snapshots with `VACUUM INTO`…"
- D-guide:228 (and :226, :621, :624) - "how many times one client IP may start a message (`MAIL FROM`) on one listener per window, accepted or not; 465 and 587 count separately"; :624 "an anvil limit per client IP and listener".
- D-guide:578 - "…build that tag while no 2.x image is published, then `docker compose up -d`."
- D-guide:795 - "re-issue certificates when the hostname changes; a new IP needs only DNS (A record and PTR)."
- D-guide:1150 - "…an application that queues its mail sends it again later, one that sends directly gets an error to retry; nothing is accepted unchecked."
- D-guide:1264 - after the A-8 code fix: "`bounced` (final failure: refused by the receiving server, or given up when the queue lifetime ran out)".
- D-guide:940 - before the config fix: "…of every message it lets through from a public address"; after it, unchanged.
- D-guide:965 - "rspamd rereads a changed file within about a minute - no restart."
- D-guide:971 - "it is the envelope sender (for a bounce, which has none, the `From:` line)…"
- D-guide:233 - "With Apache on the host (the default), the panel sees the proxy's connections come from the Docker network's gateway, e.g. `172.18.0.1`."
- D-guide:272 - "A proxy that rewrites `Host` instead breaks the panel in a browser too old to send `Sec-Fetch-Site`: it refuses every change as cross-origin, and the log prints the `Origin` and `Host` it compared."
- D-guide:281 - "Install Apache with `ssl`, `proxy`, `proxy_http` and `rewrite` enabled."
- D-guide:657 - "certbot keeps the group and mode on renewal; another client (Caddy, Traefik's extraction) has to set them again after each renewal."
- D-guide:911 - "…when SelfPost is the backup of another MX, give it a larger number than the primary's."
- D-guide:1016 - "Receivers deliver to the address's domain: to its MX, or, without one, to its A record - `SELFPOST_HOSTNAME` has that already; a report address on another domain needs that domain's MX to point here."
- D-guide:219 - "…parse gzip, zip or plain XML…"
- D-guide:398 - "processor and memory of the host (a memory limit set on the container is not reflected), and the container's own network interfaces…"
- D-sec:272 (and :44-46) - "…the client's text, which Postfix logs with non-printable characters and ` <>()\";@` turned into `?` (Postfix 3.7.11, src/smtpd/smtpd.c `helo_cmd()`)…"
- D-sec:509 - "Supervisord starts a panel that died again at once; one that dies within a second of starting is tried four times in a few seconds, and then SelfPost's `crashexit` listener takes the container down (supervisor 4.2.5, `startretries=3`)…"
- D-sec:461 - "…Postfix tells the milters ABORT when a client drops the connection mid-transaction (Postfix 3.7.11, src/smtpd/smtpd.c `mail_reset()` -> `milter_abort()`), and the reservation is released then; when that never arrives - smtpd killed, the milter connection lost - go-milter has no close callback and the reservation is released by TTL…"
- D-sec:262 - after the config fix, add "(milter_headers with `skip_local`/`skip_authenticated` off, so a sender's own headers are removed whatever his address)".
- D-sec:430 - "The filter matches the envelope sender (for a bounce, which has none, the `From:` line)…"
- D-sec:174 (:176, :580) - "…where a browser does not send it, `Origin` must name the panel's own host - which it does for htmx requests, while a plain form post says `Origin: null` under the panel's `Referrer-Policy: no-referrer` and is refused;"

### (b) Need a code or config fix

1. **Port-25 message size** (D-guide:227): cleanup keeps main.cf's `message_size_limit` = 10 240 000 (src/cleanup/cleanup_init.c:450-451), so the 25 MiB inbound limit and a DMARC limit above that are never reached. Set main.cf `message_size_limit` to the largest listener limit in use and pin 465/587 with their own `-o message_size_limit`. Add an e2e test with a 12 MB message on port 25. (`build/postfix-config.sh`)
2. **rspamd verdict headers for "local" clients** (D-arch:88, D-guide:940, D-sec:262): milter_headers `skip_local`/`skip_authenticated` default true (rspamd 3.11.1 milter_headers.lua:37-38, 183-194; `local_addrs` includes RFC1918/ULA, conf/options.inc:65). For such a client the sender's own `X-Rspamd-Action`/`X-Spamd-Result` stay, and the custom routine signs them. Set `skip_local = false; skip_authenticated = false;` in `deploy/antispam/rspamd/milter_headers.conf`, or make the custom routine remove both headers and sign only what rspamd wrote. Add an e2e case from a 172.16/12 client.
3. **certbot symlinks** (D-guide:258, :291, :722): `live/` holds relative links into `archive/` (certbot storage.py). The `./certs -> live/<host>` symlink and `deploy/nginx/docker-compose.nginx.yml:35` dangle inside the container: no TLS for Postfix, and the panel's backup omits `certs/` (internal/backup/backup.go:258-262). Ship a deploy hook that copies with `cp -L` into `./certs` (Apache), and mount `/etc/letsencrypt` with `TLS_CERT_FILE`/`TLS_KEY_FILE` pointing into `live/` (nginx fragment), or the same hook.
4. **Queue-lifetime expiry** (D-guide:1264 = register A-8): Postfix logs `<id>: from=<…>, status=expired, returned to sender` with no `to=` (src/qmgr/qmgr_active.c:456-459), so the rows stay `deferred`. Map that line to the message's rows as bounced (internal/logtail).
5. **Lists.Usage for null-sender mail** (D-arch:121): rspamd's `from` map falls back to the header From when the envelope sender is empty. Lists.Usage should do the same, or say it counts envelope senders only (check internal/inbound).
6. Comment and config tidying, no behaviour change: the "recreate" wording in build/supervisord.conf:8-10 and build/crashexit.py (D-arch:40-docker); the "Postfix insists" comment in build/postfix-config.sh:71 (D-arch:154); the dead `prefilter = true;` lines in deploy/antispam/rspamd/multimap.conf (ignored since rspamd 3.3) (D-arch:121); the nginx fragment's comment showing the certbot `run` without `--entrypoint` (deploy/nginx/docker-compose.nginx.yml:18) (D-guide:301).
7. Optional: the inbound DNS hint `10 <hostname>.` could suggest a larger preference when the domain already has an MX (D-guide:911). Health could read the container's cgroup memory limit (D-guide:398).

### (c) NEEDS A MEASUREMENT

- D-arch:121 - rspamd 3.11.x with two forced actions (allow as `action=accept`, deny as `reject`) on one message, under both rule orders: which one decides. Record the patch level and date.
- D-arch:532 - on a copy of the live DB: `SELECT count(*)*1.0/count(DISTINCT queue_id), avg(length(line)) FROM send_log_lines;`.
- D-arch:516 - time `PruneSendLogLines` on a copy with about 1M `send_log_lines` rows.
- D-arch:291 - already measured, needs its date in the doc ("seen on the e2e stand, <date>").
- D-guide:61 - from outside, `nmap -p 25,587 <host>` with both port-25 flags off and `SUBMISSION_ENABLE=false`, plus `ss -ltnp` on the host: do the unused published ports look open or closed?
- D-guide:933 (also affects D-guide:625 and D-arch:88) - send to port 25 over IPv6 (and from the host itself). What client address do mail.log's `client=` and rspamd see: the real one, or the bridge gateway (docker-proxy)?
- D-guide:233 - confirm the address the panel sees behind host Apache (expected: the compose network's gateway).
- D-sec:424 - `docker compose exec antispam id` (and the image digest): does rspamd run as a non-root user other than the panel's?

