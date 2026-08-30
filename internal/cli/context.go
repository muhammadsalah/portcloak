// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"portcloak/internal/app"
	"portcloak/internal/engine/config"
	"portcloak/internal/engine/obs"
)

// run is one command's world: the engine, the claim on the home folder, the
// streams, and the flags that shaped all three.
type run struct {
	eng   *app.Engine
	g     *globals
	s     Streams
	out   *renderer
	close func()
}

// version is what the engine stamps into a snapshot manifest. It is set once
// from the build main was linked with.
var version = "0.0.0-unknown"

// open builds everything a command needs and returns a teardown.
//
// mode decides the claim: a command that only reads takes a shared one so it can
// run alongside the desktop app, and anything that writes takes the folder to
// itself. Getting this wrong is not a style question — see StartupSweep, which
// rewrites another process's running jobs.
func open(cmd *cobra.Command, g *globals, s Streams, mode config.LockMode) (*run, error) {
	loc, err := config.LocateWith(g.home)
	if err != nil {
		return nil, err
	}
	home := loc.Home
	if g.config != "" {
		// A config file named on the command line is read, never created. A
		// mistyped path that Bootstrap filled in with the empty template would
		// start a PortCloak with no environments in it and look deliberate.
		abs, err := absPath(g.config)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, precondition(fmt.Sprintf(
				"pcloak: --config names %s, and there is no file there.\n  It is read, never created; check the path.", abs))
		}
		home.ConfigPath = abs
	}

	if err := home.Bootstrap(); err != nil {
		return nil, err
	}

	eng, err := app.NewEngineFor(config.Location{Home: home, Source: loc.Source}, version)
	if err != nil {
		return nil, err
	}

	// Housekeeping before the claim, never after: a process holding the folder
	// shared cannot then take it exclusively, because advisory locks conflict
	// between open file descriptions and it would be waiting for itself. It is
	// opportunistic either way — a folder another PortCloak is using does not
	// need sweeping, because what the sweep would tidy is that process's live
	// state.
	eng.SweepIfSolelyHere()

	held, err := acquire(home, mode, commandPath(cmd), g.waitForLock)
	if err != nil {
		_ = eng.Close()
		return nil, err
	}
	eng.Hold(held)

	if g.noKeychain {
		// Nothing may prompt and nothing may be written. An in-memory store is
		// empty, so every credential has to come from a flag or the
		// environment — which is the point: a run that cannot reach the
		// keychain should fail naming the entry, not hang on a dialog.
		eng.Creds = config.NewMemoryCredentials()
	}
	eng.Creds = config.NewFallback(eng.Creds, envCredentials())

	r := &run{eng: eng, g: g, s: s}
	r.out = newRenderer(s, g)
	r.close = func() {
		if err := eng.Close(); err != nil {
			fmt.Fprintln(s.Err, "pcloak: shutting down:", err)
		}
	}

	// The engine's own configuration problems are reported, not fatal: an
	// operator with a malformed file needs to be told which line to fix, and
	// `pcloak config show` is how they would look.
	if lerr := eng.LoadError; lerr != nil && !g.quiet {
		fmt.Fprintln(s.Err, "pcloak: the configuration could not be loaded:", app.Fail(lerr).Message)
	}
	return r, nil
}

// acquire claims the folder, optionally waiting for whoever has it.
//
// Waiting is a poll rather than a blocking flock, because a blocking one cannot
// be given a deadline portably and an operator who typed a duration meant it.
func acquire(home config.Home, mode config.LockMode, command string, wait time.Duration) (*config.Lock, error) {
	who := config.Holder{Program: "pcloak", Command: command}

	deadline := time.Now().Add(wait)
	for {
		l, err := config.Acquire(home, mode, who)
		if err == nil {
			return l, nil
		}
		if wait <= 0 || time.Now().After(deadline) {
			// Both errors stay in the chain: errHomeBusy so the exit code can be
			// chosen without matching on message text, and the engine's own
			// error so its sentence and its advice survive. Flattening it to a
			// string here lost the advice line, and the refusal stopped saying
			// what still works.
			return nil, errors.Join(errHomeBusy, err)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// commandPath is the subcommand chain, for the lock file's description. It is
// what turns "pcloak (pid 5120) has held it" into "pcloak capture (pid 5120)".
func commandPath(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	parts := strings.Fields(cmd.CommandPath())
	if len(parts) > 1 {
		return strings.Join(parts[1:], " ")
	}
	return ""
}

// envCreds answers credential handles out of the process environment.
//
// The handle keychain://portcloak/<kind>/<name> is looked up as
// PORTCLOAK_CREDENTIAL_<KIND>_<NAME>, upper-cased with everything that is not a
// letter or a digit turned into an underscore. It is the CI path: a value
// reaches the process without appearing in argv, where `ps` would show it, and
// nothing is written anywhere.
//
// It is read-only. Set and Delete are refused rather than silently dropped,
// because a caller that thought it had stored a key and had not would find out
// at the next restore.
type envCreds struct{}

func (envCreds) Get(handle string) (string, error) {
	name := envNameFor(handle)
	if name == "" {
		return "", &config.MissingCredentialError{Handle: handle}
	}
	if v := os.Getenv(name); v != "" {
		return v, nil
	}
	return "", &config.MissingCredentialError{Handle: handle}
}

func (envCreds) Set(string, string) error {
	return errors.New("a credential supplied through the environment cannot be changed from here")
}

func (envCreds) Delete(string) error { return nil }

// envNameFor derives the environment variable a handle is read from.
func envNameFor(handle string) string {
	rest, ok := strings.CutPrefix(handle, config.HandleScheme)
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString("PORTCLOAK_CREDENTIAL_")
	for _, r := range strings.ToUpper(rest) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// envCredentials is the environment-backed store, or nil when nothing in the
// environment looks like one — so the ordinary case adds no indirection.
func envCredentials() config.CredentialStore {
	const prefix = "PORTCLOAK_CREDENTIAL_"
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, prefix) {
			return envCreds{}
		}
	}
	return nil
}

// withTimeout applies --timeout, if one was given.
func (r *run) withTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	if r.g.timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, r.g.timeout)
}

// sink installs the terminal renderer as the engine's progress destination and
// returns it.
func (r *run) sink() *termSink {
	ts := newTermSink(r.s, r.g)
	r.eng.AttachSink(obs.SinkFunc(ts.Emit))
	return ts
}

func absPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("no path")
	}
	return filepath.Abs(p)
}
