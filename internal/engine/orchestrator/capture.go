// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/crypto"
	"portcloak/internal/engine/kc"
	"portcloak/internal/engine/manifest"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/realm"
	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/snapshot"
	"portcloak/internal/engine/store"
	"portcloak/internal/engine/target"
	"portcloak/internal/engine/target/ports"
)

// CaptureRequest is what the wizard collected.
type CaptureRequest struct {
	Environment string
	// Realms may name several. Each becomes its own job and its own snapshot:
	// one snapshot holds exactly one realm, so several realms means several
	// independent, individually restorable bundles rather than one big one.
	Realms       []string
	Storage      string
	UsersMode    string
	UsersPerFile int
	// Verify runs the optional Admin API pass. Declining it, or the API being
	// unreachable, is recorded as skipped rather than treated as a failure.
	Verify bool
	// DetectDependencies enumerates themes and provider JARs.
	DetectDependencies bool
	// NoTransactionTimeout lets the export's transactions run without a time
	// limit. Transactions themselves cannot be turned off — Keycloak's export
	// is written as a sequence of them — so this lifts the limit that cancels
	// one, which is what kills a long export of a federated realm. It is the
	// operator's call per capture, never a default: the limit is also what
	// stops an export that has stopped making progress from holding a
	// connection to the serving database open indefinitely.
	NoTransactionTimeout bool
	Encryption           crypto.Config
}

// CaptureHandle is what a queued run hands back.
type CaptureHandle struct {
	JobIDs []string `json:"jobIds"`
	Realms []string `json:"realms"`
}

// maxExportAttempts bounds the retry of a port-conflict export. The race
// between releasing a port and Keycloak binding it is unavoidable, so this is
// a real retry rather than a defensive one — but it is bounded, because a
// machine with nothing free will not become free by trying harder.
const maxExportAttempts = 3

// Capture queues one job per realm and runs them in sequence.
//
// They run sequentially against one execution context, so an ephemeral clone is
// created once and reused across the queued realms rather than paid for each
// time. A clone is a parked execution context, not a per-realm resource.
func (o *Orchestrator) Capture(ctx context.Context, req CaptureRequest) (CaptureHandle, error) {
	cfg := o.opts.Config.Config()

	env, ok := cfg.Environment(req.Environment)
	if !ok {
		return CaptureHandle{}, resil.Fatal("start the capture",
			fmt.Sprintf("There is no environment called %q.", req.Environment), config.ErrNotFound)
	}
	st, ok := cfg.StorageByName(req.Storage)
	if !ok {
		return CaptureHandle{}, resil.Fatal("start the capture",
			fmt.Sprintf("There is no storage called %q.", req.Storage), config.ErrNotFound)
	}
	if len(req.Realms) == 0 {
		return CaptureHandle{}, resil.Fatal("start the capture", "No realm was selected.", nil)
	}

	// A storage marked encryption-required removes the opt-out for anything
	// written there. The check is here rather than only in the UI, so a config
	// edited by hand or a job resumed from an older build cannot bypass it.
	if st.EncryptionRequired && !req.Encryption.Enabled {
		return CaptureHandle{}, resil.Fatal("start the capture",
			fmt.Sprintf("The storage %q requires encryption, so a snapshot cannot be written there unencrypted.", st.Name), nil).
			WithAdvice("Turn encryption on for this capture, or choose a different storage.")
	}
	if err := req.Encryption.Validate(); err != nil {
		return CaptureHandle{}, err
	}

	prefs := o.opts.Config.Preferences()
	if req.UsersMode == "" {
		req.UsersMode = prefs.UsersMode
	}
	if req.UsersPerFile <= 0 {
		req.UsersPerFile = prefs.UsersPerFile
	}
	// The range is enforced here rather than only in the wizard, so a config
	// edited by hand or a job queued by an older build cannot put a page size
	// on the command line that kc.sh will choke on or that no transaction
	// finishes.
	req.UsersPerFile = kc.ClampUsersPerFile(req.UsersPerFile)

	handle := CaptureHandle{Realms: req.Realms}
	jobs := make([]*config.Job, 0, len(req.Realms))
	now := o.opts.Now()
	for _, r := range req.Realms {
		j := o.newJob(config.JobCapture, snapshot.NewID(now))
		j.Realm = r
		j.Environment = env.Name
		j.Storage = st.Name
		j.Source = env.Target()
		j.Encrypted = req.Encryption.Enabled
		if req.Encryption.Enabled {
			j.EncryptionMode = string(req.Encryption.Mode)
			j.Recipients = append([]string(nil), req.Encryption.Recipients...)
		}
		j.Provenance.EnvironmentKind = string(env.Kind)
		j.Provenance.CaptureMode = "offline-export"
		j.Message = "Queued."
		o.saveJob(j)
		jobs = append(jobs, j)
		handle.JobIDs = append(handle.JobIDs, j.ID)
	}

	// The run detaches: the caller is a UI action, and a capture takes minutes.
	go o.runCaptureBatch(context.WithoutCancel(ctx), env, st, req, jobs)
	return handle, nil
}

// runCaptureBatch executes the queued realms against one shared execution
// context.
func (o *Orchestrator) runCaptureBatch(ctx context.Context, env config.Environment, st config.Storage, req CaptureRequest, jobs []*config.Job) {
	batchCtx, cancelBatch := context.WithCancel(ctx)
	defer cancelBatch()

	// Cancelling any job in the batch cancels the shared context, because they
	// share one clone and one connection.
	for _, j := range jobs {
		defer o.track(j.ID, cancelBatch)()
	}

	exec, err := o.opts.Registry.Executor(env)
	if err != nil {
		o.failAll(jobs, obs.PhaseProbe, err)
		return
	}
	defer exec.Close() //nolint:errcheck // the job is over either way.

	blobs, err := o.opts.Registry.Store(st)
	if err != nil {
		o.failAll(jobs, obs.PhaseUpload, err)
		return
	}
	defer blobs.Close() //nolint:errcheck

	batchReporter := o.reporterFor(jobs...)

	// Probe once and share the result: the wizard already ran this probe, and
	// running it per realm would tell the operator the same thing four times.
	batchReporter.StartPhase(obs.PhaseProbe)
	facts, err := exec.Probe(batchCtx)
	if err != nil {
		o.failAll(jobs, obs.PhaseProbe, err)
		return
	}
	if !facts.OK() {
		blocker, _ := facts.FirstBlocker()
		o.failAll(jobs, obs.PhaseProbe, resil.Fatal("check the environment",
			fmt.Sprintf("%s: %s", blocker.Name, blocker.Value), nil).WithAdvice(blocker.Advice))
		return
	}
	batchReporter.CompletePhase(obs.PhaseProbe, facts.Summary())

	// Prepare once: on Docker and Kubernetes this materialises the ephemeral
	// clone, which is then reused for every queued realm and destroyed once.
	batchReporter.StartPhase(obs.PhaseClone)
	execCtx, err := exec.Prepare(batchCtx, target.PrepareOptions{
		JobID: jobs[0].ID, Realms: req.Realms, Purpose: "capture",
	})

	// Teardown runs from a defer covering every exit path, including panic and
	// including a Prepare that failed. Prepare can fail *after* the clone was
	// created — waiting for it to come up, or setting up its work directory —
	// and a clone left running carries the same database credentials as the
	// serving instance. So the defer is registered before the error is checked,
	// and the executor tears down whatever it recorded rather than whatever
	// Prepare managed to return.
	defer o.teardown(ctx, exec, execCtx, batchReporter, jobs)

	if err != nil {
		o.failAll(jobs, obs.PhaseClone, err)
		return
	}
	// The phase is closed either way. A target that exports in place creates no
	// clone, and leaving the step open left the pipeline showing a preparation
	// that never finished for the rest of the run.
	if execCtx.CloneRef != "" {
		batchReporter.CloneCreated(execCtx.CloneRef)
		batchReporter.CompletePhase(obs.PhaseClone, execCtx.CloneRef+" is running.")
	} else {
		batchReporter.CompletePhase(obs.PhaseClone,
			"This environment exports in place, so no clone was needed. The serving instance is not touched either way.")
	}

	var verifier Verifier
	if req.Verify || req.DetectDependencies {
		if o.opts.Registry.Verifier != nil {
			v, vErr := o.opts.Registry.Verifier(env)
			if vErr != nil {
				o.opts.Log.Info("the Admin API is not usable for this environment", "err", vErr)
			} else {
				verifier = v
			}
		}
	}

	cc := captureContext{
		env: env, storage: st, req: req, facts: facts,
		execCtx: execCtx, exec: exec, blobs: blobs, verifier: verifier,
		exportOptions: o.discoverOptions(batchCtx, exec, facts.KcPath, "export", env.Sudo, batchReporter),
	}

	// The run is in two passes, and the split is the point: everything that
	// needs the execution context happens first, so the clone is destroyed the
	// moment the artifacts are out. Packaging and uploading then run against
	// local files, with nothing parked in the operator's cluster while a
	// multi-gigabyte upload crawls over a bad link.
	collected := make([]*collected, len(jobs))
	for i, j := range jobs {
		if err := batchCtx.Err(); err != nil {
			// The shared clone is gone or the operator cancelled. Remaining
			// realms fail fast with that reason rather than each re-attempting
			// and re-failing against something that is not there.
			for _, rest := range jobs[i:] {
				_ = o.fail(rest, o.reporterFor(rest), obs.PhaseExport, err)
			}
			break
		}
		c, err := o.collect(batchCtx, cc, j)
		if err != nil {
			continue // collect already recorded the failure on the job.
		}
		collected[i] = c
	}

	// Teardown before packaging, and again from the deferred path above if
	// anything above returned early.
	o.teardown(ctx, exec, execCtx, batchReporter, jobs)

	for i, j := range jobs {
		c := collected[i]
		if c == nil {
			continue
		}
		o.sealAndStore(context.WithoutCancel(batchCtx), cc, j, c)
	}
}

// teardown destroys the execution context. It is idempotent, so the deferred
// path and the explicit call after collection cannot double-destroy.
func (o *Orchestrator) teardown(ctx context.Context, exec target.Executor, execCtx target.ExecContext, rep *obs.Reporter, jobs []*config.Job) {
	o.mu.Lock()
	if o.tornDown == nil {
		o.tornDown = map[string]bool{}
	}
	key := execCtx.WorkDir + "\x00" + execCtx.CloneRef
	if o.tornDown[key] {
		o.mu.Unlock()
		return
	}
	o.tornDown[key] = true
	o.mu.Unlock()

	rep.StartPhase(obs.PhaseTeardown)
	tdCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()

	if err := exec.Teardown(tdCtx); err != nil {
		// A failed teardown is raised prominently with the clone's identifier
		// so it can be removed by hand, and the orphan sweep retries it on the
		// next launch.
		o.opts.Log.Error("teardown failed", "clone", execCtx.CloneRef, "err", err)
		rep.FailPhase(obs.PhaseTeardown, err.Error())
		for _, j := range jobs {
			j.Append(config.LedgerEntry{
				Phase: string(obs.PhaseTeardown), Item: execCtx.CloneRef, Attempts: 1,
				LastError: err.Error(), Outcome: "left behind", At: o.opts.Now(),
			})
			o.saveJob(j)
		}
		return
	}
	if execCtx.CloneRef != "" {
		rep.CloneDestroyed(execCtx.CloneRef)
	}
	rep.CompletePhase(obs.PhaseTeardown, "Nothing was left behind.")
	for _, j := range jobs {
		j.Provenance.CloneRef = execCtx.CloneRef
		j.CompletePhase(string(obs.PhaseTeardown))
		o.saveJob(j)
	}
}

func (o *Orchestrator) failAll(jobs []*config.Job, phase obs.Phase, err error) {
	for _, j := range jobs {
		_ = o.fail(j, o.reporterFor(j), phase, err)
	}
}

type captureContext struct {
	env      config.Environment
	storage  config.Storage
	req      CaptureRequest
	facts    target.TargetFacts
	execCtx  target.ExecContext
	exec     target.Executor
	blobs    store.BlobStore
	verifier Verifier
	// exportOptions is what this kc.sh said its export command accepts,
	// discovered once for the batch because every realm runs the same binary.
	exportOptions kc.OptionSet
}

// discoverOptions asks kc.sh which options a subcommand takes, in the execution
// context the command will actually run in — the clone on Docker and
// Kubernetes, the host on local and SSH. That is the only place the answer is
// authoritative: it is a property of that binary, and kc.OptionSet records how
// much it has moved between releases.
//
// A failure here is not fatal. The caller falls back to passing no port
// options, which is the recoverable direction: see kc.portArgs.
func (o *Orchestrator) discoverOptions(
	ctx context.Context, exec target.Executor, kcPath, subcommand string, sudo bool, rep *obs.Reporter,
) kc.OptionSet {
	cmd, err := kc.BuildHelp(kcPath, subcommand)
	if err != nil {
		o.opts.Log.Info("kc.sh options could not be asked for", "subcommand", subcommand, "err", err)
		return nil
	}
	res, err := exec.Run(ctx, target.Command{Path: cmd.Path, Args: cmd.Args, Sudo: sudo})
	if err != nil || res.ExitCode != 0 {
		o.opts.Log.Info("kc.sh did not list its options; port options will be omitted",
			"subcommand", subcommand, "exit", res.ExitCode, "err", err)
		rep.Log("kc.sh did not list the options its " + subcommand +
			" command accepts, so none of the isolated ports will be passed to it.")
		return nil
	}
	opts := kc.ParseOptions(res.Stdout, res.Stderr)
	o.opts.Log.Info("kc.sh option support discovered",
		"subcommand", subcommand, "options", len(opts),
		"httpManagementPort", opts.Has("http-management-port"))
	return opts
}

// collected is what one realm produced while the execution context was alive.
type collected struct {
	builder  *snapshot.Builder
	manifest *manifest.Manifest
	started  time.Time
	rep      *obs.Reporter
}

// collect is the first pass: everything that needs the target.
func (o *Orchestrator) collect(ctx context.Context, cc captureContext, j *config.Job) (*collected, error) {
	rep := o.reporterFor(j)
	started := o.opts.Now()

	j.State = config.JobRunning
	j.StartedAt = &started
	j.Message = "Running."
	j.Provenance.ExecutionMode = string(cc.execCtx.Mode)
	j.Provenance.CloneRef = cc.execCtx.CloneRef
	j.Provenance.KeycloakVersion = cc.facts.KeycloakVersion
	o.saveJob(j)
	rep.JobState(string(config.JobRunning), "Capturing "+j.Realm+".")

	// Everything the job writes locally lives under the PortCloak home rather
	// than the system temp directory, because these files hold realm material.
	stageDir := o.home().WorkPath(j.ID, "stage")
	builder, err := snapshot.NewBuilder(stageDir)
	if err != nil {
		return nil, o.fail(j, rep, obs.PhasePackage, err)
	}

	realmDir := filepath.Join(cc.execCtx.WorkDir, j.Realm)
	layout, err := o.export(ctx, cc, j, rep, realmDir)
	if err != nil {
		_ = builder.Cleanup()
		return nil, o.fail(j, rep, obs.PhaseExport, err)
	}
	j.CompletePhase(string(obs.PhaseExport))

	// The user files are renamed on the way into the bundle so their numbers
	// are all the same width. kc.sh numbers them 0, 1, … 10, which anything
	// ordering names as text reads as 0, 1, 10, 2. What the snapshot carries is
	// the padded name, so a listing anywhere downstream — a startup import
	// scanning its directory, an operator's ls — is in the order the pages were
	// written.
	layout, renames := kc.PadUserFiles(layout)
	staged, err := o.fetch(ctx, cc, j, rep, builder, realmDir, renames)
	if err != nil {
		_ = builder.Cleanup()
		return nil, o.fail(j, rep, obs.PhaseFetch, err)
	}
	j.CompletePhase(string(obs.PhaseFetch))

	rep.StartPhase(obs.PhaseManifest)
	m, err := o.buildManifest(ctx, cc, j, rep, builder, staged, layout)
	if err != nil {
		_ = builder.Cleanup()
		return nil, o.fail(j, rep, obs.PhaseManifest, err)
	}
	rep.CompletePhase(obs.PhaseManifest, fmt.Sprintf("%d users, %d clients, %d key providers.",
		m.Counts.Users, m.Counts.Clients, m.Counts.KeyProviders))
	j.CompletePhase(string(obs.PhaseManifest))

	return &collected{builder: builder, manifest: m, started: started, rep: rep}, nil
}

// sealAndStore is the second pass: packaging and upload, which need nothing
// from the target and therefore run after the clone is gone.
func (o *Orchestrator) sealAndStore(ctx context.Context, cc captureContext, j *config.Job, c *collected) {
	defer c.builder.Cleanup() //nolint:errcheck // best effort; purge sweeps the rest.

	rep, m := c.rep, c.manifest
	sealed, err := o.seal(ctx, cc, j, rep, c.builder, m, c.started)
	if err != nil {
		_ = o.fail(j, rep, obs.PhasePackage, err)
		return
	}
	j.CompletePhase(string(obs.PhasePackage))

	if err := o.upload(ctx, cc, j, rep, m, sealed, c.started); err != nil {
		_ = o.fail(j, rep, obs.PhaseUpload, err)
		return
	}
	j.CompletePhase(string(obs.PhaseUpload))

	o.complete(j, rep, fmt.Sprintf("Captured %s: %d users, %d clients. %s",
		j.Realm, m.Counts.Users, m.Counts.Clients, m.Completeness.Verdict))

	_ = o.opts.Audit.Record(obs.AuditEntry{
		Action:      obs.ActionCapture,
		Outcome:     "captured",
		Realm:       j.Realm,
		SnapshotID:  j.SnapshotID,
		Environment: j.Environment,
		Storage:     j.Storage,
		Detail: fmt.Sprintf("%s in an %s · %s · %s",
			j.Provenance.CaptureMode, j.Provenance.ExecutionMode,
			encryptionLabel(cc.req.Encryption), m.Completeness.Verdict),
	})
	if !cc.req.Encryption.Enabled {
		// The choice to write in the clear is recorded, every time. An operator
		// must never be able to say afterwards that they did not realise the
		// file held unmasked secrets.
		_ = o.opts.Audit.Record(obs.AuditEntry{
			Action: obs.ActionEncryptionDeclin, Outcome: "declined",
			Realm: j.Realm, SnapshotID: j.SnapshotID, Storage: j.Storage,
			Detail: snapshot.UnencryptedWarning,
		})
	}
}

func encryptionLabel(c crypto.Config) string {
	if !c.Enabled {
		return "UNENCRYPTED"
	}
	return "encrypted · " + string(c.Mode)
}

// export runs offline kc.sh export, retrying a port conflict with fresh ports.
func (o *Orchestrator) export(ctx context.Context, cc captureContext, j *config.Job, rep *obs.Reporter, realmDir string) (kc.ExportLayout, error) {
	rep.StartPhase(obs.PhaseExport)

	portSet := cc.execCtx.Ports
	for attempt := 1; attempt <= maxExportAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return kc.ExportLayout{}, err
		}

		cmd, err := kc.BuildExport(kc.ExportRequest{
			KcPath:               cc.facts.KcPath,
			Dir:                  realmDir,
			Realm:                j.Realm,
			UsersMode:            kc.UsersMode(cc.req.UsersMode),
			UsersPerFile:         cc.req.UsersPerFile,
			Ports:                kc.Ports{HTTP: portSet.HTTP, HTTPS: portSet.HTTPS, Management: portSet.Management},
			Supported:            cc.exportOptions,
			NoTransactionTimeout: cc.req.NoTransactionTimeout,
		})
		if err != nil {
			return kc.ExportLayout{}, resil.Fatal("build the export command", err.Error(), err)
		}
		rep.Log(cmd.String())

		result, err := cc.exec.Run(ctx, target.Command{
			Path: cmd.Path, Args: cmd.Args, Env: cmd.Env, Sudo: cc.env.Sudo,
			OnStdout: rep.Log, OnStderr: rep.Log,
		})
		if err != nil {
			return kc.ExportLayout{}, err
		}

		outcome := kc.ParseOutput(result.Stdout, result.Stderr)
		outcome.ExitCode = result.ExitCode
		for _, w := range outcome.Warnings {
			j.Append(config.LedgerEntry{
				Phase: string(obs.PhaseExport), Item: "kc.sh warning",
				Attempts: 1, LastError: w, Outcome: "warning", At: o.opts.Now(),
			})
		}

		if result.ExitCode == 0 {
			layout, err := o.readLayout(ctx, cc, j.Realm, realmDir)
			if err != nil {
				return kc.ExportLayout{}, err
			}
			if !layout.Complete() {
				// kc.sh can exit zero having written nothing usable. Treating
				// that as success would ship a snapshot missing its realm.
				return kc.ExportLayout{}, resil.Fatal("export the realm",
					fmt.Sprintf("kc.sh reported success but produced no realm file for %q.", j.Realm), nil).
					WithAdvice("Check that the realm name is exactly right. The export names its output after it.")
			}
			rep.CompletePhase(obs.PhaseExport, fmt.Sprintf("%d file(s) written.", 1+len(layout.UserFiles)+len(layout.Other)))
			return layout, nil
		}

		message, advice, retryable := kc.ClassifyFailure(j.Realm, outcome, result.Stderr)
		if outcome.BindConflict && !cmd.PortsPassed {
			// Reallocating is the answer only when the ports can be handed to
			// kc.sh. This build takes no port option, so the same conflict
			// would come back three times and then be reported as a port race
			// that PortCloak had never had any say in.
			retryable = false
			advice = "This Keycloak's export command accepts no port options, so PortCloak cannot move the export off the port that is taken. Stop whatever is holding it, or capture from an environment where the export runs in its own network namespace. Docker and Kubernetes do."
		}
		if !retryable || !outcome.BindConflict || attempt == maxExportAttempts {
			return kc.ExportLayout{}, resil.Fatal("export the realm", message, nil).WithAdvice(advice)
		}

		// The port race is the one export failure worth retrying, and it is
		// retried with newly allocated ports rather than the same ones.
		rep.Retry("export", attempt, 0, message)
		fresh, allocErr := ports.Allocate()
		if allocErr != nil {
			return kc.ExportLayout{}, allocErr
		}
		portSet = fresh
	}
	return kc.ExportLayout{}, resil.Fatal("export the realm",
		"The export could not find free ports after several attempts.", nil)
}

// readLayout lists what the export actually produced, inside whichever
// execution context applies.
func (o *Orchestrator) readLayout(ctx context.Context, cc captureContext, realmName, dir string) (kc.ExportLayout, error) {
	var names []string
	err := cc.exec.FetchDir(ctx, dir, target.SinkFunc(func(_ context.Context, a target.Artifact, r io.Reader) error {
		names = append(names, a.Name)
		// The listing pass must still drain each artifact, or a streaming
		// transport is left mid-record.
		_, err := io.Copy(io.Discard, r)
		return err
	}))
	if err != nil {
		return kc.ExportLayout{}, err
	}
	return kc.ReadLayout(realmName, names), nil
}

// fetch streams the export back into the snapshot builder, hashing as bytes
// pass rather than in a second read.
func (o *Orchestrator) fetch(ctx context.Context, cc captureContext, j *config.Job, rep *obs.Reporter, builder *snapshot.Builder, realmDir string, renames map[string]string) ([]string, error) {
	rep.StartPhase(obs.PhaseFetch)

	var staged []string
	var bytesSeen int64
	err := cc.exec.FetchDir(ctx, realmDir, target.SinkFunc(func(fctx context.Context, a target.Artifact, r io.Reader) error {
		carried := a.Name
		if to, ok := renames[carried]; ok {
			carried = to
		}
		name := snapshot.RealmDir + carried
		d, err := builder.Stage(fctx, name, r)
		if err != nil {
			return err
		}
		staged = append(staged, name)
		bytesSeen += d.Size

		// Per-artifact checkpointing is what makes a dropped link cost one file
		// rather than the whole fetch.
		j.Checkpoint = &config.Checkpoint{
			Stage:            string(obs.PhaseFetch),
			FetchedArtifacts: staged,
			UpdatedAt:        o.opts.Now(),
		}
		o.saveJob(j)
		// The name reported is the one the snapshot holds, not the one kc.sh
		// wrote, so the log and the bundle agree.
		rep.Progress(bytesSeen, 0, "bytes", carried)
		return nil
	}))
	if err != nil {
		return nil, err
	}
	rep.CompletePhase(obs.PhaseFetch, fmt.Sprintf("%d file(s), %s.", len(staged), humanBytes(bytesSeen)))
	return staged, nil
}

// buildManifest parses the staged export into the inventory, running the
// optional verification pass if one is available.
func (o *Orchestrator) buildManifest(ctx context.Context, cc captureContext, j *config.Job, rep *obs.Reporter, builder *snapshot.Builder, staged []string, layout kc.ExportLayout) (*manifest.Manifest, error) {
	realmFile := ""
	var userFiles []string
	for _, name := range staged {
		base := strings.TrimPrefix(name, snapshot.RealmDir)
		switch base {
		case layout.RealmFile:
			realmFile = filepath.Join(builder.Dir(), filepath.FromSlash(name))
		default:
			for _, uf := range layout.UserFiles {
				if base == uf {
					userFiles = append(userFiles, filepath.Join(builder.Dir(), filepath.FromSlash(name)))
				}
			}
		}
	}
	if realmFile == "" {
		return nil, resil.Fatal("read the export",
			fmt.Sprintf("The realm file for %q is not in what was collected.", j.Realm), nil)
	}

	source := manifest.Source{
		EnvironmentName:    cc.env.Name,
		Kind:               string(cc.env.Kind),
		KeycloakVersion:    cc.facts.KeycloakVersion,
		CaptureMode:        "offline-export",
		ExecutionMode:      string(cc.execCtx.Mode),
		CloneRef:           cc.execCtx.CloneRef,
		SecretVerification: "skipped",
		DependencyScan:     "skipped",
		UsersMode:          cc.req.UsersMode,
	}

	rep2, err := realm.Load(realmFile)
	if err != nil {
		return nil, resil.Fatal("read the export", err.Error(), err)
	}

	opts := manifest.BuildOptions{
		Source:    source,
		UserFiles: userFiles,
		Progress: func(file string, users int) {
			rep.Progress(int64(users), 0, "users", filepath.Base(file))
		},
	}

	// The Admin API pass is optional and strictly secondary. If it is
	// unreachable the capture still succeeds and the report says verification
	// was skipped — which is a normal condition, not a fault.
	if cc.verifier != nil {
		rep.StartPhase(obs.PhaseVerify)
		if cc.verifier.Reachable(ctx) {
			if cc.req.Verify {
				ledger, lerr := o.ledgerFor(ctx, rep2, source)
				if lerr == nil {
					masked, verr := cc.verifier.VerifySecrets(ctx, j.Realm, ledger)
					if verr != nil {
						o.opts.Log.Info("secret verification did not complete", "err", verr)
						source.SecretVerification = "skipped"
					} else {
						opts.VerificationRan = true
						opts.MaskedLocations = masked
						source.SecretVerification = "passed"
						if len(masked) > 0 {
							source.SecretVerification = "partial"
						}
					}
				}
			}
			if cc.req.DetectDependencies {
				deps, derr := cc.verifier.DetectDependencies(ctx, j.Realm, rep2)
				if derr != nil {
					o.opts.Log.Info("dependency detection did not complete", "err", derr)
				} else {
					opts.DependencyScanRan = true
					opts.Dependencies = deps
					source.DependencyScan = "completed"
				}
			}
			rep.CompletePhase(obs.PhaseVerify, verificationSummary(source))
		} else {
			rep.CompletePhase(obs.PhaseVerify,
				"The Admin API was not reachable, so secret verification and dependency detection were skipped. The export itself is unaffected.")
		}
		j.CompletePhase(string(obs.PhaseVerify))
	}
	opts.Source = source

	m, err := manifest.Build(ctx, rep2, opts)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ledgerFor builds just the secret ledger, which the verifier needs before the
// full manifest exists.
func (o *Orchestrator) ledgerFor(ctx context.Context, rep *realm.Representation, source manifest.Source) ([]manifest.Secret, error) {
	m, err := manifest.Build(ctx, rep, manifest.BuildOptions{Source: source})
	if err != nil {
		return nil, err
	}
	return m.Secrets, nil
}

func verificationSummary(s manifest.Source) string {
	switch s.SecretVerification {
	case "passed":
		return "Every carried secret was confirmed to be a real value."
	case "partial":
		return "Some secrets were exported masked and are flagged in the report."
	default:
		return "Verification was skipped."
	}
}

// seal writes the sealed bundle to a local working file.
//
// The bundle is kept locally until the upload succeeds, so a failed upload
// costs the transfer rather than the capture — the expensive part is already
// done and on disk.
func (o *Orchestrator) seal(ctx context.Context, cc captureContext, j *config.Job, rep *obs.Reporter, builder *snapshot.Builder, m *manifest.Manifest, createdAt time.Time) (sealResult, error) {
	rep.StartPhase(obs.PhasePackage)

	if _, err := builder.Document(snapshot.ManifestPath, m); err != nil {
		return sealResult{}, err
	}
	if _, err := builder.Document(snapshot.DependenciesPath, m.ExternalDependencies); err != nil {
		return sealResult{}, err
	}
	provenance := snapshot.Provenance{
		EnvironmentName:    cc.env.Name,
		EnvironmentKind:    string(cc.env.Kind),
		Target:             cc.env.Target(),
		KeycloakVersion:    cc.facts.KeycloakVersion,
		CaptureMode:        "offline-export",
		ExecutionMode:      string(cc.execCtx.Mode),
		CloneRef:           cc.execCtx.CloneRef,
		Ports:              cc.execCtx.Ports.String(),
		UsersMode:          cc.req.UsersMode,
		StartedAt:          createdAt,
		FinishedAt:         o.opts.Now(),
		SecretVerification: m.Source.SecretVerification,
		DependencyScan:     m.Source.DependencyScan,
		JobID:              j.ID,
	}
	tree := builder.Tree()
	provenance.IntegrityRoot = tree.Root
	if _, err := builder.Document(snapshot.ProvenancePath, provenance); err != nil {
		return sealResult{}, err
	}
	// The tree is recomputed after provenance is staged, so it covers it.
	tree = builder.Tree()
	if _, err := builder.Document(snapshot.IntegrityPath, tree); err != nil {
		return sealResult{}, err
	}

	sealer, err := crypto.NewSealer(cc.req.Encryption)
	if err != nil {
		return sealResult{}, err
	}
	envelope := snapshot.Envelope{
		SchemaVersion:    snapshot.SchemaVersion,
		SnapshotID:       j.ID,
		Realm:            j.Realm,
		CreatedAt:        createdAt.UTC(),
		PortCloakVersion: o.opts.Version,
		KeycloakVersion:  cc.facts.KeycloakVersion,
		IntegrityRoot:    tree.Root,
		ArtifactCount:    len(tree.Artifacts),
		PayloadBytes:     builder.PayloadBytes(),
	}
	if sealer != nil {
		envelope.Encryption = sealer.Describe()
	}
	if _, err := builder.Document(snapshot.EnvelopePath, envelope); err != nil {
		return sealResult{}, err
	}

	bundlePath := o.home().WorkPath(j.ID, j.ID+store.BundleExt)
	if err := os.MkdirAll(filepath.Dir(bundlePath), 0o700); err != nil {
		return sealResult{}, err
	}
	f, err := os.OpenFile(bundlePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return sealResult{}, err
	}

	var res snapshot.SealResult
	if sealer != nil {
		res, err = builder.Seal(ctx, f, sealer)
	} else {
		res, err = builder.Seal(ctx, f, nil)
	}
	if err != nil {
		_ = f.Close()
		_ = os.Remove(bundlePath)
		return sealResult{}, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return sealResult{}, err
	}
	if err := f.Close(); err != nil {
		return sealResult{}, err
	}

	// A bundle that cannot be decrypted should be discovered now, not eighteen
	// months later during an incident.
	if err := crypto.VerifyDecryptable(ctx, func() (io.ReadCloser, error) {
		return os.Open(bundlePath)
	}, cc.req.Encryption); err != nil {
		_ = os.Remove(bundlePath)
		return sealResult{}, err
	}

	j.SnapshotID = j.ID
	j.Checkpoint = &config.Checkpoint{
		Stage: string(obs.PhasePackage), LocalBundle: bundlePath,
		Digest: res.Digest, UpdatedAt: o.opts.Now(),
	}
	o.saveJob(j)

	rep.CompletePhase(obs.PhasePackage, fmt.Sprintf("%s sealed%s.", humanBytes(res.Size), encryptionSuffix(cc.req.Encryption)))
	return sealResult{path: bundlePath, seal: res, envelope: envelope}, nil
}

func encryptionSuffix(c crypto.Config) string {
	if !c.Enabled {
		return " · unencrypted"
	}
	return " and encrypted"
}

type sealResult struct {
	path     string
	seal     snapshot.SealResult
	envelope snapshot.Envelope
}

// upload writes the bundle and both sidecars to storage.
func (o *Orchestrator) upload(ctx context.Context, cc captureContext, j *config.Job, rep *obs.Reporter, m *manifest.Manifest, sealed sealResult, createdAt time.Time) error {
	rep.StartPhase(obs.PhaseUpload)

	layout := store.NewLayout(cc.storage.Prefix)
	if cc.storage.Kind == config.StoreDisk || cc.storage.Kind == config.StoreSSH {
		layout = store.NewLayout("")
	}
	bundleKey := layout.BundleKey(j.Realm, createdAt, j.ID)

	info, err := os.Stat(sealed.path)
	if err != nil {
		return err
	}
	f, err := os.Open(sealed.path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // read-only.

	// The file is handed over whole and the checkpoint travels as a hint. The
	// backend reconciles it against what the destination actually holds and
	// positions the reader itself, because the destination is the only
	// authority on where a transfer got to.
	var offset int64
	var hashState []byte
	if cp := j.Checkpoint; cp != nil && cp.Stage == string(obs.PhaseUpload) && cp.Key == bundleKey {
		offset, hashState = cp.ByteOffset, cp.HashState
	}

	// LocalBundle travels with the upload checkpoint. Without it a resume has
	// nothing to resume from and would have to re-export the realm — which is
	// the expensive half, and the half that was already done.
	checkpoint := func(written int64, state []byte) {
		j.Checkpoint = &config.Checkpoint{
			Stage: string(obs.PhaseUpload), Key: bundleKey,
			ByteOffset: written, Digest: sealed.seal.Digest, HashState: state,
			LocalBundle: sealed.path, UpdatedAt: o.opts.Now(),
		}
	}
	checkpoint(offset, hashState)

	_, err = cc.blobs.Put(ctx, bundleKey, f, store.PutOptions{
		Size:      info.Size(),
		Digest:    sealed.seal.Digest,
		Offset:    offset,
		HashState: hashState,
		Progress: func(written int64) {
			rep.Progress(written, info.Size(), "bytes", bundleKey)
		},
		Checkpoint: checkpoint,
	})
	if err != nil {
		o.saveJob(j)
		return err
	}

	// The sidecars are what make the library browsable with no key at all, so
	// they are written next to the bundle rather than inside it.
	sidecar := m.BuildSidecar(j.ID, createdAt.UTC().Format(time.RFC3339), o.opts.Version,
		sealed.envelope.Encryption.Enabled, string(sealed.envelope.Encryption.Mode),
		sealed.seal.Size, sealed.seal.Root)
	sidecarBytes, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		return err
	}
	manifestKey := layout.ManifestKey(j.Realm, createdAt, j.ID)
	if _, err := cc.blobs.Put(ctx, manifestKey, strings.NewReader(string(sidecarBytes)+"\n"),
		store.PutOptions{Size: int64(len(sidecarBytes) + 1)}); err != nil {
		return err
	}

	digestKey := layout.DigestKey(j.Realm, createdAt, j.ID)
	digestLine := fmt.Sprintf("%s  %s\n", sealed.seal.Digest, filepath.Base(bundleKey))
	if _, err := cc.blobs.Put(ctx, digestKey, strings.NewReader(digestLine),
		store.PutOptions{Size: int64(len(digestLine))}); err != nil {
		return err
	}

	j.StorageKey = bundleKey
	j.Checkpoint = nil
	o.saveJob(j)

	// The sealed local copy has served its purpose. It holds the same unmasked
	// secrets the bundle does, so it does not linger.
	_ = os.Remove(sealed.path)

	rep.CompletePhase(obs.PhaseUpload, bundleKey)
	return nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
