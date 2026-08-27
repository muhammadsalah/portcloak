// Package reliable wraps target and storage adapters so every remote operation
// goes through the resilience layer.
//
// It is a decorator rather than retry code inside each adapter, for two
// reasons. An adapter cannot forget to use it, because it never sees the
// unwrapped path at all; and the classification stays in one place, where
// "which failures are worth another try" can be read as a single decision
// instead of reconstructed from a dozen call sites.
//
// The registry in internal/app wraps everything it hands the orchestrator, and
// an architecture test asserts nothing reaches the orchestrator unwrapped.
package reliable

import (
	"context"
	"io"

	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/store"
	"portcloak/internal/engine/target"
)

// Options configure a wrap.
type Options struct {
	Policy   resil.Policy
	Breakers *resil.Registry
	Reporter *obs.Reporter
}

func (o Options) doer(endpoint string) resil.Doer {
	opts := []resil.Option{}
	if o.Breakers != nil {
		opts = append(opts, resil.WithBreaker(o.Breakers.For(endpoint)))
	}
	if o.Reporter != nil {
		opts = append(opts, resil.WithReporter(o.Reporter))
	}
	return resil.New(endpoint, o.Policy, opts...)
}

// Store wraps a BlobStore.
type Store struct {
	inner store.BlobStore
	doer  resil.Doer
}

// WrapStore returns a BlobStore whose every operation retries appropriately.
func WrapStore(inner store.BlobStore, opts Options) *Store {
	return &Store{inner: inner, doer: opts.doer(inner.Endpoint())}
}

// Unwrap exposes the underlying store, for a caller that genuinely needs the
// concrete type — the storage browser's "create this folder", for instance.
func (s *Store) Unwrap() store.BlobStore { return s.inner }

func (s *Store) Endpoint() string { return s.inner.Endpoint() }
func (s *Store) Close() error     { return s.inner.Close() }

func (s *Store) Probe(ctx context.Context) (store.Reach, error) {
	// A probe is not retried. It is an operator asking a question, and the
	// honest answer to "is this reachable right now" is the first answer, not
	// the one that arrived after thirty seconds of quiet retrying.
	return s.inner.Probe(ctx)
}

func (s *Store) Stat(ctx context.Context, key string) (out store.ObjectInfo, err error) {
	err = s.doer.Do(ctx, "read object metadata", func(ctx context.Context) error {
		out, err = s.inner.Stat(ctx, key)
		return err
	})
	return out, err
}

// Put retries only when the reader can be replayed.
//
// This is the distinction that matters most in the whole package: a stream
// consumed by a failed attempt cannot be re-read, so retrying it would upload a
// truncated object. Resume, driven by the checkpoint, is the mechanism for
// that case — not a retry loop.
func (s *Store) Put(ctx context.Context, key string, r io.Reader, opts store.PutOptions) (out store.PutResult, err error) {
	if !replayable(r) {
		return s.inner.Put(ctx, key, r, opts)
	}
	err = s.doer.Do(ctx, "upload the snapshot", func(ctx context.Context) error {
		if err := rewind(r); err != nil {
			return err
		}
		out, err = s.inner.Put(ctx, key, r, opts)
		return err
	})
	return out, err
}

func (s *Store) Get(ctx context.Context, key string, w io.Writer, opts store.GetOptions) (out store.GetResult, err error) {
	err = s.doer.Do(ctx, "download the snapshot", func(ctx context.Context) error {
		out, err = s.inner.Get(ctx, key, w, opts)
		return err
	})
	return out, err
}

func (s *Store) List(ctx context.Context, prefix string) (out []store.ObjectInfo, err error) {
	err = s.doer.Do(ctx, "list the storage", func(ctx context.Context) error {
		out, err = s.inner.List(ctx, prefix)
		return err
	})
	return out, err
}

func (s *Store) Delete(ctx context.Context, key string) error {
	return s.doer.Do(ctx, "delete the snapshot", func(ctx context.Context) error {
		return s.inner.Delete(ctx, key)
	})
}

// Executor wraps a target adapter.
type Executor struct {
	inner target.Executor
	doer  resil.Doer
}

// WrapExecutor returns an Executor whose remote operations retry appropriately.
func WrapExecutor(inner target.Executor, endpoint string, opts Options) *Executor {
	return &Executor{inner: inner, doer: opts.doer(endpoint)}
}

// Unwrap exposes the underlying executor, for the orphan sweep which needs the
// concrete Sweeper.
func (e *Executor) Unwrap() target.Executor { return e.inner }

func (e *Executor) Probe(ctx context.Context) (target.TargetFacts, error) {
	// Like a storage probe, this is a question and not a transfer.
	return e.inner.Probe(ctx)
}

func (e *Executor) Prepare(ctx context.Context, opts target.PrepareOptions) (out target.ExecContext, err error) {
	err = e.doer.Do(ctx, "prepare the execution context", func(ctx context.Context) error {
		out, err = e.inner.Prepare(ctx, opts)
		return err
	})
	return out, err
}

// Run is not retried here.
//
// Re-running a command is a decision about that command, not about the
// transport: kc.sh export is expensive and has side effects on the target's
// filesystem, and the one failure worth retrying — the port race — is retried
// by the orchestrator with fresh ports, which a blind retry could not do.
func (e *Executor) Run(ctx context.Context, cmd target.Command) (target.ExecResult, error) {
	return e.inner.Run(ctx, cmd)
}

// FetchDir is not retried wholesale.
//
// A partially consumed sink cannot be replayed without duplicating artifacts,
// so the checkpoint is the recovery mechanism: a resumed job re-fetches from
// the last fully-received artifact, which per-user files make granular.
func (e *Executor) FetchDir(ctx context.Context, remote string, sink target.ArtifactSink) error {
	return e.inner.FetchDir(ctx, remote, sink)
}

func (e *Executor) PushFile(ctx context.Context, remote string, size int64, r io.Reader) error {
	if !replayable(r) {
		return e.inner.PushFile(ctx, remote, size, r)
	}
	return e.doer.Do(ctx, "send the file", func(ctx context.Context) error {
		if err := rewind(r); err != nil {
			return err
		}
		return e.inner.PushFile(ctx, remote, size, r)
	})
}

// Teardown is retried, because leaving a clone behind is the worst outcome the
// tool has and a transient API error is a poor reason to accept it.
func (e *Executor) Teardown(ctx context.Context) error {
	return e.doer.Do(ctx, "destroy the ephemeral clone", func(ctx context.Context) error {
		return e.inner.Teardown(ctx)
	})
}

func (e *Executor) Close() error { return e.inner.Close() }

// FindOrphans passes through to a sweeper when the wrapped executor is one.
func (e *Executor) FindOrphans(ctx context.Context) ([]target.Orphan, error) {
	sweeper, ok := e.inner.(target.Sweeper)
	if !ok {
		return nil, nil
	}
	var out []target.Orphan
	err := e.doer.Do(ctx, "check for orphaned clones", func(ctx context.Context) error {
		var err error
		out, err = sweeper.FindOrphans(ctx)
		return err
	})
	return out, err
}

// RemoveOrphan passes through to a sweeper when the wrapped executor is one.
func (e *Executor) RemoveOrphan(ctx context.Context, ref string) error {
	sweeper, ok := e.inner.(target.Sweeper)
	if !ok {
		return nil
	}
	return e.doer.Do(ctx, "remove an orphaned clone", func(ctx context.Context) error {
		return sweeper.RemoveOrphan(ctx, ref)
	})
}

// replayable reports whether a reader can be read again from the start, which
// is what makes retrying a write safe rather than corrupting.
func replayable(r io.Reader) bool {
	_, ok := r.(io.Seeker)
	return ok
}

func rewind(r io.Reader) error {
	s, ok := r.(io.Seeker)
	if !ok {
		return nil
	}
	_, err := s.Seek(0, io.SeekStart)
	return err
}
