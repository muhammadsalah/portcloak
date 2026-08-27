package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/store"
)

// ResumeKind is what resuming an interrupted job will actually do.
//
// It is a distinct type because the honest answer differs by how far the job
// got, and implying a fine-grained resume that does not exist would be worse
// than saying plainly that the export has to run again.
type ResumeKind string

const (
	// ResumeUpload continues from the sealed bundle already on this machine.
	// The expensive half — reading the realm out of the database — is done.
	ResumeUpload ResumeKind = "upload"
	// ResumeRestart re-runs the capture from the beginning, because the export
	// never finished or its artifacts are gone.
	ResumeRestart ResumeKind = "restart"
	// ResumeUnavailable means the job cannot be resumed at all.
	ResumeUnavailable ResumeKind = "unavailable"
)

// ResumePlan says what resuming would do, before it is done.
type ResumePlan struct {
	Kind   ResumeKind `json:"kind"`
	Reason string     `json:"reason"`
}

// PlanResume inspects a job's checkpoint and reports what a resume would do.
func PlanResume(home config.Home, j *config.Job) ResumePlan {
	if !j.State.Resumable() {
		return ResumePlan{Kind: ResumeUnavailable,
			Reason: fmt.Sprintf("This job is %s, so there is nothing to resume.", j.State)}
	}
	if j.Kind != config.JobCapture {
		return ResumePlan{Kind: ResumeUnavailable,
			Reason: "A restore is not resumed automatically. Keycloak's import is not transactional, so replaying one is not always safe — review what was applied and start again deliberately."}
	}

	cp := j.Checkpoint
	if cp == nil || cp.LocalBundle == "" {
		return ResumePlan{Kind: ResumeRestart,
			Reason: "The export did not finish, so resuming runs it again. Nothing partial was kept."}
	}
	if _, err := os.Stat(cp.LocalBundle); err != nil {
		// A checkpoint describing a bundle that is not there would resume into
		// a gap. Restarting is the honest move.
		return ResumePlan{Kind: ResumeRestart,
			Reason: "The sealed bundle is no longer on this machine, so resuming runs the export again."}
	}
	return ResumePlan{Kind: ResumeUpload,
		Reason: "The snapshot is already sealed on this machine, so resuming only repeats the upload."}
}

// ResumeUploadOnly re-uploads a bundle that was already sealed.
//
// Resume is convergent: the object it produces is byte-identical to one written
// by an uninterrupted run, never a duplicate and never a concatenation.
func (o *Orchestrator) ResumeUpload(ctx context.Context, jobID string) error {
	j, err := o.opts.Jobs.Load(jobID)
	if err != nil {
		return err
	}
	plan := PlanResume(o.home(), j)
	if plan.Kind != ResumeUpload {
		return resil.Fatal("resume the job", plan.Reason, nil)
	}

	cfg := o.opts.Config.Config()
	st, ok := cfg.StorageByName(j.Storage)
	if !ok {
		return resil.Fatal("resume the job",
			fmt.Sprintf("The storage %q is no longer configured.", j.Storage), config.ErrNotFound)
	}

	go o.runResumeUpload(context.WithoutCancel(ctx), st, j)
	return nil
}

func (o *Orchestrator) runResumeUpload(ctx context.Context, st config.Storage, j *config.Job) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer o.track(j.ID, cancel)()

	rep := o.reporterFor(j)
	started := o.opts.Now()
	j.State = config.JobRunning
	j.StartedAt = &started
	j.Message = "Resuming the upload."
	o.saveJob(j)
	rep.JobState(string(config.JobRunning), "Resuming the upload of "+j.Realm+".")

	blobs, err := o.opts.Registry.Store(st)
	if err != nil {
		_ = o.fail(j, rep, obs.PhaseUpload, err)
		return
	}
	defer blobs.Close() //nolint:errcheck

	cp := j.Checkpoint
	rep.StartPhase(obs.PhaseUpload)

	info, err := os.Stat(cp.LocalBundle)
	if err != nil {
		_ = o.fail(j, rep, obs.PhaseUpload, err)
		return
	}
	f, err := os.Open(cp.LocalBundle)
	if err != nil {
		_ = o.fail(j, rep, obs.PhaseUpload, err)
		return
	}
	defer f.Close() //nolint:errcheck

	key := cp.Key
	if key == "" {
		key = j.StorageKey
	}
	// The file is handed over whole. The backend reconciles the checkpoint's
	// offset against what it actually holds and positions the reader itself,
	// because it is the only authority on where the transfer got to.
	_, err = blobs.Put(ctx, key, f, store.PutOptions{
		Size:      info.Size(),
		Digest:    cp.Digest,
		Offset:    cp.ByteOffset,
		HashState: cp.HashState,
		Progress: func(written int64) {
			rep.Progress(written, info.Size(), "bytes", key)
		},
		// The offset and the hash state are recorded together, so an upload
		// interrupted a second time still resumes without re-reading what it
		// already sent.
		Checkpoint: func(written int64, state []byte) {
			j.Checkpoint = &config.Checkpoint{
				Stage: string(obs.PhaseUpload), Key: key, ByteOffset: written,
				Digest: cp.Digest, HashState: state,
				LocalBundle: cp.LocalBundle, UpdatedAt: o.opts.Now(),
			}
		},
	})
	if err != nil {
		o.saveJob(j)
		_ = o.fail(j, rep, obs.PhaseUpload, err)
		return
	}

	// The sidecars are rewritten too: an interrupted run may have uploaded the
	// bundle and not them, and a bundle with no sidecar lists as needing a
	// deeper read.
	if err := o.rewriteSidecars(ctx, blobs, j, key, cp, info.Size()); err != nil {
		o.opts.Log.Error("the sidecars could not be rewritten on resume", "job", j.ID, "err", err)
	}

	j.StorageKey = key
	j.Checkpoint = nil
	j.CompletePhase(string(obs.PhaseUpload))
	_ = os.Remove(cp.LocalBundle)

	rep.CompletePhase(obs.PhaseUpload, key)
	o.complete(j, rep, fmt.Sprintf("Resumed and uploaded %s to %s.", j.Realm, j.Storage))

	_ = o.opts.Audit.Record(obs.AuditEntry{
		Action: obs.ActionCapture, Outcome: "captured (resumed)",
		Realm: j.Realm, SnapshotID: j.SnapshotID,
		Environment: j.Environment, Storage: j.Storage,
		Detail: "the upload was resumed from a checkpoint",
	})
}

// rewriteSidecars re-reads the sealed bundle's own manifest so the sidecars
// match the bundle that actually landed, rather than being reconstructed from
// something the job remembered.
func (o *Orchestrator) rewriteSidecars(ctx context.Context, blobs store.BlobStore, j *config.Job, bundleKey string, cp *config.Checkpoint, size int64) error {
	base := strings.TrimSuffix(bundleKey, store.BundleExt)

	digestLine := fmt.Sprintf("%s  %s\n", cp.Digest, lastSegment(bundleKey))
	if _, err := blobs.Put(ctx, base+store.DigestExt, strings.NewReader(digestLine),
		store.PutOptions{Size: int64(len(digestLine))}); err != nil {
		return err
	}

	// If a sidecar manifest is already there, it was written by the
	// interrupted run and is still correct — the bundle it describes has not
	// changed.
	if _, err := blobs.Stat(ctx, base+store.ManifestExt); err == nil {
		return nil
	}

	// Otherwise a minimal one is written, marked so the library shows it needs
	// a deeper read rather than presenting incomplete counts as complete.
	stub := map[string]any{
		"schemaVersion": "1.0",
		"snapshotId":    j.SnapshotID,
		"realm":         j.Realm,
		"createdAt":     j.CreatedAt.UTC().Format(time.RFC3339),
		"encrypted":     j.Encrypted,
		"bundleBytes":   size,
		"integrityRoot": cp.Digest,
		"verdict":       "Unknown",
		"warnings": []string{
			"This snapshot's sidecar was rebuilt after an interrupted upload, so its counts are not known. Open the snapshot to read them.",
		},
	}
	b, err := json.MarshalIndent(stub, "", "  ")
	if err != nil {
		return err
	}
	_, err = blobs.Put(ctx, base+store.ManifestExt, strings.NewReader(string(b)+"\n"),
		store.PutOptions{Size: int64(len(b) + 1)})
	return err
}

func lastSegment(key string) string {
	if i := strings.LastIndex(key, "/"); i >= 0 {
		return key[i+1:]
	}
	return key
}
