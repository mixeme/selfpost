package logtail

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mixeme/selfpost/internal/store"
)

type retentionProbeStore struct {
	mu     sync.Mutex
	cutoff time.Time
}

func (s *retentionProbeStore) UpdateStatus(string, string, string) (int64, error) {
	return 0, nil
}

func (s *retentionProbeStore) ListQueuedOlderThan(time.Time) ([]store.QueuedDelivery, error) {
	return nil, nil
}

func (s *retentionProbeStore) DeleteSendLogBefore(cutoff time.Time) (int64, error) {
	s.mu.Lock()
	s.cutoff = cutoff
	s.mu.Unlock()
	return 0, nil
}

func (s *retentionProbeStore) LogtailState(string) (store.LogtailState, bool, error) {
	return store.LogtailState{}, false, nil
}

func (s *retentionProbeStore) SaveLogtailState(string, store.LogtailState) error {
	return nil
}

func (s *retentionProbeStore) lastCutoff() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cutoff
}

// retentionLoop must re-read the configured window every cycle so a panel
// change takes effect without restarting the process.
func TestRetentionLoopUsesUpdatedValue(t *testing.T) {
	old := retentionInterval
	retentionInterval = 20 * time.Millisecond
	t.Cleanup(func() { retentionInterval = old })

	var days atomic.Int32
	days.Store(30)

	st := &retentionProbeStore{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go retentionLoop(ctx, st, func() int { return int(days.Load()) })

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if age := time.Since(st.lastCutoff()); age < 31*24*time.Hour && age > 29*24*time.Hour {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if age := time.Since(st.lastCutoff()); age < 29*24*time.Hour || age > 31*24*time.Hour {
		t.Fatalf("first prune cutoff age %v, want about 30 days", age)
	}

	days.Store(7)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if age := time.Since(st.lastCutoff()); age < 8*24*time.Hour && age > 6*24*time.Hour {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	age := time.Since(st.lastCutoff())
	t.Fatalf("second prune cutoff age %v, want about 7 days", age)
}
