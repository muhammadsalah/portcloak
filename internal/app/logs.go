// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"sync"

	"portcloak/internal/engine/obs"
)

// maxLogLines is how much of a job's output is kept. It is still a tail rather
// than a log file — a run's output is unbounded and this is held in memory —
// but ten thousand lines is enough to hold a whole export of a large realm,
// which is what an operator scrolls back through when a job is stuck.
//
// It is also why the screen fetches by cursor rather than re-reading the whole
// tail every second: at this size, doing that would be most of a megabyte a
// second per running job, for output that has not changed.
const maxLogLines = 10_000

// LogLine is one line as the screen draws it.
type LogLine struct {
	Text string `json:"text"`
	// FromPortCloak marks a line the tool wrote rather than one the subprocess
	// said, which the screen colours differently.
	FromPortCloak bool `json:"fromPortCloak"`
}

// LogView is a job's output as the engine holds it, from wherever the caller
// had got to.
type LogView struct {
	JobID string    `json:"jobId"`
	Lines []LogLine `json:"lines"`
	// Next is the cursor to ask for next time. It counts every line the job has
	// ever said, not the position in the tail, so it stays meaningful after the
	// cap has discarded the front of it.
	Next int `json:"next"`
	// Reset says these lines replace what the caller has rather than extending
	// it: either the caller asked from the beginning, or it asked from a point
	// the cap has already discarded and cannot be continued from.
	Reset bool `json:"reset"`
	// Truncated says the tail is not the whole run, so the screen can say so
	// rather than implying the job started with the line at the top.
	Truncated bool `json:"truncated"`
}

// logStore keeps each job's output where the screen can ask for it again.
//
// It exists because the output cannot be fetched back from anywhere else. The
// export's stdout arrives over a Docker or Kubernetes exec stream, and neither
// keeps it: an exec session is not a container log, and by the time an operator
// reloads the screen the clone that produced the output has usually been
// destroyed. So the one moment those bytes can be captured is as they go past,
// and this is what captures them.
//
// The screen still folds the live event stream for immediacy — a line has to
// appear the instant it is said, not on the next poll — and then reconciles
// against this on every refresh, asking only for what it has not already been
// given. That makes this the authority and the stream an approximation of it,
// rather than two records that can disagree permanently.
type logStore struct {
	mu    sync.Mutex
	lines map[string][]LogLine
	// said counts every line a job has ever said, including the ones the cap
	// discarded. It is what makes a cursor mean something: the index of a line
	// in the run rather than in whatever is still held.
	said map[string]int
}

func newLogStore() *logStore {
	return &logStore{lines: map[string][]LogLine{}, said: map[string]int{}}
}

// record folds one event into the job's tail, using the same rule the screen
// uses: the subprocess's own output, plus the few things PortCloak says into
// the same panel because they belong in the run's story.
func (s *logStore) record(e obs.Event) {
	line, ok := logLineFor(e)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	lines := append(s.lines[e.JobID], line)
	if over := len(lines) - maxLogLines; over > 0 {
		// Copied rather than resliced: keeping the tail as a slice of an
		// ever-growing array holds every discarded line alive behind it, which
		// is the leak the cap exists to prevent.
		lines = append(make([]LogLine, 0, maxLogLines), lines[over:]...)
	}
	s.lines[e.JobID] = lines
	s.said[e.JobID]++
}

// logLineFor is the single rule for which events are lines. The frontend's fold
// mirrors it for the live case; this is the copy that survives a reload.
func logLineFor(e obs.Event) (LogLine, bool) {
	switch e.Kind {
	case obs.EventLog:
		if e.Message == "" {
			return LogLine{}, false
		}
		return LogLine{Text: e.Message}, true
	case obs.EventCloneCreated:
		return LogLine{Text: "Ephemeral clone " + e.Item + " is running.", FromPortCloak: true}, true
	case obs.EventCloneDestroyed:
		return LogLine{Text: "Ephemeral clone " + e.Item + " destroyed.", FromPortCloak: true}, true
	}
	return LogLine{}, false
}

// tail returns what a job has said since the caller's cursor, which is empty
// rather than an error for a job that never said anything.
//
// A cursor of zero, or one older than the cap still holds, comes back as
// everything held with Reset set: there is no way to continue from a line that
// no longer exists, and quietly returning the newest lines as if they followed
// it would splice a hole into the middle of the operator's log.
func (s *logStore) tail(jobID string, after int) LogView {
	s.mu.Lock()
	defer s.mu.Unlock()

	held := s.lines[jobID]
	said := s.said[jobID]
	oldest := said - len(held)

	out := LogView{JobID: jobID, Next: said, Truncated: oldest > 0}
	switch {
	case after >= said:
		// Nothing new. The caller keeps what it has.
		out.Lines = []LogLine{}
	case after <= oldest:
		out.Reset = true
		out.Lines = append(make([]LogLine, 0, len(held)), held...)
	default:
		from := after - oldest
		out.Lines = append(make([]LogLine, 0, len(held)-from), held[from:]...)
	}
	return out
}

// forget drops a job's output. A discarded or purged job takes its output with
// it: keeping it would grow this map for the life of the process and would put
// a deleted job's export lines in front of whoever looks next.
func (s *logStore) forget(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.lines, jobID)
	delete(s.said, jobID)
}

// recordingSink records every event into the store on its way to wherever
// events were already going.
//
// It wraps rather than replaces, so the bridge to the frontend and the
// recorder a headless test installs are both unaffected by the store existing.
type recordingSink struct {
	store *logStore
	next  obs.Sink
}

func (r recordingSink) Emit(e obs.Event) {
	r.store.record(e)
	if r.next != nil {
		r.next.Emit(e)
	}
}
