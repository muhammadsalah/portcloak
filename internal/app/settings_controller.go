// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/inspect"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/target"
	"portcloak/internal/engine/target/clone"
)

// SettingsController is the configuration screen: where PortCloak keeps its
// files, what a previous session left running in someone else's cluster, and
// what is sitting on this disk.
//
// Everything here is an action on PortCloak itself rather than on a realm,
// which is why it is not on the audit screen: that one is a record, and a
// record you can press buttons in is neither.
type SettingsController struct{ eng *Engine }

// NewSettingsController binds the settings screen.
func NewSettingsController(eng *Engine) *SettingsController {
	return &SettingsController{eng: eng}
}

// ServiceName is the name internal/desktop logs this service under. It is
// not the address a bound method is called by — see the comment on
// controllers there, which is where reading it as one caused real damage.
func (s *SettingsController) ServiceName() string { return "SettingsController" }

// LocationView is the "where PortCloak keeps its files" panel.
type LocationView struct {
	// Root is the folder itself; ConfigFile is the file inside it that an
	// operator is most likely to want to open by hand.
	Root       string `json:"root"`
	ConfigFile string `json:"configFile"`
	// Source is how the location was decided: "default", "chosen" or
	// "environment". The last of those cannot be changed from here.
	Source string `json:"source"`
	// SourceNote says the same thing in a sentence.
	SourceNote string `json:"sourceNote"`
	// Default is ~/.portcloak, reported whether or not it is in force, so the
	// screen can offer the way back.
	Default string `json:"default"`
	// Pointer is the file that records a chosen folder. It sits outside the
	// tree, because a note saying where the folder went cannot live in the
	// folder that went.
	Pointer   string `json:"pointer"`
	Movable   bool   `json:"movable"`
	AtDefault bool   `json:"atDefault"`
	// Blocked is why a move would be refused right now — an open snapshot, a
	// running job — and empty when nothing is in the way.
	Blocked string `json:"blocked"`
	Note    string `json:"note"`
	// Credentials reports, per entry, whether its keychain secret is on this
	// machine — which is what turns "copied the config across" from an obscure
	// connection failure into a prompt.
	Credentials []config.CredentialStatus `json:"credentials"`
	Failure     *Failure                  `json:"failure,omitempty"`
}

// Location describes where configuration lives, how that was decided, and what
// is in it.
func (s *SettingsController) Location() (res LocationView) {
	defer func() { res = lists(res) }()
	return s.location(nil)
}

func (s *SettingsController) location(failure *Failure) LocationView {
	home := s.eng.Home()
	out := LocationView{
		Root:        home.Root,
		ConfigFile:  home.ConfigFile(),
		Note:        "Plain YAML. Read it, diff it, commit it, hand-edit a hostname. PortCloak re-reads it on launch. No credential is ever written here; only keychain handles.",
		Credentials: s.eng.Config.CheckCredentials(s.eng.Creds),
		Failure:     failure,
	}

	loc, err := config.Locate()
	if err != nil {
		out.Source = string(config.HomeDefault)
		out.SourceNote = "PortCloak could not work out where your home folder is."
		return out
	}
	// The source comes off the engine, not off Locate: Locate answers for a
	// caller with nothing to say about the folder, and a caller that passed
	// --home has something to say. Reading it from Locate reported a folder
	// named on the command line as the default, and then offered to move it.
	// The default and the pointer path are Locate's to know either way.
	source := s.eng.HomeSource()
	out.Source = string(source)
	out.Default = loc.Default
	out.Pointer = loc.Pointer
	out.AtDefault = source == config.HomeDefault

	switch source {
	case config.HomePinned:
		out.SourceNote = "PORTCLOAK_HOME is set in this application's environment, and it wins over anything chosen here."
		out.Blocked = "Unset PORTCLOAK_HOME and restart PortCloak to choose a folder from this screen."
	case config.HomeFlag:
		// The window never passes an override, so this arm is unreachable from
		// the desktop app today. It is here because the alternative is falling
		// through to the default, which reports the folder as movable — and a
		// folder named on a command line is the one thing that certainly is
		// not, because there is nowhere to record a different choice.
		out.SourceNote = "This folder was named on the command line for this run."
		out.Blocked = "Run PortCloak without --home to choose a folder from this screen."
	case config.HomeChosen:
		out.Movable = true
		out.SourceNote = "You chose this folder. The choice is recorded in the file below, which is why it survives an update."
	default:
		out.Movable = true
		out.SourceNote = "The default. Nothing has been chosen, so PortCloak uses the folder beside your other dotfiles."
	}

	if out.Movable {
		if err := s.eng.idleForRelocation(); err != nil {
			out.Blocked = Fail(err).Message
		}
	}
	return out
}

// Move relocates the whole folder — config.yaml, job checkpoints, the audit
// log, the logs — and rebinds the running application to it, so nothing has to
// be restarted.
//
// It returns the panel again rather than a bare failure: whether the move
// happened or not, the one thing the screen has to show afterwards is where
// PortCloak is now actually reading from.
func (s *SettingsController) Move(folder string) (res LocationView) {
	defer func() { res = lists(res) }()
	if err := s.eng.Relocate(folder); err != nil {
		return s.location(Fail(err))
	}
	_ = s.eng.Audit.Record(obs.AuditEntry{
		Action: obs.ActionHomeMoved, Outcome: "moved",
		Detail: s.eng.Home().Root,
	})
	return s.location(nil)
}

// UseDefault moves the folder back to ~/.portcloak.
func (s *SettingsController) UseDefault() (res LocationView) {
	defer func() { res = lists(res) }()
	if err := s.eng.UseDefaultLocation(); err != nil {
		return s.location(Fail(err))
	}
	_ = s.eng.Audit.Record(obs.AuditEntry{
		Action: obs.ActionHomeMoved, Outcome: "moved",
		Detail: s.eng.Home().Root,
	})
	return s.location(nil)
}

// OrphanView is one ephemeral clone a previous session left behind.
type OrphanView struct {
	target.Orphan
	Age         string `json:"age"`
	Description string `json:"description"`
}

// OrphanReport is the sweep's result.
type OrphanReport struct {
	Orphans []OrphanView `json:"orphans"`
	// Unchecked lists environments that could not be reached. They are reported
	// as unchecked, never as clean.
	Unchecked []UncheckedEnvironment `json:"unchecked"`
	Note      string                 `json:"note"`
}

// UncheckedEnvironment is an environment the sweep could not reach.
type UncheckedEnvironment struct {
	Environment string `json:"environment"`
	Reason      string `json:"reason"`
}

// Orphans looks for ephemeral clones left behind by a crash.
//
// Removal is offered, never automatic — the operator's cluster is not ours to
// garbage-collect without asking.
//
// A clone belonging to a job this process is running is not an orphan. It is
// the clone that job is exporting through, and the only thing distinguishing it
// from an abandoned one is whether anything is still driving it. Listing it here
// described a working capture as wreckage, and offered a button that would have
// destroyed the export mid-realm.
func (s *SettingsController) Orphans() (res OrphanReport) {
	defer func() { res = lists(res) }()
	cfg := s.eng.Config.Config()
	report := OrphanReport{}
	now := time.Now()
	running := s.runningJobs()

	for _, env := range cfg.Environments {
		if env.Kind != config.EnvDocker && env.Kind != config.EnvKubernetes {
			// Local and SSH have no clone to orphan.
			continue
		}
		exec, err := s.eng.executorFor(env)
		if err != nil {
			report.Unchecked = append(report.Unchecked, UncheckedEnvironment{
				Environment: env.Name, Reason: err.Error(),
			})
			continue
		}

		sweeper, ok := exec.(target.Sweeper)
		if !ok {
			_ = exec.Close()
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		found, err := sweeper.FindOrphans(ctx)
		cancel()
		_ = exec.Close()

		if err != nil {
			report.Unchecked = append(report.Unchecked, UncheckedEnvironment{
				Environment: env.Name, Reason: err.Error(),
			})
			continue
		}
		for _, o := range abandoned(found, running) {
			report.Orphans = append(report.Orphans, OrphanView{
				Orphan:      o,
				Age:         renderAge(o.Age(now)),
				Description: clone.DescribeOrphan(o, now),
			})
		}
	}

	sort.Slice(report.Orphans, func(i, j int) bool {
		return report.Orphans[i].CreatedAt.Before(report.Orphans[j].CreatedAt)
	})

	switch {
	case len(report.Orphans) == 0 && len(report.Unchecked) == 0:
		report.Note = "No ephemeral clones were left behind."
	case len(report.Orphans) == 0:
		report.Note = fmt.Sprintf("Nothing was found, but %d environment%s could not be checked, so this is not a clean bill of health.",
			len(report.Unchecked), plural(len(report.Unchecked)))
	default:
		report.Note = "Left behind when a session crashed mid-capture. A clone serves nothing, but it holds the same database credentials as the serving instance and consumes capacity."
	}
	return report
}

// abandoned drops the clones that belong to a job this process is driving.
//
// Those are not orphans. They are the clones those jobs are exporting through,
// and the only thing that distinguishes one from an abandoned clone is whether
// anything is still driving it.
//
// A clone with no job label is kept. It cannot be tied to a run, and the
// alternative — reporting nothing whenever anything is running — would hide
// real wreckage for the length of a capture.
func abandoned(found []target.Orphan, running map[string]bool) []target.Orphan {
	out := make([]target.Orphan, 0, len(found))
	for _, o := range found {
		if o.JobID != "" && running[o.JobID] {
			continue
		}
		out = append(out, o)
	}
	return out
}

// refuseIfRunning stops a clone being destroyed out from under the job using it.
//
// Where the platform cannot be listed, the answer depends on what is at stake:
// with nothing running there is nothing to protect and the removal goes ahead,
// and with a job in flight it does not, because an unverifiable delete of a
// clone is a capture that dies mid-realm and a snapshot that never existed.
func refuseIfRunning(
	ctx context.Context, sweeper target.Sweeper, running map[string]bool, ref string,
) *Failure {
	if len(running) == 0 {
		return nil
	}

	found, err := sweeper.FindOrphans(ctx)
	if err != nil {
		return &Failure{
			Message: fmt.Sprintf("PortCloak could not check whether that clone belongs to a running job, and %d job%s in flight.",
				len(running), isAre(len(running))),
			Hint: "A clone that a capture is exporting through must not be removed underneath it. Wait for the job to finish, or cancel it from Activity, and try again.",
		}
	}

	for _, o := range found {
		if o.Ref != ref {
			continue
		}
		if o.JobID != "" && running[o.JobID] {
			return &Failure{
				Message: "That clone belongs to a job that is running now, so it is not an orphan.",
				Hint:    "Cancel the job from Activity if you want it stopped. Cancelling destroys the clone as part of the teardown, which is the path that also cleans up what it wrote.",
			}
		}
		return nil
	}
	// Not in the listing: already gone, or never there. Removal is harmless.
	return nil
}

// runningJobs is the set of jobs this process is driving right now.
func (s *SettingsController) runningJobs() map[string]bool {
	out := map[string]bool{}
	for _, id := range s.eng.Orch.Running() {
		out[id] = true
	}
	return out
}

// RemoveOrphan deletes one clone, on the operator's say-so.
func (s *SettingsController) RemoveOrphan(environment, ref string) *Failure {
	cfg := s.eng.Config.Config()
	env, ok := cfg.Environment(environment)
	if !ok {
		return Fail(config.ErrNotFound)
	}
	exec, err := s.eng.executorFor(env)
	if err != nil {
		return Fail(err)
	}
	defer exec.Close() //nolint:errcheck

	sweeper, ok := exec.(target.Sweeper)
	if !ok {
		return &Failure{Message: "That environment cannot have orphaned clones."}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// Checked again here, not only when the list was drawn. The panel an
	// operator is looking at was accurate when it loaded; a capture started
	// since is driving a clone that was not on it, and on Docker the reference
	// is a container id, so nothing about the string itself says which job it
	// belongs to. The label does, and reading it costs one list call before an
	// irreversible act.
	if fail := refuseIfRunning(ctx, sweeper, s.runningJobs(), ref); fail != nil {
		return fail
	}

	if err := sweeper.RemoveOrphan(ctx, ref); err != nil {
		return Fail(err)
	}
	_ = s.eng.Audit.Record(obs.AuditEntry{
		Action: obs.ActionOrphanRemoved, Outcome: "removed",
		Environment: environment, Detail: ref,
	})
	return nil
}

// WorkingData is what PortCloak is holding on this machine.
type WorkingData struct {
	IndexCount      int    `json:"indexCount"`
	IndexBytes      int64  `json:"indexBytes"`
	IndexNote       string `json:"indexNote"`
	FinishedJobs    int    `json:"finishedJobs"`
	FinishedBytes   int64  `json:"finishedBytes"`
	InterruptedJobs int    `json:"interruptedJobs"`
	WorkBytes       int64  `json:"workBytes"`
	LogBytes        int64  `json:"logBytes"`
	// Keeps states what a purge will not touch, which is the more important
	// half of the sentence.
	Keeps []string `json:"keeps"`
	Note  string   `json:"note"`
}

// WorkingData reports what a purge would remove and what it would leave.
func (s *SettingsController) WorkingData() (res WorkingData) {
	defer func() { res = lists(res) }()
	out := WorkingData{
		Keeps: []string{
			"config.yaml holds your environments, storage definitions and preferences",
			"this machine's keychain entries",
			"every snapshot in storage. Purging local data is never a way to lose a backup",
			"any interrupted job that can still be resumed",
		},
	}

	open := s.eng.OpenSessionIDs()
	if entries, err := os.ReadDir(s.eng.Home().IndexDir()); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sqlite") {
				continue
			}
			out.IndexCount++
			if info, err := e.Info(); err == nil {
				out.IndexBytes += info.Size()
			}
		}
	}
	switch {
	case len(open) > 0:
		out.IndexNote = fmt.Sprintf("%d snapshot%s open. Closing them removes their indexes.", len(open), plural(len(open)))
	case out.IndexCount == 0:
		out.IndexNote = "none: no snapshot is open"
	default:
		out.IndexNote = fmt.Sprintf("%d left by a previous session", out.IndexCount)
	}

	if jobs, err := s.eng.Jobs.List(); err == nil {
		for _, j := range jobs {
			if j.State.Terminal() {
				out.FinishedJobs++
				if info, err := os.Stat(s.eng.Home().JobFile(j.ID)); err == nil {
					out.FinishedBytes += info.Size()
				}
			}
			if j.State == config.JobInterrupted {
				out.InterruptedJobs++
			}
		}
	}
	out.WorkBytes = dirSize(s.eng.Home().WorkDir())
	out.LogBytes = dirSize(s.eng.Home().LogsDir())

	out.Note = "Purging clears inspection indexes, decrypted working files, finished job records and rotated logs."
	return out
}

func dirSize(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

// PurgeResult reports what was removed.
type PurgeResult struct {
	Removed []string `json:"removed"`
	Bytes   int64    `json:"bytes"`
	Note    string   `json:"note"`
	Failure *Failure `json:"failure,omitempty"`
}

// Purge clears local working data.
//
// It never touches config.yaml, the keychain, or a stored snapshot: purging
// local working data must never be a way to accidentally destroy a backup.
// Interrupted jobs are kept too, because discarding one is job control rather
// than housekeeping.
func (s *SettingsController) Purge() (res PurgeResult) {
	defer func() { res = lists(res) }()
	out := PurgeResult{}

	open := s.eng.OpenSessionIDs()
	if count, bytes, err := inspect.SweepIndexes(s.eng.Home(), open); err != nil {
		return PurgeResult{Failure: Fail(err)}
	} else if count > 0 {
		out.Removed = append(out.Removed, fmt.Sprintf("%d inspection index%s", count, pluralES(count)))
		out.Bytes += bytes
	}

	keep := map[string]bool{}
	for id := range open {
		keep["open-"+id] = true
	}
	if jobs, err := s.eng.Jobs.List(); err == nil {
		for _, j := range jobs {
			if !j.State.Terminal() {
				keep[j.ID] = true
			}
		}
	}
	if count, err := inspect.SweepWorkDirs(s.eng.Home(), keep); err != nil {
		return PurgeResult{Failure: Fail(err)}
	} else if count > 0 {
		out.Removed = append(out.Removed, fmt.Sprintf("%d set%s of decrypted working files", count, plural(count)))
	}

	if count, bytes, err := s.eng.Jobs.PurgeFinished(); err != nil {
		return PurgeResult{Failure: Fail(err)}
	} else if count > 0 {
		out.Removed = append(out.Removed, fmt.Sprintf("%d finished job record%s", count, plural(count)))
		out.Bytes += bytes
	}

	if count, bytes := purgeRotatedLogs(s.eng.Home()); count > 0 {
		out.Removed = append(out.Removed, fmt.Sprintf("%d rotated log%s", count, plural(count)))
		out.Bytes += bytes
	}

	_ = s.eng.Audit.Record(obs.AuditEntry{
		Action: obs.ActionPurge, Outcome: "purged",
		Detail: join(out.Removed, ", "),
	})

	if len(out.Removed) == 0 {
		out.Note = "There was nothing local to remove."
	} else {
		out.Note = "Removed " + join(out.Removed, ", ") +
			". Your configuration, this machine's keychain and every stored snapshot are untouched."
	}
	return out
}

// purgeRotatedLogs removes rotated logs but not the current one, which is where
// anything that goes wrong during the purge itself will be recorded.
func purgeRotatedLogs(home config.Home) (count int, bytes int64) {
	entries, err := os.ReadDir(home.LogsDir())
	if err != nil {
		return 0, 0
	}
	current := filepath.Base(home.LogFile())
	for _, e := range entries {
		if e.IsDir() || e.Name() == current {
			continue
		}
		path := filepath.Join(home.LogsDir(), e.Name())
		if info, err := e.Info(); err == nil {
			bytes += info.Size()
		}
		if err := os.Remove(path); err == nil {
			count++
		}
	}
	return count, bytes
}

func pluralES(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}

// AboutView is what the About panel shows: which build this is, and where its
// files live.
//
// The release gate requires version, commit and build date to be visible in the
// app. They are here rather than in a dialog behind the menu bar because on
// Windows and Linux PortCloak has no menu bar at all, and a support detail that
// only exists on one platform is not a support detail.
type AboutView struct {
	Version  string `json:"version"`
	Commit   string `json:"commit"`
	Date     string `json:"date"`
	Go       string `json:"go"`
	Platform string `json:"platform"`
	// Licence is what PortCloak is distributed under. A desktop application is
	// handed to people who never see the repository, so the terms have to be
	// reachable from inside it.
	Licence   string `json:"licence"`
	Copyright string `json:"copyright"`
	// Support is every field above on one line, formatted to be pasted into a
	// bug report without the reporter having to transcribe five values.
	Support string `json:"support"`
	// LogFile is the file to attach to that report.
	LogFile string `json:"logFile"`
}

// About reports the identity of the running build.
func (s *SettingsController) About() AboutView {
	b := s.eng.Build
	return AboutView{
		Version:   b.Version,
		Commit:    b.Commit,
		Date:      b.DisplayDate(),
		Go:        b.Go,
		Platform:  b.Platform,
		Licence:   "Apache License 2.0",
		Copyright: "Copyright 2026 Muhammad Salah <muhammadsalahmasoud@icloud.com>",
		Support:   b.String(),
		LogFile:   s.eng.Home().LogFile(),
	}
}
