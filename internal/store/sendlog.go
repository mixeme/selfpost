package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Send-log status values (architecture.md § Persistence). "queued" is written
// by the journal-milter when a message is accepted; the log-tailer advances it
// to one of the final states as Postfix reports delivery per recipient.
const (
	StatusQueued   = "queued"
	StatusSent     = "sent"
	StatusDeferred = "deferred"
	StatusBounced  = "bounced"
	// StatusRejected marks a message the journal-milter refused with a 4xx under
	// a level-2 rate limit (guide § Rate limiting). Such a row never gets a
	// queue-id and is excluded from the level-2 message count (it was never
	// sent).
	StatusRejected = "rejected"
)

// SendLogEntry is a single queued send-log row. The journal-milter creates one
// per (queue-id, recipient) pair at end-of-message (architecture.md §
// Persistence); every field except the status/timestamps comes from the
// accepted message.
type SendLogEntry struct {
	QueueID  string
	Domain   string
	AppLogin string
	From     string
	To       string
	Subject  string
	// Reason and HeaderFrom are set on refused rows only: why the message
	// was refused (Reason*), and for ReasonFrom what its From: line said.
	Reason     string
	HeaderFrom string
	// ClientIP is the client a refusal at MAIL FROM came from (InsertRejected
	// only), which Postfix's line about it names.
	ClientIP string
}

// loginDomainID is the domain_id a row is written with: the domain of the
// application whose login it is, NULL for a login that is no application's
// (audit 2026-10-12, L12). The domain name of the row stays what the
// envelope said.
//
// The login is matched exactly (BINARY): it reaches the milter and the log in
// the case it is stored in, because Cyrus SASL 2.1.28 looks it up as given -
// lib/canonusr.c _canonuser_internal() only trims spaces and appends a realm,
// sasldb/allockey.c _sasl_alloc_key() copies it byte for byte into the key.
const loginDomainID = `(SELECT a.domain_id FROM applications a WHERE a.login = ?)`

// InsertQueued records an accepted message in the send log with status
// "queued". It is called from the journal-milter hot path, so it returns any
// error for the caller to log rather than deciding policy here; the milter
// must stay fail-open regardless (architecture.md § Persistence).
func (s *Store) InsertQueued(e SendLogEntry) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT INTO send_log
			(queue_id, domain, app_login, from_addr, to_addr, subject, status, created_at, updated_at, domain_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, `+loginDomainID+`)`,
		e.QueueID, e.Domain, e.AppLogin, e.From, e.To, e.Subject, StatusQueued, now, now, e.AppLogin,
	)
	if err != nil {
		return fmt.Errorf("insert send_log: %w", err)
	}
	return nil
}

// InsertRejected records a message the journal-milter refused under a level-2
// rate limit (guide § Rate limiting), so the rejection is visible in the
// send-log UI. Only the fields known at MAIL FROM are set (domain, sender, app
// login); there is no queue-id or recipient because the message was rejected
// before it was queued.
func (s *Store) InsertRejected(e SendLogEntry) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT INTO send_log
			(queue_id, domain, app_login, from_addr, to_addr, subject, status, reason, header_from, client_ip, created_at, updated_at, domain_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, `+loginDomainID+`)`,
		e.QueueID, e.Domain, e.AppLogin, e.From, e.To, e.Subject, StatusRejected, e.Reason, e.HeaderFrom, e.ClientIP, now, now, e.AppLogin,
	)
	if err != nil {
		return fmt.Errorf("insert rejected send_log: %w", err)
	}
	return nil
}

// UpdateStatus advances the delivery status of the send-log rows matching a
// (queue-id, recipient) pair, which the log-tailer parses out of mail.log.
// Recipient matching is case-insensitive because Postfix may normalise address
// case between the milter (envelope) and the delivery log. It returns the
// number of rows updated so the caller can tell whether the line matched a
// journal entry.
//
// Only the newest row of the pair is updated. Postfix reuses a short queue id
// once the message has left the queue, and a row outlives that by months; a
// line about today's message must not rewrite an old one (audit 2026-10-12,
// review of stage 1).
//
// A row the reconcile sweep closed (reason "reconcile") that a delivery line
// now speaks of was closed on a guess the log could have settled: the log's
// status wins, the reason becomes "reconcile-contradicted" - which row, and
// updated_at says when - and the day's reconcile_contradicted counts it
// (delivery_log_days), once, since the row no longer reads "reconcile".
//
// The transaction's first statement is a write. A transaction that reads
// first and then writes fails at once with "database is locked" when another
// connection - the dmarc-ingest process opens the same file - commits in
// between, and the line would be lost; a write first takes the write lock,
// and the busy timeout waits for it. SQLite 3.53.4, btree.c btreeBeginTrans():
// the busy handler is retried only while the connection has no transaction
// open, so the upgrade of a read transaction fails with SQLITE_BUSY at once -
// also while the other connection merely holds the write lock - and with
// SQLITE_BUSY_SNAPSHOT after it committed (https://sqlite.org/rescode.html#busy_snapshot).
// Every other transaction of this package opens with a write for this reason.
func (s *Store) UpdateStatus(queueID, recipient, status string) (int64, error) {
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("update send_log status: %w", err)
	}
	defer tx.Rollback()
	// A refused row carries the queue id it was given, and that id may be the
	// next message's: a delivery line is never about a refusal.
	const newest = `id = (SELECT MAX(id) FROM send_log WHERE queue_id = ? AND to_addr = ? COLLATE NOCASE AND status <> 'rejected')`
	marked, err := tx.Exec(
		`UPDATE send_log SET reason = ? WHERE `+newest+` AND reason = ?`,
		ReasonReconcileContradicted, queueID, recipient, ReasonReconcile,
	)
	if err != nil {
		return 0, fmt.Errorf("update send_log status: %w", err)
	}
	contradicted, _ := marked.RowsAffected()
	res, err := tx.Exec(
		`UPDATE send_log SET status = ?, updated_at = ? WHERE `+newest,
		status, now.Format(time.RFC3339), queueID, recipient,
	)
	if err != nil {
		return 0, fmt.Errorf("update send_log status: %w", err)
	}
	if contradicted > 0 {
		if _, err := tx.Exec(
			`INSERT INTO delivery_log_days (day, reconcile_contradicted) VALUES (?, ?)
			 ON CONFLICT (day) DO UPDATE SET reconcile_contradicted = reconcile_contradicted + excluded.reconcile_contradicted`,
			now.Format(dayLayout), contradicted,
		); err != nil {
			return 0, fmt.Errorf("update send_log status: count the contradicted sweep: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("update send_log status: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// CloseUnreported is the reconcile sweep's close of one row, by its id:
// "bounced", marked reason "reconcile" so it is known who decided it
// (architecture.md § Log tailer). Only a row still open - "queued" or
// "deferred" - is closed; a final result the log wrote an instant earlier
// stands (audit 2026-10-12, H3). It returns the number of rows closed, 0 or 1.
func (s *Store) CloseUnreported(id int64) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE send_log SET status = ?, reason = ?, updated_at = ? WHERE id = ? AND status IN (?, ?)`,
		StatusBounced, ReasonReconcile, now, id, StatusQueued, StatusDeferred,
	)
	if err != nil {
		return 0, fmt.Errorf("close unreported send_log row: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ExpireMessage closes the rows of a message Postfix gave up on at the end of
// its queue lifetime: each recipient's newest row with that queue id, if it
// is still open ("queued" or "deferred"), becomes "bounced" - Postfix returns
// the message to its sender. Postfix 3.7.11, src/qmgr/qmgr_active.c,
// qmgr_active_done_25_generic(), logs expiry once per message, "<id>:
// from=<..>, status=expired, returned to sender", with no line per
// recipient: the recipients' last lines say "deferred" (assumptions A-8). It
// returns the number of rows closed.
func (s *Store) ExpireMessage(queueID string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE send_log SET status = ?, updated_at = ?
		 WHERE status IN (?, ?) AND id IN (
		   SELECT MAX(id) FROM send_log WHERE queue_id = ? AND status <> 'rejected'
		   GROUP BY to_addr COLLATE NOCASE)`,
		StatusBounced, now, StatusQueued, StatusDeferred, queueID,
	)
	if err != nil {
		return 0, fmt.Errorf("expire send_log rows of %s: %w", queueID, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// QueuedDelivery is a send-log row still waiting for a delivery result,
// reduced to what the log-tailer's reconcile sweep needs to look it up in the
// Postfix queue and, failing that, to close it (architecture.md § Log tailer).
type QueuedDelivery struct {
	ID      int64
	QueueID string
	To      string
}

// ListOpenOlderThan returns the rows still open - "queued", or "deferred"
// and waiting for the next attempt - that were accepted before cutoff, old
// enough that Postfix should long since have reported a result for them or
// still hold the message. Rows without a queue-id are skipped: the milter
// refused those before Postfix ever saw the message, so the queue has
// nothing to say about them. A deferred row is among them because Postfix
// can drop a deferred message without a line per recipient: at expiry (one
// line per message, ExpireMessage) and by postsuper -d (only "removed").
//
// created_at is stored as RFC3339 UTC, so a lexical comparison against the same
// format is chronologically correct. SQLite 3.53.4 compares TEXT with the
// BINARY collation, memcmp() (https://sqlite.org/datatype3.html § 7), and
// time.RFC3339 of a UTC time is fixed-width, YYYY-MM-DDTHH:MM:SSZ.
func (s *Store) ListOpenOlderThan(cutoff time.Time) ([]QueuedDelivery, error) {
	rows, err := s.db.Query(
		`SELECT id, queue_id, to_addr FROM send_log
		 WHERE status IN (?, ?) AND queue_id <> '' AND created_at < ?
		 ORDER BY id`,
		StatusQueued, StatusDeferred, cutoff.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("list queued send_log rows: %w", err)
	}
	defer rows.Close()

	var out []QueuedDelivery
	for rows.Next() {
		var d QueuedDelivery
		if err := rows.Scan(&d.ID, &d.QueueID, &d.To); err != nil {
			return nil, fmt.Errorf("scan queued send_log row: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SendLogRow is one row as returned to the monitoring UI (architecture.md §
// Persistence): a SendLogEntry plus the fields that only exist once a row has
// been written (id, current status, timestamps).
type SendLogRow struct {
	ID      int64
	QueueID string
	Domain  string
	// DomainID is the domain the row belongs to; 0 when it is nobody's but
	// the global role's (its domain was deleted, or its login was none).
	DomainID int64
	AppLogin string
	From     string
	To       string
	Subject  string
	Status   string
	// Reason and HeaderFrom: see SendLogEntry.
	Reason     string
	HeaderFrom string
	// RefusalLine is Postfix's line about a refusal that has no queue id
	// (migration 0009); "" for every other row, and for one refused before.
	RefusalLine string
	// Recovered is a refusal row the log tailer wrote from mail.log because
	// the panel stopped before it wrote the row itself.
	Recovered bool
	CreatedAt time.Time
	// UpdatedAt is when the status last changed — the log-tailer's report of
	// the delivery attempt. It equals CreatedAt while a row is still queued.
	UpdatedAt time.Time
}

// AttachRefusalLine puts Postfix's line about a refusal at MAIL FROM on the
// row the journal milter wrote for it (audit 2026-10-12, stage 2b). Such a
// refusal has no queue id, and the line names no login, so the row is found
// by what the line says: a refused row of that client, login and sender
// without a line yet - the login is smtpd's sasl_username= on the line, so
// two applications behind one address sending as one sender are told apart
// (assumptions A-21) - written within two minutes of the line's time; the nearest in
// time, the oldest of equals, when there are several - lines are read in the
// order the rows were written, so two refusals of one second take one line
// each in turn (review of stage 2b). A line already on a row - read again
// after a restart - goes on no other. It reports whether a row took the line.
//
// SQLite 3.53.4: the choice and the write are one UPDATE, a write transaction
// from its start (https://sqlite.org/lang_transaction.html § 2.1); julianday()
// reads YYYY-MM-DDTHH:MM:SSZ, the T and the Z included (lang_datefunc.html
// § 2); NOCASE folds ASCII only (datatype3.html § 7.1); ?1 is bound once and
// used twice (lang_expr.html § 4).
func (s *Store) AttachRefusalLine(clientIP, login, from string, at time.Time, line string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE send_log SET refusal_line = ?1 WHERE id = (
			SELECT id FROM send_log
			WHERE status = ?2 AND queue_id = '' AND refusal_line = ''
			  AND reason IN ('', ?3, ?4) AND from_addr = ?5 COLLATE NOCASE AND client_ip = ?6
			  AND app_login = ?10 COLLATE NOCASE
			  AND created_at BETWEEN ?7 AND ?8
			ORDER BY ABS(julianday(created_at) - julianday(?9)), id
			LIMIT 1)
		 AND NOT EXISTS (SELECT 1 FROM send_log WHERE refusal_line = ?1)`,
		line, StatusRejected, ReasonLimit, ReasonClient, from, clientIP,
		at.Add(-refusalLineWindow).UTC().Format(time.RFC3339),
		at.Add(refusalLineWindow).UTC().Format(time.RFC3339),
		at.UTC().Format(time.RFC3339),
		login,
	)
	if err != nil {
		return false, fmt.Errorf("attach refusal line: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RecoverRefusal writes the row of a message the sender check refused for its
// From: line, from Postfix's line about it, when the panel stopped before it
// wrote the row itself (audit 2026-10-12, stage 2b): the check answers 550
// and writes the row through a buffer a moment later. The message had a queue
// id; a refused row with that id from around at means the row was written,
// and nothing is. The domain is the login's, or - when the tailer did not see the login, its
// client= line read before a restart - the envelope's, so the row is still
// its domain's administrator's (review of stage 2b). It reports whether it
// wrote one: the check and the insert are one statement (see
// CreateGlobalUser), and an INSERT ... SELECT of no row reports 0.
func (s *Store) RecoverRefusal(e SendLogEntry, at time.Time) (bool, error) {
	stamp := at.UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO send_log
			(queue_id, domain, app_login, from_addr, to_addr, subject, status, reason, header_from, recovered, created_at, updated_at, domain_id)
		 SELECT ?, ?, ?, ?, ?, '', ?, ?, ?, 1, ?, ?,
			COALESCE(`+loginDomainID+`, (SELECT d.id FROM domains d WHERE d.name = ?))
		 WHERE NOT EXISTS (SELECT 1 FROM send_log
			WHERE queue_id = ? AND status = ? AND created_at BETWEEN ? AND ?)`,
		e.QueueID, e.Domain, e.AppLogin, e.From, e.To, StatusRejected, e.Reason, e.HeaderFrom, stamp, stamp, e.AppLogin, e.Domain,
		e.QueueID, StatusRejected,
		at.Add(-refusalLineWindow).UTC().Format(time.RFC3339), at.Add(refusalLineWindow).UTC().Format(time.RFC3339),
	)
	if err != nil {
		return false, fmt.Errorf("recover refusal %s: %w", e.QueueID, err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// refusalLineWindow is how far apart a refusal's row and its log line may be
// dated: the milter writes the row at MAIL FROM, Postfix the line a moment
// later, and the log's stamps are whole seconds.
const refusalLineWindow = 2 * time.Minute

// ErrSendLogNotFound is returned by GetSendLog for an id that no longer exists.
// Send-log rows are pruned on the retention window, so a bookmarked delivery
// disappearing is expected, not a fault.
var ErrSendLogNotFound = errors.New("send-log entry not found")

// GetSendLog returns a single send-log row by id, for the delivery detail page
// the log links each row to.
func (s *Store) GetSendLog(id int64) (SendLogRow, error) {
	var (
		row                  SendLogRow
		createdAt, updatedAt string
	)
	err := s.db.QueryRow(
		`SELECT id, queue_id, domain, COALESCE(domain_id, 0), app_login, from_addr, to_addr, subject, status, reason, header_from, refusal_line, recovered, created_at, updated_at
		 FROM send_log WHERE id = ?`, id,
	).Scan(&row.ID, &row.QueueID, &row.Domain, &row.DomainID, &row.AppLogin, &row.From, &row.To,
		&row.Subject, &row.Status, &row.Reason, &row.HeaderFrom, &row.RefusalLine, &row.Recovered, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SendLogRow{}, ErrSendLogNotFound
	}
	if err != nil {
		return SendLogRow{}, fmt.Errorf("get send_log %d: %w", id, err)
	}
	row.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	row.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return row, nil
}

// SendLogFilter narrows QuerySendLog/CountSendLog. It carries two kinds of
// narrowing, and they behave in opposite ways on purpose.
//
// Domain and AppLogin are the operator's own filters, chosen in the UI: an
// empty field matches everything.
//
// DomainIDs and AllDomains are the authorization scope, which no query
// parameter may widen. DomainIDs is the exhaustive set of domains the caller
// is entitled to read, by id - a row whose domain was deleted is nobody's
// (audit 2026-10-12, L12) - applied as an IN constraint; AllDomains lifts that
// restriction and is the only way to read the whole journal. A zero-valued
// filter therefore matches *no* rows: a caller that forgets to state a scope
// gets an empty log rather than every tenant's mail, which is the failure mode
// this struct exists to make impossible.
type SendLogFilter struct {
	Domain     string
	AppLogin   string
	DomainIDs  []int64
	AllDomains bool
}

// QuerySendLog returns send-log rows matching filter, newest first, for the
// monitoring screen's server-side pagination (product.md's send-log view).
// The filter's authorization scope is mandatory: see SendLogFilter.
func (s *Store) QuerySendLog(filter SendLogFilter, limit, offset int) ([]SendLogRow, error) {
	where, args := sendLogWhere(filter)
	args = append(args, limit, offset)
	rows, err := s.db.Query(
		`SELECT id, queue_id, domain, COALESCE(domain_id, 0), app_login, from_addr, to_addr, subject, status, reason, header_from, created_at, updated_at
		 FROM send_log`+where+`
		 ORDER BY id DESC
		 LIMIT ? OFFSET ?`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query send_log: %w", err)
	}
	defer rows.Close()

	var out []SendLogRow
	for rows.Next() {
		var (
			row                  SendLogRow
			createdAt, updatedAt string
		)
		if err := rows.Scan(&row.ID, &row.QueueID, &row.Domain, &row.DomainID, &row.AppLogin,
			&row.From, &row.To, &row.Subject, &row.Status, &row.Reason, &row.HeaderFrom, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan send_log row: %w", err)
		}
		row.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		row.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		out = append(out, row)
	}
	return out, rows.Err()
}

// CountSendLog returns how many send-log rows match filter, so the monitoring
// screen can render page numbers/next-prev links.
func (s *Store) CountSendLog(filter SendLogFilter) (int64, error) {
	where, args := sendLogWhere(filter)
	var n int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM send_log`+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count send_log: %w", err)
	}
	return n, nil
}

func sendLogWhere(f SendLogFilter) (string, []any) {
	var clauses []string
	var args []any
	if !f.AllDomains {
		// No scope is not "no restriction": a domain administrator whose last
		// assignment was deleted owns nothing in the journal and must see
		// nothing, and the same clause catches a caller that never set a scope.
		if len(f.DomainIDs) == 0 {
			return " WHERE 1 = 0", nil
		}
		marks := make([]string, len(f.DomainIDs))
		for i, id := range f.DomainIDs {
			marks[i] = "?"
			args = append(args, id)
		}
		clauses = append(clauses, "domain_id IN ("+strings.Join(marks, ", ")+")")
	}
	if f.Domain != "" {
		clauses = append(clauses, "domain = ?")
		args = append(args, f.Domain)
	}
	if f.AppLogin != "" {
		clauses = append(clauses, "app_login = ?")
		args = append(args, f.AppLogin)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// DeleteSendLogBefore removes send-log rows created before cutoff,
// implementing the configurable retention window (architecture.md §
// Persistence, SEND_LOG_RETENTION_DAYS). It returns the number of rows pruned.
// created_at is stored as RFC3339 UTC, so a lexical comparison against the
// same format is chronologically correct (see ListOpenOlderThan).
func (s *Store) DeleteSendLogBefore(cutoff time.Time) (int64, error) {
	res, err := s.db.Exec(
		`DELETE FROM send_log WHERE created_at < ?`,
		cutoff.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("prune send_log: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
