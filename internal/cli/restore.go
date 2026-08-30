// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"portcloak/internal/app"
	"portcloak/internal/engine/config"
	"portcloak/internal/engine/kc"
)

func newRestoreCmd(s Streams, g *globals) *cobra.Command {
	var k keySource
	var env, strategy, confirmRealm string
	var dryRun, noTxTimeout bool

	c := &cobra.Command{
		Use:   "restore <snapshot>",
		Short: "Import a snapshot into a Keycloak",
		Long: `Fetches a snapshot, proves it intact, previews what the import would change, and
then applies it.

A snapshot that cannot be proven intact is never written to a target. Overwriting
a realm that already exists is destructive and irreversible, so it takes the
realm's own name typed back — --yes does not answer that.

Keycloak's import is not transactional. A dry run is a preview, not a guarantee,
and it says so.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			sink := r.sink()

			view, done, err := openSnapshot(r, args[0], k)
			if err != nil {
				return err
			}
			defer done()

			if view.Degraded {
				return precondition("pcloak: " + view.DegradedNote)
			}

			restore := app.NewRestoreController(r.eng)
			plan := restore.Plan(app.PlanRequest{
				SnapshotID: view.SnapshotID, Environment: env, Strategy: strategy,
			})
			if plan.Failure != nil {
				return exitWith(ExitPrecondition, "pcloak: "+plan.Failure.Message)
			}
			if plan.Blocked {
				return precondition("pcloak: " + plan.BlockedNote)
			}

			if g.json && dryRun {
				return r.out.JSON(plan)
			}
			renderPlan(r, plan, strategy)

			if dryRun {
				r.out.Note("Dry run only; nothing was written.")
				return nil
			}

			if plan.ConfirmationRequired {
				// The realm exists and overwrite replaces it entirely. Typing
				// its name is the pause; a y/N here is one keystroke from an
				// irreversible mistake.
				if confirmRealm != view.Realm &&
					!confirmPhrase(r, view.Realm, "The realm %q already exists on %s and will be replaced entirely.", view.Realm, env) {
					return precondition(fmt.Sprintf(
						"pcloak: overwriting %q on %s was not confirmed.\n"+
							"  Pass --confirm-realm %s to go ahead, or --strategy skip to create only what is missing.",
						view.Realm, env, view.Realm))
				}
			} else if !g.yes && !confirm(r, "Apply this restore to %s?", env) {
				return exitWith(ExitOK, "Nothing was written.")
			}

			res := restore.Apply(app.ApplyRequest{
				SnapshotID:           view.SnapshotID,
				Storage:              view.Storage,
				BundleKey:            view.BundleKey,
				Realm:                view.Realm,
				Environment:          env,
				Strategy:             strategy,
				ConfirmRealm:         view.Realm,
				NoTransactionTimeout: noTxTimeout,
			})
			if res.Failure != nil {
				return exitWith(ExitPrecondition, "pcloak: "+res.Failure.Message)
			}
			sink.Watch(res.JobID, view.Realm)

			ctx, cancel := r.withTimeout(context.Background())
			defer cancel()

			out := newWaiter(r, sink).wait(ctx, []string{res.JobID})
			renderRestoreSummary(r, out)
			return verdict(out)
		},
	}

	k.register(c)
	fl := c.Flags()
	fl.StringVarP(&env, "env", "e", "", "the environment to restore into (required)")
	fl.StringVar(&strategy, "strategy", string(kc.StrategyOverwrite), "overwrite, skip or merge")
	fl.BoolVar(&dryRun, "dry-run", false, "preview the change and write nothing")
	fl.StringVar(&confirmRealm, "confirm-realm", "", "the realm's own name, to confirm an overwrite")
	fl.BoolVar(&noTxTimeout, "no-transaction-timeout", false,
		"let the import's transactions run without a time limit, for a large realm")
	_ = c.MarkFlagRequired("env")
	return c
}

// renderPlan shows what the import would do before it does it.
func renderPlan(r *run, plan app.Plan, strategy string) {
	pre := plan.Preconditions
	r.out.Line("integrity   %s", okOr(pre.IntegrityPassed, "verified", "NOT VERIFIED"))

	// "None" and "not checked" are different answers, and only one means it is
	// safe to restore without provisioning anything first.
	if !pre.Checked {
		r.out.Note("Dependency detection did not run at capture time, so this snapshot cannot say whether the realm needs themes or provider JARs.")
	} else if len(pre.Dependencies) > 0 {
		r.out.Line("")
		r.out.Line("Provision these at the destination first:")
		for _, d := range pre.Dependencies {
			r.out.Line("  %-10s %-24s %s", d.Type, d.Name, d.Consequence)
		}
	}

	dry := plan.DryRun
	r.out.Line("")
	if !dry.Available {
		r.out.Note("No preview: %s", dry.Unavailable)
		return
	}
	r.out.Line("Dry run · strategy %s · realm %s on the destination",
		cell(strategy), existsOr(dry.TargetExists))
	rows := make([][]string, 0, len(dry.Categories))
	for _, cat := range dry.Categories {
		note := cat.Note
		if cat.NoteLevel == "warning" || cat.NoteLevel == "caution" {
			note = "⚠ " + note
		}
		rows = append(rows, []string{"  " + cat.Category,
			itoa(cat.Create), itoa(cat.Overwrite), itoa(cat.LeaveAlone), note})
	}
	r.out.Table([]string{"  CATEGORY", "CREATE", "OVERWRITE", "LEAVE", ""}, rows)
	r.out.Line("")
	r.out.Line("%s", dry.Summary)
	r.out.Note("%s", dry.Caveat)
}

func renderRestoreSummary(r *run, o outcome) {
	for _, j := range o.Jobs {
		if j == nil {
			continue
		}
		r.out.Line("")
		r.out.Line("%s", j.Message)
		if len(j.SkippedPhases) > 0 {
			// Post-restore validation that could not reach the Admin API is
			// the usual one. It ran and abstained, and reporting it as passed
			// would claim the destination was checked when nothing checked it.
			r.out.Note("Skipped: %s — these ran and had nothing to report.", joinNames(j.SkippedPhases))
		}
	}
}

func okOr(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func existsOr(exists bool) string {
	if exists {
		return "exists"
	}
	return "is new"
}
