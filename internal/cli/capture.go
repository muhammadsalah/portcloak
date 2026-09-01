// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"portcloak/internal/app"
	"portcloak/internal/engine/config"
	"portcloak/internal/engine/snapshot"
)

type captureFlags struct {
	env       string
	realms    []string
	allRealms bool
	storage   string

	usersMode    string
	usersPerFile int
	verify       bool
	detectDeps   bool
	noTxTimeout  bool

	encrypt    bool
	noEncrypt  bool
	key        string
	recipients []string
	recipFile  string
	passphrase string
	passFile   string
	passStdin  bool
	// acknowledge is the unencrypted acknowledgement. It is long, first-person
	// and has no shorthand on purpose: the thing it agrees to is that the file
	// will hold RSA private signing keys in the clear, and it must not be
	// reachable by muscle memory or by a blanket --yes.
	acknowledge bool
}

func newCaptureCmd(s Streams, g *globals) *cobra.Command {
	var f captureFlags

	c := &cobra.Command{
		Use:   "capture",
		Short: "Capture one or more realms into snapshots",
		Long: `Reads a realm out of a Keycloak through its own offline export and seals it into
a portable, checksummed snapshot.

The serving instance is never disturbed. On Docker and Kubernetes the export runs
inside an ephemeral clone of the workload, which is destroyed afterwards on every
path; on local and SSH targets it binds ports the OS has just said are free.

Each realm becomes its own job and its own snapshot: one snapshot holds exactly
one realm, so several realms means several independent, individually restorable
bundles rather than one large one.

This blocks until the run finishes. There is no --detach: the engine's own
detach is an in-process goroutine, so a command that returned early would take
the export down with it and leave a clone running in your cluster. Background it
with the shell and follow it with ` + "`pcloak job logs -f`" + `.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCapture(cmd, s, g, &f)
		},
	}

	fl := c.Flags()
	fl.StringVarP(&f.env, "env", "e", "", "the environment to capture from (required)")
	fl.StringArrayVarP(&f.realms, "realm", "r", nil, "a realm to capture; repeat for several")
	fl.BoolVar(&f.allRealms, "all-realms", false, "capture every realm the Admin API reports")
	fl.StringVarP(&f.storage, "storage", "s", "", "where to put the snapshots (default: the default storage)")

	fl.StringVar(&f.usersMode, "users-mode", "", "different_files, same_file, realm_file or skip")
	fl.IntVar(&f.usersPerFile, "users-per-file", 0, "users per file in different_files mode")
	fl.BoolVar(&f.verify, "verify", true, "confirm through the Admin API that exported secrets are real values")
	fl.BoolVar(&f.detectDeps, "detect-deps", true, "report the themes and provider JARs the realm needs")
	fl.BoolVar(&f.noTxTimeout, "no-transaction-timeout", false,
		"let the export's transactions run without a time limit, for a large or federated realm")

	fl.BoolVar(&f.encrypt, "encrypt", true, "seal the snapshot")
	fl.BoolVar(&f.noEncrypt, "no-encrypt", false, "write the snapshot in the clear")
	fl.StringVar(&f.key, "key", "", "seal with a key already stored on this machine")
	fl.StringArrayVar(&f.recipients, "recipient", nil, "an age recipient to seal to; repeat for several")
	fl.StringVar(&f.recipFile, "recipients-file", "", "read age recipients from this file, one per line")
	fl.StringVar(&f.passphrase, "passphrase", "", "the passphrase itself; visible in ps, so prefer the two below")
	fl.StringVar(&f.passFile, "passphrase-file", "", "read the passphrase from this file (- for stdin)")
	fl.BoolVar(&f.passStdin, "passphrase-stdin", false, "read the passphrase from stdin")
	c.MarkFlagsMutuallyExclusive("passphrase", "passphrase-file", "passphrase-stdin")
	fl.BoolVar(&f.acknowledge, "i-understand-unencrypted", false,
		"confirm that an unencrypted snapshot holds unmasked secrets and private signing keys in the clear")

	_ = c.MarkFlagRequired("env")
	c.MarkFlagsMutuallyExclusive("realm", "all-realms")
	c.MarkFlagsMutuallyExclusive("encrypt", "no-encrypt")
	return c
}

func runCapture(cmd *cobra.Command, s Streams, g *globals, f *captureFlags) error {
	r, err := open(cmd, g, s, config.LockShared)
	if err != nil {
		return err
	}
	defer r.close()

	capture := app.NewCaptureController(r.eng)
	defaults := capture.Defaults()

	storage := f.storage
	if storage == "" {
		storage = defaults.DefaultStorage
	}
	if storage == "" {
		return precondition(
			"pcloak: no storage was named and there is no default.\n" +
				"  Pass --storage, or set a default in the PortCloak app.")
	}

	realms, err := resolveRealms(r, capture, f)
	if err != nil {
		return err
	}

	opts := app.CaptureOptions{
		Environment:          f.env,
		Storage:              storage,
		Realms:               realms,
		UsersMode:            f.usersMode,
		UsersPerFile:         f.usersPerFile,
		Verify:               f.verify,
		DetectDependencies:   f.detectDeps,
		NoTransactionTimeout: f.noTxTimeout,
	}
	if err := resolveEncryption(r, f, &opts); err != nil {
		return err
	}

	sink := r.sink()
	res := capture.Start(opts)
	if res.Failure != nil {
		// Start refuses before queueing anything — an unacknowledged
		// unencrypted capture, a storage that requires encryption, a realm list
		// that is empty. Nothing was written anywhere.
		return exitWith(ExitPrecondition, "pcloak: "+res.Failure.Message+hintLine(res.Failure.Hint))
	}
	for i, id := range res.JobIDs {
		realm := ""
		if i < len(res.Realms) {
			realm = res.Realms[i]
		}
		sink.Watch(id, realm)
	}

	r.out.Note("Capturing %s from %s → %s (%s)",
		strings.Join(realms, ", "), f.env, storage, describeSealing(opts))

	ctx, cancel := r.withTimeout(context.Background())
	defer cancel()

	out := newWaiter(r, sink).wait(ctx, res.JobIDs)
	renderCaptureSummary(r, out, storage)
	return verdict(out)
}

// resolveRealms decides what to capture.
//
// --all-realms depends on the Admin API being reachable, which is not a given:
// an offline export reads the database directly and works perfectly well against
// a stopped Keycloak that has no API at all. Where the realms cannot be
// enumerated this refuses and says to name them, rather than capturing a guess.
func resolveRealms(r *run, capture *app.CaptureController, f *captureFlags) ([]string, error) {
	if len(f.realms) > 0 {
		return f.realms, nil
	}
	if !f.allRealms {
		return nil, precondition(
			"pcloak: no realm was named.\n" +
				"  Pass --realm <name>, repeated for several, or --all-realms.")
	}

	res := capture.Realms(f.env)
	if res.Failure != nil {
		return nil, exitWith(ExitPrecondition, "pcloak: "+res.Failure.Message)
	}
	if !res.Discovered {
		return nil, precondition(
			"pcloak: the realms on " + f.env + " could not be listed, so --all-realms has nothing to expand to.\n" +
				"  " + res.Note + "\n" +
				"  Name them with --realm instead.")
	}
	if len(res.Realms) == 0 {
		return nil, precondition("pcloak: " + f.env + " reports no realms.")
	}
	return res.Realms, nil
}

// resolveEncryption works out how the snapshot is sealed, and enforces that
// declining is a deliberate act.
//
// The mode is inferred rather than named by a flag of its own: a recipient
// source means recipients, a passphrase source means a passphrase. There is no
// third thing it could mean, and a --mode flag that had to agree with the other
// flags would be one more way to be wrong.
func resolveEncryption(r *run, f *captureFlags, opts *app.CaptureOptions) error {
	encrypt := !f.noEncrypt
	opts.Encrypt = encrypt

	if !encrypt {
		if !f.acknowledge {
			// The engine refuses this too — CaptureController.Start will not
			// queue an unacknowledged unencrypted capture — but refusing here
			// means the notice is printed before a probe runs rather than after.
			return precondition("pcloak: " + declineNoticeFor(r) +
				"\n\n  Nothing was captured. Pass --i-understand-unencrypted to go ahead.")
		}
		opts.AcknowledgedUnencrypted = true
		r.out.Note("WARNING: writing UNENCRYPTED. The file will hold unmasked client secrets,")
		r.out.Note("  LDAP bind credentials and RSA private signing keys in the clear.")
		return nil
	}

	if f.key != "" {
		return sealWithStoredKey(r, f.key, opts)
	}

	recipients := append([]string(nil), f.recipients...)
	if f.recipFile != "" {
		lines, err := readLines(f.recipFile, nil)
		if err != nil {
			return exitWith(ExitPrecondition, "pcloak: reading "+f.recipFile+": "+err.Error())
		}
		recipients = append(recipients, lines...)
	}
	if len(recipients) > 0 {
		opts.EncryptionMode = string(snapshot.EncryptionRecipients)
		opts.Recipients = recipients
		return nil
	}

	pass, err := (secretSource{
		value: f.passphrase, file: f.passFile, stdin: f.passStdin,
		env: "PORTCLOAK_PASSPHRASE", required: true,
	}).read(r, "Passphrase to seal the snapshot", true)
	if err != nil {
		if err == errNotATerminal {
			return missingSecret("a passphrase to seal the snapshot with",
				"--key", "--recipient", "--passphrase-file", "--passphrase-stdin")
		}
		return exitWith(ExitPrecondition, "pcloak: "+err.Error())
	}
	opts.EncryptionMode = string(snapshot.EncryptionPassphrase)
	opts.Passphrase = pass
	return nil
}

// sealWithStoredKey resolves --key into whichever half a capture needs.
//
// An identity key contributes its recipient — the public half, which is not a
// secret and needs no keychain read to seal with. A passphrase key contributes
// the passphrase itself, which does. Either way nothing appears in argv.
func sealWithStoredKey(r *run, name string, opts *app.CaptureOptions) error {
	keys := app.NewKeysController(r.eng)
	for _, rec := range keys.Recipients() {
		if rec.Name == name {
			opts.EncryptionMode = string(snapshot.EncryptionRecipients)
			opts.Recipients = []string{rec.PublicKey}
			if !rec.Openable {
				// Sealing to a key whose private half is elsewhere is a real
				// workflow — capture here, restore there — but it is worth
				// saying out loud, because the snapshot cannot be opened on
				// this machine afterwards.
				r.out.Note("Sealing to %q, whose private half is not on this machine: this snapshot will not open here.", name)
			}
			return nil
		}
	}

	view := keys.List()
	for _, k := range view.Keys {
		if k.Name != name || k.Kind != config.KeyPassphrase {
			continue
		}
		if !k.Usable {
			return precondition(fmt.Sprintf(
				"pcloak: the key %q is configured, but its secret is not in this machine's keychain.\n"+
					"  Configuration is portable between machines; the secrets deliberately are not.", name))
		}
		secret, err := r.eng.Creds.Get(k.CredentialRef)
		if err != nil {
			return exitWith(ExitPrecondition, "pcloak: "+app.Fail(err).Message)
		}
		opts.EncryptionMode = string(snapshot.EncryptionPassphrase)
		opts.Passphrase = secret
		return nil
	}
	return notFound("key", name, names(view.Keys, func(k app.KeyView) string { return k.Name }))
}

// declineNoticeFor is the engine's own paragraph about what declining encryption
// means. It is read off the controller rather than restated, so the window and
// the terminal cannot come to describe the same decision differently.
func declineNoticeFor(r *run) string {
	return app.NewCaptureController(r.eng).Defaults().DeclineNotice
}

func describeSealing(o app.CaptureOptions) string {
	if !o.Encrypt {
		return "UNENCRYPTED"
	}
	if o.EncryptionMode != "" {
		return "encrypted · " + o.EncryptionMode
	}
	return "encrypted"
}

// renderCaptureSummary is what a run leaves on screen.
//
// One line per realm, because that is the unit: one snapshot holds exactly one
// realm, and a run where two of three succeeded produced two real, restorable
// snapshots. Collapsing that into "failed" would hide them.
//
// The skipped phases come off the job record rather than off the event stream.
// A phase can be both done and skipped — it reached its turn and abstained —
// and only the record carries that. Verification that did not run must never
// read as verification that passed.
func renderCaptureSummary(r *run, o outcome, storage string) {
	if len(o.Jobs) == 0 {
		return
	}
	ok := 0
	rows := make([][]string, 0, len(o.Jobs))
	for _, j := range o.Jobs {
		if j == nil {
			rows = append(rows, []string{dash, "FAILED", "the job record could not be read", ""})
			continue
		}
		state := strings.ToUpper(string(j.State))
		if j.State == config.JobCompleted {
			ok++
			state = "ok"
		}
		detail := j.Message
		if len(j.SkippedPhases) > 0 {
			detail += " (skipped: " + strings.Join(j.SkippedPhases, ", ") + ")"
		}
		where := j.StorageKey
		if j.State != config.JobCompleted {
			where = "job " + j.ID
		}
		rows = append(rows, []string{j.Realm, state, detail, where})
	}

	r.out.Line("")
	if len(o.Jobs) > 1 {
		r.out.Line("Captured %d of %d realms into %s.", ok, len(o.Jobs), storage)
	}
	r.out.Table(nil, rows)

	for _, j := range o.Jobs {
		if j != nil && j.State == config.JobInterrupted {
			r.out.Note("Resume it with: pcloak job resume %s", j.ID)
		}
	}
}
