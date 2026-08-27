package reliable_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"
	"time"

	"portcloak/internal/engine/reliable"
	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/store"
)

// flakyStore fails a set number of times before succeeding.
type flakyStore struct {
	failures int
	attempts int
	received []byte
	err      error
}

func (f *flakyStore) Probe(context.Context) (store.Reach, error) {
	f.attempts++
	return store.Reach{Access: store.AccessWritable}, f.err
}
func (f *flakyStore) Stat(context.Context, string) (store.ObjectInfo, error) {
	f.attempts++
	if f.attempts <= f.failures {
		return store.ObjectInfo{}, syscall.ECONNRESET
	}
	return store.ObjectInfo{Key: "k"}, nil
}
func (f *flakyStore) Put(_ context.Context, key string, r io.Reader, _ store.PutOptions) (store.PutResult, error) {
	f.attempts++
	b, _ := io.ReadAll(r)
	f.received = b
	if f.attempts <= f.failures {
		return store.PutResult{}, syscall.ECONNRESET
	}
	return store.PutResult{Key: key, Size: int64(len(b))}, nil
}
func (f *flakyStore) Get(context.Context, string, io.Writer, store.GetOptions) (store.GetResult, error) {
	f.attempts++
	if f.attempts <= f.failures {
		return store.GetResult{}, syscall.ECONNRESET
	}
	return store.GetResult{}, nil
}
func (f *flakyStore) List(context.Context, string) ([]store.ObjectInfo, error) {
	f.attempts++
	if f.attempts <= f.failures {
		return nil, syscall.ECONNRESET
	}
	return nil, nil
}
func (f *flakyStore) Delete(context.Context, string) error {
	f.attempts++
	if f.attempts <= f.failures {
		return syscall.ECONNRESET
	}
	return nil
}
func (f *flakyStore) Endpoint() string { return "flaky://endpoint" }
func (f *flakyStore) Close() error     { return nil }

func fastOptions() reliable.Options {
	return reliable.Options{Policy: resil.Policy{
		MaxAttempts: 4, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond,
	}}
}

func TestWrapStore_RetriesTransientFailures(t *testing.T) {
	inner := &flakyStore{failures: 2}
	s := reliable.WrapStore(inner, fastOptions())

	if _, err := s.Stat(context.Background(), "acme/x.pck"); err != nil {
		t.Fatalf("a transient failure was not retried: %v", err)
	}
	if inner.attempts != 3 {
		t.Errorf("made %d attempts, want 3", inner.attempts)
	}
}

// A stream consumed by a failed attempt cannot be re-read, so retrying it would
// upload a truncated object. Resume, not retry, is the mechanism there.
func TestWrapStore_DoesNotRetryAnUnreplayableStream(t *testing.T) {
	inner := &flakyStore{failures: 1}
	s := reliable.WrapStore(inner, fastOptions())

	// A pipe cannot be rewound.
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte("some bundle bytes"))
		_ = pw.Close()
	}()

	_, err := s.Put(context.Background(), "acme/x.pck", pr, store.PutOptions{})
	if err == nil {
		t.Fatal("an unreplayable stream was retried into a success, which means it was truncated")
	}
	if inner.attempts != 1 {
		t.Fatalf("made %d attempts on an unreplayable stream, want 1", inner.attempts)
	}
}

// A file, which is what a sealed bundle actually is, can be rewound — so it is
// retried, and the retry sends the whole object rather than the remainder.
func TestWrapStore_RetriesAReplayableStreamFromTheStart(t *testing.T) {
	inner := &flakyStore{failures: 1}
	s := reliable.WrapStore(inner, fastOptions())

	body := []byte("the whole sealed bundle")
	if _, err := s.Put(context.Background(), "acme/x.pck", bytes.NewReader(body), store.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if inner.attempts != 2 {
		t.Fatalf("made %d attempts, want 2", inner.attempts)
	}
	if !bytes.Equal(inner.received, body) {
		t.Fatalf("the retry sent %q, not the whole object", inner.received)
	}
}

// A probe is an operator asking a question. The honest answer to "is this
// reachable right now" is the first one, not the one that arrived after thirty
// seconds of quiet retrying.
func TestWrapStore_DoesNotRetryAProbe(t *testing.T) {
	inner := &flakyStore{err: errors.New("no route to host")}
	s := reliable.WrapStore(inner, fastOptions())

	_, _ = s.Probe(context.Background())
	if inner.attempts != 1 {
		t.Fatalf("a probe was attempted %d times", inner.attempts)
	}
}

func TestWrapStore_GivesUpWithAResumableError(t *testing.T) {
	inner := &flakyStore{failures: 99}
	s := reliable.WrapStore(inner, fastOptions())

	err := s.Delete(context.Background(), "acme/x.pck")
	if err == nil {
		t.Fatal("a permanently failing operation reported success")
	}
	if !resil.IsRetryable(err) {
		t.Error("an exhausted budget should still read as resumable")
	}
	if !strings.Contains(err.Error(), "4 attempts") {
		t.Errorf("the message does not say how hard it tried: %v", err)
	}
}

func TestWrapStore_SharesABreakerAcrossOperations(t *testing.T) {
	inner := &flakyStore{failures: 99}
	opts := fastOptions()
	opts.Breakers = resil.NewRegistry(resil.BreakerConfig{Threshold: 2, Cooldown: time.Minute}, nil)
	s := reliable.WrapStore(inner, opts)

	_ = s.Delete(context.Background(), "acme/x.pck")

	// The breaker opened during the first operation, so the second is refused
	// immediately rather than repeating the whole budget.
	before := inner.attempts
	err := s.Delete(context.Background(), "acme/y.pck")
	if err == nil {
		t.Fatal("the second operation should have been refused by the breaker")
	}
	if inner.attempts != before {
		t.Errorf("the breaker let %d more attempts through", inner.attempts-before)
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("the operator-facing message does not explain the pause: %v", err)
	}
}

func TestWrapStore_PassesThroughToTheInnerStore(t *testing.T) {
	inner := &flakyStore{}
	s := reliable.WrapStore(inner, fastOptions())
	if s.Unwrap() != store.BlobStore(inner) {
		t.Fatal("the wrapper does not expose the store it wraps")
	}
	if s.Endpoint() != inner.Endpoint() {
		t.Fatal("the endpoint has to survive wrapping, or the breaker keys on the wrong thing")
	}
}
