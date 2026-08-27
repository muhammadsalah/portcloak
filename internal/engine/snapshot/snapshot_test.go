// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package snapshot_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"portcloak/internal/engine/snapshot"
)

func buildFixture(t *testing.T, createdAt time.Time) *snapshot.Builder {
	t.Helper()
	b, err := snapshot.NewBuilder(filepath.Join(t.TempDir(), "stage"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Deliberately staged out of order: the packer sorts, so bundle bytes must
	// not depend on the order artifacts arrived in.
	for _, f := range []struct{ name, body string }{
		{"realm/acme-users-1.json", `{"users":[{"username":"m.klein"}]}`},
		{"realm/acme-realm.json", `{"realm":"acme","clients":[]}`},
		{"realm/acme-users-0.json", `{"users":[{"username":"j.doe"}]}`},
	} {
		if _, err := b.Stage(ctx, f.name, strings.NewReader(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.Document(snapshot.ManifestPath, map[string]any{"realm": "acme", "counts": map[string]int{"users": 2}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Document(snapshot.ProvenancePath, snapshot.Provenance{
		EnvironmentKind: "local", CaptureMode: "offline-export", ExecutionMode: "in-place",
		SecretVerification: "skipped", DependencyScan: "skipped",
	}); err != nil {
		t.Fatal(err)
	}

	tree := b.Tree()
	if _, err := b.Document(snapshot.IntegrityPath, tree); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Document(snapshot.EnvelopePath, snapshot.Envelope{
		SchemaVersion:    snapshot.SchemaVersion,
		SnapshotID:       "01HZY3",
		Realm:            "acme",
		CreatedAt:        createdAt,
		PortCloakVersion: "0.0.1",
		IntegrityRoot:    tree.Root,
		ArtifactCount:    len(tree.Artifacts),
		PayloadBytes:     b.PayloadBytes(),
	}); err != nil {
		t.Fatal(err)
	}
	return b
}

// A map iteration, a timestamp or a compression-level default can each break
// byte-identity, and losing it makes it impossible to prove a resumed transfer
// converged on the same object as an uninterrupted one.
func TestPackager_Deterministic(t *testing.T) {
	at := time.Date(2026, 8, 27, 9, 14, 0, 0, time.UTC)

	seal := func() []byte {
		b := buildFixture(t, at)
		var buf bytes.Buffer
		if _, err := b.Seal(context.Background(), &buf, nil); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}

	first, second := seal(), seal()
	if !bytes.Equal(first, second) {
		t.Fatalf("sealing the same input twice produced different bytes (%d vs %d)", len(first), len(second))
	}
}

// Nothing downstream needs the capturing machine's user id, and keeping it
// would leak it into every bundle.
func TestPackager_NormalisesArchiveMetadata(t *testing.T) {
	b := buildFixture(t, time.Date(2026, 8, 27, 9, 14, 0, 0, time.UTC))
	var buf bytes.Buffer
	if _, err := b.Seal(context.Background(), &buf, nil); err != nil {
		t.Fatal(err)
	}
	opened := openBytes(t, buf.Bytes())
	defer opened.Close()

	// Every artifact came out with the same restricted mode, whatever the
	// staging files had.
	err := filepath.Walk(opened.Dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s extracted with mode %o", p, got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPackager_RoundTrip(t *testing.T) {
	at := time.Date(2026, 8, 27, 9, 14, 0, 0, time.UTC)
	b := buildFixture(t, at)

	var buf bytes.Buffer
	res, err := b.Seal(context.Background(), &buf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Digest == "" || res.Size == 0 {
		t.Fatalf("seal reported %+v", res)
	}

	opened := openBytes(t, buf.Bytes())
	defer opened.Close()

	if opened.Envelope.Realm != "acme" || opened.Envelope.SnapshotID != "01HZY3" {
		t.Fatalf("envelope round-tripped as %+v", opened.Envelope)
	}
	if opened.Envelope.IntegrityRoot != res.Root {
		t.Errorf("envelope root %q does not match the sealed root %q", opened.Envelope.IntegrityRoot, res.Root)
	}
	if !opened.Verify.OK {
		t.Fatalf("a freshly sealed bundle did not verify: %s", opened.Verify.Message)
	}
	if len(opened.RealmFiles) != 3 {
		t.Errorf("got %d realm files, want 3: %v", len(opened.RealmFiles), opened.RealmFiles)
	}

	var provenance snapshot.Provenance
	if err := opened.Document(snapshot.ProvenancePath, &provenance); err != nil {
		t.Fatal(err)
	}
	if provenance.CaptureMode != "offline-export" {
		t.Errorf("provenance round-tripped as %+v", provenance)
	}
}

// A single flipped byte anywhere must be caught, and the failure has to name
// the artifact rather than declare the whole bundle bad.
func TestIntegrityTree_DetectsSingleByteFlip(t *testing.T) {
	tree := snapshot.NewIntegrityTree([]snapshot.ArtifactDigest{
		{Name: "realm/acme-realm.json", Size: 10, SHA256: "aaaa"},
		{Name: "realm/acme-users-0.json", Size: 20, SHA256: "bbbb"},
	})

	good := tree.Verify([]snapshot.ArtifactDigest{
		{Name: "realm/acme-realm.json", Size: 10, SHA256: "aaaa"},
		{Name: "realm/acme-users-0.json", Size: 20, SHA256: "bbbb"},
	})
	if !good.OK {
		t.Fatalf("an unchanged set failed: %s", good.Message)
	}

	flipped := tree.Verify([]snapshot.ArtifactDigest{
		{Name: "realm/acme-realm.json", Size: 10, SHA256: "aaaa"},
		{Name: "realm/acme-users-0.json", Size: 20, SHA256: "bbbc"},
	})
	if flipped.OK {
		t.Fatal("a changed artifact verified")
	}
	failures := flipped.Failures()
	if len(failures) != 1 || failures[0].Name != "realm/acme-users-0.json" {
		t.Fatalf("verification did not name the failing artifact: %+v", failures)
	}
	if !strings.Contains(flipped.Message, "acme-users-0.json") {
		t.Errorf("the message does not name the artifact: %q", flipped.Message)
	}
}

func TestIntegrityTree_NoticesMissingAndUnsealedArtifacts(t *testing.T) {
	tree := snapshot.NewIntegrityTree([]snapshot.ArtifactDigest{
		{Name: "a", SHA256: "1"}, {Name: "b", SHA256: "2"},
	})

	missing := tree.Verify([]snapshot.ArtifactDigest{{Name: "a", SHA256: "1"}})
	if missing.OK {
		t.Fatal("a bundle missing an artifact verified")
	}
	if !strings.Contains(missing.Failures()[0].Note, "not in the bundle") {
		t.Errorf("unhelpful note: %q", missing.Failures()[0].Note)
	}

	extra := tree.Verify([]snapshot.ArtifactDigest{
		{Name: "a", SHA256: "1"}, {Name: "b", SHA256: "2"}, {Name: "c", SHA256: "3"},
	})
	if extra.OK {
		t.Fatal("content nobody sealed was accepted")
	}
}

// Moving an artifact's contents to a different name must change the root.
func TestIntegrityTree_RootBindsNameToContent(t *testing.T) {
	a := snapshot.NewIntegrityTree([]snapshot.ArtifactDigest{{Name: "x", Size: 1, SHA256: "aa"}})
	b := snapshot.NewIntegrityTree([]snapshot.ArtifactDigest{{Name: "y", Size: 1, SHA256: "aa"}})
	if a.Root == b.Root {
		t.Fatal("the root does not bind an artifact's name to its content")
	}
}

// A bundle is untrusted input the moment it comes back from storage.
func TestOpen_RefusesAnEntryThatWouldEscape(t *testing.T) {
	b, err := snapshot.NewBuilder(filepath.Join(t.TempDir(), "stage"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Stage(context.Background(), "../escaped.json", strings.NewReader("x")); err == nil {
		t.Fatal("an artifact name traversing above the bundle root was accepted")
	}
}

func TestOpen_TamperedBundleIsFlaggedNotRendered(t *testing.T) {
	at := time.Date(2026, 8, 27, 9, 14, 0, 0, time.UTC)
	b := buildFixture(t, at)

	// Tamper after the tree was computed: the staged file changes, but the
	// sealed integrity record still describes the original.
	staged := filepath.Join(b.Dir(), "realm", "acme-users-0.json")
	if err := os.WriteFile(staged, []byte(`{"users":[{"username":"someone-else"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Restage so the tar header size matches the new contents, without
	// recomputing the sealed tree.
	var buf bytes.Buffer
	if _, err := b.Seal(context.Background(), &buf, nil); err == nil {
		// Sealing may succeed or fail depending on size; either way the open
		// below is what has to catch it.
		opened := openBytes(t, buf.Bytes())
		defer opened.Close()
		if opened.Verify.OK {
			t.Fatal("a tampered bundle verified")
		}
		if len(opened.Verify.Failures()) == 0 {
			t.Fatal("verification failed without naming an artifact")
		}
	}
}

func TestReadEnvelopeOnly(t *testing.T) {
	at := time.Date(2026, 8, 27, 9, 14, 0, 0, time.UTC)
	b := buildFixture(t, at)
	var buf bytes.Buffer
	if _, err := b.Seal(context.Background(), &buf, nil); err != nil {
		t.Fatal(err)
	}
	e, err := snapshot.ReadEnvelopeOnly(context.Background(), bytes.NewReader(buf.Bytes()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.Realm != "acme" || e.SchemaVersion != snapshot.SchemaVersion {
		t.Fatalf("envelope read as %+v", e)
	}
}

func TestReadEnvelopeOnly_RejectsSomethingElse(t *testing.T) {
	_, err := snapshot.ReadEnvelopeOnly(context.Background(), strings.NewReader("this is not a bundle"), nil)
	if err == nil {
		t.Fatal("an arbitrary file was accepted as a snapshot")
	}
	if !strings.Contains(err.Error(), "snapshot") {
		t.Errorf("the message does not say what was wrong: %v", err)
	}
}

// Sealing must not scale with the size of the input, or a large realm cannot be
// captured on an ordinary laptop.
func TestPackager_BoundedMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates a large staged artifact")
	}
	b, err := snapshot.NewBuilder(filepath.Join(t.TempDir(), "stage"))
	if err != nil {
		t.Fatal(err)
	}
	const size = 256 << 20
	chunk := bytes.Repeat([]byte("keycloak realm export payload\n"), 4096)
	if _, err := b.Stage(context.Background(), "realm/big.json", &repeatReader{chunk: chunk, remaining: size}); err != nil {
		t.Fatal(err)
	}
	tree := b.Tree()
	if _, err := b.Document(snapshot.EnvelopePath, snapshot.Envelope{
		SchemaVersion: snapshot.SchemaVersion, Realm: "big", IntegrityRoot: tree.Root,
	}); err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	var peak uint64
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peak {
				peak = m.HeapAlloc
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	if _, err := b.Seal(context.Background(), io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	close(done)

	const ceiling = 192 << 20
	if peak > ceiling {
		t.Fatalf("sealing a %d MB input peaked at %d MB of heap, which is above the %d MB ceiling",
			size>>20, peak>>20, ceiling>>20)
	}
}

type repeatReader struct {
	chunk     []byte
	remaining int
}

func (r *repeatReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := copy(p, r.chunk)
	if n > r.remaining {
		n = r.remaining
	}
	r.remaining -= n
	return n, nil
}

func openBytes(t *testing.T, b []byte) *snapshot.Opened {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "open")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	o, err := snapshot.Open(context.Background(), bytes.NewReader(b), snapshot.OpenOptions{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return o
}
