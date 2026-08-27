// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// decoderConcurrency bounds decompression memory the same way
// encoderConcurrency bounds compression memory.
const decoderConcurrency = 2

// ErrEncrypted is returned when a bundle needs a key that was not supplied.
var ErrEncrypted = errors.New("this snapshot is encrypted")

// Opened is an unsealed bundle: its documents parsed, its artifacts extracted
// to a working directory, and its integrity established.
//
// Verification happens before any of this is handed back. A bundle that fails
// its checksum is never presented as readable content — it opens in a clearly
// flagged degraded state for diagnosis, and restore is blocked.
type Opened struct {
	Dir       string
	Envelope  Envelope
	Integrity IntegrityTree
	Verify    VerifyResult

	// Documents holds the raw bytes of the bundle's own JSON records, so a
	// caller can decode them into whatever shape it needs without the snapshot
	// package having to know about manifests.
	Documents map[string][]byte
	// RealmFiles are the export artifacts, relative to Dir.
	RealmFiles []string
}

// Close removes the working directory. Decrypted realm material must not
// outlive the session that needed it.
func (o *Opened) Close() error {
	if o.Dir == "" {
		return nil
	}
	return os.RemoveAll(o.Dir)
}

// Document decodes one of the bundle's own JSON records.
func (o *Opened) Document(name string, into any) error {
	b, ok := o.Documents[name]
	if !ok {
		return fmt.Errorf("this snapshot does not contain %s", name)
	}
	return json.Unmarshal(b, into)
}

// Path resolves an artifact name to its extracted location.
func (o *Opened) Path(name string) string {
	return filepath.Join(o.Dir, filepath.FromSlash(name))
}

// OpenOptions controls how a bundle is unsealed.
type OpenOptions struct {
	// Opener decrypts, when the bundle is encrypted.
	Opener Opener
	// Dir is where artifacts are extracted. It must already exist and be
	// restricted; the caller owns removing it.
	Dir string
	// MaxArtifactBytes caps a single artifact, so a hostile or corrupt bundle
	// cannot fill a disk. Zero means no cap.
	MaxArtifactBytes int64
	// Progress reports extraction, which on a large realm takes real time.
	Progress func(name string, bytes int64)
}

// Open unseals a bundle from r, verifies it, and returns what it holds.
func Open(ctx context.Context, r io.Reader, opts OpenOptions) (*Opened, error) {
	if opts.Dir == "" {
		return nil, fmt.Errorf("no working directory was given to open into")
	}

	body := r
	if opts.Opener != nil {
		var err error
		body, err = opts.Opener.Unwrap(r)
		if err != nil {
			return nil, err
		}
	}

	zr, err := zstd.NewReader(body, zstd.WithDecoderConcurrency(decoderConcurrency))
	if err != nil {
		return nil, fmt.Errorf("this file is not a PortCloak snapshot, or it is encrypted and no key was supplied: %w", err)
	}
	defer zr.Close()

	out := &Opened{Dir: opts.Dir, Documents: map[string][]byte{}}
	var observed []ArtifactDigest

	tr := tar.NewReader(zr)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("this file is not a readable PortCloak snapshot: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name, err := safeName(hdr.Name)
		if err != nil {
			return nil, err
		}
		if opts.MaxArtifactBytes > 0 && hdr.Size > opts.MaxArtifactBytes {
			return nil, fmt.Errorf("%s is %d bytes, which is larger than this open allows", name, hdr.Size)
		}

		digest, err := out.extract(name, tr, opts)
		if err != nil {
			return nil, err
		}
		observed = append(observed, digest)
	}

	if err := out.loadDocuments(); err != nil {
		return nil, err
	}

	// The tree is verified against what was actually extracted, and the result
	// travels with the opened bundle so every caller sees the same verdict.
	out.Verify = out.Integrity.Verify(SealedLeaves(observed))
	out.Verify.Decryptable = true
	return out, nil
}

func (o *Opened) extract(name string, r io.Reader, opts OpenOptions) (ArtifactDigest, error) {
	dest := filepath.Join(o.Dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return ArtifactDigest{}, err
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return ArtifactDigest{}, err
	}
	defer f.Close() //nolint:errcheck // read path; a failed close surfaces on the next read.

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), r)
	if err != nil {
		return ArtifactDigest{}, fmt.Errorf("extracting %s: %w", name, err)
	}
	if opts.Progress != nil {
		opts.Progress(name, n)
	}
	if strings.HasPrefix(name, RealmDir) {
		o.RealmFiles = append(o.RealmFiles, name)
	}
	return ArtifactDigest{Name: name, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

func (o *Opened) loadDocuments() error {
	for _, name := range []string{EnvelopePath, ManifestPath, ProvenancePath, DependenciesPath, IntegrityPath} {
		b, err := os.ReadFile(o.Path(name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		o.Documents[name] = b
	}
	if b, ok := o.Documents[EnvelopePath]; ok {
		if err := json.Unmarshal(b, &o.Envelope); err != nil {
			return fmt.Errorf("this snapshot's envelope could not be read: %w", err)
		}
	} else {
		return fmt.Errorf("this file has no envelope, so it is not a PortCloak snapshot")
	}
	if b, ok := o.Documents[IntegrityPath]; ok {
		if err := json.Unmarshal(b, &o.Integrity); err != nil {
			return fmt.Errorf("this snapshot's integrity record could not be read: %w", err)
		}
	}
	return nil
}

// safeName rejects an archive entry that would write outside the working
// directory. A bundle is an untrusted input the moment it comes back from
// storage, whoever wrote it.
func safeName(name string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean("/" + name))
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || clean == "." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("this bundle contains an entry named %q, which would write outside the folder it is being opened into", name)
	}
	return clean, nil
}

// ReadEnvelopeOnly pulls just the envelope out of a bundle without extracting
// it, which is what a cheap "what is this file" check needs.
func ReadEnvelopeOnly(ctx context.Context, r io.Reader, opener Opener) (Envelope, error) {
	body := r
	if opener != nil {
		var err error
		body, err = opener.Unwrap(r)
		if err != nil {
			return Envelope{}, err
		}
	}
	zr, err := zstd.NewReader(body, zstd.WithDecoderConcurrency(decoderConcurrency))
	if err != nil {
		return Envelope{}, fmt.Errorf("this file is not a readable PortCloak snapshot: %w", err)
	}
	defer zr.Close()

	tr := tar.NewReader(zr)
	for {
		if err := ctx.Err(); err != nil {
			return Envelope{}, err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return Envelope{}, fmt.Errorf("this file has no envelope, so it is not a PortCloak snapshot")
		}
		if err != nil {
			return Envelope{}, fmt.Errorf("this file is not a readable PortCloak snapshot: %w", err)
		}
		if hdr.Name != EnvelopePath {
			continue
		}
		var e Envelope
		if err := json.NewDecoder(io.LimitReader(tr, 1<<20)).Decode(&e); err != nil {
			return Envelope{}, fmt.Errorf("this snapshot's envelope could not be read: %w", err)
		}
		return e, nil
	}
}
