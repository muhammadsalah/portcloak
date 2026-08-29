// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/inspect"
	"portcloak/internal/engine/kc"
	"portcloak/internal/engine/manifest"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/snapshot"
	"portcloak/internal/engine/target"
)

// Destination is what a restore needs to know about a live target realm.
type Destination interface {
	ReadRealm(ctx context.Context, realmName string) (RealmShape, error)
	PartialImport(ctx context.Context, realmName string, body []byte, policy string) (int, int, int, error)
	Reachable(ctx context.Context) bool
}

// RealmShape is the destination realm as it stands.
type RealmShape struct {
	Exists            bool
	Users             int
	Clients           int
	ClientScopes      int
	RealmRoles        int
	Groups            int
	IdentityProviders int
	Federations       int
	KeyIDs            []string
}

// DestinationFactory builds the Admin API view of a destination environment.
type DestinationFactory func(env config.Environment) (Destination, error)

// Precondition is one thing the destination is expected to already have.
//
// The step is informative only. Nothing is checked off and nothing is blocked:
// the operator manages these environments and is assumed to know what is
// deployed where.
type Precondition struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	DetectedAt  string `json:"detectedAt,omitempty"`
	Action      string `json:"action"`
	Consequence string `json:"consequence"`
}

// PreconditionReport is the whole preconditions step.
type PreconditionReport struct {
	Summary      string         `json:"summary"`
	Dependencies []Precondition `json:"dependencies"`
	// Checked is false when dependency detection did not run at capture time.
	// Shown as "not checked", never as "none": absence of data is not absence
	// of dependencies.
	Checked bool `json:"checked"`
	// IntegrityPassed and Decrypted are shown alongside as already-passed items.
	IntegrityPassed bool `json:"integrityPassed"`
	Decrypted       bool `json:"decrypted"`
	// Blocks is always false. It is a field rather than an omission so the
	// frontend cannot accidentally infer a gate that does not exist.
	Blocks bool `json:"blocks"`
}

// Preconditions builds the informative step from an open snapshot.
func Preconditions(s *inspect.Session) PreconditionReport {
	r := PreconditionReport{
		IntegrityPassed: s.Verify.OK,
		Decrypted:       s.Envelope.Encryption.Enabled,
		Blocks:          false,
	}
	r.Checked = s.Manifest.Source.DependencyScan == "completed"

	for _, d := range s.Dependencies() {
		r.Dependencies = append(r.Dependencies, Precondition{
			Type:        string(d.Type),
			Name:        d.Name,
			DetectedAt:  d.DetectedAt,
			Action:      d.Action,
			Consequence: d.Consequence,
		})
	}

	switch {
	case !r.Checked:
		r.Summary = "Dependency detection did not run when this snapshot was captured, so PortCloak cannot say whether this realm needs anything the destination does not have."
	case len(r.Dependencies) == 0:
		r.Summary = "This realm references no themes, provider JARs or keystore files outside the realm itself."
	default:
		r.Summary = fmt.Sprintf("This realm expects %d item%s to already exist on the destination. A realm referencing a missing theme or authenticator imports cleanly and then fails at login.",
			len(r.Dependencies), plural(len(r.Dependencies)))
	}
	return r
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// DiffCategory is one line of the dry run.
type DiffCategory struct {
	Category   string `json:"category"`
	Create     int    `json:"create"`
	Overwrite  int    `json:"overwrite"`
	LeaveAlone int    `json:"leaveAlone"`
	Note       string `json:"note,omitempty"`
	// NoteLevel is info, caution or warning, so the UI can colour a note that
	// deserves attention without inventing the judgement itself.
	NoteLevel string `json:"noteLevel,omitempty"`
}

// DryRun is the preview of what an import would do.
type DryRun struct {
	Strategy     string         `json:"strategy"`
	Available    bool           `json:"available"`
	TargetExists bool           `json:"targetExists"`
	Categories   []DiffCategory `json:"categories"`
	Summary      string         `json:"summary"`
	// Caveat states the thing an operator most needs to know about a preview of
	// a non-transactional import.
	Caveat string `json:"caveat"`
	// Unavailable explains why there is no preview, when there is none.
	Unavailable string `json:"unavailable,omitempty"`
}

// ComputeDryRun diffs a snapshot against the live destination realm.
//
// The diff is computed for the strategy actually selected. A preview computed
// under a different strategy would be worse than no preview.
func ComputeDryRun(ctx context.Context, s *inspect.Session, dest Destination, strategy kc.ImportStrategy) DryRun {
	d := DryRun{
		Strategy: string(strategy),
		Caveat:   "Keycloak's import is not transactional. If it fails part-way, this preview is also the list of what may already have been written.",
	}

	if dest == nil || !dest.Reachable(ctx) {
		// Not silently skipped: the operator is told the import will proceed
		// without a preview.
		d.Unavailable = "The destination's Admin API could not be read, so PortCloak cannot preview the changes. The import can still proceed."
		return d
	}

	shape, err := dest.ReadRealm(ctx, s.Realm)
	if err != nil {
		d.Unavailable = "The destination realm could not be read: " + err.Error()
		return d
	}
	d.Available = true
	d.TargetExists = shape.Exists

	m := s.Manifest
	add := func(category string, incoming, existing int, note, level string) {
		row := DiffCategory{Category: category, Note: note, NoteLevel: level}
		switch {
		case !shape.Exists:
			// Diffing a realm that does not exist is the common case and reads
			// as "everything is new", not as an error.
			row.Create = incoming
		case strategy == kc.StrategySkip:
			row.Create = maxInt(incoming-existing, 0)
			row.LeaveAlone = minInt(incoming, existing)
		default:
			row.Overwrite = minInt(incoming, existing)
			row.Create = maxInt(incoming-existing, 0)
		}
		d.Categories = append(d.Categories, row)
	}

	add("Users", m.Counts.Users, shape.Users, usersNote(m.Counts.Users, shape.Users, shape.Exists), "info")
	add("Clients", m.Counts.Clients, shape.Clients, clientsNote(m), noteLevel(clientsNote(m)))
	add("Key providers", m.Counts.KeyProviders, len(shape.KeyIDs), keysNote(m, shape, strategy), noteLevel(keysNote(m, shape, strategy)))
	add("Identity providers", m.Counts.IdentityProviders, shape.IdentityProviders, idpNote(m), noteLevel(idpNote(m)))
	add("User federation", m.Counts.Federations, shape.Federations, federationNote(m), "info")
	add("Roles, groups, scopes, flows",
		m.Counts.RealmRoles+m.Counts.ClientRoles+m.Counts.Groups+m.Counts.ClientScopes+m.Counts.AuthFlows,
		shape.RealmRoles+shape.Groups+shape.ClientScopes, "", "")

	created, overwritten := 0, 0
	for _, row := range d.Categories {
		created += row.Create
		overwritten += row.Overwrite
	}
	if shape.Exists {
		d.Summary = fmt.Sprintf("%d to create, %d to overwrite against the existing realm.", created, overwritten)
	} else {
		d.Summary = fmt.Sprintf("The realm %q does not exist on the destination, so all %d items are new.", s.Realm, created)
	}
	return d
}

func usersNote(incoming, existing int, exists bool) string {
	if !exists || existing == 0 {
		return ""
	}
	return fmt.Sprintf("%d accounts already exist here", minInt(incoming, existing))
}

func clientsNote(m manifest.Manifest) string {
	var missing []string
	for _, c := range m.Clients {
		if c.Confidential && !c.SecretPresent {
			missing = append(missing, c.ClientID)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	sort.Strings(missing)
	if len(missing) == 1 {
		return missing[0] + " arrives without its secret"
	}
	return fmt.Sprintf("%d clients arrive without their secrets", len(missing))
}

func keysNote(m manifest.Manifest, shape RealmShape, strategy kc.ImportStrategy) string {
	if !shape.Exists {
		return ""
	}
	if strategy == kc.StrategySkip {
		return "the destination keeps its own signing keys, so tokens signed by the source will not verify here"
	}
	if len(shape.KeyIDs) > 0 {
		return "tokens signed by the destination's current keys stop verifying"
	}
	return ""
}

func idpNote(m manifest.Manifest) string {
	for _, idp := range m.IdentityProviders {
		if !idp.SecretCarried {
			return idp.Alias + " arrives without its client secret"
		}
	}
	if len(m.IdentityProviders) > 0 {
		return "redirect URIs still point at the source — review after import"
	}
	return ""
}

func federationNote(m manifest.Manifest) string {
	for _, f := range m.Federations {
		if !f.BindCarried {
			return f.Name + " arrives without its bind credential"
		}
	}
	if len(m.Federations) > 0 {
		return "bind credential carried"
	}
	return ""
}

func noteLevel(note string) string {
	switch {
	case note == "":
		return ""
	case strings.Contains(note, "without its"), strings.Contains(note, "not verify"), strings.Contains(note, "stop verifying"):
		return "warning"
	default:
		return "caution"
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// RestoreRequest is what the wizard collected.
type RestoreRequest struct {
	Environment string
	Storage     string
	BundleKey   string
	SnapshotID  string
	Realm       string
	Strategy    kc.ImportStrategy
	Passphrase  string
	Identities  []string
	// Candidates are the named keys held in this machine's keychain, resolved
	// by the binding layer. The orchestrator never reaches into a keychain
	// itself; it is handed material like every other dependency.
	Candidates []inspect.KeyCandidate
	// ConfirmRealm must equal the realm name when overwriting a realm that
	// already exists, because that operation is destructive and irreversible.
	ConfirmRealm string
	// NoTransactionTimeout lets the import's transactions run without a time
	// limit, for a realm too large to write inside the destination's. It is the
	// same escape hatch the capture offers, and the same trade: the limit is
	// what stops an import that has stopped making progress from holding a
	// connection to the destination database open indefinitely.
	NoTransactionTimeout bool
}

// RestoreResult is what an operator sees at the end.
type RestoreResult struct {
	JobID string `json:"jobId"`
}

// Restore imports a snapshot into a target environment.
func (o *Orchestrator) Restore(ctx context.Context, req RestoreRequest) (RestoreResult, error) {
	cfg := o.opts.Config.Config()

	env, ok := cfg.Environment(req.Environment)
	if !ok {
		return RestoreResult{}, resil.Fatal("start the restore",
			fmt.Sprintf("There is no environment called %q.", req.Environment), config.ErrNotFound)
	}
	st, ok := cfg.StorageByName(req.Storage)
	if !ok {
		return RestoreResult{}, resil.Fatal("start the restore",
			fmt.Sprintf("There is no storage called %q.", req.Storage), config.ErrNotFound)
	}
	if req.Strategy == "" {
		return RestoreResult{}, resil.Fatal("start the restore", "No import strategy was chosen.", nil)
	}

	j := o.newJob(config.JobRestore, snapshot.NewID(o.opts.Now()))
	j.Realm = req.Realm
	j.Environment = env.Name
	j.Storage = st.Name
	j.SnapshotID = req.SnapshotID
	j.StorageKey = req.BundleKey
	j.Source = env.Target()
	j.Provenance.EnvironmentKind = string(env.Kind)
	j.Message = "Queued."
	o.saveJob(j)

	go o.runRestore(context.WithoutCancel(ctx), env, st, req, j)
	return RestoreResult{JobID: j.ID}, nil
}

func (o *Orchestrator) runRestore(ctx context.Context, env config.Environment, st config.Storage, req RestoreRequest, j *config.Job) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer o.track(j.ID, cancel)()

	rep := o.reporterFor(j)
	started := o.opts.Now()
	j.State = config.JobRunning
	j.StartedAt = &started
	o.saveJob(j)
	rep.JobState(string(config.JobRunning), "Restoring "+req.Realm+" into "+env.Name+".")

	blobs, err := o.opts.Registry.Store(st)
	if err != nil {
		_ = o.fail(j, rep, obs.PhaseDownload, err)
		return
	}
	defer blobs.Close() //nolint:errcheck

	// Verification and decryption gate the restore before the target is
	// contacted at all. A restore that half-writes a corrupted realm is worse
	// than one that never starts.
	session, err := inspect.Open(ctx, o.home(), blobs, inspect.OpenRequest{
		Storage: st.Name, BundleKey: req.BundleKey, SnapshotID: req.SnapshotID,
		Passphrase: req.Passphrase, Identities: req.Identities,
		Candidates: req.Candidates,
	}, rep)
	if err != nil {
		_ = o.fail(j, rep, obs.PhaseIntegrity, err)
		return
	}
	defer session.Close() //nolint:errcheck

	// Which snapshot this is, recorded now that the manifest can be read. The
	// Activity screen names it: a realm and a destination do not distinguish
	// two captures of the same realm a fortnight apart, and restoring the wrong
	// one is not a mistake that announces itself.
	j.Origin = &config.SnapshotOrigin{
		// The envelope's timestamp rather than the manifest's: the envelope is
		// what the integrity check covers, so it is the copy that has been
		// proven to belong to this bundle by the time this line runs.
		CapturedAt:  session.Envelope.CreatedAt.Format(time.RFC3339),
		Environment: session.Manifest.Source.EnvironmentName,
	}
	o.saveJob(j)

	// A key used without being asked for is still named. Silence would be the
	// one thing worse than the prompt it replaces.
	if session.UnlockedWith != "" {
		rep.Log(fmt.Sprintf("Opened with the stored key %q.", session.UnlockedWith))
	}

	if !session.Verify.OK {
		_ = o.fail(j, rep, obs.PhaseIntegrity, resil.Fatal("verify the snapshot",
			"This snapshot could not be proven intact, so PortCloak will not write it to a target. "+session.Verify.Message, nil).
			WithAdvice("Verify it against another copy, or capture again. Nothing was contacted on the destination."))
		return
	}
	j.CompletePhase(string(obs.PhaseIntegrity))

	// Overwriting a realm that already exists is destructive and irreversible,
	// so it takes a deliberate confirmation naming the realm.
	var dest Destination
	if o.opts.Registry.Destination != nil {
		if d, derr := o.opts.Registry.Destination(env); derr == nil {
			dest = d
		}
	}
	if req.Strategy == kc.StrategyOverwrite && dest != nil && dest.Reachable(ctx) {
		if shape, serr := dest.ReadRealm(ctx, session.Realm); serr == nil && shape.Exists {
			if req.ConfirmRealm != session.Realm {
				_ = o.fail(j, rep, obs.PhaseImport, resil.Fatal("apply the import",
					fmt.Sprintf("Overwriting %q replaces the realm that is already on %s. Confirm by typing its name.", session.Realm, env.Name), nil))
				return
			}
		}
	}

	rep.StartPhase(obs.PhasePreconditions)
	pre := Preconditions(session)
	rep.CompletePhase(obs.PhasePreconditions, pre.Summary)
	j.CompletePhase(string(obs.PhasePreconditions))

	rep.StartPhase(obs.PhaseDryRun)
	dry := ComputeDryRun(ctx, session, dest, req.Strategy)
	if dry.Available {
		rep.CompletePhase(obs.PhaseDryRun, dry.Summary)
	} else {
		rep.CompletePhase(obs.PhaseDryRun, dry.Unavailable)
	}
	j.CompletePhase(string(obs.PhaseDryRun))

	applied, err := o.applyImport(ctx, env, session, req, j, rep, dest)
	if err != nil {
		// A restore cannot always be unwound, and pretending otherwise would
		// leave an operator with a wrong picture of their own system.
		// A distinct item, so the honest account of what reached the
		// destination sits beside the failure rather than being collapsed into
		// it. What the destination now holds is the thing an operator most
		// needs from a failed restore.
		j.Append(config.LedgerEntry{
			Phase: string(obs.PhaseImport), Item: "destination state", Attempts: 1,
			LastError: err.Error(), Outcome: applied.describe(), At: o.opts.Now(),
		})
		_ = o.fail(j, rep, obs.PhaseImport, err)
		_ = o.opts.Audit.Record(obs.AuditEntry{
			Action: obs.ActionRestore, Outcome: "did not finish",
			Realm: session.Realm, SnapshotID: session.ID, Environment: env.Name,
			Detail: applied.describe(),
		})
		return
	}
	j.CompletePhase(string(obs.PhaseImport))

	rep.StartPhase(obs.PhaseValidate)
	validation := o.validate(ctx, session, dest)
	rep.CompletePhase(obs.PhaseValidate, validation.Summary)
	j.CompletePhase(string(obs.PhaseValidate))
	if !validation.Performed {
		// It ran and abstained. Ticked as done it would read as "the realm
		// checked out", which is the one thing it does not say.
		j.SkipPhase(string(obs.PhaseValidate))
	}

	o.complete(j, rep, fmt.Sprintf("Restored %s into %s. %s", session.Realm, env.Name, validation.Summary))
	_ = o.opts.Audit.Record(obs.AuditEntry{
		Action: obs.ActionRestore, Outcome: "restored",
		Realm: session.Realm, SnapshotID: session.ID, Environment: env.Name, Storage: st.Name,
		Detail: fmt.Sprintf("%s · %s · %s", req.Strategy, applied.describe(), validation.Summary),
	})
}

// appliedState is what was actually written, which a cancel or failure reports
// honestly rather than claiming a rollback it cannot perform.
type appliedState struct {
	Started     bool
	Completed   bool
	Added       int
	Overwritten int
	Skipped     int
	Note        string
}

func (a appliedState) describe() string {
	switch {
	case !a.Started:
		return "nothing was written to the destination"
	case a.Completed:
		return fmt.Sprintf("%d created, %d overwritten, %d left alone", a.Added, a.Overwritten, a.Skipped)
	default:
		if a.Note != "" {
			return a.Note
		}
		return "the import had started, so some changes may already have been applied — Keycloak's import is not transactional"
	}
}

func (o *Orchestrator) applyImport(ctx context.Context, env config.Environment, s *inspect.Session, req RestoreRequest, j *config.Job, rep *obs.Reporter, dest Destination) (appliedState, error) {
	rep.StartPhase(obs.PhaseImport)
	var applied appliedState

	if req.Strategy == kc.StrategyMerge {
		// Merge has no offline equivalent: it is partialImport against a
		// running server. Saying which path will be used, and what it will do,
		// beats silently downgrading to a different operation.
		if dest == nil {
			return applied, resil.Fatal("apply the import",
				"Merge is applied through the destination's Admin API, and this environment has no Admin API configured.", nil).
				WithAdvice("Configure the Admin API on this environment, or choose overwrite or skip, which use kc.sh import.")
		}
		realmFile, err := s.RealmFileBytes()
		if err != nil {
			return applied, err
		}
		applied.Started = true
		added, overwritten, skipped, err := dest.PartialImport(ctx, s.Realm, realmFile, "OVERWRITE")
		if err != nil {
			return applied, err
		}
		applied.Completed = true
		applied.Added, applied.Overwritten, applied.Skipped = added, overwritten, skipped
		rep.CompletePhase(obs.PhaseImport, applied.describe())
		return applied, nil
	}

	// Offline import runs through the same execution machinery as a capture,
	// including an ephemeral clone on Docker and Kubernetes — so a restore does
	// not disturb the serving instance either.
	exec, err := o.opts.Registry.Executor(env)
	if err != nil {
		return applied, err
	}
	defer exec.Close() //nolint:errcheck

	facts, err := exec.Probe(ctx)
	if err != nil {
		return applied, err
	}
	if !facts.OK() {
		blocker, _ := facts.FirstBlocker()
		return applied, resil.Fatal("check the destination",
			fmt.Sprintf("%s: %s", blocker.Name, blocker.Value), nil).WithAdvice(blocker.Advice)
	}

	execCtx, err := exec.Prepare(ctx, target.PrepareOptions{
		JobID: j.ID, Realms: []string{s.Realm}, Purpose: "restore",
	})

	// The defer is registered before the error is checked, for the reason
	// capture.go gives at the same point: Prepare can fail *after* the clone
	// was created — waiting for it to come up, or setting up its work directory
	// — and a clone left running carries the same database credentials as the
	// serving instance. The executor tears down whatever it recorded rather
	// than whatever Prepare managed to return.
	defer o.teardown(ctx, exec, execCtx, rep, []*config.Job{j})

	if err != nil {
		return applied, err
	}
	if execCtx.CloneRef != "" {
		rep.CloneCreated(execCtx.CloneRef)
		j.Provenance.CloneRef = execCtx.CloneRef
		j.Provenance.ExecutionMode = string(execCtx.Mode)
		o.saveJob(j)
	}

	// The realm's artifacts are pushed into the execution context so kc.sh
	// import can read them where it runs.
	importDir := path.Join(execCtx.WorkDir, "import")
	pushed := 0
	for _, name := range s.RealmFiles() {
		f, err := os.Open(s.PathOf(name))
		if err != nil {
			return applied, err
		}
		info, _ := f.Stat()
		var size int64
		if info != nil {
			size = info.Size()
		}
		err = exec.PushFile(ctx, path.Join(importDir, path.Base(name)), size, f)
		_ = f.Close()
		if err != nil {
			return applied, err
		}
		pushed++
		rep.Progress(int64(pushed), int64(len(s.RealmFiles())), "files", path.Base(name))
	}

	cmd, err := kc.BuildImport(kc.ImportRequest{
		KcPath:               facts.KcPath,
		Dir:                  importDir,
		Strategy:             req.Strategy,
		Ports:                kc.Ports{HTTP: execCtx.Ports.HTTP, HTTPS: execCtx.Ports.HTTPS, Management: execCtx.Ports.Management},
		Supported:            o.discoverOptions(ctx, exec, facts.KcPath, "import", env.Sudo, rep),
		NoTransactionTimeout: req.NoTransactionTimeout,
	})
	if err != nil {
		return applied, resil.Fatal("build the import command", err.Error(), err)
	}
	rep.Log(cmd.String())

	applied.Started = true
	result, err := exec.Run(ctx, target.Command{
		Path: cmd.Path, Args: cmd.Args, Env: cmd.Env, Sudo: env.Sudo,
		OnStdout: rep.Log, OnStderr: rep.Log,
	})
	if err != nil {
		return applied, err
	}
	outcome := kc.ParseOutput(result.Stdout, result.Stderr)
	outcome.ExitCode = result.ExitCode
	if result.ExitCode != 0 {
		message, advice, retryable := kc.ClassifyFailure(s.Realm, outcome, result.Stderr)
		applied.Note = "the import ran and did not finish; Keycloak's import is not transactional, so some resources may already exist on the destination"
		err := resil.Fatal("apply the import", message, nil).WithAdvice(advice)
		if retryable {
			err.Class = resil.Retryable
		}
		return applied, err
	}

	applied.Completed = true
	applied.Added = s.Manifest.Counts.Users + s.Manifest.Counts.Clients
	rep.CompletePhase(obs.PhaseImport, fmt.Sprintf("The realm was imported using the %s strategy.", req.Strategy))
	return applied, nil
}

// Validation is the post-restore reconciliation.
type Validation struct {
	Summary        string          `json:"summary"`
	ContinuityNote string          `json:"continuityNote"`
	Rows           []ValidationRow `json:"rows"`
	OutOfScope     []string        `json:"outOfScope"`
	Performed      bool            `json:"performed"`
	// TokenContinuity is the assertion the whole tool has been building
	// toward: a token signed before the move still verifies after it.
	TokenContinuity bool `json:"tokenContinuity"`
}

// ValidationRow is one category's expected-versus-actual.
type ValidationRow struct {
	Category string `json:"category"`
	Expected int    `json:"expected"`
	Actual   int    `json:"actual"`
	OK       bool   `json:"ok"`
	Note     string `json:"note,omitempty"`
}

func (o *Orchestrator) validate(ctx context.Context, s *inspect.Session, dest Destination) Validation {
	v := Validation{
		OutOfScope: []string{
			"Sessions were not carried, so users will re-authenticate.",
			"Themes and provider JARs were the operator's to deploy; PortCloak reported them but never migrates them.",
		},
	}
	if dest == nil || !dest.Reachable(ctx) {
		// Reported as not performed rather than passed. They are different
		// answers and only one of them is safe to act on.
		v.Summary = "Validation was not performed, because the destination's Admin API could not be read."
		return v
	}
	shape, err := dest.ReadRealm(ctx, s.Realm)
	if err != nil || !shape.Exists {
		v.Summary = "Validation was not performed, because the destination realm could not be read back."
		return v
	}
	v.Performed = true

	add := func(category string, expected, actual int) {
		v.Rows = append(v.Rows, ValidationRow{
			Category: category, Expected: expected, Actual: actual, OK: actual >= expected,
		})
	}
	m := s.Manifest
	add("Users", m.Counts.Users, shape.Users)
	add("Clients", m.Counts.Clients, shape.Clients)
	add("Realm roles", m.Counts.RealmRoles, shape.RealmRoles)
	add("Groups", m.Counts.Groups, shape.Groups)
	add("Identity providers", m.Counts.IdentityProviders, shape.IdentityProviders)
	add("User federation", m.Counts.Federations, shape.Federations)

	// The key check is the token-continuity one, and it is done by KID rather
	// than by count: the right number of keys with the wrong ids would still
	// mean every existing token stops verifying.
	expectedKID := ""
	if k, ok := m.ActiveSigningKey(); ok {
		expectedKID = k.KID
	}
	if expectedKID != "" {
		present := false
		for _, kid := range shape.KeyIDs {
			if kid == expectedKID {
				present = true
				break
			}
		}
		v.TokenContinuity = present
		v.Rows = append(v.Rows, ValidationRow{
			Category: "Active signing key", Expected: 1, Actual: boolToInt(present), OK: present,
			Note: "kid " + expectedKID,
		})
		if present {
			v.ContinuityNote = fmt.Sprintf("The active signing key (kid %s) is present on the destination, so tokens issued before the move still verify.", expectedKID)
		} else {
			v.ContinuityNote = fmt.Sprintf("The active signing key (kid %s) is not on the destination, so tokens issued before the move will not verify here.", expectedKID)
		}
	} else {
		v.ContinuityNote = "This snapshot carried no active signing key, so token continuity could not be established."
	}

	drift := 0
	for _, r := range v.Rows {
		if !r.OK {
			drift++
		}
	}
	if drift == 0 {
		v.Summary = "Every category reconciles with the manifest."
	} else {
		v.Summary = fmt.Sprintf("%d categor%s did not reconcile with the manifest.", drift, plural2(drift))
	}
	return v
}

func plural2(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
