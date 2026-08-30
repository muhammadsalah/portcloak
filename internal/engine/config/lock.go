// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"portcloak/internal/engine/resil"
)

// The home folder is shared mutable state, and until there was a command line
// only one process could ever hold it: the desktop app refuses a second launch
// through Wails' SingleInstance, which raises the existing window instead.
//
// pcloak is by definition a second process. Two of them over one ~/.portcloak do
// not merely interleave — StartupSweep rewrites every running job to interrupted
// and deletes the working directories and index files of snapshots it cannot see
// are open, because the set it keeps is its own process's. A capture running in
// the window would be marked interrupted and have its staging area swept while
// it was still writing into it.
//
// So the guarantee moves down here, where both front ends can take it, and
// SingleInstance goes back to being what it actually is: the courtesy of raising
// a window rather than printing an error.

// LockMode is what a caller is claiming.
//
// The two tiers are not "reads" and "writes", which was the obvious split and
// the wrong one. Two PortCloaks capturing at once are fine: each writes its own
// job record and its own staging directory, and one snapshot holds one realm, so
// there is nothing shared to corrupt. What is genuinely unsafe between processes
// is narrower, and there are exactly two of them:
//
//   - the startup sweep, which rewrites every running job to interrupted and
//     deletes the working directories of snapshots it cannot see are open;
//   - a read-modify-write of config.yaml, where the second writer's copy of the
//     file predates the first writer's change and silently drops it.
//
// So being here is Shared, and Exclusive means "I am the only PortCloak here"
// and is held for as long as one of those two things takes and no longer. A
// window that held Exclusive for its whole session would refuse every terminal
// command for hours, which is the opposite of the point.
type LockMode int

const (
	// LockShared says a PortCloak is active on this folder. Every process takes
	// it and keeps it: the window for its session, a command for its run.
	LockShared LockMode = iota
	// LockExclusive says no other PortCloak is active. It is taken briefly, for
	// the startup sweep and for a change to config.yaml, and released.
	LockExclusive
)

// Holder is what a process records about itself so the next one can say who is
// in the way. It is diagnostics only — the lock is the lock, and this is written
// after it has already been won.
type Holder struct {
	PID     int       `json:"pid"`
	Program string    `json:"program"`
	Command string    `json:"command,omitempty"`
	Since   time.Time `json:"since"`
}

// Lock is a held claim on a home folder. Release drops it.
type Lock struct {
	mode LockMode
	path string
	file *os.File
}

// Mode reports what the lock was taken as.
func (l *Lock) Mode() LockMode { return l.mode }

// Acquire claims the home folder, returning a lock that must be released.
//
// The claim is an OS advisory lock on a file rather than a pidfile, for one
// reason that decides it: the kernel drops a flock when the process holding it
// dies, however it dies. A pidfile survives a crash, wedges every later run, and
// the only cure is telling an operator to delete a file by hand — which teaches
// them to delete it whenever anything looks stuck, including when it is not.
//
// The lock file is created if it is missing and never removed. Removing it would
// race: a process that unlinked the file on release would leave a second process
// holding a lock on an inode nothing can find, and a third would then create a
// new file and take a lock that excludes neither.
func Acquire(home Home, mode LockMode, who Holder) (*Lock, error) {
	path := home.LockFile()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, resil.Fatal("claim the PortCloak folder",
			fmt.Sprintf("PortCloak could not open %s.", path), err)
	}

	held, err := lockFile(f, mode)
	if err != nil {
		// Read the holder before closing: whatever it says was written by
		// whoever is in the way, and it is the only thing that turns "busy"
		// into a sentence naming what to quit.
		other := readHolder(f)
		_ = f.Close()
		return nil, busy(home, other, err)
	}
	if !held {
		other := readHolder(f)
		_ = f.Close()
		return nil, busy(home, other, nil)
	}

	// The description goes in only under an exclusive lock. Shared holders are
	// concurrent by design, so each would overwrite the last and the file would
	// name whichever reader happened to be most recent — worse than empty,
	// because it reads as authoritative.
	if mode == LockExclusive {
		who.PID = os.Getpid()
		who.Since = time.Now()
		if b, err := json.Marshal(who); err == nil {
			_ = f.Truncate(0)
			if _, err := f.WriteAt(b, 0); err != nil {
				// A lock that is held but undescribed is still a lock. Losing
				// the diagnostics is not worth failing an operation over.
				_ = err
			}
		}
	}
	return &Lock{mode: mode, path: path, file: f}, nil
}

// Sweep runs fn only if this process is momentarily the only PortCloak here.
//
// It exists because the startup sweep is opportunistic housekeeping, not a
// precondition: a folder that another PortCloak is using does not need sweeping,
// because whatever the sweep would tidy up is that process's live state. So a
// refused claim is a reason to skip, never a reason to fail.
//
// A separate descriptor is used rather than upgrading the caller's shared one.
// flock conflicts are between open file descriptions and not between processes,
// so upgrading in place is possible on Unix and is a different operation on
// Windows; taking a fresh claim behaves identically on both, and the cost is one
// open.
func Sweep(home Home, who Holder, fn func()) {
	l, err := Acquire(home, LockExclusive, who)
	if err != nil {
		return
	}
	defer func() { _ = l.Release() }()
	fn()
}

// Release drops the claim.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	f := l.file
	l.file = nil
	// Closing the descriptor releases the lock on every platform this runs on.
	// unlockFile is called first anyway, so a caller who keeps the file open for
	// some later reason does not silently keep the claim as well.
	_ = unlockFile(f)
	return f.Close()
}

// readHolder reports what the current holder wrote about itself, if anything.
//
// Everything here is best-effort. The file may be empty because the holder is a
// reader, truncated because it was caught mid-write, or written by a version
// that recorded different fields. None of that is worth an error: the caller is
// already reporting a refusal, and an unnamed holder only makes the sentence
// vaguer.
func readHolder(f *os.File) Holder {
	var h Holder
	b := make([]byte, 4096)
	n, err := f.ReadAt(b, 0)
	if n == 0 || (err != nil && n == 0) {
		return h
	}
	_ = json.Unmarshal(b[:n], &h)
	return h
}

// busy turns a refused claim into something an operator can act on: who has it,
// since when, why two would be a problem, and what still works meanwhile.
func busy(home Home, other Holder, cause error) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Another PortCloak is using %s.", home.Root)

	if other.Program != "" && other.PID != 0 {
		what := other.Program
		if other.Command != "" {
			what += " " + other.Command
		}
		fmt.Fprintf(&b, " %s (pid %d) has held it", what, other.PID)
		if !other.Since.IsZero() {
			fmt.Fprintf(&b, " since %s", other.Since.Format("15:04"))
		}
		b.WriteString(".")
	}

	b.WriteString(" This change needs the folder to itself: two processes editing config.yaml both read it before either writes, so the second one silently drops the first one's change.")

	return resil.Fatal("claim the PortCloak folder", b.String(), cause).
		WithAdvice("Quit the desktop app, or wait for the run that holds it to finish. Everything else works while it is held — capturing, restoring, and reading the library.")
}
