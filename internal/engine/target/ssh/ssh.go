// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package ssh captures from a Keycloak on a remote host.
//
// There is no clone to make here: the host is the execution context. Port
// isolation is the available lever and it is sufficient, because the export runs
// as a separate operating-system process rather than inside the serving one.
//
// SSH is also the most drop-prone path PortCloak has, so everything here is
// written on the assumption that the connection will fail partway.
package ssh

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	cryptossh "golang.org/x/crypto/ssh"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/kc"
	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/target"
	"portcloak/internal/engine/target/sshx"
)

// Executor is the SSH target adapter.
type Executor struct {
	env config.Environment
	cfg sshx.Config

	mu       sync.Mutex
	conn     *sshx.Conn
	workDir  string
	prepared bool
}

// New builds an SSH executor.
func New(env config.Environment, creds config.CredentialStore) (*Executor, error) {
	cfg, err := sshx.FromEnvironment(env, creds)
	if err != nil {
		return nil, err
	}
	return &Executor{env: env, cfg: cfg}, nil
}

// AcceptHostKey records the operator's decision to trust a first connection.
func (e *Executor) AcceptHostKey() { e.cfg.AcceptNewHostKey = true }

func (e *Executor) connect(ctx context.Context) (*sshx.Conn, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.conn != nil {
		return e.conn, nil
	}
	conn, err := sshx.Dial(ctx, e.cfg)
	if err != nil {
		return nil, err
	}
	e.conn = conn
	return conn, nil
}

// Probe reads facts about the remote installation without changing anything.
func (e *Executor) Probe(ctx context.Context) (target.TargetFacts, error) {
	facts := target.TargetFacts{
		Kind:         string(config.EnvSSH),
		Mode:         target.ModeInPlace,
		ProbedAt:     time.Now(),
		ReadOnlyNote: "Nothing was written to the host. The probe only reads.",
	}

	conn, err := e.connect(ctx)
	if err != nil {
		facts.Fail("Connection", fmt.Sprintf("%s@%s — %v", e.env.User, e.env.Host, err), resil.Hint(err))
		return facts, nil
	}
	facts.Reachable = true
	facts.Pass("Connection", fmt.Sprintf("%s@%s", e.env.User, conn.Address()))

	kcPath := path.Join(strings.TrimRight(e.env.ServerFolder, "/"), "bin", "kc.sh")
	if out, code, err := e.runSimple(ctx, conn, "test -x "+shellQuote(kcPath)+" && echo ok"); err != nil || code != 0 || !strings.Contains(out, "ok") {
		facts.Fail("kc.sh", kcPath+" — not found or not executable",
			"That is the path PortCloak looked for on the remote host. If Keycloak lives elsewhere, correct the server folder.")
		return facts, nil
	}
	facts.KcPath = kcPath
	facts.Pass("kc.sh", kcPath)

	if e.env.Sudo {
		// Verifying elevation can actually be obtained non-interactively is the
		// difference between a message now and a hang at step four.
		if _, code, _ := e.runSimple(ctx, conn, "sudo -n true"); code != 0 {
			facts.Fail("sudo", "not available without a password",
				"This environment is set to use sudo, but the account cannot obtain it non-interactively.")
			return facts, nil
		}
		facts.Pass("sudo", "available without a password")
	}

	versionCmd := shellQuote(kcPath) + " --version 2>&1 || true"
	if e.env.Sudo {
		versionCmd = "sudo -n " + versionCmd
	}
	if out, _, err := e.runSimple(ctx, conn, versionCmd); err == nil {
		if v := kc.ParseVersion(out); v != "" {
			facts.KeycloakVersion = v
			facts.Pass("Keycloak version", v)
		}
	}
	if facts.KeycloakVersion == "" {
		facts.Warn("Keycloak version", "could not be determined",
			"The export will still run. The version is recorded when it is known.")
	}

	tempDir := "/tmp"
	facts.TempDir = tempDir
	if out, code, err := e.runSimple(ctx, conn, "df -Pk "+tempDir+" | tail -1 | awk '{print $4}'"); err == nil && code == 0 {
		if kb, convErr := strconv.ParseInt(strings.TrimSpace(out), 10, 64); convErr == nil {
			facts.FreeBytes = kb * 1024
			facts.Pass("Free space for the export", fmt.Sprintf("%s on %s", humanBytes(facts.FreeBytes), tempDir))
		}
	}
	if facts.FreeBytes == 0 {
		facts.Skipped("Free space for the export", "could not be determined on the remote host")
	}

	if out, code, err := e.runSimple(ctx, conn, "command -v tar >/dev/null && echo yes || echo no"); err == nil && code == 0 {
		facts.HasTar = strings.Contains(out, "yes")
	}

	// The port problem exists remotely too, and is harder to see there, so the
	// probe asks the remote host for free ports rather than assuming.
	set, err := e.allocateRemotePorts(ctx, conn)
	if err != nil {
		facts.Fail("Free ports", "could not be allocated on the remote host",
			"The export needs three free ports so it cannot collide with the Keycloak already running there.")
		return facts, nil
	}
	facts.Ports = set
	facts.Pass("Free ports", set.String()+" allocated on the host")

	facts.CloneCapable = false
	facts.CloneDetail = "not applicable — the export runs as a separate process on the host, isolated by the ports above"
	facts.AddCheck(target.Check{Name: "Ephemeral clone", Value: facts.CloneDetail, Status: target.CheckPass})

	return facts, nil
}

// allocateRemotePorts asks the remote host for three ports it is not using.
//
// The same release-then-bind race exists here as locally, and is handled the
// same way: a bind conflict is retryable and the whole allocation is redone.
func (e *Executor) allocateRemotePorts(ctx context.Context, conn *sshx.Conn) (target.PortSet, error) {
	// The fallback runs one awk rather than three.
	//
	// It used to call `awk 'BEGIN{srand();...}'` once per port. srand() with no
	// argument seeds from the clock in whole seconds, so three calls inside the
	// same second draw the same number and the host reported one port three
	// times — which is what an Alpine image with no python3 did on every run,
	// and what the contract means by "three distinct values".
	//
	// One process is seeded once and asked for three values, so they come from
	// a sequence rather than three identical draws, and a set rejects a repeat.
	// The seed comes from /dev/urandom rather than the clock so that a retried
	// allocation — the documented answer to a bind conflict — proposes
	// different ports instead of the same ones a second later.
	const script = `python3 - <<'PY' 2>/dev/null || ` +
		`{ s=$(od -An -N4 -tu4 /dev/urandom 2>/dev/null | tr -dc 0-9); ` +
		`  awk -v s="$s" 'BEGIN{if(s=="")srand();else srand(s);n=0;` +
		`while(n<3){p=int(20000+rand()*20000);if(!(p in seen)){seen[p]=1;print p;n++}}}' | ` +
		`  while read p; do ` +
		`    (command -v ss >/dev/null && ss -ltn 2>/dev/null | grep -q ":$p ") || ` +
		`    (command -v netstat >/dev/null && netstat -ltn 2>/dev/null | grep -q ":$p ") || ` +
		`    echo $p; ` +
		`  done; }
import socket
ports = []
socks = []
for _ in range(3):
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    ports.append(s.getsockname()[1])
    socks.append(s)
for s in socks:
    s.close()
print("\n".join(str(p) for p in ports))
PY`

	out, code, err := e.runSimple(ctx, conn, script)
	if err != nil {
		return target.PortSet{}, err
	}
	if code != 0 {
		return target.PortSet{}, resil.Retry("allocate free ports on the host",
			"PortCloak could not find three free ports on the remote host.", nil)
	}

	var got []int
	for _, line := range strings.Fields(out) {
		if n, convErr := strconv.Atoi(strings.TrimSpace(line)); convErr == nil && n > 0 {
			got = append(got, n)
		}
	}
	if len(got) < 3 {
		return target.PortSet{}, resil.Retry("allocate free ports on the host",
			"The remote host did not report three usable ports.", nil)
	}
	return target.PortSet{HTTP: got[0], HTTPS: got[1], Management: got[2]}, nil
}

// Prepare creates the remote temp directory and allocates ports.
func (e *Executor) Prepare(ctx context.Context, opts target.PrepareOptions) (target.ExecContext, error) {
	conn, err := e.connect(ctx)
	if err != nil {
		return target.ExecContext{}, err
	}

	set, err := e.allocateRemotePorts(ctx, conn)
	if err != nil {
		return target.ExecContext{}, err
	}

	dir := target.WorkDirFor(opts.JobID)
	// 0700 because the export directory holds unmasked secrets on the remote
	// host from the moment kc.sh starts writing.
	if _, code, err := e.runSimple(ctx, conn, "mkdir -p "+shellQuote(dir)+" && chmod 700 "+shellQuote(dir)); err != nil {
		return target.ExecContext{}, err
	} else if code != 0 {
		return target.ExecContext{}, resil.Fatal("create the export directory",
			fmt.Sprintf("PortCloak could not create %s on %s.", dir, e.env.Host), nil)
	}

	e.mu.Lock()
	e.workDir, e.prepared = dir, true
	e.mu.Unlock()

	return target.ExecContext{Mode: target.ModeInPlace, WorkDir: dir, Ports: set}, nil
}

// Run executes a command on the remote host, streaming both output streams.
func (e *Executor) Run(ctx context.Context, cmd target.Command) (target.ExecResult, error) {
	conn, err := e.connect(ctx)
	if err != nil {
		return target.ExecResult{}, err
	}
	session, err := conn.Client().NewSession()
	if err != nil {
		return target.ExecResult{}, resil.Retry("open a command channel",
			fmt.Sprintf("PortCloak could not open a session on %s.", e.env.Host), err)
	}
	defer session.Close() //nolint:errcheck // the channel closes with the session.

	line := buildCommandLine(cmd, e.env.Sudo)

	stdout, err := session.StdoutPipe()
	if err != nil {
		return target.ExecResult{}, err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return target.ExecResult{}, err
	}

	started := time.Now()
	if err := session.Start(line); err != nil {
		return target.ExecResult{}, resil.Retry("run the command",
			fmt.Sprintf("PortCloak could not start the command on %s.", e.env.Host), err)
	}

	var wg sync.WaitGroup
	var outBuf, errBuf strings.Builder
	wg.Add(2)
	go func() { defer wg.Done(); drain(stdout, &outBuf, cmd.OnStdout) }()
	go func() { defer wg.Done(); drain(stderr, &errBuf, cmd.OnStderr) }()

	// Cancellation has to close the session: an SSH command does not stop
	// because the caller stopped waiting for it.
	done := make(chan error, 1)
	go func() { done <- session.Wait() }()

	var waitErr error
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		_ = session.Signal(cryptossh.SIGTERM)
		_ = session.Close()
		<-done
		wg.Wait()
		return target.ExecResult{}, ctx.Err()
	}
	wg.Wait()

	res := target.ExecResult{Stdout: outBuf.String(), Stderr: errBuf.String(), Duration: time.Since(started)}
	if waitErr != nil {
		var exitErr *cryptossh.ExitError
		if ok := asExitError(waitErr, &exitErr); ok {
			res.ExitCode = exitErr.ExitStatus()
			return res, nil
		}
		return res, resil.Retry("run the command",
			fmt.Sprintf("The connection to %s dropped while the command was running.", e.env.Host), waitErr)
	}
	return res, nil
}

func buildCommandLine(cmd target.Command, envSudo bool) string {
	parts := make([]string, 0, len(cmd.Args)+4)
	if cmd.Sudo || envSudo {
		parts = append(parts, "sudo", "-n")
	}
	parts = append(parts, shellQuote(cmd.Path))
	for _, a := range cmd.Args {
		parts = append(parts, shellQuote(a))
	}
	line := strings.Join(parts, " ")

	if len(cmd.Env) > 0 {
		keys := make([]string, 0, len(cmd.Env))
		for k := range cmd.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var prefix strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&prefix, "%s=%s ", k, shellQuote(cmd.Env[k]))
		}
		line = prefix.String() + line
	}
	if cmd.Dir != "" {
		line = "cd " + shellQuote(cmd.Dir) + " && " + line
	}
	return line
}

// FetchDir streams a remote directory back over SFTP.
//
// Per-file granularity is what makes a dropped link cost one file rather than
// the whole fetch, which is why the export is asked for users in separate files
// in the first place.
func (e *Executor) FetchDir(ctx context.Context, remote string, sink target.ArtifactSink) error {
	conn, err := e.connect(ctx)
	if err != nil {
		return err
	}
	client, err := sftp.NewClient(conn.Client())
	if err != nil {
		return resil.Retry("collect the exported files",
			"PortCloak could not open an SFTP channel.", err)
	}
	defer client.Close() //nolint:errcheck

	var files []string
	walker := client.Walk(remote)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return resil.Retry("collect the exported files",
				fmt.Sprintf("PortCloak could not read %s on %s.", remote, e.env.Host), err)
		}
		if walker.Stat().IsDir() {
			continue
		}
		files = append(files, walker.Path())
	}
	sort.Strings(files)

	for _, p := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		st, err := client.Stat(p)
		if err != nil {
			return err
		}
		f, err := client.Open(p)
		if err != nil {
			return resil.Retry("collect the exported files",
				fmt.Sprintf("PortCloak could not open %s.", p), err)
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, remote), "/")
		err = sink.Artifact(ctx, target.Artifact{Name: rel, Size: st.Size()}, f)
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// PushFile writes a file to the remote host, for the restore path.
func (e *Executor) PushFile(ctx context.Context, remote string, size int64, r io.Reader) error {
	conn, err := e.connect(ctx)
	if err != nil {
		return err
	}
	client, err := sftp.NewClient(conn.Client())
	if err != nil {
		return resil.Retry("send the file", "PortCloak could not open an SFTP channel.", err)
	}
	defer client.Close() //nolint:errcheck

	if err := client.MkdirAll(path.Dir(remote)); err != nil {
		return err
	}
	f, err := client.Create(remote)
	if err != nil {
		return resil.Retry("send the file", fmt.Sprintf("PortCloak could not create %s.", remote), err)
	}
	defer f.Close() //nolint:errcheck
	if _, err := io.Copy(f, r); err != nil {
		return resil.Retry("send the file", "The connection dropped while sending.", err)
	}
	return f.Chmod(0o600)
}

// Teardown removes the remote temp directory.
//
// It runs unconditionally: the directory holds unmasked realm secrets on
// someone else's host, which is worse than leaving them on this one.
func (e *Executor) Teardown(ctx context.Context) error {
	e.mu.Lock()
	dir, prepared := e.workDir, e.prepared
	e.workDir, e.prepared = "", false
	e.mu.Unlock()

	if !prepared || dir == "" {
		return nil
	}
	conn, err := e.connect(ctx)
	if err != nil {
		return err
	}
	if _, code, err := e.runSimple(ctx, conn, "rm -rf "+shellQuote(dir)); err != nil || code != 0 {
		return resil.Fatal("clean up the export directory",
			fmt.Sprintf("PortCloak could not remove %s on %s. It holds unmasked realm secrets, so remove it by hand.", dir, e.env.Host), err)
	}
	return nil
}

// Close shuts the connection down.
func (e *Executor) Close() error {
	e.mu.Lock()
	conn := e.conn
	e.conn = nil
	e.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Close()
}

// runSimple runs a short command and collects its output.
func (e *Executor) runSimple(ctx context.Context, conn *sshx.Conn, line string) (string, int, error) {
	session, err := conn.Client().NewSession()
	if err != nil {
		return "", 0, resil.Retry("run a check",
			fmt.Sprintf("PortCloak could not open a session on %s.", e.env.Host), err)
	}
	defer session.Close() //nolint:errcheck

	var out strings.Builder
	session.Stdout = &out
	session.Stderr = &out

	done := make(chan error, 1)
	go func() { done <- session.Run(line) }()

	select {
	case err := <-done:
		if err != nil {
			var exitErr *cryptossh.ExitError
			if ok := asExitError(err, &exitErr); ok {
				return out.String(), exitErr.ExitStatus(), nil
			}
			return out.String(), 0, err
		}
		return out.String(), 0, nil
	case <-ctx.Done():
		_ = session.Close()
		return "", 0, ctx.Err()
	}
}

func drain(r io.Reader, buf *strings.Builder, onLine func(string)) {
	sc := bufio.NewScanner(r)
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

func asExitError(err error, out **cryptossh.ExitError) bool {
	if ee, ok := err.(*cryptossh.ExitError); ok {
		*out = ee
		return true
	}
	return false
}

// shellQuote makes a value safe to pass through a remote shell. A realm name
// comes from someone else's Keycloak, so it is never interpolated raw.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == '/', r == ':', r == '=', r == ',', r == '+':
		default:
			safe = false
		}
		if !safe {
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
