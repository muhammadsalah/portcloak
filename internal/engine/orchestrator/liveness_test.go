// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package orchestrator_test

import (
	"testing"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/orchestrator"
)

// The Activity screen reads two things: a live event stream, and the job record
// it re-reads on every refresh. They have to agree. A phase that is only ever
// announced to the stream leaves the record saying nothing, which is what made
// a running capture look identical to a stalled one until the screen was left
// and reopened.
func TestCapture_PhasesAreWrittenOntoTheJobRecord(t *testing.T) {
	h := newHarness(t)
	jobs := h.capture(defaultRequest())
	j := jobs[0]

	if j.State != config.JobCompleted {
		t.Fatalf("job ended %s: %s", j.State, j.Message)
	}

	done := map[string]bool{}
	for _, p := range j.CompletedPhases {
		done[p] = true
	}
	// Every phase of the capture pipeline the Activity screen ticks off.
	for _, want := range []obs.Phase{
		obs.PhaseProbe, obs.PhaseClone, obs.PhaseExport, obs.PhaseFetch,
		obs.PhaseTeardown, obs.PhaseManifest, obs.PhasePackage, obs.PhaseUpload,
	} {
		if !done[string(want)] {
			t.Errorf("phase %q never reached the job record; the pipeline would draw it as pending forever", want)
		}
	}
	if j.Phase == "" {
		t.Error("the job record never recorded which phase it was in")
	}
}

// A batch of realms shares one probe and one clone. Reporting those under the
// first realm's id alone left every other card blank through the slowest part
// of the run, so the shared phases fan out to every job in the batch.
func TestCapture_SharedPhasesReachEveryRealmInTheBatch(t *testing.T) {
	h := newHarness(t)
	req := defaultRequest()
	req.Realms = []string{"acme", "acme"}
	jobs := h.capture(req)

	if len(jobs) != 2 {
		t.Fatalf("got %d jobs", len(jobs))
	}
	for _, j := range jobs {
		done := map[string]bool{}
		for _, p := range j.CompletedPhases {
			done[p] = true
		}
		for _, want := range []obs.Phase{obs.PhaseProbe, obs.PhaseClone} {
			if !done[string(want)] {
				t.Errorf("job %s never saw the shared phase %q", j.ID, want)
			}
		}
	}

	// And the same fan-out on the stream itself: both cards, not just the first.
	seen := map[string]bool{}
	for _, e := range h.sink.Events() {
		if e.Kind == obs.EventPhaseStarted && e.Phase == obs.PhaseProbe {
			seen[e.JobID] = true
		}
	}
	for _, j := range jobs {
		if !seen[j.ID] {
			t.Errorf("the probe was never announced for job %s", j.ID)
		}
	}
}

// Nothing above should have cost the reporter its identity: an event still
// carries the job it belongs to, redacted and stamped.
func TestReporterFanout_KeepsOneEventPerJob(t *testing.T) {
	h := newHarness(t)
	jobs := h.capture(defaultRequest())

	var count int
	for _, e := range h.sink.Events() {
		if e.JobID != jobs[0].ID {
			t.Fatalf("an event carried an unknown job id %q", e.JobID)
		}
		if e.At.IsZero() {
			t.Error("an event went out without a timestamp")
		}
		count++
	}
	if count == 0 {
		t.Fatal("no events were emitted at all")
	}
	_ = orchestrator.PlanResume
}
