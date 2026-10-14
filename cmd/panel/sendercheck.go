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
// socket of its own. Postfix attaches that socket strictly
// (default_action=tempfail in build/postfix-config.sh; Postfix 3.7.11,
// src/milter/milter8.c, milter8_comm_error(): "451 4.7.1 Service unavailable -
// try again later"), so while this role is not serving, mail is deferred:
// what that costs is counted here and
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
	now := time.Now()
	mark := cfg.senderSocket + ".alive"
	// The mark's age is read before the mark is kept again: asking supervisord
	// may take half a minute, and the mark must not go stale meanwhile, or a
	// restart in that half minute would count it as downtime.
	var markAt time.Time
	if fi, err := os.Stat(mark); err == nil {
		markAt = fi.ModTime()
	}
	go keepAlive(ctx, mark, st)
	go func() {
		if err := st.StartSenderCheckStats(now); err != nil {
			log.Printf("sender-check: statistics: %v", err)
		}
		if down, estimated := senderCheckDowntime(markAt, now, postfixStarted); down > 0 {
			if estimated {
				log.Printf("sender-check: was not answering for at most %s (supervisord did not say since when Postfix ran)", down.Round(time.Second))
			} else {
				log.Printf("sender-check: was not answering for %s while Postfix was running", down.Round(time.Second))
			}
			for _, part := range byDay(now.Add(-down), now) {
				if err := st.AddSenderCheckDown(part.day, part.down, estimated); err != nil {
					log.Printf("sender-check: statistics: %v", err)
				}
			}
		}
	}()

	// The two tables the mail path itself goes by: whom Postfix lets send as
	// what, and what OpenDKIM signs.
	bind := milter.NewBindings(postfix.New(cfg.postfixDir).SenderLoginMapsPath())
	signers := milter.NewSigners(domain.NewOpenDKIM(cfg.opendkimDir).SigningTablePath())
	return milter.ServeSender(ctx, ln, bind, signers, st)
}

// postfixState is what supervisord said about Postfix: since when it runs, if
// it does; known is false when supervisord did not answer.
type postfixState struct {
	started        time.Time
	running, known bool
}

// senderCheckDowntime is for how long the check was not serving before now
// while Postfix was: the time since the mark was last set (markAt; zero
// when there is none), or since
// Postfix started if that was later. No mark means the container is new, and
// a Postfix that is not running means mail was stopped for another reason -
// a rollout, a reboot - and neither is the check's downtime. When supervisord
// could not say, the time since the mark is taken and estimated is true: an
// upper bound, since Postfix may not have been running all of it (audit
// 2026-10-12, stage 2b - until then such a start counted nothing).
func senderCheckDowntime(markAt, now time.Time, ask func() postfixState) (down time.Duration, estimated bool) {
	if markAt.IsZero() {
		return 0, false
	}
	from := markAt
	p := ask()
	switch {
	case !p.known:
		estimated = true
	case !p.running:
		return 0, false
	case p.started.After(from):
		from = p.started
	}
	if d := now.Sub(from); d > 0 {
		return d, estimated
	}
	return 0, false
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

// postfixAskFor is how long postfixStarted keeps asking a supervisord that
// does not answer - at the container's start it may not be listening yet.
var postfixAskFor, postfixAskEvery = 30 * time.Second, 2 * time.Second

// postfixStarted asks supervisord since when Postfix has been running, again
// and again for postfixAskFor while it does not answer. supervisor 4.2.5,
// supervisor/rpcinterface.py, _interpretProcessInfo(): the uptime runs from
// the spawn of program "postfix", which is build/postfix-wrapper.sh; it waits
// for the milter sockets (up to MILTER_WAIT_TIMEOUT) before it execs
// "postfix start-fg", so the time given is up to that much early
// (assumptions A-46).
func postfixStarted() postfixState {
	deadline := time.Now().Add(postfixAskFor)
	for {
		procs, err := health.Processes()
		if err == nil {
			for _, p := range procs {
				if p.Name != "postfix" || p.State != "RUNNING" {
					continue
				}
				if up, ok := health.Uptime(p.Detail); ok {
					return postfixState{started: time.Now().Add(-up), running: true, known: true}
				}
			}
			return postfixState{known: true}
		}
		if time.Now().After(deadline) {
			// Said aloud: this start's downtime is then an estimate.
			log.Printf("sender-check: statistics: when Postfix started is not known: %v", err)
			return postfixState{}
		}
		time.Sleep(postfixAskEvery)
	}
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
