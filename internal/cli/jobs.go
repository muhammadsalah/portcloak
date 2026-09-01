// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"time"

	"github.com/spf13/cobra"

	"portcloak/internal/app"
	"portcloak/internal/engine/config"
)

func newJobCmd(s Streams, g *globals) *cobra.Command {
	c := group("job", "Look at and control captures and restores",
		`Every capture and every restore is a job with a durable record under
~/.portcloak/jobs. The record outlives the process, which is what lets an
interrupted transfer be resumed after PortCloak has been closed and reopened.

Listing and reading work while the desktop app is running. Cancelling, resuming
and discarding need the folder to themselves.`, "jobs")
	c.AddCommand(newJobListCmd(s, g), newJobShowCmd(s, g), newJobLogsCmd(s, g),
		newJobCancelCmd(s, g), newJobResumeCmd(s, g), newJobDiscardCmd(s, g))
	return c
}

func newJobListCmd(s Streams, g *globals) *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Every job, newest first",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			view := app.NewJobsController(r.eng).List()
			if g.json {
				return r.out.JSON(view)
			}
			rows := make([][]string, 0, len(view.Jobs))
			for _, j := range view.Jobs {
				if !all && j.State == config.JobCompleted && len(rows) >= 20 {
					continue
				}
				rows = append(rows, []string{j.ID, string(j.Kind), cell(j.Realm),
					string(j.State), cell(phaseOf(j)), cell(j.Elapsed)})
			}
			r.out.Table([]string{"JOB", "KIND", "REALM", "STATE", "PHASE", "ELAPSED"}, rows)
			if len(rows) == 0 {
				r.out.Note("Nothing has run yet.")
				return nil
			}
			r.out.Note("%s", view.Summary)
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "include every completed job, not just the recent ones")
	return c
}

// phaseOf is where a job got to. A finished job shows nothing rather than the
// last phase it happened to pass through, which reads as though it stopped there.
func phaseOf(j app.JobView) string {
	if j.State.Terminal() && j.State != config.JobInterrupted {
		return ""
	}
	return j.Phase
}

func newJobShowCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "show <job>",
		Short: "One job: its phases, what it carried, and what resuming would do",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			j, err := findJob(r, args[0])
			if err != nil {
				return err
			}
			if g.json {
				return r.out.JSON(j)
			}
			r.out.Line("job         %s", j.ID)
			r.out.Line("kind        %s", j.Kind)
			r.out.Line("realm       %s", cell(j.Realm))
			r.out.Line("environment %s", cell(j.Environment))
			r.out.Line("storage     %s", cell(j.Storage))
			r.out.Line("state       %s", j.State)
			r.out.Line("elapsed     %s", cell(j.Elapsed))
			if j.Message != "" {
				r.out.Line("message     %s", j.Message)
			}
			r.out.Line("")
			renderPhases(r, j.Phases)
			if j.Resumable {
				r.out.Line("")
				r.out.Note("Resumable: %s", j.ResumeNote)
				if j.NeedsPassphrase {
					r.out.Note("It was sealed with a passphrase, which a job record never holds. Resuming asks for it.")
				}
			}
			if j.CheckpointNote != "" {
				r.out.Note("%s", j.CheckpointNote)
			}
			return nil
		},
	}
}

// renderPhases draws a job's phases from its record.
//
// The record is the authority, not the event stream: a phase can be both done
// and skipped — it reached its turn and abstained — and only the record carries
// that. Drawing a skipped phase with a tick is the bug commit 475e4db fixed in
// the desktop app, and it matters more here, because "verification passed" and
// "verification did not run" are the difference between a snapshot whose secrets
// were checked and one whose were not.
func renderPhases(r *run, phases []app.PhaseView) {
	for _, p := range phases {
		mark := "  ·"
		switch {
		case p.Skipped:
			mark = "  –"
		case p.Live:
			mark = "  ▸"
		case p.Done:
			mark = "  ✓"
		}
		note := ""
		if p.Skipped {
			note = "  (skipped — it ran and had nothing to report)"
		}
		r.out.Line("%s %s%s", mark, p.Label, note)
	}
}

func newJobLogsCmd(s Streams, g *globals) *cobra.Command {
	var follow bool
	c := &cobra.Command{
		Use:     "logs <job>",
		Aliases: []string{"log"},
		Short:   "What a job said",
		Long: `The run's output, including what kc.sh wrote.

Only what this PortCloak process has in memory: the log store is per-process, so
a job run by the desktop app is readable while that app is open and gone once it
closes. The durable record of what happened is the job itself and the audit log.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			jobs := app.NewJobsController(r.eng)
			cursor := 0
			for {
				view := jobs.Log(args[0], cursor)
				for _, l := range view.Lines {
					r.out.Line("%s", l.Text)
				}
				cursor = view.Next
				if !follow {
					if len(view.Lines) == 0 {
						r.out.Note("Nothing recorded for %s in this process.", args[0])
					}
					return nil
				}
				j, err := r.eng.Jobs.Load(args[0])
				if err == nil && j.State.Terminal() {
					return nil
				}
				time.Sleep(500 * time.Millisecond)
			}
		},
	}
	c.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing until the job finishes")
	return c
}

// findJob resolves a job id, accepting an unambiguous prefix.
//
// Job ids are time-ordered and sixteen characters long, and nobody types one in
// full from a listing. An ambiguous prefix names the candidates rather than
// picking one, because picking one silently is how the wrong job gets cancelled.
func findJob(r *run, id string) (app.JobView, error) {
	view := app.NewJobsController(r.eng).List()
	var hits []app.JobView
	for _, j := range view.Jobs {
		if j.ID == id {
			return j, nil
		}
		if len(id) >= 4 && len(j.ID) >= len(id) && j.ID[:len(id)] == id {
			hits = append(hits, j)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return app.JobView{}, notFound("job", id, names(view.Jobs, func(j app.JobView) string { return j.ID }))
	default:
		return app.JobView{}, precondition("pcloak: \"" + id + "\" matches " +
			itoa(len(hits)) + " jobs. Give more of the id:\n  " +
			joinNames(names(hits, func(j app.JobView) string { return j.ID })))
	}
}

func newJobCancelCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <job>",
		Short: "Stop a running job, tearing down what it created",
		Long: `Cancelling is not abandoning. A capture may be holding an ephemeral clone in a
cluster with the production database credentials in it, and cancelling runs the
teardown that destroys it.

This only reaches a job running in this process. A job the desktop app is running
is cancelled there — the engine's cancel works through an in-memory handle, not
through the job file.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			if f := app.NewJobsController(r.eng).Cancel(args[0]); f != nil {
				return exitWith(ExitPrecondition, "pcloak: "+f.Message+
					"\n  Only a job this process is running can be cancelled from here.")
			}
			r.out.Line("Cancelling %s.", args[0])
			return nil
		},
	}
}

func newJobResumeCmd(s Streams, g *globals) *cobra.Command {
	var passphrase, passFile string
	var passStdin bool
	c := &cobra.Command{
		Use:   "resume <job>",
		Short: "Continue an interrupted job",
		Long: `What resuming does depends on how far the job got. If the snapshot was already
sealed on this machine, only the upload is repeated — and the object it produces
is byte-identical to one an uninterrupted run would have written, never a
duplicate and never a concatenation. If the export never finished, it runs again.

A restore is not resumed. Keycloak's import is not transactional, so replaying one
is not always safe; review what was applied and start again deliberately.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			j, err := findJob(r, args[0])
			if err != nil {
				return err
			}
			if !j.Resumable {
				return precondition("pcloak: job " + j.ID + " is " + string(j.State) + ", so there is nothing to resume.")
			}
			r.out.Note("%s", j.ResumeNote)

			pass := ""
			if j.NeedsPassphrase {
				// A job record never holds a passphrase, deliberately, so a
				// resume of an encrypted capture has to be given one again.
				pass, err = (secretSource{
					value: passphrase, file: passFile, stdin: passStdin,
					env: "PORTCLOAK_PASSPHRASE", required: true,
				}).read(r, "Passphrase the snapshot was sealed with", false)
				if err != nil {
					if err == errNotATerminal {
						return missingSecret("the passphrase this snapshot was sealed with",
							"--passphrase-file", "--passphrase-stdin")
					}
					return exitWith(ExitPrecondition, "pcloak: "+err.Error())
				}
			}

			sink := r.sink()
			res := app.NewJobsController(r.eng).Resume(j.ID, pass)
			if res.Failure != nil {
				return exitWith(ExitPrecondition, "pcloak: "+res.Failure.Message)
			}
			for _, id := range res.JobIDs {
				sink.Watch(id, j.Realm)
			}

			ctx, cancel := r.withTimeout(cmd.Context())
			defer cancel()
			out := newWaiter(r, sink).wait(ctx, res.JobIDs)
			for _, done := range out.Jobs {
				if done != nil {
					r.out.Line("%s", done.Message)
				}
			}
			return verdict(out)
		},
	}
	c.Flags().StringVar(&passphrase, "passphrase", "", "the passphrase itself; visible in ps, so prefer the two below")
	c.Flags().StringVar(&passFile, "passphrase-file", "", "read the passphrase from this file (- for stdin)")
	c.Flags().BoolVar(&passStdin, "passphrase-stdin", false, "read the passphrase from stdin")
	c.MarkFlagsMutuallyExclusive("passphrase", "passphrase-file", "passphrase-stdin")
	return c
}

func newJobDiscardCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "discard <job>",
		Short: "Abandon an interrupted job and clean up after it",
		Long: `Removes the job record and the partial work behind it: the sealed bundle staged
on this machine, and any incomplete upload at the destination.

Discarding is job control rather than housekeeping, which is why it is deliberate
and not something a sweep does.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			j, err := findJob(r, args[0])
			if err != nil {
				return err
			}
			if !g.yes && !confirm(r, "Discard job %s (%s, %s)?", j.ID, cell(j.Realm), j.State) {
				return exitWith(ExitOK, "Left alone.")
			}
			res := app.NewJobsController(r.eng).Discard(j.ID)
			if res.Failure != nil {
				return exitWith(ExitFailed, "pcloak: "+res.Failure.Message)
			}
			r.out.Line("%s", res.Note)
			return nil
		},
	}
}
