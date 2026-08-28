// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// The orphan sweep's one rule: a clone that a job is using is not wreckage.
//
// The two are indistinguishable by inspection — same image, same labels, same
// name — and the sweep offers to destroy what it finds. Getting this wrong does
// not produce a wrong screen; it produces a capture that dies part-way through
// a realm because its clone was deleted underneath it.

package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"portcloak/internal/engine/target"
	"portcloak/internal/engine/target/faketarget"
)

// An ephemeral clone looks exactly the same whether a capture is exporting
// through it or a crashed session abandoned it: same image, same labels, same
// name. The only thing that tells them apart is whether anything is still
// driving it — so a clone belonging to a running job is not an orphan, and the
// sweep that said otherwise offered a button that would have killed a capture
// mid-realm.
func TestAbandoned_LeavesRunningJobsAlone(t *testing.T) {
	found := []target.Orphan{
		{Ref: "pod/portcloak-running", JobID: "job-running"},
		{Ref: "pod/portcloak-dead", JobID: "job-dead"},
	}
	running := map[string]bool{"job-running": true}

	out := abandoned(found, running)

	if len(out) != 1 {
		t.Fatalf("kept %d of 2: %+v", len(out), out)
	}
	if out[0].Ref != "pod/portcloak-dead" {
		t.Errorf("the wrong clone was reported as abandoned: %+v", out[0])
	}
}

// A clone whose job label did not survive is still reported. It cannot be tied
// to a run, and the alternative — reporting nothing at all while any job is in
// flight — hides real wreckage for the length of a capture.
func TestAbandoned_KeepsAnUnlabelledClone(t *testing.T) {
	out := abandoned([]target.Orphan{{Ref: "container/abc", JobID: ""}}, map[string]bool{"job-1": true})
	if len(out) != 1 {
		t.Fatalf("an unlabelled clone was dropped: %+v", out)
	}
}

// With nothing running, nothing is filtered — the sweep's whole purpose.
func TestAbandoned_ReportsEverythingWhenNothingRuns(t *testing.T) {
	found := []target.Orphan{{Ref: "pod/a", JobID: "job-a"}, {Ref: "pod/b", JobID: "job-b"}}
	if out := abandoned(found, map[string]bool{}); len(out) != 2 {
		t.Fatalf("reported %d of 2 with nothing running", len(out))
	}
}

// The list an operator is looking at was accurate when it loaded. A capture
// started since is driving a clone that was not on it, and on Docker the
// reference is a container id, so nothing about the string itself says which
// job it belongs to.
func TestRefuseIfRunning_StopsRemovingALiveClone(t *testing.T) {
	sweeper := faketarget.NewSweeper(
		target.Orphan{Ref: "pod/portcloak-live", JobID: "job-1", CreatedAt: time.Now()},
	)

	fail := refuseIfRunning(context.Background(), sweeper, map[string]bool{"job-1": true}, "pod/portcloak-live")

	if fail == nil {
		t.Fatal("a clone belonging to a running job was cleared for removal")
	}
	if !strings.Contains(fail.Message, "running") {
		t.Errorf("the refusal does not say why: %q", fail.Message)
	}
	// The way out is the one that also cleans up what the job wrote.
	if !strings.Contains(fail.Hint, "Cancel") {
		t.Errorf("the refusal offers no way forward: %q", fail.Hint)
	}
}

// A clone belonging to a job that is not running is removable, which is what
// the sweep is for.
func TestRefuseIfRunning_AllowsATrueOrphan(t *testing.T) {
	sweeper := faketarget.NewSweeper(
		target.Orphan{Ref: "pod/portcloak-dead", JobID: "job-dead"},
	)
	if fail := refuseIfRunning(context.Background(), sweeper, map[string]bool{"job-other": true}, "pod/portcloak-dead"); fail != nil {
		t.Errorf("a true orphan was refused: %+v", fail)
	}
}

// A reference that is not in the listing is already gone, or was never there.
// Removing it is harmless and must not be blocked by the guard.
func TestRefuseIfRunning_AllowsACloneThatIsNotThere(t *testing.T) {
	sweeper := faketarget.NewSweeper(target.Orphan{Ref: "pod/other", JobID: "job-other"})
	if fail := refuseIfRunning(context.Background(), sweeper, map[string]bool{"job-1": true}, "pod/gone"); fail != nil {
		t.Errorf("removing something that is not there was refused: %+v", fail)
	}
}

// Where the platform cannot be listed, what happens depends on what is at
// stake. With a job in flight an unverifiable delete is a capture that dies
// mid-realm, so it is refused; with nothing running there is nothing to
// protect, so it goes ahead.
func TestRefuseIfRunning_WillNotGuessWhenItMatters(t *testing.T) {
	sweeper := faketarget.NewSweeper()
	sweeper.FindErr = errors.New("the cluster could not be listed")

	if fail := refuseIfRunning(context.Background(), sweeper, map[string]bool{"job-1": true}, "pod/a"); fail == nil {
		t.Error("an unverifiable removal was allowed while a job was running")
	}
	if fail := refuseIfRunning(context.Background(), sweeper, map[string]bool{}, "pod/a"); fail != nil {
		t.Errorf("an unverifiable removal was refused with nothing running: %+v", fail)
	}
}
