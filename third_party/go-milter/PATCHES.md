# go-milter v0.4.1, as SelfPost builds it

This is github.com/emersion/go-milter at v0.4.1 (BSD 2-Clause, `LICENSE`),
copied from the Go module cache, with one change. The main module takes it
through a `replace` in its `go.mod`; the module path stays the upstream one.
Left out of the copy: `cmd/` (a command-line tool SelfPost does not build) and
the protocol notes (`milter-protocol*.txt`).

## The change: headers are not kept

`session.go`, `Process()`, cases `CodeHeader` and `CodeEOH`. Upstream adds
every header of a session to a map (`m.headers`) that it empties only on
ABORT, and hands that map to the backend's `Headers` at the end of the
headers. Postfix bounds one header (60000 bytes with milters) but not their
number, so a message of nothing but headers, up to `message_size_limit`,
was held whole by the library in every milter that receives headers - the
sender check, the DKIM signer and the journal milter on 465/587, the inbound
journal milter on port 25 - on top of what the milters keep themselves
(security review of panel-dkim-signing, 2026-10-16).

Patched, `CodeHeader` hands each header to the backend's `Header` and keeps
nothing; `CodeEOH` hands `Headers` an empty map. No SelfPost milter reads
that map: the sender check and the signer keep the headers they need
themselves (internal/milter, `senderSession`, `signSession`), and the journal
milters do not implement `Headers`.

The client side (`client.go`, used by the unit tests) is unchanged.

## Taking a new upstream version

Copy the files again, apply the change above, and keep this note. Read the
new `session.go` first: if upstream bounds or drops the map itself, the
patch - and this copy - may go.
