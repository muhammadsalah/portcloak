package clone

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"portcloak/internal/engine/target"
)

// FakePlatform is a clone platform that exists only in memory.
//
// It is exported rather than test-only because the teardown guarantees are
// asserted from the orchestrator's tests as well as this package's, and both
// need to force the same exit paths.
type FakePlatform struct {
	mu sync.Mutex

	// Spec is what Inspect returns before the lifecycle fills in the rest.
	Base Spec

	InspectErr error
	CreateErr  error
	WaitErr    error
	ExecErr    error
	CopyErr    error
	DestroyErr error

	// ExecFunc overrides what a command does inside the clone.
	ExecFunc func(ctx context.Context, ref string, cmd target.Command) (target.ExecResult, error)
	// CopyOutFunc supplies the artifacts a fetch streams back.
	CopyOutFunc func(ctx context.Context, ref, path string, sink target.ArtifactSink) error

	// Live is the set of clones that currently exist. A test asserts it is
	// empty at the end, whichever way the job finished.
	Live         map[string]Spec
	Created      []string
	createdSpecs []Spec
	Destroyed    []string
	Orphans      []target.Orphan
	Closed       bool
}

// NewFakePlatform builds a fake with a plausible base spec.
func NewFakePlatform() *FakePlatform {
	return &FakePlatform{
		Base: Spec{
			Image: "quay.io/keycloak/keycloak:25.0.2",
			Env: map[string]string{
				"KC_DB":          "postgres",
				"KC_DB_URL":      "jdbc:postgresql://db:5432/keycloak",
				"KC_DB_USERNAME": "keycloak",
				"KC_DB_PASSWORD": "the-database-password",
			},
		},
		Live: map[string]Spec{},
	}
}

func (f *FakePlatform) Inspect(ctx context.Context, jobID string, realms []string) (Spec, error) {
	if f.InspectErr != nil {
		return Spec{}, f.InspectErr
	}
	s := f.Base
	s.JobID = jobID
	s.WorkDir = target.WorkDirFor(jobID)
	// A faithful fake copies the env map, so a caller mutating one clone's
	// environment cannot reach into another's.
	s.Env = map[string]string{}
	for k, v := range f.Base.Env {
		s.Env[k] = v
	}
	// The serving workload's own labels are carried into the derived spec so
	// that the lifecycle replacing them is observable rather than assumed.
	s.Labels = map[string]string{}
	for k, v := range f.Base.Labels {
		s.Labels[k] = v
	}
	return s, nil
}

func (f *FakePlatform) Create(ctx context.Context, spec Spec) (string, error) {
	if f.CreateErr != nil {
		return "", f.CreateErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	ref := "clone/" + Name(spec.JobID)
	f.Live[ref] = spec
	f.Created = append(f.Created, ref)
	f.createdSpecs = append(f.createdSpecs, spec)
	return ref, nil
}

func (f *FakePlatform) WaitRunning(ctx context.Context, ref string) error {
	return f.WaitErr
}

func (f *FakePlatform) Exec(ctx context.Context, ref string, cmd target.Command) (target.ExecResult, error) {
	if f.ExecFunc != nil {
		return f.ExecFunc(ctx, ref, cmd)
	}
	if f.ExecErr != nil {
		return target.ExecResult{}, f.ExecErr
	}
	return target.ExecResult{ExitCode: 0}, nil
}

func (f *FakePlatform) CopyOut(ctx context.Context, ref, path string, sink target.ArtifactSink) error {
	if f.CopyOutFunc != nil {
		return f.CopyOutFunc(ctx, ref, path, sink)
	}
	return f.CopyErr
}

func (f *FakePlatform) CopyIn(ctx context.Context, ref, path string, size int64, r io.Reader) error {
	if f.CopyErr != nil {
		return f.CopyErr
	}
	_, err := io.Copy(io.Discard, r)
	return err
}

func (f *FakePlatform) Destroy(ctx context.Context, ref string) error {
	if f.DestroyErr != nil {
		return f.DestroyErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.Live, ref)
	f.Destroyed = append(f.Destroyed, ref)
	for i, o := range f.Orphans {
		if o.Ref == ref {
			f.Orphans = append(f.Orphans[:i], f.Orphans[i+1:]...)
			break
		}
	}
	return nil
}

func (f *FakePlatform) Probe(ctx context.Context) (target.TargetFacts, error) {
	facts := target.TargetFacts{
		Kind: "fake", Reachable: true, KeycloakVersion: "25.0.2",
		KcPath: "/opt/keycloak/bin/kc.sh", Mode: target.ModeEphemeralClone,
		CloneCapable: true, CloneDetail: "can be created", HasTar: true,
		ProbedAt: time.Now(), ReadOnlyNote: "Nothing was written. The probe only reads.",
	}
	facts.Pass("Ephemeral clone", "can be created")
	return facts, nil
}

func (f *FakePlatform) FindOrphans(ctx context.Context) ([]target.Orphan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]target.Orphan, len(f.Orphans))
	copy(out, f.Orphans)
	return out, nil
}

func (f *FakePlatform) Close() error {
	f.Closed = true
	return nil
}

// CreatedSpecs is the exact specification each clone was created from, which
// is the authoritative record of what would have been applied to the cluster.
func (f *FakePlatform) CreatedSpecs() []Spec {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Spec, len(f.createdSpecs))
	copy(out, f.createdSpecs)
	return out
}

// LiveCount is how many clones currently exist.
func (f *FakePlatform) LiveCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Live)
}

// LiveRefs lists the clones that still exist, for a leak assertion.
func (f *FakePlatform) LiveRefs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.Live))
	for ref := range f.Live {
		out = append(out, ref)
	}
	return out
}

// ErrExecFailed is what a scripted exec failure returns.
var ErrExecFailed = errors.New("the exec channel dropped")

// Leaked renders the leak-sweep failure, so a test that finds one says which
// object survived.
func (f *FakePlatform) Leaked() error {
	refs := f.LiveRefs()
	if len(refs) == 0 {
		return nil
	}
	return fmt.Errorf("%d ephemeral clone(s) survived: %v", len(refs), refs)
}
