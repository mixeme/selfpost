# Plan: dmarc-reports

**Status:** candidate

---

## Goal

SelfPost **receives** DMARC aggregate reports on SMTP, parses them inside the
image, and **shows summaries in the panel** — pass/fail by source, hints when
`tighten p=` is reasonable. No external DMARC SaaS and no IMAP workflow for the
operator.

## Scope

**In:**
- Inbound SMTP for configured report addresses only (not a general backup-MX).
- gzip + XML aggregate parsing → SQLite summaries per sending domain.
- Panel page and/or per-domain section: recent reports, third-party senders,
  delivery health of report ingestion.
- Reuse the `dmarc_report_email` setting (moved off the old `admin` table into
  `settings` by migration `0005`) and `domains.dmarc_rua` for DNS templates;
  when enabled, suggest a SelfPost-hosted report address.

**Out:**
- Forensic reports (`ruf=`).
- Full dashboards, APIs, email alerting.
- Mailboxes for people (IMAP/POP3/webmail).

## Architecture (sketch)

1. Receiving MTAs → SMTP to SelfPost (hub MX).
2. Postfix virtual alias or dedicated listener → panel ingest worker.
3. Parse XML → `dmarc_reports` table (domain, reporter, counts, date).
4. Panel reads SQLite; links from domain DNS card.

May share port-25 plumbing with [inbound-relay.md](inbound-relay.md) but must
remain a separate, opt-in feature that does not forward mail upstream.

## Done when

- Operator can point `rua=` at an address SelfPost accepts and see parsed
  summaries in the panel within one reporting cycle.
- With the feature off, outbound-only behaviour is unchanged.
- Documented in [guide.md](../guide.md); migrations are backward-compatible.

## Risks

- Attack surface of accepting mail (mitigate: strict recipient allow-list).
- Report volume and retention (mitigate: caps + pruning).

## Implementation checklist

Target version cut: **`1.7.0`** (MINOR). One commit per step; code only after
roadmap status is **agreed**. Expand the sketch sections above before step 1
if still thin. See [development.md](../development.md) § Plan checklists.

- [ ] Expand plan: ingest path, `dmarc_reports` schema, retention caps — **Sonnet**
- [ ] Opt-in inbound SMTP for report addresses only (allow-list) — **Opus**
- [ ] Worker: gzip/XML parse → SQLite — **Opus**
- [ ] Panel: domain roll-up + parsed report (panel-ui mockups) — **Sonnet**
- [ ] Tie-in `dmarc_report_email` / `domains.dmarc_rua` — **Sonnet**
- [ ] Tests and [guide.md](../guide.md) — **Sonnet**
- [ ] Security review ingest path — **Fable**
- [ ] `go vet`, `go test` on touched packages — **Haiku**
