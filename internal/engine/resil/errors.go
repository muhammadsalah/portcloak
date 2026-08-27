// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package resil is the cross-cutting resilience layer: bounded retry with
// backoff and jitter, a per-endpoint circuit breaker, and the error
// classification both depend on.
//
// Every remote operation in PortCloak goes through it. That is a structural
// decision rather than a stylistic one: retry logic sprinkled at call sites
// drifts, and the drift that matters — treating an unretryable operation as
// retryable — is invisible until it wastes a minute burying the message an
// operator needed immediately.
package resil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
)

// Class is what PortCloak decided to do about an error.
type Class string

const (
	// Retryable means waiting and trying again could reasonably work: a
	// timeout, a reset connection, a 5xx, a throttle.
	Retryable Class = "retryable"
	// Terminal means trying again will produce the same answer: a rejected
	// credential, a missing bucket, a malformed request.
	Terminal Class = "terminal"
)

// Error is the engine's operator-facing error.
//
// It carries three things a raw wrapped error does not: whether waiting would
// help, a sentence about the operator's system, and what to do next. "context
// deadline exceeded" tells an operator nothing; "the export ran for 20 minutes
// without producing output; the database may be under load" tells them what to
// check.
type Error struct {
	// Op is what was being attempted, in the operator's terms.
	Op string
	// Message is the sentence shown in the UI.
	Message string
	// Advice is what to do next, where there is something to do.
	Advice string
	// Endpoint identifies the remote thing involved, for the circuit breaker
	// and for the ledger.
	Endpoint string
	Class    Class
	Cause    error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Op, e.Cause)
	}
	return e.Op
}

func (e *Error) Unwrap() error { return e.Cause }

// Retryable reports whether waiting and trying again could work.
func (e *Error) Retryable() bool { return e.Class == Retryable }

// Hint is what the UI shows under the message.
func (e *Error) Hint() string { return e.Advice }

// Retry builds a retryable error.
func Retry(op, message string, cause error) *Error {
	return &Error{Op: op, Message: message, Class: Retryable, Cause: cause}
}

// Fatal builds a terminal error.
func Fatal(op, message string, cause error) *Error {
	return &Error{Op: op, Message: message, Class: Terminal, Cause: cause}
}

// WithAdvice attaches the next step.
func (e *Error) WithAdvice(advice string) *Error {
	e.Advice = advice
	return e
}

// WithEndpoint attaches the remote endpoint this error came from.
func (e *Error) WithEndpoint(endpoint string) *Error {
	e.Endpoint = endpoint
	return e
}

// IsRetryable reports whether an error anywhere in the chain says waiting could
// help.
//
// The default for an unclassified error is terminal. That direction is
// deliberate: a new error type nobody has classified yet surfaces to the
// operator instead of silently looping.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// A cancelled context is the operator's decision, and an expired
		// deadline has already had its budget.
		return false
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Class == Retryable
	}
	return false
}

// Hint pulls the next step out of an error chain, if one carries it.
func Hint(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Advice
	}
	return ""
}

// Endpoint pulls the endpoint out of an error chain.
func Endpoint(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Endpoint
	}
	return ""
}

// Classifier decides whether an adapter's raw error is worth retrying. Each
// adapter supplies its own, because "connection reset" means something
// different to SFTP than a 409 does to S3.
type Classifier func(error) Class

// ClassifyNetwork is the shared base every adapter's classifier builds on. It
// recognises the transport-level failures that are the same everywhere.
func ClassifyNetwork(err error) Class {
	if err == nil {
		return Terminal
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Terminal
	}

	// An unexpected EOF mid-stream is a dropped connection, not a decision.
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return Retryable
	}
	for _, target := range []error{syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.ECONNABORTED,
		syscall.EPIPE, syscall.ETIMEDOUT, syscall.EHOSTUNREACH, syscall.ENETUNREACH, syscall.ENETDOWN} {
		if errors.Is(err, target) {
			return Retryable
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return Retryable
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		// A temporary resolver failure is worth another try; NXDOMAIN is not.
		if dnsErr.IsTemporary || dnsErr.IsTimeout {
			return Retryable
		}
		return Terminal
	}

	// Some transports only ever hand back a string.
	msg := strings.ToLower(err.Error())
	for _, s := range []string{
		"connection reset", "connection refused", "broken pipe", "i/o timeout",
		"unexpected eof", "use of closed network connection", "no route to host",
		"tls handshake timeout", "server misbehaving", "temporary failure",
		"network is unreachable", "operation timed out", "eof",
	} {
		if strings.Contains(msg, s) {
			return Retryable
		}
	}
	return Terminal
}

// ClassifyHTTPStatus applies the shared rule for status codes: server-side and
// throttling responses are worth another try, and everything the client got
// wrong is not.
func ClassifyHTTPStatus(code int) Class {
	switch {
	case code == 408, code == 425, code == 429:
		return Retryable
	case code >= 500 && code != 501:
		return Retryable
	default:
		return Terminal
	}
}
