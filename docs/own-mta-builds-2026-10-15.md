# Own builds of Postfix and OpenDKIM - the analysis of 2026-10-15

**Question (owner, 2026-10-15; roadmap [own-mta-builds](roadmap.md#own-mta-builds)):**
is it worth patching Postfix and OpenDKIM and shipping our own builds, to
optimise something in SelfPost or to lift a limit we work around?

**Answer:** no to carrying patched builds of either - nothing we work around
needs a patch, and a patch set makes one maintainer the security team of
two MTAs. The question does point at two real problems, each with a cheaper
answer:

1. **The image's base is out of date.** `debian:bookworm-slim` left regular
   security support on 2026-07-12; its Postfix `3.7.11` is code of mid-2024,
   and the three Postfix security batches of 2026 (June, August, September)
   carry no CVE ids, so nothing will bring them to bookworm's LTS. The
   answer is Debian trixie (`postfix 3.10.13`, the August batch included),
   not a build of our own: proposed as roadmap row
   [base-trixie](roadmap.md#base-trixie).
2. **OpenDKIM has no upstream left.** The package is a beta of 2018; three
   CVEs of 2026 are open in every Debian suite with "the vendor did not
   respond"; Debian's bug on them has had no answer since 2026-10-03. Every
   limit we have worked around is OpenDKIM's. A patched OpenDKIM of our own
   would make us its maintainer; signing in the panel's own milter, which
   already runs in the mail path, removes the program and most of what was
   built around it: proposed as roadmap row
   [panel-dkim-signing](roadmap.md#panel-dkim-signing).

A side finding of the reading: port 25 of the inbound relay accepts bare
newlines - Postfix 3.7's default for the 2023 SMTP-smuggling parameter -
one line fixes it: [port25-bare-newline](roadmap.md#port25-bare-newline).

Everything below is the evidence and the prices. Versions are those of the
image (`docs/development.md`, the cited-package table); upstream source is
`dev/upstream/`; web sources are linked where a fact came from one.

---

## 1. What the image runs, and who maintains it now

| Component | In the image (bookworm) | In Debian trixie | Upstream |
|---|---|---|---|
| Debian | 12 bookworm, LTS since 2026-07-12, until 2028-06-30 ([debian.org](https://www.debian.org/News/2026/20260712.en.html)) | 13 trixie, stable since 2025-08-09 | — |
| Postfix | `3.7.11-0+deb12u1` (unchanged since the image was pinned; [tracker](https://tracker.debian.org/pkg/postfix)) | `3.10.13-0+deb13u1` (trixie-security) | `3.11.7`, stable branches 3.8–3.11 ([3.11.7](https://postfix.cs.utah.edu/announcements/postfix-3.11.7.html)) |
| OpenDKIM | `2.11.0~beta2-8+deb12u1` | `2.11.0~beta2-9.1` (same code; a gcc-14 build fix, a path move, one verifier-side header patch) | `2.11.0-Beta2`, 2018; no release since ([releases](https://github.com/trusteddomainproject/OpenDKIM/releases), [issue #422](https://github.com/trusteddomainproject/OpenDKIM/issues/422), unanswered) |
| libmilter | `8.17.1.9-2+deb12u2` | `8.18.1-6` | — |
| Cyrus SASL | `2.1.28+dfsg-10` | `2.1.28+dfsg1-9` (same upstream) | — |
| supervisor | `4.2.5-1` | `4.2.5-3` (same upstream) | — |
| logrotate | `3.21.0-1` | `3.22.0-1` | — |
| Go build stage | `golang:1.26-bookworm` | `golang:1.26-trixie` exists (Docker Hub, 2026-10-10) | — |

**Postfix in bookworm is 2024 code with the 2026 fixes missing.** Upstream
issued three security batches this year, all without CVE ids:

- June 2026: unbounded reads of SMTP commands, responses and BDAT data
  ("large SMTP inputs"); a TLSA-parsing crash (DANE only - not us).
- August 2026 ([3.11.6](https://openwall.com/lists/oss-security/2026/08/20/5)):
  sixteen items, among them a remote memory exhaustion by many small BDAT
  commands (smtpd, since 3.4), a remote smtpd crash by DATA without MAIL
  FROM after a BDAT error, a policy bypass after an
  `smtpd_end_of_data_restrictions` reject (we set none), and the Milter
  left in "accept" state by the same bug.
- September 2026 ([3.11.7](https://postfix.cs.utah.edu/announcements/postfix-3.11.7.html)):
  two SMTP-smuggling cases behind `smtpd_proxy_filter` (we use none), a
  crash under `strict_smtputf8` (not set), a multi-line response over-read
  reachable by a malicious Milter, a `postqueue` use-after-free.

Legacy releases 3.7.22 and 3.7.23 exist upstream but, as the announcements
say, "do not include the patches for out-of-support Postfix versions" of
June. Debian's tracker lists one 2026 CVE for bookworm's Postfix
([CVE-2026-43964](https://security-tracker.debian.org/tracker/CVE-2026-43964),
a one-byte over-read, `no-dsa`) and no bookworm upload at all in 2026; the
LTS team works from CVE ids, and these batches have none. The exposed
listeners are ours: 465 and 587 to the Internet, 25 when the inbound relay
is on.

**OpenDKIM has three open CVEs and nobody to fix them.** Debian bug
[#1149875](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=1149875)
(2026-10-03, severity important, no reply from maintainer or security team
as of 2026-10-15) covers
[CVE-2026-100888](https://security-tracker.debian.org/tracker/CVE-2026-100888)
(`dkim_canon_selecthdrs`, out-of-bounds write),
[CVE-2026-100889](https://security-tracker.debian.org/tracker/CVE-2026-100889)
(`dkim_qp_decode`, off-by-one) and
[CVE-2026-101277](https://security-tracker.debian.org/tracker/CVE-2026-101277)
(`dkim_process_set`, the tag tokenizer); all three "vendor was contacted
early but did not respond in any way". Read in `dev/upstream/opendkim-2.11.0~beta2`
for our configuration (`Mode s`, attached to 465/587 and to the panel's
local submission only; never to port 25):

- `dkim_qp_decode` is called from `dkim_sig_domainok()`,
  `dkim_sig_getreportinfo()` and `dkim_sig_getidentity()`
  (`libopendkim/dkim.c:1718, 7712, 7732, 7801`) - verifier-side functions,
  reached from `dkim_eoh_verify()`. Not on our path.
- `dkim_process_set` runs on every `DKIM-Signature:` header of a message
  **in either mode**: `dkim_header()` (`dkim.c:6447`, the call at `:6625`)
  has no mode gate. On our path that header comes only from an authenticated
  application; the exposure is a tenant's, not the Internet's.
- `dkim_canon_selecthdrs` runs when signing as well as when verifying
  (`dkim_canon_runheaders()`, `dkim-canon.c:1194, 1280`). The write past
  the array is `lhdrs[shcnt] = NULL` (`dkim-canon.c:1052`) once every
  header of the message has been matched and a further name follows. When
  verifying, the names are the signature's `h=`, the attacker's. When
  signing, OpenDKIM composes the list itself - the headers present that
  are in `SignHeaders` (by default RFC 6376 § 5.4's SHOULD list,
  `dkim_should_signhdrs[]`, `dkim.c:223`) plus `OversignHeaders`, our
  `From` - so the condition is a message whose every header is in that
  list. Whether Postfix's own `Received:` keeps it out of reach I did not
  run; it is one ASan run with `miltertest` on the stand to know, and the
  answer changes only how soon § 4's row is wanted, not whether.

A patch for each exists in the researchers' write-ups (for `dkim_qp_decode`:
[weitongli.com](https://weitongli.com/share/opendkim-qp-off-by-one.html)).
Nobody upstream will take them; Debian may yet carry them - the bug is
twelve days old - but the project behind the package is gone, and the
next finding meets the same silence.

## 2. The limits met so far, and what would lift each

| Limit (where it is worked around) | Own patched build | Debian trixie | Signing in the panel |
|---|---|---|---|
| OpenDKIM logs through syslog(3) only (`internal/syslogrelay`, a supervisord program, `/dev/log` in the entrypoint; audit L30) | yes, a stderr patch | no | yes - the program goes, and the relay with it (nothing else in the container uses syslog(3)) |
| The panel reads Postfix's state from `mail.log`, `postconf -h -x`, `postqueue -p`/`-j` | no: these *are* Postfix's interfaces; a patch would invent a private one to maintain | partly: 3.11 adds `postconf -j` (JSON) - not in trixie's 3.10 | no (unchanged) |
| The sender check reads `From:` the way OpenDKIM does: `dkim_mail_parse()` ported (`internal/milter/mailparse.go`, 327 lines), the 256-byte `MAXADDRESS` refusal, two diff/probe scripts against the C | no: the port exists because two programs parse the line; a patch does not merge them (OpenDKIM's `SenderMacro` could pick the key by the envelope instead - it changes nothing, since the check already pins `From:` to the login's senders) | no | yes: one parser, ours, and the key chosen by the login's domain, not by `From:` - the class "signed with another tenant's key" is gone by construction; `From:` still has to be the application's (DMARC alignment), with `net/mail` |
| The image held to bookworm's versions (Postfix 3.7.11, OpenDKIM 2.11.0~beta2) | yes, at the price in § 3 | yes for Postfix (3.10.13, security-tracked to 2028 and beyond in trixie LTS); OpenDKIM's version is the same everywhere | OpenDKIM's version stops mattering |
| A socket file outlives a crashed OpenDKIM (`health/socket.go`, assumptions #38) | yes, with a patch | no | yes - no second milter process |
| Keys group-readable, `RequireSafeKeys no`, the `selfpost` group between panel and OpenDKIM | no | no | yes - the panel reads its own keys; the group stays for Postfix's sasldb and maps |
| `SIGUSR1` through supervisord to reload the tables, which is what needs `CAP_KILL` in the compose file (cross-uid kill) | no | no | yes - no tables, no signal; `CAP_KILL` can probably go (to verify on the stand: supervisord then signals only root's wrapper and the panel's own uid) |
| `smtpd_forbid_bare_newline` defaults to `no` on Postfix < 3.9 (`src/smtpd/smtpd.c:807`); our port 25 does not set it | yes | yes - the default is `normalize` since 3.9 | no (it is Postfix's) - see § 5 |

Nothing in the Postfix column needs a patch. The limits are OpenDKIM's.

## 3. The options, priced

Prices are estimates in stages of the kind the 2026-10-12 audit used (a
stage: write, tests, two reviews, e2e, rollout, commit). Nothing here was
measured on the stand; where a number matters it says so.

### A. Patched builds of both, carried by us - **not recommended**

- *Gives:* the stderr patch for OpenDKIM, the three CVE fixes, any option
  compiled in or out, the newest Postfix on the day we rebuild.
- *Costs, once:* a build stage per program (`apt-get source`, quilt, a
  patch series, `dpkg-buildpackage`), its build-dependencies, the e2e
  `checkCitedPackageVersions` rewritten for our version strings, every
  citation of `3.7.11` (331 lines in 65 files) and `2.11.0~beta2` (55 in
  14) re-pointed at our tree. Image build time is not measured; from the
  size of the sources it is minutes, not seconds, on every rebuild.
- *Costs, for ever:* Postfix issued seven releases in 2026; each is ours
  to notice, rebase the series on, rebuild and roll out, with no Debian
  security team between the announcement and the image. For OpenDKIM there
  is no upstream to rebase on at all: the series *is* the program, and
  the next CVE is ours to find and fix. One maintainer, two MTAs.
- *Why not:* § 2 shows no Postfix limit a patch lifts, and every OpenDKIM
  limit lifted by option E without taking OpenDKIM on.

### B. Unpatched upstream build where the distribution's version is the limit - **not recommended**

- *Gives:* Postfix 3.11.7 today instead of trixie's 3.10.13 - the
  September batch two to four weeks earlier (trixie-security carried 3.10.11
  → 3.10.12 → 3.10.13 between July and August; 3.10.14 is upstream now).
  For OpenDKIM there is no newer upstream to build.
- *Costs:* A's build stage and watching duty for Postfix, minus the patch
  series; Debian's own patches (paths, SASL and LMDB build options)
  become ours to reproduce.
- *Why not:* the gap to trixie-security is weeks, the duty is permanent.

### C. A patch sent upstream instead of carried - **nothing to send**

- Postfix: no code change is needed (§ 2); Wietse Venema's project is
  responsive, but we have no patch for it.
- OpenDKIM: the project does not answer CVE reporters, the Debian
  maintainer or its own issue tracker. A patch sent there is a patch
  carried - option A.

### D. The base image to Debian trixie - **recommended, next code row** → [base-trixie](roadmap.md#base-trixie)

- *Gives:* Postfix 3.10.13 with the June and August batches and the
  September one when trixie-security ships 3.10.14; Debian's security team
  until 2028, then trixie LTS; `smtpd_forbid_bare_newline=normalize` by
  default; `smtpd_hide_client_session` (3.10: a `Received:` header without
  the application's address - a product question of its own, not decided
  here); OpenSSL 3.5; libmilter 8.18.1.
- *Costs:* one stage, one MINOR release. The work is the re-reading: the
  331 citation lines against the 3.10.13 tree (most will hold word for
  word; `src/` paths are stable), with attention to what 3.8–3.10 changed
  on our paths - reject logging now carries `sasl_method`, `sasl_username`,
  `sasl_sender` (3.10, 20241023: `refusals.go`, `inbound.go` regexes), the
  queue id or `NOQUEUE` on an abnormal connection end and cleanup's
  `canceled` line (3.10: new line shapes for the log tailer), `postfix(1)`
  logging to stderr when it is not a terminal (3.8: the wrapper's output
  under supervisord), DNS results capped at 100 records and stray CR/LF
  replaced by default (3.9), the removed parameters (3.9:
  `permit_naked_ip_address`, `reject_maps_rbl`, `check_relay_domains` - we
  set none). Then: `dev/upstream` refetched from trixie, the cited-package
  table and `checkCitedPackageVersions`, the e2e sink image, the build
  stage tag, `docs/assumptions-2026-10-14.md`'s header, the guide's
  "what the image contains". OpenDKIM's citations do not change (same
  upstream code).
- *Risk:* a log line we parse changing shape unnoticed - the e2e suite
  and the fixtures labelled "seen" are the net; a day on the test server
  before the cut.

### E. DKIM signing in the panel's own milter; OpenDKIM removed - **recommended, after D** → [panel-dkim-signing](roadmap.md#panel-dkim-signing)

- *Gives:* every OpenDKIM row of § 2; about 1,500 lines gone (the relay
  and its tests, the parser port and its tests, the probe scripts, the
  table renderer and its reload, `opendkim.conf`), two programs fewer
  under supervisord, two packages fewer in the image, no CVE watch on a
  dead project, probably `CAP_KILL` dropped. Keys, selectors and the
  published DNS records are unchanged: the panel already generates the
  keys (`internal/domain/dkim.go`, PKCS#1 PEM) - nothing for an operator
  to do.
- *How:* `github.com/emersion/go-msgauth/dkim` (v0.7.0, 2025-04, MIT; the
  author of the `go-milter` we run; the e2e suite already verifies with it).
  Read for this analysis: `SignOptions` gives `relaxed`/`simple` per side,
  `HeaderKeys` with over-signing as RFC 6376 § 5.4.2 wants it (a name
  listed once more than present is kept in `h=` and nothing is hashed for
  it, `sign.go`), last-instance-first header picking (`header.go`), RSA and
  Ed25519, and a streaming `NewSigner` (the body is hashed as it passes,
  not buffered). The milter: headers and body chunks in, `InsertHeader(0,
  "DKIM-Signature", …)` at end of message - the shape of go-msgauth's own
  `cmd/dkim-milter`, some 450 lines. The key: by the login's domain on
  465/587 (`Bindings.DomainOf`, which the sender check has), by the
  server's name for the panel's local submission (`non_smtpd_milters`
  pointed at the same socket - strict, as OpenDKIM is today). The header
  set: `dkim_should_signhdrs[]`, `From` over-signed once; canonicalization
  `relaxed/simple`, as now.
- *Costs:* two to three stages, one MINOR release: the signer and its
  tests; the removal (code, image, compose, supervisord, entrypoint,
  Health's liveness set, Resync's steps, docs: architecture § OpenDKIM,
  security, guide, development's table, assumptions § 3); e2e with an
  **independent verifier as the oracle** - go-msgauth must not check its
  own work: `opendkim-testmsg` in the sink image or rspamd's verifier, the
  way `mailparse-diff.sh` holds the port to the C today - and a message to
  a real mailbox before the cut.
- *Risk:* a signing defect fails DMARC for every tenant at once; the
  oracle and the canary are the answer. go-msgauth is a small library with
  a quiet release pace - vendorable if it ever matters.
- *Why after D:* D's exposure is remote and unauthenticated (465/587),
  E's is a tenant's; D is one stage, E is three; neither waits on the
  other (E re-reads no Postfix citation, D no OpenDKIM one).

## 4. Recommendation

In this order, each a roadmap row of its own, none of them an own build:

1. **[port25-bare-newline](roadmap.md#port25-bare-newline)** - one line in
   `build/postfix-config.sh`, a PATCH release, now (§ 5).
2. **[base-trixie](roadmap.md#base-trixie)** - option D, one stage, MINOR.
3. **[panel-dkim-signing](roadmap.md#panel-dkim-signing)** - option E, two
   to three stages, MINOR.

Options A, B and C are declined with the arguments above; what would bring
A back is a limit of Postfix's that its interfaces cannot answer, and none
is known.

## 5. Side finding: bare newlines on port 25

Postfix 3.7.11 has the 2023 SMTP-smuggling parameter
(`smtpd_forbid_bare_newline`, `src/global/mail_params.h:4249`) with the
default `no` "for Postfix < 3.9" (`src/smtpd/smtpd.c:807`); its
`RELEASE_NOTES` recommend `normalize`, which 3.9 made the default. The
inbound relay's listener (`build/postfix-config.sh`, the `smtp/inet`
block) does not set it, so with `INBOUND_RELAY_ENABLE=true` the relay is
the receiving side the smuggling research described: a bare `<LF>.<LF>`
inside DATA ends the message early and what follows is read as the next
transaction. Ports 465 and 587 are not the case that matters - the
smuggled transaction is still the authenticated session's, under its
sender bindings and milters. The fix is
`smtp/inet/smtpd_forbid_bare_newline=normalize` with an e2e check on port
25 (a bare-LF DATA, one message delivered); a PATCH release. Trixie's 3.10
would set it by default, but the relay should not wait for that row.

A smaller one: `opendkim-tools` is installed in the image and used by
nothing at run time (the key is generated in Go; the probe script runs on
the server from source). It goes with option E, or with D if E waits.
