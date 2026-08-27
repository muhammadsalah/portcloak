// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package inspect turns a snapshot from an opaque file into a queryable
// artifact.
//
// Inspection is tiered so cheap questions stay cheap and keys are needed only
// when genuinely required:
//
//	Tier 0 — the library, served from the secret-free sidecar. No key at all.
//	Tier 1 — one snapshot's detail. Download, decrypt and verify once.
//	Tier 2 — the user index. Built on open, destroyed on close.
//
// Tier 0 is what makes the library usable: an operator can survey every
// snapshot across every backend while holding nothing.
package inspect

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/manifest"
	"portcloak/internal/engine/store"
)

// Entry is one row of the snapshot library.
type Entry struct {
	SnapshotID      string    `json:"snapshotId"`
	Realm           string    `json:"realm"`
	CreatedAt       time.Time `json:"createdAt"`
	Storage         string    `json:"storage"`
	BundleKey       string    `json:"bundleKey"`
	Bytes           int64     `json:"bytes"`
	Environment     string    `json:"environment,omitempty"`
	ExecutionMode   string    `json:"executionMode,omitempty"`
	KeycloakVersion string    `json:"keycloakVersion,omitempty"`
	Users           int       `json:"users"`
	Clients         int       `json:"clients"`
	Verdict         string    `json:"verdict"`
	Encrypted       bool      `json:"encrypted"`
	EncryptionMode  string    `json:"encryptionMode,omitempty"`
	SecretCount     int       `json:"secretCount"`
	DependencyCount int       `json:"dependencyCount"`
	TokenContinuity bool      `json:"tokenContinuity"`
	// Warning is the unmissable label an unencrypted bundle carries everywhere
	// it appears.
	Warning string `json:"warning,omitempty"`
	// MetadataReadable is false when the sidecar is missing or unreadable. Such
	// an entry is still listed — it can be opened, which verifies properly —
	// rather than vanishing from the library.
	MetadataReadable bool   `json:"metadataReadable"`
	MetadataNote     string `json:"metadataNote,omitempty"`
}

// StorageStatus reports whether one backend could be read.
//
// A storage that could not be reached is shown as unreachable rather than
// having its snapshots quietly omitted: the list is never silently short.
type StorageStatus struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Reachable bool   `json:"reachable"`
	Snapshots int    `json:"snapshots"`
	Error     string `json:"error,omitempty"`
}

// Library is the whole Tier 0 view.
type Library struct {
	Entries  []Entry         `json:"entries"`
	Storages []StorageStatus `json:"storages"`
	// Foreign lists objects PortCloak did not write, per storage. They are
	// shown rather than hidden, because an operator debugging a misconfigured
	// prefix needs to see what is really there.
	Foreign map[string][]store.ObjectInfo `json:"foreign,omitempty"`
}

// StoreOpener builds a BlobStore for a storage definition.
type StoreOpener func(st config.Storage) (store.BlobStore, error)

// BuildLibrary lists snapshots across every configured storage, using only the
// secret-free sidecars. No key is requested and none is needed.
func BuildLibrary(ctx context.Context, storages []config.Storage, open StoreOpener) Library {
	lib := Library{Foreign: map[string][]store.ObjectInfo{}}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, st := range storages {
		wg.Add(1)
		go func(st config.Storage) {
			defer wg.Done()
			status := StorageStatus{Name: st.Name, Kind: string(st.Kind)}

			blobs, err := open(st)
			if err != nil {
				status.Error = err.Error()
				mu.Lock()
				lib.Storages = append(lib.Storages, status)
				mu.Unlock()
				return
			}
			defer blobs.Close() //nolint:errcheck

			layout := layoutFor(st)
			objects, err := blobs.List(ctx, layout.Root())
			if err != nil {
				status.Error = err.Error()
				mu.Lock()
				lib.Storages = append(lib.Storages, status)
				mu.Unlock()
				return
			}
			status.Reachable = true

			triplets, foreign := store.Group(layout, objects)
			entries := make([]Entry, 0, len(triplets))
			for _, t := range triplets {
				if !t.Complete() {
					// A sidecar with no bundle is residue, not a snapshot.
					continue
				}
				entries = append(entries, readEntry(ctx, blobs, st, t))
			}
			status.Snapshots = len(entries)

			mu.Lock()
			lib.Entries = append(lib.Entries, entries...)
			lib.Storages = append(lib.Storages, status)
			if len(foreign) > 0 {
				lib.Foreign[st.Name] = foreign
			}
			mu.Unlock()
		}(st)
	}
	wg.Wait()

	sort.Slice(lib.Entries, func(i, j int) bool { return lib.Entries[i].CreatedAt.After(lib.Entries[j].CreatedAt) })
	sort.Slice(lib.Storages, func(i, j int) bool { return lib.Storages[i].Name < lib.Storages[j].Name })
	return lib
}

func layoutFor(st config.Storage) store.Layout {
	switch st.Kind {
	case config.StoreDisk, config.StoreSSH:
		// Disk and SSH are already rooted at the configured folder, so the
		// prefix is not repeated inside it.
		return store.NewLayout("")
	default:
		return store.NewLayout(st.Prefix)
	}
}

// readEntry turns one triplet into a library row, reading only the sidecar.
func readEntry(ctx context.Context, blobs store.BlobStore, st config.Storage, t store.Triplet) Entry {
	e := Entry{
		SnapshotID: t.SnapshotID,
		Realm:      t.Realm,
		CreatedAt:  t.CreatedAt,
		Storage:    st.Name,
		BundleKey:  t.Bundle.Key,
		Bytes:      t.Bundle.Size,
	}
	if t.Manifest == nil {
		e.MetadataNote = "This snapshot has no sidecar manifest, so its contents are unknown until it is opened."
		return e
	}

	var buf strings.Builder
	if _, err := blobs.Get(ctx, t.Manifest.Key, &buf, store.GetOptions{}); err != nil {
		e.MetadataNote = "The sidecar manifest could not be read: " + err.Error()
		return e
	}
	var sc manifest.Sidecar
	if err := json.Unmarshal([]byte(buf.String()), &sc); err != nil {
		e.MetadataNote = "The sidecar manifest is not readable, so this snapshot needs a deeper read."
		return e
	}

	e.MetadataReadable = true
	e.Realm = sc.Realm
	e.Users = sc.Counts.Users
	e.Clients = sc.Counts.Clients
	e.Verdict = sc.Verdict
	e.Encrypted = sc.Encrypted
	e.EncryptionMode = sc.EncryptionMode
	e.SecretCount = sc.SecretCount
	e.DependencyCount = sc.DependencyCount
	e.TokenContinuity = sc.TokenContinuity
	e.Environment = sc.Source.EnvironmentName
	e.ExecutionMode = sc.Source.ExecutionMode
	e.KeycloakVersion = sc.KeycloakVersion
	if sc.CreatedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, sc.CreatedAt); err == nil {
			e.CreatedAt = parsed
		}
	}
	if !sc.Encrypted {
		e.Warning = unencryptedWarning
	}
	return e
}

// unencryptedWarning is the label an unencrypted bundle carries in the library,
// in the manifest and in the completeness report — all three, because an
// operator must never be able to say afterwards that they did not realise.
const unencryptedWarning = "Unencrypted — this bundle holds unmasked client secrets and private signing keys in the clear."

// Filter narrows the library.
type Filter struct {
	Realm     string
	Storage   string
	Verdict   string
	Encrypted *bool
	Query     string
}

// Apply filters library entries.
func (l Library) Apply(f Filter) []Entry {
	var out []Entry
	q := strings.ToLower(strings.TrimSpace(f.Query))
	for _, e := range l.Entries {
		if f.Realm != "" && e.Realm != f.Realm {
			continue
		}
		if f.Storage != "" && e.Storage != f.Storage {
			continue
		}
		if f.Verdict != "" && e.Verdict != f.Verdict {
			continue
		}
		if f.Encrypted != nil && e.Encrypted != *f.Encrypted {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Realm), q) &&
			!strings.Contains(strings.ToLower(e.SnapshotID), q) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Summary is the sentence under the library heading.
func (l Library) Summary() string {
	reachable := 0
	for _, s := range l.Storages {
		if s.Reachable {
			reachable++
		}
	}
	unreachable := len(l.Storages) - reachable

	s := fmt.Sprintf("%d snapshot%s across %d storage definition%s · this listing needs no decryption key",
		len(l.Entries), plural(len(l.Entries)), reachable, plural(reachable))
	if unreachable > 0 {
		s += fmt.Sprintf(" · %d storage definition%s could not be reached, so this list may be short",
			unreachable, plural(unreachable))
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
