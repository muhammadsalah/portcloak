package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/obs"
)

// relocatableEngine is a PortCloak whose home was resolved the way a real
// launch resolves it — from the user's home folder rather than from
// PORTCLOAK_HOME, which is pinned and refuses to move for exactly that reason.
func relocatableEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(userHome, "config"))
	t.Setenv("PORTCLOAK_HOME", "")

	eng, err := NewEngine("test")
	if err != nil {
		t.Fatalf("an engine could not be built: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng, userHome
}

// The move has to take the operator's configuration with it and leave the
// running application reading from the new folder — not from the old path it
// was holding when the screen opened.
func TestRelocate_TakesTheConfigurationAndTheRunningEngineWithIt(t *testing.T) {
	eng, userHome := relocatableEngine(t)

	if err := eng.Config.AddStorage(config.Storage{
		Name: "archive", Kind: config.StoreDisk, Folder: t.TempDir(),
	}); err != nil {
		t.Fatalf("the storage could not be added: %v", err)
	}
	before := eng.Home().Root

	dst := filepath.Join(userHome, "elsewhere", "portcloak")
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := eng.Relocate(dst); err != nil {
		t.Fatalf("the folder could not be moved: %v", err)
	}

	if got := eng.Home().Root; got != dst {
		t.Errorf("the engine is still reading from %s after moving to %s", got, dst)
	}
	if _, err := os.Stat(before); !os.IsNotExist(err) {
		t.Errorf("%s is still there; a move that leaves config.yaml behind leaves a second PortCloak to be found later", before)
	}
	if _, err := os.Stat(filepath.Join(dst, "config.yaml")); err != nil {
		t.Fatalf("config.yaml did not arrive at the new folder: %v", err)
	}
	if _, ok := eng.Config.Config().StorageByName("archive"); !ok {
		t.Error("the storage definition did not survive the move")
	}

	// Writing after the move has to land in the new folder. A store still
	// holding the old path would recreate the tree it was just moved out of.
	if err := eng.Config.AddEnvironment(config.Environment{
		Name: "prod", Kind: config.EnvLocal, ServerFolder: t.TempDir(),
	}); err != nil {
		t.Fatalf("the configuration could not be written after the move: %v", err)
	}
	if _, err := os.Stat(before); !os.IsNotExist(err) {
		t.Errorf("a write after the move recreated %s", before)
	}
	if body, err := os.ReadFile(filepath.Join(dst, "config.yaml")); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(body), "prod") {
		t.Error("the environment added after the move is not in the moved config.yaml")
	}

	// The audit log follows too, which matters because the move itself is an
	// entry in it.
	if err := eng.Audit.Record(obs.AuditEntry{Action: obs.ActionHomeMoved, Outcome: "moved"}); err != nil {
		t.Fatalf("the audit log could not be written after the move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "audit.log")); err != nil {
		t.Errorf("the audit log did not follow the folder: %v", err)
	}

	// And the next launch has to find it, which is the whole point of the
	// pointer file living outside the tree that moved.
	loc, err := config.Locate()
	if err != nil {
		t.Fatal(err)
	}
	if loc.Source != config.HomeChosen || loc.Home.Root != dst {
		t.Errorf("the next launch would look in %s (%s), not %s", loc.Home.Root, loc.Source, dst)
	}
}

// Moving back to the default has to clear the pointer as well as the files, or
// a machine that looks reset is still carrying a note about a folder that is
// no longer special.
func TestUseDefaultLocation_ForgetsTheChoiceAsWellAsMovingTheFiles(t *testing.T) {
	eng, userHome := relocatableEngine(t)
	def := eng.Home().Root

	if err := eng.Relocate(filepath.Join(userHome, "elsewhere")); err != nil {
		t.Fatalf("the folder could not be moved: %v", err)
	}
	if err := eng.UseDefaultLocation(); err != nil {
		t.Fatalf("the folder could not be moved back: %v", err)
	}

	if got := eng.Home().Root; got != def {
		t.Errorf("the engine is reading from %s, not the default %s", got, def)
	}
	loc, err := config.Locate()
	if err != nil {
		t.Fatal(err)
	}
	if loc.Source != config.HomeDefault {
		t.Errorf("the choice was not forgotten: the location still resolves as %s", loc.Source)
	}
	if _, err := os.Stat(loc.Pointer); !os.IsNotExist(err) {
		t.Errorf("%s is still there after going back to the default", loc.Pointer)
	}
}

// A job in flight holds paths under the old root for its whole life. Moving the
// folder out from under it would strand its checkpoint, so the refusal has to
// come before anything moves — and has to say what to do about it.
func TestRelocate_RefusesWhileAJobIsInFlight(t *testing.T) {
	eng, userHome := relocatableEngine(t)
	before := eng.Home().Root

	if err := eng.Jobs.Save(&config.Job{
		ID: "job-1", Kind: config.JobCapture, State: config.JobRunning,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("the job could not be written: %v", err)
	}

	err := eng.Relocate(filepath.Join(userHome, "elsewhere"))
	if err == nil {
		t.Fatal("the folder was moved with a job running")
	}
	if !strings.Contains(err.Error(), "Activity") {
		t.Errorf("the refusal does not say where to go: %v", err)
	}
	if eng.Home().Root != before {
		t.Error("the engine moved anyway")
	}
	if _, err := os.Stat(filepath.Join(userHome, "elsewhere")); !os.IsNotExist(err) {
		t.Error("a refused move still created the destination")
	}
}

// PORTCLOAK_HOME is set outside the application and wins over anything chosen
// in it. A Settings screen that offered to move the folder anyway would be
// offering something it cannot deliver.
func TestRelocate_RefusesWhenTheEnvironmentDecides(t *testing.T) {
	pinned := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PORTCLOAK_HOME", pinned)

	eng, err := NewEngine("test")
	if err != nil {
		t.Fatalf("an engine could not be built: %v", err)
	}
	defer eng.Close() //nolint:errcheck

	err = eng.Relocate(filepath.Join(t.TempDir(), "elsewhere"))
	if err == nil {
		t.Fatal("the folder was moved out from under PORTCLOAK_HOME")
	}
	if !strings.Contains(err.Error(), "PORTCLOAK_HOME") {
		t.Errorf("the refusal does not name the variable that is in charge: %v", err)
	}
	if eng.Home().Root != pinned {
		t.Error("the engine moved anyway")
	}
}
