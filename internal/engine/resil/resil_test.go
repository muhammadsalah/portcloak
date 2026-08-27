// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package resil

import (
	"context"
	"errors"
	"io"
	"syscall"
	"testing"
	"time"
)

// fakeClock lets backoff be asserted without waiting for it.
type fakeClock struct {
	now    time.Time
	slept  []time.Duration
	cancel func()
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.slept = append(c.slept, d)
	c.now = c.now.Add(d)
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	return ctx.Err()
}

func TestRetrier_RetriesRetryableAndStopsAtBudget(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	r := New("sftp://backup", Policy{MaxAttempts: 4, BaseDelay: time.Second, MaxDelay: time.Minute},
		WithClock(clk), WithRand(func() float64 { return 1 }))

	attempts := 0
	err := r.Do(context.Background(), "fetch", func(context.Context) error {
		attempts++
		return syscall.ECONNRESET
	})
	if attempts != 4 {
		t.Fatalf("made %d attempts, want the full budget of 4", attempts)
	}
	if err == nil {
		t.Fatal("expected an error after the budget was exhausted")
	}
	if !IsRetryable(err) {
		t.Error("an exhausted budget should still be resumable, not terminal")
	}
	// Exponential with full jitter, and the jitter pinned to its maximum.
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	if len(clk.slept) != len(want) {
		t.Fatalf("slept %v, want %v", clk.slept, want)
	}
	for i := range want {
		if clk.slept[i] != want[i] {
			t.Errorf("backoff %d was %v, want %v", i, clk.slept[i], want[i])
		}
	}
}

// Retrying a rejected credential wastes a minute and buries the message the
// operator needs immediately.
func TestRetrier_TerminalErrorIsNotRetried(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	r := New("s3://bucket", DefaultPolicy(), WithClock(clk))

	attempts := 0
	err := r.Do(context.Background(), "put", func(context.Context) error {
		attempts++
		return Fatal("put", "the access key was rejected", nil)
	})
	if attempts != 1 {
		t.Fatalf("made %d attempts on a rejected credential, want 1", attempts)
	}
	if IsRetryable(err) {
		t.Error("a rejected credential must not be reported as retryable")
	}
	if len(clk.slept) != 0 {
		t.Error("a terminal error should not wait")
	}
}

// The default for an unclassified error is terminal, so a new error type
// surfaces instead of silently looping.
func TestClassify_UnknownIsTerminal(t *testing.T) {
	if got := ClassifyNetwork(errors.New("something nobody has classified")); got != Terminal {
		t.Fatalf("got %v, want Terminal", got)
	}
	if got := IsRetryable(errors.New("bare error")); got {
		t.Fatal("a bare error must not be treated as retryable")
	}
}

func TestClassify_NetworkTable(t *testing.T) {
	cases := []struct {
		err  error
		want Class
	}{
		{syscall.ECONNRESET, Retryable},
		{syscall.ECONNREFUSED, Retryable},
		{syscall.EPIPE, Retryable},
		{syscall.ETIMEDOUT, Retryable},
		{io.ErrUnexpectedEOF, Retryable},
		{errors.New("read: connection reset by peer"), Retryable},
		{errors.New("i/o timeout"), Retryable},
		{context.Canceled, Terminal},
		{context.DeadlineExceeded, Terminal},
		{errors.New("permission denied"), Terminal},
		{errors.New("no such bucket"), Terminal},
	}
	for _, c := range cases {
		if got := ClassifyNetwork(c.err); got != c.want {
			t.Errorf("ClassifyNetwork(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestClassify_HTTPStatus(t *testing.T) {
	retry := []int{408, 425, 429, 500, 502, 503, 504}
	for _, c := range retry {
		if got := ClassifyHTTPStatus(c); got != Retryable {
			t.Errorf("status %d classified %v", c, got)
		}
	}
	terminal := []int{400, 401, 403, 404, 409, 412, 501}
	for _, c := range terminal {
		if got := ClassifyHTTPStatus(c); got != Terminal {
			t.Errorf("status %d classified %v", c, got)
		}
	}
}

// A Cancel button that appears to do nothing for thirty seconds is a bug.
func TestRetrier_CancellationDuringBackoffReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clk := &fakeClock{now: time.Unix(0, 0), cancel: cancel}
	r := New("ssh://kc-01", Policy{MaxAttempts: 10, BaseDelay: time.Hour, MaxDelay: time.Hour},
		WithClock(clk), WithRand(func() float64 { return 1 }))

	attempts := 0
	err := r.Do(ctx, "fetch", func(context.Context) error {
		attempts++
		return syscall.ECONNRESET
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("made %d attempts after cancellation, want 1", attempts)
	}
}

func TestBreaker_OpensRecoversAndSaysSo(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	b := NewBreaker("s3.eu-west-1", BreakerConfig{Threshold: 3, Cooldown: 2 * time.Minute}, clk)

	for i := 0; i < 2; i++ {
		b.Failure()
		if b.Open() {
			t.Fatalf("breaker opened after %d failures, threshold is 3", i+1)
		}
	}
	b.Failure()
	if !b.Open() {
		t.Fatal("breaker did not open at the threshold")
	}

	err := b.Allow()
	if err == nil {
		t.Fatal("an open breaker should refuse an attempt")
	}
	if !IsRetryable(err) {
		t.Error("an open breaker is a pause, not a terminal failure")
	}
	msg := err.Error()
	for _, want := range []string{"s3.eu-west-1", "unreachable", "try again"} {
		if !contains(msg, want) {
			t.Errorf("the operator-facing message is missing %q: %s", want, msg)
		}
	}

	// After the cooldown, one probe is allowed through and success closes it,
	// with no operator action.
	clk.now = clk.now.Add(3 * time.Minute)
	if err := b.Allow(); err != nil {
		t.Fatalf("the breaker should allow a probe after the cooldown: %v", err)
	}
	b.Success()
	if b.Open() {
		t.Fatal("a successful probe should have closed the breaker")
	}
}

func TestBreaker_FailedProbeReopensWithAFreshCooldown(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	b := NewBreaker("azurite", BreakerConfig{Threshold: 1, Cooldown: time.Minute}, clk)
	b.Failure()
	clk.now = clk.now.Add(2 * time.Minute)

	if err := b.Allow(); err != nil {
		t.Fatal("probe should have been allowed")
	}
	b.Failure()
	if !b.Open() {
		t.Fatal("a failed probe should reopen the breaker")
	}
	if got := b.Remaining(); got != time.Minute {
		t.Errorf("cooldown restarted at %v, want a full minute", got)
	}
}

func TestRegistry_SharesOneBreakerPerEndpoint(t *testing.T) {
	r := NewRegistry(BreakerConfig{Threshold: 1, Cooldown: time.Minute}, &fakeClock{now: time.Unix(0, 0)})
	a := r.For("s3://bucket")
	b := r.For("s3://bucket")
	if a != b {
		t.Fatal("two jobs against the same endpoint got separate breakers")
	}
	if r.For("s3://other") == a {
		t.Fatal("different endpoints must not share a breaker")
	}
}

func TestError_CarriesAdviceAndEndpoint(t *testing.T) {
	err := Retry("upload", "the connection dropped at 62%", io.ErrUnexpectedEOF).
		WithAdvice("Resume picks up from block 41.").
		WithEndpoint("azurite")
	if Hint(err) != "Resume picks up from block 41." {
		t.Errorf("hint lost: %q", Hint(err))
	}
	if Endpoint(err) != "azurite" {
		t.Errorf("endpoint lost: %q", Endpoint(err))
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Error("the cause is no longer reachable through the chain")
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
