// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/inspect"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/store"
)

// SnapshotController is the library and the storage browser.
type SnapshotController struct{ eng *Engine }

// NewSnapshotController binds the library.
func NewSnapshotController(eng *Engine) *SnapshotController { return &SnapshotController{eng: eng} }

// ServiceName is the name internal/desktop logs this service under. It is
// not the address a bound method is called by — see the comment on
// controllers there, which is where reading it as one caused real damage.
func (s *SnapshotController) ServiceName() string { return "SnapshotController" }

// LibraryView is the Tier 0 listing.
type LibraryView struct {
	Entries  []inspect.Entry         `json:"entries"`
	Storages []inspect.StorageStatus `json:"storages"`
	Summary  string                  `json:"summary"`
	Realms   []string                `json:"realms"`
	// Open lists the snapshots with an inspection session on this machine.
	//
	// An open snapshot has a decrypted working directory and, above the
	// threshold, an index file — realm material sitting unencrypted on disk
	// until it is closed. The list is what lets the library say which ones
	// those are and offer to close them, rather than leaving an operator to
	// remember which screens they visited.
	Open []string `json:"open"`
	// Environments names the environments that are still configured.
	//
	// A snapshot records the environment it was captured from, and that
	// environment can since have been renamed or removed — the snapshot is a
	// record of something that happened, not a foreign key. The list is what
	// lets the screen offer a link to the ones that are still there and say
	// plainly that the others are gone, rather than offering a link that leads
	// nowhere.
	Environments []string `json:"environments"`
	// FirstRun is what an empty library shows instead of an empty grid.
	FirstRun *FirstRun `json:"firstRun,omitempty"`
}

// Library lists every snapshot across every configured storage, with no key.
func (s *SnapshotController) Library() (res LibraryView) {
	defer func() { res = lists(res) }()
	cfg := s.eng.Config.Config()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	lib := inspect.BuildLibrary(ctx, cfg.Storage, s.eng.storeFor)
	view := LibraryView{
		Entries:  lib.Entries,
		Storages: lib.Storages,
		Summary:  lib.Summary(),
	}

	for id := range s.eng.OpenSessionIDs() {
		view.Open = append(view.Open, id)
	}
	sort.Strings(view.Open)

	for _, env := range cfg.Environments {
		view.Environments = append(view.Environments, env.Name)
	}
	sort.Strings(view.Environments)

	seen := map[string]bool{}
	for _, e := range lib.Entries {
		if e.Realm != "" && !seen[e.Realm] {
			seen[e.Realm] = true
			view.Realms = append(view.Realms, e.Realm)
		}
	}
	sort.Strings(view.Realms)

	if len(lib.Entries) == 0 {
		view.FirstRun = s.eng.firstRun(
			"No snapshots yet",
			"PortCloak needs two things before it can capture a realm: where Keycloak runs, and where the snapshot should go.",
		)
	}
	return view
}

// BrowseResult is what a storage actually holds, including what PortCloak did
// not write.
type BrowseResult struct {
	Storage   string          `json:"storage"`
	Snapshots []inspect.Entry `json:"snapshots"`
	// Foreign objects are shown rather than hidden. An operator debugging a
	// misconfigured prefix needs to see what is really there, and silently
	// filtering is how a prefix typo goes unnoticed for a month.
	Foreign []store.ObjectInfo    `json:"foreign"`
	Status  inspect.StorageStatus `json:"status"`
	Note    string                `json:"note"`
	Failure *Failure              `json:"failure,omitempty"`
}

// Browse lists one storage's contents.
func (s *SnapshotController) Browse(name string) (res BrowseResult) {
	defer func() { res = lists(res) }()
	cfg := s.eng.Config.Config()
	st, ok := cfg.StorageByName(name)
	if !ok {
		return BrowseResult{Failure: Fail(config.ErrNotFound)}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	lib := inspect.BuildLibrary(ctx, []config.Storage{st}, s.eng.storeFor)
	out := BrowseResult{Storage: name, Snapshots: lib.Entries, Foreign: lib.Foreign[name]}
	if len(lib.Storages) > 0 {
		out.Status = lib.Storages[0]
	}
	switch {
	case !out.Status.Reachable:
		out.Note = "This storage could not be read, so nothing below is a complete picture."
	case len(out.Foreign) > 0:
		out.Note = fmt.Sprintf("%d object%s here were not written by PortCloak. They are shown rather than hidden, because a mistyped prefix looks exactly like an empty one.",
			len(out.Foreign), plural(len(out.Foreign)))
	default:
		out.Note = "Everything under this prefix was written by PortCloak."
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// DeleteResult reports what a deletion actually removed.
type DeleteResult struct {
	Removed []string `json:"removed"`
	Note    string   `json:"note"`
	Failure *Failure `json:"failure,omitempty"`
}

// Delete removes a snapshot's bundle and both sidecars as one operation, and
// reports what it actually removed rather than assuming.
func (s *SnapshotController) Delete(storageName, bundleKey string) (res DeleteResult) {
	defer func() { res = lists(res) }()
	cfg := s.eng.Config.Config()
	st, ok := cfg.StorageByName(storageName)
	if !ok {
		return DeleteResult{Failure: Fail(config.ErrNotFound)}
	}
	blobs, err := s.eng.storeFor(st)
	if err != nil {
		return DeleteResult{Failure: Fail(err)}
	}
	defer blobs.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	base := strings.TrimSuffix(bundleKey, store.BundleExt)
	var removed []string
	for _, key := range []string{
		base + store.BundleExt, base + store.ManifestExt, base + store.DigestExt,
	} {
		if _, statErr := blobs.Stat(ctx, key); statErr != nil {
			continue
		}
		if err := blobs.Delete(ctx, key); err != nil {
			return DeleteResult{Removed: removed, Failure: Fail(err)}
		}
		removed = append(removed, key)
	}

	_ = s.eng.Audit.Record(obs.AuditEntry{
		Action: obs.ActionSnapshotDelete, Outcome: "deleted",
		Storage: storageName, Detail: strings.Join(removed, ", "),
	})

	note := fmt.Sprintf("Removed %d object%s.", len(removed), plural(len(removed)))
	if len(removed) < 3 {
		note += " Some of this snapshot's objects were already gone."
	}
	return DeleteResult{Removed: removed, Note: note}
}
