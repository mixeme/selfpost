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
   code or a document, the fix. Answers are added under each part.

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

