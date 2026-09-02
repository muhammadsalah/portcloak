// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"testing"

	"portcloak/internal/engine/config"
)

// The startup sweep is the one piece of housekeeping that is destructive to
// another process: AdoptRunning rewrites every running job to interrupted, and
// the index and work-directory sweeps keep only the sessions this process knows
// are open. Run beside a capture in another PortCloak it would mark that capture
// interrupted and delete the staging directory it is still writing into.
//
// The guard is structural: the destructive part is unexported, and the only way
// in takes the exclusive claim first. These tests pin the behaviour on both
// sides of that claim.
func TestSweepIfSolelyHere_LeavesAnotherPortCloaksJobsAlone(t *testing.T) {
	home := config.Home{Root: t.TempDir()}
	eng, err := NewEngineAt(home, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = eng.Close() }()

	running := &config.Job{ID: "j1", Kind: config.JobCapture, State: config.JobRunning}
	if err := eng.Jobs.Save(running); err != nil {
		t.Fatal(err)
	}

	// Somebody else is here — a window with a capture in flight is the case
	// that matters.
	other, err := config.Acquire(home, config.LockShared, config.Holder{Program: "PortCloak"})
	if err != nil {
		t.Fatal(err)
	}
	eng.SweepIfSolelyHere()
	if j, err := eng.Jobs.Load("j1"); err != nil {
		t.Fatal(err)
	} else if j.State != config.JobRunning {
		t.Fatalf("the sweep adopted another PortCloak's running job: %s", j.State)
	}
	if err := other.Release(); err != nil {
		t.Fatal(err)
	}

	// Alone, it does its job: a job left running by a process that died is
	// offered for resume rather than appearing to still be in flight.
	eng.SweepIfSolelyHere()
	if j, err := eng.Jobs.Load("j1"); err != nil {
		t.Fatal(err)
	} else if j.State != config.JobInterrupted {
		t.Fatalf("the sweep should have adopted the job; it is %s", j.State)
	}
}
