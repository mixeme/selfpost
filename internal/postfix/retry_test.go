package postfix

import (
	"errors"
	"testing"
	"time"
)

func TestLoadRetryPolicyUsesPostconfValues(t *testing.T) {
	old := readRetryConf
	readRetryConf = func() ([]string, error) {
		return []string{"300s", "300s", "4000s", "2d", "5d", "0"}, nil
	}
	t.Cleanup(func() { readRetryConf = old })

	p := LoadRetryPolicy()
	if p.FromDefaults {
		t.Fatal("FromDefaults = true, want live postconf values")
	}
	if p.MinimalBackoff != 300*time.Second {
		t.Errorf("MinimalBackoff = %v, want 300s", p.MinimalBackoff)
	}
	if p.MaximalBackoff != 4000*time.Second {
		t.Errorf("MaximalBackoff = %v, want 4000s", p.MaximalBackoff)
	}
	if p.MaximalQueueLifetime != 2*24*time.Hour {
		t.Errorf("MaximalQueueLifetime = %v, want 2d", p.MaximalQueueLifetime)
	}
	if p.BounceQueueLifetime != 5*24*time.Hour {
		t.Errorf("BounceQueueLifetime = %v, want 5d", p.BounceQueueLifetime)
	}
	if p.DelayWarningTime != 0 {
		t.Errorf("DelayWarningTime = %v, want 0", p.DelayWarningTime)
	}
}

func TestLoadRetryPolicyFallsBackWhenPostconfFails(t *testing.T) {
	old := readRetryConf
	readRetryConf = func() ([]string, error) {
		return nil, errors.New("exec: not found")
	}
	t.Cleanup(func() { readRetryConf = old })

	p := LoadRetryPolicy()
	want := DefaultRetryPolicy()
	if !p.FromDefaults {
		t.Error("FromDefaults = false, want true when postconf fails")
	}
	if p.QueueRunDelay != want.QueueRunDelay || p.MinimalBackoff != want.MinimalBackoff ||
		p.MaximalBackoff != want.MaximalBackoff || p.MaximalQueueLifetime != want.MaximalQueueLifetime {
		t.Errorf("fallback = %+v, want compiled-in defaults %+v", p, want)
	}
}

func TestLoadRetryPolicyFallsBackOnUnparseableValues(t *testing.T) {
	old := readRetryConf
	readRetryConf = func() ([]string, error) {
		return []string{"300s", "nope", "4000s", "5d", "5d", "0"}, nil
	}
	t.Cleanup(func() { readRetryConf = old })

	p := LoadRetryPolicy()
	if !p.FromDefaults {
		t.Error("FromDefaults = false, want true when a value cannot be parsed")
	}
}

func TestParseRetryPolicyStock(t *testing.T) {
	p, err := parseRetryPolicy([]string{"300s", "300s", "4000s", "5d", "5d", "0"})
	if err != nil {
		t.Fatalf("parseRetryPolicy: %v", err)
	}
	if p.FromDefaults {
		t.Error("parsed policy should not be marked FromDefaults")
	}
	if p.QueueRunDelay != 300*time.Second || p.MaximalQueueLifetime != 5*24*time.Hour {
		t.Errorf("parsed = %+v", p)
	}
}
