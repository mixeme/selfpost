package main

import (
	"context"
	"log"
	"time"

	"github.com/mixeme/selfpost/internal/logtail"
	"github.com/mixeme/selfpost/internal/store"
)

// runAutoRateLimitRecalc recomputes auto-mode level-2 limits on the same
// interval as send-log retention pruning (6 hours).
func runAutoRateLimitRecalc(ctx context.Context, cfg config, st *store.Store) {
	recalc := func() {
		retention := cfg.retentionDays
		if days, err := st.GetSendLogRetentionDays(cfg.retentionDays); err == nil {
			retention = days
		}
		l1Max := cfg.rateLimitMessagesPerIP
		if l1Max <= 0 {
			l1Max = 100
		}
		l1Window := cfg.rateLimitWindowSeconds
		if l1Window <= 0 {
			l1Window = 3600
		}
		n, err := st.RecalcAllAutoRateLimits(retention, l1Max, l1Window)
		if err != nil {
			log.Printf("auto rate-limit recalc: %v", err)
			return
		}
		if n > 0 {
			log.Printf("auto rate-limit recalc: updated %d limit(s)", n)
		}
	}

	recalc()
	t := time.NewTicker(logtail.RetentionInterval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			recalc()
		}
	}
}
