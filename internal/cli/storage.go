// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"github.com/spf13/cobra"

	"portcloak/internal/app"
	"portcloak/internal/engine/config"
)

func newStorageCmd(s Streams, g *globals) *cobra.Command {
	c := group("storage", "Look at where snapshots are kept",
		`A storage is where snapshots go: a folder on disk, a volume over SSH, an
S3-compatible bucket, or an Azure container.

Read-only here, apart from the round-trip probe, which writes a small object and
removes it again.`, "storages")
	c.AddCommand(newStorageListCmd(s, g), newStorageShowCmd(s, g),
		newStorageTestCmd(s, g), newStorageBrowseCmd(s, g))
	return c
}

func newStorageListCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Every configured storage",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			sts := app.NewConfigController(r.eng).Load().Storage
			if g.json {
				return r.out.JSON(sts)
			}
			if len(sts) == 0 {
				r.out.Note("No storage is configured. Add one in the PortCloak app.")
				return nil
			}
			rows := make([][]string, 0, len(sts))
			for _, st := range sts {
				flags := ""
				if st.Default {
					flags = "default"
				}
				if st.EncryptionRequired {
					// Worth a column of its own: it removes the opt-out, so a
					// capture aimed here cannot be written in the clear.
					if flags != "" {
						flags += ", "
					}
					flags += "encryption required"
				}
				rows = append(rows, []string{st.Name, string(st.Kind), cell(st.Root), readiness(st.Readiness), cell(flags)})
			}
			r.out.Table([]string{"NAME", "KIND", "ROOT", "STATE", "NOTES"}, rows)
			return nil
		},
	}
}

func newStorageShowCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "One storage in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			sts := app.NewConfigController(r.eng).Load().Storage
			for _, st := range sts {
				if st.Name != args[0] {
					continue
				}
				if g.json {
					return r.out.JSON(st)
				}
				r.out.Line("name        %s", st.Name)
				r.out.Line("kind        %s", st.Kind)
				r.out.Line("root        %s", cell(st.Root))
				r.out.Line("state       %s", readiness(st.Readiness))
				r.out.Line("default     %s", yesNo(st.Default))
				r.out.Line("encryption  %s", requiredOrOptional(st.EncryptionRequired))
				r.out.Line("credential  %s", presence(st.CredentialPresent, st.CredentialRef))
				return nil
			}
			return notFound("storage", args[0], names(sts, func(v app.StorageView) string { return v.Name }))
		},
	}
}

func newStorageTestCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:     "test <name>",
		Aliases: []string{"probe"},
		Short:   "Round-trip a storage: list, write, verify, remove",
		Long: `Writes a small probe object and removes it again, so the answer is whether this
storage can actually hold a snapshot rather than whether it merely answers.

The probe object is cleaned up even when a step fails.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Exclusive for the same reason as env probe: the round trip writes
			// a probe object to the storage and removes it again, and then
			// records what it found on the definition — a read-modify-write of
			// config.yaml that a concurrent one would silently overwrite.
			r, err := open(cmd, g, s, config.LockExclusive)
			if err != nil {
				return err
			}
			defer r.close()

			res := app.NewConfigController(r.eng).TestStorage(args[0])
			if g.json {
				if err := r.out.JSON(res); err != nil {
					return err
				}
				return storageVerdict(res)
			}
			r.out.Line("root        %s", cell(res.Reach.Root))
			r.out.Line("access      %s", cell(string(res.Reach.Access)))
			r.out.Line("integrity   %s", cell(string(res.Reach.Integrity)))
			r.out.Line("resumable   %s", yesNo(res.Reach.Resumable))
			if res.Reach.FailedStep != "" {
				r.out.Line("failed at   %s", res.Reach.FailedStep)
			}
			r.out.Line("")
			r.out.Line("%s", res.Note)
			if res.Failure != nil {
				r.out.Note("%s", res.Failure.Message)
			}
			return storageVerdict(res)
		},
	}
}

func storageVerdict(res app.StorageProbeResult) error {
	switch {
	case res.OK:
		return nil
	case res.Failure != nil && res.Failure.Retryable:
		return exitWith(ExitRetryable, "")
	default:
		return exitWith(ExitPrecondition, "")
	}
}

func newStorageBrowseCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "browse <name>",
		Short: "What a storage actually holds",
		Long: `Every snapshot PortCloak wrote here, and every object it did not.

Foreign objects are shown rather than filtered out. A prefix typo puts snapshots
somewhere nobody looks, and silently hiding what does not match the layout is how
that goes unnoticed for a month.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			res := app.NewSnapshotController(r.eng).Browse(args[0])
			if g.json {
				return r.out.JSON(res)
			}
			if res.Failure != nil {
				return failure(ExitFailed, config.ErrNotFound)
			}
			rows := make([][]string, 0, len(res.Snapshots))
			for _, e := range res.Snapshots {
				rows = append(rows, []string{e.SnapshotID, e.Realm,
					e.CreatedAt.Local().Format("2006-01-02 15:04"),
					humanBytes(e.Bytes), sealedAs(e)})
			}
			r.out.Table([]string{"SNAPSHOT", "REALM", "CREATED", "SIZE", "SEALED"}, rows)
			if len(res.Foreign) > 0 {
				r.out.Line("")
				r.out.Line("Objects PortCloak did not write (%d):", len(res.Foreign))
				for _, o := range res.Foreign {
					r.out.Line("  %s", o.Key)
				}
			}
			r.out.Note("%s", res.Note)
			return nil
		},
	}
}

func requiredOrOptional(required bool) string {
	if required {
		return "required — a snapshot cannot be written here unencrypted"
	}
	return "optional"
}
