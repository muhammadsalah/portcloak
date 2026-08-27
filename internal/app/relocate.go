// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"errors"
	"fmt"
	"path/filepath"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/resil"
)

// Relocate moves the PortCloak folder — config.yaml, job checkpoints, the audit
// log, the logs and the working directories — to dst, and rebinds everything
// that reads from it.
//
// It takes effect immediately rather than on the next launch. A setting that
// says one thing while the running application does another is the kind of
// half-applied state that costs an operator a support call, and the alternative
// — moving the tree out from under a live engine and asking for a restart —
// leaves every open file handle pointing at a folder that is no longer there.
//
// The cost is that it is only allowed when nothing is in flight. A job holds
// paths under the old root for its whole life, and an open snapshot has a
// decrypted working directory and an index file under it.
func (e *Engine) Relocate(dst string) error {
	loc, err := config.Locate()
	if err != nil {
		return err
	}
	if loc.Source == config.HomePinned {
		return resil.Fatal("move the PortCloak folder",
			"PORTCLOAK_HOME is set in the environment, and it wins over anything chosen here. Unset it and restart PortCloak to choose a folder from this screen.",
			errors.New("PORTCLOAK_HOME is set"))
	}
	if err := e.idleForRelocation(); err != nil {
		return err
	}

	src := e.Home()
	dst = filepath.Clean(dst)
	if err := config.CheckDestination(src, dst); err != nil {
		return err
	}

	// The log file is closed first, because Windows will not rename a directory
	// with an open file inside it. Records written during the move are dropped;
	// nothing else is running, so there are none worth keeping.
	if err := e.Log.Suspend(); err != nil {
		return fmt.Errorf("releasing the log file before the move: %w", err)
	}
	if err := config.MoveTree(src.Root, dst); err != nil {
		// The tree is still whole at the old location. Put the log back so the
		// failure has somewhere to be recorded.
		_ = e.Log.Reopen(src.LogFile())
		return err
	}
	return e.rebind(config.Home{Root: dst})
}

// UseDefaultLocation moves the folder back to ~/.portcloak and forgets the
// pointer, so a machine that has been reset behaves like a fresh install.
func (e *Engine) UseDefaultLocation() error {
	loc, err := config.Locate()
	if err != nil {
		return err
	}
	if loc.Source == config.HomeDefault {
		return resil.Fatal("move the PortCloak folder",
			"PortCloak is already using the default folder.", errors.New("already at the default"))
	}
	return e.Relocate(loc.Default)
}

// idleForRelocation refuses the move while anything holds a path under the
// current root.
func (e *Engine) idleForRelocation() error {
	if open := e.OpenSessionIDs(); len(open) > 0 {
		return resil.Fatal("move the PortCloak folder",
			fmt.Sprintf("%d snapshot%s open. Close it from the inspector first — an open snapshot is being read out of the folder you are asking to move.",
				len(open), isAre(len(open))),
			errors.New("a snapshot is open"))
	}
	if running := e.Orch.Running(); len(running) > 0 {
		return resil.Fatal("move the PortCloak folder",
			fmt.Sprintf("%d job%s running. Wait for it to finish, or cancel it on the Activity screen.",
				len(running), isAre(len(running))),
			errors.New("a job is running"))
	}
	jobs, err := e.Jobs.List()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.State == config.JobQueued || j.State == config.JobRunning {
			return resil.Fatal("move the PortCloak folder",
				"A job is queued or running. Wait for it to finish, or cancel it on the Activity screen.",
				errors.New("a job is in flight"))
		}
	}
	return nil
}

// rebind points every part of the engine at the folder's new location.
//
// The objects themselves survive — controllers, the orchestrator and every
// running goroutine hold pointers to them, and swapping those pointers would be
// a race with nothing to gain over rebinding what is behind them.
func (e *Engine) rebind(home config.Home) error {
	if err := home.Bootstrap(); err != nil {
		return err
	}
	e.mu.Lock()
	e.home = home
	e.mu.Unlock()

	e.Jobs.Rebind(home)
	e.Orch.SetHome(home)

	var problems []error
	if err := e.Log.Reopen(home.LogFile()); err != nil {
		problems = append(problems, err)
	}
	if err := e.Audit.Rebind(home.AuditFile()); err != nil {
		problems = append(problems, err)
	}
	// A configuration that will not load is reported the same way it is at
	// launch: the app keeps running and the screens say which line is wrong.
	e.LoadError = e.Config.Rebind(home)

	// The pointer is written last, so a folder PortCloak could not actually
	// bind to is not the folder it will look for on the next launch.
	if home.Root == defaultRoot() {
		if err := config.ClearPointer(); err != nil {
			problems = append(problems, err)
		}
	} else if err := config.WritePointer(home.Root); err != nil {
		problems = append(problems, err)
	}
	return errors.Join(problems...)
}

func defaultRoot() string {
	loc, err := config.Locate()
	if err != nil {
		return ""
	}
	return loc.Default
}

func isAre(n int) string {
	if n == 1 {
		return " is"
	}
	return "s are"
}
