package store

import (
	"testing"
	"time"
)

func TestSendStatsTotalPeakAvg(t *testing.T) {
	st := openTestStore(t)
	d, err := st.AddDomain("example.com", "selfpost")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	a, err := st.AddApplication(d.ID, "app1", AddressModeWildcard, nil)
	if err != nil {
		t.Fatalf("AddApplication: %v", err)
	}

	now := time.Now().UTC()
	hour := now.Format("2006-01-02T15")
	prevHour := now.Add(-2 * time.Hour).Format("2006-01-02T15")

	// Hour 1: two messages (Q1 two recipients + Q2).
	for _, to := range []string{"a@x.net", "b@x.net"} {
		if err := st.InsertQueued(SendLogEntry{QueueID: "Q1", Domain: "example.com", AppLogin: "app1", To: to}); err != nil {
			t.Fatalf("InsertQueued: %v", err)
		}
	}
	if err := st.InsertQueued(SendLogEntry{QueueID: "Q2", Domain: "example.com", AppLogin: "app1", To: "c@x.net"}); err != nil {
		t.Fatalf("InsertQueued: %v", err)
	}
	// Hour 2: one message.
	if _, err := st.db.Exec(`UPDATE send_log SET created_at = ? WHERE queue_id = 'Q2'`,
		prevHour+":00:00Z"); err != nil {
		t.Fatalf("backdate Q2: %v", err)
	}
	if _, err := st.db.Exec(`UPDATE send_log SET created_at = ? WHERE queue_id = 'Q1'`,
		hour+":10:00Z"); err != nil {
		t.Fatalf("backdate Q1: %v", err)
	}
	if err := st.InsertRejected(SendLogEntry{Domain: "example.com", AppLogin: "app1"}); err != nil {
		t.Fatalf("InsertRejected: %v", err)
	}

	stats, err := st.DomainSendStats("example.com", 90, d.CreatedAt)
	if err != nil {
		t.Fatalf("DomainSendStats: %v", err)
	}
	if stats.Total != 2 {
		t.Fatalf("total = %d, want 2", stats.Total)
	}
	if stats.PeakPerHour != 1 {
		t.Fatalf("peak = %d, want 1 (one message per hour bucket)", stats.PeakPerHour)
	}
	if stats.AvgPerHour <= 0 {
		t.Fatalf("avg should be positive, got %v", stats.AvgPerHour)
	}

	appStats, err := st.AppSendStats("app1", 90, a.CreatedAt)
	if err != nil {
		t.Fatalf("AppSendStats: %v", err)
	}
	if appStats.Total != 2 {
		t.Fatalf("app total = %d, want 2", appStats.Total)
	}
}

func TestStatsWindowShortRetention(t *testing.T) {
	st := openTestStore(t)
	d, _ := st.AddDomain("example.com", "selfpost")
	stats, err := st.DomainSendStats("example.com", 7, d.CreatedAt)
	if err != nil {
		t.Fatalf("DomainSendStats: %v", err)
	}
	if stats.WindowDays != 7 {
		t.Fatalf("window days = %d, want 7", stats.WindowDays)
	}
}
