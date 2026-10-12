package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/mixeme/selfpost/internal/domain"
	"github.com/mixeme/selfpost/internal/health"
	"github.com/mixeme/selfpost/internal/milter"
	"github.com/mixeme/selfpost/internal/postfix"
	"github.com/mixeme/selfpost/internal/store"
)

// aliveEvery is how often the sender check marks itself alive. The gap a
// restart finds since the last mark is the check's downtime, so the mark's
// age is the error of that figure.
const aliveEvery = 2 * time.Second

// serveSenderCheck runs the sender check (internal/milter/sender.go) on a
// socket of its own. Postfix attaches that socket strictly, so while this
// role is not serving, mail is deferred: what that costs is counted here and
// by the log tailer, and shown on Server > Health
// (docs/plans/from-header-binding.md § Statistics).
func serveSenderCheck(ctx context.Context, cfg config, st *store.Store) error {
	ln, err := listenMilter(cfg.senderSocket)
	if err != nil {
		return err
	}
	log.Printf("sender-check listening on %s", cfg.senderSocket)

	// Statistics are the database's business and supervisord's, and the check
	// is neither: they are kept beside it, and nothing of them is waited for
	// before it serves.
	go func() {
		now := time.Now()
		mark := cfg.senderSocket + ".alive"
		if err := st.StartSenderCheckStats(now); err != nil {
			log.Printf("sender-check: statistics: %v", err)
		}
		if down := senderCheckDowntime(mark, now, postfixStarted); down > 0 {
			log.Printf("sender-check: was not answering for %s while Postfix was running", down.Round(time.Second))
			for _, part := range byDay(now.Add(-down), now) {
				if err := st.AddSenderCheckDown(part.day, part.down); err != nil {
					log.Printf("sender-check: statistics: %v", err)
				}
			}
		}
		keepAlive(ctx, mark, st)
	}()

	// The two tables the mail path itself goes by: whom Postfix lets send as
	// what, and what OpenDKIM signs.
	bind := milter.NewBindings(postfix.New(cfg.postfixDir).SenderLoginMapsPath())
	signers := milter.NewSigners(domain.NewOpenDKIM(cfg.opendkimDir).SigningTablePath())
	return milter.ServeSender(ctx, ln, bind, signers, st)
}

// senderCheckDowntime is for how long the check was not serving before now
// while Postfix was: the time since the mark at path was last set, or since
// Postfix started if that was later. No mark means the container is new, and
// a Postfix that is not running means mail was stopped for another reason -
// a rollout, a reboot - and neither is the check's downtime.
func senderCheckDowntime(path string, now time.Time, postfixStarted func() (time.Time, bool)) time.Duration {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	from := fi.ModTime()
	started, running := postfixStarted()
	if !running {
		return 0
	}
	if started.After(from) {
		from = started
	}
	if d := now.Sub(from); d > 0 {
		return d
	}
	return 0
}

// dayPart is the part of a stretch of downtime that fell on one UTC day.
type dayPart struct {
	day  time.Time
	down time.Duration
}

// byDay cuts the time from 'from' to 'to' at the UTC midnights between them,
// so that a night's outage is not all booked on the morning it ended.
func byDay(from, to time.Time) []dayPart {
	from, to = from.UTC(), to.UTC()
	var parts []dayPart
	for from.Before(to) {
		next := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
		if next.After(to) {
			next = to
		}
		parts = append(parts, dayPart{day: from, down: next.Sub(from)})
		from = next
	}
	return parts
}

// postfixStarted asks supervisord since when Postfix has been running.
func postfixStarted() (time.Time, bool) {
	procs, err := health.Processes()
	if err != nil {
		// Said aloud: the downtime of this start is then not counted.
		log.Printf("sender-check: statistics: when Postfix started is not known: %v", err)
		return time.Time{}, false
	}
	for _, p := range procs {
		if p.Name != "postfix" || p.State != "RUNNING" {
			continue
		}
		if up, ok := health.Uptime(p.Detail); ok {
			return time.Now().Add(-up), true
		}
	}
	return time.Time{}, false
}

// keepAlive sets the mark until ctx ends, and once a day drops the days of
// the statistics that are older than a year.
func keepAlive(ctx context.Context, path string, st *store.Store) {
	touch := func() {
		now := time.Now()
		if err := os.Chtimes(path, now, now); err != nil {
			if err := os.WriteFile(path, nil, 0o644); err != nil {
				log.Printf("sender-check: alive mark: %v", err)
			}
		}
	}
	touch()
	tick := time.NewTicker(aliveEvery)
	defer tick.Stop()
	prune := time.NewTicker(24 * time.Hour)
	defer prune.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			touch()
		case <-prune.C:
			if err := st.PruneSenderCheckDays(time.Now()); err != nil {
				log.Printf("sender-check: statistics: %v", err)
			}
		}
	}
}
