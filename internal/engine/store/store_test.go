package store_test

import (
	"testing"
	"time"

	"portcloak/internal/engine/store"
)

func TestLayout_KeysAreRootedAtThePrefixAndPartitionedByRealm(t *testing.T) {
	l := store.NewLayout("portcloak/")
	at := time.Date(2026, 8, 27, 9, 14, 0, 0, time.UTC)

	if got, want := l.BundleKey("acme", at, "01HZY3"), "portcloak/acme/2026-08-27T0914-01HZY3.pck"; got != want {
		t.Errorf("BundleKey = %q, want %q", got, want)
	}
	if got, want := l.ManifestKey("acme", at, "01HZY3"), "portcloak/acme/2026-08-27T0914-01HZY3.manifest.json"; got != want {
		t.Errorf("ManifestKey = %q, want %q", got, want)
	}
	if got, want := l.DigestKey("acme", at, "01HZY3"), "portcloak/acme/2026-08-27T0914-01HZY3.sha256"; got != want {
		t.Errorf("DigestKey = %q, want %q", got, want)
	}
	// The realm prefix is what lets listing and access control both be scoped
	// per realm — possible only because a snapshot holds exactly one.
	if got, want := l.RealmPrefix("acme"), "portcloak/acme/"; got != want {
		t.Errorf("RealmPrefix = %q, want %q", got, want)
	}
}

func TestLayout_EmptyPrefixIsRootLevel(t *testing.T) {
	l := store.NewLayout("")
	at := time.Date(2026, 8, 27, 9, 14, 0, 0, time.UTC)
	if got, want := l.BundleKey("acme", at, "x"), "acme/2026-08-27T0914-x.pck"; got != want {
		t.Errorf("BundleKey = %q, want %q", got, want)
	}
}

// Foreign objects are shown rather than hidden. Silently filtering them is how
// a prefix typo goes unnoticed for a month.
func TestGroup_SeparatesSnapshotsFromForeignObjects(t *testing.T) {
	l := store.NewLayout("portcloak")
	objects := []store.ObjectInfo{
		{Key: "portcloak/acme/2026-08-27T0914-a.pck", Size: 100},
		{Key: "portcloak/acme/2026-08-27T0914-a.manifest.json", Size: 10},
		{Key: "portcloak/acme/2026-08-27T0914-a.sha256", Size: 64},
		{Key: "portcloak/acme/2026-08-26T2203-b.pck", Size: 200},
		{Key: "portcloak/acme/notes.txt", Size: 5},
		{Key: "portcloak/README", Size: 1},
	}
	snapshots, foreign := store.Group(l, objects)

	if len(snapshots) != 2 {
		t.Fatalf("got %d snapshots, want 2: %+v", len(snapshots), snapshots)
	}
	// Newest first, which is the order the library reads in.
	if snapshots[0].SnapshotID != "a" {
		t.Errorf("snapshots are not newest-first: %+v", snapshots)
	}
	if snapshots[0].Realm != "acme" {
		t.Errorf("realm parsed as %q", snapshots[0].Realm)
	}
	if snapshots[0].Manifest == nil || snapshots[0].Digest == nil || snapshots[0].Bundle == nil {
		t.Errorf("the triplet did not group: %+v", snapshots[0])
	}
	// A bundle whose sidecar is missing still lists, marked as needing a deeper
	// read, rather than vanishing.
	if !snapshots[1].Complete() {
		t.Error("a bundle with no sidecar should still be listed")
	}
	if snapshots[1].Manifest != nil {
		t.Error("snapshot b has no manifest sidecar")
	}

	if len(foreign) != 2 {
		t.Fatalf("got %d foreign objects, want 2: %+v", len(foreign), foreign)
	}
}

func TestGroup_ParsesTheCaptureTimeOutOfTheKey(t *testing.T) {
	l := store.NewLayout("")
	snapshots, _ := store.Group(l, []store.ObjectInfo{
		{Key: "acme/2026-08-27T0914-01HZY3.pck"},
	})
	if len(snapshots) != 1 {
		t.Fatal("expected one snapshot")
	}
	want := time.Date(2026, 8, 27, 9, 14, 0, 0, time.UTC)
	if !snapshots[0].CreatedAt.Equal(want) {
		t.Errorf("capture time parsed as %v, want %v", snapshots[0].CreatedAt, want)
	}
}
