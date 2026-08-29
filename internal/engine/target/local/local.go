// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package local captures from a Keycloak installed on this machine.
//
// There is no clone here — the machine is the execution context — so isolation
// comes from free ports instead. That is sufficient because the export runs as
// a separate operating-system process rather than inside the serving one.
package local

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/kc"
	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/target"
	"portcloak/internal/engine/target/ports"
)

// Executor is the local target adapter.
type Executor struct {
	env config.Environment

	mu       sync.Mutex
	workDir  string
	prepared bool
}

// New builds a local executor for an environment.
func New(env config.Environment) *Executor {
	return &Executor{env: env}
}

// KcPath is where kc.sh (or kc.bat) should be, given the configured server
// folder.
func KcPath(serverFolder string) string {
	name := "kc.sh"
	if runtime.GOOS == "windows" {
		name = "kc.bat"
	}
	return filepath.Join(expand(serverFolder), "bin", name)
}

func expand(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// Probe reads facts about the installation without changing anything.
func (e *Executor) Probe(ctx context.Context) (target.TargetFacts, error) {
	facts := target.TargetFacts{
		Kind:         string(config.EnvLocal),
		Mode:         target.ModeInPlace,
		ProbedAt:     time.Now(),
		ReadOnlyNote: "Nothing was written to this machine. The probe only reads.",
	}

	folder := expand(e.env.ServerFolder)
	if folder == "" {
		facts.Fail("Keycloak server folder", "not set",
			"Point the environment at the install root, the folder containing bin/kc.sh.")
		return facts, nil
	}
	if st, err := os.Stat(folder); err != nil || !st.IsDir() {
		facts.Fail("Keycloak server folder", folder+": not found",
			"Check the path. PortCloak expects the install root, not the bin folder inside it.")
		return facts, nil
	}
	facts.Reachable = true

	kcPath := KcPath(e.env.ServerFolder)
	st, err := os.Stat(kcPath)
	if err != nil {
		// Naming the exact path it looked for is the difference between a
		// fixable message and a puzzle.
		facts.Fail("kc.sh", kcPath+": not found",
			"That is the path PortCloak looked for. If Keycloak lives elsewhere, correct the server folder.")
		return facts, nil
	}
	if st.Mode()&0o111 == 0 && runtime.GOOS != "windows" {
		facts.Fail("kc.sh", kcPath+": not executable",
			"Give it execute permission, or run PortCloak as an account that can.")
		return facts, nil
	}
	facts.KcPath = kcPath
	facts.Pass("kc.sh", kcPath)

	if version := e.readVersion(ctx, kcPath); version != "" {
		facts.KeycloakVersion = version
		facts.Pass("Keycloak version", version)
	} else {
		// A version PortCloak could not read does not stop a capture — kc.sh
		// still runs — but it is worth saying so rather than showing a blank.
		facts.Warn("Keycloak version", "could not be determined",
			"The export will still run. The version is recorded in the snapshot's provenance when it is known.")
	}

	tempDir := os.TempDir()
	facts.TempDir = tempDir
	facts.FreeBytes = freeSpace(tempDir)
	if facts.FreeBytes > 0 {
		facts.Pass("Free space for the export", fmt.Sprintf("%s on %s", humanBytes(facts.FreeBytes), tempDir))
	} else {
		facts.Skipped("Free space for the export", "could not be determined")
	}

	set, err := ports.Allocate()
	if err != nil {
		facts.Fail("Free ports", "none available",
			"The export needs three free ports so it cannot collide with a Keycloak that is already running.")
		return facts, nil
	}
	facts.Ports = set
	facts.Pass("Free ports", set.String()+" allocated")

	// A local install has no clone to make, and saying so plainly is better
	// than leaving the row blank on a screen where the other kinds fill it in.
	facts.CloneCapable = false
	facts.CloneDetail = "not applicable: the export runs as a separate process on this machine, isolated by the ports above"
	facts.AddCheck(target.Check{Name: "Ephemeral clone", Value: facts.CloneDetail, Status: target.CheckPass})

	facts.HasTar = true
	return facts, nil
}

// readVersion runs kc.sh --version. Keycloak writes it to stdout on some
// releases and stderr on others, so both are read.
func (e *Executor) readVersion(ctx context.Context, kcPath string) string {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, kcPath, "--version")
	cmd.Env = e.commandEnv()
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// A non-zero exit is not fatal here: some builds print the banner and then
	// complain about a missing build step.
	_ = cmd.Run()
	return kc.ParseVersion(stdout.String(), stderr.String())
}

func (e *Executor) commandEnv() []string {
	env := os.Environ()
	if e.env.JavaHome != "" {
		env = append(env, "JAVA_HOME="+expand(e.env.JavaHome))
	}
	return env
}

// Prepare creates the temp export directory and allocates ports.
func (e *Executor) Prepare(ctx context.Context, opts target.PrepareOptions) (target.ExecContext, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	set, err := ports.Allocate()
	if err != nil {
		return target.ExecContext{}, err
	}
	dir := filepath.Join(os.TempDir(), "portcloak-"+opts.JobID)
	// 0700 because the export directory holds unmasked secrets from the moment
	// kc.sh starts writing into it.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return target.ExecContext{}, resil.Fatal("create the export directory",
			fmt.Sprintf("PortCloak could not create %s.", dir), err)
	}
	e.workDir, e.prepared = dir, true

	return target.ExecContext{Mode: target.ModeInPlace, WorkDir: dir, Ports: set}, nil
}

// Run executes a command on this machine, streaming both output streams live.
func (e *Executor) Run(ctx context.Context, cmd target.Command) (target.ExecResult, error) {
	started := time.Now()

	path, args := cmd.Path, cmd.Args
	if cmd.Sudo {
		args = append([]string{"-n", path}, args...)
		path = "sudo"
	}

	c := exec.CommandContext(ctx, path, args...)
	c.Env = e.commandEnv()
	for k, v := range cmd.Env {
		c.Env = append(c.Env, k+"="+v)
	}
	if cmd.Dir != "" {
		c.Dir = cmd.Dir
	}

	stdout, err := c.StdoutPipe()
	if err != nil {
		return target.ExecResult{}, err
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		return target.ExecResult{}, err
	}
	if err := c.Start(); err != nil {
		return target.ExecResult{}, resil.Fatal("run kc.sh",
			fmt.Sprintf("PortCloak could not start %s.", path), err)
	}

	var wg sync.WaitGroup
	var outBuf, errBuf strings.Builder
	wg.Add(2)
	go func() { defer wg.Done(); drain(stdout, &outBuf, cmd.OnStdout) }()
	go func() { defer wg.Done(); drain(stderr, &errBuf, cmd.OnStderr) }()
	wg.Wait()

	runErr := c.Wait()
	res := target.ExecResult{
		Stdout:   outBuf.String(),
		Stderr:   errBuf.String(),
		Duration: time.Since(started),
	}
	if runErr != nil {
		var exitErr *exec.ExitError
		if ok := asExitError(runErr, &exitErr); ok {
			res.ExitCode = exitErr.ExitCode()
			return res, nil
		}
		return res, runErr
	}
	return res, nil
}

// drain reads a stream line by line so the UI sees output as it happens rather
// than in one lump when the process ends.
func drain(r io.Reader, buf *strings.Builder, onLine func(string)) {
	sc := bufio.NewScanner(r)
	// kc.sh can emit a very long stack trace on one line.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		buf.WriteString(line)
		buf.WriteByte('\n')
		if onLine != nil {
			onLine(line)
		}
	}
}

// FetchDir streams a directory into a sink, one file at a time.
//
// Even locally this goes through the sink rather than copying the directory,
// so everything downstream stays target-agnostic and the digest is computed as
// the bytes pass rather than in a second read.
func (e *Executor) FetchDir(ctx context.Context, remote string, sink target.ArtifactSink) error {
	var files []string
	err := filepath.WalkDir(remote, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		files = append(files, p)
		return nil
	})
	if err != nil {
		return resil.Fatal("collect the exported files",
			fmt.Sprintf("PortCloak could not read the export directory %s.", remote), err)
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
		artifact := target.Artifact{Name: filepath.ToSlash(rel), Size: st.Size(), Mode: int64(st.Mode().Perm())}
		err = sink.Artifact(ctx, artifact, f)
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// PushFile writes a file into the execution context, for the restore path.
func (e *Executor) PushFile(ctx context.Context, remote string, size int64, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(remote), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(remote, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // Sync below is what commits.
	if _, err := io.Copy(f, r); err != nil {
		return err
	}
	return f.Sync()
}

// Teardown removes the temp export directory.
//
// It runs unconditionally, on success and on every failure path. Even locally
// the export directory holds unmasked secrets on disk, so leaving it behind
// after a successful capture would be the tool creating exactly the exposure it
// exists to manage.
func (e *Executor) Teardown(ctx context.Context) error {
	e.mu.Lock()
	dir, prepared := e.workDir, e.prepared
	e.workDir, e.prepared = "", false
	e.mu.Unlock()

	if !prepared || dir == "" {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return resil.Fatal("clean up the export directory",
			fmt.Sprintf("PortCloak could not remove %s. It holds unmasked realm secrets, so remove it by hand.", dir), err)
	}
	return nil
}

// Close releases nothing; a local target holds no connection.
func (e *Executor) Close() error { return nil }

func asExitError(err error, out **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*out = ee
		return true
	}
	return false
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
