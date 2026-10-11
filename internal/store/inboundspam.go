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
	SpamEnginePostfix  = "postfix"  // Postfix's own policy: unknown recipient, size, rate
	SpamEngineSelfPost = "selfpost" // nobody: relayed with no filter attached
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
	// At is when it happened; zero means now. The log tailer dates a refusal
	// by its mail.log line, which it may read long after.
	At time.Time
}

// InsertInboundSpam records one journal row. It is called from the inbound
// journal milter's hot path and from the log tailer, so it returns any error
// for the caller to log; neither of them may hold up mail over it.
func (s *Store) InsertInboundSpam(e InboundSpamEntry) error {
	at := e.At
	if at.IsZero() {
		at = time.Now()
	}
	_, err := s.db.Exec(
		`INSERT INTO inbound_spam_log
			(inbound_domain, client_ip, from_addr, to_addr, subject, decision, engine, detail, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.InboundDomain, e.ClientIP, e.From, e.To, e.Subject, e.Decision, e.Engine, e.Detail,
		at.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert inbound_spam_log: %w", err)
	}
	return nil
}

// PruneInboundSpam deletes journal rows older than cutoff and then, if more
// than keep rows are left, the oldest of those. It returns how many went.
func (s *Store) PruneInboundSpam(cutoff time.Time, keep int) (int64, error) {
	res, err := s.db.Exec("DELETE FROM inbound_spam_log WHERE created_at < ?", cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("prune inbound_spam_log by age: %w", err)
	}
	n, _ := res.RowsAffected()
	res, err = s.db.Exec(
		`DELETE FROM inbound_spam_log WHERE id <=
			(SELECT id FROM inbound_spam_log ORDER BY id DESC LIMIT 1 OFFSET ?)`, keep)
	if err != nil {
		return n, fmt.Errorf("prune inbound_spam_log by count: %w", err)
	}
	m, _ := res.RowsAffected()
	return n + m, nil
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
