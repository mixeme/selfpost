package postfix

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

// The postconf values in these tests are composed by hand: values main.cf
// could hold. Stock Postfix 3.10.13 prints "0h" for delay_warning_time
// (src/global/mail_params.h, DEF_DELAY_WARN_TIME), not "0".
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
	// 5d over a maximal 2d: qmgr uses 2d (Postfix 3.10.13, src/qmgr/qmgr.c,
	// qmgr_post_init(), "adjusting bounce_queue_lifetime").
	if p.BounceQueueLifetime != 2*24*time.Hour {
		t.Errorf("BounceQueueLifetime = %v, want 2d", p.BounceQueueLifetime)
	}
	if p.DelayWarningTime != 0 {
		t.Errorf("DelayWarningTime = %v, want 0", p.DelayWarningTime)
	}
}

// A message lives at most maximal_queue_lifetime: qmgr caps
// bounce_queue_lifetime at it (Postfix 3.10.13, src/qmgr/qmgr.c,
// qmgr_post_init()), so a larger bounce lifetime does not lengthen it.
func TestLongestLifetimeIsTheQueueLifetime(t *testing.T) {
	p, err := parseRetryPolicy([]string{"300s", "300s", "4000s", "1d", "5d", "0h"})
	if err != nil {
		t.Fatal(err)
	}
	if p.LongestLifetime() != 24*time.Hour || p.BounceQueueLifetime != 24*time.Hour {
		t.Fatalf("LongestLifetime %v, BounceQueueLifetime %v; want 24h both", p.LongestLifetime(), p.BounceQueueLifetime)
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

// TestFakePostconf is not a test: it is the fake `postconf -h -x` the test
// below runs in place of the real one. Its warning is composed after Postfix
// 3.10.13, src/postconf/postconf_unused.c, pcf_flag_unused_parameters(),
// written to stderr after the values (src/postconf/postconf.c, main()).
func TestFakePostconf(t *testing.T) {
	if os.Getenv("SP_FAKE_POSTCONF") != "1" {
		t.Skip("helper process")
	}
	fmt.Fprint(os.Stdout, "300s\n300s\n4000s\n2d\n5d\n0h\n")
	fmt.Fprint(os.Stderr, "postconf: warning: /etc/postfix/main.cf: support for parameter \"smtpd_use_tls\" will be removed; instead, specify \"smtpd_tls_security_level\"\n")
	os.Exit(0)
}

// postconf's warnings are not values: since Postfix 3.9 a deprecated
// main.cf parameter draws one on every run, and read with the values it made
// one line too many, so the panel showed the compiled-in defaults.
func TestPostconfRetryValuesReadsStdoutOnly(t *testing.T) {
	old := postconfRetryCmd
	postconfRetryCmd = func() *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestFakePostconf$")
		cmd.Env = append(os.Environ(), "SP_FAKE_POSTCONF=1")
		return cmd
	}
	t.Cleanup(func() { postconfRetryCmd = old })

	lines, err := postconfRetryValues()
	if err != nil {
		t.Fatalf("postconfRetryValues: %v", err)
	}
	p, err := parseRetryPolicy(lines)
	if err != nil {
		t.Fatalf("parse %q: %v", lines, err)
	}
	if p.MaximalQueueLifetime != 2*24*time.Hour || p.FromDefaults {
		t.Errorf("policy = %+v, want the postconf values", p)
	}
}
