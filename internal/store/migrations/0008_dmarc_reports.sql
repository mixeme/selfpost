-- DMARC aggregate report summaries (plans/dmarc-reports.md). One row per parsed
-- report; per-source rows hang off it. Forensic (ruf=) payloads are not stored.

CREATE TABLE dmarc_reports (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    domain        TEXT NOT NULL,
    reporter      TEXT NOT NULL,
    report_id     TEXT NOT NULL,
    period_begin  TEXT NOT NULL,
    period_end    TEXT NOT NULL,
    received_at   TEXT NOT NULL,
    contact_email TEXT NOT NULL DEFAULT '',
    policy_p      TEXT NOT NULL DEFAULT '',
    policy_sp     TEXT NOT NULL DEFAULT '',
    policy_pct    INTEGER NOT NULL DEFAULT 100,
    policy_adkim  TEXT NOT NULL DEFAULT '',
    policy_aspf   TEXT NOT NULL DEFAULT '',
    pass_count    INTEGER NOT NULL DEFAULT 0,
    fail_count    INTEGER NOT NULL DEFAULT 0,
    recipient     TEXT NOT NULL DEFAULT '',
    UNIQUE (reporter, report_id, domain)
);

CREATE INDEX idx_dmarc_reports_domain ON dmarc_reports (domain);
CREATE INDEX idx_dmarc_reports_received ON dmarc_reports (received_at);

CREATE TABLE dmarc_report_records (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    report_row_id INTEGER NOT NULL REFERENCES dmarc_reports(id) ON DELETE CASCADE,
    source_ip     TEXT NOT NULL DEFAULT '',
    count         INTEGER NOT NULL,
    disposition   TEXT NOT NULL DEFAULT '',
    spf_result    TEXT NOT NULL DEFAULT '',
    dkim_result   TEXT NOT NULL DEFAULT '',
    header_from   TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_dmarc_report_records_report ON dmarc_report_records (report_row_id);
