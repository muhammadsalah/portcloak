package orchestrator_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/orchestrator"
	"portcloak/internal/engine/store"
	"portcloak/internal/engine/target"
)

// The defining case: a capture interrupted during upload resumes and converges
// on an object byte-identical to one written by an uninterrupted run.
func TestResume_ConvergesOnTheSameObject(t *testing.T) {
	h := newHarness(t)

	// An upload that drops partway leaves the sealed bundle on this machine and
	// a checkpoint describing how far it got.
	failing := &failingStore{inner: mustDisk(t, h.storageRoot)}
	h.orc = orchestrator.New(orchestrator.Options{
		Home: h.home, Config: h.cfg, Jobs: h.jobs, Log: obs.Discard(), Audit: h.audit,
		Version: "0.0.1-test",
		Registry: orchestrator.Registry{
			Executor: func(config.Environment) (target.Executor, error) { return h.exec, nil },
			Store:    func(config.Storage) (store.BlobStore, error) { return failing, nil },
		},
	})
	h.orc.SetSink(h.sink)

	handle, err := h.orc.Capture(context.Background(), defaultRequest())
	if err != nil {
		t.Fatal(err)
	}
	j := h.waitForJobs(handle.JobIDs)[0]

	if j.State != config.JobInterrupted {
		t.Fatalf("a dropped upload ended as %s, want interrupted: %s", j.State, j.Message)
	}
	if j.Checkpoint == nil || j.Checkpoint.LocalBundle == "" {
		t.Fatalf("the checkpoint does not name the sealed bundle: %+v", j.Checkpoint)
	}

	// The sealed bundle is what an uninterrupted upload would have written, so
	// it is the thing the resumed object has to converge on. Comparing against
	// a separate capture would compare two different bundles: the envelope
	// carries a capture timestamp.
	want, err := os.ReadFile(j.Checkpoint.LocalBundle)
	if err != nil {
		t.Fatalf("the sealed bundle was not retained: %v", err)
	}
	wantDigest := sha256.Sum256(want)
	if hex.EncodeToString(wantDigest[:]) != j.Checkpoint.Digest {
		t.Fatal("the checkpoint's digest does not describe the bundle it points at")
	}

	// The plan says what a resume will do before it does it.
	plan := orchestrator.PlanResume(h.home, j)
	if plan.Kind != orchestrator.ResumeUpload {
		t.Fatalf("resume planned as %q: %s", plan.Kind, plan.Reason)
	}
	if !strings.Contains(plan.Reason, "only repeats the upload") {
		t.Errorf("the plan does not say what it will do: %q", plan.Reason)
	}

	// Resume against a storage that works this time.
	working := mustDisk(t, h.storageRoot)
	h.orc = orchestrator.New(orchestrator.Options{
		Home: h.home, Config: h.cfg, Jobs: h.jobs, Log: obs.Discard(), Audit: h.audit,
		Version: "0.0.1-test",
		Registry: orchestrator.Registry{
			Executor: func(config.Environment) (target.Executor, error) { return h.exec, nil },
			Store:    func(config.Storage) (store.BlobStore, error) { return working, nil },
		},
	})
	h.exec.Reset()

	if err := h.orc.ResumeUpload(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	resumed := h.waitForJobs([]string{j.ID})[0]

	if resumed.State != config.JobCompleted {
		t.Fatalf("the resumed job ended %s: %s", resumed.State, resumed.Message)
	}
	// The export did not run again: the expensive half was already done.
	if h.exec.RunCount() != 0 {
		t.Errorf("the resume re-ran the export %d times", h.exec.RunCount())
	}

	got, err := os.ReadFile(filepath.Join(h.storageRoot, filepath.FromSlash(resumed.StorageKey)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the resumed object is %d bytes and the sealed bundle %d — a resume must converge, never concatenate or truncate",
			len(got), len(want))
	}

	// Nothing is left behind on either side.
	if resumed.Checkpoint != nil {
		t.Error("a completed resume kept its checkpoint")
	}
	if _, err := os.Stat(j.Checkpoint.LocalBundle); !os.IsNotExist(err) {
		t.Error("the sealed local copy outlived the upload")
	}

	// Both sidecars are in place, so the snapshot lists properly rather than as
	// a bundle needing a deeper read.
	objects, err := working.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	triplets, _ := store.Group(store.NewLayout(""), objects)
	if len(triplets) != 1 {
		t.Fatalf("storage holds %d snapshots after a resume", len(triplets))
	}
	if triplets[0].Manifest == nil || triplets[0].Digest == nil {
		t.Error("a resumed upload left the snapshot without its sidecars")
	}
}

// A checkpoint describing a bundle that is no longer there would resume into a
// gap, so it restarts instead — and says so.
func TestResume_RestartsWhenTheSealedBundleIsGone(t *testing.T) {
	h := newHarness(t)
	j := &config.Job{
		ID: "01HZY3", Kind: config.JobCapture, State: config.JobInterrupted,
		Realm: "acme", Storage: "local-disk", Environment: "laptop",
		Checkpoint: &config.Checkpoint{
			Stage: "upload", LocalBundle: filepath.Join(t.TempDir(), "gone.pck"),
		},
	}
	plan := orchestrator.PlanResume(h.home, j)
	if plan.Kind != orchestrator.ResumeRestart {
		t.Fatalf("resume planned as %q", plan.Kind)
	}
	if !strings.Contains(plan.Reason, "no longer on this machine") {
		t.Errorf("the reason does not explain the restart: %q", plan.Reason)
	}

	// And a job that never got as far as sealing restarts too.
	j.Checkpoint = &config.Checkpoint{Stage: "fetch", FetchedArtifacts: []string{"a", "b"}}
	if got := orchestrator.PlanResume(h.home, j); got.Kind != orchestrator.ResumeRestart {
		t.Errorf("an interrupted fetch planned as %q", got.Kind)
	}
}

// A restore is not resumed automatically: Keycloak's import is not
// transactional, so replaying one is not always safe.
func TestResume_RestoreIsNotResumedAutomatically(t *testing.T) {
	h := newHarness(t)
	j := &config.Job{
		ID: "01HZY4", Kind: config.JobRestore, State: config.JobInterrupted, Realm: "acme",
	}
	plan := orchestrator.PlanResume(h.home, j)
	if plan.Kind != orchestrator.ResumeUnavailable {
		t.Fatalf("a restore planned as %q", plan.Kind)
	}
	if !strings.Contains(plan.Reason, "not transactional") {
		t.Errorf("the reason does not say why: %q", plan.Reason)
	}
}

// A job that already finished has nothing to resume.
func TestResume_TerminalJobsAreNotOffered(t *testing.T) {
	h := newHarness(t)
	for _, state := range []config.JobState{config.JobCompleted, config.JobFailed, config.JobCancelled} {
		j := &config.Job{ID: "x", Kind: config.JobCapture, State: state}
		if got := orchestrator.PlanResume(h.home, j); got.Kind != orchestrator.ResumeUnavailable {
			t.Errorf("a %s job planned as %q", state, got.Kind)
		}
	}
}
