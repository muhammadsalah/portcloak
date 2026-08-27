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

// MaintenanceController is the audit log and maintenance screen.
type MaintenanceController struct{ eng *Engine }

// NewMaintenanceController binds the maintenance screen.
func NewMaintenanceController(eng *Engine) *MaintenanceController {
	return &MaintenanceController{eng: eng}
}

// ServiceName is what the Wails binding layer calls this.
func (m *MaintenanceController) ServiceName() string { return "MaintenanceController" }

// AuditView is the audit log panel.
type AuditView struct {
	Entries []obs.AuditEntry `json:"entries"`
	Path    string           `json:"path"`
	// Note states the thing that surprises people: there is no user recorded,
	// because there is no user.
	Note    string   `json:"note"`
	Failure *Failure `json:"failure,omitempty"`
}

// Audit returns the audit log, newest first.
func (m *MaintenanceController) Audit(action string, sinceDays int) (res AuditView) {
	defer func() { res = lists(res) }()
	filter := obs.AuditFilter{Action: obs.Action(action)}
	if sinceDays > 0 {
		filter.Since = time.Now().AddDate(0, 0, -sinceDays)
	}
	entries, err := m.eng.Audit.Read(filter)
	if err != nil {
		return AuditView{Failure: Fail(err)}
	}
	return AuditView{
		Entries: entries, Path: m.eng.Audit.Path(),
		Note: "No user is recorded, because there is none — PortCloak is a single-user local tool. Each entry says what happened and when.",
	}
}

// ConfigFileView is the configuration panel on the maintenance screen.
type ConfigFileView struct {
	Path string `json:"path"`
	Note string `json:"note"`
	// Credentials reports, per entry, whether its keychain secret is on this
	// machine — which is what turns "copied the config across" from an obscure
	// connection failure into a prompt.
	Credentials []config.CredentialStatus `json:"credentials"`
}

// ConfigFile describes where configuration lives and what is in it.
func (m *MaintenanceController) ConfigFile() (res ConfigFileView) {
	defer func() { res = lists(res) }()
	return ConfigFileView{
		Path:        m.eng.Home.ConfigFile(),
		Note:        "Plain YAML — read it, diff it, commit it, hand-edit a hostname. PortCloak re-reads it on launch. No credential is ever written here; only keychain handles.",
		Credentials: m.eng.Config.CheckCredentials(m.eng.Creds),
	}
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
func (m *MaintenanceController) Orphans() (res OrphanReport) {
	defer func() { res = lists(res) }()
	cfg := m.eng.Config.Config()
	report := OrphanReport{}
	now := time.Now()

	for _, env := range cfg.Environments {
		if env.Kind != config.EnvDocker && env.Kind != config.EnvKubernetes {
			// Local and SSH have no clone to orphan.
			continue
		}
		exec, err := m.eng.executorFor(env)
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
		for _, o := range found {
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

// RemoveOrphan deletes one clone, on the operator's say-so.
func (m *MaintenanceController) RemoveOrphan(environment, ref string) *Failure {
	cfg := m.eng.Config.Config()
	env, ok := cfg.Environment(environment)
	if !ok {
		return Fail(config.ErrNotFound)
	}
	exec, err := m.eng.executorFor(env)
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

	if err := sweeper.RemoveOrphan(ctx, ref); err != nil {
		return Fail(err)
	}
	_ = m.eng.Audit.Record(obs.AuditEntry{
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
func (m *MaintenanceController) WorkingData() (res WorkingData) {
	defer func() { res = lists(res) }()
	out := WorkingData{
		Keeps: []string{
			"config.yaml — your environments, storage definitions and preferences",
			"this machine's keychain entries",
			"every snapshot in storage — purging local data is never a way to lose a backup",
			"any interrupted job that can still be resumed",
		},
	}

	open := m.eng.OpenSessionIDs()
	if entries, err := os.ReadDir(m.eng.Home.IndexDir()); err == nil {
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
		out.IndexNote = fmt.Sprintf("%d snapshot%s open — closing them removes their indexes.", len(open), plural(len(open)))
	case out.IndexCount == 0:
		out.IndexNote = "none — no snapshot is open"
	default:
		out.IndexNote = fmt.Sprintf("%d left by a previous session", out.IndexCount)
	}

	if jobs, err := m.eng.Jobs.List(); err == nil {
		for _, j := range jobs {
			if j.State.Terminal() {
				out.FinishedJobs++
				if info, err := os.Stat(m.eng.Home.JobFile(j.ID)); err == nil {
					out.FinishedBytes += info.Size()
				}
			}
			if j.State == config.JobInterrupted {
				out.InterruptedJobs++
			}
		}
	}
	out.WorkBytes = dirSize(m.eng.Home.WorkDir())
	out.LogBytes = dirSize(m.eng.Home.LogsDir())

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
func (m *MaintenanceController) Purge() (res PurgeResult) {
	defer func() { res = lists(res) }()
	out := PurgeResult{}

	open := m.eng.OpenSessionIDs()
	if count, bytes, err := inspect.SweepIndexes(m.eng.Home, open); err != nil {
		return PurgeResult{Failure: Fail(err)}
	} else if count > 0 {
		out.Removed = append(out.Removed, fmt.Sprintf("%d inspection index%s", count, pluralES(count)))
		out.Bytes += bytes
	}

	keep := map[string]bool{}
	for id := range open {
		keep["open-"+id] = true
	}
	if jobs, err := m.eng.Jobs.List(); err == nil {
		for _, j := range jobs {
			if !j.State.Terminal() {
				keep[j.ID] = true
			}
		}
	}
	if count, err := inspect.SweepWorkDirs(m.eng.Home, keep); err != nil {
		return PurgeResult{Failure: Fail(err)}
	} else if count > 0 {
		out.Removed = append(out.Removed, fmt.Sprintf("%d set%s of decrypted working files", count, plural(count)))
	}

	if count, bytes, err := m.eng.Jobs.PurgeFinished(); err != nil {
		return PurgeResult{Failure: Fail(err)}
	} else if count > 0 {
		out.Removed = append(out.Removed, fmt.Sprintf("%d finished job record%s", count, plural(count)))
		out.Bytes += bytes
	}

	if count, bytes := purgeRotatedLogs(m.eng.Home); count > 0 {
		out.Removed = append(out.Removed, fmt.Sprintf("%d rotated log%s", count, plural(count)))
		out.Bytes += bytes
	}

	_ = m.eng.Audit.Record(obs.AuditEntry{
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
