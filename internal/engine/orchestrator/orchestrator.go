// Package orchestrator sequences a job from end to end and is the only place
// that knows the order things happen in.
//
// It depends on interfaces only — Executor, BlobStore, Verifier — never on a
// concrete adapter. Adapters are selected by a registry wired in internal/app,
// which is the seam that makes "add a podman target later" additive rather than
// surgical.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/manifest"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/realm"
	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/store"
	"portcloak/internal/engine/target"
)

// Verifier is the optional Admin REST pass.
//
// Everything it offers is strictly secondary: offline kc.sh export is the
// authoritative capture mechanism, and an unreachable Admin API is a normal,
// expected outcome rather than a failure.
type Verifier interface {
	// Reachable reports whether the Admin API answered.
	Reachable(ctx context.Context) bool
	// Realms lists the realms on the target, for the capture wizard.
	Realms(ctx context.Context) ([]string, error)
	// VerifySecrets confirms exported values are real rather than masked,
	// returning the locations that are not, each with a reason.
	VerifySecrets(ctx context.Context, realmName string, secrets []manifest.Secret) (map[string]string, error)
	// DetectDependencies enumerates themes and provider JARs the realm
	// references but the export does not carry.
	DetectDependencies(ctx context.Context, realmName string, rep *realm.Representation) ([]manifest.Dependency, error)
}

// ExecutorFactory builds the adapter for an environment.
type ExecutorFactory func(env config.Environment) (target.Executor, error)

// StoreFactory builds the adapter for a storage definition.
type StoreFactory func(st config.Storage) (store.BlobStore, error)

// VerifierFactory builds the optional Admin API client. Returning nil, nil
// means this environment has no Admin API configured, which is a supported
// configuration rather than an error.
type VerifierFactory func(env config.Environment) (Verifier, error)

// Registry is how concrete adapters reach the orchestrator without the
// orchestrator importing any of them.
type Registry struct {
	Executor ExecutorFactory
	Store    StoreFactory
	Verifier VerifierFactory
	// Destination is the Admin API view of a restore target, used for the dry
	// run, the merge strategy and post-restore validation. A nil factory, or
	// one that returns an error, means those steps report themselves as not
	// performed rather than failing the restore.
	Destination DestinationFactory
}

// Options configures an orchestrator.
type Options struct {
	Home     config.Home
	Config   *config.Store
	Jobs     *config.JobStore
	Log      *obs.Logger
	Audit    *obs.AuditLog
	Registry Registry
	Version  string
	// Now is the clock, replaceable in tests.
	Now func() time.Time
}

// Orchestrator runs capture and restore jobs.
type Orchestrator struct {
	opts Options

	mu      sync.Mutex
	sink    obs.Sink
	running map[string]context.CancelFunc
	// tornDown makes teardown idempotent, so the deferred path and the
	// explicit one cannot double-destroy an execution context.
	tornDown map[string]bool
}

// New builds an orchestrator.
func New(opts Options) *Orchestrator {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Log == nil {
		opts.Log = obs.Discard()
	}
	return &Orchestrator{
		opts:     opts,
		sink:     obs.NopSink{},
		running:  map[string]context.CancelFunc{},
		tornDown: map[string]bool{},
	}
}

// SetSink points progress events at a destination.
func (o *Orchestrator) SetSink(s obs.Sink) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if s == nil {
		s = obs.NopSink{}
	}
	o.sink = s
}

func (o *Orchestrator) currentSink() obs.Sink {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.sink
}

// Cancel stops a running job. Cancellation propagates by context through the
// whole stack, and teardown still runs — a cancel destroys the ephemeral clone,
// it does not merely abandon the job.
func (o *Orchestrator) Cancel(jobID string) error {
	o.mu.Lock()
	cancel, ok := o.running[jobID]
	o.mu.Unlock()
	if !ok {
		return fmt.Errorf("job %s is not running", jobID)
	}
	cancel()
	return nil
}

// Running reports which jobs are in flight.
func (o *Orchestrator) Running() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]string, 0, len(o.running))
	for id := range o.running {
		out = append(out, id)
	}
	return out
}

func (o *Orchestrator) track(jobID string, cancel context.CancelFunc) func() {
	o.mu.Lock()
	o.running[jobID] = cancel
	o.mu.Unlock()
	return func() {
		o.mu.Lock()
		delete(o.running, jobID)
		o.mu.Unlock()
	}
}

// saveJob persists a job, logging rather than failing when it cannot.
//
// A job whose record could not be written is still a job that is running; the
// consequence is that resume will not be offered for it, which is worth a log
// line but not worth abandoning work an operator asked for.
func (o *Orchestrator) saveJob(j *config.Job) {
	if err := o.opts.Jobs.Save(j); err != nil {
		o.opts.Log.Error("the job record could not be written", "job", j.ID, "err", err)
	}
}

// fail records a terminal failure on a job in the shape the Activity and job
// outcome screens read.
func (o *Orchestrator) fail(j *config.Job, rep *obs.Reporter, phase obs.Phase, err error) error {
	now := o.opts.Now()
	j.EndedAt = &now
	j.Phase = string(phase)

	switch {
	case errors.Is(err, context.Canceled):
		j.State = config.JobCancelled
		j.Message = "Cancelled."
	case resil.IsRetryable(err):
		// A retryable failure is not the end of the job. Keeping the checkpoint
		// and calling it Interrupted is what turns a dropped connection into
		// seconds lost rather than the whole job.
		j.State = config.JobInterrupted
		j.EndedAt = nil
		j.Retryable = true
		j.Message = err.Error()
		j.Hint = resil.Hint(err)
	default:
		j.State = config.JobFailed
		j.Message = err.Error()
		j.Hint = resil.Hint(err)
	}

	j.Append(config.LedgerEntry{
		Phase: string(phase), Item: j.Realm, Attempts: 1,
		LastError: j.Message, Outcome: string(j.State),
		Retryable: j.Retryable, At: now,
	})
	o.saveJob(j)
	rep.FailPhase(phase, j.Message)
	rep.JobState(string(j.State), j.Message)
	return err
}

func (o *Orchestrator) complete(j *config.Job, rep *obs.Reporter, message string) {
	now := o.opts.Now()
	j.State = config.JobCompleted
	j.EndedAt = &now
	j.Message = message
	j.Checkpoint = nil
	o.saveJob(j)
	rep.JobState(string(j.State), message)
}

// newJob creates and persists a job record before any work starts, so a crash
// in the first second still leaves something the next launch can adopt.
func (o *Orchestrator) newJob(kind config.JobKind, id string) *config.Job {
	now := o.opts.Now()
	return &config.Job{
		ID:        id,
		Kind:      kind,
		State:     config.JobQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// reporterFor builds the progress reporter for one or more jobs.
//
// It exists because two things were true of every screen watching a run, and
// both made the tool look stuck when it was not.
//
// The first is that phases were announced to the event stream and nowhere else.
// A job record therefore never learned which phase it was in, so anything that
// re-read the job list — a poll, a reopened Activity screen, a relaunch after a
// crash — drew a pipeline with no live step in it. Every phase announcement is
// now written onto the record and persisted as it happens, so the live stream
// and a cold read of the same job agree.
//
// The second is that a batch of realms shares one probe and one clone, and
// those phases were reported under the first realm's job id alone. The other
// realms' cards sat blank through the slowest part of the run. Passing every
// job in the batch here fans each event out to all of them, so a shared phase
// is visible on every card it applies to.
func (o *Orchestrator) reporterFor(jobs ...*config.Job) *obs.Reporter {
	if len(jobs) == 0 {
		return obs.NewReporter("", o.currentSink())
	}
	return obs.NewReporter(jobs[0].ID, obs.SinkFunc(func(e obs.Event) {
		sink := o.currentSink()
		for _, j := range jobs {
			if o.recordPhase(j, e) {
				o.saveJob(j)
			}
			out := e
			out.JobID = j.ID
			sink.Emit(out)
		}
	}))
}

// recordPhase mirrors a phase announcement onto the job record, reporting
// whether anything actually changed so an unchanged record is not rewritten
// once per streamed log line.
func (o *Orchestrator) recordPhase(j *config.Job, e obs.Event) bool {
	switch e.Kind {
	case obs.EventPhaseStarted:
		if j.Phase == string(e.Phase) {
			return false
		}
		j.Phase = string(e.Phase)
		return true
	case obs.EventPhaseCompleted:
		before := len(j.CompletedPhases)
		j.CompletePhase(string(e.Phase))
		return len(j.CompletedPhases) != before
	default:
		return false
	}
}
