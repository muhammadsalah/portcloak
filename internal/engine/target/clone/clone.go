// Package clone owns ephemeral clone execution: deriving a throwaway copy of a
// serving workload, running the export inside it, and guaranteeing it is
// destroyed.
//
// The strip list below is total rather than selective, and that is deliberate.
// A pod inheriting app=keycloak is picked up by the production Service and
// starts receiving real user traffic into a container that serves nothing. A
// later change that copies "just the useful labels" brings that straight back,
// so nothing is inherited and only PortCloak's own labels are applied.
//
// The clone lifecycle lives here once, and Docker and Kubernetes plug a
// Platform into it. Teardown is therefore one tested implementation rather than
// two, which matters because a leaked clone in a production namespace is a real
// operational incident.
package clone

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/target"
)

// Spec is a derived clone, in the parts both platforms share.
type Spec struct {
	JobID string
	Image string
	// Command replaces the workload's own entrypoint with a hang, so the clone
	// boots nothing and serves nothing.
	Command []string
	// Env is the serving workload's environment, including the database URL and
	// credentials — which is what makes the clone able to read the realm, and
	// why leaving one running is a standing credential exposure.
	Env map[string]string
	// Labels are PortCloak's own and nothing else.
	Labels map[string]string
	// WorkDir is the temp directory inside the clone.
	WorkDir string
	// Kept records what was deliberately preserved from the source, for the
	// golden-file derivation test.
	Kept []string
	// Stripped records what was deliberately removed.
	Stripped []string
}

// StrippedFields is the total list of what a derived clone never inherits.
//
// ownerReferences, resourceVersion, uid and controller references go because a
// clone that keeps them can be adopted or garbage-collected by the wrong
// controller. nodeName and status go because they describe a pod that already
// exists. Selector labels go because of the trap this package exists to avoid.
var StrippedFields = []string{
	"labels",
	"annotations",
	"ownerReferences",
	"resourceVersion",
	"uid",
	"selfLink",
	"creationTimestamp",
	"nodeName",
	"status",
	"livenessProbe",
	"readinessProbe",
	"startupProbe",
	"lifecycle",
	"ports",
	"networkAliases",
	"healthcheck",
	"restartPolicy",
	"hostname",
}

// KeptFields is the total list of what a derived clone deliberately preserves.
//
// A clone that will not schedule, or that OpenShift's SCC rejects, is useless —
// so the things that make it admissible and able to reach the database all
// survive.
var KeptFields = []string{
	"image",
	"env",
	"envFrom",
	"imagePullSecrets",
	"serviceAccountName",
	"volumes",
	"volumeMounts",
	"securityContext",
	"nodeSelector",
	"tolerations",
	"affinity",
	"resources",
	"networks",
	"dnsPolicy",
	"dnsConfig",
	"terminationGracePeriodSeconds",
}

// Labels are the only labels a clone ever carries.
func Labels(jobID, realm string, createdAt time.Time) map[string]string {
	l := map[string]string{
		target.LabelEphemeral: "true",
		target.LabelJob:       jobID,
		target.LabelCreatedAt: createdAt.UTC().Format(time.RFC3339),
	}
	if realm != "" {
		l[target.LabelRealm] = sanitiseLabelValue(realm)
	}
	return l
}

// sanitiseLabelValue makes a realm name safe as a Kubernetes label value, which
// is stricter than a realm name is.
func sanitiseLabelValue(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	if len(out) > 63 {
		out = out[:63]
	}
	if out == "" {
		return "realm"
	}
	return out
}

// HangCommand is what replaces the workload's entrypoint.
//
// The container is hung and exec'd into rather than being given `kc.sh export`
// as its command. That buys three things: the export's output streams back
// interactively for live progress; a failed export can be retried in place
// without paying container startup again; and artifacts can be streamed out of
// a container that is still alive, rather than racing a terminating one for its
// filesystem.
func HangCommand() []string {
	// The trap keeps the clone responsive to a delete, and the loop avoids
	// relying on `sleep infinity`, which is not present in every base image.
	return []string{"/bin/sh", "-c", "trap 'exit 0' TERM INT; while :; do sleep 3600 & wait $!; done"}
}

// Name is the clone's own name, derived from the job.
func Name(jobID string) string {
	return "portcloak-" + strings.ToLower(jobID)
}

// Platform is what a container runtime has to provide for the clone lifecycle
// to work against it.
type Platform interface {
	// Inspect reads the serving workload, read-only, and derives a spec.
	Inspect(ctx context.Context, jobID string, realms []string) (Spec, error)
	// Create materialises the clone and returns its reference.
	Create(ctx context.Context, spec Spec) (ref string, err error)
	// WaitRunning blocks until the clone is up, or reports why it will not be.
	WaitRunning(ctx context.Context, ref string) error
	// Exec runs a command inside the clone.
	Exec(ctx context.Context, ref string, cmd target.Command) (target.ExecResult, error)
	// CopyOut streams a directory out of the clone.
	CopyOut(ctx context.Context, ref, path string, sink target.ArtifactSink) error
	// CopyIn writes a file into the clone, for the restore path.
	CopyIn(ctx context.Context, ref, path string, size int64, r io.Reader) error
	// Destroy removes the clone. It must succeed when the clone is already gone.
	Destroy(ctx context.Context, ref string) error
	// Probe reads facts without creating anything.
	Probe(ctx context.Context) (target.TargetFacts, error)
	// FindOrphans lists anything carrying PortCloak's own label.
	FindOrphans(ctx context.Context) ([]target.Orphan, error)
	// Close releases the client.
	Close() error
}

// Executor is the target.Executor built on a Platform. Docker and Kubernetes
// both use it unchanged.
type Executor struct {
	platform Platform

	mu       sync.Mutex
	ref      string
	spec     Spec
	created  bool
	tornDown bool
}

// NewExecutor wraps a platform.
func NewExecutor(p Platform) *Executor { return &Executor{platform: p} }

// Probe reads facts without creating the clone it reports as feasible.
func (e *Executor) Probe(ctx context.Context) (target.TargetFacts, error) {
	return e.platform.Probe(ctx)
}

// Prepare derives and materialises the clone, then waits for it to be running.
func (e *Executor) Prepare(ctx context.Context, opts target.PrepareOptions) (target.ExecContext, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	spec, err := e.platform.Inspect(ctx, opts.JobID, opts.Realms)
	if err != nil {
		return target.ExecContext{}, err
	}
	spec.Command = HangCommand()
	spec.Labels = Labels(opts.JobID, firstRealm(opts.Realms), time.Now())
	if spec.WorkDir == "" {
		spec.WorkDir = target.WorkDirFor(opts.JobID)
	}
	spec.Stripped = append([]string(nil), StrippedFields...)
	spec.Kept = append([]string(nil), KeptFields...)

	ref, err := e.platform.Create(ctx, spec)
	if err != nil {
		return target.ExecContext{}, err
	}
	// The reference is recorded before waiting, so a clone that never became
	// ready is still torn down.
	e.ref, e.spec, e.created = ref, spec, true

	if err := e.platform.WaitRunning(ctx, ref); err != nil {
		return target.ExecContext{}, err
	}

	// The work directory is created here rather than left to whatever runs
	// first, so a clone arrives in the same state a local or SSH target does:
	// Prepare hands back a directory that exists. 0700 because it holds
	// unmasked secrets from the moment kc.sh starts writing into it.
	mk, err := e.platform.Exec(ctx, ref, target.Command{
		Path: "/bin/sh",
		Args: []string{"-c", "mkdir -p " + shellQuote(spec.WorkDir) + " && chmod 700 " + shellQuote(spec.WorkDir)},
	})
	if err != nil {
		return target.ExecContext{}, err
	}
	if mk.ExitCode != 0 {
		return target.ExecContext{}, resil.Fatal("prepare the ephemeral clone",
			fmt.Sprintf("PortCloak could not create %s inside %s.", spec.WorkDir, ref), nil).
			WithAdvice("The clone's image may have no writable /tmp, or no shell to create it with.")
	}

	// The port flags are passed even here, where the clone's own network
	// namespace makes a collision impossible. It costs nothing and keeps one
	// code path serving all four target kinds.
	return target.ExecContext{
		Mode:     target.ModeEphemeralClone,
		CloneRef: ref,
		WorkDir:  spec.WorkDir,
		Ports:    target.PortSet{HTTP: 8080, HTTPS: 8443, Management: 9000},
	}, nil
}

// Run execs a command inside the clone.
func (e *Executor) Run(ctx context.Context, cmd target.Command) (target.ExecResult, error) {
	ref, ok := e.currentRef()
	if !ok {
		return target.ExecResult{}, resil.Fatal("run the export",
			"There is no ephemeral clone to run in. Prepare did not complete.", nil)
	}
	return e.platform.Exec(ctx, ref, cmd)
}

// FetchDir streams the export out of the clone while it is still alive.
func (e *Executor) FetchDir(ctx context.Context, remote string, sink target.ArtifactSink) error {
	ref, ok := e.currentRef()
	if !ok {
		return resil.Fatal("collect the exported files",
			"There is no ephemeral clone to read from.", nil)
	}
	return e.platform.CopyOut(ctx, ref, remote, sink)
}

// PushFile writes a file into the clone, for the restore path.
func (e *Executor) PushFile(ctx context.Context, remote string, size int64, r io.Reader) error {
	ref, ok := e.currentRef()
	if !ok {
		return resil.Fatal("send the snapshot", "There is no ephemeral clone to write to.", nil)
	}
	return e.platform.CopyIn(ctx, ref, remote, size, r)
}

// Teardown destroys the clone. It is idempotent and safe when Prepare never
// ran, so it can be called unconditionally from a defer.
func (e *Executor) Teardown(ctx context.Context) error {
	e.mu.Lock()
	ref, created, already := e.ref, e.created, e.tornDown
	if created && ref != "" {
		// The flag is set only when there is something to destroy. Marking it
		// on a no-op call would let a defensive early Teardown silently
		// disarm the real one that follows.
		e.tornDown = true
	}
	e.mu.Unlock()

	if !created || already || ref == "" {
		return nil
	}
	if err := e.platform.Destroy(ctx, ref); err != nil {
		// The identifier is in the message so the operator can remove it by
		// hand, and the orphan sweep retries on the next launch.
		return resil.Fatal("destroy the ephemeral clone",
			fmt.Sprintf("PortCloak could not remove %s. It holds the same database credentials as the serving instance, so remove it by hand.", ref), err).
			WithAdvice("PortCloak will offer to remove it again the next time it starts.")
	}
	return nil
}

// Close releases the platform client.
func (e *Executor) Close() error { return e.platform.Close() }

// FindOrphans lists clones a previous session left behind.
func (e *Executor) FindOrphans(ctx context.Context) ([]target.Orphan, error) {
	orphans, err := e.platform.FindOrphans(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].CreatedAt.Before(orphans[j].CreatedAt) })
	return orphans, nil
}

// RemoveOrphan deletes one, on the operator's say-so. Removal is offered, never
// automatic — the operator's cluster is not ours to garbage-collect without
// asking.
func (e *Executor) RemoveOrphan(ctx context.Context, ref string) error {
	return e.platform.Destroy(ctx, ref)
}

// CloneRef is the clone's identifier, for provenance.
func (e *Executor) CloneRef() string {
	ref, _ := e.currentRef()
	return ref
}

// Spec is the derived specification, for the derivation test.
func (e *Executor) Spec() Spec {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.spec
}

func (e *Executor) currentRef() (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.created || e.tornDown {
		return "", false
	}
	return e.ref, true
}

func firstRealm(realms []string) string {
	if len(realms) == 0 {
		return ""
	}
	return realms[0]
}

// DescribeOrphan renders an orphan for the maintenance screen: what it is, how
// old, and why it matters.
func DescribeOrphan(o target.Orphan, now time.Time) string {
	age := o.Age(now)
	return fmt.Sprintf("%s · created %s ago · %s", o.Ref, roundAge(age), strings.ToLower(o.State))
}

func roundAge(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	case d >= 2*time.Minute:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	default:
		return "moments"
	}
}

// shellQuote wraps a path for /bin/sh. Work directories are derived from a job
// id and never contain a quote, but the export writes secrets into whatever
// this returns, so it is quoted rather than assumed safe.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
