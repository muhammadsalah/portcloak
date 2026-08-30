// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"strconv"
	"strings"

	"portcloak/internal/app"
	"portcloak/internal/engine/config"
	"portcloak/internal/engine/resil"
)

// exitError carries a code a command has already decided, with a message it has
// already rendered. It exists so a command can say "this is a precondition, not
// a crash" without inventing a second error hierarchy alongside resil's.
type exitError struct {
	code    int
	message string
	cause   error
}

func (e *exitError) Error() string {
	if e.message != "" {
		return e.message
	}
	if e.cause != nil {
		return e.cause.Error()
	}
	return "pcloak failed"
}

func (e *exitError) Unwrap() error { return e.cause }

// exitWith builds an error that carries its own exit code and message.
func exitWith(code int, message string) error {
	return &exitError{code: code, message: message}
}

// precondition is a refusal before any work was done. Nothing was written to the
// target, which is the fact the code is promising.
func precondition(message string) error {
	return &exitError{code: ExitPrecondition, message: message}
}

// failure wraps an engine error at a chosen code, keeping the engine's own
// sentence and advice.
func failure(code int, err error) error {
	f := app.Fail(err)
	msg := "pcloak: " + f.Message
	if f.Hint != "" {
		msg += "\n  " + f.Hint
	}
	return &exitError{code: code, message: msg, cause: err}
}

// codeFor classifies an error the commands did not classify themselves.
//
// The mapping is deliberately narrow. Only two things are inferred — a busy home
// folder and a retryable failure — because those are the two a script acts on
// differently, and everything else is honestly just "it failed". Guessing more
// finely would mean a script branching on a distinction the tool cannot actually
// make.
func codeFor(err error, f *app.Failure) int {
	switch {
	case errors.Is(err, errHomeBusy):
		return ExitBusy
	case f != nil && f.Retryable:
		return ExitRetryable
	case resil.IsRetryable(err):
		return ExitRetryable
	default:
		return ExitFailed
	}
}

// errHomeBusy marks a refusal to share the home folder, so it can be given its
// own exit code without matching on the message text.
var errHomeBusy = errors.New("another PortCloak is using this folder")

// itoa and joinNames keep the ambiguity messages readable without pulling
// fmt into every call site.
func itoa(n int) string { return strconv.Itoa(n) }

func joinNames(v []string) string { return strings.Join(v, "\n  ") }

// notFound turns the engine's bare "not found" into a sentence naming what was
// asked for and what exists, which is the difference between a typo taking five
// seconds to spot and taking a `config show`.
func notFound(kind, name string, known []string) error {
	var b strings.Builder
	b.WriteString("pcloak: there is no " + kind + " called \"" + name + "\".")
	switch {
	case len(known) == 0:
		b.WriteString("\n  None is configured. Add one in the PortCloak app.")
	default:
		b.WriteString("\n  Configured: " + strings.Join(known, ", "))
	}
	return &exitError{code: ExitPrecondition, message: b.String(), cause: config.ErrNotFound}
}
