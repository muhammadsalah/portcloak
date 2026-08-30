// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/inspect"
	"portcloak/internal/engine/obs"
)

// renderer writes a command's result.
//
// Results go to stdout and everything else to stderr, without exception. That is
// what lets `pcloak snapshot list --json | jq` work while a run is still
// narrating its phases, and it is the difference between a tool that can be
// piped and one that can only be watched.
type renderer struct {
	out  io.Writer
	err  io.Writer
	json bool
	tty  bool
}

func newRenderer(s Streams, g *globals) *renderer {
	return &renderer{
		out:  s.Out,
		err:  s.Err,
		json: g.json,
		tty:  !g.noColor && isTerminal(s.Out),
	}
}

// JSON prints a value as the machine-readable answer.
//
// The value is a controller's own struct wherever there is one. They are all
// JSON-tagged already, all redacted already, and all covered by the tests that
// keep the desktop frontend in step with them — so the CLI's machine output is
// the same contract rather than a second one that can drift.
func (r *renderer) JSON(v any) error {
	enc := json.NewEncoder(r.out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Line prints one line of human output.
func (r *renderer) Line(format string, a ...any) {
	fmt.Fprintf(r.out, format+"\n", a...)
}

// Note prints a sentence of context to stderr, where it cannot corrupt a pipe.
func (r *renderer) Note(format string, a ...any) {
	fmt.Fprintf(r.err, format+"\n", a...)
}

// Table prints aligned columns. An empty body prints nothing at all rather than
// a header over a void, which reads as a bug in the filter.
func (r *renderer) Table(header []string, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	w := tabwriter.NewWriter(r.out, 0, 0, 2, ' ', 0)
	if len(header) > 0 {
		fmt.Fprintln(w, strings.Join(header, "\t"))
	}
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	_ = w.Flush()
}

// dash is what an empty cell shows. A blank one makes a table look truncated.
const dash = "—"

func cell(s string) string {
	if strings.TrimSpace(s) == "" {
		return dash
	}
	return s
}

// yesNo renders a boolean the way a table column reads best.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// isTerminal reports whether w is a terminal, which decides whether progress
// redraws in place or prints one line per transition.
//
// Anything that is not an *os.File — a test's buffer, a pipe wrapper — is not a
// terminal, which is the answer that makes output deterministic when something
// is capturing it.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// phaseLine is one rendered progress line.
func phaseLine(e obs.Event) string {
	label := obs.PhaseLabel(e.Phase)
	switch e.Kind {
	case obs.EventPhaseStarted:
		return label + "…"
	case obs.EventPhaseCompleted:
		return label + " — " + e.Message
	case obs.EventPhaseFailed:
		return label + " failed — " + e.Message
	case obs.EventRetry:
		return fmt.Sprintf("%s — attempt %d: %s", label, e.Attempt, e.Message)
	case obs.EventBreakerOpen:
		return fmt.Sprintf("%s — paused, retrying in %s", label, e.RetryIn.Round(0))
	case obs.EventCloneCreated:
		return "clone created — " + e.Item
	case obs.EventCloneDestroyed:
		return "clone destroyed — " + e.Item
	default:
		return e.Message
	}
}

// readiness renders whether a definition can be used yet.
//
// It is a softer question than whether the file is valid: a definition can be
// well-formed and still be missing the one credential this machine never
// received, which is the ordinary state after copying config.yaml between
// machines.
func readiness(r config.Readiness) string {
	if r.Ready {
		return "ready"
	}
	if r.Reason != "" {
		return r.Reason
	}
	return "not ready"
}

// presence renders whether the secret behind a handle is on this machine.
//
// A configuration copied between machines brings its handles and not its
// secrets, deliberately — so "the handle is there, the value is not" is an
// ordinary state that has to read as one rather than as a fault.
func presence(present bool, handle string) string {
	if handle == "" {
		return dash
	}
	if present {
		return handle + " (on this machine)"
	}
	return handle + " (not on this machine)"
}

// names projects a slice to the names in it, for the "configured: a, b, c" line
// a not-found error carries.
func names[T any](items []T, of func(T) string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, of(it))
	}
	return out
}

// humanBytes renders a size the way a person reads one.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// sealedAs says how a snapshot was sealed, and says the dangerous case loudly.
//
// An unencrypted bundle holds unmasked client secrets, LDAP bind credentials and
// RSA private signing keys in the clear; holding the file is equivalent to
// holding the realm. It carries a warning everywhere it appears, and a listing is
// somewhere it appears.
func sealedAs(e inspect.Entry) string {
	if !e.Encrypted {
		return "UNENCRYPTED"
	}
	if e.EncryptionMode != "" {
		return string(e.EncryptionMode)
	}
	return "encrypted"
}

// readAll drains a reader, tolerating a nil one so a command that never expects
// stdin does not have to guard every call.
func readAll(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	return io.ReadAll(r)
}
