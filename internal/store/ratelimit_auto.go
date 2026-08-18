package store

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"time"
)

// RecalcAllAutoRateLimits recomputes max_messages for every auto-mode limit.
// Store errors for individual rows are logged and skipped (fail-open on the
// last successfully written limit). Returns how many rows were updated.
func (s *Store) RecalcAllAutoRateLimits(retentionDays, l1Max, l1Window int) (int, error) {
	rows, err := s.listAutoRateLimits()
	if err != nil {
		return 0, err
	}
	var updated int
	for _, rl := range rows {
		if err := s.recalcAutoRateLimit(rl, retentionDays, l1Max, l1Window); err != nil {
			log.Printf("store: auto rate-limit recalc %s ref %d: %v", rl.Scope, rl.RefID, err)
			continue
		}
		updated++
	}
	return updated, nil
}

// RecalcAutoRateLimit recomputes one auto-mode limit. Returns an error when
// the row is missing or not in auto mode.
func (s *Store) RecalcAutoRateLimit(scope string, refID int64, retentionDays, l1Max, l1Window int) error {
	rl, ok, err := s.GetRateLimit(scope, refID)
	if err != nil {
		return err
	}
	if !ok || !rl.IsAuto() {
		return fmt.Errorf("rate limit not in auto mode")
	}
	rl.Scope, rl.RefID = scope, refID
	return s.recalcAutoRateLimit(rl, retentionDays, l1Max, l1Window)
}

func (s *Store) listAutoRateLimits() ([]RateLimit, error) {
	rows, err := s.db.Query(
		`SELECT scope, ref_id, allowed_ips, max_messages, window_seconds, mode, auto_multiplier, auto_updated_at
		 FROM rate_limits WHERE mode = ?`, RateLimitModeAuto,
	)
	if err != nil {
		return nil, fmt.Errorf("list auto rate limits: %w", err)
	}
	defer rows.Close()

	var out []RateLimit
	for rows.Next() {
		var scope string
		var refID int64
		var ips sql.NullString
		var maxMsgs, windowSecs sql.NullInt64
		var mode sql.NullString
		var autoMult sql.NullFloat64
		var autoUpdated sql.NullString
		if err := rows.Scan(&scope, &refID, &ips, &maxMsgs, &windowSecs, &mode, &autoMult, &autoUpdated); err != nil {
			return nil, fmt.Errorf("scan auto rate limit: %w", err)
		}
		rl := RateLimit{
			Scope:         scope,
			RefID:         refID,
			AllowedIPs:    splitIPs(ips.String),
			MaxMessages:   int(maxMsgs.Int64),
			WindowSeconds: int(windowSecs.Int64),
			Mode:          mode.String,
		}
		if autoMult.Valid {
			rl.AutoMultiplier = autoMult.Float64
		}
		if autoUpdated.Valid {
			rl.AutoUpdatedAt, _ = time.Parse(time.RFC3339, autoUpdated.String)
		}
		out = append(out, rl)
	}
	return out, rows.Err()
}

func (s *Store) recalcAutoRateLimit(rl RateLimit, retentionDays, l1Max, l1Window int) error {
	if !rl.IsAuto() {
		return fmt.Errorf("not auto mode")
	}
	mult := rl.AutoMultiplier
	if mult <= 0 {
		mult = DefaultAutoMultiplier
	}

	var stats SendStats

	switch rl.Scope {
	case RateLimitScopeDomain:
		d, err := s.GetDomain(rl.RefID)
		if err != nil {
			return err
		}
		stats, err = s.DomainSendStats(d.Name, retentionDays, d.CreatedAt)
		if err != nil {
			return err
		}
	case RateLimitScopeApp:
		a, err := s.GetApplication(rl.RefID)
		if err != nil {
			return err
		}
		stats, err = s.AppSendStats(a.Login, retentionDays, a.CreatedAt)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown scope %q", rl.Scope)
	}

	maxMsgs := computeAutoMaxMessages(stats, mult, l1Max)

	rl.MaxMessages = maxMsgs
	rl.WindowSeconds = l1Window
	rl.AutoUpdatedAt = time.Now().UTC()
	if rl.AutoMultiplier <= 0 {
		rl.AutoMultiplier = mult
	}

	return s.SetRateLimit(rl)
}

func computeAutoMaxMessages(stats SendStats, multiplier float64, l1Max int) int {
	if stats.Total == 0 {
		return 0
	}
	max := int(math.Ceil(stats.AvgPerHour * multiplier))
	if max > l1Max {
		max = l1Max
	}
	return max
}

