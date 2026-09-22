package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// Rate-limit scopes (spec 7.4). A level-2 limit is attached either to a domain
// (counted across all its applications and IPs) or to a single application.
const (
	RateLimitScopeDomain = "domain"
	RateLimitScopeApp    = "application"
)

// RateLimit is a differentiated level-2 rate limit (spec 7.4): an optional set
// of expected client IPs plus a message ceiling over a sliding window, attached
// to a domain or an application. It is enforced in the journal-milter; level 1
// (Postfix anvil, spec 5) is the IP backstop that always applies even when this
// is absent or the milter is down.
//
// Both the IP binding and the ceiling are optional in the schema, but a limit is
// only enforced when it is Active(): the design deliberately allows an admin to
// leave the IP binding empty for apps that send from changing IPs, in which case
// only level 1 protects them (spec 7.4's caveat).
type RateLimit struct {
	Scope         string
	RefID         int64
	AllowedIPs    []string // canonical client IPs this limit applies to
	MaxMessages   int
	WindowSeconds int
}

// Active reports whether the limit is fully configured and should be enforced.
// A missing IP binding, ceiling or window leaves the differentiated limit inert
// (spec 7.4): the IP binding is what scopes the limit to a known sender.
func (r RateLimit) Active() bool {
	return len(r.AllowedIPs) > 0 && r.MaxMessages > 0 && r.WindowSeconds > 0
}

// AllowsIP reports whether ip is one of the limit's registered client IPs. The
// comparison parses both sides so equivalent textual forms of the same address
// match; a client IP outside the list means the differentiated limit does not
// apply to it (level 1 still does).
func (r RateLimit) AllowsIP(ip string) bool {
	c := net.ParseIP(ip)
	if c == nil {
		return false
	}
	for _, a := range r.AllowedIPs {
		if p := net.ParseIP(a); p != nil && p.Equal(c) {
			return true
		}
	}
	return false
}

// GetRateLimit loads the level-2 limit configured for a domain or application by
// its id, for the panel's edit form. ok is false when none is configured.
func (s *Store) GetRateLimit(scope string, refID int64) (RateLimit, bool, error) {
	row := s.db.QueryRow(
		`SELECT allowed_ips, max_messages, window_seconds
		 FROM rate_limits WHERE scope = ? AND ref_id = ?`, scope, refID)
	rl, err := scanRateLimit(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RateLimit{Scope: scope, RefID: refID}, false, nil
	}
	if err != nil {
		return RateLimit{}, false, fmt.Errorf("get rate limit: %w", err)
	}
	rl.Scope, rl.RefID = scope, refID
	return rl, true, nil
}

// SetRateLimit upserts the level-2 limit for a domain or application. The caller
// (panel) has already validated the IPs and numbers (spec 7.6.2); values are
// stored via bound parameters and read back live by the milter.
func (s *Store) SetRateLimit(rl RateLimit) error {
	_, err := s.db.Exec(
		`INSERT INTO rate_limits (scope, ref_id, allowed_ips, max_messages, window_seconds)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(scope, ref_id) DO UPDATE SET
		   allowed_ips    = excluded.allowed_ips,
		   max_messages   = excluded.max_messages,
		   window_seconds = excluded.window_seconds`,
		rl.Scope, rl.RefID, strings.Join(rl.AllowedIPs, ","), rl.MaxMessages, rl.WindowSeconds,
	)
	if err != nil {
		return fmt.Errorf("set rate limit: %w", err)
	}
	return nil
}

// DeleteRateLimit removes the level-2 limit for a domain or application, so the
// admin can clear it and fall back to level 1 only.
func (s *Store) DeleteRateLimit(scope string, refID int64) error {
	if _, err := s.db.Exec(`DELETE FROM rate_limits WHERE scope = ? AND ref_id = ?`, scope, refID); err != nil {
		return fmt.Errorf("delete rate limit: %w", err)
	}
	return nil
}

// DeleteRateLimitsForDomain removes the domain's own limit and the limits of all
// its applications in one statement. It is called on domain deletion, before the
// application rows are cascade-deleted, to avoid leaving orphan limit rows.
func (s *Store) DeleteRateLimitsForDomain(domainID int64) error {
	_, err := s.db.Exec(
		`DELETE FROM rate_limits
		 WHERE (scope = 'domain' AND ref_id = ?)
		    OR (scope = 'application' AND ref_id IN (SELECT id FROM applications WHERE domain_id = ?))`,
		domainID, domainID,
	)
	if err != nil {
		return fmt.Errorf("delete domain rate limits: %w", err)
	}
	return nil
}

// RateLimit resolves the level-2 limit that applies to a message, keyed by the
// human-readable reference the milter has on the receive path: the sending
// domain name (scope "domain") or the SASL login (scope "application"). ok is
// false when no limit is configured for that reference.
func (s *Store) RateLimit(scope, ref string) (RateLimit, bool, error) {
	var query string
	switch scope {
	case RateLimitScopeDomain:
		query = `SELECT rl.allowed_ips, rl.max_messages, rl.window_seconds
		         FROM rate_limits rl JOIN domains d ON d.id = rl.ref_id
		         WHERE rl.scope = 'domain' AND d.name = ?`
	case RateLimitScopeApp:
		query = `SELECT rl.allowed_ips, rl.max_messages, rl.window_seconds
		         FROM rate_limits rl JOIN applications a ON a.id = rl.ref_id
		         WHERE rl.scope = 'application' AND a.login = ?`
	default:
		return RateLimit{}, false, fmt.Errorf("unknown rate-limit scope %q", scope)
	}
	rl, err := scanRateLimit(s.db.QueryRow(query, ref))
	if errors.Is(err, sql.ErrNoRows) {
		return RateLimit{}, false, nil
	}
	if err != nil {
		return RateLimit{}, false, fmt.Errorf("rate limit for %s %q: %w", scope, ref, err)
	}
	rl.Scope = scope
	return rl, true, nil
}

// CountMessages returns how many distinct messages the reference (a domain name
// or an application login) has queued since t, for the level-2 sliding window
// (spec 7.4). It counts distinct queue-ids — one message with many recipients is
// one message, matching level 1's per-message semantics — and excludes rows that
// were themselves rejected by a limit (they were never sent). It reuses the send
// log the journal already writes (spec 7.4: "переиспользует данные журнала").
func (s *Store) CountMessages(scope, ref string, since time.Time) (int64, error) {
	var column string
	switch scope {
	case RateLimitScopeDomain:
		column = "domain"
	case RateLimitScopeApp:
		column = "app_login"
	default:
		return 0, fmt.Errorf("unknown rate-limit scope %q", scope)
	}
	var n int64
	// created_at is stored as RFC3339 UTC, so a lexical comparison against the
	// same format is chronologically correct (as in DeleteSendLogBefore).
	err := s.db.QueryRow(
		`SELECT COUNT(DISTINCT queue_id) FROM send_log
		 WHERE `+column+` = ? AND status != ? AND created_at >= ?`,
		ref, StatusRejected, since.UTC().Format(time.RFC3339),
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count messages for %s %q: %w", scope, ref, err)
	}
	return n, nil
}

// scanRateLimit reads the three stored columns, tolerating NULL numeric columns
// (an IP-only draft) by leaving the corresponding field zero, which makes the
// limit inert via Active().
func scanRateLimit(r scanRow) (RateLimit, error) {
	var (
		ips        sql.NullString
		maxMsgs    sql.NullInt64
		windowSecs sql.NullInt64
	)
	if err := r.Scan(&ips, &maxMsgs, &windowSecs); err != nil {
		return RateLimit{}, err
	}
	return RateLimit{
		AllowedIPs:    splitIPs(ips.String),
		MaxMessages:   int(maxMsgs.Int64),
		WindowSeconds: int(windowSecs.Int64),
	}, nil
}

// splitIPs parses the comma-separated storage form back into a slice, dropping
// empties so an empty column yields nil (an inactive limit).
func splitIPs(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
