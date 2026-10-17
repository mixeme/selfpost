package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Decisions of the inbound antispam journal. Kept in sync with the CHECK
// constraint of inbound_spam_log.
const (
	SpamDecisionAccept     = "accept"
	SpamDecisionReject     = "reject"
	SpamDecisionTempfail   = "tempfail"
	SpamDecisionQuarantine = "quarantine"
)

// Who made a decision of the inbound antispam journal. Kept in sync with the
// CHECK constraint of inbound_spam_log.
const (
	SpamEngineRspamd   = "rspamd"   // the external filter
	SpamEnginePostfix  = "postfix"  // Postfix's own policy: an unknown recipient, a sender domain that does not exist
	SpamEngineSelfPost = "selfpost" // nobody: relayed with no filter attached
)

// What the upstream made of a message accepted on port 25, on its journal
// rows (migration 0012). Kept in sync with the CHECK constraint of
// inbound_spam_log; "" is a row with no result (refused mail, or written
// before 2.10.0). sent and bounced are final.
const (
	UpstreamQueued   = "queued"   // accepted, not tried yet
	UpstreamHeld     = "held"     // accepted into the hold queue by the filter's verdict, not tried until released
	UpstreamSent     = "sent"     // the upstream took it
	UpstreamDeferred = "deferred" // put off or not reached; Postfix tries again
	UpstreamBounced  = "bounced"  // refused for good, or expired in the queue
)

// InboundSpamEntry is one row of the inbound antispam journal: what happened
// to a message offered on port 25 for one recipient. Metadata only - the
// message itself is relayed, not stored. Subject and Detail may be empty.
type InboundSpamEntry struct {
	InboundDomain string
	ClientIP      string
	From          string
	To            string
	Subject       string
	Decision      string
	Engine        string
	Detail        string
	// QueueID and Upstream are set on mail that was accepted: the queue id
	// the delivery lines will name, and UpstreamQueued (UpstreamHeld for a
	// message the filter had held).
	QueueID  string
	Upstream string
	// At is when it happened; zero means now. The log tailer dates a refusal
	// by its mail.log line, which it may read long after.
	At time.Time
}

// inboundDomainID is the inbound_domain_id a journal row is written with: the
// inbound domain registered under the row's name now (audit 2026-10-12, L12).
const inboundDomainID = `(SELECT d.id FROM inbound_domains d WHERE d.name = ?)`

// InboundSpamLogMaxRows is how many rows the journal keeps whatever their
// age: refusals are written at the rate strangers knock on port 25, and
// accepted mail at the rate they send recipients.
const InboundSpamLogMaxRows = 10000

// inboundCapRows and inboundCapEvery are the cap and how many rows - from
// either writer - are written before the journal is cut back to it, without
// waiting for the six-hourly prune. Postfix's one limit on the rate of RCPT
// TO, smtpd_client_recipient_rate_limit, is per client address and off by
// default (postconf(5), Postfix 3.10.13) and not set here. Until 2.14.4 only
// the log tailer counted its rows; the journal milter's - one per accepted
// recipient - were cut only every six hours (security audit 2026-10-17, M3).
// Variables so that a test can make them small.
var (
	inboundCapRows  = InboundSpamLogMaxRows
	inboundCapEvery = int64(500)
)

// countInboundWrites adds n written rows to the count and, once it reaches
// inboundCapEvery, cuts the journal to its cap. Two writers crossing the mark
// together may both cut; the second finds nothing to do.
func (s *Store) countInboundWrites(n int) error {
	if n <= 0 || s.inboundWrites.Add(int64(n)) < inboundCapEvery {
		return nil
	}
	s.inboundWrites.Store(0)
	// The zero time removes nothing by age: this is the cap alone.
	if _, err := s.PruneInboundSpam(time.Time{}, inboundCapRows); err != nil {
		return fmt.Errorf("the rows were written; cutting inbound_spam_log to %d rows: %w", inboundCapRows, err)
	}
	return nil
}

// InboundRecipient is one recipient of an accepted message at an inbound
// domain: a row of InsertInboundSpamMessage.
type InboundRecipient struct {
	Domain string
	To     string
}

// InsertInboundSpamMessage records the rows of one accepted message, one per
// recipient, e with InboundDomain and To taken from each, in one transaction:
// the journal milter writes inside Postfix's end-of-message callback on the
// panel's one connection, and a thousand recipients are one commit, not a
// thousand. It returns any error for the caller to log.
func (s *Store) InsertInboundSpamMessage(e InboundSpamEntry, rcpts []InboundRecipient) error {
	if len(rcpts) == 0 {
		return nil
	}
	at := e.At
	if at.IsZero() {
		at = time.Now()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("insert inbound_spam_log: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // a no-op after Commit
	for _, r := range rcpts {
		if _, err := tx.Exec(
			`INSERT INTO inbound_spam_log
				(inbound_domain, client_ip, from_addr, to_addr, subject, decision, engine, detail, created_at, inbound_domain_id,
				 queue_id, upstream_status)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, `+inboundDomainID+`, ?, ?)`,
			r.Domain, e.ClientIP, e.From, r.To, e.Subject, e.Decision, e.Engine, e.Detail,
			at.UTC().Format(time.RFC3339), r.Domain, e.QueueID, e.Upstream,
		); err != nil {
			return fmt.Errorf("insert inbound_spam_log: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("insert inbound_spam_log: commit: %w", err)
	}
	return s.countInboundWrites(len(rcpts))
}

// InsertInboundSpam records one journal row. It is called from the log
// tailer (and was from the journal milter, which now writes a message's rows
// at once), so it returns any error for the caller to log; it may not hold up
// mail over it.
func (s *Store) InsertInboundSpam(e InboundSpamEntry) error {
	at := e.At
	if at.IsZero() {
		at = time.Now()
	}
	_, err := s.db.Exec(
		`INSERT INTO inbound_spam_log
			(inbound_domain, client_ip, from_addr, to_addr, subject, decision, engine, detail, created_at, inbound_domain_id,
			 queue_id, upstream_status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, `+inboundDomainID+`, ?, ?)`,
		e.InboundDomain, e.ClientIP, e.From, e.To, e.Subject, e.Decision, e.Engine, e.Detail,
		at.UTC().Format(time.RFC3339), e.InboundDomain, e.QueueID, e.Upstream,
	)
	if err != nil {
		return fmt.Errorf("insert inbound_spam_log: %w", err)
	}
	return s.countInboundWrites(1)
}

// InsertInboundSpamOnce records a journal row unless the same row is there
// already - same moment, client, sender, recipient, decision, engine and
// detail - and says whether it wrote. The log tailer uses it for the lines it
// reads when it starts: after an unclean stop those are lines it may have read
// before, and a refusal read twice is otherwise two rows. e.At must be set; a
// row dated "now" is never the same as another. The check and the insert are
// one statement, and one of no row reports 0 (see CreateGlobalUser).
func (s *Store) InsertInboundSpamOnce(e InboundSpamEntry) (bool, error) {
	at := e.At.UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT INTO inbound_spam_log
			(inbound_domain, client_ip, from_addr, to_addr, subject, decision, engine, detail, created_at, inbound_domain_id)
		 SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, `+inboundDomainID+`
		 WHERE NOT EXISTS (
			SELECT 1 FROM inbound_spam_log
			WHERE created_at = ? AND client_ip = ? AND from_addr = ? AND to_addr = ?
			  AND decision = ? AND engine = ? AND detail = ?)`,
		e.InboundDomain, e.ClientIP, e.From, e.To, e.Subject, e.Decision, e.Engine, e.Detail, at, e.InboundDomain,
		at, e.ClientIP, e.From, e.To, e.Decision, e.Engine, e.Detail,
	)
	if err != nil {
		return false, fmt.Errorf("insert inbound_spam_log once: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil
	}
	return true, s.countInboundWrites(1)
}

// updateUpstreamQuery puts one delivery attempt's result on the rows of a
// queue id and recipient whose result is not final; expireUpstreamQuery makes
// those of a queue id bounced, saying it expired after the last reply. Both
// read the queue id through idx_inbound_spam_log_queue_id
// (TestInboundUpstreamUsesItsIndex).
const (
	updateUpstreamQuery = `UPDATE inbound_spam_log
		SET upstream_status = ?, upstream_relay = ?, upstream_reply = ?, upstream_at = ?
		WHERE queue_id = ? AND queue_id <> '' AND to_addr = ? COLLATE NOCASE
		  AND upstream_status IN ('queued', 'held', 'deferred')`
	expireUpstreamQuery = `UPDATE inbound_spam_log
		SET upstream_status = 'bounced', upstream_at = ?,
		    upstream_reply = substr(CASE WHEN upstream_reply = '' THEN 'expired in the queue'
		                                 ELSE 'expired in the queue; last reply: ' || upstream_reply END, 1, ?)
		WHERE queue_id = ? AND queue_id <> '' AND upstream_status IN ('queued', 'held', 'deferred')`
	forgetQueueIDQuery = `UPDATE inbound_spam_log
		SET queue_id = '',
		    upstream_status = CASE WHEN upstream_status IN ('queued', 'held', 'deferred') THEN '' ELSE upstream_status END
		WHERE queue_id = ? AND queue_id <> ''`
)

// ForgetInboundQueueID detaches the journal rows of a queue id from it: those
// still open lose their result too, as nothing will ever report it. The
// milter calls it before it writes a new message's rows - a short queue id
// repeats (microseconds and the queue file's inode, src/global/mail_queue.h;
// the image has made long, non-repeating ones since 2.10.1, but a queue file
// from before keeps its short name), and only one live queue file has an id,
// so older rows of it are of a message that is gone and must not take the
// new one's results. The tailer calls it when cleanup removes a
// queue file it did not queue ("removed (canceled)"): the milter had written
// the rows, but the message was never queued.
func (s *Store) ForgetInboundQueueID(queueID string) error {
	if _, err := s.db.Exec(forgetQueueIDQuery, queueID); err != nil {
		return fmt.Errorf("forget inbound queue id %s: %w", queueID, err)
	}
	return nil
}

// UpstreamReplyMax is how much of an upstream's reply a row keeps.
const UpstreamReplyMax = 255

// UpdateInboundUpstream puts the result of one attempt to hand a message to
// its upstream on its journal rows - those of queueID and recipient (case
// folded, as Postfix logs the address it was given) - unless they hold a
// final one already: a line read twice after a restart, or an older one read
// after a newer, does not undo a sent. relay is the host that answered, ""
// when there was no session; reply is cut to UpstreamReplyMax by the caller.
// It returns how many rows took it: 0 for mail that is not port 25's. A
// message the filter had held takes its results once it is released.
func (s *Store) UpdateInboundUpstream(queueID, recipient, status, relay, reply string, at time.Time) (int64, error) {
	res, err := s.db.Exec(updateUpstreamQuery, status, relay, reply, at.UTC().Format(time.RFC3339), queueID, recipient)
	if err != nil {
		return 0, fmt.Errorf("update inbound upstream %s: %w", queueID, err)
	}
	return res.RowsAffected()
}

// ExpireInboundUpstream makes the rows of a message Postfix gave up on
// bounced (qmgr's "status=expired, returned to sender", which names no
// recipient): the sender was sent a bounce. The last reply stays, after a
// note that the message expired.
func (s *Store) ExpireInboundUpstream(queueID string, at time.Time) (int64, error) {
	res, err := s.db.Exec(expireUpstreamQuery, at.UTC().Format(time.RFC3339), UpstreamReplyMax, queueID)
	if err != nil {
		return 0, fmt.Errorf("expire inbound upstream %s: %w", queueID, err)
	}
	return res.RowsAffected()
}

// PruneInboundSpam deletes journal rows older than cutoff and then, if more
// than keep rows are left, the oldest of those. It returns how many went.
func (s *Store) PruneInboundSpam(cutoff time.Time, keep int) (int64, error) {
	res, err := s.db.Exec("DELETE FROM inbound_spam_log WHERE created_at < ?", cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("prune inbound_spam_log by age: %w", err)
	}
	n, _ := res.RowsAffected()
	// Oldest by when it happened, not by id: the tailer dates a refusal by its
	// log line, so after the panel was down the newest ids are old rows.
	// SQLite 3.53.4, https://sqlite.org/lang_select.html § 5: a negative LIMIT
	// is no upper bound, so this is every row after the newest keep.
	res, err = s.db.Exec(
		`DELETE FROM inbound_spam_log WHERE id IN
			(SELECT id FROM inbound_spam_log ORDER BY created_at DESC, id DESC LIMIT -1 OFFSET ?)`, keep)
	if err != nil {
		return n, fmt.Errorf("prune inbound_spam_log by count: %w", err)
	}
	m, _ := res.RowsAffected()
	return n + m, nil
}

// InboundSpamRow is one row of the inbound antispam journal as it is read
// back for the panel: the entry as it was written, with its id and when it
// happened.
type InboundSpamRow struct {
	ID            int64
	InboundDomain string
	ClientIP      string
	From          string
	To            string
	Subject       string
	Decision      string
	Engine        string
	Detail        string
	CreatedAt     time.Time
	// The upstream's answer: its status ("" for none), the host that gave
	// it, the reply and when.
	Upstream      string
	UpstreamRelay string
	UpstreamReply string
	UpstreamAt    time.Time
}

// InboundSpamFilter narrows QueryInboundSpam and CountInboundSpam. Like
// SendLogFilter it carries two kinds of narrowing that behave in opposite ways
// on purpose.
//
// Domain, Decision, Since and Until are the operator's own filters, chosen in
// the UI: an empty field matches everything. Since and Until are days, both
// inclusive and read as days of the container's zone whatever the time of
// day they carry.
//
// DomainIDs is the authorization scope, which no query parameter may widen:
// the exhaustive set of inbound domains the caller may read, by id - a row of
// a domain deleted since is nobody's (audit 2026-10-12, L12) - applied as an
// IN constraint. nil lifts it (the global role reads the whole journal, including
// rows of a domain deleted since); an empty set is a caller who reaches
// nothing and gets nothing. Unlike SendLogFilter, forgetting the scope is
// therefore the permissive case - the caller states "all" by leaving it out -
// so every caller that serves someone other than the global role must set it.
type InboundSpamFilter struct {
	Domain   string
	Decision string
	Since    time.Time
	Until    time.Time
	// DomainIDs: the inbound domains the caller may read, by id; nil reads
	// everything (the global role).
	DomainIDs []int64
}

// QueryInboundSpam returns journal rows matching filter, newest first, for
// the Inbound log's server-side pagination. A refusal is dated by the mail.log
// line the tailer read it from, which can be older than rows written since, so
// the order is by time and not by id.
func (s *Store) QueryInboundSpam(filter InboundSpamFilter, limit, offset int) ([]InboundSpamRow, error) {
	where, args := inboundSpamWhere(filter)
	args = append(args, limit, offset)
	rows, err := s.db.Query(
		`SELECT id, inbound_domain, client_ip, from_addr, to_addr, subject, decision, engine, detail, created_at,
		        upstream_status, upstream_relay, upstream_reply, upstream_at
		 FROM inbound_spam_log`+where+`
		 ORDER BY created_at DESC, id DESC
		 LIMIT ? OFFSET ?`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query inbound_spam_log: %w", err)
	}
	defer rows.Close()

	var out []InboundSpamRow
	for rows.Next() {
		var (
			row          InboundSpamRow
			created, upd string
		)
		if err := rows.Scan(&row.ID, &row.InboundDomain, &row.ClientIP, &row.From, &row.To,
			&row.Subject, &row.Decision, &row.Engine, &row.Detail, &created,
			&row.Upstream, &row.UpstreamRelay, &row.UpstreamReply, &upd); err != nil {
			return nil, fmt.Errorf("scan inbound_spam_log row: %w", err)
		}
		row.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if upd != "" {
			row.UpstreamAt, _ = time.Parse(time.RFC3339, upd)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// CountInboundSpam returns how many journal rows match filter, for the page
// numbers under the Inbound log.
func (s *Store) CountInboundSpam(filter InboundSpamFilter) (int64, error) {
	where, args := inboundSpamWhere(filter)
	var n int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM inbound_spam_log`+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count inbound_spam_log: %w", err)
	}
	return n, nil
}

func inboundSpamWhere(f InboundSpamFilter) (string, []any) {
	var clauses []string
	var args []any
	if f.DomainIDs != nil {
		if len(f.DomainIDs) == 0 {
			return " WHERE 1 = 0", nil
		}
		marks := make([]string, len(f.DomainIDs))
		for i, id := range f.DomainIDs {
			marks[i] = "?"
			args = append(args, id)
		}
		clauses = append(clauses, "inbound_domain_id IN ("+strings.Join(marks, ", ")+")")
	}
	if f.Domain != "" {
		clauses = append(clauses, "inbound_domain = ?")
		args = append(args, f.Domain)
	}
	if f.Decision != "" {
		clauses = append(clauses, "decision = ?")
		args = append(args, f.Decision)
	}
	// created_at is RFC3339 UTC, so comparing it as text against the same
	// format is comparing moments: from the container zone's midnight of the
	// first day up to, not including, its midnight after the last.
	if !f.Since.IsZero() {
		y, m, d := f.Since.Date()
		clauses = append(clauses, "created_at >= ?")
		args = append(args, time.Date(y, m, d, 0, 0, 0, 0, time.Local).UTC().Format(time.RFC3339))
	}
	if !f.Until.IsZero() {
		y, m, d := f.Until.Date()
		clauses = append(clauses, "created_at < ?")
		args = append(args, time.Date(y, m, d+1, 0, 0, 0, 0, time.Local).UTC().Format(time.RFC3339))
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// IsInboundDomain reports whether name is a configured inbound domain. Port 25
// also takes mail that is not the relay's - DMARC reports for the server's own
// name - and the journal keeps rows for inbound domains only.
func (s *Store) IsInboundDomain(name string) (bool, error) {
	var one int
	err := s.db.QueryRow("SELECT 1 FROM inbound_domains WHERE name = ?", strings.ToLower(name)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("look up inbound domain: %w", err)
	}
	return true, nil
}

// Lists and entry kinds of the filter lists. Kept in sync with the CHECK
// constraints of inbound_spam_list.
const (
	SpamListAllow = "allow"
	SpamListDeny  = "deny"

	SpamEntryAddress = "address" // sender address
	SpamEntryDomain  = "domain"  // sender domain
	SpamEntryIP      = "ip"      // client address or network
)

// ErrSpamListEntryExists is returned when the entry is already on that list.
var ErrSpamListEntryExists = errors.New("filter list entry already exists")

// ErrSpamListEntryNotFound is returned when a filter list entry id does not exist.
var ErrSpamListEntryNotFound = errors.New("filter list entry not found")

// SpamListEntry is one allow or deny entry for the external filter. Value is
// stored normalised by the caller (internal/inbound), which is what makes the
// uniqueness of an entry on its list mean anything.
type SpamListEntry struct {
	ID        int64
	List      string
	Kind      string
	Value     string
	Note      string
	CreatedAt time.Time
}

// AddSpamListEntry inserts an entry. The caller must have validated it.
func (s *Store) AddSpamListEntry(list, kind, value, note string) (SpamListEntry, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		"INSERT INTO inbound_spam_list (list_type, entry_type, value, note, created_at) VALUES (?, ?, ?, ?, ?)",
		list, kind, value, note, now.Format(time.RFC3339),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return SpamListEntry{}, ErrSpamListEntryExists
		}
		return SpamListEntry{}, fmt.Errorf("insert inbound_spam_list: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return SpamListEntry{}, fmt.Errorf("filter list entry id: %w", err)
	}
	return SpamListEntry{ID: id, List: list, Kind: kind, Value: value, Note: note, CreatedAt: now.Truncate(time.Second)}, nil
}

// ListSpamListEntries returns every entry of both lists: by list, then kind,
// then value.
func (s *Store) ListSpamListEntries() ([]SpamListEntry, error) {
	rows, err := s.db.Query(
		"SELECT id, list_type, entry_type, value, note, created_at FROM inbound_spam_list ORDER BY list_type, entry_type, value")
	if err != nil {
		return nil, fmt.Errorf("list inbound_spam_list: %w", err)
	}
	defer rows.Close()
	var out []SpamListEntry
	for rows.Next() {
		var e SpamListEntry
		var created string
		if err := rows.Scan(&e.ID, &e.List, &e.Kind, &e.Value, &e.Note, &created); err != nil {
			return nil, fmt.Errorf("scan inbound_spam_list: %w", err)
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteSpamListEntry removes an entry by id.
func (s *Store) DeleteSpamListEntry(id int64) error {
	res, err := s.db.Exec("DELETE FROM inbound_spam_list WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete inbound_spam_list: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSpamListEntryNotFound
	}
	return nil
}

// SpamListHit is what the journal holds about one message a filter list
// decided: who it was from, which server offered it, when, and the detail that
// says which list it was.
type SpamListHit struct {
	ClientIP  string
	From      string
	Decision  string
	Detail    string
	CreatedAt time.Time
}

// ListSpamListHits returns the journal rows of the filter in whose detail one
// of the marks occurs - the marks the filter lists leave (internal/inbound).
// Which entry a row belongs to is for the caller to work out. No index covers
// detail or engine, so the query goes through the whole journal row by row and
// returns the rspamd rows whose detail holds a mark.
func (s *Store) ListSpamListHits(marks ...string) ([]SpamListHit, error) {
	if len(marks) == 0 {
		return nil, nil
	}
	likes := make([]string, len(marks))
	args := []any{SpamEngineRspamd}
	for i, m := range marks {
		likes[i] = "instr(detail, ?) > 0"
		args = append(args, m)
	}
	rows, err := s.db.Query(
		`SELECT client_ip, from_addr, decision, detail, created_at FROM inbound_spam_log
		 WHERE engine = ? AND (`+strings.Join(likes, " OR ")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("query filter list hits: %w", err)
	}
	defer rows.Close()
	var out []SpamListHit
	for rows.Next() {
		var h SpamListHit
		var created string
		if err := rows.Scan(&h.ClientIP, &h.From, &h.Decision, &h.Detail, &created); err != nil {
			return nil, fmt.Errorf("scan filter list hit: %w", err)
		}
		h.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, h)
	}
	return out, rows.Err()
}
