// Package targettest holds the Executor contract suite.
//
// There is one table and every target kind runs it: local, SSH, Docker and
// Kubernetes. The orchestrator knows only that it can Probe, Prepare, Run,
// FetchDir, PushFile and Teardown, so a divergence between adapters is a bug in
// the newest one rather than a reason to fork the table — which is the whole
// point of "capture from anywhere" being one workflow instead of four.
//
// The rows that matter are the awkward ones: fetching a directory that is not
// there, tearing down twice, tearing down when Prepare never ran. Those are
// where an adapter that works in the happy case quietly differs.
package targettest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"

	"portcloak/internal/engine/target"
)

// Factory builds a fresh executor for one subtest, along with a shell command
// that prints its arguments — the adapters run on different operating system
// images, so the table asks for the command rather than assuming one.
type Factory func(t *testing.T) target.Executor

// RunContract exercises every behaviour the orchestrator relies on.
//
// It never invokes kc.sh: what is under test is the seam, not Keycloak. The
// commands are ordinary shell so the table can run against a target that has no
// Keycloak on it at all.
func RunContract(t *testing.T, newExecutor Factory) {
	t.Helper()

	t.Run("probe reads facts and changes nothing", func(t *testing.T) {
		e := newExecutor(t)
		defer e.Close() //nolint:errcheck

		facts, err := e.Probe(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if facts.Kind == "" {
			t.Error("a probe that does not say what it probed cannot be rendered")
		}
		if facts.ProbedAt.IsZero() {
			t.Error("a probe result with no timestamp cannot be aged")
		}
		if facts.Mode != target.ModeInPlace && facts.Mode != target.ModeEphemeralClone {
			t.Errorf("the probe reported execution mode %q", facts.Mode)
		}
		// A probe against production has to be visibly harmless, so the
		// sentence that says so is part of the contract rather than UI copy.
		if strings.TrimSpace(facts.ReadOnlyNote) == "" {
			t.Error("the probe carries no read-only note, so the UI cannot promise it was harmless")
		}
		// Probing must not have created the execution context it reports on.
		if err := e.Teardown(context.Background()); err != nil {
			t.Errorf("tearing down after a probe alone failed: %v", err)
		}
	})

	t.Run("prepare gives a work directory and three distinct ports", func(t *testing.T) {
		e := newExecutor(t)
		defer e.Close()                        //nolint:errcheck
		defer e.Teardown(context.Background()) //nolint:errcheck

		ec, err := e.Prepare(context.Background(), target.PrepareOptions{
			JobID: "01HCONTRACT", Realms: []string{"acme"}, Purpose: "capture",
		})
		if err != nil {
			t.Fatal(err)
		}
		if ec.WorkDir == "" {
			t.Fatal("the execution context has no work directory to export into")
		}
		if !ec.Ports.Allocated() {
			t.Errorf("ports %s were not allocated", ec.Ports)
		}
		// Three distinct ports, because kc.sh binds three and a collision
		// between them fails the export as surely as a collision with the
		// serving instance.
		seen := map[int]bool{ec.Ports.HTTP: true, ec.Ports.HTTPS: true, ec.Ports.Management: true}
		if len(seen) != 3 {
			t.Errorf("ports %s are not three distinct values", ec.Ports)
		}
		if ec.Mode == target.ModeEphemeralClone && ec.CloneRef == "" {
			t.Error("a clone was used but not identified, so provenance cannot name it")
		}
	})

	t.Run("run reports output, exit code and failure separately", func(t *testing.T) {
		e := newExecutor(t)
		defer e.Close()                        //nolint:errcheck
		defer e.Teardown(context.Background()) //nolint:errcheck
		ec := prepare(t, e)

		var out, errOut []string
		res, err := e.Run(context.Background(), target.Command{
			Path: "/bin/sh", Args: []string{"-c", "echo hello; echo trouble 1>&2"},
			Dir:      ec.WorkDir,
			OnStdout: func(l string) { out = append(out, l) },
			OnStderr: func(l string) { errOut = append(errOut, l) },
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.ExitCode != 0 {
			t.Errorf("a successful command exited %d", res.ExitCode)
		}
		if !contains(out, "hello") {
			t.Errorf("stdout was %q", out)
		}
		// The streams stay separate: kc.sh writes its version banner to stderr
		// and a parser that has merged them cannot tell a banner from a fault.
		if !contains(errOut, "trouble") {
			t.Errorf("stderr was %q", errOut)
		}
		if contains(out, "trouble") {
			t.Error("stderr was merged into stdout")
		}

		// A non-zero exit is a result, not a transport error. Only the second
		// can be retried, and confusing them retries a failing export forever.
		res, err = e.Run(context.Background(), target.Command{
			Path: "/bin/sh", Args: []string{"-c", "exit 7"}, Dir: ec.WorkDir,
		})
		if err != nil {
			t.Fatalf("a command that exited non-zero was reported as a transport failure: %v", err)
		}
		if res.ExitCode != 7 {
			t.Errorf("exit code %d, want 7", res.ExitCode)
		}
	})

	t.Run("environment reaches the command", func(t *testing.T) {
		e := newExecutor(t)
		defer e.Close()                        //nolint:errcheck
		defer e.Teardown(context.Background()) //nolint:errcheck
		ec := prepare(t, e)

		var out []string
		if _, err := e.Run(context.Background(), target.Command{
			Path: "/bin/sh", Args: []string{"-c", "echo $PORTCLOAK_CONTRACT"},
			Env:      map[string]string{"PORTCLOAK_CONTRACT": "reached"},
			Dir:      ec.WorkDir,
			OnStdout: func(l string) { out = append(out, l) },
		}); err != nil {
			t.Fatal(err)
		}
		if !contains(out, "reached") {
			t.Errorf("the environment did not reach the command: %q", out)
		}
	})

	t.Run("a cancelled run stops rather than finishing", func(t *testing.T) {
		e := newExecutor(t)
		defer e.Close()                        //nolint:errcheck
		defer e.Teardown(context.Background()) //nolint:errcheck
		ec := prepare(t, e)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		res, err := e.Run(ctx, target.Command{
			Path: "/bin/sh", Args: []string{"-c", "sleep 30"}, Dir: ec.WorkDir,
		})
		if err == nil && res.ExitCode == 0 {
			t.Error("a cancelled run reported clean success")
		}
	})

	t.Run("fetchdir streams every file with its size", func(t *testing.T) {
		e := newExecutor(t)
		defer e.Close()                        //nolint:errcheck
		defer e.Teardown(context.Background()) //nolint:errcheck
		ec := prepare(t, e)

		// A shape the export actually produces: a realm file and per-user
		// files, which is what makes a fetch resumable at file granularity.
		script := fmt.Sprintf(`cd %s && mkdir -p out && `+
			`printf 'realm' > out/acme-realm.json && `+
			`printf 'users-0!' > out/acme-users-0.json && `+
			`printf 'users-1!!' > out/acme-users-1.json`, shellQuote(ec.WorkDir))
		mustRun(t, e, ec, script)

		got := map[string]string{}
		sizes := map[string]int64{}
		err := e.FetchDir(context.Background(), ec.WorkDir+"/out",
			target.SinkFunc(func(_ context.Context, a target.Artifact, r io.Reader) error {
				b, err := io.ReadAll(r)
				if err != nil {
					return err
				}
				got[a.Name] = string(b)
				sizes[a.Name] = a.Size
				return nil
			}))
		if err != nil {
			t.Fatal(err)
		}

		want := map[string]string{
			"acme-realm.json":   "realm",
			"acme-users-0.json": "users-0!",
			"acme-users-1.json": "users-1!!",
		}
		if len(got) != len(want) {
			t.Fatalf("fetched %v, want %d files", keys(got), len(want))
		}
		for name, body := range want {
			if got[name] != body {
				t.Errorf("%s came back as %q, want %q", name, got[name], body)
			}
			// The declared size is what the resume checkpoint and the
			// completeness check are both built on, so a wrong one is worse
			// than an absent one.
			if sizes[name] != int64(len(body)) {
				t.Errorf("%s declared %d bytes but carried %d", name, sizes[name], len(body))
			}
		}
	})

	t.Run("fetching a directory that is not there says so", func(t *testing.T) {
		e := newExecutor(t)
		defer e.Close()                        //nolint:errcheck
		defer e.Teardown(context.Background()) //nolint:errcheck
		ec := prepare(t, e)

		called := false
		err := e.FetchDir(context.Background(), ec.WorkDir+"/never-created",
			target.SinkFunc(func(context.Context, target.Artifact, io.Reader) error {
				called = true
				return nil
			}))
		if err == nil {
			t.Error("fetching a missing directory reported success")
		}
		if called {
			t.Error("a missing directory still produced artifacts")
		}
	})

	// A sink that fails must abort the fetch. Swallowing the error would leave
	// a capture that looks complete and is missing a user file.
	t.Run("a sink failure aborts the fetch", func(t *testing.T) {
		e := newExecutor(t)
		defer e.Close()                        //nolint:errcheck
		defer e.Teardown(context.Background()) //nolint:errcheck
		ec := prepare(t, e)

		mustRun(t, e, ec, fmt.Sprintf(`cd %s && mkdir -p out && printf a > out/a.json && printf b > out/b.json`,
			shellQuote(ec.WorkDir)))

		boom := errors.New("the sink gave up")
		err := e.FetchDir(context.Background(), ec.WorkDir+"/out",
			target.SinkFunc(func(context.Context, target.Artifact, io.Reader) error { return boom }))
		if err == nil {
			t.Fatal("a failing sink was swallowed, so a short capture would look complete")
		}
	})

	t.Run("pushfile lands the bytes the restore will import", func(t *testing.T) {
		e := newExecutor(t)
		defer e.Close()                        //nolint:errcheck
		defer e.Teardown(context.Background()) //nolint:errcheck
		ec := prepare(t, e)

		body := bytes.Repeat([]byte("realm-json-"), 500)
		remote := ec.WorkDir + "/pushed.json"
		if err := e.PushFile(context.Background(), remote, int64(len(body)), bytes.NewReader(body)); err != nil {
			if errors.Is(err, target.ErrNotImplemented) {
				t.Skip("this target does not implement PushFile yet")
			}
			t.Fatal(err)
		}

		var out []string
		if _, err := e.Run(context.Background(), target.Command{
			Path: "/bin/sh", Args: []string{"-c", "wc -c < " + shellQuote(remote)},
			OnStdout: func(l string) { out = append(out, strings.TrimSpace(l)) },
		}); err != nil {
			t.Fatal(err)
		}
		if len(out) == 0 || out[0] != fmt.Sprint(len(body)) {
			t.Errorf("the pushed file measures %q, want %d bytes", out, len(body))
		}
	})

	// Teardown is called on every terminal path, including paths where Prepare
	// never ran and paths that already tore down. Both have to be silent.
	t.Run("teardown is idempotent and safe with nothing prepared", func(t *testing.T) {
		e := newExecutor(t)
		defer e.Close() //nolint:errcheck

		if err := e.Teardown(context.Background()); err != nil {
			t.Errorf("tearing down before Prepare failed: %v", err)
		}
		ec := prepare(t, e)
		if err := e.Teardown(context.Background()); err != nil {
			t.Fatalf("the first teardown failed: %v", err)
		}
		if err := e.Teardown(context.Background()); err != nil {
			t.Errorf("the second teardown failed: %v", err)
		}
		// Nothing is left holding the export's unmasked secrets.
		if ec.WorkDir != "" {
			assertGone(t, e, ec.WorkDir)
		}
	})

	t.Run("close is safe and repeatable", func(t *testing.T) {
		e := newExecutor(t)
		if err := e.Close(); err != nil {
			t.Fatalf("the first close failed: %v", err)
		}
		if err := e.Close(); err != nil {
			t.Errorf("the second close failed: %v", err)
		}
	})
}

func prepare(t *testing.T, e target.Executor) target.ExecContext {
	t.Helper()
	ec, err := e.Prepare(context.Background(), target.PrepareOptions{
		JobID: "01HCONTRACT", Realms: []string{"acme"}, Purpose: "capture",
	})
	if err != nil {
		t.Fatal(err)
	}
	return ec
}

func mustRun(t *testing.T, e target.Executor, ec target.ExecContext, script string) {
	t.Helper()
	res, err := e.Run(context.Background(), target.Command{
		Path: "/bin/sh", Args: []string{"-c", script}, Dir: ec.WorkDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("setting the fixture up exited %d: %s", res.ExitCode, script)
	}
}

// assertGone checks the work directory did not survive teardown. It runs
// through the executor rather than the local filesystem, because on a clone the
// directory only ever existed inside the clone.
func assertGone(t *testing.T, e target.Executor, dir string) {
	t.Helper()
	res, err := e.Run(context.Background(), target.Command{
		Path: "/bin/sh", Args: []string{"-c", "test -e " + shellQuote(dir)},
	})
	if err != nil {
		// The execution context is gone entirely, which is a stronger form of
		// the same guarantee.
		return
	}
	if res.ExitCode == 0 {
		t.Errorf("%s survived teardown, still holding whatever the export wrote", dir)
	}
}

func contains(lines []string, want string) bool {
	for _, l := range lines {
		if strings.TrimSpace(l) == want {
			return true
		}
	}
	return false
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
