// Package store is the seam that makes backend choice a configuration setting.
//
// Every backend implements BlobStore; the three network backends also implement
// ResumableStore. The orchestrator writes and reads the sealed bundle through
// this one contract, so adding a backend is additive rather than surgical.
package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"
)

// ErrNotFound is returned when a key does not exist. It is a sentinel because
// callers routinely need to distinguish "absent" from "could not tell".
var ErrNotFound = errors.New("no object with that key")

// ObjectInfo describes one stored object.
type ObjectInfo struct {
	Key     string    `json:"key"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	ETag    string    `json:"etag,omitempty"`
}

// Access is the three-way result of a storage probe.
//
// Read-only is deliberately not collapsed into failure: browsing a snapshot
// library with a read-only credential is a legitimate configuration, and
// reporting it as broken would be wrong.
type Access string

const (
	AccessNone     Access = "unreachable"
	AccessReadOnly Access = "read-only"
	AccessWritable Access = "writable"
)

// Integrity names how a backend's stored digest is obtained.
type Integrity string

const (
	// IntegrityClientSide means PortCloak's own SHA-256 is the only check.
	IntegrityClientSide Integrity = "client-side sha-256"
	// IntegrityRemoteCommand means a remote shell computed the digest.
	IntegrityRemoteCommand Integrity = "remote sha256sum"
	// IntegrityServerSide means the backend returned its own checksum.
	IntegrityServerSide Integrity = "server-side checksum"
	// IntegrityReadBack means the object was re-read and hashed.
	IntegrityReadBack Integrity = "re-read and hash"
)

// Reach is what a storage probe found. Like a target probe it reports concrete
// facts rather than a tick.
type Reach struct {
	Access    Access        `json:"access"`
	Root      string        `json:"root"`
	Latency   time.Duration `json:"latency"`
	Resumable bool          `json:"resumable"`
	Integrity Integrity     `json:"integrity"`
	FreeBytes int64         `json:"freeBytes,omitempty"`
	// FailedStep names which part of the round trip failed, so a failure says
	// "listing the prefix" rather than wrapping an SDK error.
	FailedStep string `json:"failedStep,omitempty"`
	Detail     string `json:"detail,omitempty"`
	ProbedAt   time.Time `json:"probedAt"`
}

// OK reports whether the storage can be reached at all.
func (r Reach) OK() bool { return r.Access != AccessNone }

// PutOptions controls a write.
type PutOptions struct {
	// Size is the content length where it is known, which lets a backend pick
	// single-shot or multipart without buffering to find out.
	Size int64
	// Digest is the expected SHA-256, hex encoded, computed before the bundle
	// reached any backend. Corruption is therefore always caught on retrieval,
	// whatever the backend did.
	Digest string
	// Offset resumes a partial write. Backends that cannot resume ignore it and
	// report so through Reach.Resumable.
	Offset int64
	// Progress is called with bytes written so far.
	Progress func(written int64)
	// ContentType is advisory metadata.
	ContentType string
}

// PutResult is what a completed write produced.
type PutResult struct {
	Key     string
	Size    int64
	Digest  string
	ETag    string
	Resumed bool
}

// GetOptions controls a read.
type GetOptions struct {
	Offset   int64
	Progress func(read int64)
}

// GetResult is what a completed read produced.
type GetResult struct {
	Size   int64
	Digest string
}

// BlobStore is the contract every backend satisfies.
type BlobStore interface {
	// Probe resolves credentials, confirms the root exists, and establishes
	// whether writing is possible — without leaving a probe artifact behind,
	// even when the probe fails.
	Probe(ctx context.Context) (Reach, error)
	Stat(ctx context.Context, key string) (ObjectInfo, error)
	Put(ctx context.Context, key string, r io.Reader, opts PutOptions) (PutResult, error)
	Get(ctx context.Context, key string, w io.Writer, opts GetOptions) (GetResult, error)
	List(ctx context.Context, prefix string) ([]ObjectInfo, error)
	Delete(ctx context.Context, key string) error
	// Endpoint identifies the backend for the circuit breaker and for error
	// messages.
	Endpoint() string
	Close() error
}

// UploadID identifies a multipart or staged-block upload in progress.
type UploadID string

// PartETag is one completed part.
type PartETag struct {
	Number int    `json:"number"`
	ETag   string `json:"etag"`
	Size   int64  `json:"size"`
}

// ResumableStore adds the operations that make an interrupted upload resume
// from where it stopped rather than from the beginning.
type ResumableStore interface {
	BlobStore
	InitMultipart(ctx context.Context, key string) (UploadID, error)
	PutPart(ctx context.Context, id UploadID, key string, number int, r io.Reader, size int64) (PartETag, error)
	CompleteMultipart(ctx context.Context, id UploadID, key string, parts []PartETag) (PutResult, error)
	// AbortMultipart cleans up so a cancelled job does not leave billable
	// incomplete uploads behind.
	AbortMultipart(ctx context.Context, id UploadID, key string) error
	// ListParts re-establishes what the server already holds, which is what
	// makes resume work after PortCloak itself has been restarted.
	ListParts(ctx context.Context, id UploadID, key string) ([]PartETag, error)
	// PartSize is the chunk size this backend uses.
	PartSize() int64
}

// Layout builds the object keys for a snapshot, under a storage's configured
// folder or prefix.
//
// The realm sits in the path so it cleanly partitions the backend: listing and
// access control can both be scoped per realm, which is possible only because a
// snapshot holds exactly one.
type Layout struct {
	Prefix string
}

// NewLayout normalises a configured prefix.
func NewLayout(prefix string) Layout {
	return Layout{Prefix: strings.Trim(strings.TrimSpace(prefix), "/")}
}

// Base is the shared stem of a snapshot's three objects.
func (l Layout) Base(realm string, createdAt time.Time, snapshotID string) string {
	name := fmt.Sprintf("%s-%s", createdAt.UTC().Format("2006-01-02T1504"), snapshotID)
	if l.Prefix == "" {
		return path.Join(realm, name)
	}
	return path.Join(l.Prefix, realm, name)
}

// BundleKey is the sealed bundle.
func (l Layout) BundleKey(realm string, createdAt time.Time, snapshotID string) string {
	return l.Base(realm, createdAt, snapshotID) + BundleExt
}

// ManifestKey is the non-secret sidecar that makes Tier 0 listing possible
// without a key.
func (l Layout) ManifestKey(realm string, createdAt time.Time, snapshotID string) string {
	return l.Base(realm, createdAt, snapshotID) + ManifestExt
}

// DigestKey is the sidecar holding the sealed bundle's digest.
func (l Layout) DigestKey(realm string, createdAt time.Time, snapshotID string) string {
	return l.Base(realm, createdAt, snapshotID) + DigestExt
}

// RealmPrefix scopes a listing to one realm.
func (l Layout) RealmPrefix(realm string) string {
	if l.Prefix == "" {
		return realm + "/"
	}
	return path.Join(l.Prefix, realm) + "/"
}

// Root is the prefix everything lives under.
func (l Layout) Root() string {
	if l.Prefix == "" {
		return ""
	}
	return l.Prefix + "/"
}

// The three object extensions. They are constants because an operator reading a
// bucket with `ls` should be able to tell what each file is, and because the
// gitignore that stops a bundle being committed matches them literally.
const (
	BundleExt   = ".pck"
	ManifestExt = ".manifest.json"
	DigestExt   = ".sha256"
)

// Triplet groups the three objects belonging to one snapshot.
type Triplet struct {
	Base       string
	Realm      string
	Bundle     *ObjectInfo
	Manifest   *ObjectInfo
	Digest     *ObjectInfo
	SnapshotID string
	CreatedAt  time.Time
}

// Complete reports whether the bundle itself is present.
func (t Triplet) Complete() bool { return t.Bundle != nil }

// Group sorts a flat listing into snapshot triplets and leftovers.
//
// Foreign objects are returned rather than hidden. An operator debugging a
// misconfigured prefix needs to see what is really there, and silently
// filtering is how a prefix typo goes unnoticed for a month.
func Group(l Layout, objects []ObjectInfo) (snapshots []Triplet, foreign []ObjectInfo) {
	byBase := map[string]*Triplet{}
	order := []string{}

	for i := range objects {
		o := objects[i]
		var base, ext string
		switch {
		case strings.HasSuffix(o.Key, ManifestExt):
			base, ext = strings.TrimSuffix(o.Key, ManifestExt), ManifestExt
		case strings.HasSuffix(o.Key, DigestExt):
			base, ext = strings.TrimSuffix(o.Key, DigestExt), DigestExt
		case strings.HasSuffix(o.Key, BundleExt):
			base, ext = strings.TrimSuffix(o.Key, BundleExt), BundleExt
		default:
			foreign = append(foreign, o)
			continue
		}

		t, ok := byBase[base]
		if !ok {
			realm, id, created := parseBase(l, base)
			t = &Triplet{Base: base, Realm: realm, SnapshotID: id, CreatedAt: created}
			byBase[base] = t
			order = append(order, base)
		}
		info := o
		switch ext {
		case BundleExt:
			t.Bundle = &info
		case ManifestExt:
			t.Manifest = &info
		case DigestExt:
			t.Digest = &info
		}
	}

	for _, base := range order {
		snapshots = append(snapshots, *byBase[base])
	}
	sort.SliceStable(snapshots, func(i, j int) bool {
		return snapshots[i].CreatedAt.After(snapshots[j].CreatedAt)
	})
	sort.SliceStable(foreign, func(i, j int) bool { return foreign[i].Key < foreign[j].Key })
	return snapshots, foreign
}

func parseBase(l Layout, base string) (realm, id string, created time.Time) {
	rest := strings.TrimPrefix(base, l.Root())
	dir, name := path.Split(rest)
	realm = strings.Trim(dir, "/")

	// The stamp is fixed width and contains dashes of its own, so the split is
	// positional rather than on the first separator.
	const stampLen = len("2006-01-02T1504")
	if len(name) <= stampLen || name[stampLen] != '-' {
		return realm, name, time.Time{}
	}
	t, err := time.Parse("2006-01-02T1504", name[:stampLen])
	if err != nil {
		return realm, name, time.Time{}
	}
	return realm, name[stampLen+1:], t
}
