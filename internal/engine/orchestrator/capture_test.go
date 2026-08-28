// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package orchestrator_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/crypto"
	"portcloak/internal/engine/manifest"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/orchestrator"
	"portcloak/internal/engine/realm"
	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/snapshot"
	"portcloak/internal/engine/store"
	"portcloak/internal/engine/store/disk"
	"portcloak/internal/engine/target"
	"portcloak/internal/engine/target/faketarget"
)

const richFixture = "../../../testdata/exports/rich"

type harness struct {
	t     *testing.T
	home  config.Home
	cfg   *config.Store
	jobs  *config.JobStore
	orc   *orchestrator.Orchestrator
	exec  *faketarget.Executor
	blobs *disk.Store
	sink  *obs.RecordingSink
	audit *obs.AuditLog
	// storageRoot is the disk folder snapshots land in.
	storageRoot string
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	home := config.Home{Root: t.TempDir()}
	if err := home.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	cfg := config.NewStore(home)
	if err := cfg.Load(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.AddEnvironment(config.Environment{
		Name: "laptop", Kind: config.EnvLocal, ServerFolder: "/opt/keycloak",
	}); err != nil {
		t.Fatal(err)
	}
	storageRoot := t.TempDir()
	if err := cfg.AddStorage(config.Storage{
		Name: "local-disk", Kind: config.StoreDisk, Folder: storageRoot,
	}); err != nil {
		t.Fatal(err)
	}

	blobs, err := disk.New(storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	audit, err := obs.NewAuditLog(home.AuditFile())
	if err != nil {
		t.Fatal(err)
	}

	h := &harness{
		t: t, home: home, cfg: cfg,
		jobs:        config.NewJobStore(home),
		exec:        faketarget.New(richFixture),
		blobs:       blobs,
		sink:        &obs.RecordingSink{},
		audit:       audit,
		storageRoot: storageRoot,
	}
	h.orc = orchestrator.New(orchestrator.Options{
		Home: home, Config: cfg, Jobs: h.jobs, Log: obs.Discard(), Audit: audit,
		Version: "0.0.1-test",
		Registry: orchestrator.Registry{
			Executor: func(config.Environment) (target.Executor, error) { return h.exec, nil },
			Store:    func(config.Storage) (store.BlobStore, error) { return h.blobs, nil },
		},
	})
	h.orc.SetSink(h.sink)
	return h
}

// waitForJobs blocks until every job reaches a terminal or interrupted state.
func (h *harness) waitForJobs(ids []string) []*config.Job {
	h.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		out := make([]*config.Job, 0, len(ids))
		settled := true
		for _, id := range ids {
			j, err := h.jobs.Load(id)
			if err != nil {
				settled = false
				break
			}
			if !j.State.Terminal() && j.State != config.JobInterrupted {
				settled = false
			}
			out = append(out, j)
		}
		// The batch also has to have finished: teardown, the audit entry and
		// the final save all happen after the last job reaches its state.
		if settled && len(out) == len(ids) && len(h.orc.Running()) == 0 {
			return out
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("jobs did not settle: %v", ids)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForResume waits for a resumed job, which waitForJobs alone cannot do.
//
// ResumeUpload starts a goroutine and returns, so for a moment afterwards the
// job on disk is still the interrupted one and the orchestrator is not yet
// tracking it as running. waitForJobs counts interrupted as settled — it has
// to, because that is how a dropped upload legitimately ends — and so returns
// the job as it was before the resume, and the assertions that follow read the
// previous attempt's state and message. Under coverage instrumentation that
// window is wide enough to hit every time.
//
// So: first wait for the resume to be picked up, then wait for it to finish.
func (h *harness) waitForResume(id string) *config.Job {
	h.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		j, err := h.jobs.Load(id)
		if err == nil && j.State != config.JobInterrupted {
			break
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("the resume of %s never started", id)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return h.waitForJobs([]string{id})[0]
}

func (h *harness) capture(req orchestrator.CaptureRequest) []*config.Job {
	h.t.Helper()
	handle, err := h.orc.Capture(context.Background(), req)
	if err != nil {
		h.t.Fatal(err)
	}
	return h.waitForJobs(handle.JobIDs)
}

func defaultRequest() orchestrator.CaptureRequest {
	return orchestrator.CaptureRequest{
		Environment: "laptop",
		Realms:      []string{"acme"},
		Storage:     "local-disk",
	}
}

func TestCapture_Local_ProducesASealedBundleAndBothSidecars(t *testing.T) {
	h := newHarness(t)
	jobs := h.capture(defaultRequest())

	if len(jobs) != 1 {
		t.Fatalf("got %d jobs", len(jobs))
	}
	j := jobs[0]
	if j.State != config.JobCompleted {
		t.Fatalf("job ended %s: %s", j.State, j.Message)
	}

	objects, err := h.blobs.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	snapshots, foreign := store.Group(store.NewLayout(""), objects)
	if len(foreign) != 0 {
		t.Errorf("unexpected objects in storage: %+v", foreign)
	}
	if len(snapshots) != 1 {
		t.Fatalf("got %d snapshots in storage: %+v", len(snapshots), objects)
	}
	s := snapshots[0]
	if s.Bundle == nil || s.Manifest == nil || s.Digest == nil {
		t.Fatalf("the bundle and both sidecars should be side by side: %+v", s)
	}
	if s.Realm != "acme" {
		t.Errorf("realm partition is %q", s.Realm)
	}

	// The checkpoint is cleared on success, so nothing offers to resume a job
	// that is finished.
	if j.Checkpoint != nil {
		t.Error("a completed job kept a checkpoint")
	}
	// The sealed local copy holds the same unmasked secrets as the bundle, so
	// it must not linger.
	if entries, _ := os.ReadDir(h.home.WorkPath(j.ID, "")); len(entries) > 0 {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), store.BundleExt) {
				t.Errorf("the sealed local copy was left behind: %s", e.Name())
			}
		}
	}
}

// The sidecar is what makes the library browsable with no key at all.
func TestCapture_SidecarIsReadableWithoutAnyKey(t *testing.T) {
	h := newHarness(t)
	req := defaultRequest()
	req.Encryption = crypto.Config{
		Enabled: true, Mode: snapshot.EncryptionPassphrase, Passphrase: "a passphrase",
	}
	jobs := h.capture(req)
	if jobs[0].State != config.JobCompleted {
		t.Fatalf("job ended %s: %s", jobs[0].State, jobs[0].Message)
	}

	sidecar := h.readSidecar(t)
	if sidecar.Realm != "acme" {
		t.Fatalf("sidecar realm = %q", sidecar.Realm)
	}
	if !sidecar.Encrypted || sidecar.EncryptionMode != string(snapshot.EncryptionPassphrase) {
		t.Errorf("sidecar did not record the encryption: %+v", sidecar)
	}
	if sidecar.Counts.Users != 5 || sidecar.Counts.Clients != 4 {
		t.Errorf("sidecar counts = %+v", sidecar.Counts)
	}
	if !sidecar.TokenContinuity {
		t.Error("sidecar should surface token continuity without a key")
	}

	// The one artifact deliberately written in the clear is the one most likely
	// to leak by accident.
	raw := h.readSidecarBytes(t)
	for _, forbidden := range []string{"app-web-real-secret", "ldap-bind-password", "j.doe", "a passphrase"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("the sidecar contains %q", forbidden)
		}
	}
}

func (h *harness) readSidecarBytes(t *testing.T) []byte {
	t.Helper()
	objects, err := h.blobs.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objects {
		if strings.HasSuffix(o.Key, store.ManifestExt) {
			b, err := os.ReadFile(filepath.Join(h.storageRoot, filepath.FromSlash(o.Key)))
			if err != nil {
				t.Fatal(err)
			}
			return b
		}
	}
	t.Fatal("no sidecar manifest was written")
	return nil
}

func (h *harness) readSidecar(t *testing.T) manifest.Sidecar {
	t.Helper()
	var s manifest.Sidecar
	if err := json.Unmarshal(h.readSidecarBytes(t), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// Capturing several realms produces several independent snapshots, not one
// multi-realm bundle.
func TestCapture_MultiRealm_ProducesOneBundlePerRealm(t *testing.T) {
	h := newHarness(t)
	h.exec.PerRealm = map[string]string{"acme": richFixture, "partners": richFixture}

	req := defaultRequest()
	req.Realms = []string{"acme", "partners"}
	jobs := h.capture(req)

	for _, j := range jobs {
		if j.State != config.JobCompleted {
			t.Fatalf("job for %s ended %s: %s", j.Realm, j.State, j.Message)
		}
	}
	objects, _ := h.blobs.List(context.Background(), "")
	snapshots, _ := store.Group(store.NewLayout(""), objects)
	if len(snapshots) != 2 {
		t.Fatalf("got %d snapshots, want one per realm", len(snapshots))
	}
	realms := map[string]bool{}
	for _, s := range snapshots {
		realms[s.Realm] = true
	}
	if !realms["acme"] || !realms["partners"] {
		t.Fatalf("the realms did not partition storage: %v", realms)
	}

	// One clone, reused: it is a parked execution context, not a per-realm
	// resource.
	if h.exec.Teardowns != 1 {
		t.Errorf("teardown ran %d times for a two-realm run, want once", h.exec.Teardowns)
	}
}

// There is no shared bundle to corrupt, so partial success is genuinely
// partial: N-1 valid snapshots plus one failed job.
func TestCapture_MultiRealm_PartialFailure(t *testing.T) {
	h := newHarness(t)
	// The middle realm does not exist on the target.
	h.exec.ExportDir = ""
	h.exec.PerRealm = map[string]string{"acme": richFixture, "partners": richFixture}

	req := defaultRequest()
	req.Realms = []string{"acme", "missing", "partners"}
	jobs := h.capture(req)

	byRealm := map[string]*config.Job{}
	for _, j := range jobs {
		byRealm[j.Realm] = j
	}
	if byRealm["acme"].State != config.JobCompleted {
		t.Errorf("acme should have completed: %s", byRealm["acme"].Message)
	}
	if byRealm["partners"].State != config.JobCompleted {
		t.Errorf("partners should have completed despite an earlier failure: %s", byRealm["partners"].Message)
	}
	failed := byRealm["missing"]
	if failed.State != config.JobFailed {
		t.Fatalf("the missing realm ended %s", failed.State)
	}
	if !strings.Contains(strings.ToLower(failed.Message), "realm") {
		t.Errorf("the failure does not say plainly what happened: %q", failed.Message)
	}

	objects, _ := h.blobs.List(context.Background(), "")
	snapshots, _ := store.Group(store.NewLayout(""), objects)
	if len(snapshots) != 2 {
		t.Fatalf("got %d snapshots, want the two that succeeded", len(snapshots))
	}
}

// Teardown is a guarantee, not a courtesy: a clone left running carries the
// same database credentials as the serving instance.
func TestCapture_TeardownRunsOnEveryExitPath(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(*harness)
	}{
		{"success", func(*harness) {}},
		{"export fails", func(h *harness) {
			h.exec.RunFunc = func(context.Context, target.Command) (target.ExecResult, error) {
				return target.ExecResult{ExitCode: 1, Stderr: "ERROR: Realm 'acme' not found"}, nil
			}
		}},
		{"fetch fails", func(h *harness) {
			h.exec.FetchErr = errors.New("the connection dropped")
		}},
		{"run errors outright", func(h *harness) {
			h.exec.RunFunc = func(context.Context, target.Command) (target.ExecResult, error) {
				return target.ExecResult{}, errors.New("the process could not be started")
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.exec.CloneRef = "container/portcloak-test"
			c.break_(h)

			h.capture(defaultRequest())

			if !h.exec.WasTornDown() {
				t.Fatalf("teardown did not run on the %q path — a clone would have been left running", c.name)
			}
		})
	}
}

// Cancel destroys the clone; it does not merely abandon the job.
func TestCapture_CancelRunsTeardown(t *testing.T) {
	h := newHarness(t)
	h.exec.CloneRef = "job/portcloak-cancel"

	started := make(chan struct{})

	// `release` is a leak guard, not part of the flow being tested. Cancelling
	// is what has to unblock the command, and that is the whole assertion.
	//
	// It used to be closed immediately after Cancel, which left the select
	// below with both cases ready — and Go chooses uniformly at random among
	// ready cases. Roughly half the time the command returned success instead
	// of the cancellation error, the capture carried on to fail somewhere
	// else, and the job ended `failed` rather than `cancelled`. That is a coin
	// toss, so it passed locally and lost on CI often enough to matter.
	//
	// Closing it from Cleanup keeps the goroutine from outliving a test that
	// fails before cancellation propagates, without racing the thing under
	// test.
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	h.exec.RunFunc = func(ctx context.Context, cmd target.Command) (target.ExecResult, error) {
		close(started)
		select {
		case <-ctx.Done():
			return target.ExecResult{}, ctx.Err()
		case <-release:
			return target.ExecResult{ExitCode: 0}, nil
		}
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
		t.Fatalf("job ended %s, want cancelled", jobs[0].State)
	}
	if !h.exec.WasTornDown() {
		t.Fatal("cancelling left the clone running")
	}
	objects, _ := h.blobs.List(context.Background(), "")
	if len(objects) != 0 {
		t.Errorf("a cancelled capture left objects in storage: %+v", objects)
	}
}

// A storage marked encryption-required cannot receive a plaintext bundle, and
// the check lives in the engine so a hand-edited config cannot bypass it.
func TestCapture_EncryptionRequiredStorageRejectsPlaintext(t *testing.T) {
	h := newHarness(t)
	if err := h.cfg.SaveStorage("local-disk", config.Storage{
		Name: "local-disk", Kind: config.StoreDisk, Folder: h.storageRoot,
		Default: true, EncryptionRequired: true,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := h.orc.Capture(context.Background(), defaultRequest())
	if err == nil {
		t.Fatal("an unencrypted capture to an encryption-required storage was accepted")
	}
	if !strings.Contains(err.Error(), "requires encryption") {
		t.Errorf("unhelpful message: %v", err)
	}
}

// The capture path invokes offline kc.sh export only. The Admin API is
// optional, and a capture must succeed without it.
func TestCapture_SucceedsWithoutAdminAPI(t *testing.T) {
	h := newHarness(t)
	req := defaultRequest()
	req.Verify = true
	req.DetectDependencies = true

	jobs := h.capture(req)
	if jobs[0].State != config.JobCompleted {
		t.Fatalf("a capture with no Admin API failed: %s", jobs[0].Message)
	}

	sidecar := h.readSidecar(t)
	if sidecar.Source.SecretVerification != "skipped" {
		t.Errorf("verification recorded as %q, want skipped", sidecar.Source.SecretVerification)
	}
}

// The invocation carries the options this kc.sh said it takes, and only those.
// It used to carry --http-port and --https-port unconditionally, which no
// Keycloak accepts on export: the command exits before it reads the realm.
func TestCapture_UsesOfflineExportWithTheOptionsKcAccepts(t *testing.T) {
	h := newHarness(t)
	h.capture(defaultRequest())

	cmd, ok := h.exec.LastCommand()
	if !ok {
		t.Fatal("no command was run")
	}
	joined := strings.Join(cmd.Args, " ")
	for _, want := range []string{"export", "--realm acme", "--users different_files", "--http-management-port"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the export invocation is missing %q: %s", want, joined)
		}
	}
	for _, unwanted := range []string{"--http-port", "--https-port"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("%q is not an export option on any Keycloak: %s", unwanted, joined)
		}
	}
}

// Where kc.sh takes no port option — 24.0 and 26.5 of the versions measured —
// none is passed, and a bind conflict is reported rather than retried, because
// reallocating cannot change anything the command will see.
func TestCapture_PassesNoPortWhereKcAcceptsNone(t *testing.T) {
	h := newHarness(t)
	h.exec.HelpOutput = "Options:\n\n--dir <dir>          Where to write.\n--realm <realm>      What to export.\n--users <strategy>   How.\n"

	h.capture(defaultRequest())

	cmd, ok := h.exec.LastCommand()
	if !ok {
		t.Fatal("no command was run")
	}
	if joined := strings.Join(cmd.Args, " "); strings.Contains(joined, "-port") {
		t.Errorf("a port option was passed to a kc.sh that does not take one: %s", joined)
	}
}

// Where the question itself fails, nothing is guessed at. A rejected option
// fails every capture; a missing one risks a conflict only if something is
// listening.
func TestCapture_PassesNoPortWhenTheOptionsCannotBeAsked(t *testing.T) {
	h := newHarness(t)
	h.exec.HelpExitCode = 1

	jobs := h.capture(defaultRequest())
	if jobs[0].State != config.JobCompleted {
		t.Fatalf("a capture failed because kc.sh would not list its options: %s", jobs[0].Message)
	}
	cmd, _ := h.exec.LastCommand()
	if joined := strings.Join(cmd.Args, " "); strings.Contains(joined, "-port") {
		t.Errorf("a port option was guessed at: %s", joined)
	}
}

// A rejected option is reported as itself. Before the classifier learned this
// wording it fell through to "kc.sh export exited with code 2", which says
// nothing about the one thing that has to change.
func TestCapture_ReportsARejectedOptionByName(t *testing.T) {
	h := newHarness(t)
	h.exec.RunFunc = func(ctx context.Context, cmd target.Command) (target.ExecResult, error) {
		return target.ExecResult{
			ExitCode: 2,
			Stderr:   "Option: '--http-port' not valid for command export\n",
		}, nil
	}

	jobs := h.capture(defaultRequest())
	if jobs[0].State != config.JobFailed {
		t.Fatalf("a rejected option should fail the capture, got %s", jobs[0].State)
	}
	if !strings.Contains(jobs[0].Message, "--http-port") {
		t.Errorf("the failure should name the option: %s", jobs[0].Message)
	}
}

// The race between releasing a port and Keycloak binding it is unavoidable, so
// a bind conflict is retried with newly allocated ports rather than reported.
func TestCapture_RetriesAPortConflictWithFreshPorts(t *testing.T) {
	h := newHarness(t)

	var seenPorts []string
	attempts := 0
	realRun := h.exec.RunFunc
	_ = realRun
	h.exec.RunFunc = func(ctx context.Context, cmd target.Command) (target.ExecResult, error) {
		attempts++
		seenPorts = append(seenPorts, portArgs(cmd.Args))
		if attempts == 1 {
			return target.ExecResult{
				ExitCode: 1,
				Stderr:   "ERROR: java.net.BindException: Address already in use",
			}, nil
		}
		h.exec.RunFunc = nil
		return h.exec.Run(ctx, cmd)
	}

	jobs := h.capture(defaultRequest())
	if jobs[0].State != config.JobCompleted {
		t.Fatalf("a retryable port conflict was not retried: %s", jobs[0].Message)
	}
	if attempts < 2 {
		t.Fatalf("only %d attempts were made", attempts)
	}
	if seenPorts[0] == seenPorts[1] {
		t.Errorf("the retry reused the ports that were already taken: %s", seenPorts[0])
	}
}

func portArgs(args []string) string {
	var parts []string
	for i, a := range args {
		if strings.HasSuffix(a, "-port") && i+1 < len(args) {
			parts = append(parts, args[i+1])
		}
	}
	return strings.Join(parts, ",")
}

// kc.sh can exit zero having written nothing usable, and treating that as
// success would ship a snapshot missing its realm.
func TestCapture_TruncatedExportIsNotSuccess(t *testing.T) {
	h := newHarness(t)
	empty := t.TempDir()
	if err := os.WriteFile(filepath.Join(empty, "notes.txt"), []byte("nothing useful"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.exec.ExportDir = empty

	jobs := h.capture(defaultRequest())
	if jobs[0].State == config.JobCompleted {
		t.Fatal("an export with no realm file was reported as a successful capture")
	}
	if !strings.Contains(jobs[0].Message, "no realm file") {
		t.Errorf("the failure does not explain itself: %q", jobs[0].Message)
	}
}

func TestCapture_RecordsProvenanceAndAudit(t *testing.T) {
	h := newHarness(t)
	h.exec.CloneRef = "job/portcloak-01HZY3-acme"
	jobs := h.capture(defaultRequest())

	j := jobs[0]
	if j.Provenance.ExecutionMode != string(target.ModeEphemeralClone) {
		t.Errorf("execution mode recorded as %q", j.Provenance.ExecutionMode)
	}
	if j.Provenance.CloneRef != "job/portcloak-01HZY3-acme" {
		t.Errorf("clone reference recorded as %q", j.Provenance.CloneRef)
	}

	entries, err := h.audit.Read(obs.AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var capture, declined bool
	for _, e := range entries {
		switch e.Action {
		case obs.ActionCapture:
			capture = true
			if e.Realm != "acme" || e.SnapshotID == "" {
				t.Errorf("capture audit entry is incomplete: %+v", e)
			}
		case obs.ActionEncryptionDeclin:
			declined = true
		}
	}
	if !capture {
		t.Error("a capture produced no audit entry")
	}
	// The choice to write in the clear is recorded, every time.
	if !declined {
		t.Error("declining encryption was not recorded in the audit log")
	}
}

// The bundle has to be openable and complete, which is the only proof that
// packaging, staging and the manifest agree with each other.
func TestCapture_BundleOpensAndVerifies(t *testing.T) {
	h := newHarness(t)
	jobs := h.capture(defaultRequest())
	j := jobs[0]

	bundlePath := filepath.Join(h.storageRoot, filepath.FromSlash(j.StorageKey))
	f, err := os.Open(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	dir := filepath.Join(t.TempDir(), "open")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	opened, err := snapshot.Open(context.Background(), f, snapshot.OpenOptions{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = opened.Close() }()

	if !opened.Verify.OK {
		t.Fatalf("a freshly captured bundle did not verify: %s", opened.Verify.Message)
	}
	if opened.Envelope.Realm != "acme" || opened.Envelope.SnapshotID != j.ID {
		t.Errorf("envelope = %+v", opened.Envelope)
	}

	var m manifest.Manifest
	if err := opened.Document(snapshot.ManifestPath, &m); err != nil {
		t.Fatal(err)
	}
	if m.Counts.Users != 5 {
		t.Errorf("the bundled manifest counts %d users", m.Counts.Users)
	}

	var prov snapshot.Provenance
	if err := opened.Document(snapshot.ProvenancePath, &prov); err != nil {
		t.Fatal(err)
	}
	if prov.CaptureMode != "offline-export" {
		t.Errorf("provenance = %+v", prov)
	}

	// The realm artifacts travel byte for byte: re-serialising them would put
	// PortCloak in the path of the data it promises to carry faithfully.
	original, err := os.ReadFile(filepath.Join(richFixture, "acme-realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	carried, err := os.ReadFile(opened.Path(snapshot.RealmDir + "acme-realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != string(carried) {
		t.Error("the realm file was altered on its way into the bundle")
	}

	// And the users it carries are the ones that were exported.
	users := 0
	for _, name := range opened.RealmFiles {
		if !strings.Contains(name, "-users-") {
			continue
		}
		n, err := realm.StreamUsersFile(context.Background(), opened.Path(name), func(realm.User) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		users += n
	}
	if users != 5 {
		t.Errorf("the bundle carries %d users, want 5", users)
	}
}

func TestCapture_ProgressEventsReachTheSink(t *testing.T) {
	h := newHarness(t)
	h.capture(defaultRequest())

	seen := map[obs.Phase]bool{}
	var logs int
	for _, e := range h.sink.Events() {
		if e.Kind == obs.EventPhaseCompleted {
			seen[e.Phase] = true
		}
		if e.Kind == obs.EventLog {
			logs++
		}
	}
	for _, want := range []obs.Phase{obs.PhaseProbe, obs.PhaseExport, obs.PhaseFetch, obs.PhaseManifest, obs.PhasePackage, obs.PhaseUpload, obs.PhaseTeardown} {
		if !seen[want] {
			t.Errorf("no completion event for the %s phase", want)
		}
	}
	if logs == 0 {
		t.Error("no streamed kc.sh output reached the UI")
	}
}

func TestCapture_UnknownEnvironmentOrStorageIsNamed(t *testing.T) {
	h := newHarness(t)

	req := defaultRequest()
	req.Environment = "nowhere"
	if _, err := h.orc.Capture(context.Background(), req); err == nil {
		t.Error("an unknown environment was accepted")
	} else if !strings.Contains(err.Error(), "nowhere") {
		t.Errorf("the error does not name the environment: %v", err)
	}

	req = defaultRequest()
	req.Storage = "nowhere"
	if _, err := h.orc.Capture(context.Background(), req); err == nil {
		t.Error("an unknown storage was accepted")
	}

	req = defaultRequest()
	req.Realms = nil
	if _, err := h.orc.Capture(context.Background(), req); err == nil {
		t.Error("a capture with no realm was accepted")
	}
}

func TestCapture_InterruptedUploadLeavesAResumableJob(t *testing.T) {
	h := newHarness(t)
	h.blobs = nil // replaced below

	failing := &failingStore{inner: mustDisk(t, h.storageRoot)}
	h.orc = orchestrator.New(orchestrator.Options{
		Home: h.home, Config: h.cfg, Jobs: h.jobs, Log: obs.Discard(), Audit: h.audit,
		Version: "0.0.1-test",
		Registry: orchestrator.Registry{
			Executor: func(config.Environment) (target.Executor, error) { return h.exec, nil },
			Store:    func(config.Storage) (store.BlobStore, error) { return failing, nil },
		},
	})

	handle, err := h.orc.Capture(context.Background(), defaultRequest())
	if err != nil {
		t.Fatal(err)
	}
	jobs := h.waitForJobs(handle.JobIDs)
	j := jobs[0]

	if j.State != config.JobInterrupted {
		t.Fatalf("a dropped upload ended the job as %s, want interrupted: %s", j.State, j.Message)
	}
	if !j.Retryable {
		t.Error("an interrupted upload should be offered as resumable")
	}
	// The sealed bundle is retained locally, so resuming costs the transfer
	// rather than the capture.
	if j.Checkpoint == nil {
		t.Fatal("no checkpoint was written")
	}
}

func mustDisk(t *testing.T, root string) *disk.Store {
	t.Helper()
	s, err := disk.New(root)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// failingStore drops the connection partway through the bundle upload, which
// is the failure an operator actually hits on a bad link.
type failingStore struct {
	inner *disk.Store
	puts  int
}

func (f *failingStore) Probe(ctx context.Context) (store.Reach, error) { return f.inner.Probe(ctx) }

func (f *failingStore) Stat(ctx context.Context, key string) (store.ObjectInfo, error) {
	return f.inner.Stat(ctx, key)
}

func (f *failingStore) Put(ctx context.Context, key string, r io.Reader, opts store.PutOptions) (store.PutResult, error) {
	f.puts++
	if strings.HasSuffix(key, store.BundleExt) {
		// Read a little so a checkpoint has something to describe, then drop.
		_, _ = io.CopyN(io.Discard, r, 512)
		if opts.Progress != nil {
			opts.Progress(512)
		}
		return store.PutResult{}, resil.Retry("upload the snapshot",
			"The connection to the storage dropped partway through the upload.", io.ErrUnexpectedEOF)
	}
	return f.inner.Put(ctx, key, r, opts)
}

func (f *failingStore) Get(ctx context.Context, key string, w io.Writer, opts store.GetOptions) (store.GetResult, error) {
	return f.inner.Get(ctx, key, w, opts)
}

func (f *failingStore) List(ctx context.Context, prefix string) ([]store.ObjectInfo, error) {
	return f.inner.List(ctx, prefix)
}

func (f *failingStore) Delete(ctx context.Context, key string) error { return f.inner.Delete(ctx, key) }
func (f *failingStore) Endpoint() string                             { return f.inner.Endpoint() }
func (f *failingStore) Close() error                                 { return f.inner.Close() }
