package orchestrator_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/inspect"
	"portcloak/internal/engine/kc"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/orchestrator"
	"portcloak/internal/engine/store"
	"portcloak/internal/engine/target"
)

// fakeDestination is the Admin API view of a restore target.
type fakeDestination struct {
	mu        sync.Mutex
	reachable bool
	shape     orchestrator.RealmShape
	// contacted records every call, so a test can assert the target was never
	// touched.
	contacted []string
	imported  []byte
	policy    string
}

func (d *fakeDestination) Reachable(context.Context) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.contacted = append(d.contacted, "reachable")
	return d.reachable
}

func (d *fakeDestination) ReadRealm(_ context.Context, realmName string) (orchestrator.RealmShape, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.contacted = append(d.contacted, "readRealm:"+realmName)
	return d.shape, nil
}

func (d *fakeDestination) PartialImport(_ context.Context, realmName string, body []byte, policy string) (int, int, int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.contacted = append(d.contacted, "partialImport:"+realmName)
	d.imported = body
	d.policy = policy
	return 12, 3, 0, nil
}

func (d *fakeDestination) touched() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.contacted...)
}

// restoreHarness captures a snapshot, then wires an orchestrator that can
// restore it.
type restoreHarness struct {
	*harness
	dest       *fakeDestination
	bundleKey  string
	snapshotID string
}

func newRestoreHarness(t *testing.T) *restoreHarness {
	t.Helper()
	h := newHarness(t)
	if err := h.cfg.AddEnvironment(config.Environment{
		Name: "staging", Kind: config.EnvLocal, ServerFolder: "/opt/keycloak",
	}); err != nil {
		t.Fatal(err)
	}

	jobs := h.capture(defaultRequest())
	if jobs[0].State != config.JobCompleted {
		t.Fatalf("the seeding capture failed: %s", jobs[0].Message)
	}

	// The seeding capture used the same fake executor, so its record is cleared
	// before any restore assertion is made about it.
	h.exec.Reset()

	dest := &fakeDestination{reachable: true}
	rh := &restoreHarness{
		harness: h, dest: dest,
		bundleKey: jobs[0].StorageKey, snapshotID: jobs[0].SnapshotID,
	}
	rh.rewire()
	return rh
}

func (rh *restoreHarness) rewire() {
	rh.orc = orchestrator.New(orchestrator.Options{
		Home: rh.home, Config: rh.cfg, Jobs: rh.jobs, Log: obs.Discard(), Audit: rh.audit,
		Version: "0.0.1-test",
		Registry: orchestrator.Registry{
			Executor: func(config.Environment) (target.Executor, error) { return rh.exec, nil },
			Store:    func(config.Storage) (store.BlobStore, error) { return rh.blobs, nil },
			Destination: func(config.Environment) (orchestrator.Destination, error) {
				return rh.dest, nil
			},
		},
	})
	rh.orc.SetSink(rh.sink)
}

func (rh *restoreHarness) restoreRequest() orchestrator.RestoreRequest {
	return orchestrator.RestoreRequest{
		Environment: "staging",
		Storage:     "local-disk",
		BundleKey:   rh.bundleKey,
		SnapshotID:  rh.snapshotID,
		Realm:       "acme",
		Strategy:    kc.StrategyOverwrite,
	}
}

func (rh *restoreHarness) restore(t *testing.T, req orchestrator.RestoreRequest) *config.Job {
	t.Helper()
	res, err := rh.orc.Restore(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return rh.waitForJobs([]string{res.JobID})[0]
}

func TestRestore_AppliesAndValidates(t *testing.T) {
	rh := newRestoreHarness(t)
	rh.dest.shape = orchestrator.RealmShape{
		Exists: true, Users: 5, Clients: 4, RealmRoles: 2, Groups: 3,
		IdentityProviders: 2, Federations: 1, KeyIDs: []string{"abc123"},
	}

	req := rh.restoreRequest()
	req.ConfirmRealm = "acme"
	j := rh.restore(t, req)

	if j.State != config.JobCompleted {
		t.Fatalf("restore ended %s: %s", j.State, j.Message)
	}
	// The realm artifacts were pushed into the execution context.
	if len(rh.exec.Pushed) == 0 {
		t.Fatal("nothing was sent to the destination")
	}
	found := false
	for name := range rh.exec.Pushed {
		if strings.HasSuffix(name, "acme-realm.json") {
			found = true
		}
	}
	if !found {
		t.Errorf("the realm file was not sent: %v", keysOf(rh.exec.Pushed))
	}

	cmd, ok := rh.exec.LastCommand()
	if !ok {
		t.Fatal("no import command was run")
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "import") || !strings.Contains(joined, "--override true") {
		t.Errorf("the import invocation is wrong: %s", joined)
	}

	entries, err := rh.audit.Read(obs.AuditFilter{Action: obs.ActionRestore})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Outcome != "restored" {
		t.Errorf("restore audit = %+v", entries)
	}
}

// A restore that half-writes a corrupted realm is worse than one that never
// starts, so the gate is unconditional and comes before any contact.
func TestRestore_RefusesTamperedBundleBeforeContactingTarget(t *testing.T) {
	rh := newRestoreHarness(t)

	path := filepath.Join(rh.storageRoot, filepath.FromSlash(rh.bundleKey))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 0xff
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	j := rh.restore(t, rh.restoreRequest())
	if j.State == config.JobCompleted {
		t.Fatal("a tampered bundle was restored")
	}
	if len(rh.dest.touched()) != 0 {
		t.Fatalf("the destination was contacted before the bundle was proven intact: %v", rh.dest.touched())
	}
	if rh.exec.RunCount() != 0 {
		t.Fatal("a command ran against the destination despite the failed verification")
	}
}

// The step is informative only. Next stays enabled even when every dependency
// is missing, because the operator manages these environments.
func TestPreconditions_NeverBlocks(t *testing.T) {
	rh := newRestoreHarness(t)
	session := rh.openSnapshot(t)
	defer session.Close()

	report := orchestrator.Preconditions(session)
	if report.Blocks {
		t.Fatal("the preconditions step reported itself as blocking")
	}
	if report.IntegrityPassed != session.Verify.OK {
		t.Error("the already-passed integrity result is not shown alongside")
	}
	for _, d := range report.Dependencies {
		if d.Consequence == "" {
			t.Errorf("%s does not state the consequence of its absence", d.Name)
		}
		if d.Action == "" {
			t.Errorf("%s does not say what to do about it", d.Name)
		}
	}
}

// Absence of data is never presented as absence of dependencies.
func TestPreconditions_NotCheckedIsNotNone(t *testing.T) {
	rh := newRestoreHarness(t)
	session := rh.openSnapshot(t)
	defer session.Close()

	// The seeding capture ran with no Admin API, so detection did not run.
	report := orchestrator.Preconditions(session)
	if report.Checked {
		t.Fatal("dependency detection was reported as having run")
	}
	if !strings.Contains(report.Summary, "did not run") {
		t.Errorf("the summary should say the check did not run: %q", report.Summary)
	}
	if strings.Contains(strings.ToLower(report.Summary), "no themes") {
		t.Error("a skipped check was reported as an empty result")
	}
}

func (rh *restoreHarness) openSnapshot(t *testing.T) *inspect.Session {
	t.Helper()
	s, err := inspect.Open(context.Background(), rh.home, rh.blobs, inspect.OpenRequest{
		Storage: "local-disk", BundleKey: rh.bundleKey, SnapshotID: rh.snapshotID,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Diffing a realm that does not exist is the common case and reads as
// "everything is new", not as an error.
func TestDryRun_EmptyTarget(t *testing.T) {
	rh := newRestoreHarness(t)
	session := rh.openSnapshot(t)
	defer session.Close()

	rh.dest.shape = orchestrator.RealmShape{Exists: false}
	dry := orchestrator.ComputeDryRun(context.Background(), session, rh.dest, kc.StrategyOverwrite)

	if !dry.Available {
		t.Fatalf("the dry run was unavailable: %s", dry.Unavailable)
	}
	if dry.TargetExists {
		t.Fatal("an absent realm reported as existing")
	}
	for _, row := range dry.Categories {
		if row.Overwrite != 0 {
			t.Errorf("%s reports overwrites against a realm that does not exist", row.Category)
		}
	}
	if !strings.Contains(dry.Summary, "does not exist") {
		t.Errorf("summary = %q", dry.Summary)
	}
}

// The preview shown has to be the preview of the strategy actually selected.
func TestDryRun_ReflectsStrategy(t *testing.T) {
	rh := newRestoreHarness(t)
	session := rh.openSnapshot(t)
	defer session.Close()

	rh.dest.shape = orchestrator.RealmShape{
		Exists: true, Users: 3, Clients: 4, RealmRoles: 2, Groups: 3,
		IdentityProviders: 1, Federations: 1, KeyIDs: []string{"old-kid"},
	}

	overwrite := orchestrator.ComputeDryRun(context.Background(), session, rh.dest, kc.StrategyOverwrite)
	skip := orchestrator.ComputeDryRun(context.Background(), session, rh.dest, kc.StrategySkip)

	users := func(d orchestrator.DryRun) orchestrator.DiffCategory {
		for _, row := range d.Categories {
			if row.Category == "Users" {
				return row
			}
		}
		t.Fatal("no Users row")
		return orchestrator.DiffCategory{}
	}
	if users(overwrite).Overwrite == 0 {
		t.Error("overwrite should report overwrites against existing users")
	}
	if users(skip).Overwrite != 0 || users(skip).LeaveAlone == 0 {
		t.Errorf("skip should leave existing users alone: %+v", users(skip))
	}
	if overwrite.Summary == skip.Summary {
		t.Error("the preview did not change with the strategy")
	}

	// The caveat about a non-transactional import is always present.
	if !strings.Contains(overwrite.Caveat, "not transactional") {
		t.Errorf("caveat = %q", overwrite.Caveat)
	}
}

// A note that matters gets said, and it says what happens rather than naming a
// flag.
func TestDryRun_NotesTheThingsThatBreakLogins(t *testing.T) {
	rh := newRestoreHarness(t)
	session := rh.openSnapshot(t)
	defer session.Close()

	rh.dest.shape = orchestrator.RealmShape{Exists: true, Users: 5, Clients: 4, KeyIDs: []string{"old"}}
	dry := orchestrator.ComputeDryRun(context.Background(), session, rh.dest, kc.StrategyOverwrite)

	notes := map[string]string{}
	for _, row := range dry.Categories {
		notes[row.Category] = row.Note
	}
	// The rich fixture has a client whose secret was masked at source.
	if !strings.Contains(notes["Clients"], "without its secret") {
		t.Errorf("the clients note does not mention the missing secret: %q", notes["Clients"])
	}
	if notes["Key providers"] == "" {
		t.Error("overwriting a realm with existing keys should say what happens to tokens")
	}
}

// The dry run is marked unavailable rather than silently skipped.
func TestDryRun_UnreadableTargetIsSaidNotHidden(t *testing.T) {
	rh := newRestoreHarness(t)
	session := rh.openSnapshot(t)
	defer session.Close()

	rh.dest.reachable = false
	dry := orchestrator.ComputeDryRun(context.Background(), session, rh.dest, kc.StrategyOverwrite)
	if dry.Available {
		t.Fatal("a dry run was produced against an unreadable target")
	}
	if !strings.Contains(dry.Unavailable, "can still proceed") {
		t.Errorf("the operator should be told the import can proceed without a preview: %q", dry.Unavailable)
	}
}

// Overwriting a realm that already exists is destructive and irreversible.
func TestRestore_OverwriteRequiresConfirmingTheRealmName(t *testing.T) {
	rh := newRestoreHarness(t)
	rh.dest.shape = orchestrator.RealmShape{Exists: true, Users: 3}

	j := rh.restore(t, rh.restoreRequest()) // no ConfirmRealm
	if j.State == config.JobCompleted {
		t.Fatal("an unconfirmed overwrite of an existing realm was applied")
	}
	if !strings.Contains(j.Message, "acme") {
		t.Errorf("the refusal does not name the realm: %q", j.Message)
	}
	if rh.exec.RunCount() != 0 {
		t.Fatal("the import ran despite the missing confirmation")
	}

	// An empty destination needs no confirmation: there is nothing to destroy.
	rh.dest.shape = orchestrator.RealmShape{Exists: false}
	j = rh.restore(t, rh.restoreRequest())
	if j.State != config.JobCompleted {
		t.Fatalf("restoring into an empty destination should not need a confirmation: %s", j.Message)
	}
}

// Merge has no offline equivalent, and silently downgrading it would apply a
// different, destructive operation than the one previewed.
func TestRestore_MergeUsesPartialImportAndSaysSoWhenItCannot(t *testing.T) {
	rh := newRestoreHarness(t)
	rh.dest.shape = orchestrator.RealmShape{Exists: true}

	req := rh.restoreRequest()
	req.Strategy = kc.StrategyMerge
	j := rh.restore(t, req)

	if j.State != config.JobCompleted {
		t.Fatalf("merge ended %s: %s", j.State, j.Message)
	}
	if rh.dest.policy != "OVERWRITE" {
		t.Errorf("partialImport policy = %q", rh.dest.policy)
	}
	// The realm document travels verbatim.
	var applied map[string]any
	if err := json.Unmarshal(rh.dest.imported, &applied); err != nil {
		t.Fatalf("what was applied is not readable JSON: %v", err)
	}
	if applied["realm"] != "acme" {
		t.Errorf("the wrong realm was applied: %v", applied["realm"])
	}
	if rh.exec.RunCount() != 0 {
		t.Error("merge should not run kc.sh import")
	}

	// With no Admin API, merge says which path it needs rather than quietly
	// becoming an overwrite.
	rh.dest = nil
	rh.orc = orchestrator.New(orchestrator.Options{
		Home: rh.home, Config: rh.cfg, Jobs: rh.jobs, Log: obs.Discard(), Audit: rh.audit,
		Version: "0.0.1-test",
		Registry: orchestrator.Registry{
			Executor: func(config.Environment) (target.Executor, error) { return rh.exec, nil },
			Store:    func(config.Storage) (store.BlobStore, error) { return rh.blobs, nil },
		},
	})
	j = rh.restore(t, req)
	if j.State == config.JobCompleted {
		t.Fatal("merge completed with no Admin API")
	}
	if !strings.Contains(j.Message, "Admin API") {
		t.Errorf("the failure does not name the path it needs: %q", j.Message)
	}
}

// A restore cannot always be unwound, and pretending otherwise leaves an
// operator with a wrong picture of their own system.
func TestRestore_PartialApplicationIsReportedHonestly(t *testing.T) {
	rh := newRestoreHarness(t)
	rh.dest.shape = orchestrator.RealmShape{Exists: false}
	rh.exec.RunFunc = func(ctx context.Context, cmd target.Command) (target.ExecResult, error) {
		return target.ExecResult{
			ExitCode: 1,
			Stderr:   "ERROR [org.keycloak.exportimport] Failed to import client app-web",
		}, nil
	}

	j := rh.restore(t, rh.restoreRequest())
	if j.State == config.JobCompleted {
		t.Fatal("a failed import was reported as a success")
	}

	var ledgerNote string
	for _, e := range j.Ledger {
		if e.Phase == string(obs.PhaseImport) && e.Item == "destination state" {
			ledgerNote = e.Outcome
		}
	}
	if !strings.Contains(ledgerNote, "not transactional") {
		t.Errorf("the ledger does not say what state the destination may be in: %q", ledgerNote)
	}

	entries, err := rh.audit.Read(obs.AuditFilter{Action: obs.ActionRestore})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Outcome != "did not finish" {
		t.Errorf("audit = %+v", entries)
	}
}

// The assertion the whole tool builds toward: the active signing key is on the
// destination, so tokens issued before the move still verify.
func TestPostRestoreValidation_ChecksTheSigningKeyByKID(t *testing.T) {
	rh := newRestoreHarness(t)
	rh.dest.shape = orchestrator.RealmShape{
		Exists: true, Users: 5, Clients: 4, RealmRoles: 2, Groups: 3,
		IdentityProviders: 2, Federations: 1, KeyIDs: []string{"abc123"},
	}

	req := rh.restoreRequest()
	req.ConfirmRealm = "acme"
	j := rh.restore(t, req)
	if j.State != config.JobCompleted {
		t.Fatalf("restore ended %s: %s", j.State, j.Message)
	}
	if !strings.Contains(j.Message, "reconciles") {
		t.Errorf("the result does not report validation: %q", j.Message)
	}

	// The wrong key ids mean every existing token stops verifying, even if the
	// count is right.
	rh.dest.shape.KeyIDs = []string{"a-different-kid"}
	j = rh.restore(t, req)
	if j.State != config.JobCompleted {
		t.Fatalf("restore ended %s: %s", j.State, j.Message)
	}
	if !strings.Contains(j.Message, "did not reconcile") {
		t.Errorf("a missing signing key was not reported as drift: %q", j.Message)
	}
}

// Validation that could not run is reported as not performed, never as passed.
func TestPostRestoreValidation_UnreachableIsNotPassed(t *testing.T) {
	rh := newRestoreHarness(t)
	rh.dest.reachable = false

	j := rh.restore(t, rh.restoreRequest())
	if j.State != config.JobCompleted {
		t.Fatalf("restore ended %s: %s", j.State, j.Message)
	}
	if !strings.Contains(j.Message, "not performed") {
		t.Errorf("validation that did not run should say so: %q", j.Message)
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
