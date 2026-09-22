# Plan: domain-admin (domain administrator role)

**Status:** agreed  
**Version:** target bump **1.x** MINOR, given a compatible migration of the
current administrator into a global one.  
**Order:** recommended after [web-split](web-split.md), before
[inbound-relay](inbound-relay.md).

---

## What this is

Today the panel has exactly one subject: `requireAuth` is a boolean gate, not a
role ([web.go](../../internal/web/web.go) — the
`mux.Handle("/", s.requireAuth(authed))` wrapper), and the session carries
nothing beyond the fact of being signed in.

The role grants access to **explicitly assigned domains** (one or several); the
list of domains is set by the **global administrator**. For each domain on that
list:

- that domain's applications (creation, sender mode, password regeneration,
  deletion, its own L2 limit);
- the domain's DKIM/DNS status;
- the send log filtered to the domain — the filter already exists in the log
  ([sendLogData](../../internal/web/handlers_monitor.go)).

What stays outside the role is what is global by nature:

- adding and removing domains;
- creating domain-admin users and assigning domains to them;
- `/reload`;
- the full backup (that is all of `/data` including `sasldb2`, i.e. every
  domain at once);
- the queue and the `mail.log` tail — those are server-wide and not tied to a
  domain.

## Why this extends v1.0

[product.md](../product.md) puts "multiple panel users, roles" out of scope
(one administrator). A second subject is a deliberate widening of the project's
boundary, as inbound-relay is.

The cost is phase-sized, not patch-sized:

- a users table and their binding to domains;
- the role in the session;
- authorisation in every handler (not only on the route — today `{id}`/`{aid}`
  are checked for nothing beyond existence);
- reworking first-run setup and password change for several users;
- accounting for the new subject in backup and domain export.

*(The earlier wording of this item — "2FA and multiple administrators" — has
been replaced: 2FA is off the table, and "multiple administrators" is narrowed
to one specific role, because what is needed is not a second all-powerful admin
but limited access for the owner of one or several domains, with the list set
by the global administrator.)*

## Done when

- A global administrator and a domain-admin with different rights both work
  through the panel; the domain-admin cannot reach past the **assigned**
  domains;
- the current single admin migrates into a global one without losing access;
- backup/restore accounts for users and their bindings;
- `build`/`vet`/`test`/image green.

## Risks

- An incomplete `{id}`/`{aid}` check in a handler — access leaking to someone
  else's domain;
- breaking setup or backup — that would be a semver major, not 1.x.
