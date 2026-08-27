// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package clone_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/target"
	"portcloak/internal/engine/target/clone"
)

// Teardown is parameterised over every exit path, and it is written before any
// real platform exists so the guarantee is pinned before a clone can be created.
func TestTeardown_AllExitPaths(t *testing.T) {
	paths := []struct {
		name string
		run  func(t *testing.T, e *clone.Executor, p *clone.FakePlatform)
	}{
		{"success", func(t *testing.T, e *clone.Executor, p *clone.FakePlatform) {
			if _, err := e.Run(context.Background(), target.Command{Path: "kc.sh"}); err != nil {
				t.Fatal(err)
			}
		}},
		{"the export fails", func(t *testing.T, e *clone.Executor, p *clone.FakePlatform) {
			p.ExecErr = clone.ErrExecFailed
			_, _ = e.Run(context.Background(), target.Command{Path: "kc.sh"})
		}},
		{"the fetch fails", func(t *testing.T, e *clone.Executor, p *clone.FakePlatform) {
			p.CopyErr = errors.New("the connection dropped mid-copy")
			_ = e.FetchDir(context.Background(), "/tmp/x", target.SinkFunc(nil))
		}},
		{"the context is cancelled", func(t *testing.T, e *clone.Executor, p *clone.FakePlatform) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			p.ExecFunc = func(ctx context.Context, _ string, _ target.Command) (target.ExecResult, error) {
				return target.ExecResult{}, ctx.Err()
			}
			_, _ = e.Run(ctx, target.Command{Path: "kc.sh"})
		}},
		{"the caller panics", func(t *testing.T, e *clone.Executor, p *clone.FakePlatform) {
			defer func() { _ = recover() }()
			panic("something went very wrong")
		}},
	}

	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			p := clone.NewFakePlatform()
			e := clone.NewExecutor(p)

			func() {
				// This defer is the guarantee under test: teardown runs on every
				// exit path, including panic.
				defer func() {
					if err := e.Teardown(context.Background()); err != nil {
						t.Errorf("teardown failed: %v", err)
					}
				}()
				if _, err := e.Prepare(context.Background(), target.PrepareOptions{
					JobID: "01HZY3", Realms: []string{"acme"},
				}); err != nil {
					t.Fatal(err)
				}
				path.run(t, e, p)
			}()

			if err := p.Leaked(); err != nil {
				t.Fatalf("on the %q path, %v", path.name, err)
			}
		})
	}
}

// A clone that never became ready still has to be destroyed: the object exists
// whether or not it started.
func TestTeardown_DestroysACloneThatNeverBecameReady(t *testing.T) {
	p := clone.NewFakePlatform()
	p.WaitErr = errors.New("the pod was rejected by PodSecurity admission")
	e := clone.NewExecutor(p)

	if _, err := e.Prepare(context.Background(), target.PrepareOptions{JobID: "01HZY3"}); err == nil {
		t.Fatal("Prepare should have surfaced the admission failure")
	}
	if err := e.Teardown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := p.Leaked(); err != nil {
		t.Fatal(err)
	}
}

// Teardown is called unconditionally from a defer, so it has to be safe when
// Prepare never ran and safe to call twice.
func TestTeardown_IsSafeWhenNothingWasCreatedAndWhenRepeated(t *testing.T) {
	p := clone.NewFakePlatform()
	e := clone.NewExecutor(p)

	if err := e.Teardown(context.Background()); err != nil {
		t.Fatalf("teardown before prepare: %v", err)
	}
	if _, err := e.Prepare(context.Background(), target.PrepareOptions{JobID: "01HZY3"}); err != nil {
		t.Fatal(err)
	}
	if err := e.Teardown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := e.Teardown(context.Background()); err != nil {
		t.Fatalf("a second teardown should be a no-op: %v", err)
	}
	if len(p.Destroyed) != 1 {
		t.Fatalf("destroy was called %d times, want once", len(p.Destroyed))
	}
}

// A failed teardown names the clone so it can be removed by hand, and says the
// sweep will try again.
func TestTeardown_FailureNamesTheCloneAndPromisesARetry(t *testing.T) {
	p := clone.NewFakePlatform()
	p.DestroyErr = errors.New("forbidden: cannot delete jobs")
	e := clone.NewExecutor(p)

	if _, err := e.Prepare(context.Background(), target.PrepareOptions{JobID: "01HZY3"}); err != nil {
		t.Fatal(err)
	}
	err := e.Teardown(context.Background())
	if err == nil {
		t.Fatal("a failed destroy was reported as success")
	}
	msg := err.Error()
	if !strings.Contains(msg, "portcloak-01hzy3") {
		t.Errorf("the failure does not name the clone: %q", msg)
	}
	if !strings.Contains(msg, "database credentials") {
		t.Errorf("the failure does not say why it matters: %q", msg)
	}
}

// The strip list is total and the keep list is explicit, so an accidental
// change to either shows up as a diff rather than as a production incident.
func TestCloneSpec_Derivation(t *testing.T) {
	p := clone.NewFakePlatform()
	e := clone.NewExecutor(p)

	if _, err := e.Prepare(context.Background(), target.PrepareOptions{
		JobID: "01HZY3", Realms: []string{"acme corp/eu"},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Teardown(context.Background()) })

	spec := e.Spec()

	// Only PortCloak's own labels, and nothing inherited.
	if len(spec.Labels) != 4 {
		t.Fatalf("the clone carries %d labels: %v", len(spec.Labels), spec.Labels)
	}
	for k := range spec.Labels {
		if !strings.HasPrefix(k, "portcloak.io/") {
			t.Errorf("the clone inherited a label: %s", k)
		}
	}
	if spec.Labels[target.LabelEphemeral] != "true" || spec.Labels[target.LabelJob] != "01HZY3" {
		t.Errorf("the sweep labels are wrong: %v", spec.Labels)
	}
	// A realm name is not a valid label value, so it is sanitised rather than
	// making the clone unschedulable.
	if got := spec.Labels[target.LabelRealm]; got != "acme-corp-eu" {
		t.Errorf("realm label = %q", got)
	}

	// The entrypoint is replaced with a hang: the clone boots nothing.
	if len(spec.Command) == 0 || !strings.Contains(strings.Join(spec.Command, " "), "sleep") {
		t.Fatalf("the command was not replaced with a hang: %v", spec.Command)
	}
	if strings.Contains(strings.Join(spec.Command, " "), "kc.sh") {
		t.Error("the clone's command should not be the export itself")
	}

	// What travels, and what does not.
	stripped := strings.Join(spec.Stripped, " ")
	for _, want := range []string{"labels", "ownerReferences", "resourceVersion", "uid", "nodeName", "status", "ports", "livenessProbe", "networkAliases"} {
		if !strings.Contains(stripped, want) {
			t.Errorf("the strip list is missing %q", want)
		}
	}
	kept := strings.Join(spec.Kept, " ")
	for _, want := range []string{"imagePullSecrets", "serviceAccountName", "securityContext", "nodeSelector", "tolerations", "volumes", "env", "resources"} {
		if !strings.Contains(kept, want) {
			t.Errorf("the keep list is missing %q — a clone without it may not schedule or may be rejected by an SCC", want)
		}
	}

	// The database environment travels, which is what lets the clone read the
	// realm — and exactly why leaving one running is a credential exposure.
	if spec.Env["KC_DB_URL"] == "" {
		t.Error("the clone did not inherit the database configuration")
	}
}

func TestLabels_AreTheOnlyOnesApplied(t *testing.T) {
	at := time.Date(2026, 8, 27, 9, 14, 0, 0, time.UTC)
	l := clone.Labels("01HZY3", "acme", at)

	if l[target.LabelCreatedAt] != "20260827T091400Z" {
		t.Errorf("the created-at label is %q", l[target.LabelCreatedAt])
	}
	// The sweep finds orphans by the ephemeral label alone, so it must always
	// be present even when there is no realm.
	if clone.Labels("x", "", at)[target.LabelEphemeral] != "true" {
		t.Error("a clone with no realm lost its sweep label")
	}
}

// kubeLabelValue is the rule the API server applies, quoted from its own
// rejection message. Asserting against the real regex rather than against the
// strings we happen to produce is the difference between catching this class of
// fault and re-encoding it: the previous version of the test above asserted the
// created-at label was "2026-08-27T09:14:00Z", which is precisely the value the
// cluster refuses.
var kubeLabelValue = regexp.MustCompile(`^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$`)

// A clone that cannot be created is a capture that dies at the clone step, so
// every label value has to be one the API server will accept — including the
// ones assembled from something that is not free text.
func TestLabels_AreAlwaysValidKubernetesLabelValues(t *testing.T) {
	at := time.Date(2026, 8, 27, 18, 54, 58, 0, time.UTC)

	for _, tc := range []struct{ name, jobID, realm string }{
		{"ordinary", "00003824jfann7v2", "acme"},
		{"a realm with spaces and punctuation", "job-1", "Corp A / Customers (EU)"},
		{"a realm of only illegal characters", "job-1", "!!!"},
		{"a realm with leading and trailing separators", "job-1", "-.acme._"},
		{"a realm longer than a label", "job-1", strings.Repeat("a", 40) + "-" + strings.Repeat("b", 40)},
		// 63 legal characters then a separator: truncating to 63 and trimming
		// in the wrong order leaves a value ending in '-'.
		{"a realm that truncates onto a separator", "job-1", strings.Repeat("a", 62) + "-tail"},
		{"no realm at all", "job-1", ""},
		{"a unicode realm", "job-1", "réalm-München-日本"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := clone.Labels(tc.jobID, tc.realm, at)

			for k, v := range l {
				if len(v) > 63 {
					t.Errorf("%s is %d characters; a label value may be 63", k, len(v))
				}
				if !kubeLabelValue.MatchString(v) {
					t.Errorf("%s = %q, which the API server rejects", k, v)
				}
			}
			// Whatever else happens, the sweep has to still be able to find it.
			if l[target.LabelEphemeral] != "true" {
				t.Error("the label the orphan sweep selects on was lost")
			}
		})
	}
}

func TestOrphans_AreListedOldestFirstAndRemovedOnRequest(t *testing.T) {
	now := time.Now()
	p := clone.NewFakePlatform()
	p.Orphans = []target.Orphan{
		{Ref: "pod/portcloak-2c81f4", CreatedAt: now.Add(-3 * 24 * time.Hour), State: "Running", Kind: "kubernetes"},
		{Ref: "pod/portcloak-aaa", CreatedAt: now.Add(-2 * time.Hour), State: "Running", Kind: "kubernetes"},
	}
	e := clone.NewExecutor(p)

	found, err := e.FindOrphans(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("found %d orphans", len(found))
	}
	if found[0].Ref != "pod/portcloak-2c81f4" {
		t.Errorf("orphans are not oldest-first: %+v", found)
	}

	// The age is what decides whether an orphan matters, so it is rendered.
	desc := clone.DescribeOrphan(found[0], now)
	if !strings.Contains(desc, "3 days") {
		t.Errorf("the description does not say how old it is: %q", desc)
	}

	// Removal happens on request, never automatically.
	if err := e.RemoveOrphan(context.Background(), found[0].Ref); err != nil {
		t.Fatal(err)
	}
	after, _ := e.FindOrphans(context.Background())
	if len(after) != 1 {
		t.Fatalf("removal left %d orphans", len(after))
	}
}

// A clone that is gone cannot be exec'd into, and saying so plainly beats a nil
// dereference.
func TestExecutor_RefusesToUseATornDownClone(t *testing.T) {
	p := clone.NewFakePlatform()
	e := clone.NewExecutor(p)
	if _, err := e.Prepare(context.Background(), target.PrepareOptions{JobID: "01HZY3"}); err != nil {
		t.Fatal(err)
	}
	if err := e.Teardown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Run(context.Background(), target.Command{Path: "kc.sh"}); err == nil {
		t.Fatal("a command ran against a destroyed clone")
	}
	if err := e.FetchDir(context.Background(), "/tmp", target.SinkFunc(nil)); err == nil {
		t.Fatal("a fetch ran against a destroyed clone")
	}
}

// A restore pushes into <workdir>/import/, a directory Prepare does not create
// because it is named by the caller. Local and SSH have always made the parent
// on the way past; the clone path did not, so a restore onto Docker or
// Kubernetes failed on its first file — five times over, because a missing
// directory is indistinguishable at that layer from a dropped connection.
func TestPushFile_CreatesTheDirectoryAndOwnsTheFile(t *testing.T) {
	p := clone.NewFakePlatform()

	var execs []string
	p.ExecFunc = func(_ context.Context, _ string, cmd target.Command) (target.ExecResult, error) {
		script := strings.Join(cmd.Args, " ")
		execs = append(execs, script)
		if strings.Contains(script, "id -u") && cmd.OnStdout != nil {
			// The clone runs as an unprivileged user, as every Keycloak image does.
			cmd.OnStdout("1000")
			cmd.OnStdout("1001")
		}
		return target.ExecResult{ExitCode: 0}, nil
	}

	e := clone.NewExecutor(p)
	ec, err := e.Prepare(context.Background(), target.PrepareOptions{JobID: "01HPUSH"})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Teardown(context.Background()) //nolint:errcheck

	dir := ec.WorkDir + "/import"
	for _, name := range []string{"acme-realm.json", "acme-users-0.json", "acme-users-1.json"} {
		if err := e.PushFile(context.Background(), dir+"/"+name, 4, strings.NewReader("body")); err != nil {
			t.Fatalf("pushing %s failed: %v", name, err)
		}
	}

	var made int
	for _, script := range execs {
		if strings.Contains(script, "mkdir -p") && strings.Contains(script, dir) {
			made++
		}
	}
	if made == 0 {
		t.Fatalf("the import directory was never created: %q", execs)
	}
	// Three files into one directory is one round trip, not three.
	if made > 1 {
		t.Errorf("the same directory was created %d times", made)
	}

	if len(p.CopiedIn) != 3 {
		t.Fatalf("%d files were copied in, want 3", len(p.CopiedIn))
	}
	for _, f := range p.CopiedIn {
		// Docker's CopyToContainer unpacks as root and honours the tar entry's
		// ids, so zeroes here land the realm as root:root and kc.sh — running as
		// the image's own user — cannot read what it was asked to import.
		if f.Owner.UID != 1000 || f.Owner.GID != 1001 {
			t.Errorf("%s was written for %d:%d, want the clone's own 1000:1001", f.Path, f.Owner.UID, f.Owner.GID)
		}
	}
}

// An image with no `id` is not a reason to fail the restore: root is what the
// header carried before, and it is correct for a clone that does run as root.
func TestPushFile_FallsBackToRootWhenTheCloneCannotSayWhoItIs(t *testing.T) {
	p := clone.NewFakePlatform()
	p.ExecFunc = func(_ context.Context, _ string, cmd target.Command) (target.ExecResult, error) {
		if strings.Contains(strings.Join(cmd.Args, " "), "id -u") {
			return target.ExecResult{ExitCode: 127}, nil
		}
		return target.ExecResult{ExitCode: 0}, nil
	}

	e := clone.NewExecutor(p)
	ec, err := e.Prepare(context.Background(), target.PrepareOptions{JobID: "01HPUSH2"})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Teardown(context.Background()) //nolint:errcheck

	if err := e.PushFile(context.Background(), ec.WorkDir+"/import/a.json", 4, strings.NewReader("body")); err != nil {
		t.Fatalf("a clone that cannot report its user should still take a file: %v", err)
	}
	if len(p.CopiedIn) != 1 || p.CopiedIn[0].Owner != (clone.FileOwner{}) {
		t.Errorf("expected a root-owned fallback, got %+v", p.CopiedIn)
	}
}

// A directory that cannot be created is the same failure every time. Reported,
// not retried five times against a clone that will never accept the file.
func TestPushFile_ADirectoryThatCannotBeMadeIsFatal(t *testing.T) {
	p := clone.NewFakePlatform()
	p.ExecFunc = func(_ context.Context, _ string, cmd target.Command) (target.ExecResult, error) {
		script := strings.Join(cmd.Args, " ")
		if strings.Contains(script, "mkdir -p") && strings.Contains(script, "/import") {
			return target.ExecResult{ExitCode: 1}, nil
		}
		return target.ExecResult{ExitCode: 0}, nil
	}

	e := clone.NewExecutor(p)
	ec, err := e.Prepare(context.Background(), target.PrepareOptions{JobID: "01HPUSH3"})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Teardown(context.Background()) //nolint:errcheck

	err = e.PushFile(context.Background(), ec.WorkDir+"/import/a.json", 4, strings.NewReader("body"))
	if err == nil {
		t.Fatal("a directory that could not be created was swallowed")
	}
	if resil.IsRetryable(err) {
		t.Errorf("a missing directory is deterministic; retrying it is a loop: %v", err)
	}
	if len(p.CopiedIn) != 0 {
		t.Error("a file was copied into a directory that does not exist")
	}
}
