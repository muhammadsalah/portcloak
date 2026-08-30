// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"github.com/spf13/cobra"

	"portcloak/internal/app"
	"portcloak/internal/engine/config"
)

func newVersionCmd(build app.Build, s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Aliases: []string{"about"},
		Short:   "Print the build this binary was cut from",
		Long: `Version, commit and build date.

A snapshot records the version that wrote it, so when a bundle will not restore
the first question is which build produced it — and this is the answer.`,
		Args: cobra.NoArgs,
		// Deliberately no engine and no lock: knowing what you are running must
		// work when the folder is busy, unreadable, or somewhere else entirely.
		RunE: func(cmd *cobra.Command, args []string) error {
			r := newRenderer(s, g)
			if g.json {
				return r.JSON(build)
			}
			r.Line("pcloak %s", build.Version)
			r.Line("  commit    %s", build.Commit)
			r.Line("  built     %s", build.DisplayDate())
			r.Line("  go        %s", build.Go)
			r.Line("  platform  %s", build.Platform)
			return nil
		},
	}
}

func newConfigCmd(s Streams, g *globals) *cobra.Command {
	c := group("config", "Show where PortCloak keeps its files, and what is in them",
		`Read-only. pcloak never defines an environment, a storage or a preference:
those are forms with a dozen fields and a connection test behind them, and they
belong in the app. Keys are the one exception — see `+"`pcloak key`"+`.`)
	c.AddCommand(newConfigPathCmd(s, g), newConfigShowCmd(s, g))
	return c
}

func newConfigPathCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Where the configuration, jobs, logs and indexes live",
		Long: `Prints the PortCloak folder and how it was decided.

The "how" matters as much as the "where": a folder fixed by PORTCLOAK_HOME or by
--home cannot be moved from the app, because there would be nowhere to record a
different choice that the outside setting would not immediately override.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Shared: this reads, and an operator asking where the files are is
			// exactly the person who might be looking because the app has them.
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			view := app.NewSettingsController(r.eng).Location()
			if g.json {
				return r.out.JSON(view)
			}
			r.out.Line("folder      %s", view.Root)
			r.out.Line("config      %s", view.ConfigFile)
			r.out.Line("source      %s", view.Source)
			r.out.Line("default     %s", view.Default)
			r.out.Line("pointer     %s", view.Pointer)
			r.out.Note("%s", view.SourceNote)
			if view.Blocked != "" {
				r.out.Note("%s", view.Blocked)
			}
			return nil
		},
	}
}

func newConfigShowCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the resolved configuration",
		Long: `Environments, storage definitions, keys and preferences as PortCloak read them.

No secret is printed, because none is stored: config.yaml holds only keychain
handles, and this prints whether the secret behind each handle is on this
machine — which is what turns "copied the config across" from an obscure
connection failure into an obvious missing credential.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			view := app.NewConfigController(r.eng).Load()
			if g.json {
				return r.out.JSON(view)
			}

			r.out.Line("Environments")
			rows := make([][]string, 0, len(view.Environments))
			for _, e := range view.Environments {
				rows = append(rows, []string{"  " + e.Name, string(e.Kind), cell(e.Target), readiness(e.Readiness)})
			}
			r.out.Table([]string{"  NAME", "KIND", "TARGET", "STATE"}, rows)
			if len(rows) == 0 {
				r.out.Line("  none configured")
			}

			r.out.Line("")
			r.out.Line("Storage")
			rows = rows[:0]
			for _, st := range view.Storage {
				name := "  " + st.Name
				if st.Default {
					name += " (default)"
				}
				rows = append(rows, []string{name, string(st.Kind), cell(st.Root), readiness(st.Readiness)})
			}
			r.out.Table([]string{"  NAME", "KIND", "ROOT", "STATE"}, rows)
			if len(rows) == 0 {
				r.out.Line("  none configured")
			}
			return nil
		},
	}
}
