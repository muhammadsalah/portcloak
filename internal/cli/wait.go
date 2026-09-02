// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"portcloak/internal/app"
	"portcloak/internal/engine/config"
)

// Capture and Restore both detach: they save their jobs, start a goroutine under
// context.WithoutCancel, and return the ids immediately. That suits a window,
// which has an event loop to go back to. A command has nothing to go back to, so
// it has to block on something.
//
// It blocks on two things at once. The job records are the authority and the
// event stream is the immediacy — the same arrangement internal/app/logs.go
// already makes between the log store and the live events, for the same reason.
//
// Sink-only would hang: an event that is never emitted, because a save failed or
// because a state transition was added later without a reporter call, leaves the
// command waiting for ever with nothing on screen. Polling-only would be up to a
// tick stale, which in a terminal reads as a process that has died.

// pollEvery is how often the job records are re-read. It is short enough that a
// missed event costs a quarter of a second and long enough that a capture taking
// twenty minutes does not spend it stat-ing files.
const pollEvery = 250 * time.Millisecond

// waiter blocks on a set of jobs and handles the signals that arrive meanwhile.
type waiter struct {
	r    *run
	sink *termSink
	jobs *app.JobsController
}

func newWaiter(r *run, sink *termSink) *waiter {
	return &waiter{r: r, sink: sink, jobs: app.NewJobsController(r.eng)}
}

// outcome is what a run of jobs came to.
type outcome struct {
	Jobs      []*config.Job
	Cancelled bool
	// TimedOut records that --timeout expired rather than the operator asking.
	TimedOut bool
}

// wait blocks until every job is terminal, or until the operator or the deadline
// says to stop.
//
// Cancelling is not abandoning. A capture may be holding an ephemeral clone in
// somebody else's cluster, with the production database credentials in it, and
// the clone is destroyed by the job's own teardown — which only runs if the job
// is cancelled through the orchestrator and then allowed to finish. Cancelling
// this command's context would do nothing at all: the run detached under
// context.WithoutCancel and is not listening to it.
func (w *waiter) wait(ctx context.Context, ids []string) outcome {
	sig := make(chan os.Signal, 2)
	// SIGTERM as well as SIGINT: a CI job that hits its time limit is the
	// commonest way a capture is stopped, and it deserves the same teardown as
	// a Ctrl-C.
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)

	tick := time.NewTicker(pollEvery)
	defer tick.Stop()

	out := outcome{}
	cancelling := false
	var deadline <-chan time.Time

	for {
		if jobs, done := w.settled(ids); done {
			out.Jobs = jobs
			return out
		}

		select {
		case <-tick.C:
		case <-w.sink.Wake():
		case <-ctx.Done():
			if !cancelling {
				cancelling = true
				out.TimedOut = true
				w.beginCancel(ids, "The time limit was reached.")
				deadline = time.After(cancelGrace)
			}
		case <-sig:
			if !cancelling {
				cancelling = true
				out.Cancelled = true
				w.beginCancel(ids, "")
				deadline = time.After(cancelGrace)
				continue
			}
			// A second signal from somebody who has decided not to wait. Say
			// what is being left behind before going, because after this
			// nothing else will.
			w.abandon(ids)
			out.Cancelled = true
			out.Jobs = w.load(ids)
			return out
		case <-deadline:
			w.abandon(ids)
			out.Jobs = w.load(ids)
			return out
		}
	}
}

// cancelGrace is how long teardown is given after a cancel before the operator
// is told what is being left behind. Destroying a container or deleting a Job and
// waiting for the pod to go is seconds, not minutes; two minutes is generous
// enough that the deadline means something has gone wrong.
const cancelGrace = 2 * time.Minute

func (w *waiter) beginCancel(ids []string, why string) {
	if why != "" {
		fmt.Fprintln(w.r.s.Err, "\npcloak:", why)
	}
	fmt.Fprintln(w.r.s.Err, "pcloak: cancelling. PortCloak is tearing down what this run created.")
	fmt.Fprintln(w.r.s.Err, "  Do not kill this process — an ephemeral clone may be holding database")
	fmt.Fprintln(w.r.s.Err, "  credentials until teardown finishes.")
	for _, id := range ids {
		// "not running" is not an error here: a job that already finished
		// needs no cancelling, and one that never started has nothing to tear
		// down.
		_ = w.r.eng.Orch.Cancel(id)
	}
}

// abandon leaves without waiting for teardown, and says exactly what may still
// be running so it can be removed by hand.
//
// This is the only path on which an operator who must kill the process is told
// what was left behind. Exiting silently here is the one thing that must not
// happen: a clone holding production credentials, and nothing on screen naming
// it.
func (w *waiter) abandon(ids []string) {
	fmt.Fprintln(w.r.s.Err, "\npcloak: leaving without waiting for teardown.")
	left := false
	for _, j := range w.load(ids) {
		if j == nil || j.Provenance.CloneRef == "" {
			continue
		}
		left = true
		fmt.Fprintf(w.r.s.Err, "  %s may still be running in %s (job %s)\n",
			j.Provenance.CloneRef, j.Environment, j.ID)
	}
	if left {
		fmt.Fprintln(w.r.s.Err, "  Remove it with: pcloak orphans list")
	}
}

// settled reports the jobs and whether every one has finished.
func (w *waiter) settled(ids []string) ([]*config.Job, bool) {
	jobs := w.load(ids)
	for _, j := range jobs {
		if j == nil || !j.State.Terminal() {
			return jobs, false
		}
	}
	return jobs, true
}

func (w *waiter) load(ids []string) []*config.Job {
	out := make([]*config.Job, 0, len(ids))
	for _, id := range ids {
		j, err := w.r.eng.Jobs.Load(id)
		if err != nil {
			out = append(out, nil)
			continue
		}
		out = append(out, j)
	}
	return out
}

// verdict turns finished jobs into an exit code.
//
// A run where some realms produced a snapshot and some did not is reported as
// partial, not as failure. One snapshot holds exactly one realm, so there is no
// shared bundle to corrupt: what exists is N-1 valid, individually restorable
// snapshots plus a failed job. Calling that "failed" would send an operator
// looking for damage that is not there.
func verdict(o outcome) error {
	if len(o.Jobs) == 0 {
		return nil
	}
	var ok, failed, interrupted, cancelled int
	for _, j := range o.Jobs {
		switch {
		case j == nil:
			failed++
		case j.State == config.JobCompleted:
			ok++
		case j.State == config.JobCancelled:
			cancelled++
		case j.State == config.JobInterrupted:
			interrupted++
		default:
			failed++
		}
	}
	switch {
	case o.Cancelled || cancelled > 0:
		return exitWith(ExitCancelled, "")
	case interrupted > 0 && failed == 0:
		// Interrupted is resumable, and waiting is what helps.
		return exitWith(ExitRetryable, "")
	case failed == 0:
		return nil
	case ok > 0:
		return exitWith(ExitPartial, "")
	default:
		return exitWith(ExitFailed, "")
	}
}
