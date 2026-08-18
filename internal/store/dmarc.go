package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	// DMARCReportsMaxKeep is how many parsed reports are kept before pruning.
	DMARCReportsMaxKeep = 500
	// DMARCReportsMaxAgeDays drops reports older than this window.
	DMARCReportsMaxAgeDays = 90
	// DMARCParseFailuresKey counts ingest failures (settings).
	DMARCParseFailuresKey = "dmarc_parse_failures_total"
)

// ErrDMARCReportNotFound is returned when a report id does not exist.
var ErrDMARCReportNotFound = errors.New("dmarc report not found")

// DMARCReport is one parsed aggregate summary.
type DMARCReport struct {
	ID           int64
	Domain       string
	Reporter     string
	ReportID     string
	PeriodBegin  time.Time
	PeriodEnd    time.Time
	ReceivedAt   time.Time
	ContactEmail string
	PolicyP      string
	PolicySP     string
	PolicyPct    int
	PolicyADKIM  string
	PolicyASPF   string
	PassCount    int
	FailCount    int
	Recipient    string
	Records      []DMARCReportRecord
}

// DMARCReportRecord is one source row inside a report.
type DMARCReportRecord struct {
	SourceIP    string
	Count       int
	Disposition string
	SPFResult   string
	DKIMResult  string
	HeaderFrom  string
}

// DMARCReportSummary is a list-row without per-record detail.
type DMARCReportSummary struct {
	ID          int64
	Domain      string
	Reporter    string
	PeriodBegin time.Time
	PeriodEnd   time.Time
	ReceivedAt  time.Time
	PassCount   int
	FailCount   int
}

// DMARCSourceRollup aggregates pass/fail per source over a window.
type DMARCSourceRollup struct {
	SourceIP    string
	PassCount   int
	FailCount   int
	Disposition string
}

// DMARCIngestStats is panel-facing ingest health.
type DMARCIngestStats struct {
	LastReceivedAt *time.Time
	KeptThisWeek   int
	ParseFailures  int
	IngestOK       bool
}

// InsertDMARCReport stores a parsed report and its records, replacing any prior
// row with the same reporter/report_id/domain triple.
func (s *Store) InsertDMARCReport(rep DMARCReport) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin insert dmarc report: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`DELETE FROM dmarc_reports WHERE reporter = ? AND report_id = ? AND domain = ?`,
		rep.Reporter, rep.ReportID, rep.Domain,
	); err != nil {
		return 0, fmt.Errorf("delete prior dmarc report: %w", err)
	}

	res, err := tx.Exec(`
		INSERT INTO dmarc_reports (
			domain, reporter, report_id, period_begin, period_end, received_at,
			contact_email, policy_p, policy_sp, policy_pct, policy_adkim, policy_aspf,
			pass_count, fail_count, recipient
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rep.Domain, rep.Reporter, rep.ReportID,
		rep.PeriodBegin.UTC().Format(time.RFC3339),
		rep.PeriodEnd.UTC().Format(time.RFC3339),
		rep.ReceivedAt.UTC().Format(time.RFC3339),
		rep.ContactEmail, rep.PolicyP, rep.PolicySP, rep.PolicyPct,
		rep.PolicyADKIM, rep.PolicyASPF, rep.PassCount, rep.FailCount, rep.Recipient,
	)
	if err != nil {
		return 0, fmt.Errorf("insert dmarc report: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("dmarc report id: %w", err)
	}
	for _, rec := range rep.Records {
		if _, err := tx.Exec(`
			INSERT INTO dmarc_report_records (
				report_row_id, source_ip, count, disposition, spf_result, dkim_result, header_from
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, rec.SourceIP, rec.Count, rec.Disposition, rec.SPFResult, rec.DKIMResult, rec.HeaderFrom,
		); err != nil {
			return 0, fmt.Errorf("insert dmarc record: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit dmarc report: %w", err)
	}
	return id, nil
}

// PruneDMARCReports enforces count and age caps.
func (s *Store) PruneDMARCReports() error {
	cutoff := time.Now().UTC().AddDate(0, 0, -DMARCReportsMaxAgeDays).Format(time.RFC3339)
	if _, err := s.db.Exec(`DELETE FROM dmarc_reports WHERE received_at < ?`, cutoff); err != nil {
		return fmt.Errorf("prune dmarc by age: %w", err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM dmarc_reports`).Scan(&count); err != nil {
		return fmt.Errorf("count dmarc reports: %w", err)
	}
	if count <= DMARCReportsMaxKeep {
		return nil
	}
	excess := count - DMARCReportsMaxKeep
	_, err := s.db.Exec(`
		DELETE FROM dmarc_reports WHERE id IN (
			SELECT id FROM dmarc_reports ORDER BY received_at ASC LIMIT ?
		)`, excess)
	if err != nil {
		return fmt.Errorf("prune dmarc by count: %w", err)
	}
	return nil
}

// IncrDMARCParseFailures bumps the failure counter in settings.
func (s *Store) IncrDMARCParseFailures() error {
	raw, err := s.GetSetting(DMARCParseFailuresKey)
	if err != nil {
		return err
	}
	n := 0
	if raw != "" {
		fmt.Sscanf(raw, "%d", &n)
	}
	return s.SetSetting(DMARCParseFailuresKey, fmt.Sprintf("%d", n+1))
}

// ListDMARCReports returns recent summaries, optionally limited to domains.
func (s *Store) ListDMARCReports(domains []string, limit int) ([]DMARCReportSummary, error) {
	if limit <= 0 {
		limit = 50
	}
	var (
		rows *sql.Rows
		err  error
	)
	if len(domains) == 0 {
		rows, err = s.db.Query(`
			SELECT id, domain, reporter, period_begin, period_end, received_at, pass_count, fail_count
			FROM dmarc_reports ORDER BY received_at DESC LIMIT ?`, limit)
	} else {
		placeholders := make([]any, 0, len(domains)+1)
		q := `SELECT id, domain, reporter, period_begin, period_end, received_at, pass_count, fail_count
			FROM dmarc_reports WHERE domain IN (`
		for i, d := range domains {
			if i > 0 {
				q += ","
			}
			q += "?"
			placeholders = append(placeholders, d)
		}
		q += `) ORDER BY received_at DESC LIMIT ?`
		placeholders = append(placeholders, limit)
		rows, err = s.db.Query(q, placeholders...)
	}
	if err != nil {
		return nil, fmt.Errorf("list dmarc reports: %w", err)
	}
	defer rows.Close()
	var out []DMARCReportSummary
	for rows.Next() {
		var (
			summary DMARCReportSummary
			begin   string
			end     string
			recv    string
		)
		if err := rows.Scan(&summary.ID, &summary.Domain, &summary.Reporter, &begin, &end, &recv, &summary.PassCount, &summary.FailCount); err != nil {
			return nil, err
		}
		summary.PeriodBegin, _ = time.Parse(time.RFC3339, begin)
		summary.PeriodEnd, _ = time.Parse(time.RFC3339, end)
		summary.ReceivedAt, _ = time.Parse(time.RFC3339, recv)
		out = append(out, summary)
	}
	return out, rows.Err()
}

// GetDMARCReport loads one report with records.
func (s *Store) GetDMARCReport(id int64) (DMARCReport, error) {
	row := s.db.QueryRow(`
		SELECT id, domain, reporter, report_id, period_begin, period_end, received_at,
		       contact_email, policy_p, policy_sp, policy_pct, policy_adkim, policy_aspf,
		       pass_count, fail_count, recipient
		FROM dmarc_reports WHERE id = ?`, id)
	rep, err := scanDMARCReport(row)
	if errors.Is(err, sql.ErrNoRows) {
		return DMARCReport{}, ErrDMARCReportNotFound
	}
	if err != nil {
		return DMARCReport{}, err
	}
	recs, err := s.listDMARCReportRecords(id)
	if err != nil {
		return DMARCReport{}, err
	}
	rep.Records = recs
	return rep, nil
}

// ListDMARCReportsForDomain returns summaries for one sending domain.
func (s *Store) ListDMARCReportsForDomain(domain string, limit int) ([]DMARCReportSummary, error) {
	return s.ListDMARCReports([]string{domain}, limit)
}

// DMARCDomainRollup summarises pass/fail for a domain over the last windowDays.
func (s *Store) DMARCDomainRollup(domain string, windowDays int) (pass, fail int, err error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -windowDays).Format(time.RFC3339)
	err = s.db.QueryRow(`
		SELECT COALESCE(SUM(pass_count), 0), COALESCE(SUM(fail_count), 0)
		FROM dmarc_reports WHERE domain = ? AND received_at >= ?`,
		domain, cutoff,
	).Scan(&pass, &fail)
	if err != nil {
		return 0, 0, fmt.Errorf("dmarc domain rollup: %w", err)
	}
	return pass, fail, nil
}

// DMARCSourceRollups aggregates per-source rows for a domain over windowDays.
func (s *Store) DMARCSourceRollups(domain string, windowDays int) ([]DMARCSourceRollup, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -windowDays).Format(time.RFC3339)
	rows, err := s.db.Query(`
		SELECT r.source_ip,
		       SUM(CASE WHEN r.spf_result = 'pass' OR r.dkim_result = 'pass' THEN r.count ELSE 0 END),
		       SUM(CASE WHEN r.spf_result != 'pass' AND r.dkim_result != 'pass' THEN r.count ELSE 0 END),
		       MAX(r.disposition)
		FROM dmarc_report_records r
		INNER JOIN dmarc_reports d ON d.id = r.report_row_id
		WHERE d.domain = ? AND d.received_at >= ?
		GROUP BY r.source_ip
		ORDER BY 2 DESC, 3 DESC`, domain, cutoff)
	if err != nil {
		return nil, fmt.Errorf("dmarc source rollups: %w", err)
	}
	defer rows.Close()
	var out []DMARCSourceRollup
	for rows.Next() {
		var rollup DMARCSourceRollup
		if err := rows.Scan(&rollup.SourceIP, &rollup.PassCount, &rollup.FailCount, &rollup.Disposition); err != nil {
			return nil, err
		}
		out = append(out, rollup)
	}
	return out, rows.Err()
}

// DMARCIngestStats returns ingest health for the panel.
func (s *Store) DMARCIngestStats() (DMARCIngestStats, error) {
	var stats DMARCIngestStats
	var last sql.NullString
	err := s.db.QueryRow(`SELECT MAX(received_at) FROM dmarc_reports`).Scan(&last)
	if err != nil {
		return stats, fmt.Errorf("dmarc last received: %w", err)
	}
	if last.Valid && last.String != "" {
		t, _ := time.Parse(time.RFC3339, last.String)
		stats.LastReceivedAt = &t
		stats.IngestOK = time.Since(t) < 8*24*time.Hour
	}
	weekCutoff := time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339)
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM dmarc_reports WHERE received_at >= ?`, weekCutoff).Scan(&stats.KeptThisWeek); err != nil {
		return stats, fmt.Errorf("dmarc week count: %w", err)
	}
	raw, err := s.GetSetting(DMARCParseFailuresKey)
	if err != nil {
		return stats, err
	}
	if raw != "" {
		fmt.Sscanf(raw, "%d", &stats.ParseFailures)
	}
	return stats, nil
}

func (s *Store) listDMARCReportRecords(reportID int64) ([]DMARCReportRecord, error) {
	rows, err := s.db.Query(`
		SELECT source_ip, count, disposition, spf_result, dkim_result, header_from
		FROM dmarc_report_records WHERE report_row_id = ? ORDER BY count DESC`, reportID)
	if err != nil {
		return nil, fmt.Errorf("list dmarc records: %w", err)
	}
	defer rows.Close()
	var out []DMARCReportRecord
	for rows.Next() {
		var rec DMARCReportRecord
		if err := rows.Scan(&rec.SourceIP, &rec.Count, &rec.Disposition, &rec.SPFResult, &rec.DKIMResult, &rec.HeaderFrom); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func scanDMARCReport(r scanRow) (DMARCReport, error) {
	var (
		rep   DMARCReport
		begin string
		end   string
		recv  string
	)
	if err := r.Scan(
		&rep.ID, &rep.Domain, &rep.Reporter, &rep.ReportID, &begin, &end, &recv,
		&rep.ContactEmail, &rep.PolicyP, &rep.PolicySP, &rep.PolicyPct,
		&rep.PolicyADKIM, &rep.PolicyASPF, &rep.PassCount, &rep.FailCount, &rep.Recipient,
	); err != nil {
		return DMARCReport{}, err
	}
	rep.PeriodBegin, _ = time.Parse(time.RFC3339, begin)
	rep.PeriodEnd, _ = time.Parse(time.RFC3339, end)
	rep.ReceivedAt, _ = time.Parse(time.RFC3339, recv)
	return rep, nil
}
