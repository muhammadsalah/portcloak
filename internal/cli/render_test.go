// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"portcloak/internal/app"
	"portcloak/internal/engine/config"
	"portcloak/internal/engine/inspect"
	"portcloak/internal/engine/obs"
)

// An unencrypted snapshot holds unmasked client secrets, LDAP bind credentials
// and RSA private signing keys in the clear. It carries a warning everywhere it
// appears, and a listing is somewhere it appears — so this must not quietly read
// as one more encryption mode.
func TestSealedAs_ShoutsAboutAnUnencryptedBundle(t *testing.T) {
	if got := sealedAs(inspect.Entry{Encrypted: false}); got != "UNENCRYPTED" {
		t.Errorf("an unencrypted bundle rendered as %q", got)
	}
	if got := sealedAs(inspect.Entry{Encrypted: true, EncryptionMode: "passphrase"}); got != "passphrase" {
		t.Errorf("got %q", got)
	}
	// Encrypted but with no mode recorded — an older bundle — must still not
	// read as unencrypted.
	if got := sealedAs(inspect.Entry{Encrypted: true}); got != "encrypted" {
		t.Errorf("got %q", got)
	}
}

// A phase that reached its turn and abstained must never be drawn as one that
// passed: "verification passed" and "verification did not run" are the
// difference between a snapshot whose secrets were checked and one whose were
// not. This is the terminal half of the fix in commit 475e4db.
func TestRenderPhases_ASkippedPhaseIsNotDrawnAsPassed(t *testing.T) {
	var res result
	r := &run{s: Streams{Out: &res.Out, Err: &res.Err}, g: &globals{}}
	r.out = newRenderer(r.s, r.g)

	renderPhases(r, []app.PhaseView{
		{Phase: "export", Label: "Exporting the realm", Done: true},
		{Phase: "verify", Label: "Verifying secrets", Done: true, Skipped: true},
		{Phase: "upload", Label: "Uploading", Live: true},
	})

	out := res.stdout()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected three phases, got:\n%s", out)
	}
	if !strings.Contains(lines[0], "✓") {
		t.Errorf("a completed phase should be ticked: %q", lines[0])
	}
	if strings.Contains(lines[1], "✓") {
		t.Errorf("a skipped phase was ticked like one that passed: %q", lines[1])
	}
	if !strings.Contains(lines[1], "skipped") {
		t.Errorf("a skipped phase does not say so: %q", lines[1])
	}
}

// A finished job shows no phase. Showing the last one it passed through reads as
// though it stopped there.
func TestPhaseOf_IsBlankForAFinishedJob(t *testing.T) {
	for _, tc := range []struct {
		state config.JobState
		want  string
	}{
		{config.JobRunning, "export"},
		{config.JobInterrupted, "export"},
		{config.JobCompleted, ""},
		{config.JobFailed, ""},
		{config.JobCancelled, ""},
	} {
		j := app.JobView{Job: config.Job{State: tc.state, Phase: "export"}}
		if got := phaseOf(j); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.state, got, tc.want)
		}
	}
}

// A credential handle maps to one environment variable name, and it has to be
// derivable in a shell without running PortCloak to ask.
func TestEnvNameFor_DerivesAReadableVariable(t *testing.T) {
	got := envNameFor(config.Handle("environment", "prod-keycloak"))
	if got != "PORTCLOAK_CREDENTIAL_ENVIRONMENT_PROD_KEYCLOAK" {
		t.Errorf("got %q", got)
	}
	// Anything that is not a handle has no variable, rather than a plausible
	// wrong one.
	if got := envNameFor("not-a-handle"); got != "" {
		t.Errorf("a non-handle produced %q", got)
	}
}

// A trailing newline is how every editor ends a file. Including it in a
// passphrase would seal a snapshot that the same file cannot then open.
func TestFirstLine_DropsTheNewlineAnEditorAdded(t *testing.T) {
	for in, want := range map[string]string{
		"secret\n":          "secret",
		"secret\r\n":        "secret",
		"secret":            "secret",
		"secret\nignored\n": "secret",
		"":                  "",
	} {
		if got := firstLine(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

// A recipients file is written by a person, so it has blank lines and comments
// in it.
func TestReadLines_SkipsBlanksAndComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recipients.txt")
	body := "# the release key\nage1aaa\n\n  age1bbb  \n# and a spare\nage1ccc\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readLines(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"age1aaa", "age1bbb", "age1ccc"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// The classification a script branches on. Only two things are inferred, because
// those are the two a caller acts on differently; everything else is honestly
// just "it failed", and guessing more finely would invite a script to branch on
// a distinction the tool cannot make.
func TestCodeFor_ClassifiesOnlyWhatItCanTell(t *testing.T) {
	if got := codeFor(errHomeBusy, nil); got != ExitBusy {
		t.Errorf("a busy folder mapped to %d", got)
	}
	if got := codeFor(nil, &app.Failure{Retryable: true}); got != ExitRetryable {
		t.Errorf("a retryable failure mapped to %d", got)
	}
	if got := codeFor(nil, &app.Failure{}); got != ExitFailed {
		t.Errorf("an ordinary failure mapped to %d", got)
	}
}

// The terminal sink renders only the jobs this invocation started. The engine's
// sink is process-wide, so without this a resumed job or a second realm would
// interleave its phases into a report about something else.
func TestTermSink_RendersOnlyTheJobsItWasToldToWatch(t *testing.T) {
	var res result
	ts := newTermSink(Streams{Out: &res.Out, Err: &res.Err}, &globals{noColor: true})
	ts.Watch("mine", "corp-a")

	ts.Emit(obs.Event{JobID: "mine", Kind: obs.EventPhaseCompleted,
		Phase: obs.PhaseExport, Message: "3 files written."})
	ts.Emit(obs.Event{JobID: "somebody-elses", Kind: obs.EventPhaseCompleted,
		Phase: obs.PhaseExport, Message: "should not appear"})

	out := res.stderr()
	if !strings.Contains(out, "3 files written") {
		t.Errorf("the watched job's phase was not rendered:\n%s", out)
	}
	if strings.Contains(out, "should not appear") {
		t.Errorf("another job's phase was rendered:\n%s", out)
	}
	// Progress narrates on stderr, never on stdout, or a run could not be piped
	// while it talked.
	if res.stdout() != "" {
		t.Errorf("progress reached stdout: %q", res.stdout())
	}
}

// Two of the three skip paths say so in the message they complete with. The
// third does not, which is why the job record and not this is the authority —
// but the two that do should still not be ticked live.
func TestIsSkip_RecognisesTheEnginesOwnWording(t *testing.T) {
	for _, yes := range []string{
		"Verification was skipped.",
		"The Admin API was not reachable, so secret verification and dependency detection were skipped.",
		"Skipped — nothing to do.",
		"Dependency detection did not run.",
	} {
		if !isSkip(yes) {
			t.Errorf("not recognised as a skip: %q", yes)
		}
	}
	for _, no := range []string{
		"Every carried secret was confirmed to be a real value.",
		"3 file(s) written.",
		"",
	} {
		if isSkip(no) {
			t.Errorf("wrongly read as a skip: %q", no)
		}
	}
}

func TestHumanBytes_ReadsLikeASize(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0 B", 512: "512 B", 1024: "1.0 KiB",
		1536: "1.5 KiB", 1 << 20: "1.0 MiB", 1 << 30: "1.0 GiB",
	} {
		if got := humanBytes(n); got != want {
			t.Errorf("%d → %q, want %q", n, got, want)
		}
	}
}

// A definition can be well-formed and still unusable — the ordinary state after
// copying config.yaml between machines, which brings the handles and not the
// secrets. That has to read as a state, not as a fault.
func TestReadiness_SaysWhyWhenItCan(t *testing.T) {
	if got := readiness(config.Readiness{Ready: true}); got != "ready" {
		t.Errorf("got %q", got)
	}
	if got := readiness(config.Readiness{Reason: "no SSH key on this machine"}); got != "no SSH key on this machine" {
		t.Errorf("got %q", got)
	}
	if got := readiness(config.Readiness{}); got != "not ready" {
		t.Errorf("got %q", got)
	}
}

// An empty table prints nothing rather than a header over a void, which reads as
// a bug in the filter that produced it.
func TestTable_PrintsNothingWhenThereAreNoRows(t *testing.T) {
	var res result
	r := newRenderer(Streams{Out: &res.Out, Err: &res.Err}, &globals{})
	r.Table([]string{"NAME", "KIND"}, nil)
	if res.stdout() != "" {
		t.Errorf("an empty table printed a header:\n%q", res.stdout())
	}
}
