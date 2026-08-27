// Package target is the seam that makes "capture from anywhere" one workflow
// instead of four. The orchestrator knows only that it can Probe, Prepare, Run,
// FetchDir and Teardown; it never knows whether that meant a local process, an
// SSH channel, an exec into a throwaway container, or a Kubernetes Job.
package target

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// ErrNotImplemented is returned by a lifecycle method an adapter does not yet
// provide. It is a distinct error so a half-built adapter reports what it is
// rather than failing obscurely.
var ErrNotImplemented = errors.New("this target does not support that operation yet")

// ExecMode is how the export is executed against a target.
type ExecMode string

const (
	// ModeInPlace runs the export as a separate process in the target's own
	// context, isolated by free ports. Local and SSH report this.
	ModeInPlace ExecMode = "in-place"
	// ModeEphemeralClone runs the export inside a throwaway copy of the
	// serving workload. Docker and Kubernetes report this, and it is what makes
	// "never disturb the serving instance" structural rather than a convention.
	ModeEphemeralClone ExecMode = "ephemeral-clone"
)

// PortSet is the three ports an offline export binds.
//
// They are passed on every target, including inside a clone where a collision
// is impossible. It costs nothing and keeps one code path honest across all
// four kinds.
type PortSet struct {
	HTTP       int `json:"http"`
	HTTPS      int `json:"https"`
	Management int `json:"management"`
}

// Allocated reports whether a port set has been filled in.
func (p PortSet) Allocated() bool { return p.HTTP > 0 && p.HTTPS > 0 && p.Management > 0 }

func (p PortSet) String() string {
	return fmt.Sprintf("%d / %d / %d", p.HTTP, p.HTTPS, p.Management)
}

// CheckStatus is the outcome of one probe check.
type CheckStatus string

const (
	CheckPass CheckStatus = "pass"
	// CheckWarn is a fact worth knowing that does not stop a capture.
	CheckWarn CheckStatus = "warn"
	CheckFail CheckStatus = "fail"
	// CheckSkipped is a check that could not run. It is reported as skipped
	// rather than as passed, because "not checked" and "fine" are different
	// answers and only one of them is safe to act on.
	CheckSkipped CheckStatus = "skipped"
)

// Check is one thing the probe looked at, rendered as the concrete fact an
// operator needs. A green tick answers "did it connect"; an operator needs to
// know "will my capture work", and those are different questions.
type Check struct {
	Name   string      `json:"name"`
	Value  string      `json:"value"`
	Status CheckStatus `json:"status"`
	// Blocking marks a failure that stops a capture from starting at all.
	Blocking bool `json:"blocking"`
	// Advice is the suggested fix.
	Advice string `json:"advice,omitempty"`
}

// TargetFacts is what a probe found. It is read-only on the target, without
// exception: it reads a version, stats a path, checks a permission. It never
// writes, never restarts anything, and never creates the clone it reports as
// feasible.
type TargetFacts struct {
	Kind            string    `json:"kind"`
	Reachable       bool      `json:"reachable"`
	KeycloakVersion string    `json:"keycloakVersion,omitempty"`
	KcPath          string    `json:"kcPath,omitempty"`
	TempDir         string    `json:"tempDir,omitempty"`
	FreeBytes       int64     `json:"freeBytes,omitempty"`
	HasTar          bool      `json:"hasTar"`
	Mode            ExecMode  `json:"mode"`
	CloneCapable    bool      `json:"cloneCapable"`
	CloneDetail     string    `json:"cloneDetail,omitempty"`
	AdminReachable  bool      `json:"adminReachable"`
	AdminDetail     string    `json:"adminDetail,omitempty"`
	Ports           PortSet   `json:"ports"`
	Realms          []string  `json:"realms,omitempty"`
	Checks          []Check   `json:"checks"`
	ProbedAt        time.Time `json:"probedAt"`
	// ReadOnlyNote is the sentence the UI shows under a probe result, because a
	// probe against production has to be visibly harmless.
	ReadOnlyNote string `json:"readOnlyNote"`
}

// OK reports whether a capture could start against these facts.
func (f TargetFacts) OK() bool {
	if !f.Reachable {
		return false
	}
	for _, c := range f.Checks {
		if c.Status == CheckFail && c.Blocking {
			return false
		}
	}
	return true
}

// FirstBlocker returns the check that stops a capture, if there is one.
func (f TargetFacts) FirstBlocker() (Check, bool) {
	for _, c := range f.Checks {
		if c.Status == CheckFail && c.Blocking {
			return c, true
		}
	}
	return Check{}, false
}

// Summary renders the one-line result for the environments list.
func (f TargetFacts) Summary() string {
	if c, blocked := f.FirstBlocker(); blocked {
		return c.Name + ": " + c.Value
	}
	if !f.Reachable {
		return "not reachable"
	}
	parts := []string{"Keycloak " + f.KeycloakVersion}
	if f.CloneCapable {
		parts = append(parts, "clone permitted")
	}
	if f.AdminReachable {
		parts = append(parts, "Admin API reachable")
	}
	return strings.Join(parts, " · ")
}

// AddCheck appends a check.
func (f *TargetFacts) AddCheck(c Check) { f.Checks = append(f.Checks, c) }

// Pass records a check that succeeded.
func (f *TargetFacts) Pass(name, value string) {
	f.AddCheck(Check{Name: name, Value: value, Status: CheckPass})
}

// Warn records a fact worth knowing that does not stop a capture.
func (f *TargetFacts) Warn(name, value, advice string) {
	f.AddCheck(Check{Name: name, Value: value, Status: CheckWarn, Advice: advice})
}

// Fail records a blocking failure.
func (f *TargetFacts) Fail(name, value, advice string) {
	f.AddCheck(Check{Name: name, Value: value, Status: CheckFail, Blocking: true, Advice: advice})
}

// Skipped records a check that could not run.
func (f *TargetFacts) Skipped(name, reason string) {
	f.AddCheck(Check{Name: name, Value: reason, Status: CheckSkipped})
}

// PrepareOptions is what the orchestrator asks for before running anything.
type PrepareOptions struct {
	JobID string
	// Realms is what the run intends to export, so a clone can be created once
	// and reused across several realms rather than per realm.
	Realms []string
	// Purpose distinguishes a capture from a restore, which affects nothing
	// about how a clone is built but everything about what gets audited.
	Purpose string
}

// ExecContext describes where commands will actually run.
type ExecContext struct {
	Mode ExecMode `json:"mode"`
	// CloneRef identifies the ephemeral clone, recorded in provenance so a
	// snapshot can say which throwaway container produced it.
	CloneRef string `json:"cloneRef,omitempty"`
	// WorkDir is the temp directory inside the execution context.
	WorkDir string `json:"workDir"`
	// Ports are the free ports the offline export will bind.
	Ports PortSet `json:"ports"`
}

// Command is one thing to run inside the execution context.
type Command struct {
	Path string
	Args []string
	Env  map[string]string
	Dir  string
	// OnStdout and OnStderr receive output line by line, so the UI can stream
	// it live rather than waiting for the process to end.
	OnStdout func(string)
	OnStderr func(string)
	// Sudo asks for elevation where the environment says kc.sh needs it.
	Sudo bool
}

// ExecResult is how a command ended.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
}

// Artifact is one file streamed back from a target.
type Artifact struct {
	// Name is relative to the export directory.
	Name string
	Size int64
	Mode int64
}

// ArtifactSink receives files as they arrive. Streaming into a sink rather than
// returning a directory is what keeps a 2 GB export off the heap and lets the
// checksum be computed as bytes pass rather than in a second read.
type ArtifactSink interface {
	Artifact(ctx context.Context, a Artifact, r io.Reader) error
}

// SinkFunc adapts a function to an ArtifactSink.
type SinkFunc func(ctx context.Context, a Artifact, r io.Reader) error

func (f SinkFunc) Artifact(ctx context.Context, a Artifact, r io.Reader) error { return f(ctx, a, r) }

// Executor is the contract every target kind implements.
//
// Teardown is called from a defer that runs on every exit path, including
// panic. That is a convention with a test behind it, not a suggestion: a clone
// left running in a production namespace carries the same database credentials
// as the serving instance.
type Executor interface {
	// Probe reads facts about the target without changing anything.
	Probe(ctx context.Context) (TargetFacts, error)
	// Prepare materialises the execution context — an ephemeral clone, or the
	// target in place — and allocates the temp directory and ports.
	Prepare(ctx context.Context, opts PrepareOptions) (ExecContext, error)
	// Run executes a command in the execution context.
	Run(ctx context.Context, cmd Command) (ExecResult, error)
	// FetchDir streams a directory back, one file at a time.
	FetchDir(ctx context.Context, remote string, sink ArtifactSink) error
	// PushFile writes a file into the execution context, for the restore path.
	PushFile(ctx context.Context, remote string, size int64, r io.Reader) error
	// Teardown destroys the clone and the temp directory. It is idempotent and
	// safe to call when Prepare never ran.
	Teardown(ctx context.Context) error
	// Close releases connections.
	Close() error
}

// Orphan is an ephemeral clone a previous session left behind.
type Orphan struct {
	Environment string    `json:"environment"`
	Kind        string    `json:"kind"`
	Ref         string    `json:"ref"`
	JobID       string    `json:"jobId,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	State       string    `json:"state,omitempty"`
}

// Age is how long the orphan has been around, which is the number that decides
// whether it matters.
func (o Orphan) Age(now time.Time) time.Duration { return now.Sub(o.CreatedAt) }

// Sweeper is implemented by targets that can leave something behind. Local and
// SSH do not implement it, because there is no clone to orphan.
type Sweeper interface {
	// FindOrphans lists anything carrying PortCloak's own label.
	FindOrphans(ctx context.Context) ([]Orphan, error)
	// RemoveOrphan deletes one, on the operator's say-so. Removal is offered,
	// never automatic — the operator's cluster is not ours to garbage-collect
	// without asking.
	RemoveOrphan(ctx context.Context, ref string) error
}

// Labels PortCloak applies to everything it creates, and the only labels an
// ephemeral clone ever carries.
const (
	LabelEphemeral = "portcloak.io/ephemeral"
	LabelJob       = "portcloak.io/job"
	LabelRealm     = "portcloak.io/realm"
	LabelCreatedAt = "portcloak.io/created-at"
)

// WorkDirFor is the temp directory an export writes into, inside whichever
// execution context applies.
func WorkDirFor(jobID string) string { return "/tmp/portcloak-" + jobID }
