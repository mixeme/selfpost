# Plan: preflight (installation check page)

**Status:** candidate  
**Date:** 2026-08-19  
**Version:** `1.x` MINOR; `candidate` until explicitly agreed.

---

## Goal

A dedicated `/preflight` page (global-admin only) that runs infrastructure-level
checks and helps the administrator confirm the instance is fully configured and
ready to deliver mail. Each failed check shows an actionable recommendation
(what to do or where to look).

## Scope

**In:**

- rDNS / PTR verification (reuse `dnscheck.Checker.checkServer`)
- TLS certificate validity + hostname match (CN/SAN contains `SELFPOST_HOSTNAME`)
- Port connectivity: 25, 465, and 587 (if `SUBMISSION_ENABLE`) — dial own
  public IP from inside the container, expect a 220 banner
- HELO/EHLO banner — connect to localhost:25, verify Postfix announces
  `SELFPOST_HOSTNAME` (not `localhost` or a container ID)
- Trusted proxy headers:
  - If `TRUSTED_PROXY_CIDR` set: verify peer IP is in range and
    `X-Forwarded-For` is present
  - If `PANEL_COOKIE_SECURE=true`: verify `X-Forwarded-Proto: https`
  - If `TRUSTED_PROXY_CIDR` empty: informational warning (panel exposed
    directly)
- DKIM signing — OpenDKIM milter socket reachable (reuse `health.CheckSocket`)
- Send test email — admin enters recipient address, system sends via
  localhost:25, shows queued/error result

**Out:**

- Per-domain DNS checks (SPF, DKIM TXT, DMARC) — those live on domain detail
  pages.
- Delivery tracking of test email (only confirms local MTA accepted).

## Done when

An administrator can open `/preflight`, see a traffic-light summary of all
infrastructure prerequisites, read specific fix instructions for any failures,
and send a test email to verify end-to-end flow.

## Risks

- Port-connectivity check from inside the container may not reflect external
  reachability if NAT hairpin is unsupported (document limitation).
- Test email requires at least one configured domain (or falls back to
  `postmaster@SELFPOST_HOSTNAME`).

## Implementation checklist

Target version cut: TBD (MINOR). One commit per step; code only after roadmap
status is **agreed**. See [development.md](../development.md) § Plan checklists.

- [ ] Port-connectivity helper (`internal/health/port.go`) — dial own IP, read
      SMTP banner — **Sonnet**
- [ ] HELO banner check (connect localhost:25, parse 220 line) — **Sonnet**
- [ ] Hostname-in-certificate check (extend or wrap `health.CheckCertificate`)
      — **Sonnet**
- [ ] Handler `handlers_preflight.go` (assemble all checks, actionable
      recommendations) — **Sonnet**
- [ ] Test-email handler (`POST /preflight/test-email`, `net/smtp` to
      localhost:25) — **Sonnet**
- [ ] Template `preflight.html` (traffic-light cards + test-email form) —
      **Sonnet**
- [ ] Routes in `web.go`, nav link — **Sonnet**
