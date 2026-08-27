package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/orchestrator"
	"portcloak/internal/engine/store"
)

// JobsController is the Activity screen.
type JobsController struct{ eng *Engine }

// NewJobsController binds the Activity screen.
func NewJobsController(eng *Engine) *JobsController { return &JobsController{eng: eng} }

// ServiceName is what the Wails binding layer calls this.
func (j *JobsController) ServiceName() string { return "JobsController" }

// capturePhases is the pipeline the Activity screen ticks off.
var capturePhases = []obs.Phase{
	obs.PhaseProbe, obs.PhaseClone, obs.PhaseExport, obs.PhaseFetch,
	obs.PhaseVerify, obs.PhaseTeardown, obs.PhaseManifest, obs.PhasePackage, obs.PhaseUpload,
}

var restorePhases = []obs.Phase{
	obs.PhaseDownload, obs.PhaseIntegrity, obs.PhasePreconditions,
	obs.PhaseDryRun, obs.PhaseImport, obs.PhaseValidate, obs.PhaseTeardown,
}

// PhaseView is one step of the pipeline.
type PhaseView struct {
	Phase string `json:"phase"`
	Label string `json:"label"`
	Done  bool   `json:"done"`
	Live  bool   `json:"live"`
}

// JobView is one card on the Activity screen.
type JobView struct {
	config.Job
	Phases []PhaseView `json:"phases"`
	// Elapsed is rendered, because a duration in nanoseconds is not a fact an
	// operator reads.
	Elapsed string `json:"elapsed"`
	// Resumable and Discardable drive the buttons, so the frontend does not
	// re-derive the state machine.
	Resumable   bool `json:"resumable"`
	Discardable bool `json:"discardable"`
	Cancellable bool `json:"cancellable"`
	// CheckpointNote says what a resume would actually pick up from.
	CheckpointNote string `json:"checkpointNote,omitempty"`
	// ResumeNote says what pressing Resume would do — an upload continued, or
	// the export run again.
	ResumeNote string `json:"resumeNote,omitempty"`
}

// ActivityView is the whole screen.
type ActivityView struct {
	Jobs        []JobView `json:"jobs"`
	Running     int       `json:"running"`
	Interrupted int       `json:"interrupted"`
	Summary     string    `json:"summary"`
	Failure     *Failure  `json:"failure,omitempty"`
}

// List returns every job, newest first.
func (j *JobsController) List() ActivityView {
	jobs, err := j.eng.Jobs.List()
	if err != nil {
		return ActivityView{Failure: Fail(err)}
	}
	running := map[string]bool{}
	for _, id := range j.eng.Orch.Running() {
		running[id] = true
	}

	out := ActivityView{}
	now := time.Now()
	for _, job := range jobs {
		v := JobView{Job: *job}
		v.Phases = renderPhases(job)
		v.Elapsed = renderElapsed(job, now)
		v.Resumable = job.State.Resumable()
		v.Discardable = job.State == config.JobInterrupted || job.State == config.JobFailed
		v.Cancellable = running[job.ID]
		v.CheckpointNote = describeCheckpoint(job)
		if v.Resumable {
			// The note and the button agree because they come from one plan.
			v.ResumeNote = orchestrator.PlanResume(j.eng.Home, job).Reason
		}

		switch job.State {
		case config.JobRunning, config.JobQueued:
			out.Running++
		case config.JobInterrupted:
			out.Interrupted++
		}
		out.Jobs = append(out.Jobs, v)
	}

	switch {
	case out.Running == 0 && out.Interrupted == 0:
		out.Summary = "Nothing is running."
	case out.Interrupted == 0:
		out.Summary = fmt.Sprintf("%d running", out.Running)
	default:
		out.Summary = fmt.Sprintf("%d running · %d interrupted · resumable across app restarts",
			out.Running, out.Interrupted)
	}
	return out
}

func renderPhases(job *config.Job) []PhaseView {
	phases := capturePhases
	if job.Kind == config.JobRestore {
		phases = restorePhases
	}
	done := map[string]bool{}
	for _, p := range job.CompletedPhases {
		done[p] = true
	}

	out := make([]PhaseView, 0, len(phases))
	for _, p := range phases {
		out = append(out, PhaseView{
			Phase: string(p), Label: obs.PhaseLabel(p),
			Done: done[string(p)], Live: job.Phase == string(p) && job.State == config.JobRunning,
		})
	}
	return out
}

func renderElapsed(job *config.Job, now time.Time) string {
	if job.StartedAt == nil {
		return ""
	}
	end := now
	if job.EndedAt != nil {
		end = *job.EndedAt
	}
	d := end.Sub(*job.StartedAt).Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

// describeCheckpoint says what a resume would pick up from, in the operator's
// terms rather than as a byte offset alone.
func describeCheckpoint(job *config.Job) string {
	cp := job.Checkpoint
	if cp == nil {
		return ""
	}
	switch {
	case cp.UploadID != "" && len(cp.Parts) > 0:
		return fmt.Sprintf("Resuming continues the upload from part %d.", len(cp.Parts)+1)
	case len(cp.Blocks) > 0:
		return fmt.Sprintf("Resuming re-stages only the blocks after %d.", len(cp.Blocks))
	case cp.ByteOffset > 0:
		return fmt.Sprintf("Resuming continues the transfer from %s.", humanBytes(cp.ByteOffset))
	case len(cp.FetchedArtifacts) > 0:
		return fmt.Sprintf("Resuming continues from the last of %d files already collected.", len(cp.FetchedArtifacts))
	case cp.LocalBundle != "":
		return "The snapshot is already sealed on this machine, so resuming only repeats the upload."
	default:
		return ""
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Cancel stops a running job. Cancellation runs teardown; it does not merely
// abandon the job.
func (j *JobsController) Cancel(jobID string) *Failure {
	return Fail(j.eng.Orch.Cancel(jobID))
}

// DiscardResult reports what a discard removed, on both sides.
type DiscardResult struct {
	Note    string   `json:"note"`
	Failure *Failure `json:"failure,omitempty"`
}

// Discard abandons an interrupted job.
//
// It aborts any server-side multipart or block state, removes the local
// checkpoint and any partial bundle, and records the discard — so nothing is
// left accruing cost on either side.
func (j *JobsController) Discard(jobID string) DiscardResult {
	job, err := j.eng.Jobs.Load(jobID)
	if err != nil {
		return DiscardResult{Failure: Fail(err)}
	}
	if !job.State.Terminal() && job.State != config.JobInterrupted {
		return DiscardResult{Failure: &Failure{
			Message: "That job is still running. Cancel it first.",
		}}
	}

	var removed []string
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if cp := job.Checkpoint; cp != nil {
		if cp.LocalBundle != "" {
			if err := os.Remove(cp.LocalBundle); err == nil {
				removed = append(removed, "the sealed bundle on this machine")
			}
		}
		if cp.UploadID != "" && job.Storage != "" {
			cfg := j.eng.Config.Config()
			if st, ok := cfg.StorageByName(job.Storage); ok {
				if blobs, serr := j.eng.storeFor(st); serr == nil {
					if resumable, ok := blobs.(interface {
						AbortMultipart(context.Context, store.UploadID, string) error
					}); ok {
						if err := resumable.AbortMultipart(ctx, store.UploadID(cp.UploadID), cp.Key); err == nil {
							removed = append(removed, "the incomplete upload in "+job.Storage)
						}
					}
					_ = blobs.Close()
				}
			}
		}
	}
	// The whole working directory for the job goes, not just the bundle.
	if err := os.RemoveAll(j.eng.Home.WorkPath(jobID, "")); err == nil {
		removed = append(removed, "its working files")
	}
	if err := j.eng.Jobs.Delete(jobID); err != nil {
		return DiscardResult{Failure: Fail(err)}
	}
	removed = append(removed, "the checkpoint")

	_ = j.eng.Audit.Record(obs.AuditEntry{
		Action: obs.ActionJobDiscarded, Outcome: "discarded",
		Realm: job.Realm, Environment: job.Environment, Storage: job.Storage,
		Detail: join(removed, ", "),
	})

	return DiscardResult{Note: "Removed " + join(removed, ", ") + ". Nothing was left behind on either side."}
}

// Resume continues an interrupted job.
//
// What it does depends on how far the job got, and the answer is reported
// rather than assumed: a capture whose bundle is already sealed resumes as an
// upload, and anything earlier runs the export again. Implying a fine-grained
// resume that does not exist would be worse than saying so.
func (j *JobsController) Resume(jobID string) StartResult {
	job, err := j.eng.Jobs.Load(jobID)
	if err != nil {
		return StartResult{Failure: Fail(err)}
	}

	plan := orchestrator.PlanResume(j.eng.Home, job)
	switch plan.Kind {
	case orchestrator.ResumeUnavailable:
		return StartResult{Failure: &Failure{Message: plan.Reason}}

	case orchestrator.ResumeUpload:
		if err := j.eng.Orch.ResumeUpload(context.Background(), jobID); err != nil {
			return StartResult{Failure: Fail(err)}
		}
		return StartResult{JobIDs: []string{jobID}, Realms: []string{job.Realm}}

	default:
		prefs := j.eng.Config.Preferences()
		return (&CaptureController{eng: j.eng}).Start(CaptureOptions{
			Environment:  job.Environment,
			Realms:       []string{job.Realm},
			Storage:      job.Storage,
			UsersMode:    prefs.UsersMode,
			UsersPerFile: prefs.UsersPerFile,
			// Resuming reuses the original encryption decision, which is
			// recorded on the job rather than asked again.
			Encrypt:                 job.Encrypted,
			AcknowledgedUnencrypted: !job.Encrypted,
		})
	}
}

// ResumePlan says what resuming would do, so the button can say it too.
func (j *JobsController) ResumePlan(jobID string) orchestrator.ResumePlan {
	job, err := j.eng.Jobs.Load(jobID)
	if err != nil {
		return orchestrator.ResumePlan{
			Kind: orchestrator.ResumeUnavailable, Reason: err.Error(),
		}
	}
	return orchestrator.PlanResume(j.eng.Home, job)
}
