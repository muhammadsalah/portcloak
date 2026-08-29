// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/crypto"
	"portcloak/internal/engine/orchestrator"
	"portcloak/internal/engine/snapshot"
)

// CaptureController is the capture wizard.
type CaptureController struct{ eng *Engine }

// NewCaptureController binds the capture wizard.
func NewCaptureController(eng *Engine) *CaptureController { return &CaptureController{eng: eng} }

// WizardDefaults is what the wizard opens with.
type WizardDefaults struct {
	Environments   []EnvironmentView  `json:"environments"`
	Storages       []StorageView      `json:"storages"`
	DefaultStorage string             `json:"defaultStorage"`
	Preferences    config.Preferences `json:"preferences"`
	// EncryptionNotice is the sentence beside the toggle. Encryption is opt-in
	// and declining it is a respected choice, so the notice states the
	// consequence rather than scolding.
	EncryptionNotice string `json:"encryptionNotice"`
	// DeclineNotice is what an operator confirms when turning encryption off.
	DeclineNotice string `json:"declineNotice"`
}

// Defaults returns what the wizard needs to open.
func (c *CaptureController) Defaults() (res WizardDefaults) {
	defer func() { res = lists(res) }()
	cfg := c.eng.Config.Config()
	snapshot := (&ConfigController{eng: c.eng}).Load()

	out := WizardDefaults{
		Environments:     snapshot.Environments,
		Storages:         snapshot.Storage,
		Preferences:      c.eng.Config.Preferences(),
		EncryptionNotice: "Recommended. An unencrypted snapshot holds unmasked client secrets, LDAP bind credentials and RSA private signing keys in the clear. Holding the file is equivalent to holding the realm.",
		DeclineNotice:    declineNotice,
	}
	if d, ok := cfg.DefaultStorage(); ok {
		out.DefaultStorage = d.Name
	}
	return out
}

// declineNotice is what turning encryption off actually means. It is stated in
// full, once, at the moment of the decision.
const declineNotice = "This snapshot will be written unencrypted. The file will contain unmasked client secrets, LDAP bind credentials, IdP secrets, SMTP passwords and RSA private signing keys, in the clear. Anyone who obtains it effectively holds the realm. PortCloak will label the snapshot and record this choice, and will not expire the file. Where it ends up afterwards is yours to decide."

// Realms lists what can be captured from an environment.
//
// Where the Admin API is reachable this is the realms it reports. Where it is
// not, the operator types a realm name — which is a normal path, because an
// offline capture from a stopped Keycloak has no Admin API at all.
type RealmsResult struct {
	Realms []string `json:"realms"`
	// Discovered is false when PortCloak could not enumerate them, so the UI
	// asks for a name rather than showing an empty list as if it were an answer.
	Discovered bool     `json:"discovered"`
	Note       string   `json:"note"`
	Failure    *Failure `json:"failure,omitempty"`
}

// Realms enumerates the realms on an environment.
func (c *CaptureController) Realms(environment string) (res RealmsResult) {
	defer func() { res = lists(res) }()
	cfg := c.eng.Config.Config()
	env, ok := cfg.Environment(environment)
	if !ok {
		return RealmsResult{Failure: Fail(config.ErrNotFound)}
	}

	v, err := c.eng.verifierFor(env)
	if err != nil || v == nil {
		return RealmsResult{
			Note: "This environment has no Admin API configured, so PortCloak cannot list its realms. Type the realm name. The export reads it straight from the database.",
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if !v.Reachable(ctx) {
		return RealmsResult{
			Note: "The Admin API was not reachable, so PortCloak cannot list the realms. Type the realm name. The export does not need the server running.",
		}
	}
	realms, err := v.Realms(ctx)
	if err != nil {
		return RealmsResult{Note: "The realms could not be listed. Type the realm name instead.", Failure: Fail(err)}
	}
	return RealmsResult{Realms: realms, Discovered: true, Note: "Each realm selected becomes its own snapshot."}
}

// CaptureOptions is what the wizard collected.
type CaptureOptions struct {
	Environment        string   `json:"environment"`
	Storage            string   `json:"storage"`
	UsersMode          string   `json:"usersMode"`
	EncryptionMode     string   `json:"encryptionMode"`
	Passphrase         string   `json:"passphrase"`
	Realms             []string `json:"realms"`
	Recipients         []string `json:"recipients"`
	UsersPerFile       int      `json:"usersPerFile"`
	Verify             bool     `json:"verify"`
	DetectDependencies bool     `json:"detectDependencies"`
	// NoTransactionTimeout lets the export's transactions run without a time
	// limit, for a realm too large or too slow to read inside the default one.
	NoTransactionTimeout bool `json:"noTransactionTimeout"`
	Encrypt              bool `json:"encrypt"`
	// AcknowledgedUnencrypted records that the operator saw and confirmed the
	// decline notice. It is required rather than assumed, so declining is one
	// deliberate action.
	AcknowledgedUnencrypted bool `json:"acknowledgedUnencrypted"`
}

// StartResult is the handle a started run hands back.
type StartResult struct {
	JobIDs  []string `json:"jobIds"`
	Realms  []string `json:"realms"`
	Failure *Failure `json:"failure,omitempty"`
}

// Start runs the capture.
func (c *CaptureController) Start(opts CaptureOptions) (res StartResult) {
	defer func() { res = lists(res) }()
	if !opts.Encrypt && !opts.AcknowledgedUnencrypted {
		return StartResult{Failure: &Failure{
			Message: "Writing this snapshot unencrypted has not been confirmed.",
			Hint:    declineNotice,
		}}
	}

	enc := crypto.Config{Enabled: opts.Encrypt}
	if opts.Encrypt {
		enc.Mode = snapshot.EncryptionMode(opts.EncryptionMode)
		enc.Passphrase = opts.Passphrase
		enc.Recipients = opts.Recipients
	}

	handle, err := c.eng.Orch.Capture(context.Background(), orchestrator.CaptureRequest{
		Environment:          opts.Environment,
		Realms:               opts.Realms,
		Storage:              opts.Storage,
		UsersMode:            opts.UsersMode,
		UsersPerFile:         opts.UsersPerFile,
		Verify:               opts.Verify,
		DetectDependencies:   opts.DetectDependencies,
		NoTransactionTimeout: opts.NoTransactionTimeout,
		Encryption:           enc,
	})
	if err != nil {
		return StartResult{Failure: Fail(err)}
	}
	return StartResult{JobIDs: handle.JobIDs, Realms: handle.Realms}
}
