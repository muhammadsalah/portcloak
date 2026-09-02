// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package cli is PortCloak's terminal front end: it turns the controllers in
// internal/app into commands, and engine progress events into something a
// terminal or a CI log can render.
//
// It is a peer of internal/desktop, not a layer under it. Both sit on the same
// composition root, both call the same controllers, and neither knows about the
// other. That is what stops the two surfaces drifting: the gate refusing an
// unacknowledged unencrypted capture, the confirmation before overwriting a
// live realm, the refusal to restore a snapshot that cannot be proven intact —
// all of them live in internal/app, so satisfying them is the only way through
// from either side.
//
// No business logic lives here either. A command parses flags, calls one
// controller method, renders the result, and picks an exit code.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"portcloak/internal/app"
)

// Exit codes.
//
// Three of these — Partial, Precondition and Busy — exist because a script
// genuinely branches on them. "It failed" is not enough to decide between
// retrying, waiting for a window to close, and stopping to fix a realm name.
const (
	// ExitOK is success.
	ExitOK = 0
	// ExitFailed is a terminal error: there is nothing to retry.
	ExitFailed = 1
	// ExitUsage is a bad flag or argument. Cobra's own convention.
	ExitUsage = 2
	// ExitPartial is a multi-realm run where some realms produced a snapshot
	// and some did not. One snapshot holds exactly one realm, so partial
	// success is genuinely partial: N-1 valid snapshots plus a failed job, with
	// nothing corrupt. Reporting it as plain failure would send an operator
	// looking for damage that is not there.
	ExitPartial = 3
	// ExitPrecondition is a refusal before any work: a probe blocker, an
	// unacknowledged unencrypted capture, an unconfirmed overwrite, a snapshot
	// that could not be proven intact, a key that is not here. Nothing was
	// written to the target.
	ExitPrecondition = 4
	// ExitRetryable is a failure where waiting could help, or a --timeout that
	// expired. The job is left resumable.
	ExitRetryable = 5
	// ExitBusy is another PortCloak holding the home folder. It is its own code
	// so a CI wrapper can retry on exactly this and nothing else.
	ExitBusy = 6
	// ExitCancelled is SIGINT or SIGTERM. 130 is the shell's own convention for
	// a process ended by SIGINT, and scripts already treat it as one.
	ExitCancelled = 130
)

// Streams are the three files a command talks through, injectable so tests can
// read what an operator would have seen without building a binary.
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Main runs the command line and returns the process exit code.
//
// It returns rather than calling os.Exit so that deferred work — releasing the
// home folder, closing a snapshot session, tearing down an ephemeral clone —
// actually runs. An os.Exit buried in a command would skip every one of those,
// and the ones that matter are in somebody else's cluster.
func Main(build app.Build, args []string, s Streams) int {
	root := newRootCmd(build, s)
	root.SetArgs(args)
	root.SetIn(s.In)
	root.SetOut(s.Out)
	root.SetErr(s.Err)

	// Everything cobra does before RunE is parsing and validation: unknown
	// flags, the wrong number of arguments, a missing required flag, two
	// mutually exclusive ones. All of those are usage errors and none of them
	// means the tool tried and failed, so they get their own code.
	//
	// Marked by reaching RunE rather than by inspecting the error, because the
	// alternative is matching cobra's message text — which is not an interface
	// and changes between releases. ValidateFlagGroups is the last thing to run
	// before RunE, so "did not reach it" is exactly "did not get past
	// validation".
	ran := false
	markReached(root, &ran)

	if err := root.Execute(); err != nil {
		if !ran {
			fmt.Fprintln(s.Err, "pcloak:", err)
			return ExitUsage
		}
		return report(s.Err, err)
	}
	return ExitOK
}

// markReached wraps every RunE in the tree so the caller can tell whether
// execution got past cobra's own validation.
func markReached(c *cobra.Command, ran *bool) {
	if inner := c.RunE; inner != nil {
		c.RunE = func(cmd *cobra.Command, args []string) error {
			*ran = true
			return inner(cmd, args)
		}
	}
	for _, sub := range c.Commands() {
		markReached(sub, ran)
	}
}

// report prints a failure the way an operator reads one and returns its code.
//
// Engine errors already carry a sentence written for a person and a redacted
// message, so this adds nothing to them but the advice line and the exit code.
func report(w io.Writer, err error) int {
	var coded *exitError
	if errors.As(err, &coded) {
		if coded.message != "" {
			fmt.Fprintln(w, coded.message)
		}
		return coded.code
	}
	f := app.Fail(err)
	fmt.Fprintln(w, "pcloak:", f.Message)
	if f.Hint != "" {
		// The advice is carried separately from the message so a screen can
		// render the two apart. Here that is a second, indented line — a
		// refusal that does not say what to do next is only half of one.
		fmt.Fprintln(w, " ", f.Hint)
	}
	return codeFor(err, f)
}

// DefaultStreams is the real process's files.
func DefaultStreams() Streams {
	return Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
}

// SetVersion tells the package which build it is, for the version a capture
// stamps into a snapshot manifest.
//
// It is a package variable rather than a field threaded through every command
// because it is genuinely process-global — one binary is one version — and
// threading it would put a parameter on functions that have no other reason to
// know.
func SetVersion(v string) {
	if v != "" {
		version = v
	}
}
