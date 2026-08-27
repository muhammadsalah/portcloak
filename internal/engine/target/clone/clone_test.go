package clone_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

	if l[target.LabelCreatedAt] != "2026-08-27T09:14:00Z" {
		t.Errorf("the created-at label is %q", l[target.LabelCreatedAt])
	}
	// The sweep finds orphans by the ephemeral label alone, so it must always
	// be present even when there is no realm.
	if clone.Labels("x", "", at)[target.LabelEphemeral] != "true" {
		t.Error("a clone with no realm lost its sweep label")
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
