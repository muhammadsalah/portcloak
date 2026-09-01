// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"portcloak/internal/app"
	"portcloak/internal/engine/config"
)

// Storage, on the same reasoning as environments — see envadd.go. A CI job that
// captures into a throwaway bucket has to be able to name the bucket.

func newStorageAddCmd(s Streams, g *globals) *cobra.Command {
	c := group("add", "Define somewhere for snapshots to live",
		`One subcommand per kind. The secret — an SSH password, an S3 secret key, an Azure
account key — never comes from an argument, where ps would show it to every user
on the machine. Use --credential-file or --credential-stdin.

Nothing is contacted. Run `+"`pcloak storage test <name>`"+` afterwards: it writes a probe
object and removes it again, so the answer is whether this can actually hold a
snapshot rather than whether it merely answers.`)
	c.AddCommand(
		newStorageAddDiskCmd(s, g),
		newStorageAddSSHCmd(s, g),
		newStorageAddS3Cmd(s, g),
		newStorageAddAzureCmd(s, g),
	)
	return c
}

// storageCommon is what every kind carries.
type storageCommon struct {
	prefix      string
	makeDefault bool
	requireEnc  bool
	replace     bool
	cred        credFlags
}

func (sc *storageCommon) register(c *cobra.Command, withPrefix bool) {
	sc.registerAs(c, withPrefix, "the secret")
}

func (sc *storageCommon) registerAs(c *cobra.Command, withPrefix bool, credential string) {
	f := c.Flags()
	if withPrefix {
		f.StringVar(&sc.prefix, "prefix", "", "folder within the bucket or container, so one backend can hold several trees")
	}
	f.BoolVar(&sc.makeDefault, "default", false, "make this the storage a capture uses when none is named")
	// Worth a flag of its own rather than a preference: it removes the opt-out
	// entirely, so a capture aimed here cannot be written in the clear even by
	// somebody who passes --i-understand-unencrypted.
	f.BoolVar(&sc.requireEnc, "encryption-required", false, "refuse to write an unencrypted snapshot here")
	f.BoolVar(&sc.replace, "replace", false, "overwrite a storage of this name if one exists")
	sc.cred.registerAs(c, credential)
}

func (sc *storageCommon) apply(st *config.Storage) {
	st.Prefix = sc.prefix
	st.EncryptionRequired = sc.requireEnc
}

func saveStorage(cmd *cobra.Command, s Streams, g *globals, st config.Storage, sc *storageCommon) error {
	r, err := open(cmd, g, s, config.LockExclusive)
	if err != nil {
		return err
	}
	defer r.close()

	cfg := app.NewConfigController(r.eng)
	_, exists := r.eng.Config.Config().StorageByName(st.Name)
	if exists && !sc.replace {
		return precondition(fmt.Sprintf(
			"pcloak: there is already a storage called %q.\n"+
				"  Pass --replace to overwrite it, which is what a re-run of the same script wants.", st.Name))
	}

	secret, err := sc.cred.read(r)
	if err != nil {
		return exitWith(ExitPrecondition, "pcloak: "+err.Error())
	}

	original := ""
	if exists {
		original = st.Name
	}
	if f := cfg.SaveStorage(original, st, secret); f != nil {
		return exitWith(ExitFailed, "pcloak: "+f.Message+hintLine(f.Hint))
	}
	// After the save, because there has to be something to point at.
	if sc.makeDefault {
		if f := cfg.SetDefaultStorage(st.Name); f != nil {
			return exitWith(ExitFailed, "pcloak: "+f.Message)
		}
	}

	verb := "Added"
	if exists {
		verb = "Replaced"
	}
	r.out.Line("%s the %s storage %q.", verb, st.Kind, st.Name)
	r.out.Note("Nothing was contacted. Check it with: pcloak storage test %s", st.Name)
	return nil
}

func newStorageAddDiskCmd(s Streams, g *globals) *cobra.Command {
	var sc storageCommon
	st := config.Storage{Kind: config.StoreDisk}

	c := &cobra.Command{
		Use:   "disk <name>",
		Short: "A folder on this machine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st.Name = args[0]
			sc.apply(&st)
			return saveStorage(cmd, s, g, st, &sc)
		},
	}
	c.Flags().StringVar(&st.Folder, "folder", "", "the folder to root everything at (required)")
	_ = c.MarkFlagRequired("folder")
	// A folder on this machine needs no credential, but the flag is registered
	// anyway rather than special-cased: an empty one stores nothing, and a
	// command whose flag set changes shape by kind is harder to script than one
	// that ignores what it does not need.
	sc.registerAs(c, false, "an unused secret")
	return c
}

func newStorageAddSSHCmd(s Streams, g *globals) *cobra.Command {
	var sc storageCommon
	var auth string
	st := config.Storage{Kind: config.StoreSSH}

	c := &cobra.Command{
		Use:   "ssh <name>",
		Short: "A folder on a remote host, over SFTP",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st.Name = args[0]
			st.Auth = config.SSHAuth(auth)
			sc.apply(&st)
			return saveStorage(cmd, s, g, st, &sc)
		},
	}
	f := c.Flags()
	f.StringVar(&st.Host, "host", "", "host to connect to (required)")
	f.StringVar(&st.User, "user", "", "user to connect as (required)")
	f.IntVar(&st.Port, "port", 0, "SSH port (default 22)")
	f.StringVar(&auth, "auth", string(config.SSHAgent), "key, agent or password")
	f.StringVar(&st.Folder, "folder", "", "the remote folder to write into (required)")
	_ = c.MarkFlagRequired("host")
	_ = c.MarkFlagRequired("user")
	_ = c.MarkFlagRequired("folder")
	sc.registerAs(c, false, "the SSH password or key passphrase")
	return c
}

func newStorageAddS3Cmd(s Streams, g *globals) *cobra.Command {
	var sc storageCommon
	st := config.Storage{Kind: config.StoreS3}

	c := &cobra.Command{
		Use:     "s3 <name>",
		Aliases: []string{"minio"},
		Short:   "An S3-compatible bucket",
		Long: `Works against AWS S3 and against anything that speaks its API — MinIO, Ceph,
Backblaze. For those, set --endpoint and usually --path-style.

The credential is both halves of the key as one value, ` + "`<access-key-id>:<secret>`" + `,
because that is how the store reads it back. Leave it out entirely to fall back to
the ambient AWS credential chain — an instance role, or the environment — which is
often what a CI runner already has.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st.Name = args[0]
			sc.apply(&st)
			return saveStorage(cmd, s, g, st, &sc)
		},
	}
	f := c.Flags()
	f.StringVar(&st.Bucket, "bucket", "", "the bucket (required)")
	f.StringVar(&st.Endpoint, "endpoint", "", "endpoint URL, for anything that is not AWS")
	f.StringVar(&st.Region, "region", "", "region; a region, an endpoint, or both is required")
	f.BoolVar(&st.PathStyle, "path-style", false, "address the bucket in the path rather than the hostname, which MinIO wants")
	f.StringVar(&st.StorageClass, "storage-class", "", "S3 storage class to write with")
	f.StringVar(&st.ServerSideEnc, "server-side-encryption", "", "server-side encryption to ask for")
	f.IntVar(&st.PartSizeMB, "part-size-mb", 0, "multipart part size")
	_ = c.MarkFlagRequired("bucket")
	sc.registerAs(c, true, "the access key as <id>:<secret>")
	return c
}

func newStorageAddAzureCmd(s Streams, g *globals) *cobra.Command {
	var sc storageCommon
	st := config.Storage{Kind: config.StoreAzure}

	c := &cobra.Command{
		Use:     "azure <name>",
		Aliases: []string{"azurite"},
		Short:   "An Azure Blob container",
		Long: `Point --endpoint at Azurite's dev endpoint to use the emulator.

The credential is the account key, or a SAS token beginning with "?" — the store
tells the two apart by that leading character.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st.Name = args[0]
			sc.apply(&st)
			return saveStorage(cmd, s, g, st, &sc)
		},
	}
	f := c.Flags()
	f.StringVar(&st.Container, "container", "", "the blob container (required)")
	f.StringVar(&st.Account, "account", "", "storage account name; an account or an endpoint is required")
	f.StringVar(&st.Endpoint, "endpoint", "", "endpoint URL, for Azurite or a custom domain")
	f.StringVar(&st.AccessTier, "access-tier", "", "access tier to write with")
	f.IntVar(&st.BlockSizeMB, "block-size-mb", 0, "block size for a block blob upload")
	_ = c.MarkFlagRequired("container")
	sc.registerAs(c, true, "the account key, or a SAS token")
	return c
}

func newStorageRemoveCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm", "delete"},
		Short:   "Forget a storage definition, and the credentials behind it",
		Long: `Removes the definition and deletes its keychain entry.

**The snapshots stay where they are.** This forgets how to reach a storage; it
does not empty it. Deleting the snapshots is ` + "`pcloak snapshot delete`" + `, one at a
time and deliberately, because a storage definition is cheap to recreate and a
snapshot is not.

A storage a job is currently writing to is refused rather than removed out from
under it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockExclusive)
			if err != nil {
				return err
			}
			defer r.close()

			sts := app.NewConfigController(r.eng).Load().Storage
			found := false
			for _, st := range sts {
				if st.Name == args[0] {
					found = true
					break
				}
			}
			if !found {
				return notFound("storage", args[0],
					names(sts, func(v app.StorageView) string { return v.Name }))
			}
			if !g.yes && !confirm(r, "Forget the storage %q? The snapshots in it stay where they are.", args[0]) {
				return exitWith(ExitOK, "Left alone.")
			}
			if f := app.NewConfigController(r.eng).DeleteStorage(args[0]); f != nil {
				return exitWith(ExitPrecondition, "pcloak: "+f.Message+hintLine(f.Hint))
			}
			r.out.Line("Forgot the storage %q. Nothing in it was deleted.", args[0])
			return nil
		},
	}
}

func newStorageDefaultCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "default <name>",
		Short: "Make this the storage a capture uses when none is named",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockExclusive)
			if err != nil {
				return err
			}
			defer r.close()

			if f := app.NewConfigController(r.eng).SetDefaultStorage(args[0]); f != nil {
				sts := app.NewConfigController(r.eng).Load().Storage
				return notFound("storage", args[0],
					names(sts, func(v app.StorageView) string { return v.Name }))
			}
			r.out.Line("%q is now the default storage.", args[0])
			return nil
		},
	}
}
