// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"portcloak/internal/engine/obs"
)

// termSink renders a running job to a terminal.
//
// Everything it writes goes to stderr. A capture's narration is not its result,
// and a run that printed phases to stdout could not be piped into anything while
// it was still talking.
//
// It watches only the jobs this invocation started. The engine's sink is
// process-wide, and a resumed job or a second realm would otherwise interleave
// its phases into a report about something else.
type termSink struct {
	mu      sync.Mutex
	s       Streams
	g       *globals
	tty     bool
	watch   map[string]bool
	realmOf map[string]string
	// skipped records, per job, the phases that ran and abstained. A phase can
	// complete and skip in the same breath — verification with no reachable
	// Admin API is the usual one — and drawing that as a tick claims something
	// happened that did not. The desktop app had exactly this bug; see the
	// SkippedPhases field on config.Job.
	skipped map[string]map[obs.Phase]bool
	// wake lets a waiter react to a terminal state without waiting out its
	// poll interval. The job records stay the authority; this is only speed.
	wake chan struct{}
	// last is the phase currently drawn on a TTY, so an in-place line can be
	// cleared before the next thing is written.
	last string
}

func newTermSink(s Streams, g *globals) *termSink {
	return &termSink{
		s:       s,
		g:       g,
		tty:     !g.noColor && isTerminal(s.Err),
		watch:   map[string]bool{},
		realmOf: map[string]string{},
		skipped: map[string]map[obs.Phase]bool{},
		wake:    make(chan struct{}, 1),
	}
}

// Watch adds a job whose events should be rendered.
func (t *termSink) Watch(jobID, realm string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.watch[jobID] = true
	t.realmOf[jobID] = realm
}

// Skipped reports the phases a job ran and abstained from.
func (t *termSink) Skipped(jobID string) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []string
	for p := range t.skipped[jobID] {
		out = append(out, string(p))
	}
	return out
}

// Wake is the channel a waiter selects on to be told something happened.
func (t *termSink) Wake() <-chan struct{} { return t.wake }

func (t *termSink) Emit(e obs.Event) {
	t.mu.Lock()
	if !t.watch[e.JobID] {
		t.mu.Unlock()
		return
	}
	// A phase message that says it skipped is the only signal available here;
	// the job record carries SkippedPhases, but not until it is saved.
	if e.Kind == obs.EventPhaseCompleted && isSkip(e.Message) {
		if t.skipped[e.JobID] == nil {
			t.skipped[e.JobID] = map[obs.Phase]bool{}
		}
		t.skipped[e.JobID][e.Phase] = true
	}
	line, prefix := t.formatLocked(e)
	t.mu.Unlock()

	if e.Kind == obs.EventJobStateChange || e.Kind == obs.EventPhaseFailed {
		select {
		case t.wake <- struct{}{}:
		default:
		}
	}
	if line == "" || t.g.quiet {
		return
	}
	fmt.Fprintf(t.s.Err, "%s%s\n", prefix, line)
}

// formatLocked builds the line for an event, or "" for one not worth printing.
func (t *termSink) formatLocked(e obs.Event) (line, prefix string) {
	switch e.Kind {
	case obs.EventLog:
		// Subprocess output is a firehose — kc.sh narrates every table it
		// reads — so it is behind -v rather than on by default.
		if t.g.verbose < 1 {
			return "", ""
		}
		return e.Message, "    "

	case obs.EventProgress:
		// Byte-by-byte progress is for a TTY that can redraw. In a CI log it
		// would be thousands of lines nobody reads, so it is dropped there and
		// the phase's completion line carries the totals instead.
		if !t.tty || e.Total <= 0 {
			return "", ""
		}
		return fmt.Sprintf("%s %d/%d %s %s", obs.PhaseLabel(e.Phase),
			e.Current, e.Total, e.Unit, e.Item), t.marker(e)

	case obs.EventJobStateChange:
		return "", ""

	default:
		return phaseLine(e), t.marker(e)
	}
}

// marker is the leading glyph or timestamp for a line.
//
// A TTY gets a symbol that says at a glance what happened. Anything else gets a
// timestamp, because a CI log is read long afterwards by somebody working out
// how long a step took.
func (t *termSink) marker(e obs.Event) string {
	if !t.tty {
		return time.Now().Format("15:04:05") + " "
	}
	switch e.Kind {
	case obs.EventPhaseCompleted:
		if t.skipped[e.JobID][e.Phase] {
			// Not a tick. A skipped phase that renders like a passed one tells
			// an operator their secrets were verified when nothing checked.
			return "  – "
		}
		return "  ✓ "
	case obs.EventPhaseFailed:
		return "  ✗ "
	case obs.EventRetry:
		return "  ↻ "
	case obs.EventBreakerOpen:
		return "  ⏸ "
	default:
		return "  · "
	}
}

// isSkip recognises the engine's own way of saying a phase abstained.
//
// It is a heuristic, and knowingly so. obs carries no "skipped" event kind, and
// adding one would mean touching every reporter call for what is a presentation
// concern. Two of the three skip paths say so in the message they complete with
// — verification with no reachable Admin API is the common one — and the third,
// post-restore validation, says nothing at all and records it only on the job.
//
// So this decides the live glyph and nothing else. The authority is
// config.Job.SkippedPhases, which the summary at the end of a run reads back and
// renders exactly as the Activity screen does. That is the same split
// internal/app/logs.go already makes between the event stream and the log store,
// for the same reason: the stream is immediacy, the record is truth.
func isSkip(message string) bool {
	m := strings.ToLower(message)
	return strings.HasPrefix(m, "skipped") ||
		strings.Contains(m, "was skipped") ||
		strings.Contains(m, "were skipped") ||
		strings.Contains(m, "did not run")
}
