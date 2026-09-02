// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"github.com/spf13/cobra"

	"portcloak/internal/app"
	"portcloak/internal/engine/config"
)

func newOrphansCmd(s Streams, g *globals) *cobra.Command {
	c := group("orphans", "Find ephemeral clones a crash left running",
		`A Docker or Kubernetes capture runs inside a throwaway copy of the workload, so
the serving instance is never touched. The clone is destroyed on every path —
success, failure, cancellation — but a process that is killed outright cannot
destroy anything.

What is left behind is a container or a Job holding the production database
credentials. This finds them, across every configured environment.

Removal is never automatic. Somebody else's cluster is not ours to
garbage-collect without asking.`, "clones")
	c.AddCommand(newOrphansListCmd(s, g), newOrphansRemoveCmd(s, g))
	return c
}

func newOrphansListCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls", "sweep"},
		Short:   "What a previous run left behind",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			rep := app.NewSettingsController(r.eng).Orphans()
			if g.json {
				return r.out.JSON(rep)
			}
			rows := make([][]string, 0, len(rep.Orphans))
			for _, o := range rep.Orphans {
				rows = append(rows, []string{o.Environment, o.Ref, cell(o.Age), cell(o.Description)})
			}
			r.out.Table([]string{"ENVIRONMENT", "CLONE", "AGE", "WHAT"}, rows)

			// An environment that could not be reached is reported as
			// unchecked, never folded into "nothing found". "I looked and there
			// is nothing" and "I could not look" are different answers, and only
			// one of them means it is safe to stop worrying.
			for _, u := range rep.Unchecked {
				r.out.Note("not checked: %s — %s", u.Environment, u.Reason)
			}
			if len(rows) == 0 && len(rep.Unchecked) == 0 {
				r.out.Note("Nothing left behind.")
			}
			return nil
		},
	}
}

func newOrphansRemoveCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <environment> <clone>",
		Short: "Destroy one orphaned clone",
		Long: `Removes a container or Job a crashed run left behind.

A clone belonging to a job this PortCloak is still running is refused: it is not
wreckage, it is the execution context an export is streaming through, and
removing it would kill the capture mid-realm.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			if !g.yes && !confirm(r, "Remove clone %s from %s?", args[1], args[0]) {
				return exitWith(ExitOK, "Left alone.")
			}
			if f := app.NewSettingsController(r.eng).RemoveOrphan(args[0], args[1]); f != nil {
				return exitWith(ExitFailed, "pcloak: "+f.Message)
			}
			r.out.Line("Removed %s from %s.", args[1], args[0])
			return nil
		},
	}
}
