# SelfPost — product boundaries

**What this file is.** Stable product definition for SelfPost v1.0: purpose,
deployment assumptions, explicit out-of-scope items, and the multi-domain
model. User-facing overview in [README.md](../README.md); install and operations
in [guide.md](guide.md); as-built technical detail in
[architecture.md](architecture.md).

---

## Purpose

SelfPost is a self-hosted **outbound SMTP relay** with a web control panel,
shipped as a single Docker image. It sends mail directly to the internet from
your own IP with per-domain DKIM signing.

**Primary workflow:** configure the relay once (domains, DKIM, SASL
applications), then point scripts and apps at the SMTP endpoint. SelfPost
delivers as the configured sending domain.

---

## Deployment context (fixed assumptions)

These constraints are intentional; changing them requires an explicit product
decision:

1. **VPS or home server** — one image for both. Raspberry Pi and similar boards
   are not a current target but not ruled out forever.
2. **Send from your own IP (DIY)** — no intermediate relay provider. Requires
   outbound port 25, static IP, and configurable PTR/rDNS from the operator.
3. **Single container** — Postfix, OpenDKIM, and the panel run under one
   `supervisord` inside one image.
4. **Panel is internet-facing** — mandatory security requirements in
   [security.md](security.md).

**Infrastructure the operator provides (not SelfPost features):** unblocked
outbound TCP 25, static IP, PTR/rDNS, acceptable IP reputation. SelfPost does
not detect, bypass, or compensate for missing prerequisites; mail simply fails
to deliver when they are absent.

---

## Out of scope

Explicitly excluded to prevent scope creep:

- Inbound mail (IMAP/POP3, mailboxes, delivery to user inboxes)
- Webmail
- Organisations / tenancy beyond global + domain-admin roles; managing
  **multiple sending domains** under one global administrator is in scope (see
  below)
- Inbound antispam/antivirus engines (rspamd, ClamAV, etc.) — SelfPost may
  expose a milter hook; it does not ship or start a filter
- A custom MTA — Postfix is used as-is
- Dovecot or a full mail stack for SASL — Cyrus SASL (`sasldb2`) only

The **domain-admin** role ships in the current line (global administrator plus
domain administrators with assigned domains). The optional **inbound relay**
(backup-MX / forwarder on port 25) ships in `[1.4.0]`, off by default behind
`INBOUND_RELAY_ENABLE`; it is relay/forward, not IMAP/webmail. **Send-log
retention in Settings** ships in `[1.5.0]`. **30-day send statistics** and
**auto level-2 rate limits** ship in `[1.6.0]`. **DMARC aggregate report
ingest** ships in `[1.7.0]`, off by default behind `DMARC_REPORTS_ENABLE`.
Items marked *candidate* in the
[roadmap](roadmap.md) require explicit approval before coding.

---

## Multi-domain model

SelfPost is a **multi-domain outbound relay**. Two linked entities:

### Sending domain

Example: `example.com`. Has its own DKIM key and selector.

### Application (account)

A SASL login/password bound to **one** domain. A domain may have several
applications (e.g. newsletter vs alerts), each with its own credentials, but
each may send **only from its own domain**, never from another.

### From-address mode (per application)

Set when creating or editing an application:

1. **Any address in the domain** — `*@example.com` (wildcard in
   `smtpd_sender_login_maps`).
2. **Explicit address list** — only listed From addresses within the domain;
   anything else is rejected even if it belongs to the same domain.

In both modes the From address must belong to the application's domain.

### What the panel manages per domain

- DKIM key + selector (shared by all applications on that domain)
- One or more applications (SASL credentials + From mode + optional rate limits)
- DNS guidance (DKIM TXT; reminders for SPF/DMARC)

Adding a domain does **not** create an application automatically.

### Lifecycle

- **Add domain** — record, DKIM key, DNS instructions; no application yet.
- **Add application** — SASL pair (password shown once), From mode, map entries,
  `postfix reload`.
- **Delete domain** — removes DKIM key and **all** its applications.
- **Delete application** — removes only that app's SASL and map entries.

This is not multi-tenancy; it is one owner (or a small team with global and
domain-scoped roles) operating several sending domains with independent
application credentials.
