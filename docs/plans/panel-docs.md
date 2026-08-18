# Plan: panel-docs (in-panel operator documentation)

**Status:** agreed  
**Date:** 2026-08-17  
**Version:** `1.x` MINOR; `candidate` until explicitly agreed.

---

## Goal

Built-in operator documentation in the panel — short pages or a help drawer that
explain what each Status check and other controls mean, without sending the
operator out to [guide.md](../guide.md).

## Scope

**In:**

- Help drawer or short help pages (CSS-checkbox pattern from
  [panel-ui mockups](../assets/panel-ui/system.html)).
- Seed content: Status blurbs removed from cards for a denser layout — Machine
  (kernel counters / rate window), TLS certificate (port 465, reverse-proxy
  mount), Hostname / reverse DNS (FCrDNS, PTR at the hosting provider), and
  similar notes for other surfaces as inline commentary is removed.
- «?» entry points on domain cards (mockups).

**Out:**

- A second copy of the full operator guide.
- Translation workflow beyond English (same as the rest of the panel).

## Done when

An operator can open help from the panel for those topics; the removed Status
blurbs are preserved there (or equivalent); no requirement to read the git
tree for day-to-day meaning of a card.

## Risks

Copy ownership and keeping help in sync when checks change; not bloating every
page with a second column of prose.

## Implementation checklist

Target version cut: **`1.8.0`** (MINOR). One commit per step; code only after
roadmap status is **agreed**. See [development.md](../development.md) § Plan
checklists.

- [x] Help drawer / pages shell (CSS checkbox pattern from mockups) — **Sonnet**
- [x] Seed Status blurbs (Machine, TLS, PTR, …) — **Sonnet**
- [x] «?» entry points on domain cards — **Sonnet**
- [x] [guide.md](../guide.md) boundary: in-panel help vs full guide — **Sonnet**
- [x] Template tests — **Sonnet**
- [x] `go vet`, `go test` on touched packages — **Haiku**
