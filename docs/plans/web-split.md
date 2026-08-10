# Plan: web-split (splitting `internal/web`)

**Status:** done (see [CHANGELOG](../CHANGELOG.md) `[Unreleased]`)  
**Version:** `1.x`; an internal refactor, it does not force a break on its own.

---

## What this is

`internal/web` is the project's largest package: ~50 files (templates and
static assets included), ~25 `.go` files and ~4000 lines of Go, with the
handlers for every panel section, sessions, security headers, origin checking,
form validation and template rendering all sitting in one flat namespace.

The candidates to split out are `web/handlers` and `web/auth`, or a cut along
the panel's own domains.

## Why now

At its current size the flat package reads fine: the file names
(`handlers_domains.go`, `handlers_apps.go`, `handlers_monitor.go`) do the work
directories would, and splitting would force exporting what is package-private
today — widening the internal API for cosmetics.

It starts to pay off once the package grows: **domain-admin** and
**inbound-relay** both add code to it — the role brings authorisation into
every handler, the inbound relay brings its own pages and handlers for inbound
domains. The refactor is cheaper before that growth than after it.

## Recommended order

**web-split → domain-admin → inbound-relay** (see the
[roadmap](../roadmap.md)).

1. **web-split** — lay down the package structure (including a place for
   `web/auth`) while there are no cross-cutting edits from the role and no new
   inbound handlers.
2. **domain-admin** — authorisation in every handler builds on a package layout
   already chosen.
3. **inbound-relay** — a new vertical slice; easier to add to an already split
   package than to refactor alongside the two features before it.

The order is a recommendation, not a blocker.

## Chosen scheme

**Horizontal split into four packages** (decided at implementation):

```
internal/web/          # Config, Server, New, Handler — composition root; security.go
internal/web/view/     # embed templates/static, render/renderFragment, staticHandler
internal/web/auth/     # session, login/logout/setup, requireAuth, currentUser
internal/web/validate/ # shared form validation (avoids auth ↔ handlers import cycle)
internal/web/handlers/ # all authenticated page handlers (handlers_*.go)
```

`cmd/panel` keeps importing only `internal/web`. Subpackages are not exported
beyond what the composition root needs.

## Done when

The package is split along the scheme above. After the split: `build`/`vet`/`test`
green, the panel's behaviour unchanged.

## Risks

- Splitting too early — a superfluous internal API and churn with nothing to
  show for it;
- leaving it until after the growth — a harder refactor, tangled up with the
  features.
