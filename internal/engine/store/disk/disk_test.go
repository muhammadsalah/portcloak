package disk_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"portcloak/internal/engine/store"
	"portcloak/internal/engine/store/disk"
	"portcloak/internal/engine/store/storetest"
)

func newDisk(t *testing.T) store.BlobStore {
	t.Helper()
	s, err := disk.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDisk_Contract(t *testing.T) {
	storetest.RunContract(t, newDisk)
}

// An operator who lost the application should still be able to find and
// identify a snapshot with ls.
func TestDisk_LayoutIsBrowsable(t *testing.T) {
	root := t.TempDir()
	s, err := disk.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, k := range []string{
		"acme/2026-08-27T0914-01HZY3.pck",
		"acme/2026-08-27T0914-01HZY3.manifest.json",
		"acme/2026-08-27T0914-01HZY3.sha256",
	} {
		if _, err := s.Put(ctx, k, strings.NewReader("x"), store.PutOptions{Size: 1}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "acme"))
	if err != nil {
		t.Fatalf("the realm folder is not browsable: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected the bundle and both sidecars side by side, got %d", len(entries))
	}
}

// An interrupted write must never leave something that looks like a complete
// bundle.
func TestDisk_InterruptedWriteLeavesNothingComplete(t *testing.T) {
	root := t.TempDir()
	s, err := disk.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	broken := &storetest.Reader{Data: bytes.Repeat([]byte("a"), 4096), Fail: 1024, Err: os.ErrClosed}
	if _, err := s.Put(ctx, "acme/interrupted.pck", broken, store.PutOptions{Size: 4096}); err == nil {
		t.Fatal("an interrupted write reported success")
	}
	if _, err := os.Stat(filepath.Join(root, "acme", "interrupted.pck")); !os.IsNotExist(err) {
		t.Fatal("an interrupted write left a file at the final name")
	}
	// The partial file is left under its temp name only, which is what makes
	// resume possible without ever presenting a partial object as complete.
	objects, err := s.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objects {
		if strings.HasSuffix(o.Key, ".pck") {
			t.Errorf("a partial write is listed as a snapshot: %s", o.Key)
		}
	}
}

// Resuming an interrupted local write converges on one complete object rather
// than duplicating or concatenating.
func TestDisk_ResumeConverges(t *testing.T) {
	root := t.TempDir()
	s, err := disk.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	body := bytes.Repeat([]byte("bundle"), 2000)
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])

	broken := &storetest.Reader{Data: body, Fail: 4096, Err: os.ErrClosed}
	if _, err := s.Put(ctx, "acme/r.pck", broken, store.PutOptions{Size: int64(len(body)), Digest: digest}); err == nil {
		t.Fatal("expected the first attempt to fail")
	}

	res, err := s.Put(ctx, "acme/r.pck", bytes.NewReader(body[4096:]),
		store.PutOptions{Size: int64(len(body)), Digest: digest, Offset: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Resumed {
		t.Error("the write did not report itself as resumed")
	}
	var out bytes.Buffer
	if _, err := s.Get(ctx, "acme/r.pck", &out, store.GetOptions{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), body) {
		t.Fatalf("resumed object is %d bytes, want %d — a resume must converge, never concatenate", out.Len(), len(body))
	}
}

// A checkpoint describing progress the file cannot corroborate must restart
// rather than resume into a gap.
func TestDisk_ResumeBeyondWhatExistsRestarts(t *testing.T) {
	root := t.TempDir()
	s, err := disk.New(root)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("complete contents")
	sum := sha256.Sum256(body)

	res, err := s.Put(context.Background(), "acme/g.pck", bytes.NewReader(body),
		store.PutOptions{Size: int64(len(body)), Digest: hex.EncodeToString(sum[:]), Offset: 99999})
	if err != nil {
		t.Fatal(err)
	}
	if res.Resumed {
		t.Error("a checkpoint the file cannot corroborate should not be trusted")
	}
	var out bytes.Buffer
	if _, err := s.Get(context.Background(), "acme/g.pck", &out, store.GetOptions{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), body) {
		t.Fatalf("object is %q", out.String())
	}
}

func TestDisk_ProbeDistinguishesMissingFromUnwritable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-created-yet")
	s, err := disk.New(missing)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Access != store.AccessNone {
		t.Fatalf("a missing folder probed as %q", r.Access)
	}
	if !strings.Contains(r.Detail, missing) {
		t.Errorf("the failure does not name the path it looked for: %q", r.Detail)
	}

	if err := s.EnsureRoot(); err != nil {
		t.Fatal(err)
	}
	r, err = s.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Access != store.AccessWritable {
		t.Fatalf("after creating the folder the probe reported %q: %+v", r.Access, r)
	}
	if r.FreeBytes <= 0 {
		t.Error("a writable disk store should report free space")
	}
}

// A read-only credential is a legitimate configuration for browsing, and
// collapsing it into a failure would be wrong.
func TestDisk_ProbeReportsReadOnlyRatherThanFailing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can write to a read-only directory")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

	s, err := disk.New(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Access != store.AccessReadOnly {
		t.Fatalf("a read-only folder probed as %q", r.Access)
	}
}

// A key that escapes the root would let a malformed manifest write outside the
// folder the operator configured.
func TestDisk_KeysCannotEscapeTheRoot(t *testing.T) {
	root := t.TempDir()
	s, err := disk.New(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Put(context.Background(), "../../escaped.pck", strings.NewReader("x"), store.PutOptions{Size: 1})
	if err == nil {
		t.Fatal("a key traversing above the root was accepted")
	}
}

func TestDisk_ExpandsHome(t *testing.T) {
	s, err := disk.New("~/portcloak-test-root")
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if !strings.HasPrefix(s.Root(), home) {
		t.Fatalf("~ was not expanded: %s", s.Root())
	}
}
