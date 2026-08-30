// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"github.com/spf13/cobra"

	"portcloak/internal/app"
	"portcloak/internal/engine/config"
	"portcloak/internal/engine/inspect"
)

func newSnapshotCmd(s Streams, g *globals) *cobra.Command {
	c := group("snapshot", "Browse and read the snapshots in your storage",
		`A snapshot is one sealed, checksummed, optionally encrypted bundle holding
exactly one realm.

Listing needs no key at all: it is built from the secret-free manifest sidecar
written beside every bundle. Anything that reads inside a snapshot has to open
it, which means fetching it, decrypting it and verifying its integrity tree.`, "snapshots")
	c.AddCommand(
		newSnapshotListCmd(s, g),
		newSnapshotShowCmd(s, g),
		newSnapshotVerifyCmd(s, g),
		newSnapshotLedgerCmd(s, g),
		newSnapshotEntitiesCmd(s, g),
		newSnapshotUsersCmd(s, g),
		newSnapshotDeleteCmd(s, g),
	)
	return c
}

func newSnapshotListCmd(s Streams, g *globals) *cobra.Command {
	var realm, storage string
	c := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Every snapshot across every storage, without a key",
		Long: `Built entirely from the manifest sidecar written beside each bundle, which holds
no secret — so this works with no key on a machine that could not open any of
them.

An unencrypted snapshot is labelled as such wherever it appears, here included.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			lib := app.NewSnapshotController(r.eng).Library()
			entries := filterEntries(lib.Entries, realm, storage)
			if g.json {
				lib.Entries = entries
				return r.out.JSON(lib)
			}

			// A storage that could not be reached is named rather than folded
			// into an empty list. "There are no snapshots" and "I could not
			// look" are different answers.
			for _, st := range lib.Storages {
				if !st.Reachable {
					r.out.Note("not listed: %s — %s", st.Name, st.Error)
				}
			}
			if len(entries) == 0 {
				r.out.Note("No snapshots.")
				return nil
			}
			rows := make([][]string, 0, len(entries))
			for _, e := range entries {
				rows = append(rows, []string{
					shortID(e.SnapshotID), e.Realm, e.Storage,
					e.CreatedAt.Local().Format("2006-01-02 15:04"),
					humanBytes(e.Bytes), sealedAs(e),
				})
			}
			r.out.Table([]string{"SNAPSHOT", "REALM", "STORAGE", "CREATED", "SIZE", "SEALED"}, rows)
			r.out.Note("%s", lib.Summary)
			return nil
		},
	}
	c.Flags().StringVar(&realm, "realm", "", "only snapshots of this realm")
	c.Flags().StringVar(&storage, "storage", "", "only snapshots in this storage")
	return c
}

func filterEntries(in []inspect.Entry, realm, storage string) []inspect.Entry {
	out := make([]inspect.Entry, 0, len(in))
	for _, e := range in {
		if realm != "" && e.Realm != realm {
			continue
		}
		if storage != "" && e.Storage != storage {
			continue
		}
		out = append(out, e)
	}
	return out
}

// shortID abbreviates a snapshot id for a listing, the way git abbreviates a
// hash: long enough to be unique in practice, short enough to read across a row.
// Commands accept the abbreviation back.
func shortID(id string) string {
	if len(id) > 10 {
		return id[:10]
	}
	return id
}

func newSnapshotShowCmd(s Streams, g *globals) *cobra.Command {
	var k keySource
	c := &cobra.Command{
		Use:   "show <snapshot>",
		Short: "Open a snapshot and print what it carries",
		Long: `Fetches the bundle, decrypts it, verifies its integrity tree and reads the
manifest: what travelled, what did not, and whether tokens signed before the
capture will still verify after a restore.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			view, done, err := openSnapshot(r, args[0], k)
			if err != nil {
				return err
			}
			defer done()

			if g.json {
				return r.out.JSON(view)
			}
			renderOverview(r, view)
			return nil
		},
	}
	k.register(c)
	return c
}

func renderOverview(r *run, v *app.Overview) {
	r.out.Line("snapshot    %s", v.SnapshotID)
	r.out.Line("realm       %s", v.Realm)
	r.out.Line("storage     %s", v.Storage)
	r.out.Line("sealed      %s", sealedDescription(v))
	r.out.Line("integrity   %s", v.IntegrityMessage)
	r.out.Line("")
	r.out.Line("users       %d", v.Counts.Users)
	r.out.Line("clients     %d", v.Counts.Clients)
	r.out.Line("keys        %d", v.Counts.KeyProviders)
	r.out.Line("secrets     %d", v.SecretCount)
	r.out.Line("")
	r.out.Line("tokens      %s", v.TokenContinuityNote)

	if len(v.Dependencies) > 0 {
		// These are never migrated, only reported. Discovering at restore time
		// that a realm needs a theme nobody copied is how an import that looked
		// clean fails at the first login.
		r.out.Line("")
		r.out.Line("Provision these at the destination before restoring:")
		for _, d := range v.Dependencies {
			r.out.Line("  %-10s %s", d.Type, d.Name)
		}
	}
	if v.Warning != "" {
		r.out.Note("%s", v.Warning)
	}
	if v.Degraded {
		r.out.Note("%s", v.DegradedNote)
	}
	if v.UnlockedWith != "" {
		// A key used without being asked for is still a key the operator gets
		// to see the name of.
		r.out.Note("Opened with the stored key %q.", v.UnlockedWith)
	}
}

func sealedDescription(v *app.Overview) string {
	if !v.Encrypted {
		return "UNENCRYPTED"
	}
	if v.EncryptionMode != "" {
		return "encrypted (" + v.EncryptionMode + ")"
	}
	return "encrypted"
}

func newSnapshotVerifyCmd(s Streams, g *globals) *cobra.Command {
	var k keySource
	c := &cobra.Command{
		Use:   "verify <snapshot>",
		Short: "Prove a snapshot is intact, without restoring it",
		Long: `Recomputes every artifact checksum and the integrity tree over them, and confirms
the bundle decrypts.

Touches no Keycloak. This is the check to run before you need the snapshot, not
after.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			view, done, err := openSnapshot(r, args[0], k)
			if err != nil {
				return err
			}
			defer done()

			rep, fail := app.NewInspectController(r.eng).Verify(view.SnapshotID)
			if fail != nil {
				return exitWith(ExitFailed, "pcloak: "+fail.Message)
			}
			if g.json {
				if err := r.out.JSON(rep); err != nil {
					return err
				}
			} else {
				r.out.Line("%s", rep.Message)
				r.out.Line("root        %s", cell(rep.Root))
				for _, a := range rep.Artifacts {
					if !a.OK {
						r.out.Line("  ✗ %s", a.Name)
					}
				}
				r.out.Note("%s", rep.Note)
			}
			if !rep.OK {
				// A snapshot that cannot be proven intact is never written to a
				// target, so this is a precondition rather than a plain failure.
				return exitWith(ExitPrecondition, "")
			}
			return nil
		},
	}
	k.register(c)
	return c
}

func newSnapshotLedgerCmd(s Streams, g *globals) *cobra.Command {
	var k keySource
	c := &cobra.Command{
		Use:   "ledger <snapshot>",
		Short: "Every secret the snapshot carries, by type and location",
		Long: `What is in the bundle, never what it is worth: a client secret is listed as
present at its location, and its value is not printed.

Reading a value is a separate, audited act — and one the app gates behind a
preference an operator can switch off entirely.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			view, done, err := openSnapshot(r, args[0], k)
			if err != nil {
				return err
			}
			defer done()

			led := app.NewInspectController(r.eng).Ledger(view.SnapshotID)
			if g.json {
				return r.out.JSON(led)
			}
			rows := make([][]string, 0, len(led.Entries))
			for _, e := range led.Entries {
				rows = append(rows, []string{e.KindLabel, e.Location, e.Status})
			}
			r.out.Table([]string{"CATEGORY", "LOCATION", "STATE"}, rows)
			r.out.Note("%s", led.Summary)
			r.out.Note("%s", led.Note)
			return nil
		},
	}
	k.register(c)
	return c
}

func newSnapshotEntitiesCmd(s Streams, g *globals) *cobra.Command {
	var k keySource
	c := &cobra.Command{
		Use:   "entities <snapshot>",
		Short: "Clients, keys, identity providers, federations and flows",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			view, done, err := openSnapshot(r, args[0], k)
			if err != nil {
				return err
			}
			defer done()

			ent := app.NewInspectController(r.eng).Entities(view.SnapshotID)
			if g.json {
				return r.out.JSON(ent)
			}

			r.out.Line("Clients (%d)", len(ent.Clients))
			rows := make([][]string, 0, len(ent.Clients))
			for _, cl := range ent.Clients {
				rows = append(rows, []string{"  " + cl.ClientID, yesNo(cl.Enabled), yesNo(cl.SecretPresent)})
			}
			r.out.Table([]string{"  CLIENT", "ENABLED", "SECRET"}, rows)

			r.out.Line("")
			r.out.Line("Keys (%d)", len(ent.Keys))
			rows = rows[:0]
			for _, key := range ent.Keys {
				// Whether the private half travelled is the whole question: it
				// is what decides if tokens signed before the move still verify
				// after it.
				rows = append(rows, []string{"  " + cell(key.Algorithm), cell(key.KID), yesNo(key.PrivateCarried)})
			}
			r.out.Table([]string{"  ALGORITHM", "KID", "PRIVATE"}, rows)

			if len(ent.Dependencies) > 0 {
				r.out.Line("")
				r.out.Line("External dependencies (%d) — provision these yourself", len(ent.Dependencies))
				for _, d := range ent.Dependencies {
					r.out.Line("  %-10s %s", d.Type, d.Name)
				}
			}
			r.out.Note("%s", ent.DependencyNote)
			return nil
		},
	}
	k.register(c)
	return c
}

func newSnapshotUsersCmd(s Streams, g *globals) *cobra.Command {
	var k keySource
	var query string
	var limit, offset int
	c := &cobra.Command{
		Use:   "users <snapshot>",
		Short: "Search the users inside a snapshot",
		Long: `Credential presence only, never credential values: whether an account has a
password and which algorithm hashed it, how many passkeys it carries, whether it
has OTP enrolled. The values themselves are in the bundle and stay there.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			view, done, err := openSnapshot(r, args[0], k)
			if err != nil {
				return err
			}
			defer done()

			res := app.NewInspectController(r.eng).Users(app.UsersQuery{
				SnapshotID: view.SnapshotID,
				Query:      query,
				Limit:      limit,
				Offset:     offset,
			})
			if res.Failure != nil {
				return exitWith(ExitFailed, "pcloak: "+res.Failure.Message)
			}
			if g.json {
				return r.out.JSON(res)
			}
			rows := make([][]string, 0, len(res.Page.Rows))
			for _, u := range res.Page.Rows {
				// Presence, never values: which algorithm hashed the password
				// and whether a second factor is enrolled, not the hash and not
				// the seed.
				rows = append(rows, []string{u.Username, cell(u.Email), cell(u.Origin),
					cell(u.PasswordAlgorithm), cell(u.SecondFactor)})
			}
			r.out.Table([]string{"USERNAME", "EMAIL", "ORIGIN", "PASSWORD", "2FA"}, rows)
			r.out.Note("showing %d of %d", len(rows), res.Page.Total)
			if res.Empty != "" {
				r.out.Note("%s", res.Empty)
			}
			r.out.Note("%s", res.Note)
			return nil
		},
	}
	k.register(c)
	c.Flags().StringVar(&query, "query", "", "match username, email, first or last name, or user id")
	c.Flags().IntVar(&limit, "limit", 50, "how many to print")
	c.Flags().IntVar(&offset, "offset", 0, "skip this many first")
	return c
}

func newSnapshotDeleteCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:     "delete <snapshot>",
		Aliases: []string{"rm"},
		Short:   "Remove a snapshot and its sidecars",
		Long: `Removes the bundle, its manifest and its checksum as one operation, so a storage
is never left holding two thirds of a snapshot.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockShared)
			if err != nil {
				return err
			}
			defer r.close()

			entry, err := findSnapshot(r, args[0])
			if err != nil {
				return err
			}
			if !g.yes && !confirm(r, "Delete %s (%s, %s)?", shortID(entry.SnapshotID), entry.Realm, entry.Storage) {
				return exitWith(ExitOK, "Left alone.")
			}
			res := app.NewSnapshotController(r.eng).Delete(entry.Storage, entry.BundleKey)
			if res.Failure != nil {
				return exitWith(ExitFailed, "pcloak: "+res.Failure.Message)
			}
			r.out.Line("%s", res.Note)
			return nil
		},
	}
}
