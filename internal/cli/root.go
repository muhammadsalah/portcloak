// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"time"

	"github.com/spf13/cobra"

	"portcloak/internal/app"
)

// globals are the flags every command inherits, resolved once into the context
// each command builds its engine from.
type globals struct {
	home    string
	config  string
	json    bool
	quiet   bool
	verbose int
	noColor bool
	yes     bool
	timeout time.Duration
	// noKeychain keeps the OS keychain out of the run entirely. On a CI runner
	// with no secret service, or a macOS binary whose signature is not the one
	// that wrote the entries, reaching for it produces either a prompt nothing
	// will answer or an error about the wrong thing.
	noKeychain  bool
	waitForLock time.Duration
}

const rootLong = `PortCloak moves a Keycloak realm from one environment to another, and has it
still work when it lands: password hashes, 2FA enrolments, unmasked client
secrets, LDAP bind credentials and the private keys that sign your tokens all
travel with it.

This is the same engine the desktop app drives, reading the same ~/.portcloak.
Environments, storage and keys configured in either are visible to both.

Reading works while the app is open. Anything that writes — a capture, a
restore, job control — needs the folder to itself, and says so if it cannot
have it.`

func newRootCmd(build app.Build, s Streams) *cobra.Command {
	g := &globals{}

	root := &cobra.Command{
		Use:   "pcloak",
		Short: "Capture, inspect and restore Keycloak realms from a terminal",
		Long:  rootLong,
		// A bare `pcloak` should print help, not an error about no subcommand.
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		SilenceUsage: true,
		// Cobra otherwise prints the error itself, and then Main prints it
		// again through report() — which is where the operator-facing sentence
		// and the exit code actually come from.
		SilenceErrors: true,
	}

	f := root.PersistentFlags()
	f.StringVar(&g.home, "home", "", "use this PortCloak folder instead of ~/.portcloak")
	f.StringVar(&g.config, "config", "", "read config.yaml from here, leaving the rest of the folder alone")
	f.BoolVar(&g.json, "json", false, "print machine-readable output")
	f.BoolVarP(&g.quiet, "quiet", "q", false, "print only the result and errors")
	f.CountVarP(&g.verbose, "verbose", "v", "show more; -vv shows engine detail")
	f.BoolVar(&g.noColor, "no-color", false, "never colour the output")
	f.BoolVarP(&g.yes, "yes", "y", false, "answer ordinary confirmations yes")
	f.DurationVar(&g.timeout, "timeout", 0, "give up after this long, tearing down cleanly")
	f.BoolVar(&g.noKeychain, "no-keychain", false, "never read this machine's keychain")
	f.DurationVar(&g.waitForLock, "wait-for-lock", 0, "wait this long for another PortCloak to finish")

	root.AddCommand(
		newVersionCmd(build, s, g),
		newConfigCmd(s, g),
		newEnvCmd(s, g),
		newStorageCmd(s, g),
		newSnapshotCmd(s, g),
		newJobCmd(s, g),
		newKeyCmd(s, g),
		newOrphansCmd(s, g),
		newCaptureCmd(s, g),
		newRestoreCmd(s, g),
	)
	return root
}

// group builds a container command whose only job is to hold subcommands.
//
// Running one bare prints help rather than erroring: `pcloak snapshot` is a
// reasonable thing to type when you have forgotten the verb.
func group(use, short, long string, aliases ...string) *cobra.Command {
	return &cobra.Command{
		Use:     use,
		Short:   short,
		Long:    long,
		Aliases: aliases,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
}
