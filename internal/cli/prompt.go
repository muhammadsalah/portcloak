// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"portcloak/internal/engine/config"
)

// Every prompt in pcloak has a flag that satisfies it. That is the rule the
// whole surface is built to: a run that would block waiting for a person is a
// run that cannot go in a pipeline, and a tool that cannot go in a pipeline is
// not much of a command line.
//
// So on a terminal these ask, and everywhere else they refuse — naming the flag
// — rather than hanging on a question nothing will answer.

// errNotATerminal is returned where a prompt would have been shown.
var errNotATerminal = errors.New("there is no terminal to ask on")

// confirm asks a yes/no question. --yes answers it, and a non-terminal declines
// rather than blocks.
func confirm(r *run, format string, a ...any) bool {
	if r.g.yes {
		return true
	}
	if !isTerminal(r.s.Err) {
		return false
	}
	fmt.Fprintf(r.s.Err, format+" [y/N] ", a...)
	line, err := bufio.NewReader(r.s.In).ReadString('\n')
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// confirmPhrase requires an exact phrase to be typed.
//
// It is for the handful of operations whose cost is unbounded and unrecoverable:
// writing a snapshot in the clear, overwriting a live realm, deleting the key
// every snapshot in a storage was sealed with. A y/N there is one keystroke away
// from a mistake nothing can undo, and typing the phrase is the pause.
func confirmPhrase(r *run, phrase, format string, a ...any) bool {
	if !isTerminal(r.s.Err) {
		return false
	}
	fmt.Fprintf(r.s.Err, format+"\nType %q to continue: ", append(a, phrase)...)
	line, err := bufio.NewReader(r.s.In).ReadString('\n')
	if err != nil {
		return false
	}
	return strings.TrimSpace(line) == phrase
}

// secretSource is where a passphrase may come from, in order.
type secretSource struct {
	file  string
	stdin bool
	// env names the variable checked before prompting. It is the CI path: the
	// value reaches the process without appearing in argv, where `ps` shows it
	// to every user on the machine.
	env string
}

// read resolves a passphrase without it ever being a command-line argument.
//
// confirm decides whether a mistyped value is recoverable. Sealing gets two
// prompts and a comparison, because a snapshot sealed with a typo cannot be
// opened by anybody, ever. Opening gets one, because a wrong passphrase there
// simply fails and can be tried again.
func (src secretSource) read(r *run, prompt string, confirmTwice bool) (string, error) {
	switch {
	case src.file != "" && src.file != "-":
		b, err := os.ReadFile(src.file)
		if err != nil {
			return "", err
		}
		return firstLine(string(b)), nil

	case src.stdin || src.file == "-":
		b, err := readAll(r.s.In)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}

	if src.env != "" {
		if v := os.Getenv(src.env); v != "" {
			return v, nil
		}
	}

	f, ok := r.s.Err.(*os.File)
	if !ok || !isTerminal(r.s.Err) {
		return "", errNotATerminal
	}

	fmt.Fprint(f, prompt+": ")
	first, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(f)
	if err != nil {
		return "", err
	}
	if !confirmTwice {
		return string(first), nil
	}

	fmt.Fprint(f, "Confirm: ")
	second, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(f)
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", errors.New("the two passphrases did not match")
	}
	return string(first), nil
}

// firstLine is what a secret file holds. A trailing newline is how every editor
// ends a file, and including it in a passphrase would seal a snapshot that the
// same file cannot then open.
func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// readLines returns the non-empty, non-comment lines of a file, for the flags
// that take a list — age recipients, mostly.
func readLines(path string, in *bufio.Reader) ([]string, error) {
	var src *bufio.Scanner
	if path == "-" {
		src = bufio.NewScanner(in)
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		src = bufio.NewScanner(f)
	}
	var out []string
	for src.Scan() {
		line := strings.TrimSpace(src.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, src.Err()
}

// missingSecret is the refusal shown where a prompt cannot be answered.
func missingSecret(what string, flags ...string) error {
	return precondition(fmt.Sprintf(
		"pcloak: %s is needed, and there is no terminal to ask on.\n  Supply it with %s, or set %s.",
		what, strings.Join(flags, " or "), "PORTCLOAK_PASSPHRASE"))
}

// keyCredential is the handle a named key's secret is filed under.
func keyCredential(kind config.KeyKind, name string) string {
	return config.SuggestKeyHandle(kind, name)
}
