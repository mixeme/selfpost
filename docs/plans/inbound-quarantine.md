# Plan: inbound-quarantine

**Status:** candidate — design TBD; needs explicit agreement before any code  
**Version:** TBD (`1.x` MINOR if opt-in and backward-compatible; may warrant `2.x`
if it changes inbound storage or backup contract).

---

## Goal (draft)

Let the operator **hold suspicious inbound mail** instead of only accept/reject
at the milter — review in the panel, then release to the configured upstream,
delete, or add sender to allow/deny lists.

This is a **product boundary question**, not a settled design. The item exists
so we can compare approaches before committing.

## Why it is separate from inbound-antispam-panel

[inbound-antispam-panel.md](inbound-antispam-panel.md) covers **metadata**
(journal + lists). Quarantine implies **storing message bodies** (or a durable
reference to them) under `/data`, retention, release workflow, and a wider
attack/backup surface — closer to "mini-mailbox" than pure relay.

## Open questions (to decide before a checklist)

1. **Where mail lives**
   - SelfPost store under `/data/quarantine/` (panel owns lifecycle), or
   - delegate to rspamd/Redis and panel only proxies release (less storage in
     SelfPost, tighter rspamd coupling).
2. **What "release" means**
   - inject into Postfix as a new delivery to the domain's upstream transport;
   - or manual download only (no automatic forward).
3. **Retention and caps**
   - max age, max total bytes, per-domain limits; interaction with backup size.
4. **RBAC**
   - global admin only vs domain-admin for assigned inbound domains.
5. **Threat model**
   - malware in stored MIME, path traversal on extract, quota DoS on busy MX.
6. **Product fit**
   - does this violate [product.md](../product.md) "no mailboxes" if we only
     hold spam suspects briefly? Explicit product decision required.

## Context (as-built)

- Inbound relay forwards or rejects; no local delivery ([inbound-relay.md](inbound-relay.md)).
- Optional `INBOUND_ANTISPAM_MILTER` — external rspamd may quarantine on its
  own today, without panel integration.
- DMARC `p=quarantine` is unrelated (outbound policy for receivers).

## Likely out of scope (until revisited)

- IMAP/webmail for quarantined mail.
- End-user self-service quarantine (non-admin recipients).
- Outbound quarantine.

## Dependencies

- [inbound-relay.md](inbound-relay.md) (shipped).
- Sensible to decide **after** or **alongside**
  [inbound-antispam-panel.md](inbound-antispam-panel.md) (journal/lists), but
  not blocked on it for design discussion.

## Done when (placeholder)

TBD once approach is chosen. Minimum bar would include: opt-in flag, panel
list/detail, release or discard action, retention, backup inclusion documented,
security review.

## Implementation checklist

None yet — expand this plan after the open questions above are answered.
