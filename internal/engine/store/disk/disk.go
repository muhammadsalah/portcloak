// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package disk stores snapshots in a folder on this machine.
//
// The layout is browsable by design: an operator who lost the application
// should still be able to find and identify a snapshot with ls.
package disk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/store"
)

// Store is the disk BlobStore.
type Store struct {
	root string
}

// New builds a disk store rooted at folder. The folder is expanded so a
// configured ~/PortCloak/snapshots means what an operator expects.
func New(folder string) (*Store, error) {
	root, err := expand(folder)
	if err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

func expand(folder string) (string, error) {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return "", fmt.Errorf("no folder was given")
	}
	if strings.HasPrefix(folder, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expanding %q: %w", folder, err)
		}
		folder = filepath.Join(home, strings.TrimPrefix(folder, "~"))
	}
	return filepath.Abs(folder)
}

// Root is the expanded folder.
func (s *Store) Root() string { return s.root }

// Endpoint identifies this store.
func (s *Store) Endpoint() string { return "disk:" + s.root }

// Close releases nothing; disk holds no connection.
func (s *Store) Close() error { return nil }

// Probe confirms the folder exists and establishes whether it can be written
// to, leaving nothing behind either way.
func (s *Store) Probe(ctx context.Context) (store.Reach, error) {
	start := time.Now()
	r := store.Reach{
		Root:      s.root,
		Resumable: true,
		Integrity: store.IntegrityClientSide,
		ProbedAt:  start,
	}

	// A folder that is not there yet is created rather than reported as a
	// failure. The operator named where the snapshots go, and a folder they
	// have not made by hand is the ordinary case rather than a mistake.
	st, err := os.Stat(s.root)
	if err != nil && os.IsNotExist(err) {
		if mkErr := os.MkdirAll(s.root, 0o700); mkErr != nil {
			r.Access = store.AccessNone
			r.FailedStep = "creating the folder"
			r.Detail = fmt.Sprintf("%s does not exist and could not be created: %v", s.root, mkErr)
			return r, nil
		}
		st, err = os.Stat(s.root)
	}
	if err != nil {
		r.Access = store.AccessNone
		r.FailedStep = "reading the folder"
		r.Detail = err.Error()
		return r, nil
	}
	if !st.IsDir() {
		r.Access = store.AccessNone
		r.FailedStep = "finding the folder"
		r.Detail = fmt.Sprintf("%s is a file, not a folder.", s.root)
		return r, nil
	}

	if _, err := os.ReadDir(s.root); err != nil {
		r.Access = store.AccessNone
		r.FailedStep = "listing the folder"
		r.Detail = err.Error()
		return r, nil
	}
	r.Access = store.AccessReadOnly

	// Prove write access the only way that is honest: write something, read it
	// back, and remove it.
	probe := filepath.Join(s.root, ".portcloak-probe")
	payload := []byte("portcloak write probe")
	if err := os.WriteFile(probe, payload, 0o600); err != nil {
		r.Detail = fmt.Sprintf("The folder can be read but not written to: %v", err)
		r.Latency = time.Since(start)
		return r, nil
	}
	defer os.Remove(probe) //nolint:errcheck // cleanup runs even when the probe fails.

	got, err := os.ReadFile(probe)
	if err != nil || string(got) != string(payload) {
		r.FailedStep = "reading the probe file back"
		r.Detail = "The folder accepted a write but did not return the same bytes."
		r.Latency = time.Since(start)
		return r, nil
	}
	r.Access = store.AccessWritable
	r.FreeBytes = freeSpace(s.root)
	r.Latency = time.Since(start)
	return r, nil
}

func (s *Store) path(key string) (string, error) {
	// A traversing key is rejected rather than quietly rewritten. Every key
	// PortCloak produces comes from its own layout, so one containing ".." is
	// a bug or a tampered sidecar — and silently clamping it to the root would
	// write a snapshot to a name nobody asked for.
	for _, seg := range strings.Split(filepath.ToSlash(key), "/") {
		if seg == ".." {
			return "", resil.Fatal("resolve a key",
				fmt.Sprintf("%q points outside the storage folder.", key), nil)
		}
	}
	full := filepath.Join(s.root, filepath.FromSlash(key))
	if !strings.HasPrefix(full, s.root+string(filepath.Separator)) && full != s.root {
		return "", resil.Fatal("resolve a key",
			fmt.Sprintf("%q points outside the storage folder.", key), nil)
	}
	return full, nil
}

// Stat reports on one object.
func (s *Store) Stat(ctx context.Context, key string) (store.ObjectInfo, error) {
	p, err := s.path(key)
	if err != nil {
		return store.ObjectInfo{}, err
	}
	st, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return store.ObjectInfo{}, fmt.Errorf("%w: %s", store.ErrNotFound, key)
		}
		return store.ObjectInfo{}, err
	}
	return store.ObjectInfo{Key: key, Size: st.Size(), ModTime: st.ModTime()}, nil
}

// Put writes an object.
//
// The write goes to a temp name in the same directory, is fsynced, and then
// renamed. An interrupted write can therefore never leave something that looks
// like a complete bundle — the disk-local instance of the rule the resilience
// layer generalises.
func (s *Store) Put(ctx context.Context, key string, r io.Reader, opts store.PutOptions) (store.PutResult, error) {
	p, err := s.path(key)
	if err != nil {
		return store.PutResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return store.PutResult{}, fmt.Errorf("creating %s: %w", filepath.Dir(p), err)
	}

	// Resume writes into the same temp file rather than starting over, which is
	// what makes an interrupted local write converge instead of duplicating.
	tmp := p + ".part"
	flag := os.O_CREATE | os.O_WRONLY
	var written int64

	// The destination decides where the transfer actually got to, and then
	// positions the source to match. A checkpoint is a hint; this file is the
	// authority.
	resumeFrom := int64(0)
	if opts.Offset > 0 {
		if st, statErr := os.Stat(tmp); statErr == nil && st.Size() > 0 {
			resumeFrom = min64(st.Size(), opts.Offset)
		}
	}
	if resumeFrom > 0 {
		if err := seekTo(r, resumeFrom); err != nil {
			// A source that cannot be repositioned cannot be resumed from, so
			// the whole object is written again rather than a truncated one
			// committed.
			resumeFrom = 0
		}
	}
	if resumeFrom == 0 {
		if err := seekTo(r, 0); err != nil && opts.Offset > 0 {
			return store.PutResult{}, resil.Fatal("resume the upload",
				fmt.Sprintf("%s cannot be resumed because its source cannot be rewound.", key), err)
		}
	}

	if resumeFrom > 0 {
		flag |= os.O_APPEND
		written = resumeFrom
		if err := os.Truncate(tmp, resumeFrom); err != nil {
			return store.PutResult{}, err
		}
	} else {
		flag |= os.O_TRUNC
	}
	opts.Offset = resumeFrom

	// The hash picks up where the interrupted attempt left off, so the digest
	// checked below covers the whole file rather than only the bytes this
	// attempt wrote.
	// The state is only usable when the destination corroborated the whole
	// checkpoint. If it held less than the checkpoint claimed, the state
	// describes bytes that are not there, and re-reading is the honest way to
	// establish what is.
	state := opts.HashState
	if resumeFrom != opts.Offset {
		state = nil
	}
	h, err := store.ResumeHash(state, resumeFrom, func() (io.ReadCloser, error) {
		return os.Open(tmp)
	})
	if err != nil {
		return store.PutResult{}, resil.Fatal("resume the write",
			fmt.Sprintf("PortCloak could not re-establish the checksum of the %d bytes of %s already written, so resuming would commit them unverified.", resumeFrom, key), err)
	}

	f, err := os.OpenFile(tmp, flag, 0o600)
	if err != nil {
		return store.PutResult{}, fmt.Errorf("creating %s: %w", tmp, err)
	}

	pw := &store.ProgressWriter{W: io.MultiWriter(f, h), Ctx: ctx, OnWrite: opts.Progress,
		OnCheckpoint: opts.Checkpoint, Hash: h, Written: written}
	n, copyErr := io.Copy(pw, r)
	if copyErr != nil {
		_ = f.Close()
		return store.PutResult{}, copyErr
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return store.PutResult{}, fmt.Errorf("flushing %s to disk: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return store.PutResult{}, err
	}

	// A resumed write is verified exactly like a fresh one. The prefix it did
	// not send is still its responsibility: it is about to be committed under
	// this key.
	digest := hex.EncodeToString(h.Sum(nil))
	if opts.Digest != "" && opts.Digest != digest {
		_ = os.Remove(tmp)
		return store.PutResult{}, resil.Fatal("verify the write",
			fmt.Sprintf("%s did not arrive intact. The bytes written do not match the digest computed before the transfer.", key), nil)
	}

	if err := os.Rename(tmp, p); err != nil {
		return store.PutResult{}, fmt.Errorf("replacing %s: %w", p, err)
	}
	if d, err := os.Open(filepath.Dir(p)); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}

	return store.PutResult{
		Key:     key,
		Size:    written + n,
		Digest:  digest,
		Resumed: opts.Offset > 0,
	}, nil
}

// Get reads an object.
func (s *Store) Get(ctx context.Context, key string, w io.Writer, opts store.GetOptions) (store.GetResult, error) {
	p, err := s.path(key)
	if err != nil {
		return store.GetResult{}, err
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return store.GetResult{}, fmt.Errorf("%w: %s", store.ErrNotFound, key)
		}
		return store.GetResult{}, err
	}
	defer f.Close() //nolint:errcheck // read-only.

	if opts.Offset > 0 {
		if _, err := f.Seek(opts.Offset, io.SeekStart); err != nil {
			return store.GetResult{}, err
		}
	}
	h := sha256.New()
	pw := &store.ProgressWriter{W: io.MultiWriter(w, h), Ctx: ctx, OnWrite: opts.Progress, Written: opts.Offset}
	n, err := io.Copy(pw, f)
	if err != nil {
		return store.GetResult{}, err
	}
	return store.GetResult{Size: opts.Offset + n, Digest: hex.EncodeToString(h.Sum(nil))}, nil
}

// List returns every object under a prefix.
func (s *Store) List(ctx context.Context, prefix string) ([]store.ObjectInfo, error) {
	base := s.root
	if prefix != "" {
		p, err := s.path(prefix)
		if err != nil {
			return nil, err
		}
		base = p
		// A prefix that is not a directory still lists: it is a key stem, which
		// is how every object store treats it. It applies to a prefix only —
		// applied to the root it would scan the folder above the configured
		// one and report its contents as this storage's.
		if st, err := os.Stat(base); err != nil || !st.IsDir() {
			base = filepath.Dir(base)
		}
	}
	scanRoot := base

	var out []store.ObjectInfo
	err := filepath.WalkDir(scanRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == ".portcloak-probe" || strings.HasSuffix(name, ".part") {
			return nil
		}
		rel, relErr := filepath.Rel(s.root, p)
		if relErr != nil {
			return nil
		}
		key := filepath.ToSlash(rel)
		if prefix != "" && !strings.HasPrefix(key, strings.TrimPrefix(prefix, "/")) {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		out = append(out, store.ObjectInfo{Key: key, Size: info.Size(), ModTime: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// Delete removes an object. Deleting one that is not there has already reached
// the desired end state.
func (s *Store) Delete(ctx context.Context, key string) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Prune the realm directory once it is empty, so a browsable tree does not
	// accumulate the ghosts of deleted realms.
	dir := filepath.Dir(p)
	for dir != s.root && strings.HasPrefix(dir, s.root) {
		if entries, err := os.ReadDir(dir); err != nil || len(entries) > 0 {
			break
		}
		if err := os.Remove(dir); err != nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	return nil
}

// EnsureRoot creates the folder, for the "the folder does not exist yet, shall
// I create it?" path.
func (s *Store) EnsureRoot() error {
	return os.MkdirAll(s.root, 0o700)
}

// seekTo positions a source at an absolute offset, where it can be positioned
// at all.
func seekTo(r io.Reader, offset int64) error {
	seeker, ok := r.(io.Seeker)
	if !ok {
		if offset == 0 {
			// A non-seekable reader is already at its start.
			return nil
		}
		return fmt.Errorf("this source cannot be repositioned")
	}
	_, err := seeker.Seek(offset, io.SeekStart)
	return err
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
