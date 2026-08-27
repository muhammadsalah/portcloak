package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The restore wizard passed `passphrase: ""` to both the pre-flight open and
// the apply, and had no input to collect anything else. An encrypted snapshot
// therefore failed on the Destination step with "could not be opened with the
// key supplied" — a key nothing had ever asked for.
//
// The engine cannot defend against this. Both calls take a key and an empty one
// is a legitimate value: the inspector deliberately tries without a key first,
// because a snapshot that needs none should not be interrogated for one. The
// difference is that restore knows from the library listing whether the bundle
// is encrypted, and knows it before either call.
//
// So the invariant lives here, at the only seam where it is visible: a literal
// empty key must not be hardcoded in the restore flow. The restore job opens
// the bundle again on its own — see Orchestrator.runRestore — so a key that
// reached only the pre-flight check would fail at the point of no return, with
// the clone already created.
func TestRestoreWizard_NeverHardcodesAnEmptyKey(t *testing.T) {
	src := readView(t, "restore", "RestorePage.tsx")

	// `passphrase: ""` and `identities: []`, however they are spaced.
	empty := regexp.MustCompile(`(?m)\b(passphrase|identities)\s*:\s*(""|''|\[\s*\])`)
	if m := empty.FindAllString(src, -1); len(m) > 0 {
		t.Errorf("the restore flow hardcodes an empty key %v; it has to carry what the operator entered, "+
			"both into the pre-flight open and into apply", m)
	}

	// And the field has to exist, or there is nothing to carry. It lives on the
	// Destination step, beside the notice promising decryption runs first.
	step := readView(t, "restore", "DestinationStep.tsx")
	if !strings.Contains(step, "<SnapshotKeyFields") {
		t.Error("the restore flow renders no key input; an encrypted snapshot cannot be restored without one")
	}
}

// Both screens that open a snapshot ask for the key the same way, through
// components/SnapshotKeyFields.tsx. They ask in different containers — a modal
// on the way into the inspector, a field on the restore step that promises
// decryption runs first — and the container is allowed to differ. The question
// is not: an operator who learns it on one screen should recognise it on the
// other.
func TestSnapshotKey_IsAskedForInOnePlace(t *testing.T) {
	asks := map[string][2]string{
		"the restore wizard": {"restore", "DestinationStep.tsx"},
		"the inspector":      {"inspector", "KeyPrompt.tsx"},
	}
	for screen, where := range asks {
		src := readView(t, where[0], where[1])
		if !strings.Contains(src, `from "../../components/SnapshotKeyFields"`) {
			t.Errorf("%s does not use the shared key fields", screen)
		}
		if strings.Contains(src, "AGE-SECRET-KEY-1") {
			t.Errorf("%s spells out the key inputs itself; "+
				"components/SnapshotKeyFields.tsx is where that lives", screen)
		}
	}
}

// readView reads one file out of a page folder under frontend/src/pages.
func readView(t *testing.T, page, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "pages", page, name))
	if err != nil {
		t.Fatalf("the %s/%s view could not be read: %v", page, name, err)
	}
	return string(b)
}

// The restore wizard gates its Next button on whether a key is needed, and it
// cannot work that out for itself: which keys exist is the engine's business.
// It asked the Keys list and counted the ones stored on this machine, which is
// the wrong number — it misses the key the operator typed into the inspector a
// moment earlier, which is exactly the case that made the prompt indefensible.
func TestRestoreWizard_AsksTheEngineWhatItCanTry(t *testing.T) {
	src := readView(t, "restore", "RestorePage.tsx")

	if !strings.Contains(src, "KeysAPI.availability(") {
		t.Error("the restore wizard does not ask what an open would have to work with")
	}
	if strings.Contains(src, "KeysAPI.list(") {
		t.Error("the restore wizard counts stored keys itself; that number misses the keys typed this session")
	}
}

// "Keep encryption on" is an answer to the question the dialog asked, not a way
// out of it. Cancelling used to leave the wizard in the state neither button
// offered — encryption off, unconfirmed, and blocked on a confirmation the
// operator had just declined to give.
func TestCaptureWizard_DecliningToDeclineTurnsEncryptionBackOn(t *testing.T) {
	src := readView(t, "capture", "OptionsStep.tsx")

	if !strings.Contains(src, "onCancel:") {
		t.Fatal("the decline dialog has no cancel handler, so dismissing it decides nothing")
	}
	// The old attempt: a zero-delay timer that ran while the modal it was
	// checking for was still on screen, so it never fired.
	if strings.Contains(src, "document.querySelector(\".modal-backdrop\")") {
		t.Error("the wizard infers the dialog's outcome from the DOM; the modal reports it")
	}
}
