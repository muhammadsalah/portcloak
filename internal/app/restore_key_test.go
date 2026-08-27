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
	src := readView(t, "restore.ts")

	// `passphrase: ""` and `identities: []`, however they are spaced.
	empty := regexp.MustCompile(`(?m)\b(passphrase|identities)\s*:\s*(""|''|\[\s*\])`)
	if m := empty.FindAllString(src, -1); len(m) > 0 {
		t.Errorf("the restore flow hardcodes an empty key %v; it has to carry what the operator entered, "+
			"both into the pre-flight open and into apply", m)
	}

	// And the field has to exist, or there is nothing to carry.
	if !strings.Contains(src, "keyFields(") {
		t.Error("the restore flow renders no key input; an encrypted snapshot cannot be restored without one")
	}
}

// Both screens that open a snapshot ask for the key the same way, through
// views/key.ts. They ask in different containers — a modal on the way into the
// inspector, a field on the restore step that promises decryption runs first —
// and the container is allowed to differ. The question is not: an operator who
// learns it on one screen should recognise it on the other.
func TestSnapshotKey_IsAskedForInOnePlace(t *testing.T) {
	for _, view := range []string{"restore.ts", "inspector.ts"} {
		src := readView(t, view)
		if !strings.Contains(src, `from "./key"`) {
			t.Errorf("%s does not use the shared key fields", view)
		}
		if strings.Contains(src, "AGE-SECRET-KEY-1") {
			t.Errorf("%s spells out the key inputs itself; views/key.ts is where that lives", view)
		}
	}
}

func readView(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "views", name))
	if err != nil {
		t.Fatalf("the %s view could not be read: %v", name, err)
	}
	return string(b)
}
