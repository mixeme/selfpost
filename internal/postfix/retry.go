package postfix

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// RetryPolicy is this Postfix's deferred-mail retry timings, as postconf
// reports them. The panel loads it once at HTTP start (architecture.md) and
// never re-reads it for a request.
type RetryPolicy struct {
	QueueRunDelay        time.Duration
	MinimalBackoff       time.Duration
	MaximalBackoff       time.Duration
	MaximalQueueLifetime time.Duration
	BounceQueueLifetime  time.Duration
	DelayWarningTime     time.Duration
	// FromDefaults is true when postconf could not be read and the compiled-in
	// Postfix 3.x values were substituted. The Mail queue card shows a muted
	// note in that case so an operator who overrode the parameters is not
	// silently shown the stock numbers.
	FromDefaults bool
}

// DefaultRetryPolicy is Postfix 3.x compiled-in values for the six parameters
// build/postfix-config.sh does not set. Used when postconf is missing (unit
// tests, a binary outside the container) so the panel still starts.
//
// Postfix 3.7.11, src/global/mail_params.h: DEF_QUEUE_RUN_DELAY "300s",
// DEF_MIN_BACKOFF_TIME DEF_QUEUE_RUN_DELAY, DEF_MAX_BACKOFF_TIME "4000s",
// DEF_MAX_QUEUE_TIME "5d", DEF_DSN_QUEUE_TIME "5d", DEF_DELAY_WARN_TIME "0h".
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		QueueRunDelay:        300 * time.Second,
		MinimalBackoff:       300 * time.Second,
		MaximalBackoff:       4000 * time.Second,
		MaximalQueueLifetime: 5 * 24 * time.Hour,
		BounceQueueLifetime:  5 * 24 * time.Hour,
		DelayWarningTime:     0,
		FromDefaults:         true,
	}
}

// retryConfKeys is the fixed argv tail for `postconf -h`. Order matches the
// fields of RetryPolicy. No user input is interpolated (security.md).
var retryConfKeys = []string{
	"queue_run_delay",
	"minimal_backoff_time",
	"maximal_backoff_time",
	"maximal_queue_lifetime",
	"bounce_queue_lifetime",
	"delay_warning_time",
}

// retryConfUnits is each key's default unit: the last letter of its default
// value (Postfix 3.7.11, src/global/mail_params.h, DEF_QUEUE_RUN_DELAY
// "300s", DEF_MIN_BACKOFF_TIME, DEF_MAX_BACKOFF_TIME "4000s",
// DEF_MAX_QUEUE_TIME "5d", DEF_DSN_QUEUE_TIME "5d", DEF_DELAY_WARN_TIME
// "0h"; src/global/mail_conf_time.c, get_def_time_unit()).
var retryConfUnits = []byte{'s', 's', 's', 'd', 'd', 'h'}

// readRetryConf runs `postconf -h` for the retry-policy keys. Tests replace it
// the same way logtail stubs queueIDs.
var readRetryConf = postconfRetryValues

func postconfRetryValues() ([]string, error) {
	// Fixed argv, no user input — same pattern as Queue (security.md).
	// Postfix 3.7.11, src/postconf/postconf_main.c, pcf_show_parameters():
	// named parameters are printed in argument order, with -h one "%s\n"
	// value each; -x expands "$name" as Postfix itself does when it reads
	// the value (mail_conf_eval()).
	cmd := exec.Command("postconf", "-h", "-x",
		"queue_run_delay",
		"minimal_backoff_time",
		"maximal_backoff_time",
		"maximal_queue_lifetime",
		"bounce_queue_lifetime",
		"delay_warning_time",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("postconf -h: %w: %s", err, strings.TrimSpace(string(out)))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(retryConfKeys) {
		return nil, fmt.Errorf("postconf -h: got %d values, want %d", len(lines), len(retryConfKeys))
	}
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return lines, nil
}

// LoadRetryPolicy reads the effective Postfix retry parameters. On any failure
// it logs a warning and returns DefaultRetryPolicy; the HTTP role must not
// refuse to start because postconf is absent.
func LoadRetryPolicy() RetryPolicy {
	lines, err := readRetryConf()
	if err != nil {
		log.Printf("postfix: retry policy: %v; using compiled-in defaults", err)
		return DefaultRetryPolicy()
	}
	p, err := parseRetryPolicy(lines)
	if err != nil {
		log.Printf("postfix: retry policy: %v; using compiled-in defaults", err)
		return DefaultRetryPolicy()
	}
	return p
}

func parseRetryPolicy(lines []string) (RetryPolicy, error) {
	if len(lines) != len(retryConfKeys) {
		return RetryPolicy{}, fmt.Errorf("got %d values, want %d", len(lines), len(retryConfKeys))
	}
	var durs [6]time.Duration
	for i, line := range lines {
		d, err := ParseTime(line, retryConfUnits[i])
		if err != nil {
			return RetryPolicy{}, fmt.Errorf("%s: %w", retryConfKeys[i], err)
		}
		durs[i] = d
	}
	// Postfix 3.7.11, src/qmgr/qmgr.c, qmgr_post_init(): "bounce_queue_lifetime
	// is larger than maximal_queue_lifetime - adjusting" - qmgr uses the
	// smaller.
	if durs[4] > durs[3] {
		durs[4] = durs[3]
	}
	return RetryPolicy{
		QueueRunDelay:        durs[0],
		MinimalBackoff:       durs[1],
		MaximalBackoff:       durs[2],
		MaximalQueueLifetime: durs[3],
		BounceQueueLifetime:  durs[4],
		DelayWarningTime:     durs[5],
	}, nil
}

// FirstRetry is the human string for the first deferred retry (minimal
// backoff), shared by the Mail queue card and delivery history.
func (p RetryPolicy) FirstRetry() string {
	return FormatDuration(p.MinimalBackoff)
}

// BackoffCap is the human string for maximal_backoff_time.
func (p RetryPolicy) BackoffCap() string {
	return FormatDuration(p.MaximalBackoff)
}

// LongestLifetime is how long Postfix may go on working on a message: the
// larger of the queue lifetime for ordinary mail and for mail from the null
// sender, which an application may submit too. It bounds which stored log
// lines can be about a send-log row (store.SendLogLines).
//
// Postfix 3.7.11, src/qmgr/qmgr_active.c, qmgr_active_done_25_generic(): a
// message expires after var_max_queue_time, or var_dsn_queue_time when its
// sender is null - whoever submitted it; qmgr.c caps the second at the
// first, and so does parseRetryPolicy, which leaves the first the longer.
func (p RetryPolicy) LongestLifetime() time.Duration {
	return p.MaximalQueueLifetime
}

// QueueLifetime is the human string for maximal_queue_lifetime.
func (p RetryPolicy) QueueLifetime() string {
	return FormatDuration(p.MaximalQueueLifetime)
}
