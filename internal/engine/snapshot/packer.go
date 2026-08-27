// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Sealer wraps the compressed stream in encryption. The crypto package supplies
// one; snapshot never imports it, so packaging stays testable without keys.
type Sealer interface {
	// Wrap returns a writer whose contents end up encrypted into w.
	Wrap(w io.Writer) (io.WriteCloser, error)
	// Describe records what the envelope should say about this encryption.
	Describe() Encryption
}

// Opener reverses a Sealer.
type Opener interface {
	Unwrap(r io.Reader) (io.Reader, error)
}

// compressionLevel is pinned rather than left to the library default, because a
// default that changes between releases would silently break byte-identity
// between two builds of PortCloak.
const compressionLevel = zstd.SpeedBetterCompression

// encoderConcurrency bounds the compressor's worker count.
//
// Left to its default the encoder scales with GOMAXPROCS and allocates a window
// per worker, so the memory a capture needs would depend on how many cores the
// operator's machine happens to have — which turns a bounded-memory promise
// into a machine-dependent one. Two workers keep the pipeline fed; sealing is
// I/O bound long before it is CPU bound.
const encoderConcurrency = 2

// Builder stages the artifacts of one snapshot and then seals them.
//
// Staging to disk rather than holding the export in memory is what keeps a
// realm with 120,000 users inside a bounded memory ceiling. Each artifact is
// hashed as its bytes pass, so the digest costs nothing beyond the copy that
// was happening anyway.
type Builder struct {
	dir       string
	artifacts []ArtifactDigest
	payload   int64
	owned     bool
}

// NewBuilder creates a builder staging into dir, which it creates.
func NewBuilder(dir string) (*Builder, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating the staging folder %s: %w", dir, err)
	}
	return &Builder{dir: dir, owned: true}, nil
}

// Dir is where artifacts are staged.
func (b *Builder) Dir() string { return b.dir }

// Stage writes one artifact into the staging area, hashing as it goes.
//
// name is the path inside the bundle, e.g. realm/acme-users-0.json.
func (b *Builder) Stage(ctx context.Context, name string, r io.Reader) (ArtifactDigest, error) {
	name = path.Clean(strings.TrimPrefix(name, "/"))
	if name == "." || strings.HasPrefix(name, "..") {
		return ArtifactDigest{}, fmt.Errorf("%q is not a name an artifact can have inside a bundle", name)
	}

	dest := filepath.Join(b.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return ArtifactDigest{}, err
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return ArtifactDigest{}, err
	}
	defer f.Close() //nolint:errcheck // the explicit Sync below is what commits.

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), &ctxReader{ctx: ctx, r: r})
	if err != nil {
		return ArtifactDigest{}, err
	}
	if err := f.Sync(); err != nil {
		return ArtifactDigest{}, err
	}

	d := ArtifactDigest{Name: name, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}
	b.replace(d)
	b.payload += n
	return d, nil
}

// Document writes one of PortCloak's own JSON documents into the bundle,
// canonically encoded so the same content always produces the same bytes.
func (b *Builder) Document(name string, v any) (ArtifactDigest, error) {
	data, err := CanonicalJSON(v)
	if err != nil {
		return ArtifactDigest{}, fmt.Errorf("encoding %s: %w", name, err)
	}
	return b.Stage(context.Background(), name, bytes.NewReader(data))
}

func (b *Builder) replace(d ArtifactDigest) {
	for i := range b.artifacts {
		if b.artifacts[i].Name == d.Name {
			b.payload -= b.artifacts[i].Size
			b.artifacts[i] = d
			return
		}
	}
	b.artifacts = append(b.artifacts, d)
}

// Artifacts returns the staged leaves, sorted by name.
func (b *Builder) Artifacts() []ArtifactDigest {
	out := make([]ArtifactDigest, len(b.artifacts))
	copy(out, b.artifacts)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// PayloadBytes is the uncompressed size of everything staged.
func (b *Builder) PayloadBytes() int64 { return b.payload }

// Tree builds the checksum tree over everything staged so far.
//
// Two documents are excluded, for the same reason: integrity.json cannot hold
// its own digest, and envelope.json carries the tree's root, so hashing it
// would be circular. Both are still covered — by the .sha256 sidecar over the
// sealed bundle, and by the AEAD tag when the bundle is encrypted.
func (b *Builder) Tree() IntegrityTree {
	return NewIntegrityTree(SealedLeaves(b.artifacts))
}

// SealedLeaves filters a set of artifacts down to the ones the checksum tree
// covers. Both the writer and the reader use it, so the two can never disagree
// about what was sealed.
func SealedLeaves(artifacts []ArtifactDigest) []ArtifactDigest {
	out := make([]ArtifactDigest, 0, len(artifacts))
	for _, a := range artifacts {
		if a.Name == IntegrityPath || a.Name == EnvelopePath {
			continue
		}
		out = append(out, a)
	}
	return out
}

// Cleanup removes the staging area. Even locally, the export directory holds
// unmasked secrets, so leaving it behind after a successful capture would be
// the tool creating exactly the exposure it exists to manage.
func (b *Builder) Cleanup() error {
	if !b.owned || b.dir == "" {
		return nil
	}
	return os.RemoveAll(b.dir)
}

// SealResult describes the sealed bundle.
type SealResult struct {
	// Digest is the SHA-256 of the sealed bytes as they were written — after
	// compression and after encryption. It is what the .sha256 sidecar holds
	// and what a storage backend's own checksum is compared against.
	Digest string
	// Size is the sealed length.
	Size int64
	// Root is the integrity tree root over the artifacts inside.
	Root string
	// Encryption is what the envelope recorded.
	Encryption Encryption
}

// Seal writes the bundle to w: tar, zstd, then optionally encryption.
//
// The whole pipeline is writer-chained, so a 2 GB export is never held in
// memory. Nothing is buffered except one tar block at a time.
func (b *Builder) Seal(ctx context.Context, w io.Writer, sealer Sealer) (SealResult, error) {
	res := SealResult{Root: b.Tree().Root}
	if sealer != nil {
		res.Encryption = sealer.Describe()
	}

	digest := sha256.New()
	counter := &countingWriter{w: io.MultiWriter(w, digest)}

	// The chain is built outermost-first and torn down innermost-first, which
	// is the only order in which every buffer actually reaches the sink.
	var sink io.Writer = counter
	var encCloser io.WriteCloser
	if sealer != nil {
		var err error
		encCloser, err = sealer.Wrap(sink)
		if err != nil {
			return res, err
		}
		sink = encCloser
	}

	zw, err := zstd.NewWriter(sink,
		zstd.WithEncoderLevel(compressionLevel),
		zstd.WithEncoderConcurrency(encoderConcurrency))
	if err != nil {
		return res, fmt.Errorf("starting compression: %w", err)
	}
	tw := tar.NewWriter(zw)

	if err := b.writeTar(ctx, tw); err != nil {
		_ = tw.Close()
		_ = zw.Close()
		if encCloser != nil {
			_ = encCloser.Close()
		}
		return res, err
	}
	if err := tw.Close(); err != nil {
		return res, fmt.Errorf("finishing the archive: %w", err)
	}
	if err := zw.Close(); err != nil {
		return res, fmt.Errorf("finishing compression: %w", err)
	}
	if encCloser != nil {
		if err := encCloser.Close(); err != nil {
			return res, fmt.Errorf("finishing encryption: %w", err)
		}
	}

	res.Digest = hex.EncodeToString(digest.Sum(nil))
	res.Size = counter.n
	return res, nil
}

// epoch is the fixed modification time every tar header carries.
//
// Real timestamps would make two captures of an unchanged realm produce
// different bytes, which would break idempotence and make it impossible to
// prove a resumed transfer converged on the same object as an uninterrupted one.
var epoch = time.Unix(0, 0).UTC()

func (b *Builder) writeTar(ctx context.Context, tw *tar.Writer) error {
	for _, a := range b.Artifacts() {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     a.Name,
			Size:     a.Size,
			// Ownership and timestamps are normalised away. Nothing downstream
			// needs them, and keeping them would leak the capturing machine's
			// user id into every bundle.
			Mode:    0o600,
			Uid:     0,
			Gid:     0,
			ModTime: epoch,
			Format:  tar.FormatPAX,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("writing the header for %s: %w", a.Name, err)
		}
		f, err := os.Open(filepath.Join(b.dir, filepath.FromSlash(a.Name)))
		if err != nil {
			return err
		}
		n, err := io.Copy(tw, f)
		_ = f.Close()
		if err != nil {
			return fmt.Errorf("writing %s into the bundle: %w", a.Name, err)
		}
		if n != a.Size {
			return fmt.Errorf("%s changed size while it was being sealed", a.Name)
		}
	}
	return nil
}

// CanonicalJSON encodes a value with sorted keys, no HTML escaping and a
// trailing newline, so identical content always produces identical bytes.
func CanonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ctxReader makes a long copy cancellable, so a Cancel during a fetch stops
// promptly rather than at the end of the file.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
