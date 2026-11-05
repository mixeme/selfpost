# Code review — 2026-11-05

**What this is.** A whole-tree review of SelfPost at `5867f97` (`2.16.13`, branch
`main`), done on the branch `review/2026-11-05` in a separate worktree. The whole
tree was read: the Go of `cmd/` and `internal/`, the templates and the panel's
static assets, `build/`, `deploy/`, the CI workflows and the e2e module's
helpers — against [architecture.md](architecture.md), [security.md](security.md)
and [product.md](product.md). Nothing was changed in the code; this file is the
only output, as with the two earlier passes
([audit-2026-10-12](audit-2026-10-12.md),
[security-audit-2026-10-17](security-audit-2026-10-17.md)).

Those two audits already closed a large amount of the code, and this pass
re-reads their conclusions rather than re-opening them: the areas where a
finding was expected and none was found are named at the end, so that the
absence of a finding is on record as *looked at*, not as *not looked at*.

**Not in scope.** The vendored `third_party/go-milter` — upstream's code with one
local change, described in [`PATCHES.md`](../third_party/go-milter/PATCHES.md);
its own `client_test.go` is upstream's. The design guards' internals
(`internal/web/*/guard_*_test.go` were read to see what they hold, not audited
against the mockups), and the e2e suite beyond its helpers — it needs the Docker
stand and was not run here. The security review proper (a Fable pass over the
diff, per [development.md](development.md) § Model routing) is a separate thing
and this file does not stand in for it.

**Severity.** As in the earlier audits: **critical** — the security boundary is
crossed or mail/data lost today, by a stranger; **high** — a documented
guarantee is false in a way an operator will hit; **medium** — wrong under a
realistic condition, or a stated guard that is not there; **low** — wrong under
an unlikely condition, or cost without failure; **nit** — hygiene, wording.
Confidence is *proven* (a test, a run, or an unambiguous reading of code the
running system reaches), *likely* (a clear reading, no reproduction) or
*possible* (a condition not shown to be reachable).

**The short version.** No critical and no high finding. Two mediums: a password
the panel asks for and then cannot hash, with the operator told only that an
internal error happened; and a release procedure written twice in
`development.md`, the two copies saying opposite things about what starts a
publish. Five lows, all cost or
inconsistency rather than a broken promise, and four nits. The mail path's
guarantees — the sender check, the signer, the rate limits, the tenant boundary,
the restore guards, the secret files — held up under this reading, and the
faults found are in the panel's edges rather than in what carries mail.

The second half of this file is a measurement of what the test suite costs and
where the time goes (§ The test suite's cost): the panel's product code is
fast, and ten minutes of suite on this machine is mostly bcrypt at the panel's
real work factor, a full template parse and a database migration in every test,
and four heavy packages contending for four cores.

---

## Findings

### Medium

**M1. A long password is refused with "Internal error", and the panel cannot
say why. `validate.AdminPassword` has a floor and no ceiling, while bcrypt takes
at most 72 bytes.** *Proven by a test run in this tree.*

[validate.go:42-50](../internal/web/validate/validate.go) enforces a floor of 12
characters and no ceiling; it accepts any longer value. Every writer of a panel
password then hands the value to `bcrypt.GenerateFromPassword`, which fails with
`bcrypt: password length exceeds 72 bytes` once the UTF-8 form is longer than
that. The five call sites are [auth/handlers.go:276](../internal/web/auth/handlers.go)
(first-run setup), [auth/links.go:335](../internal/web/auth/links.go) (a reset
link), [handlers_account.go:241](../internal/web/handlers/handlers_account.go)
(Account), and [handlers_users.go:213](../internal/web/handlers/handlers_users.go)
and `:321` (the user form). Each reports the failure as its own fixed sentence —
"Internal error. Please try again." on the Account and user forms, the same
wording on the reset and setup pages — so nothing tells the person that the
password is the problem or that a shorter one would work. The log gets the
bcrypt message, which the person filling the form does not read.

Measured in this tree (`AdminPassword` then `bcrypt.GenerateFromPassword`,
`bcrypt.MinCost`, 2026-11-05):

```
len= 71 bytes  AdminPassword ok  generate err=<nil>
len= 72 bytes  AdminPassword ok  generate err=<nil>
len= 73 bytes  AdminPassword ok  generate err=bcrypt: password length exceeds 72 bytes
len=200 bytes  AdminPassword ok  generate err=bcrypt: password length exceeds 72 bytes
cyrillic runes=40 bytes=80  AdminPassword ok  generate err=bcrypt: password length exceeds 72 bytes
```

The last line is the one that will actually be hit: the floor is counted in
characters ([validate.go:46](../internal/web/validate/validate.go), deliberately,
and [security.md](security.md) § Backup and domain export says so of the
backup password), so a passphrase of 25 Cyrillic letters is 50 bytes and works,
while 37 is 74 and does not — with no hint either way. A password manager's
40-character output, or any sentence in a non-Latin script, fails.

The price is bounded — a retry with a shorter password works, and the account is
not damaged — but the first-run setup is the path where it bites hardest: the
operator is looking at a form he cannot get past, on a fresh container, with a
message that points at the logs.

The fix is a rule in the validator plus a sentence: an upper bound of 72 **bytes**
for the administrator password (counted as bytes, unlike the floor), its own
message ("at most 72 bytes — the hashing algorithm's limit"), and the same rule
where the backup and export passwords are checked, which today
([handlers_backup.go:361-377](../internal/web/handlers/handlers_backup.go))
counts characters and has no maximum — harmless there, because scrypt has no
such limit, but the two forms should not read differently for the same field.
Whichever way it goes, [security.md](security.md) § Application SASL passwords
and § Backup and domain export name the rule and have to follow it.

The same limit has a second, invisible cost in the suite: `BcryptCost` is 12,
which is 543 ms a hash and 631 ms a comparison on the machine this was measured
on (§ The test suite's cost), and every test that signs in with a wrong name
pays one. A validator that refuses an over-long password at the form would not
change that; it is named here because both come from the same constant.

**M2. The release procedure is written twice in `development.md`, and the two
copies contradict each other and the workflow.** *Proven by reading; the
workflow file decides which copy is true.*

[development.md:330-357](development.md) (from `2.16.3` on, and the section the
release commits cite) says: "A pushed git tag `vX.Y.Z`, or a `workflow_dispatch`
that supplies that version (the tag must exist)", and "Workflow `release.yml`
runs on the tag push". That is what [release.yml](../.github/workflows/release.yml)
does — `on: push: tags: ["v[0-9]+.[0-9]+.[0-9]+"]`, with `prepare` reading
`github.ref_name` for a push and `inputs.version` for a dispatch.

[development.md:539-552](development.md) (§ CI) says the opposite, twice:
"Publishing a GitHub Release runs `release.yml` directly (`release: published`,
same pattern as gosentry / imap-scrub)", "A bare git tag push does not run the
workflow", and shows a diagram that begins `release: published` and derives the
version from `github.event.release.tag_name`. The workflow has no `release`
trigger and never reads `tag_name`.

Nothing in the code is wrong; what is wrong is that the document an operator
follows when cutting a version contains both instructions. The one it will hit
is the § CI copy, because that is the section about workflows: an operator who
publishes a GitHub Release and waits for the image waits for nothing, and one who
believes a tag push is inert may skip the tag, or believe a failed run left no
image. The § CI paragraphs look like the `2.16.2`-era text that § Release image
was rewritten out of, and they should go, leaving the one description beside the
steps that use it.

### Low

**L1. The outbound journal commits once per recipient inside Postfix's
end-of-message callback.** *Likely; not measured.*

[milter.go:249-269](../internal/milter/milter.go) loops over the message's
recipients and calls `InsertQueued` for each — one statement, and so one
transaction and one `synchronous=FULL` WAL commit
([store.go:38-64](../internal/store/store.go), [sendlog.go:65-77](../internal/store/sendlog.go)
is one `INSERT`), on the panel's single pooled connection, while Postfix
waits in `cleanup` for the milter's end-of-message reply
(`milter_content_timeout`, 30 s, [postfix-config.sh:493-496](../build/postfix-config.sh)). A message
with a thousand recipients is a thousand commits: a few milliseconds each on an
SSD (a second or two, fine), and tens of milliseconds each on a small VPS's
storage, which is where this software runs — at 20 ms per commit that is 20 s,
and past the timeout Postfix defers a message the panel could have accepted.

The inbound side is built the other way for exactly this reason:
`InsertInboundSpamMessage` writes all of a message's rows in one transaction and
says so ([inboundspam.go:107-140](../internal/store/inboundspam.go), "a thousand
recipients are one commit, not a thousand"). The outbound journal has no such
path, and `InsertQueued`'s signature has nothing like `[]SendLogEntry`. That the
rows are one per recipient is by design ([architecture.md](architecture.md) § Mail
path) and is not in question — the transaction boundary is.

Worth a measurement before a change: the e2e stand can send one message to *n*
recipients and time it — the mail.log shows the moment smtpd opened the message
and the moment cleanup answered it, and the difference is what a thousand
commits cost. Nothing in the tree measures a commit's duration today; the one
figure close to it, 1.9 s to delete 342 000 rows
([architecture.md](architecture.md) § Log tailer), is a batched write on a
faster path and does not answer this. If it is a problem, the fix has the shape
the inbound side already uses, with the level-2 reservations released after the
single commit instead of per row
([milter.go:189-195](../internal/milter/milter.go)).

**L2. The Outbound log reads every application's address list on every poll, for
a filter that needs only the logins.** *Proven by reading.*

[handlers_monitor.go:358-367](../internal/web/handlers/handlers_monitor.go)
builds the application filter by calling `ListApplicationsByDomain` for each
domain the viewer reaches, and that call is not one query: after draining the
applications it runs another query per application for its address rows
([applications.go:229-262](../internal/store/applications.go), the loop under
"Fill address lists after the first query is drained"). So one poll of the
Outbound log costs *one query per domain plus one per application* on the one
connection, every five seconds while an operator has the page open. `Table`,
`Mail queue` and the rest of the panel share that connection, and so does the
journal-milter ([architecture.md](architecture.md) § Persistence, the
single-connection trade-off).

For the several-domain instance this is designed for, nothing breaks; at fifty
domains and a few applications each it is some hundreds of queries per poll per
viewer, and the applications' own lists — which the filter never uses — are the
larger half. The address lists are
only needed for the `/outbound/log/{id}` page's `sendsAs`, not for the filter's
option list, which needs logins alone (`ListApplicationLogins`, unused anywhere
in the tree — [applications.go:262-283](../internal/store/applications.go) —
exists for exactly that purpose).

**L3. A successful e-mail change on Account spends a limiter attempt, so ten of
them lock the user out of backups.** *Likely.*

[handlers_account.go:121-139](../internal/web/handlers/handlers_account.go) takes
an attempt (`AllowPasswordChange`, ten in fifteen minutes per user) before
comparing the current password, and never gives it back on success. The other
callers of that limiter do: `reauth`
([handlers_backup.go:342-358](../internal/web/handlers/handlers_backup.go)) ends
with `PasswordCheckPassed(p.ID)`, and its comment says why — "a right one gives
its attempt back, so an administrator adding users is not locked out of the next
backup" ([security.md](security.md) § Backup and domain export repeats it). The
Account form is one of the checks that limiter covers ("shared with the Account
form", same sentence), so the rule as documented holds for the backup form and
not for the form beside it.

The scenario: an operator fixing his own address, mistyping the confirmation and
retrying, or a user with a password manager rotating addresses, reaches ten in a
quarter of an hour; every later e-mail change *and every backup download* then
answers "Too many attempts. Please wait and try again." for the rest of the
window. `HandleAccountPassword` ([handlers_account.go:218-233](../internal/web/handlers/handlers_account.go))
has the same shape for a changed password, though ten password changes in
fifteen minutes is harder to reach.

**L4. The Account profile is written in two statements, and a failure of the
second one is reported as if nothing was saved.** *Proven by reading.*

[handlers_account.go:140-157](../internal/web/handlers/handlers_account.go):
`UpdateUser` writes the username and the e-mail, and then `SetUserNotify` writes
the notifications switch separately; if the second fails, the page says "Could
not save the profile. Please check the logs and try again." while the username
and address are already changed — and the e-mail change, which is the one that
matters for reset links, may therefore have gone through while the operator is
told to try again. The panel is careful elsewhere about exactly this shape
(`SaveUser` writes a user's five fields in one transaction because a partial
save is worse than none,
[users.go:291-349](../internal/store/users.go)); the user's own two writes are
not. One statement (`UPDATE users SET username = ?, email = ?, … notify = ?`) or
a transaction settles it.

**L5. `architecture.md` undercounts the panel's roles.** *Proven by reading.*

[architecture.md:348](architecture.md) says "One process, seven roles (eight with
the inbound relay on: its inbound journal milter…)", and the list beneath it
enumerates **eight** — the last being the update check added in `2.16.12`
([architecture.md:396-399](architecture.md), CHANGELOG `[2.16.12]`). With the
relay on it is nine. The rest of the same section counts them correctly
("with the inbound relay on also the inbound journal milter",
[architecture.md:29](architecture.md)), so this is the one sentence left behind
by `2.16.12`. The Health page lists nine checks for the same reason
(CHANGELOG `[2.16.12]`), and an operator comparing the two counts will look for
a role that is not there.

### Nit

**N1. The origin check compares hosts byte for byte, so any difference in case
refuses every form.** [security.go:124-171](../internal/web/security.go) ends
with `return u.Host == r.Host`. Measured in this tree: with `Host:
Panel.Example.com` and `Origin: https://panel.example.com`, `originAllowed`
returns `false`, and every `POST` in the panel answers 403 with the log line
about a cross-origin request — which sends the operator to the reverse-proxy
section of the guide for a fault that is a `strings.EqualFold` away. A browser
lower-cases both sides, so this is not reachable from one by itself; a proxy or
a client that does not normalise reaches it. (`u.Host == r.Host` also compares
the port, which is right: both carry it or neither does.)

**N2. `/healthz` answers any method.** [web.go:373-387](../internal/web/web.go)
checks the caller's address and nothing else, so `POST /healthz` from inside the
container answers 200. A stranger's is refused earlier, by the origin check that
covers every non-read request — so the effect is cosmetic, but the probe is a
read and should say so.

**N3. The certificate check takes no clock, so its caller's `now` stops at the
renewal comparison.** *Proven by reading.*
[CheckCertificate](../internal/health/cert.go) has no `now` parameter and takes
`DaysLeft`, the expiry verdict and every sentence from `time.Until`/`time.Now`;
`CheckServedCertificate(served, mounted, interval, now)`
([cert.go:163-195](../internal/health/cert.go)) uses its `now` for the waiting
renewal alone and hands the mounted certificate to `CheckCertificate` with no
time at all. A caller that decides "this is when the panel was drawn" gets a
certificate verdict from the wall clock a moment later — a second's difference in
practice, but the two halves of one box then describe two different moments, and
a test that passes a fixed `now` cannot pin the expiry verdict it is testing.

**N4. The log-tailer's retry policy is read once, at start.** *Proven by
reading.* [logtail.go:316](../internal/logtail/logtail.go) calls `retryPolicy()`
before its loop, and the value bounds which stored lines belong to a row
(`SendLogLines`) and how long a row may stay open. `main.cf` is rewritten at
runtime — `cert-reload` runs `postconf -e` when it adopts a certificate that
appeared after the start (`build/postfix-cert-reload.sh:114-118`) — so an
operator who changes `maximal_queue_lifetime` by hand and reloads gets the new
value in the panel's pages ([cmd/panel/httpserver.go:100](../cmd/panel/httpserver.go)
reads it for the HTTP role at start, and the queues card shows that snapshot)
but not in the tailer until the container restarts. The panel documents the
snapshot for the pages ("reads it once … never re-reads it for a request",
[architecture.md](architecture.md) § Panel binary) and does not document it for
the tailer.

---

## Machine results

Run in the review worktree, Windows 10, Go 1.27.0, on 2026-11-05.

- `go build ./...` — clean.
- `go vet ./...` — clean.
- `gofmt -l .` — clean.
- `go test ./...` — every package green but three, and in them five tests that
  assume a POSIX filesystem: the class
  [audit-2026-10-12](audit-2026-10-12.md) § Machine results named for this
  platform, now four more of them. The run took **636 s**; what that time is
  made of is § The test suite's cost below.
  - `internal/domain` `TestWriteLoadPrivateKeyRoundtrip` — `key perm = 666,
    want 0600`: Windows has no POSIX modes.
  - `internal/logtail` `TestFollowTailsAndRotates`,
    `TestFollowCountsALogItCannotReadAndReadsItAgain`,
    `TestFollowReadsTheRotatedFileAfterTheSwitch` — a rename or a remove of a
    file the tailer still holds open is refused on Windows and is the point of
    the test.
  - `internal/postfix` `TestQueueIDsRefusesATruncatedListing` — the fake
    `postqueue` writes 5 MiB by re-executing the test binary, and composing
    that listing takes longer than the test's 5 s deadline on this machine, all
    the more so when the processors are busy. The product path is not involved.
- `internal/web/handlers` — the package timed out once (`panic: test timed out
  after 10m0s`, `running tests: TestApplicationSaveRefusalKeepsTheLoginFixed
  (16s)`) in the first full run, while the whole suite ran beside it, and
  finished at 312 s in a second full run. Recorded as a consequence of load
  rather than as a finding — the package's work is 172 s, and four packages
  that each want a core are sharing four — but it is the reason to watch this
  package's own time in CI, where the runner is slower than this machine and
  the 10-minute ceiling is the same.

The e2e suite was not run: it needs the Docker stand and its own 20 minutes, and
this review changed nothing for it to gate.

---

## Held up under this reading

Not findings, and not claims that nothing is wrong there — the places where this
pass read the code against the document and found the document true:

- **The sender check** ([milter/sender.go](../internal/milter/sender.go)): one
  `From:` read as `net/mail` reads it, the local part unquoted, ASCII folding
  only, an unreadable table defers mail, two `From:`/`Sender:` lines at most held
  (M5 of the 2026-10-17 audit), `Sender:` judged too, and the row of a refusal
  written off the mail path. The `Sender:` case and the duplicate-key case were
  re-derived from the code here, including that a duplicate key makes the whole
  texthash table unusable — which the check defers on.
- **The DKIM signer** ([milter/sign.go](../internal/milter/sign.go)): the key
  chosen by the socket and the login, never by a header; the 1 MiB header bound
  and the "deferred, never unsigned" paths; `h=` built from what the message has,
  over-signing only names that are present; the body hash over what Postfix
  delivers (`disable_mime_output_conversion`, `SMFIP_HDR_LEADSPC`).
- **The tenant boundary** ([handlers/authz.go](../internal/web/handlers/authz.go),
  `handlers_domains.go`, `handlers_inbound.go`, `handlers_monitor.go`,
  `handlers_dmarc_reports.go`): `/server/` guarded once by `server()`, per-object
  checks for both directions, lists filtered in the query, an empty scope
  matching no rows, one 404 for missing, another tenant's and unreachable alike.
- **Sessions, links and the password form**
  ([auth/session.go](../internal/web/auth/session.go),
  [store/userlinks.go](../internal/store/userlinks.go),
  [auth/links.go](../internal/web/auth/links.go)): SHA-256 of the token only, a
  session by user id, `DELETE … RETURNING` for a link taken once, the address
  checked again in the same transaction, the link's host the server's name and
  never the request's, and the answer to a reset request the same for every name.
- **The store's transaction discipline** ([store/sendlog.go](../internal/store/sendlog.go),
  `users.go`, `sessions.go`, `ratelimits.go`): a write first in every
  transaction that may write, `busy_timeout` under `_txlock=immediate`, the last
  global user counted inside the transaction that would remove it, the level-2
  reservation taken and counted under one lock
  ([milter/inflight.go](../internal/milter/inflight.go)).
- **Backup, restore and the secret files** ([backup/backup.go](../internal/backup/backup.go),
  `contents.go`, [secretfile/secretfile.go](../internal/secretfile/secretfile.go)):
  `VACUUM INTO` of a database that must already exist, a manifest and a record of
  every file compared before the database opens, links under `data/` refused
  rather than followed, kdf parameters bounded before a password is checked,
  chunk counters and an end-of-stream flag in the AAD, and bytes after the last
  chunk refused.
- **The build and the process model** ([build/](../build/),
  [deploy/docker-compose.yml](../deploy/docker-compose.yml)): the hostname gate
  before anything else, the link guard for root's steps, the modes of the key
  tree / `sasldb2` / `mail.log` / `filter.key` and why each is what it is, the
  milter wait before Postfix starts, `crashexit`, and the eight capabilities with
  a reason each.
- **The panel's component kit and guard tests**: the page data is typed, the
  kit is the only writer of its markup, `wbr_at` escapes before it inserts
  `<wbr>`, and no page carries an inline script or style.

---

## The test suite's cost, measured

A second pass, 2026-11-06, on the same machine (Windows 10, 4 logical CPUs, Go
1.27.0): where the ten minutes that `go test ./...` takes here are spent. Every
number below is from this tree, measured with `go test -count=1 -json` per
package and with throwaway tests since deleted; the product code was not touched.

### What it costs

| Package | Alone | Sum of the top-level tests | Tests | In `go test ./...` | Worst single test |
|---|---|---|---|---|---|
| `internal/web/auth` | 208.5 s | 208.3 s | 52 | **277.2 s** | `TestLoginBehindOneUntrustedProxyKeepsOthersIn` — 102.8 s |
| `internal/web/handlers` | 172.6 s | 171.9 s | 201 | **311.9 s** | `TestRightAccountPasswordsDoNotUseUpTheLimit` — 6.8 s |
| `internal/store` | 56.2 s | — | — | **218.2 s** | — |
| `internal/web/view` | 62.6 s | 62.4 s | 207 | **193.6 s** | `TestShowWhenBlocksArriveInTheirState` — 3.7 s |

The whole suite: **636 s** wall on this machine, 10.6 minutes.

Two things in that table matter more than the totals. First, the tests within a
package are sequential — the top-level sums match the wall time to within a
second — so a package's cost is the work of its tests, and `go test ./...`
overlaps only packages, one per CPU. Second, and this is the surprise: **a
package costs two to four times more in the full run than alone** — `store`
56 s → 218 s, `view` 63 s → 194 s, `handlers` 173 s → 312 s. Four packages that
each saturate a core are fighting over four cores, and the ones that are
memory- and syscall-heavy degrade worst. That is also the reason
`internal/web/handlers` timed out at ten minutes in the first full run of this
review and finished at 312 s in this one: it is the same work, against
different neighbours.

### Where it goes

**1. bcrypt at the panel's work factor, in the code under test.** Measured
directly: `bcrypt.GenerateFromPassword` at `BcryptCost` (12) is **543 ms**, and
`CompareHashAndPassword` **631 ms**, against 1.4 ms and 1.9 ms at `MinCost`.
The tests that pay it do so through the handler, not through a fixture:

- `submitLogin` compares against `noSuchUserHash()`
  ([auth/handlers.go:180](../internal/web/auth/handlers.go)) for **every
  unknown username**, at `BcryptCost`, so that the answer takes as long as a
  wrong password (audit 2026-10-12, M8 — a deliberate property of the product).
  `TestLoginBehindOneUntrustedProxyKeepsOthersIn`
  ([limitkeys_test.go:18-43](../internal/web/auth/limitkeys_test.go)) sends 115
  failed sign-ins, most of them for names that do not exist: 115 × 0.55 s ≈ 63 s
  of the test's 103. Six more tests in the package drive `postLogin` the same
  way.
- The setup, reset-link and Account tests generate real hashes at
  `BcryptCost` through the handlers (`BcryptCost` is a constant, not a fixture
  knob): `TestSetupRejectsCredentialsItWouldNotAcceptLater` 6.6 s,
  `TestResetSendsALinkOnlyToAConfirmedAddress` 8.2 s,
  `TestAccountPassword` 5.6 s, `TestAccountPasswordGuessesHaveTheirOwnLimit`
  4.0 s.
- `TestLoginTakesAsLongForAnUnknownName`
  ([handlers_test.go:293](../internal/web/auth/handlers_test.go)) is 16.6 s and
  **is** the measurement: eleven sign-ins at cost 12, by design. It should stay
  the only test paying that price.

**2. A fresh `view.New` and a fresh migrated database per test.** Measured:
`view.New("test")` — which parses every page template with
`html/template.ParseFS` — is **343 ms**, and `kitEngine` (which calls it) is
invoked in 17 places in the view tests. `store.Open` on an empty path is **93 ms**
(13 migrations applied, one transaction each), against **4 ms** on a file that
already has the schema; the panel's test stubs open a database per test
(`settingsServerAt`, and through it each of the `settingsServer` — 34 calls —
and `newAppStand` — 26), and the auth package does the same (`testModule`, 20
calls).

**3. RSA key generation per test.** `rsa.GenerateKey(2048)` is **347 ms** on
this CPU, and there are eight such calls in the suite: two in the handlers'
test stands, two in `health_test.go`, one in `milter/sign_test.go`, one in
`domain/transfer_test.go`, one in the e2e's `upgrade_test.go` and one in
`health_served_test.go`. `internal/domain`'s 71 s in the first full run is
largely key work of its own — `EnsureKey` in five places of `keys_test.go` and
the 4096-bit key `transfer_test.go` builds.

**4. Fixed sleeps.** The `logtail` tests wait on real time —
`blindMin + 500ms` in one ([logtail_test.go:528](../internal/logtail/logtail_test.go)),
50 ms polls in four others, `time.After(2*time.Second)` ceilings — and
`schema_test.go:421` sleeps 500 ms. The package is 12 s locally, small, but it
is the one place where the cost is by construction rather than by accident.

### What could be done, in order of gain for risk

1. **Let the tests pick the work factor.** `BcryptCost` is a `const`; making it
   a package variable (or adding an unexported one the tests can lower) would
   let the auth suite run at `MinCost` — roughly **100 s of that package's
   208 s alone, and a third of its 277 s under load** — while the product keeps
   12. The panel already treats the cost as a product decision recorded in
   [security.md](security.md); a test that needs the real cost
   (`TestLoginTakesAsLongForAnUnknownName`) sets it back explicitly. This is
   the single change that removes most of the time, and it is small.
2. **One `view.New` per package, shared.** `kitEngine` is `t.Helper()` plus a
   full parse of every template; a package-level `sync.OnceValue` would cut the
   17 parses to one. In `view` the parses are spread thin — the package's 62 s
   is 207 tests at a fifth of a second each, many of which build a page — so
   this is worth a few seconds there and rather more in `handlers`, where each
   of the 34 stands parses the templates again through `mustView`.
3. **One migrated template database, copied per test.** `store.Open`'s 93 ms is
   89 ms of migrations; opening a copy of a schema-initialised file is 4 ms.
   That is a fixture, and a fixture in this project's style needs to say why
   it is safe (`Open` also sets `PRAGMA application_id` and the file's mode) —
   worth doing for `store`, less clearly worth it for the panel's stands.
4. **A handful of fixed RSA keys as test data** instead of generating them in
   eight tests: ~2.8 s, and a 2048-bit test key in `testdata/` is not a secret
   (none of them signs anything real).
5. **The sleeps in `logtail`**: `blindMin` is a package constant; a test-only
   override would turn a 3.5 s wait into milliseconds. Low absolute gain, so
   last.

One thing that is **not** on this list: making the whole suite run with
`-p 1` or with more parallelism. The four heavy packages already overlap, and
the table above is what overlapping costs them; the answer is less work per
package, not a different schedule. Nor is the answer to shrink what the tests
assert — the 115 sign-ins in `TestLoginBehindOneUntrustedProxyKeepsOthersIn`
prove a rate-limit property the audit paid for, and the only thing wrong with
them is the price of bcrypt, not the count.

None of this changes what the tests assert. The reason to do it is arithmetic:
of the 636 s the suite takes here, about 100 s is bcrypt at cost 12 and most of
the rest is setup — templates parsed and databases migrated per test — that
four processes then contend for. On a CI runner slower than this machine the
same suite does not get shorter, and a suite that takes a quarter of an hour is
one people stop running before they push.

---

## What this pass did not settle

- The outbound journal's cost with many recipients (L1): a reading of the code,
  no measurement. The e2e stand can answer it.
- Whether anything but a proxy reaches the origin check's case-sensitivity
  (N1): proven that the comparison fails, not proven reachable from a browser.
- The Tailer's stale retry policy (N4) needs a container whose `main.cf`
  changed after its start, which no test here does.
