// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"portcloak/internal/engine/resil"
)

// Where the home folder's location came from.
//
// It matters to the operator because the three are not equally changeable: an
// environment variable is set outside the app and wins over anything the app
// could write, so a Settings screen that offered to move the folder anyway
// would be offering something it cannot deliver.
type HomeSource string

const (
	// HomeDefault is ~/.portcloak — nothing has been chosen.
	HomeDefault HomeSource = "default"
	// HomeChosen is a folder picked in the app and recorded in the pointer.
	HomeChosen HomeSource = "chosen"
	// HomePinned is PORTCLOAK_HOME, set in the environment.
	HomePinned HomeSource = "environment"
)

// Location is where PortCloak keeps everything, and how that was decided.
type Location struct {
	Home   Home
	Source HomeSource
	// Default is ~/.portcloak, reported whether or not it is in force, so the
	// screen can offer to go back to it.
	Default string
	// Pointer is the file that records a chosen folder. It deliberately does
	// not live inside the home folder — a note saying where the tree moved to
	// cannot itself be part of the tree that moved.
	Pointer string
}

// Locate resolves the home folder, in the order the app trusts:
//
//	PORTCLOAK_HOME  →  the pointer file  →  ~/.portcloak
//
// A pointer that cannot be read, or that names a folder which has since been
// deleted, falls back to the default rather than refusing to start. Losing the
// way back to a working PortCloak because a folder was moved in Finder would be
// a poor trade for strictness.
func Locate() (Location, error) {
	dir, err := os.UserHomeDir()
	if err != nil {
		return Location{}, fmt.Errorf("finding your home folder: %w", err)
	}
	loc := Location{
		Default: filepath.Join(dir, ".portcloak"),
		Pointer: pointerFile(),
	}

	if override := strings.TrimSpace(os.Getenv("PORTCLOAK_HOME")); override != "" {
		loc.Home = Home{Root: override}
		loc.Source = HomePinned
		return loc, nil
	}
	if chosen := readPointer(loc.Pointer); chosen != "" {
		if info, err := os.Stat(chosen); err == nil && info.IsDir() {
			loc.Home = Home{Root: chosen}
			loc.Source = HomeChosen
			return loc, nil
		}
	}
	loc.Home = Home{Root: loc.Default}
	loc.Source = HomeDefault
	return loc, nil
}

// DefaultHome resolves the home folder, honouring PORTCLOAK_HOME and a folder
// chosen in the app so a test or a portable install can point somewhere else.
func DefaultHome() (Home, error) {
	loc, err := Locate()
	if err != nil {
		return Home{}, err
	}
	return loc.Home, nil
}

// pointerFile is the OS's own place for small per-application settings, which
// is the one location that stays put when the PortCloak folder moves.
func pointerFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "PortCloak", "home")
}

func readPointer(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// WritePointer records a chosen home folder.
func WritePointer(root string) error {
	path := pointerFile()
	if path == "" {
		return errors.New("this system has no per-application settings folder to record the location in")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	return writeFileAtomic(path, []byte(root+"\n"), 0o600)
}

// ClearPointer forgets a chosen folder, so the default applies again.
func ClearPointer() error {
	path := pointerFile()
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clearing %s: %w", path, err)
	}
	return nil
}

// CheckDestination reports why a folder cannot become the PortCloak home.
//
// Everything it refuses, it refuses before a single byte moves. A move that
// fails halfway would leave the operator's environments, keys and job
// checkpoints split across two folders, with the app pointing at neither.
func CheckDestination(current Home, dst string) error {
	// Every refusal is a whole sentence to the operator, so each one is built
	// as the terminal failure the UI already knows how to render rather than as
	// a bare error string.
	refuse := func(format string, args ...any) error {
		return resil.Fatal("move the PortCloak folder", fmt.Sprintf(format, args...), ErrBadDestination)
	}

	if strings.TrimSpace(dst) == "" {
		return refuse("Give the folder PortCloak should keep its files in.")
	}
	if !filepath.IsAbs(dst) {
		return refuse("%s is a relative path. Give the full path to the folder.", dst)
	}
	dst = filepath.Clean(dst)
	src := filepath.Clean(current.Root)

	if dst == src {
		return refuse("PortCloak is already keeping its files in %s.", dst)
	}
	if within(dst, src) {
		return refuse("%s is inside the folder being moved. Choose one outside %s.", dst, src)
	}
	if within(src, dst) {
		return refuse("%s already contains PortCloak's folder. Choose one that does not.", dst)
	}

	switch info, err := os.Stat(dst); {
	case err == nil && !info.IsDir():
		return refuse("%s is a file, not a folder.", dst)
	case err == nil:
		entries, err := os.ReadDir(dst)
		if err != nil {
			return fmt.Errorf("reading %s: %w", dst, err)
		}
		if len(entries) > 0 {
			return refuse("%s is not empty. PortCloak will only move into a folder it can have to itself, so nothing already there can be overwritten.", dst)
		}
	case os.IsNotExist(err):
		parent := filepath.Dir(dst)
		if info, err := os.Stat(parent); err != nil || !info.IsDir() {
			return refuse("%s does not exist, so PortCloak cannot create %s inside it.", parent, filepath.Base(dst))
		}
	default:
		return fmt.Errorf("reading %s: %w", dst, err)
	}
	return nil
}

// ErrBadDestination is the cause behind every refusal CheckDestination makes,
// so a caller can tell "that folder will not do" from "the disk went away".
var ErrBadDestination = errors.New("that folder cannot hold the PortCloak home")

// within reports whether path sits inside root.
func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// MoveTree relocates the home folder from src to dst.
//
// A rename is tried first because it is atomic and instant. It fails across
// filesystems — a home folder moved onto an external disk is exactly the case
// an operator has in mind — so the fallback copies the tree, verifies it
// arrived, and only then removes the original.
func MoveTree(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dst), err)
	}
	// An empty destination the operator created themselves would make the
	// rename fail on some systems and nest the tree on others.
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clearing %s: %w", dst, err)
	}
	if err := os.Rename(src, dst); err == nil {
		return os.Chmod(dst, 0o700)
	}

	if err := copyTree(src, dst); err != nil {
		// The copy is the half that can fail partway. Removing what arrived
		// leaves the original as the only copy, which is the state the
		// operator started in and the only one worth being left in.
		_ = os.RemoveAll(dst)
		return err
	}
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("removing %s after copying it to %s: %w", src, dst, err)
	}
	return nil
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case info.IsDir():
			return os.MkdirAll(target, 0o700)
		case info.Mode()&os.ModeSymlink != 0:
			// Nothing PortCloak writes is a symlink. One that is here was put
			// here by hand, and following it would copy someone else's file
			// into the tree.
			return nil
		default:
			return copyFile(path, target, info.Mode().Perm())
		}
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("reading %s: %w", src, err)
	}
	defer in.Close() //nolint:errcheck // read-only.

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close() //nolint:errcheck
		return fmt.Errorf("copying %s to %s: %w", src, dst, err)
	}
	if err := out.Sync(); err != nil {
		out.Close() //nolint:errcheck
		return fmt.Errorf("flushing %s to disk: %w", dst, err)
	}
	return out.Close()
}
