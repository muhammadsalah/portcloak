// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/kc"
	"portcloak/internal/engine/orchestrator"
)

// RestoreController is the restore wizard.
type RestoreController struct{ eng *Engine }

// NewRestoreController binds the restore wizard.
func NewRestoreController(eng *Engine) *RestoreController { return &RestoreController{eng: eng} }

// ServiceName is what the Wails binding layer calls this.
func (r *RestoreController) ServiceName() string { return "RestoreController" }

// Strategy describes one import strategy in terms of what happens to an
// existing resource, not in terms of a Keycloak flag name.
type Strategy struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// NeedsAdminAPI marks merge, which is partialImport against a running
	// server and has no offline equivalent.
	NeedsAdminAPI bool `json:"needsAdminApi"`
	// Destructive marks overwrite, which requires typing the realm name when
	// the destination realm already exists.
	Destructive bool `json:"destructive"`
}

// Strategies is what the strategy step offers.
func (r *RestoreController) Strategies() []Strategy {
	return []Strategy{
		{
			Value: string(kc.StrategyOverwrite), Label: "Overwrite",
			Description: kc.StrategyExplanation(kc.StrategyOverwrite),
			Destructive: true,
		},
		{
			Value: string(kc.StrategySkip), Label: "Skip",
			Description: kc.StrategyExplanation(kc.StrategySkip),
		},
		{
			Value: string(kc.StrategyMerge), Label: "Merge",
			Description:   kc.StrategyExplanation(kc.StrategyMerge),
			NeedsAdminAPI: true,
		},
	}
}

// Destinations lists the environments a snapshot could be restored into.
func (r *RestoreController) Destinations() []EnvironmentView {
	return (&ConfigController{eng: r.eng}).Load().Environments
}

// PlanRequest is what the wizard has collected so far.
type PlanRequest struct {
	SnapshotID  string `json:"snapshotId"`
	Environment string `json:"environment"`
	Strategy    string `json:"strategy"`
}

// Plan is the preconditions and dry-run steps together, computed for the
// strategy actually selected.
type Plan struct {
	Preconditions orchestrator.PreconditionReport `json:"preconditions"`
	DryRun        orchestrator.DryRun             `json:"dryRun"`
	// Blocked is set only when the snapshot itself could not be proven intact.
	// A snapshot that cannot be proven intact is never written to a target.
	Blocked     bool   `json:"blocked"`
	BlockedNote string `json:"blockedNote,omitempty"`
	// ConfirmationRequired is true when overwriting a realm that already
	// exists, which is destructive and irreversible.
	ConfirmationRequired bool     `json:"confirmationRequired"`
	Failure              *Failure `json:"failure,omitempty"`
}

// Plan computes the preview.
func (r *RestoreController) Plan(req PlanRequest) (res Plan) {
	defer func() { res = lists(res) }()
	s, err := r.eng.Session(req.SnapshotID)
	if err != nil {
		return Plan{Failure: Fail(err)}
	}

	out := Plan{Preconditions: orchestrator.Preconditions(s)}
	if s.Degraded() {
		out.Blocked = true
		out.BlockedNote = "This snapshot could not be proven intact, so PortCloak will not write it to a target. " + s.Verify.Message
		return out
	}

	cfg := r.eng.Config.Config()
	env, ok := cfg.Environment(req.Environment)
	if !ok {
		return Plan{Preconditions: out.Preconditions, Failure: Fail(config.ErrNotFound)}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var dest orchestrator.Destination
	if d, derr := r.eng.destinationFor(env); derr == nil {
		dest = d
	}

	strategy := kc.ImportStrategy(req.Strategy)
	if strategy == "" {
		strategy = kc.StrategyOverwrite
	}
	out.DryRun = orchestrator.ComputeDryRun(ctx, s, dest, strategy)

	if strategy == kc.StrategyOverwrite && out.DryRun.Available && out.DryRun.TargetExists {
		out.ConfirmationRequired = true
	}
	return out
}

// ApplyRequest is the confirmed restore.
type ApplyRequest struct {
	SnapshotID   string   `json:"snapshotId"`
	Storage      string   `json:"storage"`
	BundleKey    string   `json:"bundleKey"`
	Realm        string   `json:"realm"`
	Environment  string   `json:"environment"`
	Strategy     string   `json:"strategy"`
	Passphrase   string   `json:"passphrase"`
	Identities   []string `json:"identities"`
	ConfirmRealm string   `json:"confirmRealm"`
}

// ApplyResult is the handle a started restore hands back.
type ApplyResult struct {
	JobID   string   `json:"jobId"`
	Failure *Failure `json:"failure,omitempty"`
}

// Apply runs the restore.
func (r *RestoreController) Apply(req ApplyRequest) ApplyResult {
	// The wizard proved this key against the bundle on the way into
	// preconditions. Keeping it means the next restore in this session does not
	// ask for it again.
	r.eng.rememberKey(req.Passphrase, req.Identities)

	res, err := r.eng.Orch.Restore(context.Background(), orchestrator.RestoreRequest{
		Environment:  req.Environment,
		Storage:      req.Storage,
		BundleKey:    req.BundleKey,
		SnapshotID:   req.SnapshotID,
		Realm:        req.Realm,
		Strategy:     kc.ImportStrategy(req.Strategy),
		Passphrase:   req.Passphrase,
		Identities:   req.Identities,
		Candidates:   r.eng.keyCandidates(),
		ConfirmRealm: req.ConfirmRealm,
	})
	if err != nil {
		return ApplyResult{Failure: Fail(err)}
	}
	return ApplyResult{JobID: res.JobID}
}

// OutOfScopeNote is what the result screen restates after a restore.
func (r *RestoreController) OutOfScopeNote() []string {
	return []string{
		"Sessions were not carried, by design. Users will re-authenticate — token continuity comes from the signing keys, not from replaying session objects.",
		"Themes and provider JARs were reported, never migrated. Deploying them on the destination was yours to do.",
	}
}
