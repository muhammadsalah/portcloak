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

// ServiceName is what the Wails binding layer calls this.
func (s *SnapshotController) ServiceName() string { return "SnapshotController" }

// LibraryView is the Tier 0 listing.
type LibraryView struct {
	Entries  []inspect.Entry         `json:"entries"`
	Storages []inspect.StorageStatus `json:"storages"`
	Summary  string                  `json:"summary"`
	Realms   []string                `json:"realms"`
	// FirstRun is what an empty library shows instead of an empty grid.
	FirstRun *FirstRun `json:"firstRun,omitempty"`
}

// FirstRun names the two things needed before a capture is possible.
type FirstRun struct {
	Heading          string `json:"heading"`
	Body             string `json:"body"`
	NeedsEnvironment bool   `json:"needsEnvironment"`
	NeedsStorage     bool   `json:"needsStorage"`
	EnvironmentBody  string `json:"environmentBody"`
	StorageBody      string `json:"storageBody"`
	NoAccountHeading string `json:"noAccountHeading"`
	NoAccountBody    string `json:"noAccountBody"`
	ConfigFile       string `json:"configFile"`
}

// Library lists every snapshot across every configured storage, with no key.
func (s *SnapshotController) Library() LibraryView {
	cfg := s.eng.Config.Config()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	lib := inspect.BuildLibrary(ctx, cfg.Storage, s.eng.storeFor)
	view := LibraryView{
		Entries:  lib.Entries,
		Storages: lib.Storages,
		Summary:  lib.Summary(),
	}

	seen := map[string]bool{}
	for _, e := range lib.Entries {
		if e.Realm != "" && !seen[e.Realm] {
			seen[e.Realm] = true
			view.Realms = append(view.Realms, e.Realm)
		}
	}
	sort.Strings(view.Realms)

	if len(lib.Entries) == 0 {
		view.FirstRun = &FirstRun{
			Heading:          "No snapshots yet",
			Body:             "PortCloak needs two things before it can capture a realm: where Keycloak runs, and where the snapshot should go.",
			NeedsEnvironment: len(cfg.Environments) == 0,
			NeedsStorage:     len(cfg.Storage) == 0,
			EnvironmentBody:  "A Keycloak you can reach — on this machine, over SSH, in Docker, or in a Kubernetes namespace. PortCloak reads it; it never restarts or reconfigures the instance serving your logins.",
			StorageBody:      "A folder for the snapshots — on disk, on a host over SSH, in an S3 bucket, or in Azure Blob. You can mark one as requiring encryption, and nothing plaintext will ever be written there.",
			NoAccountHeading: "There is no account and no sign-in",
			NoAccountBody:    "PortCloak is a local tool. Everything it knows lives in plain files in your home folder — config.yaml holds your environments and storage, and every credential goes to this machine's keychain, referenced by handle. You can read that file, diff it, and commit it without leaking anything.",
			ConfigFile:       s.eng.Home.ConfigFile(),
		}
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
func (s *SnapshotController) Browse(name string) BrowseResult {
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
func (s *SnapshotController) Delete(storageName, bundleKey string) DeleteResult {
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
