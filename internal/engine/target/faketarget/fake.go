// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package faketarget is the in-memory Executor every engine test runs against.
//
// It exists so `go test ./internal/engine/...` passes with no network, no
// Docker and no Keycloak present. If a test in the engine needs a real target,
// a fake is missing rather than the test being justified.
package faketarget

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"portcloak/internal/engine/target"
)

// ExitPath names the way a job ended, for the teardown assertion.
type ExitPath string

const (
	ExitSuccess    ExitPath = "success"
	ExitRunFails   ExitPath = "run-fails"
	ExitFetchFails ExitPath = "fetch-fails"
	ExitCancelled  ExitPath = "cancelled"
	ExitPanic      ExitPath = "panic"
)

// Executor is a scriptable Executor.
type Executor struct {
	// Facts is what Probe returns.
	Facts target.TargetFacts
	// ProbeErr makes Probe fail outright.
	ProbeErr error
	// PrepareErr makes Prepare fail.
	PrepareErr error
	// CloneRef, when set, makes this executor behave like a clone platform.
	CloneRef string
	// ExportDir is a directory on disk whose contents are served as the export
	// for every realm.
	ExportDir string
	// PerRealm maps a realm name to its own export directory, for multi-realm
	// runs where one realm is expected to fail.
	PerRealm map[string]string
	// RunFunc overrides what Run does. It receives the command and the realm
	// it was built for.
	RunFunc func(ctx context.Context, cmd target.Command) (target.ExecResult, error)
	// HelpOutput is what `kc.sh <sub> --help-all` prints. The orchestrator asks
	// before it builds an export, because which port options kc.sh accepts is a
	// property of the binary and has changed between Keycloak releases. The
	// default mimics 25.0–26.3: a management port and no --http-port.
	HelpOutput string
	// HelpExitCode makes the options question fail, for the path where nothing
	// is discovered and no port option may be passed.
	HelpExitCode int
	// FetchErr makes FetchDir fail.
	FetchErr error
	// TeardownErr makes Teardown fail.
	TeardownErr error

	mu sync.Mutex
	// Prepared and TornDown record the lifecycle, so a test can assert that
	// teardown ran on whichever exit path it forced.
	Prepared  bool
	TornDown  bool
	Teardowns int
	Commands  []target.Command
	Pushed    map[string][]byte
	workDir   string
}

// New builds a fake serving one export directory.
func New(exportDir string) *Executor {
	return &Executor{
		ExportDir: exportDir,
		Facts: target.TargetFacts{
			Kind:            "local",
			Reachable:       true,
			KeycloakVersion: "25.0.2",
			KcPath:          "/opt/keycloak/bin/kc.sh",
			TempDir:         "/tmp",
			FreeBytes:       42 << 30,
			HasTar:          true,
			Mode:            target.ModeInPlace,
			Ports:           target.PortSet{HTTP: 41823, HTTPS: 41824, Management: 41825},
			ProbedAt:        time.Now(),
			ReadOnlyNote:    "Nothing was written. The probe only reads.",
		},
		Pushed: map[string][]byte{},
	}
}

func (e *Executor) Probe(ctx context.Context) (target.TargetFacts, error) {
	if e.ProbeErr != nil {
		return target.TargetFacts{}, e.ProbeErr
	}
	f := e.Facts
	if e.CloneRef != "" {
		f.Mode = target.ModeEphemeralClone
		f.CloneCapable = true
	}
	return f, nil
}

func (e *Executor) Prepare(ctx context.Context, opts target.PrepareOptions) (target.ExecContext, error) {
	if e.PrepareErr != nil {
		return target.ExecContext{}, e.PrepareErr
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	dir, err := os.MkdirTemp("", "faketarget-")
	if err != nil {
		return target.ExecContext{}, err
	}
	e.workDir = dir
	e.Prepared = true

	mode := target.ModeInPlace
	if e.CloneRef != "" {
		mode = target.ModeEphemeralClone
	}
	return target.ExecContext{
		Mode: mode, CloneRef: e.CloneRef, WorkDir: dir, Ports: e.Facts.Ports,
	}, nil
}

// Run pretends to be kc.sh: it copies the fixture export into the directory the
// command asked for, so FetchDir has something real to stream.
func (e *Executor) Run(ctx context.Context, cmd target.Command) (target.ExecResult, error) {
	e.mu.Lock()
	e.Commands = append(e.Commands, cmd)
	e.mu.Unlock()

	// The options question is answered ahead of RunFunc: a test scripting an
	// export failure is scripting the export, not the question that precedes
	// it, and routing it through the script would make every such test count
	// an attempt that never happened.
	if isHelp(cmd.Args) {
		if e.HelpExitCode != 0 {
			return target.ExecResult{ExitCode: e.HelpExitCode, Stderr: "no such command\n"}, nil
		}
		out := e.HelpOutput
		if out == "" {
			out = DefaultHelpOutput
		}
		return target.ExecResult{ExitCode: 0, Stdout: out}, nil
	}

	if e.RunFunc != nil {
		return e.RunFunc(ctx, cmd)
	}
	if err := ctx.Err(); err != nil {
		return target.ExecResult{}, err
	}

	dir, realmName := argValue(cmd.Args, "--dir"), argValue(cmd.Args, "--realm")
	if dir == "" {
		if file := argValue(cmd.Args, "--file"); file != "" {
			dir = filepath.Dir(file)
		}
	}
	if dir == "" {
		// An empty destination would copy the fixture into the package
		// directory, which looks like a passing test and leaves three JSON
		// files behind in the repository.
		return target.ExecResult{
			ExitCode: 2,
			Stderr:   "ERROR: the command named no export directory\n",
		}, nil
	}
	source := e.ExportDir
	if per, ok := e.PerRealm[realmName]; ok {
		source = per
	}
	if source == "" {
		return target.ExecResult{
			ExitCode: 1,
			Stderr:   fmt.Sprintf("ERROR: Realm '%s' not found\n", realmName),
		}, nil
	}
	// A fixture is written for one realm; serving it as another means renaming
	// its files, because the export names its output after the realm and the
	// driver reads the layout back by that name.
	if err := copyTree(source, dir, realmName); err != nil {
		return target.ExecResult{}, err
	}
	if cmd.OnStdout != nil {
		cmd.OnStdout(fmt.Sprintf("Exported realm %s", realmName))
	}
	return target.ExecResult{ExitCode: 0, Stdout: "Export finished\n", Duration: time.Millisecond}, nil
}

// DefaultHelpOutput is shaped like the real `kc.sh export --help-all` of
// Keycloak 25.0 through 26.3: the option definitions start at column 0, the
// descriptions wrap indented, and there is no --http-port. The full captured
// output of four real versions lives in testdata/kc-help.
const DefaultHelpOutput = `Export data from realms to a file or directory.

Options:

-h, --help           This help message.
--help-all           This same help message but with additional options.
--optimized          Use this option to achieve an optimal startup time.

Management:

--http-management-port <port>
                     Port of the management interface. Relevant only when
                       something is already listening on the default.

Export:

--dir <dir>          Set the path to a directory where files will be created.
--file <file>        Set the path to a file that will be created.
--realm <realm>      Set the name of the realm to export.
--users <strategy>   Set how users should be exported.
--users-per-file <number>
                     Set the number of users per file.
`

// isHelp reports whether a command is the options question rather than work.
func isHelp(args []string) bool {
	for _, a := range args {
		if a == "--help-all" || a == "--help" {
			return true
		}
	}
	return false
}

func (e *Executor) FetchDir(ctx context.Context, remote string, sink target.ArtifactSink) error {
	if e.FetchErr != nil {
		return e.FetchErr
	}
	var files []string
	err := filepath.WalkDir(remote, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)

	for _, p := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		st, err := os.Stat(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(remote, p)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		err = sink.Artifact(ctx, target.Artifact{Name: filepath.ToSlash(rel), Size: st.Size()}, f)
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *Executor) PushFile(ctx context.Context, remote string, size int64, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Pushed[remote] = b
	return nil
}

func (e *Executor) Teardown(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Teardowns++
	if e.TeardownErr != nil {
		return e.TeardownErr
	}
	if e.workDir != "" {
		_ = os.RemoveAll(e.workDir)
		e.workDir = ""
	}
	e.TornDown = true
	return nil
}

func (e *Executor) Close() error { return nil }

// WasTornDown reports whether teardown completed.
func (e *Executor) WasTornDown() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.TornDown
}

// Reset clears the recorded lifecycle, so a test that seeds through one phase
// can assert cleanly about the next.
func (e *Executor) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Commands = nil
	e.Pushed = map[string][]byte{}
	e.Teardowns = 0
	e.TornDown = false
}

// RunCount is how many commands were executed.
func (e *Executor) RunCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.Commands)
}

// LastCommand returns the most recent command.
func (e *Executor) LastCommand() (target.Command, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.Commands) == 0 {
		return target.Command{}, false
	}
	return e.Commands[len(e.Commands)-1], true
}

func argValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, flag+"="); ok {
			return v
		}
	}
	return ""
}

func copyTree(src, dst, realmName string) error {
	fixtureRealm := ""
	_ = filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if name, ok := strings.CutSuffix(d.Name(), "-realm.json"); ok {
			fixtureRealm = name
		}
		return nil
	})

	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if fixtureRealm != "" && realmName != "" && fixtureRealm != realmName {
			rel = strings.Replace(rel, fixtureRealm+"-", realmName+"-", 1)
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close() //nolint:errcheck // read-only.
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer out.Close() //nolint:errcheck
		_, err = io.Copy(out, in)
		return err
	})
}

// Sweeper is a fake clone platform, for the teardown and orphan-sweep tests.
type Sweeper struct {
	mu      sync.Mutex
	orphans []target.Orphan
	Removed []string
	// FindErr makes the sweep fail for this environment, which must be reported
	// as unchecked rather than as clean.
	FindErr error
}

// NewSweeper builds a fake sweeper holding some orphans.
func NewSweeper(orphans ...target.Orphan) *Sweeper {
	return &Sweeper{orphans: orphans}
}

func (s *Sweeper) FindOrphans(ctx context.Context) ([]target.Orphan, error) {
	if s.FindErr != nil {
		return nil, s.FindErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]target.Orphan, len(s.orphans))
	copy(out, s.orphans)
	return out, nil
}

func (s *Sweeper) RemoveOrphan(ctx context.Context, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, o := range s.orphans {
		if o.Ref == ref {
			s.orphans = append(s.orphans[:i], s.orphans[i+1:]...)
			s.Removed = append(s.Removed, ref)
			return nil
		}
	}
	return fmt.Errorf("no orphan named %s", ref)
}

// FixtureName joins an export fixture path.
func FixtureName(dir, name string) string { return path.Join(dir, name) }
