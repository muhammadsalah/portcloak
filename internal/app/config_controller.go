// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/store"
	"portcloak/internal/engine/store/disk"
	"portcloak/internal/engine/target"
)

// ConfigController is the environments and storage screens.
type ConfigController struct{ eng *Engine }

// NewConfigController binds the configuration screens.
func NewConfigController(eng *Engine) *ConfigController { return &ConfigController{eng: eng} }

// EnvironmentView is one row of the environments list, plus everything its
// detail pane needs.
type EnvironmentView struct {
	config.Environment
	// Target is the one-line summary — the folder, container, or namespace and
	// workload.
	Target string `json:"target"`
	// Readiness says whether this entry can be used yet, which is a softer
	// question than whether the file is valid.
	Readiness config.Readiness `json:"readiness"`
	// Stale marks a probe result old enough that believing it would be worse
	// than having no information.
	Stale bool `json:"stale"`
	// ProbeAge is rendered rather than raw, because "3 weeks ago" is the fact
	// an operator reads.
	ProbeAge string `json:"probeAge,omitempty"`
	// CredentialPresent is false after a config was copied between machines,
	// which is the correct behaviour and worth saying rather than failing
	// obscurely later.
	CredentialPresent bool `json:"credentialPresent"`
}

// StorageView is one row of the storage list.
type StorageView struct {
	config.Storage
	Root              string           `json:"root"`
	Readiness         config.Readiness `json:"readiness"`
	Stale             bool             `json:"stale"`
	ProbeAge          string           `json:"probeAge,omitempty"`
	CredentialPresent bool             `json:"credentialPresent"`
}

// Snapshot of the whole configuration, for the screens that show both lists.
type ConfigSnapshot struct {
	Environments []EnvironmentView  `json:"environments"`
	Storage      []StorageView      `json:"storage"`
	Preferences  config.Preferences `json:"preferences"`
	// ConfigFile is shown on the Settings screen so an operator can open it.
	ConfigFile string `json:"configFile"`
	FirstRun   bool   `json:"firstRun"`
	// LoadProblems are the validation failures that stopped configuration from
	// loading, each naming a line. The app still opens: an operator cannot read
	// which line to fix from a window that refused to appear.
	LoadProblems []config.Problem `json:"loadProblems,omitempty"`
	// NoSignIn states the thing a first-time operator most wants to know.
	NoSignIn string `json:"noSignIn"`
}

// Load returns everything the configuration screens render.
func (c *ConfigController) Load() (res ConfigSnapshot) {
	defer func() { res = lists(res) }()
	cfg := c.eng.Config.Config()
	now := time.Now()
	stale := c.eng.staleAfter()

	present := map[string]bool{}
	for _, s := range c.eng.Config.CheckCredentials(c.eng.Creds) {
		present[s.Kind+"\x00"+s.Name] = s.Present
	}

	out := ConfigSnapshot{
		Preferences: c.eng.Config.Preferences(),
		ConfigFile:  c.eng.Home().ConfigFile(),
		FirstRun:    c.eng.FirstRun,
		NoSignIn:    "There is no account and no sign-in. PortCloak is a local tool; the only credentials involved are the ones each environment and storage carries.",
	}

	var ve *config.ValidationError
	if errors.As(c.eng.LoadError, &ve) {
		out.LoadProblems = ve.Problems
	}

	for _, e := range cfg.Environments {
		v := EnvironmentView{
			Environment:       e,
			Target:            e.Target(),
			Readiness:         config.EnvironmentReadiness(e),
			CredentialPresent: e.CredentialRef == "" || present["environment\x00"+e.Name],
		}
		if e.LastProbe != nil {
			v.Stale = e.LastProbe.Stale(stale, now)
			v.ProbeAge = renderAge(now.Sub(e.LastProbe.At))
		}
		out.Environments = append(out.Environments, v)
	}

	for _, s := range cfg.Storage {
		v := StorageView{
			Storage:           s,
			Root:              s.Root(),
			Readiness:         config.StorageReadiness(s),
			CredentialPresent: s.CredentialRef == "" || present["storage\x00"+s.Name],
		}
		if s.LastProbe != nil {
			v.Stale = s.LastProbe.Stale(stale, now)
			v.ProbeAge = renderAge(now.Sub(s.LastProbe.At))
		}
		out.Storage = append(out.Storage, v)
	}
	return out
}

func renderAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return pluralise(int(d.Minutes()), "minute") + " ago"
	case d < 48*time.Hour:
		return pluralise(int(d.Hours()), "hour") + " ago"
	case d < 14*24*time.Hour:
		return pluralise(int(d.Hours()/24), "day") + " ago"
	default:
		return pluralise(int(d.Hours()/24/7), "week") + " ago"
	}
}

func pluralise(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return itoa(n) + " " + unit + "s"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// EnvironmentKinds and StorageKinds are what the "Add" menus offer.
func (c *ConfigController) EnvironmentKinds() []string {
	out := make([]string, 0, len(config.EnvironmentKinds))
	for _, k := range config.EnvironmentKinds {
		out = append(out, string(k))
	}
	return out
}

func (c *ConfigController) StorageKinds() []string {
	out := make([]string, 0, len(config.StorageKinds))
	for _, k := range config.StorageKinds {
		out = append(out, string(k))
	}
	return out
}

// SaveEnvironment creates or updates an environment. An empty originalName
// creates.
//
// Secret carries the value the operator typed; it goes to the OS keychain and
// only a handle is written to config.yaml.
//
// A validation failure is shaped for the form rather than for the file. Fail
// renders a *config.ValidationError as "<path> has 2 problems:" followed by
// indented lines, which is right for the banner about a hand-edited config and
// wrong over an editor: the operator is looking at the field, not the file, and
// each problem already quotes the value it rejected and states the fix.
func (c *ConfigController) SaveEnvironment(originalName string, env config.Environment, secret string) *Failure {
	if secret != "" {
		handle := env.CredentialRef
		if handle == "" {
			handle = config.SuggestHandle(env.Kind, env.Name)
			env.CredentialRef = handle
		}
		if err := c.eng.Creds.Set(handle, secret); err != nil {
			return Fail(err)
		}
	}
	if originalName == "" {
		return failSave(c.eng.Config.AddEnvironment(env))
	}
	return failSave(c.eng.Config.SaveEnvironment(originalName, env))
}

// failSave reports a rejected edit as the problems themselves, one per line.
func failSave(err error) *Failure {
	var ve *config.ValidationError
	if errors.As(err, &ve) && len(ve.Problems) > 0 {
		lines := make([]string, 0, len(ve.Problems))
		for _, p := range ve.Problems {
			lines = append(lines, p.Message)
		}
		return &Failure{Message: strings.Join(lines, "\n")}
	}
	return Fail(err)
}

// SaveAdminCredential stores the Admin API credential separately, because it is
// a different secret with a different blast radius from the connection one.
func (c *ConfigController) SaveAdminCredential(name, secret string) *Failure {
	cfg := c.eng.Config.Config()
	env, ok := cfg.Environment(name)
	if !ok {
		return Fail(config.ErrNotFound)
	}
	handle := env.AdminCredentialRef
	if handle == "" {
		handle = config.Handle(string(env.Kind)+"-admin", name)
	}
	if err := c.eng.Creds.Set(handle, secret); err != nil {
		return Fail(err)
	}
	env.AdminCredentialRef = handle
	return Fail(c.eng.Config.SaveEnvironment(name, env))
}

// DuplicateEnvironment copies an environment without its credentials.
func (c *ConfigController) DuplicateEnvironment(name string) (EnvironmentView, *Failure) {
	dup, err := c.eng.Config.DuplicateEnvironment(name, "")
	if err != nil {
		return EnvironmentView{}, Fail(err)
	}
	return EnvironmentView{
		Environment: dup, Target: dup.Target(),
		Readiness: config.EnvironmentReadiness(dup),
	}, nil
}

// DeleteEnvironment removes an environment and its keychain secrets.
func (c *ConfigController) DeleteEnvironment(name string) *Failure {
	return Fail(c.eng.Config.DeleteEnvironment(name, c.eng.Creds, c.eng.Jobs.Running()))
}

// SaveStorage creates or updates a storage definition.
func (c *ConfigController) SaveStorage(originalName string, st config.Storage, secret string) *Failure {
	if secret != "" {
		handle := st.CredentialRef
		if handle == "" {
			handle = config.SuggestStorageHandle(st.Kind, st.Name)
			st.CredentialRef = handle
		}
		if err := c.eng.Creds.Set(handle, secret); err != nil {
			return Fail(err)
		}
	}
	if originalName == "" {
		return failSave(c.eng.Config.AddStorage(st))
	}
	return failSave(c.eng.Config.SaveStorage(originalName, st))
}

// DuplicateStorage copies a storage definition without its credential.
func (c *ConfigController) DuplicateStorage(name string) (StorageView, *Failure) {
	dup, err := c.eng.Config.DuplicateStorage(name, "")
	if err != nil {
		return StorageView{}, Fail(err)
	}
	return StorageView{Storage: dup, Root: dup.Root(), Readiness: config.StorageReadiness(dup)}, nil
}

// DeleteStorage removes a storage definition. Stored snapshots are never
// touched: removing a definition forgets how to reach the data, it does not
// destroy it.
func (c *ConfigController) DeleteStorage(name string) *Failure {
	return Fail(c.eng.Config.DeleteStorage(name, c.eng.Creds, c.eng.Jobs.Running()))
}

// SetDefaultStorage marks one storage as the default for new captures.
func (c *ConfigController) SetDefaultStorage(name string) *Failure {
	return Fail(c.eng.Config.SetDefaultStorage(name))
}

// SavePreferences replaces the preferences block.
func (c *ConfigController) SavePreferences(p config.Preferences) *Failure {
	return Fail(c.eng.Config.SetPreferences(p))
}

// Reload re-reads config.yaml, for an operator who edited it in a text editor.
func (c *ConfigController) Reload() ConfigSnapshot {
	c.eng.LoadError = c.eng.Config.Load()
	return c.Load()
}

// ProbeResult is what Test reports: the concrete facts, not a tick.
type ProbeResult struct {
	OK      bool               `json:"ok"`
	Facts   target.TargetFacts `json:"facts"`
	Failure *Failure           `json:"failure,omitempty"`
}

// TestEnvironment runs the same Probe the capture wizard uses.
//
// Sharing the code is a promise: what an operator sees here is exactly what a
// capture would find. A Test that ran something different is how the two drift
// into reporting different things.
func (c *ConfigController) TestEnvironment(name string) (res ProbeResult) {
	defer func() { res = lists(res) }()
	cfg := c.eng.Config.Config()
	env, ok := cfg.Environment(name)
	if !ok {
		return ProbeResult{Failure: Fail(config.ErrNotFound)}
	}

	exec, err := c.eng.executorFor(env)
	if err != nil {
		return ProbeResult{Failure: Fail(err)}
	}
	defer exec.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	facts, err := exec.Probe(ctx)
	if err != nil {
		return ProbeResult{Failure: Fail(err)}
	}

	// The Admin API is optional. Its absence is a note, never a failure — but
	// the reason for the absence is carried through, because "not reachable"
	// over a URL the operator can open in a browser diagnoses nothing.
	if v, verr := c.eng.adminFor(env); verr == nil && v != nil {
		reason := v.Check(ctx)
		facts.AdminReachable = reason == nil
		switch {
		case facts.AdminReachable:
			facts.AdminDetail = "reachable"
			if realms, rerr := v.Realms(ctx); rerr == nil {
				facts.Realms = realms
			}
			facts.Pass("Admin API", "reachable")
		default:
			f := Fail(reason)
			facts.AdminDetail = f.Message
			advice := f.Hint
			if advice == "" {
				advice = "This does not stop a capture. The export reads the realm from the database."
			}
			facts.Warn("Admin API", f.Message, advice)
		}
	} else {
		facts.Skipped("Admin API", "not configured on this environment")
	}

	stamp := config.ProbeStamp{
		At: time.Now(), OK: facts.OK(), Summary: facts.Summary(),
		KeycloakVersion: facts.KeycloakVersion, CloneCapable: facts.CloneCapable,
	}
	if err := c.eng.Config.RecordEnvironmentProbe(name, stamp); err != nil {
		c.eng.Log.Error("the probe result could not be recorded", "environment", name, "err", err)
	}
	return ProbeResult{OK: facts.OK(), Facts: facts}
}

// StorageProbeResult is what Test reports for a storage definition.
type StorageProbeResult struct {
	OK    bool        `json:"ok"`
	Reach store.Reach `json:"reach"`
	// Note explains the three-way result in a sentence.
	Note    string   `json:"note"`
	Failure *Failure `json:"failure,omitempty"`
}

// TestStorage performs the round trip: list, write a probe, verify it, remove
// it — and cleans up even when a step fails.
func (c *ConfigController) TestStorage(name string) (res StorageProbeResult) {
	defer func() { res = lists(res) }()
	cfg := c.eng.Config.Config()
	st, ok := cfg.StorageByName(name)
	if !ok {
		return StorageProbeResult{Failure: Fail(config.ErrNotFound)}
	}

	blobs, err := c.eng.storeFor(st)
	if err != nil {
		return StorageProbeResult{Failure: Fail(err)}
	}
	defer blobs.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	reach, err := blobs.Probe(ctx)
	if err != nil {
		return StorageProbeResult{Failure: Fail(err)}
	}

	writable := reach.Access == store.AccessWritable
	stamp := config.ProbeStamp{
		At: time.Now(), OK: reach.OK(), Summary: string(reach.Access), Writable: &writable,
	}
	if err := c.eng.Config.RecordStorageProbe(name, stamp); err != nil {
		c.eng.Log.Error("the probe result could not be recorded", "storage", name, "err", err)
	}

	return StorageProbeResult{OK: reach.OK(), Reach: reach, Note: storageNote(reach)}
}

// storageNote renders the three-way result. Read-only is a legitimate
// configuration for browsing, so it is described rather than collapsed into a
// failure.
func storageNote(r store.Reach) string {
	switch r.Access {
	case store.AccessWritable:
		note := "Reachable and writable. Integrity is checked by " + string(r.Integrity) + "."
		if r.Resumable {
			note += " An interrupted upload can resume."
		}
		return note
	case store.AccessReadOnly:
		return "Reachable, but PortCloak could not write here. Snapshots can be browsed and restored from this storage; new captures cannot be written to it."
	default:
		if r.FailedStep != "" {
			return "Not reachable — " + r.FailedStep + " failed. " + r.Detail
		}
		return "Not reachable. " + r.Detail
	}
}

// CreateStorageFolder creates a disk folder that does not exist yet, for the
// "shall I create it?" path.
func (c *ConfigController) CreateStorageFolder(name string) *Failure {
	cfg := c.eng.Config.Config()
	st, ok := cfg.StorageByName(name)
	if !ok {
		return Fail(config.ErrNotFound)
	}
	if st.Kind != config.StoreDisk {
		return Fail(config.ErrNotFound)
	}
	d, err := disk.New(st.Folder)
	if err != nil {
		return Fail(err)
	}
	return Fail(d.EnsureRoot())
}
