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

// secretSource is where a secret may come from, in order.
//
// The order is deliberate and is the same everywhere a secret is read, so that
// one rule covers a capture passphrase, an SSH password and an Admin API
// password rather than three that drift.
type secretSource struct {
	// value is the secret given directly on the command line.
	//
	// It is first because a caller who typed it meant it, and it is documented
	// everywhere as the least good of these: argv is visible in `ps` to every
	// user on the machine, and it lands in shell history. Using it warns, once,
	// on stderr — see warnArgvSecret. It exists because the alternative was
	// people writing secrets to temporary files to get past a flag that would
	// not take one, which is worse.
	value string
	file  string
	stdin bool
	// env names the variable checked before prompting. It is the CI path: the
	// value reaches the process without appearing in argv.
	env string
	// prompt asks on the terminal even when the caller gave no other source.
	//
	// It is opt-in for the definition commands, because most definitions have no
	// secret at all — a local install, a disk folder, an S3 bucket reached
	// through an instance role — and prompting for one would ask a question with
	// no right answer.
	prompt bool
	// required marks a secret the command cannot proceed without, which is what
	// makes a prompt the correct last resort rather than an intrusion. Sealing a
	// snapshot needs a passphrase; describing a Keycloak may not need anything.
	required bool
}

// warnArgvSecret says once what a secret on the command line costs.
//
// Said rather than refused: it is the operator's machine and their call, and a
// tool that refuses a thing people need finds them working around it in ways it
// cannot see. But it is not said quietly either — the failure mode is somebody
// pasting a production password into a shared shell's history without ever
// having been told.
func warnArgvSecret(r *run, what string) {
	if r == nil || r.g.quiet {
		return
	}
	fmt.Fprintf(r.s.Err,
		"pcloak: %s was given on the command line, where `ps` shows it to every user on this\n"+
			"  machine and your shell records it in history. Prefer a file, stdin, or the\n"+
			"  interactive prompt instead.\n", what)
}

// read resolves a secret from the first source that has one.
//
// confirm decides whether a mistyped value is recoverable. Sealing gets two
// prompts and a comparison, because a snapshot sealed with a typo cannot be
// opened by anybody, ever. Opening gets one, because a wrong passphrase there
// simply fails and can be tried again.
func (src secretSource) read(r *run, prompt string, confirmTwice bool) (string, error) {
	switch {
	case src.value != "":
		return src.value, nil

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
	if !src.prompt && !src.required {
		// Nothing was offered and nothing was asked for. An empty secret is a
		// real answer here: most definitions have none, and prompting for one
		// would ask a question with no right answer.
		return "", nil
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
