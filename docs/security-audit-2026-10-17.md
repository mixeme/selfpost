# Security audit — 2026-10-17

**What this is.** A full security audit of SelfPost at `44bf09d` (release
`2.14.3`): the code tree, the image and its process model, the deployment
fragments, CI/CD and the supply chain, the dependencies, and the live
installation on `selfpost.mixfed.ru` (read-only, plus a handful of harmless
probes from outside). Nothing in the tree was changed; this file is the only
output. The code audit of 2026-10-12 ([audit-2026-10-12.md](audit-2026-10-12.md))
looked for defects and drift; this one looks for what an attacker gets.

**Method.** Four parallel readers, one per zone (authentication, sessions,
users and request security; the HTTP surface, templates, backup/export/import;
the mail path — Postfix configuration, the four milters, SASL, DKIM, inbound,
DMARC ingest, the log parsers; image, process model, filesystem, deployment,
CI, supply chain), each reading every file of its zone and its tests in full,
each holding the code to every sentence of [security.md](security.md) and
[architecture.md](architecture.md), each told to prove a finding (a throwaway
test, run and deleted; the Postfix or Cyrus source under `dev/upstream/`)
rather than guess. The coordinator re-read the code behind every finding of
medium or above, settled the claims that needed the running system on the
test server, and probed the public ports and the DNS from outside. Machine
checks: `go build`, `go vet`, `go mod verify`, `govulncheck`, a search of the
whole git history for key and token shapes. `staticcheck 2026.1` does not run
on the local Go 1.27 toolchain; CI runs it on 1.26.

**Machine results.** Build, vet, module verification: clean. Git history: no
private key or token outside test fixtures; `dev/` and the `.exe` files are
untracked and outside the build context. `govulncheck` on Go 1.27.0 lists
twelve standard-library advisories of 2026-10-08 (`GO-2026-6599` … `6617`:
`net/http`, HTTP/2, `net/textproto`, `crypto/tls`, `html/template`), all fixed
in Go 1.26.9 / 1.27.2 — the live `2.14.3` binary is built with go1.26.9 (the
image's `golang:1.26-trixie` tag floated onto it), so it is not affected; one
module advisory (`GO-2026-5932`, `x/crypto/openpgp`) is not reached by this
code. Debian security tracker, trixie, 2026-10-17: `postfix
3.10.13-0+deb13u1` nothing open; `cyrus-sasl2 2.1.28+dfsg1-9` has
`CVE-2026-107161` open (heap overflow in the DIGEST-MD5 *client* plugin; the
image serves `PLAIN LOGIN` only — not reachable); `supervisor 4.2.5-3` has
`CVE-2019-12105` open (needs the inet HTTP server, which the image does not
configure); `openssl 3.5.7-1~deb13u3` nothing open.

**Severity** as in the previous audit: **critical** — the security boundary
is crossed or mail/data lost today, by a stranger; **high** — a documented
guarantee is false in a way an attacker will use; **medium** — a tenant or
stranger gets something the design denies them, or a stated guard is not
there; **low** — unlikely condition, cost without a boundary, or hygiene with
a security flavour; **nit** — wording. Confidence is *proven* (a test, a
probe, or the running system), *likely* (unambiguous reading) or *possible*.
IDs keep the zone letter: **A** auth, **W** web, **M** mail, **I** image and
infrastructure, **S** the live server and its host, **X** cross-cutting.

**The short version.** No critical finding; nothing an unauthenticated
stranger can do crosses the relay's boundary: every port refuses relaying,
credentials travel only under TLS ≥ 1.2, the SASL identity cannot be chosen
beyond the stored account, the signer keys on the login and nothing in the
message, the tenant boundary holds on every HTTP route, there is no XSS,
injection or traversal path, and the backup/export files are sound. One high
(A2): a stolen `global` session is enough to take a full backup after all,
through the user form. Seven mediums, all about a tenant or a stranger being
able to affect *another* tenant's limits, logs or report pages (M1, M2, M3,
W1), the host's TLS key being readable by the `postfix` uid (I1), one gap in
the symlink guard (I2) and the shipped default under which all visitors share
one sign-in limit (A1). The live host itself has the two findings that matter
most today and are not product code (S1, S2 — recorded in `dev/`, not
published, with the commands that close them).

---

## Findings

### High

**A2. A stolen `global` session takes the server: the user form sets the
signed-in user's own password without the current one, so backup-reauth is
passed with a password the thief chose.** *Proven (by reading; three
handlers).* `POST /server/users/<own id>` with a `password` field
([handlers_users.go:237-337](../internal/web/handlers/handlers_users.go))
replaces the signed-in user's own hash with no current-password check and
keeps the current session (`endSessionsAfterPasswordSet`, :342-350, ends only
the *other* sessions when a user sets their own); `reauth`
([handlers_backup.go:336-357](../internal/web/handlers/handlers_backup.go))
compares the form's `account_password` with the *current* stored hash. Three
requests with a stolen global cookie: set a password, `POST /server/backup`
with it, download the DKIM keys, `sasldb2` and every password hash.
Alternatively `POST /server/users/new` with `role=global` creates a second
administrator with a known password, who survives the victim noticing and
changing their password (Account › password ends only the victim's other
sessions). security.md § Backup and domain export ("a stolen session cookie
or a panel left open no longer hands over a copy of the server") is false for
a `global` session; it holds for a `domain` session. Compare Account, where
changing one's own password or e-mail asks for the current one
(handlers_account.go:124-139, 229). **Fix:** `reauth` (already written) on
`POST /server/users/new`, on `POST /server/users/{uid}` whenever `password` or
`role` changes, and on `POST /server/users/{uid}/delete`; a doc line. ~2 h
with tests.

### Medium

**M1. The level-2 domain limit is keyed on the `MAIL FROM` domain before
Postfix has held it to the login, so a stranger on 465/587, or another
tenant's application, reserves a victim domain's slots.** *Proven (unit
tests by the reader; the ordering from Postfix source; the coordinator's
unauthenticated `MAIL FROM` on 587 answered `250 2.1.0 Ok`, which is the
milter's MAIL event having been sent).* `overLimit`
([ratelimit.go:28-62](../internal/milter/ratelimit.go)) takes
`domainOf(s.from)` whatever the login, and `MailFrom`
([milter.go:126-144](../internal/milter/milter.go)) runs it with an empty
login. With `smtpd_delay_reject` at its default, Postfix runs no sender or
client restriction at MAIL (`src/smtpd/smtpd.c:2767-2779`) and sends the
milter's MAIL event right away. A stranger opens TLS to 465, `EHLO`, `MAIL
FROM:<x@victim.example>`, no AUTH: a slot in `victim.example`'s window is
taken (`tryAdmit`), RCPT is refused, the transaction stays open and the slot
is held for the 10-minute in-flight TTL; the next `MAIL FROM` on the same
connection re-reserves. Under `smtpd_client_message_rate_limit=100/h` one
address holds ~16 slots indefinitely, 50 for the first ten minutes; a domain
with a ceiling of 50/h is blocked for ten minutes at a time by one address,
its own application is answered `451`, a `limit` row is written under the
victim's domain and the victim's users get the "a rate limit is refusing mail"
notification. An application of tenant A does the same against tenant B with
`MAIL FROM:<x@tenantB>`; Postfix refuses it at RCPT (553), too late.
**Fix:** take the domain from the login alone (`ApplicationByLogin(s.login).DomainName`,
the call already made for the null sender) and return at once when
`s.login == ""` — an unauthenticated session can never deliver on 465/587;
same for `recordRejected`'s `Domain`. ~1-2 h with tests. (The alternative,
`smtpd_delay_reject=no` on the submission services, moves the client
restrictions to MAIL as well and closes the stranger's case at the Postfix
level, at the price of the delayed-reject logging; the milter fix is the one
to make either way.)

**M2. The inbound journal takes a stranger's recipient for Postfix's own
log tail: a stranger writes rows with a chosen sender and recipient under any
inbound domain.** *Proven on the live server.* `inboundRejectRe`
([logtail/inbound.go:112](../internal/logtail/inbound.go)) ends the reply at
the *first* `; from=<`. Postfix's reject reply quotes the refused recipient in
*internal* form — quotes removed, `; < > =` kept (`extract_addr` →
`tok822_internalize`; `smtpd_check_reject` applies only `printable()`). Sent
from outside to port 25: `RCPT TO:<"; from=<ceo@example.net>
to=<board@example.net> proto=ESMTP helo=<x>; "@nonrelay.example>`; mail.log
on the server: `… 554 5.7.1 <; from=<ceo@example.net> to=<board@example.net>
proto=ESMTP helo=<x>; @nonrelay.example>: Relay access denied; from=<probe@example.net>
to=<"; from=…"@nonrelay.example> proto=ESMTP helo=<audit.example>`. The
regex's reply group stops at the forged `; from=<`, so the row's sender,
recipient and inbound domain are the stranger's choice (the reader's test fed
this line shape to `inboundJournal.handle` and got one row,
`domain=victim.example from=ceo@… to=board@…`); the quote/bracket/space filter
on `from`/`to` passes because the forged values are clean. The probe above
used a domain that is not inbound, so no row was written on the test server.
security.md § Mail path ("the reply now ends at Postfix's own `; from=<`") is
false. **Fix:** Postfix neuters ` <>()\";@` in a HELO name
(`smtpd.c:1634`), so Postfix's own tail is always the *last* `; from=<…>
to=<…> proto=\S+( helo=<[^<>;]*>)?( sasl_…)*$` on the line: make the reply
group greedy and anchor the tail; keep the filter; add the line above as a
test. ~1 h. (`fromRefusalRe` and `refusalLineRe` are not affected: the
sender check's reply carries no `;` and the journal milter's reply is fixed.)

**M3. The inbound journal milter neither de-duplicates recipients nor
counts its rows toward the 10 000-row cap: one accepted message writes up to
1 000 rows, uncut for up to six hours.** *Proven (unit test by the reader;
code confirmed).* `inboundSession.RcptTo`
([milter/inbound.go:86-89](../internal/milter/inbound.go)) appends every RCPT
(the outbound session de-duplicates, milter.go:159-168) and the end of the
message inserts one row per recipient at an inbound domain (:218-232),
synchronously inside the milter callback on the panel's one SQLite
connection; the cap's trigger counts only the tailer's rows
([logtail/inbound.go:274-280](../internal/logtail/inbound.go)), the six-hourly
prune is the rest. Postfix accepts a repeated recipient up to
`smtpd_recipient_limit` (1 000) and sends a RCPT event for each. In `list`
mode the stranger repeats one listed address; at the default
`INBOUND_RATE_LIMIT_MESSAGES_PER_IP=20/h` that is 20 000 rows an hour per
address, and a slow pass past the 30 s content timeout fails open.
security.md's "cut to 10 000 rows every 500 rows the tailer writes" is true as
written and does not cover this path. **Fix:** de-duplicate recipients
(case-folded) as the outbound session does; count milter inserts into the
prune trigger (a counter in `InsertInboundSpam`, or the milter calls
`PruneInboundSpam(time.Time{}, cap)` every N inserts); one transaction per
message. ~2 h.

**W1. A tenant's custom DMARC report address may be on the server's own
hostname, which opens an untagged ingest mailbox, or another tenant's, to the
whole internet.** *Proven (unit test by the reader; code confirmed).*
`HandleDomainDMARC` ([handlers_dmarc.go:37-47](../internal/web/handlers/handlers_dmarc.go))
accepts any address `validate.Email` takes; `AllowedRecipients`
([dmarc/service.go:58-66](../internal/dmarc/service.go)) keeps every stored
address whose domain is the hostname, not only `HostedReportAddress(host,
d.Name)`, and it goes into `dmarc_recipients` / `dmarc_transport`;
`checkReportDomain` ([ingest.go:122](../internal/dmarc/ingest.go)) cross-checks
the claimed domain only against a `dmarc-reports+<tag>` local part. A
`domain` user of tenant A sets `ops@<hostname>` as the report address of an
assigned domain: from then on anyone can mail a report claiming
`<policy_published><domain>tenant-b</domain>` to that address and it is
stored and shown under B's DMARC pages, and counts toward B's 500-report cap.
Variant: A stores `dmarc-reports+tenant-b@<hostname>` and B's hosted mailbox
is open without B choosing "SelfPost hosted". The accepted-risk sentence
("What a stranger can do is limited to a domain that chose a SelfPost-hosted
address, through that address") is false while this is accepted; the
existing `TestAllowedRecipientsFollowTheDomainAddresses` asserts
`ops@<host>` *is* allowed, so this was a choice, not a slip. **Fix:** refuse
a custom address whose domain is the hostname ("use the SelfPost hosted
choice"); keep only the exact hosted address per domain in
`AllowedRecipients`; require a tag for every recipient on the hostname in
`checkReportDomain`; fix the test and the accepted-risk text. ~1.5 h.

**I1. The host's TLS private key, given to group 10998 as the guide
instructs, is readable by the `postfix` uid — every network-facing Postfix
daemon.** *Proven on the live server* (`docker exec -u postfix … head
privkey.pem` prints the key). [Dockerfile:126-128](../build/Dockerfile) puts
`postfix` in `selfpost` (gid 10998) so it can read `sasldb2` and the maps;
[guide.md](guide.md) § Taking a full backup and § Reverse proxy tell the
operator `chgrp 10998 certs/privkey.pem && chmod 640` so the panel can
archive it, and [handlers_backup.go:398-399](../internal/web/handlers/handlers_backup.go)
says the same when it is missing;
[deploy/traefik/extract-cert.sh](../deploy/traefik/extract-cert.sh) keeps that
group on renewal. [postfix-config.sh:92-114](../build/postfix-config.sh) and
[postfix-cert-reload.sh](../build/postfix-cert-reload.sh) copy the key to
`/etc/postfix/tls-internal` as root 0600 *because* Postfix's privilege
separation exists so that a compromised unprivileged daemon cannot read it —
and the original, mounted at `/etc/postfix/tls`, is readable by that uid
through the backup group. A memory-safety bug in `smtpd` reached from the
internet yields the certificate's key, which is the panel's and the mail's
(one certificate). The DKIM keys (0700 `panel`) and the database (0600) are
not exposed this way; `sasldb2` is, by design. **Fix:** a third group for the
backup key alone (`groupadd --system --gid 10997 …; usermod -aG … panel`),
the guide, the handler text and the e2e stage (`stage.go:119-127` chmods the
key 0644 today) follow; `extract-cert.sh` copies the group by reference and
needs no change. ~2 h.

**I2. `drop_links` leaves out the three DMARC maps; root's `: >` in
`postfix-config.sh` follows a dangling link the panel can plant.** *Proven
mechanism; needs code already running as `panel`.* (Found independently by
the mail and the infrastructure reader as M4/I2.)
[entrypoint.sh:89-94](../build/entrypoint.sh) lists the five outbound maps,
`sasldb2`, the key tree and `mail.log`, not `/data/postfix/dmarc_recipients`,
`dmarc_transport`, `dmarc_relay_domains`;
[postfix-config.sh:545-547](../build/postfix-config.sh) runs `[ -e "$f" ] ||
: > "$f"` as root for exactly those when `DMARC_REPORTS_ENABLE=true`
(`-e` is false for a dangling link, the redirect follows it). `/data/postfix`
is the panel's (2750); the panel is in group `supervisor` and
`supervisor.shutdown` is on the XML-RPC interface it already speaks; Docker's
`restart: unless-stopped` re-runs the entrypoint as root, which creates an
empty root-owned 0644 file at any directory that exists. Inside this image
that is close to nothing (no `sudoers.d`, no `conf.d` include, logrotate runs
one named config), but security.md § Mail path ("A root step of the
entrypoint follows no symbolic link the panel could plant in the tree") is
the guard this was meant to be, and entrypoint.sh:165 records the same maps
once forgotten in the `chown` list. **Fix:** the three names in
`drop_links`; belt and braces `[ -e "$f" ] || [ -L "$f" ] || : > "$f"` at
postfix-config.sh:538 and :545; an e2e case that plants a link and expects
the "removing the symbolic link" line. ~0.5 h.

**A1. Under the shipped deployment every visitor shares one sign-in, setup,
reset and link-page bucket.** *Proven (test by the reader: two requests from
`172.18.0.1` with different `X-Forwarded-For` keyed `172.18.0.1`).* The
default is Apache on the host proxying to `127.0.0.1:8080`; the panel sees
every connection from the Docker gateway and `TRUSTED_PROXY_CIDR` is empty
by default ([main.go:168](../cmd/panel/main.go), `.env.example` commented
out, guide.md:254 "Leave unset unless…"), so XFF is ignored
([auth.go:128-146](../internal/web/auth/auth.go)). Anyone POSTs `/login` ten
times per 15 minutes (any body — the limiter is consumed before the form is
read, handlers.go:105) and every administrator gets "Too many attempts" for
as long as it goes on, at one request per 90 s; the limiter also counts
*successful* sign-ins. The same for `/login/reset` (nobody can ask for a
link), the link pages and `/setup/<token>` during first run. Only a restart
clears the buckets. The guide and Health's "Your address" card say this is
so; security.md § Authentication ("per IP") and § First-run setup ("per IP,
separate from login") state it as a property, and it is not under Accepted
risks. On the test server `TRUSTED_PROXY_CIDR=172.18.0.1` matches the
gateway, so the live install is not affected. **Fix:** (a) the shipped
compose and guide set the trust explicitly for the Apache-on-host scenario,
and the panel warns at start — not only on Health — when every request
arrives from one private peer that carries XFF; (b) key the login limiter
by *username* as well, with a per-user delay, so one key cannot lock
everyone out, and the link limiters by token hash; (c) do not count
successful sign-ins. ~3-4 h for (b)+(c), 1 h for (a).

### Low

**S1, S2 — the test server's host, not the product.** Two findings about
the host's own SSH policy and update state are recorded in
`dev/security-audit-2026-10-17-host.md`, which is not published (the
repository is public; a list of a host's open doors does not belong in it
until they are closed). Both are the owner's own actions, together under an
hour, no product code. They are listed low *in the product's scale*, and
S1's worst case is a root shell on the machine that holds `/data`, the TLS
keys, Carrel and Wynd — so they are **first in the plan**.

**S3 — host and DNS. The server's own name has no DMARC record.** The panel
sends notifications and password-reset links as `<something>@selfpost.mixfed.ru`,
SPF is `v=spf1 a -all` and the panel signs with the server's key, but with
no `_dmarc.selfpost.mixfed.ru` a receiver enforces neither, so a forged
"reset your SelfPost password" mail from the server's own name is as
deliverable as a real one. No MTA-STS, no TLSRPT, no CAA on `mixfed.ru`
either (the latter two are optional; CAA is cheap). **Fix:** `_dmarc TXT
"v=DMARC1; p=reject; rua=mailto:…"` for the server's name (the panel's
Settings page already shows the DKIM record); CAA `0 issue "letsencrypt.org"`;
15 min in DNS. Product side: the kit page could list the DMARC line for the
hostname next to the DKIM one (30 min).

**S4 — deployment. The antispam sidecar runs rspamd 3.11.1 (December 2024)
with default capabilities; upstream is 4.2.2 (2026-10-10) and the line
between fixed a heap overflow in `util.lower_utf8`, use-after-free on
reload and several parser crashes.** rspamd reads every byte a stranger sends
to port 25 before the journal does, and
[docker-compose.antispam.yml](../deploy/antispam/docker-compose.antispam.yml)
pins `rspamd/rspamd:3.11.1-3` with no `cap_drop`, no `no-new-privileges`, no
`pids_limit`. The milter is fail-open, so a crash costs filtering, not mail.
**Fix:** bump the pin to `4.2.x` after one e2e run (4.0 changed the WebUI and
DKIM pieces, neither used here; the `milter_headers`/`multimap` configs need
reading against the 4.x docs), add `cap_drop: [ALL]`, `security_opt:
[no-new-privileges:true]`, `pids_limit`; 1-2 h.

**S5 — host hygiene.** Three small items (a resolver service listening
where nothing needs it, uncapped container logs, a stale copy of the compose
file), in the `dev/` file with S1 and S2; 10 min.

**S6 — Postfix default. `ETRN` is accepted from anyone on port 25.**
*Proven from outside:* `ETRN <an inbound domain of the server>` → `250
Queuing started`.
`smtpd_etrn_restrictions` is empty (Postfix's default allows every client)
and `fast_flush_domains = $relay_domains`, so a stranger triggers a queue
flush toward an inbound domain's upstream at will — a mild lever to hammer an
upstream with a large deferred queue, nothing more. **Fix:**
`smtpd_etrn_restrictions = reject` (and `smtpd_discard_ehlo_keywords = etrn`)
in postfix-config.sh; 10 min.

**A3. Ten reset mails an hour to one mailbox from anyone who knows a
username.** `sendReset` ([links.go:154-191](../internal/web/auth/links.go))
spaces reset mails 10 minutes apart and caps them at 5/h per account; a
stranger who repeats a username keeps the administrator's confirmed mailbox
receiving a live 30-minute link five times an hour, indefinitely. No secret
leaks; nuisance, and a standing live link in a mailbox. **Fix:** after N
unsolicited links a day, one mail that says further requests are ignored for
24 h; or require username *and* confirmed address on the form. ~1-2 h.

**A4. The duplicate-cookie log line prints a link's token (unprefixed cookie
only).** *Proven by test.* [auth/handlers.go:56-58](../internal/web/auth/handlers.go)
logs `r.URL.Path` raw, so `GET /confirm/<token>` with two `selfpost_session`
cookies writes the token to the log; reachable only with
`PANEL_COOKIE_SECURE=false`, where a sibling host of the parent domain can
plant a cookie of that name (`__Host-` cannot be duplicated).
security.md:179 says a rejected request's log line hides a link's token.
**Fix:** log through `loggedPath`; 15 min.

**A5 / W2. DMARC reports belong to a domain by *name*, outlive the domain,
and a domain deleted and added again hands its old reports to the new
assignee.** [handlers_dmarc_reports.go:176-188](../internal/web/handlers/handlers_dmarc_reports.go)
(`cand.Name == rep.Domain`), [store/dmarc.go:194-204](../internal/store/dmarc.go)
(`WHERE domain IN (names)`), `DeleteDomain` leaves `dmarc_reports` alone (no
FK). The logs were moved to ids in 2.5.0 for exactly this case (security.md
§ Roles); reports were not. Aggregate statistics about a name the new tenant
now owns, so low. **Fix:** delete `dmarc_reports WHERE domain = ?` in
`domain.Service.Delete` (simplest, 0.5 h), or `domain_id` on the table and
scope by it as `send_log` does (2 h with the migration).

**A8. No log line for a refused sign-in or a lockout.** `submitLogin`
([auth/handlers.go:104-132](../internal/web/auth/handlers.go)) logs nothing
on 401 or 429; a brute-force, or A1 happening, is visible only in the proxy's
access log. **Fix:** one line per refusal, rate-limited per key; 30 min.

**A9. IPv6 clients are keyed per /128.** `ip.String()` at auth.go:138; a
client with a /64 rotates addresses and never meets the 10/15 min limit;
the bucket map is bounded (4096 × 4) so memory is safe, but blocked buckets
are evicted under that pressure, which lets the attacker flush its own
blocks. bcrypt cost 12 remains the real brake. **Fix:** key IPv6 by /64;
20 min.

**W3. The sender-map rebuild is not serialised.** `Resync`
([app/service.go:229-239](../internal/app/service.go)) reads bindings and
writes with no lock (the selector table has `tableMu`); two concurrent
application changes can leave the map stale until the next change — an
application whose binding is missing is refused with
`reject_sender_login_mismatch` until then. Availability only. **Fix:** a
mutex around read + write + reload; 0.5 h.

**W4 / W5 / W8. Import bounds.** The imported DKIM key size is unbounded
upward ([keys.go:129-146](../internal/domain/keys.go); a 16 384-bit key makes
every signature of that domain cost seconds); the number of applications in
an export is unbounded (each is a full copy of `sasldb2` plus a
`saslpasswd2` spawn — a 1 MiB export with ~5 000 applications is minutes of
quadratic work on the panel's one connection, then a rollback of the same
size); importing a domain named as the hostname silently replaces the
server's own signing key while DNS still holds the old record. All
global-only, self-inflicted. **Fix:** refuse keys outside 1024-4096 bits;
cap applications at 500 in `checkDomainImport`; refuse (or warn on) an import
named as the hostname; 1.5 h together.

**W6 / M9. A `domain` user can trigger `postfix reload` and uncached DNS
lookups without limit.** Every application, upstream, recipient or
report-address save reloads Postfix ([postfix.go:67-76](../internal/postfix/postfix.go)
→ supervisord `postfix-reload`), and `POST …/dns-recheck` bypasses the
5-minute cache for up to ~14 resolver queries. Authenticated operators only,
bounded by the HTTP server. **Fix:** coalesce reloads (debounce, or reload
only when the rendered file changed) and a small per-user limiter on the
recheck; 1-2 h.

**M5. The sender check keeps every `From:`/`Sender:` value of a message.**
*Proven (100 headers of 60 000 bytes → 6 MB held).*
[sender.go:310-323](../internal/milter/sender.go) appends every value; Postfix
bounds one header at 60 000 bytes but not their number, and the signer's
1 MiB cap runs after the sender check. Authenticated applications only, ~10
MiB per connection × 50 connections, and the message is refused anyway;
security.md:810 "a few headers each" is overstated. **Fix:** keep at most two
values of each (`judge` needs only none/one/more); 20 min.

**M7. The upstream TLS mode `encrypt` is unauthenticated TLS.**
[postfix/inbound.go:84-88](../internal/postfix/inbound.go) writes `encrypt`;
Postfix verifies no certificate at that level, so a `domain` user who chooses
it gets privacy against passive observers only. The docs promise nothing
more. **Fix:** say "no certificate check" in the guide (10 min); optionally
offer `secure` with the upstream's name (hours).

**I3. No `pids_limit`; the kept capabilities stay with root for the
container's life.** [docker-compose.yml:141-153](../deploy/docker-compose.yml):
`cap_drop: ALL`, eight `cap_add` (each justified in the file; the live
`CapEff 0x4fb` matches), `no-new-privileges`, no `read_only` (explained), no
`pids_limit`, no memory limit (an accepted risk). A fork bomb from a
compromised `panel` or `postfix` has the host's pid space as its limit. The
"root runs very briefly" comment describes the entrypoint only: supervisord,
Postfix's `master`, `cert-reload` and `logrotate` are root throughout, which
is inherent. **Fix:** `pids_limit: 512`; a word in the comment; 10 min plus
an e2e run.

**I4. Supply chain: mutable references everywhere the image is built from;
no vulnerability scan in CI.** Actions by major tag, not SHA
(`checkout@v4`, `setup-go@v5`, `build-push-action@v6`, …); `golang:1.26-trixie`
and `debian:trixie-slim` floating; apt unpinned (right for security, but two
builds of one tag differ); `provenance: false`, no SBOM; `certbot/certbot:latest`,
`caddy:2` in the fragments; no `govulncheck` step, so the twelve stdlib
advisories above would have gone unnoticed had the base tag not moved. What
is sound: `permissions` set, `pull_request` not `_target`, release only from
a published release or a dispatch, e2e-gated push of the same bytes, safe
GHCR cleanup, `go.sum` verified, the `replace` to `third_party/go-milter` is
upstream v0.4.1 byte for byte plus the one documented change. **Fix:** pin
actions by SHA with the version in a comment (30 min); `govulncheck ./...` in
`test.yml` and before the push in `release.yml` (20 min); pin `certbot` and
`caddy` (10 min); `provenance`/`sbom` only after reading the GHCR cleanup loop
(attestations appear as untagged versions) (1 h).

**I5. No rebuild cadence between releases.** A DSA for postfix, openssl or
libsasl2 after a release reaches operators only with the next release build,
and published tags are never rebuilt (the owner's rule). A policy, not a bug;
it is not written down. **Fix:** a sentence in guide § Upgrading and in
SECURITY.md; optionally a weekly job that compares the image's package list
against trixie-security and fails when postfix/libsasl2/openssl/libdb moved,
as a nudge to cut a patch release; 10 min / 1 h.

**I6. `/healthz` is public through every proxy fragment.** `web.go:212`
registers it outside auth; the live server answers `ok` to the internet. No
secret, but "unhealthy: postfix: error" is a free tell. **Fix:** answer
`/healthz` only to loopback peers (and `TRUSTED_PROXY_CIDR` peers), or a
`<Location /healthz> Require ip 127.0.0.1</Location>` line in the fragments;
30 min.

**X1. SECURITY.md's supported-versions table names the `1.x` line while the
product is at `2.14`.** An operator reading the policy would conclude that
`2.x` is unsupported and `1.x` gets fixes, the reverse of the truth. 10 min.

### Nits

- **A6.** A wrong method on `/server/*`, `/overview`, `/outbound/queue`
  answers `405 Allow: …` from the mux before `globalOnly`, not the "same 404"
  the doc promises. Routes are public source; 30 min to rewrite the 405 for a
  non-global principal, or drop the sentence.
- **A7.** The setup token is re-minted on the next `/setup/*` hit, not at
  expiry as security.md says; a stranger's `GET /setup/x` mints and logs one
  (the only GET with a side effect besides W7). 30 min or wording.
- **A10.** Session renewal math drifts after `PANEL_SESSION_IDLE_DAYS` is
  lowered (sessions keep the old window until their old expiry). 30 min.
- **W7.** `GET /server/settings` and `GET /server/health` generate the
  server's DKIM key on first view — "a GET never changes state" has this
  exception. Wording, or move the bootstrap to start.
- **M6.** `Content-Type`, `MIME-Version`, `Message-ID` are not in `h=` (same
  as OpenDKIM's default; a relay can change a boundary and the signature still
  verifies). One line to add them, with `Message-ID` *not* over-signed
  (cleanup adds a missing one after the milters).
- **M8.** A header line over 998 bytes is broken by Postfix's `smtp` client
  after signing and the signature fails silently; only body lines are logged
  today. Log it per message in `Header`.
- **README quick start** runs `docker run` with default capabilities and no
  `no-new-privileges` — it is the local trial and says so; a one-line note
  that the compose file is the hardened one would not hurt.
- The Apache vhost on the test server sets `Referrer-Policy: no-referrer`,
  the panel `same-origin`; both headers are sent and browsers take the
  panel's (last) one. Drop the Apache lines the panel already sends; the
  shipped `deploy/apache/selfpost-vhost.conf` is correct as it is.

### Doc and comment drift (no security effect once the findings above are fixed)

- security.md § Authentication: "a newer link of the same kind replaces the
  older" (true for confirmation links only; the reset sentence later in the
  section and the code say otherwise); migration `0013` has the same line.
- security.md § Backup and domain export: backup-reauth holds for a `domain`
  session, not a `global` one (A2).
- security.md § Mail path: `drop_links` (I2); "the reply ends at Postfix's
  own `; from=<`" (M2); "cut to 10 000 rows every 500 rows the tailer
  writes" (M3); "a few headers each" (M5); "What the client wrote reaches the
  reply … only as printable ASCII" holds for the From/Sender line, while a
  refused row's `from_addr`/`to_addr`/`subject` are the envelope and decoded
  Subject as written (html/template escapes them) — wording.
- security.md § Output and process: "(queue, log, journal, themes)" — no
  themes; "keeps `KILL`" reads as "only KILL" — list the eight.
- security.md § Accepted risks, DMARC: the stranger's reach sentence (W1);
  "cross-checked against the +tag" holds for tagged addresses only.
- security.md § Roles: the by-id rule is stated for logs; reports are by
  name (A5/W2).
- security.md § State-changing requests: GETs that write (A7, W7).
- architecture.md: "the error text never reaches the browser" omits the
  Backup-page exception security.md records; "an existing [key] is reused,
  never overwritten" — `ImportKey` overwrites by design (W8); "a browser that
  still holds the cookie works after a full backup restore" agrees with the
  code and is noted only because sessions and links are treated differently
  on purpose.
- Comments: `store/applications.go:32-33`, `app/password.go:16-17` say
  `sasldb2` holds a hash (plaintext, as security.md says);
  `sender.go:205-209` says Postfix folds table keys as Unicode — `texthash`
  is `dict_ht` with ASCII `lowercase()` (`dict_thash.c:113`,
  `dict_ht.c:58-64`), the safe direction; `supervisor/rpc.go:17-18` names the
  socket's group `selfpost` (it is `supervisor`); `postfix-cert-reload.sh:46`
  cites "OpenSSL 3.0.22" (trixie ships 3.5.x); `links.go:152-153` "three an
  hour" (five); `docker-compose.yml:102-104` "very briefly" (I3).
- guide.md: the `chgrp 10998` advice should say what it gives Postfix (I1,
  until the third group lands); nothing says how the panel reads Caddy's key
  for a full backup; the upstream TLS mode should say "no certificate check"
  (M7).
- The live compose on the test server is an older copy of the repository's:
  comments differ and the `INBOUND_SPAM_LOG_RETENTION_DAYS` line is missing
  (the default applies). Not a finding; re-copy at the next rollout.

---

## Checked and sound

Each claim below was held to the line that makes it true; the readers' full
lists with line references are condensed here.

**Boundary from the internet.** Not an open relay on any port (every
restriction list walked as Postfix evaluates it; confirmed from outside:
`Relay access denied` on 25 for a foreign and for a non-inbound domain,
`Client host rejected` for an unauthenticated RCPT on 587, `User unknown in
relay recipient table` for an unlisted address — no backscatter).
`mynetworks` is loopback and never referenced by a `permit_mynetworks`;
`mydestination` empty, local delivery an `error:` transport;
`allow_untrusted_routing=no`; `disable_vrfy_command=yes` (confirmed). SASL
only on 465/587, `PLAIN LOGIN` after TLS only (`smtpd_tls_auth_only`, 465
wrapper mode, 587 `encrypt`; confirmed from outside: no `AUTH` before
STARTTLS, TLS 1.1 refused, TLS 1.3 negotiated, certificate valid). Cyrus's
default proxy policy forbids a foreign authzid, no realm is appended, sasldb
keys are exact bytes (`lib/common.c:1698-1721`, `canonusr.c:390-403`).
Envelope sender held to the login by the same three lookups on both sides
(`MA_FIND_DEFAULT`; `Bindings.Allowed`), keys folded ASCII. The sender check
parses `From:`/`Sender:` as a mail program does and refuses every line of the
"other sender" list; `Resent-From` unjudged as documented. The signer keys
on `{auth_authen}` → `Bindings.DomainOf`, the local socket on the server's
name, nothing in the message chooses a key, an unknown login defers;
over-signing and `From` last verified against go-msgauth v0.7.0; relaxed
canonicalisation covers Postfix's `Name :` normalisation; 1 MiB header cap;
key tree 0700/0600 `panel:panel` (confirmed live). The 2.1.0 per-message
`{client_addr}` fix holds (`MailFrom` re-reads it; `Body` answers
`continue`). Inbound: `smtpd_forbid_bare_newline=normalize`, maps rendered
only for domains with an upstream, `[host]:port` next hop, verdict read only
beside its HMAC signature, rspamd strips a sender's own verdict headers for
every client. DMARC ingest: pipe as `panel`, one at a time, 10 MiB message,
8 MiB part and decompressed, no DTD entities (Go's `encoding/xml`), ≤ 5 000
records, 255-byte fields, domain checked before any write. `saslpasswd2`:
fixed argv, `--` before the login, password on stdin, copy renamed into
place at 0640. Notify: recipient re-checked before any header or argv,
Subject one line Q-encoded, `sendmail -i -f … -- to`, hourly ceiling.

**Panel.** Setup token 128 bits, constant-time, 10 min, void once a user
exists, first user one statement into an empty table, `/setup/` limited
before anything else; bcrypt 12 everywhere, unknown name costs the same
compare; sessions 256-bit, SHA-256 stored, `HttpOnly`/`Secure`/`SameSite`,
`__Host-`, duplicates = signed out, fixation closed (sign-in destroys the
presented session), by user id, touched only after the user is found,
hourly renewal, htmx GETs do not renew; every "what ends a session" rule
verified line by line; reset and confirmation links 256-bit, hash stored,
`https://` + `SELFPOST_HOSTNAME`, taken once in the transaction that sets the
password and re-checks the address, equal answer and timing for every name,
all the stated limits and spacing, confirmation needs the account's own live
session, restore deletes every link, stop drains the mailing goroutines;
mail header injection closed twice. Origin check: non-read methods only,
`Sec-Fetch-Site` wins, else `Origin` host:port must equal `r.Host`, neither
→ 403, before auth so the public POSTs are covered; every state change a
POST; the proxy fragments preserve `Host`. Headers: CSP `default-src
'self'; object-src 'none'; base-uri 'none'; form-action 'self';
frame-ancestors 'none'`, `nosniff`, `X-Frame-Options: DENY`,
`Referrer-Policy: same-origin`, HSTS (confirmed from outside; TRACE 405;
HTTP → 301 HTTPS). HTTP phases bounded as documented. Tenant boundary:
`/server/` registered only through `server()` and every handler
re-checks; `lookupDomain`, `lookupDomainApplication`,
`requireInboundDomain` answer the same 404 for missing and foreign; outbound
and inbound logs scoped by `domain_id IN (…)` in SQL with `1 = 0` for an
empty scope, filters only narrow; one delivery by `row.DomainID`; DMARC pages
by assignment; last-global guard inside the transaction; roles whitelisted
twice. Rendering: every user string through `html/template`, the one
`template.HTML` producer escapes first, every `hx-get`/`href`/`popovertarget`
server-built from ints and fixed paths, no inline script; SQL parameterised;
every redirect target a literal; query flags map to fixed strings.
Validation whitelists for domains, selectors, logins, addresses, hosts,
ports, filter-list values; map writers' backstops (`assertMapSafe`,
`assertMapToken`, `assertConfigSafe`, `configsafe.Token`); every write
temp + fsync + rename. Commands: `postqueue -p|-j` bounded and timed,
`supervisorctl status`, `postconf` — fixed argv, no shell. Secret pages
`no-store`, one-time password emptied on `pagehide`; backup and export
`no-store`, attachment, `reauth` (bcrypt under the password-change limiter)
before the write deadline is lifted. Import: 1 MiB, magic-byte detection,
whole plaintext read before JSON, `DisallowUnknownFields`, every field
re-validated. secretfile: scrypt N=2^15/r=8/p=1, random salt, AES-256-GCM,
random nonce prefix + counter, header/counter/end flag as AAD, caps read
before any KDF work, chunk ≤ 64 KiB; CLI password from env or file only,
wrong password caught before the output is touched. Backup: `VACUUM INTO`
on a read-only handle, live DB files/log/queue/setup-token/`sasldb2.new`
excluded, links under `data/` skipped, under `certs/` followed to regular
files only, `contents.json` last; restore: no extraction in the panel,
version check, partial unpack, journal beside the DB, names confined to
`data/`, `Lstat` + regular-file check, size and SHA-256 compared. Log
parsers: head-anchored, recipient by Postfix's quoting grammar, lazy to the
first `status=`, port-25 lines not stored, the HELO attack of 2026-10-12
closed by Postfix's neutering, stranger text printable and cut, journal
capped — except M2 and M3 above. DNS checks to fixed external resolvers with
timeouts and a lookup budget.

**Image and process.** Root phase: `chown -R`/`find -exec` do not follow
links, `drop_links` covers every other path root then touches (I2 is the
gap), no `set -x`, no secret in the environment (confirmed live: hostnames,
limits, paths, TZ), the panel is not running while root touches `/data`.
Modes as documented, confirmed live: DB 0600, DKIM 0700/0600, `sasldb2`
0640 in 2750, maps 0640, `mail.log` 0640 `postfix:selfpost`, sockets 0660
in 2750, `supervisor.sock` 0770 `root:supervisor`, key copy 0600 root in
`tls-internal` (the `postfix` uid cannot read it — I1 is the *mounted*
original). supervisord: unix socket only, no inet server, no include
(confirmed live), config root-only, so the panel can start/stop/restart/
shutdown the five programs and nothing more; `crashexit.py` only `SIGTERM`s
its parent. Root helpers read nothing the panel can write; logrotate 3.22.0
opens with `O_NOFOLLOW`/`O_EXCL` (verified in its source). Capabilities: the
eight in compose are each needed (`DAC_OVERRIDE` for root to create files in
the panel's 2750 directories, `KILL` measured on 2026-10-16), the live
`CapEff` decodes to exactly them, `no-new-privileges:true` (so the fourteen
setuid/setgid binaries of the base image — `su`, `passwd`, `mount`,
`postdrop`, `postqueue`, … — are inert for the panel; removing the Debian set
is hygiene, not a fix). `.dockerignore` and the `COPY` set keep `dev/`,
`test/`, the `.exe` files and the docs out of the image. e2e stand: isolated
project, loopback port, throwaway key, `PANEL_COOKIE_SECURE=false` only
there. Deploy fragments forward `Host` and append `X-Forwarded-For`; the
panel reads the last hop only from a trusted peer (right for Apache,
nginx, Caddy, Traefik, all of which merge repeated headers). `extract-cert.sh`
sound as a root cron script. 8080 bound to loopback on the test server;
Apache 2.4.68 fronts it with HSTS, the ACME exception and a redirect.

**Refuted hypotheses (not findings).** Postfix's `texthash` folds keys as
Unicode (it is ASCII — the envelope is held *more* strictly than the comment
assumes); an XFF the client sends ahead of Apache's can win (Apache appends;
the panel takes the last); `go-milter`'s vendored copy differs from upstream
beyond the documented patch (it does not); the DMARC ingest can be made to
expand entities or nested archives (it cannot); Cyrus's open CVE is
reachable (client-side DIGEST-MD5 only); the stdlib advisories apply to the
live binary (built with go1.26.9, which has the fixes).

---

## Plan

Seven stages. Stages 0-2 are product code, each a patch release in its own
right, each with "done when" being the test that proves it; stage H is the
owner's host and takes an hour of his own time; stages 3-6 are the rest in
the order that buys the most. The hours are the reader's estimates with the
coordinator's correction; tests and the CHANGELOG line are inside them.
Model routing per development.md (security/infra → Opus, docs → Sonnet).

**My recommendation.** Stage H today — S1 is the only finding here whose
worst case is a root shell, and it needs no code. Then stage 0 as one cut
(the high and four cheap guards, half a day), stage 1 as the next cut (the
tenant-boundary mediums, a day), stage 2 (the key group) when the e2e has a
free afternoon, and stages 3-6 in any order. Nothing here is "not planned":
the two findings I would *accept* rather than fix are named in stage 6 with
the argument.

### Stage H — the host (owner, ~1 h, no product code)

- [ ] **S1, S2, S5** — the commands, the package list and the "done when"
  of each are in `dev/security-audit-2026-10-17-host.md` (not published);
  delete that file once this stage is done.
- [ ] **S3.** DNS: `_dmarc.selfpost.mixfed.ru TXT "v=DMARC1; p=reject;
  rua=mailto:<an address you read>"`; `mixfed.ru CAA 0 issue
  "letsencrypt.org"`. Done when `dig +short TXT _dmarc.selfpost.mixfed.ru`
  answers.

### Stage 0 — the high and the cheap guards (~5 h) → patch release

- [ ] **A2.** `reauth` on user create, on a user update that carries
  `password` or changes `role`, and on user delete; the user form gets the
  `account_password` field the Backup page has; security.md § Backup and
  domain export says the user form is covered. Done when a test with a
  session and no password gets 401 on all three and the existing user-form
  tests pass with the field. — **Opus**, 2 h.
- [ ] **I2.** The three DMARC maps in `drop_links`; `[ -L ]` beside `[ -e ]`
  in postfix-config.sh:538,545; an e2e case plants a link and expects the
  log line. — **Opus**, 0.5 h.
- [ ] **M2.** Greedy reply group anchored on Postfix's own tail; the live
  line above as a test; security.md § Mail path reworded ("the last `;
  from=<`, which is Postfix's because a HELO name can hold none of
  `<>;\"`"). — **Opus**, 1 h.
- [ ] **M5.** Keep at most two `From:`/`Sender:` values; security.md:810
  reworded. — **Opus**, 0.5 h.
- [ ] **A4.** Duplicate-cookie line through `loggedPath`. — **Sonnet**, 0.25 h.
- [ ] **S6.** `smtpd_etrn_restrictions = reject`, `smtpd_discard_ehlo_keywords
  = etrn` in postfix-config.sh; architecture.md § Postfix one line. Done when
  an e2e `ETRN` on 25 gets `554`. — **Opus**, 0.5 h.
- [ ] **X1.** SECURITY.md's table: `latest 2.x` supported, `1.x` not. —
  **Sonnet**, 0.25 h.

### Stage 1 — the tenant boundary in the mail path (~7 h) → patch release

- [ ] **M1.** `overLimit` and `recordRejected` take the domain from the login
  (`ApplicationByLogin`), return early on an empty login; the reader's two
  tests (stranger; other tenant) become the regression tests; an e2e sends an
  unauthenticated `MAIL FROM` for a domain with a limit of 1 and then the
  domain's own message, which must pass. security.md § Mail path records
  that an unauthenticated session reserves nothing. — **Opus**, 2 h.
- [ ] **M3.** De-duplicate recipients in `inboundSession.RcptTo` (case-folded,
  as the outbound session does); the milter's inserts count toward the prune
  trigger (a counter in the store beside `InsertInboundSpam`, the prune
  called from whichever side crosses 500); one transaction per message.
  Done when a unit test sends one message with the same recipient 1 000
  times and sees one row, and 600 milter rows with no refusal trigger the
  cut. — **Opus**, 2 h.
- [ ] **W1.** Refuse a custom report address on the hostname; exact hosted
  address only in `AllowedRecipients`; a tag required for every recipient on
  the hostname in `checkReportDomain`; the existing test inverted; the
  accepted-risk text corrected. Check the live registry for a custom address
  on the hostname first (none expected). — **Opus**, 1.5 h.
- [ ] **A5/W2.** Delete a domain's reports in `domain.Service.Delete` (the
  cheap form); security.md § Roles says reports go with the domain. —
  **Sonnet**, 0.5 h. (The `domain_id` form is better and 2 h; take it if
  stage 1 is split across two sittings.)
- [ ] **W3.** A mutex around `Resync`'s read + write + reload. — **Sonnet**,
  0.5 h.

### Stage 2 — the key group (~2.5 h) → patch release, then the test server's `certs/`

- [ ] **I1.** `groupadd --system --gid 10997 backupkey; usermod -aG backupkey
  panel` in the Dockerfile; the guide's two `chgrp 10998` become `10997`
  with a sentence on why; `handlers_backup.go:398-399` likewise; the e2e
  stage gives the key to 10997 and `restore_test` asserts the `postfix` uid
  cannot read `/etc/postfix/tls/privkey.pem`; security.md § Output and
  process lists the three groups and who is in each. Rollout: `chgrp 10997
  /root/selfpost-deploy/certs/privkey.pem` and the same in
  `cert-deploy-hook.sh` on the test server. — **Opus**, 2.5 h.

### Stage 3 — limits and defaults (~7 h)

- [ ] **A1.** (a) Compose and guide set `TRUSTED_PROXY_CIDR` for the
  Apache-on-host scenario (the Docker gateway, with the command that prints
  it) and the panel logs a warning at start when the first N requests all
  come from one private peer carrying XFF and the list is empty; (b) the
  login limiter also keyed by username with its own window, the link-page
  limiters by token hash; (c) successful sign-ins not counted. security.md
  records (a)-(c). Done when the reader's test passes with two clients
  behind one untrusted peer. — **Opus**, 4 h.
- [ ] **A8.** One log line per refused sign-in or lockout, rate-limited per
  key. — **Sonnet**, 0.5 h.
- [ ] **A9.** IPv6 keyed by /64. — **Sonnet**, 0.5 h.
- [ ] **A3.** After five unsolicited reset links in a day, one mail saying
  further requests are ignored for 24 h, then silence. — **Opus**, 1.5 h.
- [ ] **I6.** `/healthz` answered only to loopback and trusted-proxy peers;
  the guide's "Container health" says the proxy may expose it on purpose
  with the fragment line. — **Sonnet**, 0.5 h.

### Stage 4 — resources and imports (~5 h)

- [ ] **I3.** `pids_limit: 512` in every compose fragment; the comment
  corrected; one e2e run. — **Sonnet**, 0.5 h.
- [ ] **W6/M9.** Coalesce Postfix reloads (reload only when the rendered map
  changed; a 2 s debounce for bursts); a per-user limiter on `dns-recheck`.
  — **Opus**, 1.5 h.
- [ ] **W4/W5/W8.** Key size 1024-4096 on import; ≤ 500 applications per
  export; an import named as the hostname refused with a sentence. —
  **Sonnet**, 1.5 h.
- [ ] **S4.** rspamd pin → `4.2.x` after reading `milter_headers`/`multimap`
  against the 4.x docs and one e2e run (`inbound_upstream_test`,
  `antispam_test`); `cap_drop: [ALL]`, `no-new-privileges`, `pids_limit` on
  the sidecar. — **Opus**, 1.5 h.

### Stage 5 — supply chain and currency (~2.5 h)

- [ ] **I4.** Actions pinned by SHA with the version in a comment;
  `govulncheck ./...` in `test.yml` and in `release.yml` before the push (a
  stdlib advisory fixed in the current patch of the pinned minor fails the
  build until the base tag moves — that is the point); `certbot` and
  `caddy` pinned in the fragments. Leave `provenance`/`sbom` off until the
  GHCR cleanup loop is taught to keep attestation manifests (a follow-up,
  1 h). — **Sonnet**, 1 h.
- [ ] **I5.** guide § Upgrading and SECURITY.md: base-package fixes arrive
  with the next patch release; the owner's rule on published tags. A weekly
  workflow that rebuilds the package list in `debian:trixie-slim` and fails
  when postfix/libsasl2/openssl/libdb moved, as the nudge. — **Sonnet**,
  1.5 h.

### Stage 6 — the rest, and what to accept (~3 h)

- [ ] **M7.** guide: upstream `encrypt` checks no certificate. — **Sonnet**,
  0.25 h. (Offering `secure` is hours and a `domain`-user-facing choice;
  propose it on the roadmap, not here.)
- [ ] **A6, A7, A10, W7, M6, M8** and the README note — half an hour each,
  or the wording change named under each. — **Sonnet**, 2 h.
- [ ] **Doc drift sweep** — every line under "Doc and comment drift" above.
  — **Sonnet**, 1 h.
- **Accept, with the argument:** *the setuid/setgid binaries of the base
  image* — inert under `no-new-privileges`, which the shipped compose sets
  and the e2e exercises; removing Debian's set is a maintenance cost at every
  base bump for no reachable gain. *`/data/antispam` world-readable* —
  already an accepted risk with its stated trigger (a shared host); the
  cheaper `group_add: ["10998"]` on the sidecar plus 0640 is worth 0.5 h when
  stage 4 touches that file anyway, and is listed there as optional.

### After the plan

security.md gets a paragraph in its history ("The 2026-10-17 security
audit…") like the earlier reviews, naming A2 and the seven mediums and what
closed them; the roadmap gets a row `security-audit-2026-10-17` whose
Progress column counts the `- [ ]` lines above, as the 2026-10-12 audit's
row does.
