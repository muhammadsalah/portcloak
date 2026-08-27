package orchestrator_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/orchestrator"
	"portcloak/internal/engine/store"
	"portcloak/internal/engine/target"
	"portcloak/internal/engine/target/clone"
)

// cloneHarness drives a capture through the ephemeral-clone lifecycle, using
// the fake platform so the guarantees are asserted without a container runtime.
func cloneHarness(t *testing.T) (*harness, *clone.FakePlatform) {
	t.Helper()
	h := newHarness(t)
	p := clone.NewFakePlatform()

	// The fake platform serves the rich fixture through its exec and copy
	// hooks, so the pipeline sees a real export.
	p.ExecFunc = func(ctx context.Context, ref string, cmd target.Command) (target.ExecResult, error) {
		return target.ExecResult{ExitCode: 0, Stdout: "Export finished\n"}, nil
	}
	p.CopyOutFunc = func(ctx context.Context, ref, dir string, sink target.ArtifactSink) error {
		// A real export names its output after the realm, and the orchestrator
		// asks for one realm's directory at a time, so the fixture is served
		// under whichever realm was requested.
		realmName := filepath.Base(dir)
		entries, err := os.ReadDir(richFixture)
		if err != nil {
			return err
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		sort.Strings(names)
		for _, name := range names {
			f, err := os.Open(filepath.Join(richFixture, name))
			if err != nil {
				return err
			}
			st, _ := f.Stat()
			served := strings.Replace(name, "acme-", realmName+"-", 1)
			err = sink.Artifact(ctx, target.Artifact{Name: served, Size: st.Size()}, f)
			_ = f.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}

	exec := clone.NewExecutor(p)
	h.orc = orchestrator.New(orchestrator.Options{
		Home: h.home, Config: h.cfg, Jobs: h.jobs, Log: obs.Discard(), Audit: h.audit,
		Version: "0.0.1-test",
		Registry: orchestrator.Registry{
			Executor: func(config.Environment) (target.Executor, error) { return exec, nil },
			Store:    func(config.Storage) (store.BlobStore, error) { return h.blobs, nil },
		},
	})
	h.orc.SetSink(h.sink)
	return h, p
}

// The whole point of the clone: a capture completes and nothing is left behind.
func TestCapture_ThroughAnEphemeralClone(t *testing.T) {
	h, p := cloneHarness(t)

	jobs := h.capture(defaultRequest())
	if jobs[0].State != config.JobCompleted {
		t.Fatalf("capture through a clone ended %s: %s", jobs[0].State, jobs[0].Message)
	}
	if jobs[0].Provenance.ExecutionMode != string(target.ModeEphemeralClone) {
		t.Errorf("execution mode recorded as %q", jobs[0].Provenance.ExecutionMode)
	}
	if jobs[0].Provenance.CloneRef == "" {
		t.Error("the clone reference was not recorded in provenance")
	}
	if err := p.Leaked(); err != nil {
		t.Fatal(err)
	}
	if len(p.Created) != 1 {
		t.Errorf("%d clones were created for one realm", len(p.Created))
	}
}

// A pod inheriting the serving workload's labels would be picked up by the
// production Service and receive real user traffic into a container that serves
// nothing.
func TestLabelTrap_ACloneCarriesOnlyPortCloaksOwnLabels(t *testing.T) {
	h, p := cloneHarness(t)
	// The serving workload's labels, as they would arrive from a real cluster.
	p.Base.Labels = map[string]string{
		"app":                    "keycloak",
		"app.kubernetes.io/name": "keycloak",
		"release":                "prod",
	}

	h.capture(defaultRequest())

	if len(p.Created) == 0 {
		t.Fatal("no clone was created")
	}
	// The spec the platform was asked to create is the authoritative record of
	// what would have been applied.
	created := p.CreatedSpecs()
	if len(created) != 1 {
		t.Fatalf("%d clone specs were created", len(created))
	}
	for k := range created[0].Labels {
		if !strings.HasPrefix(k, "portcloak.io/") {
			t.Errorf("the clone would have carried the inherited label %q, which is how a Service routes real traffic into it", k)
		}
	}
	if created[0].Labels[target.LabelEphemeral] != "true" {
		t.Error("the clone lost the label the orphan sweep finds it by")
	}
}

// The clone is destroyed the moment the artifacts are out, before packaging and
// upload — which on a bad link can take a long time.
func TestCapture_CloneIsDestroyedBeforePackaging(t *testing.T) {
	h, _ := cloneHarness(t)

	h.capture(defaultRequest())

	var destroyedAt, packagedAt int
	for i, e := range h.sink.Events() {
		switch {
		case e.Kind == obs.EventCloneDestroyed:
			destroyedAt = i
		case e.Kind == obs.EventPhaseStarted && e.Phase == obs.PhasePackage:
			packagedAt = i
		}
	}
	if destroyedAt == 0 {
		t.Fatal("the clone was never reported as destroyed")
	}
	if packagedAt == 0 {
		t.Fatal("packaging never started")
	}
	if destroyedAt > packagedAt {
		t.Error("the clone was still alive while the bundle was being sealed and uploaded")
	}
}

// A teardown that fails is raised prominently with the clone's identifier, and
// recorded on the job so the operator can act on it.
func TestCapture_FailedTeardownIsRaisedNotSwallowed(t *testing.T) {
	h, p := cloneHarness(t)
	p.DestroyErr = errors.New("forbidden: cannot delete pods")

	jobs := h.capture(defaultRequest())

	var found bool
	for _, e := range jobs[0].Ledger {
		if e.Phase == string(obs.PhaseTeardown) && e.Outcome == "left behind" {
			found = true
			if e.Item == "" {
				t.Error("the ledger entry does not name the clone")
			}
		}
	}
	if !found {
		t.Fatalf("a failed teardown left no trace on the job: %+v", jobs[0].Ledger)
	}
}

// One clone serves every queued realm, and is destroyed once.
func TestCapture_MultiRealmSharesOneClone(t *testing.T) {
	h, p := cloneHarness(t)

	req := defaultRequest()
	req.Realms = []string{"acme", "partners"}
	jobs := h.capture(req)

	for _, j := range jobs {
		if j.State != config.JobCompleted {
			t.Fatalf("job for %s ended %s: %s", j.Realm, j.State, j.Message)
		}
	}
	if len(p.Created) != 1 {
		t.Errorf("%d clones were created for two realms — a clone is a parked execution context, not a per-realm resource", len(p.Created))
	}
	if len(p.Destroyed) != 1 {
		t.Errorf("destroy ran %d times", len(p.Destroyed))
	}
	if err := p.Leaked(); err != nil {
		t.Fatal(err)
	}
}

// Cancelling mid-export destroys the clone rather than abandoning it.
func TestCapture_CancelDestroysTheClone(t *testing.T) {
	h, p := cloneHarness(t)

	started := make(chan struct{})
	p.ExecFunc = func(ctx context.Context, ref string, cmd target.Command) (target.ExecResult, error) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-ctx.Done()
		return target.ExecResult{}, ctx.Err()
	}

	handle, err := h.orc.Capture(context.Background(), defaultRequest())
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if err := h.orc.Cancel(handle.JobIDs[0]); err != nil {
		t.Fatal(err)
	}
	jobs := h.waitForJobs(handle.JobIDs)

	if jobs[0].State != config.JobCancelled {
		t.Fatalf("job ended %s", jobs[0].State)
	}
	if err := p.Leaked(); err != nil {
		t.Fatalf("cancelling left a clone behind: %v", err)
	}
}

// A clone that could never be created must leave nothing behind and say why.
func TestCapture_CloneRefusedLeavesNothing(t *testing.T) {
	h, p := cloneHarness(t)
	p.CreateErr = errors.New("pods \"portcloak-x\" is forbidden: exceeded quota")

	jobs := h.capture(defaultRequest())
	if jobs[0].State == config.JobCompleted {
		t.Fatal("a capture completed without a clone")
	}
	if !strings.Contains(jobs[0].Message, "quota") {
		t.Errorf("the failure does not carry the cluster's own reason: %q", jobs[0].Message)
	}
	if err := p.Leaked(); err != nil {
		t.Fatal(err)
	}
	objects, _ := h.blobs.List(context.Background(), "")
	if len(objects) != 0 {
		t.Errorf("a failed capture left objects in storage: %+v", objects)
	}
}
