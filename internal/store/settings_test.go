package store

import (
	"errors"
	"testing"
)

func TestSendLogRetentionDaysSaveLoad(t *testing.T) {
	st := openTestStore(t)

	if err := st.SetSendLogRetentionDays(45); err != nil {
		t.Fatalf("SetSendLogRetentionDays: %v", err)
	}
	got, err := st.GetSendLogRetentionDays(90)
	if err != nil {
		t.Fatalf("GetSendLogRetentionDays: %v", err)
	}
	if got != 45 {
		t.Fatalf("got %d, want 45", got)
	}
}

func TestSendLogRetentionDaysRejectsOutOfRange(t *testing.T) {
	st := openTestStore(t)

	for _, days := range []int{0, 6, 366, -1} {
		if err := st.SetSendLogRetentionDays(days); err == nil {
			t.Fatalf("SetSendLogRetentionDays(%d) want error", days)
		} else if !errors.Is(err, ErrSendLogRetentionDaysOutOfRange) {
			t.Fatalf("SetSendLogRetentionDays(%d) = %v, want ErrSendLogRetentionDaysOutOfRange", days, err)
		}
	}
}

func TestSendLogRetentionDaysBootstrapFromEnv(t *testing.T) {
	st := openTestStore(t)

	if err := st.EnsureSendLogRetentionDays(120); err != nil {
		t.Fatalf("EnsureSendLogRetentionDays: %v", err)
	}
	got, err := st.GetSendLogRetentionDays(90)
	if err != nil {
		t.Fatalf("GetSendLogRetentionDays: %v", err)
	}
	if got != 120 {
		t.Fatalf("got %d, want 120", got)
	}

	// Second call is a no-op.
	if err := st.EnsureSendLogRetentionDays(30); err != nil {
		t.Fatalf("EnsureSendLogRetentionDays again: %v", err)
	}
	got, err = st.GetSendLogRetentionDays(90)
	if err != nil {
		t.Fatalf("GetSendLogRetentionDays: %v", err)
	}
	if got != 120 {
		t.Fatalf("after second ensure got %d, want 120", got)
	}
}

func TestSendLogRetentionDaysMissingUsesEnvDefault(t *testing.T) {
	st := openTestStore(t)

	got, err := st.GetSendLogRetentionDays(60)
	if err != nil {
		t.Fatalf("GetSendLogRetentionDays: %v", err)
	}
	if got != 60 {
		t.Fatalf("got %d, want 60", got)
	}
}

func TestSendLogRetentionDaysInvalidStoredFallsBack(t *testing.T) {
	st := openTestStore(t)

	if err := st.SetSetting(SendLogRetentionDaysKey, "not-a-number"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	got, err := st.GetSendLogRetentionDays(60)
	if err != nil {
		t.Fatalf("GetSendLogRetentionDays: %v", err)
	}
	if got != 60 {
		t.Fatalf("got %d, want env fallback 60", got)
	}
}
