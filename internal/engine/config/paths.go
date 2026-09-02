// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package config owns everything PortCloak knows about itself: the environments
// and storage definitions an operator has described, their preferences, and the
// per-job checkpoints that let an interrupted transfer resume.
//
// All of it lives in plain files under ~/.portcloak/ (NFR-11) — or wherever
// the operator has moved that folder to, or pointed one run at with --home; see
// location.go. There is no database
// for tool state — a readable file can be diffed, committed, hand-edited and
// copied between machines, none of which is pleasant against an opaque store.
// SQLite appears elsewhere in the engine, and only ever for throwaway
// inspection indexes.
//
// Secrets never appear here. Each credential is held in the OS keychain and
// referenced by a handle of the form keychain://portcloak/<kind>/<name>.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Home is the PortCloak directory tree and the single place that knows where
// anything lives.
type Home struct {
	Root string
	// ConfigPath overrides where config.yaml is read from, leaving the rest of
	// the tree — jobs, logs, indexes, working files — where it was.
	//
	// It exists for the command line's --config, whose use is a checkout that
	// carries the environment and storage definitions while the state stays on
	// the machine. Empty is the ordinary case: config.yaml inside Root.
	ConfigPath string
}

// ConfigFile is the environments, storage definitions and preferences.
func (h Home) ConfigFile() string {
	if h.ConfigPath != "" {
		return h.ConfigPath
	}
	return filepath.Join(h.Root, "config.yaml")
}

// JobsDir holds one JSON file per job, carrying its state and checkpoints.
func (h Home) JobsDir() string { return filepath.Join(h.Root, "jobs") }

// JobFile is the checkpoint file for one job.
func (h Home) JobFile(id string) string { return filepath.Join(h.JobsDir(), id+".json") }

// LogsDir holds the structured, redacted log.
func (h Home) LogsDir() string { return filepath.Join(h.Root, "logs") }

// LogFile is the current log.
func (h Home) LogFile() string { return filepath.Join(h.LogsDir(), "portcloak.log") }

// AuditFile is the append-only record of what the tool has done.
func (h Home) AuditFile() string { return filepath.Join(h.Root, "audit.log") }

// IndexDir holds session-scoped inspection indexes. Deleting this directory at
// any moment is always safe, by design (NFR-10).
func (h Home) IndexDir() string { return filepath.Join(h.Root, "index") }

// IndexFile is one snapshot's throwaway index.
func (h Home) IndexFile(snapshotID string) string {
	return filepath.Join(h.IndexDir(), snapshotID+".sqlite")
}

// WorkDir holds decrypted working files while a snapshot is open or a job runs.
func (h Home) WorkDir() string { return filepath.Join(h.Root, "work") }

// LockFile is the advisory lock that makes "one PortCloak per home" true for the
// command line as well as for the window. See lock.go.
//
// It sits inside the tree rather than beside it, unlike the pointer file: a
// claim on this folder is meaningless anywhere else, and a folder that has been
// moved takes its lock with it.
func (h Home) LockFile() string { return filepath.Join(h.Root, "portcloak.lock") }

// Dirs is every directory the tree contains.
func (h Home) Dirs() []string {
	return []string{h.Root, h.JobsDir(), h.LogsDir(), h.IndexDir(), h.WorkDir()}
}

// Bootstrap creates the tree, with 0700 on every directory, and writes an empty
// config.yaml if there is not one yet.
//
// It runs on every start rather than only the first, so deleting a directory by
// hand cannot brick the app; an existing tree is left exactly as it is. The one
// thing it will not create is a config file named from outside the tree — see
// below.
func (h Home) Bootstrap() error {
	for _, d := range h.Dirs() {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
		// An existing directory may have been created with a looser mode by an
		// earlier version or by a hand-run mkdir.
		if err := os.Chmod(d, 0o700); err != nil {
			return fmt.Errorf("restricting permissions on %s: %w", d, err)
		}
	}
	// A config file named from outside the tree is never created here. The
	// template is a courtesy for a first run against ~/.portcloak; writing it
	// to a path the operator typed would turn "read this file" into "create
	// this file", which is how a mistyped --config silently starts an empty
	// PortCloak instead of reporting that the file is not there.
	if h.ConfigPath != "" {
		return nil
	}
	if _, err := os.Stat(h.ConfigFile()); os.IsNotExist(err) {
		if err := writeFileAtomic(h.ConfigFile(), []byte(emptyConfigYAML), 0o600); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("reading %s: %w", h.ConfigFile(), err)
	}
	return nil
}

// Writable reports whether the home folder can actually be written to. An
// operator whose home folder is read-only gets told so with the path, rather
// than silently losing every change they make (UC-O9 E1).
func (h Home) Writable() error {
	probe := filepath.Join(h.Root, ".write-probe")
	if err := os.WriteFile(probe, []byte("portcloak"), 0o600); err != nil {
		return fmt.Errorf("PortCloak cannot write to %s: %w", h.Root, err)
	}
	return os.Remove(probe)
}

const emptyConfigYAML = `# PortCloak configuration.
#
# This file is safe to read, diff and commit: it holds no secrets. Every
# credential lives in this machine's keychain and is referenced here by a
# handle of the form keychain://portcloak/<kind>/<name>.
#
# Copying this file to another machine is supported. The credentials will not
# come with it, and PortCloak will ask for them again rather than failing
# obscurely.

version: 1

preferences: {}

environments: []

storage: []
`
