// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"portcloak/internal/app"
	"portcloak/internal/engine/config"
)

// Key management is the one thing on this surface that writes configuration.
//
// It is here because --key and --recipient are how a secret stays off the
// command line, and they are dead flags on a machine that cannot make a key. A
// CI runner or a headless server that has to seal snapshots has no window to
// generate one in, and telling an operator to go and click on a laptop first
// defeats the point of the binary.
//
// Environments and storage followed, for the same reason arrived at the long way
// round — see the note at the top of envadd.go. Preferences have not, because
// nothing is blocked on them: every preference is a default that a flag on the
// command already overrides.

func newKeyCmd(s Streams, g *globals) *cobra.Command {
	c := group("key", "Create and manage the keys snapshots are sealed with",
		`A key is either an age keypair or a remembered passphrase. Either way the secret
half lives in this machine's keychain and config.yaml holds only a handle to it.

Naming a key is how a capture seals without a secret ever appearing in argv, and
how a snapshot opens later without being asked for anything.`, "keys")
	c.AddCommand(
		newKeyListCmd(s, g), newKeyGenerateCmd(s, g), newKeyImportCmd(s, g),
		newKeyRememberCmd(s, g), newKeyPublicCmd(s, g), newKeyRevealCmd(s, g),
		newKeyRenameCmd(s, g), newKeyDeleteCmd(s, g),
	)
	return c
}

func newKeyListCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Every key, and whether its secret is on this machine",
		Long: `Configuration is portable between machines; the secrets deliberately are not. A
key whose entry survived a copy but whose secret did not is shown as what it is,
rather than offered as usable.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			view := app.NewKeysController(r.eng).List()
			if g.json {
				return r.out.JSON(view)
			}
			rows := make([][]string, 0, len(view.Keys))
			for _, k := range view.Keys {
				rows = append(rows, []string{k.Name, string(k.Kind), cell(k.Age),
					yesNo(k.Usable), cell(k.PublicKey)})
			}
			r.out.Table([]string{"NAME", "KIND", "AGE", "USABLE HERE", "RECIPIENT"}, rows)
			if len(rows) == 0 {
				r.out.Note("%s", view.Note)
				return nil
			}
			r.out.Note("%s", view.Note)
			return nil
		},
	}
}

// requireKeychain refuses the key-writing commands when the keychain is out of
// reach.
//
// KeysController.store writes the secret first and the config entry second,
// precisely because "a config entry pointing at a handle with nothing behind it
// is the failure mode that looks like a working key until the day it is needed".
// With --no-keychain the store is in memory, so the secret would go nowhere and
// config.yaml would be left naming a dead handle — a key that lists as present,
// seals a snapshot, and cannot open it.
func requireKeychain(g *globals) error {
	if !g.noKeychain {
		return nil
	}
	return precondition(
		"pcloak: --no-keychain is set, so there is nowhere to keep the secret half of a key.\n" +
			"  Writing the entry anyway would leave config.yaml naming a handle with nothing behind it,\n" +
			"  which looks like a working key until the day it is needed. Run without --no-keychain.")
}

func newKeyGenerateCmd(s Streams, g *globals) *cobra.Command {
	var note, privateOut string
	c := &cobra.Command{
		Use:   "generate <name>",
		Short: "Create an age keypair and store it",
		Long: `Creates a keypair, stores the private half in this machine's keychain, and
records the public half in config.yaml.

The private half is shown once. PortCloak has already stored it, so you will not
be asked for it again here — but that is not a backup: a lost machine is a lost
key, and every snapshot sealed with it goes with it.

--private-key-file writes it to a new 0600 file instead of the terminal, which is
what to use in a pipeline: a private key in CI log output is a private key in
whatever archives that log.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireKeychain(g); err != nil {
				return err
			}
			r, err := open(cmd, g, s, config.LockExclusive)
			if err != nil {
				return err
			}
			defer r.close()

			res := app.NewKeysController(r.eng).Generate(args[0], note)
			if res.Failure != nil {
				return exitWith(ExitFailed, "pcloak: "+res.Failure.Message)
			}

			if privateOut != "" {
				// O_EXCL: never overwrite. A key file silently replaced is
				// every snapshot sealed with the old one made unreadable.
				f, err := os.OpenFile(privateOut, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
				if err != nil {
					return exitWith(ExitFailed, fmt.Sprintf(
						"pcloak: the key was created and stored, but %s could not be written: %v\n"+
							"  Read it back with: pcloak key reveal %s", privateOut, err, args[0]))
				}
				if _, err := fmt.Fprintln(f, res.PrivateKey); err != nil {
					_ = f.Close()
					return exitWith(ExitFailed, "pcloak: writing "+privateOut+": "+err.Error())
				}
				if err := f.Close(); err != nil {
					return exitWith(ExitFailed, "pcloak: writing "+privateOut+": "+err.Error())
				}
			}

			if g.json {
				if privateOut != "" {
					// Not into a JSON document that a pipeline may log.
					res.PrivateKey = ""
				}
				return r.out.JSON(res)
			}
			r.out.Line("Created the age keypair %q.", res.Name)
			r.out.Line("  public   %s", res.PublicKey)
			if privateOut != "" {
				r.out.Line("  private  written to %s", privateOut)
			} else {
				r.out.Line("  private  %s   (shown once)", res.PrivateKey)
			}
			r.out.Note("")
			r.out.Note("%s", res.Warning)
			return nil
		},
	}
	c.Flags().StringVar(&note, "note", "", "what this key is for")
	c.Flags().StringVar(&privateOut, "private-key-file", "", "write the private half here instead of printing it")
	return c
}

func newKeyImportCmd(s Streams, g *globals) *cobra.Command {
	var note, keyValue, keyFile string
	var keyStdin bool
	c := &cobra.Command{
		Use:   "import <name>",
		Short: "Record an age keypair you already hold",
		Long: `Only the private half is asked for. The public half is derived from it, so there
is no way to store a pair whose two halves do not match.

The key is read from a file or from stdin, never from an argument: anything on
the command line is visible in ` + "`ps`" + ` to every user on the machine.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireKeychain(g); err != nil {
				return err
			}
			if keyValue == "" && keyFile == "" && !keyStdin && !isTerminal(s.Err) {
				return precondition(
					"pcloak: the private half has to come from somewhere.\n" +
						"  Use --private-key-file <path>, or --private-key-stdin.")
			}
			r, err := open(cmd, g, s, config.LockExclusive)
			if err != nil {
				return err
			}
			defer r.close()

			secret, err := (secretSource{value: keyValue, file: keyFile, stdin: keyStdin, required: true}).read(r, "Private key", false)
			if err != nil {
				return exitWith(ExitPrecondition, "pcloak: "+err.Error())
			}
			if f := app.NewKeysController(r.eng).ImportIdentity(args[0], secret, note); f != nil {
				return exitWith(ExitFailed, "pcloak: "+f.Message)
			}
			r.out.Line("Imported the age keypair %q.", args[0])
			return nil
		},
	}
	c.Flags().StringVar(&note, "note", "", "what this key is for")
	c.Flags().StringVar(&keyValue, "private-key", "", "the private half itself; visible in ps, so prefer the two below")
	c.Flags().StringVar(&keyFile, "private-key-file", "", "read the private half from this file (- for stdin)")
	c.Flags().BoolVar(&keyStdin, "private-key-stdin", false, "read the private half from stdin")
	c.MarkFlagsMutuallyExclusive("private-key", "private-key-file", "private-key-stdin")
	return c
}

func newKeyRememberCmd(s Streams, g *globals) *cobra.Command {
	var note, passphrase, passFile string
	var passStdin bool
	c := &cobra.Command{
		Use:   "remember <name>",
		Short: "Store a passphrase under a name",
		Long: `Keeps a passphrase in this machine's keychain so captures can seal with it and
snapshots open without being asked.

On a terminal it is typed twice and compared. A passphrase mistyped while sealing
cannot be recovered from anywhere, and neither can the snapshot.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireKeychain(g); err != nil {
				return err
			}
			r, err := open(cmd, g, s, config.LockExclusive)
			if err != nil {
				return err
			}
			defer r.close()

			pass, err := (secretSource{
				value: passphrase, file: passFile, stdin: passStdin,
				env: "PORTCLOAK_PASSPHRASE", required: true,
			}).read(r, "Passphrase", true)
			if err != nil {
				return exitWith(ExitPrecondition, "pcloak: "+err.Error())
			}
			if f := app.NewKeysController(r.eng).SavePassphrase(args[0], pass, note); f != nil {
				return exitWith(ExitFailed, "pcloak: "+f.Message)
			}
			r.out.Line("Remembered the passphrase %q.", args[0])
			return nil
		},
	}
	c.Flags().StringVar(&note, "note", "", "what this key is for")
	c.Flags().StringVar(&passphrase, "passphrase", "", "the passphrase itself; visible in ps, so prefer the two below")
	c.Flags().StringVar(&passFile, "passphrase-file", "", "read it from this file (- for stdin)")
	c.Flags().BoolVar(&passStdin, "passphrase-stdin", false, "read it from stdin")
	c.MarkFlagsMutuallyExclusive("passphrase", "passphrase-file", "passphrase-stdin")
	return c
}

func newKeyPublicCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "public <name>",
		Short: "Print a key's age recipient",
		Long: `One line, no decoration, for pasting into another machine's --recipient or into
a script.

A public key is not a secret, so unlike reveal this is not audited.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			for _, rec := range app.NewKeysController(r.eng).Recipients() {
				if rec.Name == args[0] {
					r.out.Line("%s", rec.PublicKey)
					return nil
				}
			}
			all := app.NewKeysController(r.eng).List()
			return notFound("age key", args[0],
				names(all.Keys, func(k app.KeyView) string { return k.Name }))
		},
	}
}

func newKeyRevealCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "reveal <name>",
		Short: "Print the secret half of a key, once",
		Long: `For taking a backup, or handing the key to somebody who also needs to open these
snapshots.

Recorded in the audit log, like every other reveal in PortCloak.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			res := app.NewKeysController(r.eng).Reveal(args[0])
			if res.Failure != nil {
				return exitWith(ExitPrecondition, "pcloak: "+res.Failure.Message)
			}
			r.out.Line("%s", res.Secret)
			r.out.Note("%s", res.Warning)
			return nil
		},
	}
}

func newKeyRenameCmd(s Streams, g *globals) *cobra.Command {
	var note string
	c := &cobra.Command{
		Use:   "rename <name> <new-name>",
		Short: "Rename a key, or change its note",
		Long:  `The key material is untouched: snapshots sealed with it still open.`,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockExclusive)
			if err != nil {
				return err
			}
			defer r.close()

			if f := app.NewKeysController(r.eng).Rename(args[0], args[1], note); f != nil {
				return exitWith(ExitFailed, "pcloak: "+f.Message)
			}
			r.out.Line("Renamed %q to %q.", args[0], args[1])
			return nil
		},
	}
	c.Flags().StringVar(&note, "note", "", "replace the note")
	return c
}

func newKeyDeleteCmd(s Streams, g *globals) *cobra.Command {
	var confirmName string
	c := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm"},
		Short:   "Remove a key and the secret behind it",
		Long: `Every snapshot sealed with this key becomes permanently unreadable unless a copy
of the key exists somewhere else. PortCloak cannot check where those snapshots
are, cannot warn you which ones they were, and cannot undo this.

That is the same class of irreversibility as overwriting a live realm, so it takes
the same shape of confirmation: the key's own name, typed. --yes does not answer
it, deliberately — a blanket yes is exactly how this gets done by accident.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockExclusive)
			if err != nil {
				return err
			}
			defer r.close()

			name := args[0]
			view := app.NewKeysController(r.eng).List()
			if confirmName != name {
				if !confirmPhrase(r, name, "%s\n\nThis will delete the key %q.", deleteWarningFor(view), name) {
					return precondition(fmt.Sprintf(
						"pcloak: deleting %q was not confirmed.\n  Pass --confirm-name %s to go ahead.", name, name))
				}
			}
			if f := app.NewKeysController(r.eng).Delete(name); f != nil {
				return exitWith(ExitFailed, "pcloak: "+f.Message)
			}
			r.out.Line("Deleted the key %q.", name)
			return nil
		},
	}
	c.Flags().StringVar(&confirmName, "confirm-name", "", "the key's own name, to confirm an irreversible deletion")
	return c
}

// deleteWarningFor is the engine's own sentence about what deleting a key costs.
// It is read off the controller rather than restated here, so the two surfaces
// cannot come to describe the same irreversible act differently.
func deleteWarningFor(v app.KeysView) string { return v.DeleteWarning }
