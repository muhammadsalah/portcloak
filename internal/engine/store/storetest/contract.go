// Package storetest holds the BlobStore contract suite.
//
// There is one table and every backend runs it. Any divergence is a bug in the
// newest implementation, not a reason to fork the table — which is the whole
// point of having a contract rather than four sets of backend-specific tests.
package storetest

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"portcloak/internal/engine/store"
)

// Factory builds a fresh, empty store for one subtest.
type Factory func(t *testing.T) store.BlobStore

// RunContract exercises every behaviour the orchestrator relies on.
func RunContract(t *testing.T, newStore Factory) {
	t.Helper()

	t.Run("put and get round trip", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		body := []byte("a sealed bundle, more or less")
		sum := sha256.Sum256(body)

		res, err := s.Put(ctx, "acme/2026-01-02T0304-abc.pck", bytes.NewReader(body),
			store.PutOptions{Size: int64(len(body)), Digest: hex.EncodeToString(sum[:])})
		if err != nil {
			t.Fatal(err)
		}
		if res.Size != int64(len(body)) {
			t.Errorf("Put reported %d bytes, want %d", res.Size, len(body))
		}
		if res.Digest != hex.EncodeToString(sum[:]) {
			t.Errorf("Put reported digest %s", res.Digest)
		}

		var out bytes.Buffer
		got, err := s.Get(ctx, "acme/2026-01-02T0304-abc.pck", &out, store.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out.Bytes(), body) {
			t.Fatalf("read back %q", out.String())
		}
		if got.Digest != hex.EncodeToString(sum[:]) {
			t.Errorf("Get reported digest %s", got.Digest)
		}
	})

	// A zero-byte object is a real case: an empty realm's user file, or a
	// sidecar an interrupted job never filled in.
	t.Run("zero byte object", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		if _, err := s.Put(ctx, "empty/zero.pck", bytes.NewReader(nil), store.PutOptions{}); err != nil {
			t.Fatal(err)
		}
		info, err := s.Stat(ctx, "empty/zero.pck")
		if err != nil {
			t.Fatal(err)
		}
		if info.Size != 0 {
			t.Fatalf("size %d, want 0", info.Size)
		}
		var out bytes.Buffer
		if _, err := s.Get(ctx, "empty/zero.pck", &out, store.GetOptions{}); err != nil {
			t.Fatal(err)
		}
		if out.Len() != 0 {
			t.Fatalf("read %d bytes from a zero-byte object", out.Len())
		}
	})

	t.Run("missing key", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		if _, err := s.Stat(ctx, "nothing/here.pck"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("Stat on a missing key = %v, want ErrNotFound", err)
		}
		var out bytes.Buffer
		if _, err := s.Get(ctx, "nothing/here.pck", &out, store.GetOptions{}); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("Get on a missing key = %v, want ErrNotFound", err)
		}
		// Deleting what is not there has reached the desired end state.
		if err := s.Delete(ctx, "nothing/here.pck"); err != nil {
			t.Errorf("Delete on a missing key = %v, want success", err)
		}
	})

	t.Run("empty prefix lists nothing rather than failing", func(t *testing.T) {
		s := newStore(t)
		got, err := s.List(context.Background(), "no-such-realm/")
		if err != nil {
			t.Fatalf("listing an empty prefix failed: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("listing an empty prefix returned %d objects", len(got))
		}
	})

	t.Run("list is scoped and sorted", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		keys := []string{
			"acme/2026-01-02T0304-a.pck",
			"acme/2026-01-02T0304-a.manifest.json",
			"acme/2026-01-03T0304-b.pck",
			"partners/2026-01-02T0304-c.pck",
		}
		for _, k := range keys {
			if _, err := s.Put(ctx, k, strings.NewReader("x"), store.PutOptions{Size: 1}); err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.List(ctx, "acme/")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 {
			t.Fatalf("listing acme/ returned %d objects: %+v", len(got), got)
		}
		for i := 1; i < len(got); i++ {
			if got[i-1].Key > got[i].Key {
				t.Fatalf("listing is not sorted: %v", got)
			}
		}
		all, err := s.List(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 4 {
			t.Fatalf("listing everything returned %d objects", len(all))
		}
	})

	t.Run("delete removes", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		if _, err := s.Put(ctx, "acme/x.pck", strings.NewReader("x"), store.PutOptions{Size: 1}); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete(ctx, "acme/x.pck"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Stat(ctx, "acme/x.pck"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("object survived deletion: %v", err)
		}
	})

	// The digest is computed before the bundle reaches any backend, so a write
	// that does not match it is refused rather than committed.
	t.Run("digest mismatch is refused", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		_, err := s.Put(ctx, "acme/bad.pck", strings.NewReader("actual bytes"),
			store.PutOptions{Size: 12, Digest: strings.Repeat("00", 32)})
		if err == nil {
			t.Fatal("a write whose digest did not match was committed")
		}
		if _, statErr := s.Stat(ctx, "acme/bad.pck"); !errors.Is(statErr, store.ErrNotFound) {
			t.Fatal("a refused write left an object behind that looks complete")
		}
	})

	t.Run("overwrite replaces", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		if _, err := s.Put(ctx, "acme/x.pck", strings.NewReader("first"), store.PutOptions{Size: 5}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Put(ctx, "acme/x.pck", strings.NewReader("second"), store.PutOptions{Size: 6}); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if _, err := s.Get(ctx, "acme/x.pck", &out, store.GetOptions{}); err != nil {
			t.Fatal(err)
		}
		if out.String() != "second" {
			t.Fatalf("overwrite left %q", out.String())
		}
	})

	t.Run("large object streams", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		body := make([]byte, 3<<20)
		if _, err := rand.Read(body); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)

		var progressed int64
		if _, err := s.Put(ctx, "acme/big.pck", bytes.NewReader(body), store.PutOptions{
			Size:     int64(len(body)),
			Digest:   hex.EncodeToString(sum[:]),
			Progress: func(n int64) { progressed = n },
		}); err != nil {
			t.Fatal(err)
		}
		if progressed != int64(len(body)) {
			t.Errorf("progress ended at %d, want %d", progressed, len(body))
		}
		var out bytes.Buffer
		got, err := s.Get(ctx, "acme/big.pck", &out, store.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Digest != hex.EncodeToString(sum[:]) {
			t.Fatal("a large object did not round-trip intact")
		}
	})

	t.Run("get honours an offset", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		body := []byte("0123456789abcdef")
		if _, err := s.Put(ctx, "acme/off.pck", bytes.NewReader(body), store.PutOptions{Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if _, err := s.Get(ctx, "acme/off.pck", &out, store.GetOptions{Offset: 10}); err != nil {
			t.Fatal(err)
		}
		if out.String() != "abcdef" {
			t.Fatalf("offset read returned %q", out.String())
		}
	})

	t.Run("probe reports concrete access", func(t *testing.T) {
		s := newStore(t)
		r, err := s.Probe(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if r.Access != store.AccessWritable {
			t.Fatalf("probe reported %q for a writable store: %+v", r.Access, r)
		}
		if r.Integrity == "" {
			t.Error("probe should say which integrity method applies, not hide it")
		}
		// A probe leaves nothing behind, even a successful one.
		objects, err := s.List(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range objects {
			if strings.Contains(o.Key, "probe") {
				t.Errorf("the probe left %s behind", o.Key)
			}
		}
	})

	t.Run("endpoint is identifiable", func(t *testing.T) {
		s := newStore(t)
		if s.Endpoint() == "" {
			t.Fatal("a store with no endpoint cannot be circuit-broken or named in an error")
		}
	})
}

// RunResumableContract covers the operations that make an interrupted upload
// converge on one complete object rather than a duplicate.
func RunResumableContract(t *testing.T, newStore func(t *testing.T) store.ResumableStore) {
	t.Helper()

	t.Run("multipart round trip", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		key := "acme/multi.pck"

		id, err := s.InitMultipart(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		part := make([]byte, s.PartSize())
		for i := range part {
			part[i] = byte(i % 251)
		}
		var parts []store.PartETag
		var whole []byte
		for n := 1; n <= 3; n++ {
			chunk := part
			if n == 3 {
				chunk = part[:1024]
			}
			p, err := s.PutPart(ctx, id, key, n, bytes.NewReader(chunk), int64(len(chunk)))
			if err != nil {
				t.Fatal(err)
			}
			parts = append(parts, p)
			whole = append(whole, chunk...)
		}
		if _, err := s.CompleteMultipart(ctx, id, key, parts); err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		if _, err := s.Get(ctx, key, &out, store.GetOptions{}); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out.Bytes(), whole) {
			t.Fatalf("assembled object is %d bytes, want %d", out.Len(), len(whole))
		}
	})

	t.Run("resume lists what the server already holds", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		key := "acme/resume.pck"

		id, err := s.InitMultipart(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		chunk := bytes.Repeat([]byte("p"), int(s.PartSize()))
		for n := 1; n <= 2; n++ {
			if _, err := s.PutPart(ctx, id, key, n, bytes.NewReader(chunk), int64(len(chunk))); err != nil {
				t.Fatal(err)
			}
		}
		// This is the moment PortCloak is killed and restarted: it knows only
		// the upload id, and has to re-establish the rest from the server.
		held, err := s.ListParts(ctx, id, key)
		if err != nil {
			t.Fatal(err)
		}
		if len(held) != 2 {
			t.Fatalf("server reports %d parts, want 2", len(held))
		}
		for n := 3; n <= 3; n++ {
			p, err := s.PutPart(ctx, id, key, n, bytes.NewReader(chunk), int64(len(chunk)))
			if err != nil {
				t.Fatal(err)
			}
			held = append(held, p)
		}
		if _, err := s.CompleteMultipart(ctx, id, key, held); err != nil {
			t.Fatal(err)
		}
		info, err := s.Stat(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size != 3*s.PartSize() {
			t.Fatalf("resumed upload produced %d bytes, want %d", info.Size, 3*s.PartSize())
		}
	})

	// A cancelled job must not leave billable incomplete uploads behind.
	t.Run("abort leaves nothing", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		key := "acme/aborted.pck"

		id, err := s.InitMultipart(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.PutPart(ctx, id, key, 1, strings.NewReader("partial"), 7); err != nil {
			t.Fatal(err)
		}
		if err := s.AbortMultipart(ctx, id, key); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Stat(ctx, key); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("an aborted upload left an object: %v", err)
		}
		if _, err := s.ListParts(ctx, id, key); err == nil {
			t.Error("an aborted upload still reports parts")
		}
	})
}

// Reader is a helper for tests that need a reader they can interrupt.
type Reader struct {
	Data  []byte
	Fail  int
	Err   error
	pos   int
}

func (r *Reader) Read(p []byte) (int, error) {
	if r.Fail > 0 && r.pos >= r.Fail {
		return 0, r.Err
	}
	if r.pos >= len(r.Data) {
		return 0, io.EOF
	}
	n := copy(p, r.Data[r.pos:])
	if r.Fail > 0 && r.pos+n > r.Fail {
		n = r.Fail - r.pos
	}
	r.pos += n
	return n, nil
}
