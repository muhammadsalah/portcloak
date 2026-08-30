// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"github.com/spf13/cobra"

	"portcloak/internal/app"
	"portcloak/internal/engine/inspect"
)

// An inspection session owns a decrypted working directory and, above a
// threshold, a SQLite index — both destroyed when it closes. The engine keeps
// them in memory, keyed by snapshot id, for as long as the process lives.
//
// A one-shot command is therefore its own whole session: it opens, reads, and
// closes. Nothing carries over to the next invocation, which is the honest
// consequence of a process that exits — and the reason `snapshot list` is worth
// having separately, since it answers most questions without opening anything.

// keySource is how a command may be told which key opens a bundle.
type keySource struct {
	passphraseFile  string
	passphraseStdin bool
	identityFiles   []string
}

func (k *keySource) register(c *cobra.Command) {
	f := c.Flags()
	f.StringVar(&k.passphraseFile, "passphrase-file", "", "read the passphrase from this file (- for stdin)")
	f.BoolVar(&k.passphraseStdin, "passphrase-stdin", false, "read the passphrase from stdin")
	f.StringArrayVar(&k.identityFiles, "identity-file", nil, "an age identity file that can open this snapshot (repeatable)")
}

// openSnapshot fetches, decrypts and verifies a snapshot, returning a session
// and the function that destroys its working directory.
//
// Keys already on this machine are tried before anything is asked for. A key
// PortCloak generated, stored and can read is one the operator has already
// decided to trust it with, and asking again at every command is the prompt that
// teaches people to turn encryption off.
func openSnapshot(r *run, id string, k keySource) (*app.Overview, func(), error) {
	entry, err := findSnapshot(r, id)
	if err != nil {
		return nil, nil, err
	}

	req := app.OpenRequest{
		Storage:    entry.Storage,
		BundleKey:  entry.BundleKey,
		SnapshotID: entry.SnapshotID,
	}
	if entry.Encrypted {
		identities, err := readIdentities(k.identityFiles)
		if err != nil {
			return nil, nil, err
		}
		req.Identities = identities

		// Only ask for a passphrase when one was actually offered. A bundle
		// sealed to recipients opens from the keychain or from --identity-file,
		// and prompting for a passphrase it has no use for would be asking a
		// question with no right answer.
		if k.passphraseFile != "" || k.passphraseStdin {
			pass, err := (secretSource{
				file:  k.passphraseFile,
				stdin: k.passphraseStdin,
				env:   "PORTCLOAK_PASSPHRASE",
			}).read(r, "Passphrase", false)
			if err != nil {
				return nil, nil, err
			}
			req.Passphrase = pass
		}
	}

	view := app.NewInspectController(r.eng).Open(req)
	if view.Failure != nil {
		// A snapshot that will not open is a precondition, not a crash: nothing
		// was changed anywhere, and the fix is a key rather than a retry.
		return nil, nil, exitWith(ExitPrecondition, "pcloak: "+view.Failure.Message+hintLine(view.Failure.Hint))
	}
	closer := func() { app.NewInspectController(r.eng).Close(view.SnapshotID) }
	return &view, closer, nil
}

func hintLine(hint string) string {
	if hint == "" {
		return ""
	}
	return "\n  " + hint
}

// readIdentities loads age secret keys out of files, never off the command line.
func readIdentities(paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		lines, err := readLines(p, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, lines...)
	}
	return out, nil
}

// findSnapshot resolves a snapshot id, accepting an unambiguous prefix.
//
// The library is keyless, so this costs no decryption: the id, realm, storage and
// bundle key all come from the manifest sidecar.
func findSnapshot(r *run, id string) (inspect.Entry, error) {
	lib := app.NewSnapshotController(r.eng).Library()
	var hits []inspect.Entry
	for _, e := range lib.Entries {
		if e.SnapshotID == id {
			return e, nil
		}
		if len(id) >= 4 && len(e.SnapshotID) >= len(id) && e.SnapshotID[:len(id)] == id {
			hits = append(hits, e)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return inspect.Entry{}, notFound("snapshot", id,
			names(lib.Entries, func(e inspect.Entry) string { return shortID(e.SnapshotID) + " (" + e.Realm + ")" }))
	default:
		return inspect.Entry{}, precondition("pcloak: \"" + id + "\" matches " + itoa(len(hits)) +
			" snapshots. Give more of the id:\n  " +
			joinNames(names(hits, func(e inspect.Entry) string { return e.SnapshotID + " (" + e.Realm + ")" })))
	}
}
