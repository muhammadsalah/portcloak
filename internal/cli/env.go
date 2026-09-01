// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"github.com/spf13/cobra"

	"portcloak/internal/app"
	"portcloak/internal/engine/config"
	"portcloak/internal/engine/target"
)

func newEnvCmd(s Streams, g *globals) *cobra.Command {
	c := group("env", "Look at the environments a realm can be captured from",
		`An environment is where a Keycloak runs: a folder on this machine, a host over
SSH, a Docker service, or a namespace and workload in Kubernetes.

Defining one writes to config.yaml, so it needs the folder to itself for as long
as the write takes — the app being open will refuse it. Nothing here contacts
anything; `+"`env probe`"+` is what finds out whether a definition works.`, "envs", "environment", "environments")
	c.AddCommand(newEnvListCmd(s, g), newEnvShowCmd(s, g), newEnvProbeCmd(s, g),
		newEnvAddCmd(s, g), newEnvRemoveCmd(s, g))
	return c
}

func newEnvListCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Every configured environment, with what is known about it",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			envs := app.NewConfigController(r.eng).Load().Environments
			if g.json {
				return r.out.JSON(envs)
			}
			if len(envs) == 0 {
				r.out.Note("No environments are configured. Add one with: pcloak env add --help")
				return nil
			}
			rows := make([][]string, 0, len(envs))
			for _, e := range envs {
				probe := cell(e.ProbeAge)
				if e.Stale {
					// A probe old enough that believing it would be worse than
					// having no information at all.
					probe += " (stale)"
				}
				rows = append(rows, []string{e.Name, string(e.Kind), cell(e.Target), readiness(e.Readiness), probe})
			}
			r.out.Table([]string{"NAME", "KIND", "TARGET", "STATE", "PROBED"}, rows)
			return nil
		},
	}
}

func newEnvShowCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "One environment in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			envs := app.NewConfigController(r.eng).Load().Environments
			for _, e := range envs {
				if e.Name != args[0] {
					continue
				}
				if g.json {
					return r.out.JSON(e)
				}
				r.out.Line("name        %s", e.Name)
				r.out.Line("kind        %s", e.Kind)
				r.out.Line("target      %s", cell(e.Target))
				r.out.Line("state       %s", readiness(e.Readiness))
				r.out.Line("probed      %s", cell(e.ProbeAge))
				r.out.Line("admin api   %s", cell(e.AdminBaseURL))
				r.out.Line("credential  %s", presence(e.CredentialPresent, e.CredentialRef))
				return nil
			}
			return notFound("environment", args[0], names(envs, func(e app.EnvironmentView) string { return e.Name }))
		},
	}
}

func newEnvProbeCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:     "probe <name>",
		Aliases: []string{"test"},
		Short:   "Check an environment can actually be captured from",
		Long: `Runs the same checks a capture runs before it does any work: reachability, the
Keycloak version, where kc.sh lives, free space in the temp directory, whether an
ephemeral clone can be created here, and whether the Admin API answers.

Nothing on the target is changed. A capture that would fail fails here instead,
naming the check and what was found.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Exclusive, because a probe does not only read: it records what it
			// found on the environment, which is a read-modify-write of
			// config.yaml. Two of those and the second silently drops the
			// first — including the window's, if it is open and testing too.
			r, err := open(cmd, g, s, config.LockExclusive)
			if err != nil {
				return err
			}
			defer r.close()

			res := app.NewConfigController(r.eng).TestEnvironment(args[0])
			if g.json {
				if err := r.out.JSON(res); err != nil {
					return err
				}
				return probeVerdict(res)
			}
			renderProbe(r, res)
			return probeVerdict(res)
		},
	}
}

// renderProbe prints what a probe found, check by check.
//
// Every check is shown, not only the failures. A probe is what an operator runs
// when they are unsure whether PortCloak can work somewhere at all, and the
// passing lines are half the answer — "Keycloak 26.0, clone permitted" is the
// thing they were actually asking.
func renderProbe(r *run, res app.ProbeResult) {
	f := res.Facts
	for _, c := range f.Checks {
		mark := "  ·"
		switch c.Status {
		case target.CheckPass:
			mark = "  ✓"
		case target.CheckWarn:
			mark = "  !"
		case target.CheckFail:
			mark = "  ✗"
		case target.CheckSkipped:
			// Not a pass. A check that could not run says nothing about what it
			// would have found, and drawing it as a tick claims otherwise.
			mark = "  –"
		}
		r.out.Line("%s %-24s %s", mark, c.Name, c.Value)
		if c.Advice != "" && c.Status == target.CheckFail {
			r.out.Line("      %s", c.Advice)
		}
	}
	r.out.Line("")
	r.out.Line("%s", f.Summary())
	if f.ReadOnlyNote != "" {
		r.out.Note("%s", f.ReadOnlyNote)
	}
	if res.Failure != nil {
		r.out.Note("%s", res.Failure.Message)
	}
}

// probeVerdict turns a probe into an exit code.
//
// A blocked probe is a precondition, not a failure: nothing was attempted and
// nothing was changed, and the distinction is what lets a script tell "fix the
// environment" apart from "try again later".
func probeVerdict(res app.ProbeResult) error {
	switch {
	case res.OK:
		return nil
	case res.Failure != nil && res.Failure.Retryable:
		return exitWith(ExitRetryable, "")
	default:
		return exitWith(ExitPrecondition, "")
	}
}
