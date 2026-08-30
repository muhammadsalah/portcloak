// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"portcloak/internal/app"
)

// The tests go through the root command rather than around it, so the wiring —
// flag parsing, lock tier, engine construction, rendering, exit code — is what
// is under test. A test that called a controller directly would prove the
// controller works, which internal/app already proves.

type result struct {
	Out, Err bytes.Buffer
	Code     int
}

func (r result) stdout() string { return r.Out.String() }
func (r result) stderr() string { return r.Err.String() }
func (r result) all() string    { return r.Out.String() + r.Err.String() }

// runCLI executes the command line exactly as main does, against a scratch home
// and with the keychain out of reach.
//
// --no-keychain is not a convenience: a test that read this machine's real
// keychain would prompt on macOS, and would pass or fail depending on whose
// laptop it ran on.
func runCLI(t *testing.T, home string, args ...string) result {
	t.Helper()
	var res result
	full := append([]string{"--home", home, "--no-keychain", "--no-color"}, args...)
	res.Code = Main(app.NewBuild("test", "abc123", ""), full, Streams{
		In: strings.NewReader(""), Out: &res.Out, Err: &res.Err,
	})
	return res
}

// scratchHome is a PortCloak folder that exists only for one test.
func scratchHome(t *testing.T) string {
	t.Helper()
	// PORTCLOAK_HOME is cleared so a developer who has one set does not have
	// their real folder reached into by --home's precedence being wrong.
	t.Setenv("PORTCLOAK_HOME", "")
	return t.TempDir()
}

func TestCLI_SnapshotsListOnAnEmptyLibrary(t *testing.T) {
	res := runCLI(t, scratchHome(t), "snapshot", "list")
	if res.Code != ExitOK {
		t.Fatalf("an empty library is not an error; exit %d\n%s", res.Code, res.all())
	}
	if !strings.Contains(res.all(), "No snapshots") {
		t.Errorf("expected an empty-library note, got:\n%s", res.all())
	}
}

// Progress narrates on stderr and results land on stdout, without exception.
// It is what lets `pcloak snapshot list --json | jq` work while a run is still
// talking, and the difference between a tool that can be piped and one that can
// only be watched.
func TestCLI_ResultsGoToStdoutAndProgressToStderr(t *testing.T) {
	home := scratchHome(t)
	res := runCLI(t, home, "snapshot", "list", "--json")
	if res.Code != ExitOK {
		t.Fatalf("exit %d\n%s", res.Code, res.all())
	}
	// The real property is that stdout parses, whole, as one JSON document.
	// Checking for the absence of particular prose would not catch a note
	// appended after it, and would false-positive on prose the engine puts
	// inside the document — the library's own summary is a sentence.
	var doc map[string]any
	if err := json.Unmarshal(res.Out.Bytes(), &doc); err != nil {
		t.Fatalf("stdout is not a single JSON document: %v\n%q", err, res.stdout())
	}
	if _, ok := doc["entries"]; !ok {
		t.Errorf("the document is not the library view: %v", doc)
	}
}

// --help and --version have to work when the folder is busy, unreadable, or
// somewhere else entirely: they are what somebody runs when nothing else works.
func TestCLI_HelpNeedsNoHome(t *testing.T) {
	var res result
	res.Code = Main(app.NewBuild("test", "", ""),
		[]string{"--home", "/nonexistent/definitely/not/here", "--help"},
		Streams{In: strings.NewReader(""), Out: &res.Out, Err: &res.Err})
	if res.Code != ExitOK {
		t.Fatalf("--help failed against an unusable home: exit %d\n%s", res.Code, res.all())
	}
	if !strings.Contains(res.all(), "capture") {
		t.Error("help did not list the commands")
	}
}

func TestCLI_UnknownEnvironmentNamesWhatExists(t *testing.T) {
	res := runCLI(t, scratchHome(t), "env", "show", "nope")
	if res.Code != ExitPrecondition {
		t.Fatalf("a name that is not configured is a precondition; exit %d", res.Code)
	}
	if !strings.Contains(res.all(), "nope") {
		t.Errorf("the refusal does not repeat what was asked for:\n%s", res.all())
	}
}

// The gate that must never be reachable by accident. CaptureController.Start
// refuses an unacknowledged unencrypted capture, and pcloak refuses it earlier so
// the notice is printed before anything is probed.
func TestCLI_RefusesAnUnencryptedCaptureThatWasNotAcknowledged(t *testing.T) {
	home := scratchHome(t)
	res := runCLI(t, home, "capture", "-e", "anywhere", "-r", "corp", "-s", "somewhere", "--no-encrypt")
	if res.Code != ExitPrecondition {
		t.Fatalf("expected a precondition refusal, got exit %d\n%s", res.Code, res.all())
	}
	// The refusal has to state the consequence, not merely say no.
	if !strings.Contains(res.all(), "signing keys") {
		t.Errorf("the refusal does not say what an unencrypted snapshot holds:\n%s", res.all())
	}
	if !strings.Contains(res.all(), "--i-understand-unencrypted") {
		t.Errorf("the refusal does not name the flag that satisfies it:\n%s", res.all())
	}
	// And --yes must not be able to answer it.
	res = runCLI(t, home, "capture", "-e", "anywhere", "-r", "corp", "-s", "somewhere", "--no-encrypt", "--yes")
	if res.Code != ExitPrecondition {
		t.Fatalf("--yes answered the unencrypted acknowledgement; exit %d\n%s", res.Code, res.all())
	}
}

// A key entry whose secret went nowhere lists as present, seals a snapshot, and
// cannot open it. KeysController.store writes the secret first for exactly this
// reason, so the CLI refuses rather than producing the dead handle.
func TestCLI_KeyGenerateRefusesWithoutAKeychain(t *testing.T) {
	home := scratchHome(t)
	res := runCLI(t, home, "key", "generate", "doomed")
	if res.Code != ExitPrecondition {
		t.Fatalf("expected a refusal, got exit %d\n%s", res.Code, res.all())
	}
	if !strings.Contains(res.all(), "--no-keychain") {
		t.Errorf("the refusal does not name the flag responsible:\n%s", res.all())
	}
	// Nothing may have been written on the way to refusing. The refusal comes
	// before the engine is built at all, so on a fresh home there is not even a
	// config file yet — which is a stronger result than an empty one.
	body, err := os.ReadFile(home + "/config.yaml")
	if err == nil && strings.Contains(string(body), "doomed") {
		t.Error("the config entry was written for a key whose secret was not stored")
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// Deleting a key makes every snapshot sealed with it permanently unreadable, and
// PortCloak cannot say which ones those were. That is the same class of
// irreversibility as overwriting a live realm, so --yes must not answer it.
func TestCLI_KeyDeleteNeedsTheNameTyped(t *testing.T) {
	home := scratchHome(t)
	res := runCLI(t, home, "key", "delete", "whatever", "--yes")
	if res.Code != ExitPrecondition {
		t.Fatalf("--yes answered an irreversible deletion; exit %d\n%s", res.Code, res.all())
	}
	if !strings.Contains(res.all(), "--confirm-name") {
		t.Errorf("the refusal does not name what would satisfy it:\n%s", res.all())
	}
}

// --home outranks PORTCLOAK_HOME, and has to be reported as what it is. Reported
// as the default it would both mislabel the folder and offer to move something
// that cannot move.
func TestCLI_HomeFlagIsReportedAsAFlagNotAsTheDefault(t *testing.T) {
	home := scratchHome(t)
	t.Setenv("PORTCLOAK_HOME", t.TempDir())

	res := runCLI(t, home, "config", "path")
	if res.Code != ExitOK {
		t.Fatalf("exit %d\n%s", res.Code, res.all())
	}
	if !strings.Contains(res.stdout(), home) {
		t.Errorf("--home was not the folder in use:\n%s", res.stdout())
	}
	if !strings.Contains(res.stdout(), "source      flag") {
		t.Errorf("the folder's source is not reported as the flag:\n%s", res.stdout())
	}
}

// A path the operator typed is read, never created. Bootstrap writes the empty
// template on a first run, and left ungated a mistyped --config would start an
// empty PortCloak that looked deliberate.
func TestCLI_ConfigFileThatIsNotThereIsRefusedRatherThanCreated(t *testing.T) {
	home := scratchHome(t)
	missing := t.TempDir() + "/typo.yaml"

	var res result
	res.Code = Main(app.NewBuild("test", "", ""),
		[]string{"--home", home, "--config", missing, "config", "show"},
		Streams{In: strings.NewReader(""), Out: &res.Out, Err: &res.Err})

	if res.Code != ExitPrecondition {
		t.Fatalf("expected a refusal, got exit %d\n%s", res.Code, res.all())
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("a --config path that did not exist was created (%v)", err)
	}
}
