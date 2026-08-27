// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package obs

import (
	"sync"
	"time"
)

// Phase names a stage of a job. They are the same strings the Activity screen
// renders, so they read as things that happen to an operator's system rather
// than as internal state names.
type Phase string

const (
	PhaseProbe    Phase = "probe"
	PhaseClone    Phase = "clone"
	PhaseExport   Phase = "export"
	PhaseFetch    Phase = "fetch"
	PhaseVerify   Phase = "verify"
	PhaseTeardown Phase = "teardown"
	PhaseManifest Phase = "manifest"
	PhasePackage  Phase = "package"
	PhaseUpload   Phase = "upload"

	// Restore phases.
	PhaseDownload      Phase = "download"
	PhaseIntegrity     Phase = "integrity"
	PhasePreconditions Phase = "preconditions"
	PhaseDryRun        Phase = "dryRun"
	PhaseImport        Phase = "import"
	PhaseValidate      Phase = "validate"

	// Inspection phases.
	PhaseIndex Phase = "index"
)

// PhaseLabel renders a phase as the sentence fragment the UI shows.
func PhaseLabel(p Phase) string {
	switch p {
	case PhaseProbe:
		return "Checking the environment"
	case PhaseClone:
		return "Preparing an ephemeral clone"
	case PhaseExport:
		return "Exporting the realm"
	case PhaseFetch:
		return "Collecting the exported files"
	case PhaseVerify:
		return "Verifying secrets and dependencies"
	case PhaseTeardown:
		return "Cleaning up"
	case PhaseManifest:
		return "Building the manifest"
	case PhasePackage:
		return "Sealing the snapshot"
	case PhaseUpload:
		return "Uploading to storage"
	case PhaseDownload:
		return "Downloading the snapshot"
	case PhaseIntegrity:
		return "Checking integrity"
	case PhasePreconditions:
		return "Reviewing preconditions"
	case PhaseDryRun:
		return "Previewing changes"
	case PhaseImport:
		return "Importing the realm"
	case PhaseValidate:
		return "Validating the destination"
	case PhaseIndex:
		return "Building the inspection index"
	default:
		return string(p)
	}
}

// EventKind distinguishes the shapes of event a job emits.
type EventKind string

const (
	EventPhaseStarted   EventKind = "phaseStarted"
	EventPhaseCompleted EventKind = "phaseCompleted"
	EventPhaseFailed    EventKind = "phaseFailed"
	EventProgress       EventKind = "progress"
	EventLog            EventKind = "log"
	EventRetry          EventKind = "retry"
	EventBreakerOpen    EventKind = "breakerOpen"
	EventCloneCreated   EventKind = "cloneCreated"
	EventCloneDestroyed EventKind = "cloneDestroyed"
	EventJobStateChange EventKind = "jobState"
)

// Event is one thing that happened inside a job. It carries no secret: Message
// is passed through RedactText before it leaves the engine, and there is no
// field for a value.
type Event struct {
	JobID   string    `json:"jobId"`
	Kind    EventKind `json:"kind"`
	Phase   Phase     `json:"phase,omitempty"`
	Label   string    `json:"label,omitempty"`
	Message string    `json:"message,omitempty"`
	Item    string    `json:"item,omitempty"`
	// Current and Total describe a countable unit — bytes, files, users.
	Current int64  `json:"current,omitempty"`
	Total   int64  `json:"total,omitempty"`
	Unit    string `json:"unit,omitempty"`
	// Attempt and RetryIn describe a retry in flight, so a wait is never an
	// unexplained spinner.
	Attempt int           `json:"attempt,omitempty"`
	RetryIn time.Duration `json:"retryIn,omitempty"`
	At      time.Time     `json:"at"`
}

// Sink receives events. The Wails bridge is one implementation; tests use
// RecordingSink so the whole engine runs headlessly.
type Sink interface {
	Emit(Event)
}

// SinkFunc adapts a function to a Sink.
type SinkFunc func(Event)

func (f SinkFunc) Emit(e Event) { f(e) }

// NopSink discards events.
type NopSink struct{}

func (NopSink) Emit(Event) {}

// RecordingSink keeps every event for assertion in tests.
type RecordingSink struct {
	mu     sync.Mutex
	events []Event
}

func (r *RecordingSink) Emit(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

// Events returns a copy of everything recorded so far.
func (r *RecordingSink) Events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, len(r.events))
	copy(out, r.events)
	return out
}

// Reporter is the handle a job holds to report its own progress. It stamps the
// job id and the current time so callers cannot forget either, and scrubs every
// free-text message on the way out.
type Reporter struct {
	jobID string
	sink  Sink
	now   func() time.Time

	mu    sync.Mutex
	phase Phase
}

// NewReporter binds a sink to one job.
func NewReporter(jobID string, sink Sink) *Reporter {
	if sink == nil {
		sink = NopSink{}
	}
	return &Reporter{jobID: jobID, sink: sink, now: time.Now}
}

// WithClock replaces the reporter's clock, for deterministic tests.
func (r *Reporter) WithClock(now func() time.Time) *Reporter {
	r.now = now
	return r
}

func (r *Reporter) emit(e Event) {
	e.JobID = r.jobID
	e.At = r.now()
	e.Message = RedactText(e.Message)
	e.Item = RedactText(e.Item)
	if e.Phase != "" && e.Label == "" {
		e.Label = PhaseLabel(e.Phase)
	}
	r.sink.Emit(e)
}

// Phase returns the phase the job is currently in.
func (r *Reporter) Phase() Phase {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.phase
}

// StartPhase marks the beginning of a stage.
func (r *Reporter) StartPhase(p Phase) {
	r.mu.Lock()
	r.phase = p
	r.mu.Unlock()
	r.emit(Event{Kind: EventPhaseStarted, Phase: p})
}

// CompletePhase marks a stage as finished successfully.
func (r *Reporter) CompletePhase(p Phase, message string) {
	r.emit(Event{Kind: EventPhaseCompleted, Phase: p, Message: message})
}

// FailPhase marks a stage as failed. The message is the operator-facing
// sentence, not the raw error.
func (r *Reporter) FailPhase(p Phase, message string) {
	r.emit(Event{Kind: EventPhaseFailed, Phase: p, Message: message})
}

// Progress reports countable movement within the current phase.
func (r *Reporter) Progress(current, total int64, unit, item string) {
	r.emit(Event{Kind: EventProgress, Phase: r.Phase(), Current: current, Total: total, Unit: unit, Item: item})
}

// Log carries a line of streamed subprocess output to the UI.
func (r *Reporter) Log(message string) {
	r.emit(Event{Kind: EventLog, Phase: r.Phase(), Message: message})
}

// Retry announces that an operation will be attempted again, and when.
func (r *Reporter) Retry(item string, attempt int, in time.Duration, reason string) {
	r.emit(Event{Kind: EventRetry, Phase: r.Phase(), Item: item, Attempt: attempt, RetryIn: in, Message: reason})
}

// BreakerOpen announces that an endpoint has been taken out of service for a
// cooldown, which the UI states plainly instead of showing a hang.
func (r *Reporter) BreakerOpen(endpoint string, cooldown time.Duration) {
	r.emit(Event{Kind: EventBreakerOpen, Phase: r.Phase(), Item: endpoint, RetryIn: cooldown})
}

// CloneCreated records that an ephemeral clone now exists. Its existence is
// never invisible to the operator.
func (r *Reporter) CloneCreated(ref string) {
	r.emit(Event{Kind: EventCloneCreated, Phase: PhaseClone, Item: ref})
}

// CloneDestroyed records that the clone is gone.
func (r *Reporter) CloneDestroyed(ref string) {
	r.emit(Event{Kind: EventCloneDestroyed, Phase: PhaseTeardown, Item: ref})
}

// JobState announces a transition of the job as a whole.
func (r *Reporter) JobState(state, message string) {
	r.emit(Event{Kind: EventJobStateChange, Item: state, Message: message})
}
