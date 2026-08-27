package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The home folder is the one thing about a running PortCloak that can move, and
// a move that goes half way strands the operator's environments, keys and job
// checkpoints across two folders with the app bound to neither. These tests
// hold the two halves that stop that: nothing starts until the destination has
// been refused for every reason it can be, and the move itself carries the
// whole tree or leaves the original alone.

func TestCheckDestination_RefusesBeforeAnythingMoves(t *testing.T) {
	root := t.TempDir()
	home := Home{Root: filepath.Join(root, "portcloak")}
	if err := os.MkdirAll(home.Root, 0o700); err != nil {
		t.Fatal(err)
	}

	occupied := filepath.Join(root, "occupied")
	if err := os.MkdirAll(filepath.Join(occupied, "someone-elses-work"), 0o700); err != nil {
		t.Fatal(err)
	}
	aFile := filepath.Join(root, "a-file")
	if err := os.WriteFile(aFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, dst, wants string
	}{
		{"empty", "  ", "Give the folder"},
		{"relative", filepath.Join("relative", "path"), "relative path"},
		{"itself", home.Root, "already keeping its files"},
		{"inside itself", filepath.Join(home.Root, "deeper"), "inside the folder being moved"},
		{"its own parent", root, "already contains PortCloak's folder"},
		{"a file", aFile, "is a file, not a folder"},
		{"not empty", occupied, "is not empty"},
		{"parent missing", filepath.Join(root, "nope", "deeper"), "does not exist"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckDestination(home, tc.dst)
			if err == nil {
				t.Fatalf("%s was accepted as a destination", tc.dst)
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("the refusal does not say why: got %q, wanted it to mention %q", err, tc.wants)
			}
		})
	}

	// And the one that must be accepted: a folder that is not there yet, beside
	// the current one.
	if err := CheckDestination(home, filepath.Join(root, "elsewhere")); err != nil {
		t.Errorf("a fresh folder beside the current one was refused: %v", err)
	}
}

func TestMoveTree_CarriesEverythingAndLeavesNothing(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "from")
	dst := filepath.Join(root, "to")

	home := Home{Root: src}
	if err := home.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(home.JobFile("job-1"), []byte(`{"id":"job-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(home.AuditFile(), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := MoveTree(src, dst); err != nil {
		t.Fatalf("the tree could not be moved: %v", err)
	}

	moved := Home{Root: dst}
	for _, path := range []string{moved.ConfigFile(), moved.JobFile("job-1"), moved.AuditFile()} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s did not arrive: %v", path, err)
		}
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("the original folder is still there; a move that leaves a copy of config.yaml behind leaves a second PortCloak to be found later")
	}
}

// The rename that MoveTree tries first cannot cross a filesystem, which is
// exactly the case an operator has in mind when they move the folder onto an
// external disk. The copy underneath it is therefore the path that matters, and
// it is the one a same-filesystem test never reaches.
func TestCopyTree_PreservesTheModesThatKeepSecretsOut(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "from")
	dst := filepath.Join(root, "to")

	home := Home{Root: src}
	if err := home.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("the tree could not be copied: %v", err)
	}

	info, err := os.Stat(filepath.Join(dst, "jobs"))
	if err != nil {
		t.Fatalf("the jobs folder did not arrive: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("the jobs folder arrived as %o, not 0700 — job checkpoints name environments and realms", perm)
	}
	info, err = os.Stat(filepath.Join(dst, "config.yaml"))
	if err != nil {
		t.Fatalf("config.yaml did not arrive: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config.yaml arrived as %o, not 0600", perm)
	}
}

// The resolution order is what decides which folder the next launch reads, so
// each step of it is worth stating.
func TestLocate_EnvironmentBeatsTheChosenFolderBeatsTheDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("PORTCLOAK_HOME", "")

	loc, err := Locate()
	if err != nil {
		t.Fatal(err)
	}
	if loc.Source != HomeDefault || loc.Home.Root != filepath.Join(home, ".portcloak") {
		t.Fatalf("with nothing set, the default should apply; got %s at %s", loc.Source, loc.Home.Root)
	}

	chosen := filepath.Join(home, "chosen")
	if err := os.MkdirAll(chosen, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WritePointer(chosen); err != nil {
		t.Fatalf("the pointer could not be written: %v", err)
	}
	if loc, err = Locate(); err != nil {
		t.Fatal(err)
	} else if loc.Source != HomeChosen || loc.Home.Root != chosen {
		t.Fatalf("a chosen folder should beat the default; got %s at %s", loc.Source, loc.Home.Root)
	}

	pinned := filepath.Join(home, "pinned")
	t.Setenv("PORTCLOAK_HOME", pinned)
	if loc, err = Locate(); err != nil {
		t.Fatal(err)
	} else if loc.Source != HomePinned || loc.Home.Root != pinned {
		t.Fatalf("PORTCLOAK_HOME should beat a chosen folder; got %s at %s", loc.Source, loc.Home.Root)
	}

	// A chosen folder that has since been deleted falls back rather than
	// refusing to start. Losing the way back into a working PortCloak because a
	// folder was moved in Finder would be a poor trade for strictness.
	t.Setenv("PORTCLOAK_HOME", "")
	if err := os.RemoveAll(chosen); err != nil {
		t.Fatal(err)
	}
	if loc, err = Locate(); err != nil {
		t.Fatal(err)
	} else if loc.Source != HomeDefault {
		t.Fatalf("a pointer to a folder that is gone should fall back to the default; got %s", loc.Source)
	}
}
