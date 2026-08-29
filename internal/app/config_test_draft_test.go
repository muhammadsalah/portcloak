// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"path/filepath"
	"testing"

	"portcloak/internal/engine/config"
)

// Testing a definition is only useful before it is saved. A test that read the
// saved copy could tell an operator whether what they already committed works,
// and they are asking precisely because they have not committed it.
//
// This drives the editor's Test button, which sent a name and therefore probed
// whatever was on disk under that name — nothing at all, for a definition being
// entered for the first time.
func TestTestStorageDraft_ProbesTheDraftRatherThanTheSavedCopy(t *testing.T) {
	eng := emptyEngine(t)
	c := NewConfigController(eng)

	folder := filepath.Join(t.TempDir(), "never-saved")
	res := c.TestStorageDraft(config.Storage{
		Name: "not in the config at all", Kind: config.StoreDisk, Folder: folder,
	}, "")

	if res.Failure != nil {
		t.Fatalf("an unsaved draft could not be tested: %s", res.Failure.Message)
	}
	if !res.OK {
		t.Fatalf("a writable folder probed as not OK: %+v", res.Reach)
	}
}

// Nothing is written by a test, least of all against a definition that does not
// exist. A stamp recorded here would be a probe result for a name the config
// does not hold.
func TestTestStorageDraft_RecordsNothingForAnUnsavedDraft(t *testing.T) {
	eng := emptyEngine(t)
	c := NewConfigController(eng)

	before := len(eng.Config.Config().Storage)
	c.TestStorageDraft(config.Storage{
		Name: "phantom", Kind: config.StoreDisk, Folder: t.TempDir(),
	}, "")

	if got := len(eng.Config.Config().Storage); got != before {
		t.Fatalf("testing a draft changed the saved configuration: %d -> %d", before, got)
	}
	if _, found := eng.Config.Config().StorageByName("phantom"); found {
		t.Error("testing a draft created the definition it was testing")
	}
}

// A test is not a gate. A definition whose target is down this minute is not a
// definition that is wrong, so a failing test must leave saving available.
func TestTestStorageDraft_DoesNotGateSaving(t *testing.T) {
	eng := emptyEngine(t)
	c := NewConfigController(eng)

	// A folder under a path that does not exist and cannot be made.
	st := config.Storage{
		Name: "later", Kind: config.StoreDisk,
		Folder: filepath.Join("/proc", "definitely-not-writable", "snapshots"),
	}
	if res := c.TestStorageDraft(st, ""); res.OK {
		t.Fatal("an unusable folder probed as OK, so this proves nothing")
	}
	if failure := c.SaveStorage("", st, ""); failure != nil {
		t.Fatalf("a definition that failed its test could not be saved: %s", failure.Message)
	}
	if _, found := eng.Config.Config().StorageByName("later"); !found {
		t.Error("the definition was not saved")
	}
}
