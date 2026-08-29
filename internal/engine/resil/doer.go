// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package resil

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"portcloak/internal/engine/obs"
)

// Policy bounds how hard PortCloak tries.
type Policy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	// Classifier decides retryability for this adapter. Nil falls back to
	// ClassifyNetwork.
	Classifier Classifier
}

// DefaultPolicy is the starting point every adapter narrows.
func DefaultPolicy() Policy {
	return Policy{MaxAttempts: 5, BaseDelay: 500 * time.Millisecond, MaxDelay: 30 * time.Second}
}

// Doer runs an operation with retry, backoff and circuit breaking.
//
// It is an interface so a test can substitute an implementation that does not
// sleep, and so the architecture test has something concrete to assert every
// remote call site goes through.
type Doer interface {
	Do(ctx context.Context, op string, fn func(context.Context) error) error
}

// Clock is the time source, so backoff can be asserted deterministically.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		// Cancellation during a backoff wait returns promptly rather than
		// sleeping out the interval — otherwise a Cancel button appears to do
		// nothing for thirty seconds.
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Retrier is the standard Doer.
type Retrier struct {
	policy   Policy
	breaker  *Breaker
	clock    Clock
	rand     func() float64
	reporter *obs.Reporter
	endpoint string
}

// Option configures a Retrier.
type Option func(*Retrier)

// WithClock replaces the time source.
func WithClock(c Clock) Option { return func(r *Retrier) { r.clock = c } }

// WithRand replaces the jitter source, for deterministic tests.
func WithRand(f func() float64) Option { return func(r *Retrier) { r.rand = f } }

// WithReporter makes retries and breaker state visible to the operator, so a
// wait is never an unexplained spinner.
func WithReporter(rep *obs.Reporter) Option { return func(r *Retrier) { r.reporter = rep } }

// WithBreaker shares a circuit breaker across several Retriers pointed at the
// same endpoint.
func WithBreaker(b *Breaker) Option { return func(r *Retrier) { r.breaker = b } }

// New builds a Retrier for one endpoint.
func New(endpoint string, p Policy, opts ...Option) *Retrier {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 1
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = 500 * time.Millisecond
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = 30 * time.Second
	}
	if p.Classifier == nil {
		p.Classifier = ClassifyNetwork
	}
	r := &Retrier{
		policy:   p,
		clock:    realClock{},
		rand:     rand.Float64,
		endpoint: endpoint,
	}
	for _, o := range opts {
		o(r)
	}
	if r.breaker == nil {
		r.breaker = NewBreaker(endpoint, BreakerConfig{}, r.clock)
	}
	return r
}

// Do runs fn, retrying while the error is retryable and the budget holds.
func (r *Retrier) Do(ctx context.Context, op string, fn func(context.Context) error) error {
	if err := r.breaker.Allow(); err != nil {
		if r.reporter != nil {
			r.reporter.BreakerOpen(r.endpoint, r.breaker.Remaining())
		}
		return err
	}

	var last error
	for attempt := 1; attempt <= r.policy.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := fn(ctx)
		if err == nil {
			r.breaker.Success()
			return nil
		}
		last = r.classify(op, err)

		if !IsRetryable(last) {
			// A terminal failure still counts against the breaker only when it
			// is the endpoint's fault. A rejected credential is not, and
			// opening a circuit over it would hide the real message behind a
			// cooldown.
			if isEndpointFault(last) {
				r.breaker.Failure()
			}
			return last
		}
		r.breaker.Failure()

		if attempt == r.policy.MaxAttempts {
			break
		}
		if err := r.breaker.Allow(); err != nil {
			if r.reporter != nil {
				r.reporter.BreakerOpen(r.endpoint, r.breaker.Remaining())
			}
			return err
		}

		delay := r.backoff(attempt)
		if r.reporter != nil {
			r.reporter.Retry(op, attempt, delay, last.Error())
		}
		if err := r.clock.Sleep(ctx, delay); err != nil {
			return err
		}
	}

	return &Error{
		Op:       op,
		Message:  fmt.Sprintf("%s did not succeed after %d attempts. The last problem was: %s", op, r.policy.MaxAttempts, last),
		Advice:   "The job kept its checkpoint, so resuming continues from where it stopped rather than starting over.",
		Class:    Retryable,
		Endpoint: r.endpoint,
		Cause:    last,
	}
}

func (r *Retrier) classify(op string, err error) error {
	var typed *Error
	if errors.As(err, &typed) {
		if typed.Endpoint == "" {
			typed.Endpoint = r.endpoint
		}
		return typed
	}
	class := r.policy.Classifier(err)
	return &Error{
		Op:       op,
		Message:  fmt.Sprintf("%s failed: %v", op, err),
		Class:    class,
		Endpoint: r.endpoint,
		Cause:    err,
	}
}

// backoff is exponential with full jitter: the delay is drawn uniformly from
// [0, exponential). Full jitter rather than a fixed multiple is what stops
// several transfers against one backend from re-converging on the same retry
// instant after a shared outage.
func (r *Retrier) backoff(attempt int) time.Duration {
	d := r.policy.BaseDelay << (attempt - 1)
	if d > r.policy.MaxDelay || d <= 0 {
		d = r.policy.MaxDelay
	}
	return time.Duration(r.rand() * float64(d))
}

func isEndpointFault(err error) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	// Authentication and request-shape failures are ours, not the endpoint's.
	return e.Class == Retryable
}

// Direct is a Doer that runs an operation once. It exists for the local
// filesystem, where there is nothing to retry and pretending otherwise would
// just add a layer.
type Direct struct{}

// Do runs fn once.
func (Direct) Do(ctx context.Context, _ string, fn func(context.Context) error) error {
	return fn(ctx)
}

// BreakerConfig tunes when an endpoint is taken out of service.
type BreakerConfig struct {
	Threshold int
	Cooldown  time.Duration
}

// Breaker stops PortCloak hammering an endpoint that is down, and stops the
// endpoint being hit by a reconnect storm when it comes back.
type Breaker struct {
	mu sync.Mutex

	endpoint string
	cfg      BreakerConfig
	clock    Clock

	failures int
	openedAt time.Time
	halfOpen bool
}

// NewBreaker builds a breaker for one endpoint.
func NewBreaker(endpoint string, cfg BreakerConfig, clock Clock) *Breaker {
	if cfg.Threshold <= 0 {
		cfg.Threshold = 5
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = 30 * time.Second
	}
	if clock == nil {
		clock = realClock{}
	}
	return &Breaker{endpoint: endpoint, cfg: cfg, clock: clock}
}

// Allow reports whether an attempt may proceed.
func (b *Breaker) Allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.openedAt.IsZero() {
		return nil
	}
	elapsed := b.clock.Now().Sub(b.openedAt)
	if elapsed < b.cfg.Cooldown {
		remaining := b.cfg.Cooldown - elapsed
		return &Error{
			Op:       "connect",
			Message:  fmt.Sprintf("%s has been unreachable for %s. PortCloak has paused rather than keep hammering it, and will try again in %s.", b.endpoint, roundish(elapsed), roundish(remaining)),
			Advice:   "Nothing is lost. The job keeps its checkpoint and resumes from where it stopped.",
			Class:    Retryable,
			Endpoint: b.endpoint,
		}
	}
	// Cooldown elapsed: let exactly one attempt through to decide.
	b.halfOpen = true
	return nil
}

// Success closes the breaker.
func (b *Breaker) Success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.openedAt = time.Time{}
	b.halfOpen = false
}

// Failure records a failed attempt and may open the breaker.
func (b *Breaker) Failure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.halfOpen {
		// The probe failed; go straight back to open and restart the cooldown.
		b.halfOpen = false
		b.openedAt = b.clock.Now()
		return
	}
	b.failures++
	if b.failures >= b.cfg.Threshold && b.openedAt.IsZero() {
		b.openedAt = b.clock.Now()
	}
}

// Open reports whether the breaker is currently refusing attempts.
func (b *Breaker) Open() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openedAt.IsZero() {
		return false
	}
	return b.clock.Now().Sub(b.openedAt) < b.cfg.Cooldown
}

// Remaining is how long until the next probe is allowed.
func (b *Breaker) Remaining() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openedAt.IsZero() {
		return 0
	}
	r := b.cfg.Cooldown - b.clock.Now().Sub(b.openedAt)
	if r < 0 {
		return 0
	}
	return r
}

func roundish(d time.Duration) time.Duration {
	switch {
	case d >= time.Minute:
		return d.Round(time.Second)
	default:
		return d.Round(100 * time.Millisecond)
	}
}

// Registry hands out one breaker per endpoint, so several jobs against the same
// storage share the decision to back off.
type Registry struct {
	mu       sync.Mutex
	cfg      BreakerConfig
	clock    Clock
	breakers map[string]*Breaker
}

// NewRegistry builds a breaker registry.
func NewRegistry(cfg BreakerConfig, clock Clock) *Registry {
	return &Registry{cfg: cfg, clock: clock, breakers: map[string]*Breaker{}}
}

// For returns the breaker for an endpoint, creating it on first use.
func (r *Registry) For(endpoint string) *Breaker {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.breakers[endpoint]
	if !ok {
		b = NewBreaker(endpoint, r.cfg, r.clock)
		r.breakers[endpoint] = b
	}
	return b
}
