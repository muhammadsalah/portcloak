// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"strconv"
	"testing"

	"portcloak/internal/engine/obs"
)

// The output of a run cannot be fetched back from anywhere: it arrives over an
// exec stream from a clone that is destroyed at the end of the job. This store
// is the only copy, so what it keeps is what an operator can still read after a
// reload.
func TestLogStore_KeepsWhatTheRunSaid(t *testing.T) {
	s := newLogStore()
	s.record(obs.Event{JobID: "j1", Kind: obs.EventLog, Message: "Exported realm acme"})
	s.record(obs.Event{JobID: "j1", Kind: obs.EventCloneCreated, Item: "pod/portcloak-j1"})
	s.record(obs.Event{JobID: "j1", Kind: obs.EventCloneDestroyed, Item: "pod/portcloak-j1"})
	// Not every event is a line. A progress tick belongs on the bar.
	s.record(obs.Event{JobID: "j1", Kind: obs.EventProgress, Current: 5, Unit: "users"})
	// Another job's output is another job's.
	s.record(obs.Event{JobID: "j2", Kind: obs.EventLog, Message: "Exported realm other"})

	tail := s.tail("j1", 0)
	if len(tail.Lines) != 3 {
		t.Fatalf("tail = %+v, want the three lines", tail.Lines)
	}
	if tail.Lines[0].Text != "Exported realm acme" || tail.Lines[0].FromPortCloak {
		t.Errorf("the subprocess's own line is wrong: %+v", tail.Lines[0])
	}
	if !tail.Lines[1].FromPortCloak || tail.Lines[1].Text != "Ephemeral clone pod/portcloak-j1 is running." {
		t.Errorf("the clone line is wrong: %+v", tail.Lines[1])
	}
	if tail.Truncated {
		t.Error("a short tail reported itself truncated")
	}
	if len(s.tail("j2", 0).Lines) != 1 {
		t.Error("one job's output reached another's tail")
	}
	if len(s.tail("never-ran", 0).Lines) != 0 {
		t.Error("a job that said nothing has something to say")
	}
}

// It is a tail, not a log file. An export of a large realm says more than
// anyone will read, and holding all of it for the life of the process is a leak
// with an operator's realm names in it.
func TestLogStore_KeepsTheLastLinesAndSaysItDropped(t *testing.T) {
	s := newLogStore()
	for i := 0; i < maxLogLines+50; i++ {
		s.record(obs.Event{JobID: "j1", Kind: obs.EventLog, Message: strconv.Itoa(i)})
	}
	tail := s.tail("j1", 0)
	if len(tail.Lines) != maxLogLines {
		t.Fatalf("kept %d lines, want the cap of %d", len(tail.Lines), maxLogLines)
	}
	if tail.Lines[0].Text != "50" {
		t.Errorf("the oldest kept line is %q, want the 50th", tail.Lines[0].Text)
	}
	if !tail.Truncated {
		t.Error("a tail that dropped lines did not say so")
	}
}

// A discarded job takes its output with it, or a deleted job's export lines sit
// in front of whoever looks next.
func TestLogStore_ForgetsADiscardedJob(t *testing.T) {
	s := newLogStore()
	s.record(obs.Event{JobID: "j1", Kind: obs.EventLog, Message: "something"})
	s.forget("j1")
	if len(s.tail("j1", 0).Lines) != 0 {
		t.Error("a forgotten job kept its output")
	}
	if s.tail("j1", 0).Truncated {
		t.Error("a forgotten job kept its truncation")
	}
}

// The tail handed out is a copy. A caller that holds one while the run keeps
// talking must not see it change underneath, and must not be able to change
// what the store holds.
func TestLogStore_HandsOutACopy(t *testing.T) {
	s := newLogStore()
	s.record(obs.Event{JobID: "j1", Kind: obs.EventLog, Message: "first"})
	tail := s.tail("j1", 0)
	tail.Lines[0].Text = "tampered"
	s.record(obs.Event{JobID: "j1", Kind: obs.EventLog, Message: "second"})

	fresh := s.tail("j1", 0)
	if fresh.Lines[0].Text != "first" {
		t.Errorf("the store was changed through a handed-out tail: %+v", fresh.Lines)
	}
	if len(tail.Lines) != 1 {
		t.Errorf("a held tail grew: %+v", tail.Lines)
	}
}

// Recording happens whatever else is attached, and whatever is attached still
// receives the event.
func TestRecordingSink_RecordsAndPassesOn(t *testing.T) {
	s := newLogStore()
	var seen []obs.Event
	sink := recordingSink{store: s, next: obs.SinkFunc(func(e obs.Event) { seen = append(seen, e) })}

	sink.Emit(obs.Event{JobID: "j1", Kind: obs.EventLog, Message: "line"})

	if len(s.tail("j1", 0).Lines) != 1 {
		t.Error("the event was passed on but not recorded")
	}
	if len(seen) != 1 {
		t.Error("the event was recorded but not passed on")
	}

	// A sink with nothing behind it still records, which is the headless case.
	bare := recordingSink{store: s}
	bare.Emit(obs.Event{JobID: "j2", Kind: obs.EventLog, Message: "line"})
	if len(s.tail("j2", 0).Lines) != 1 {
		t.Error("an unattached sink recorded nothing")
	}
}

// The screen asks for what it has not been given yet. At ten thousand lines,
// handing back the whole tail every second would be most of a megabyte a second
// per running job, for output that has not changed.
func TestLogStore_AnswersFromTheCallersCursor(t *testing.T) {
	s := newLogStore()
	for i := 0; i < 5; i++ {
		s.record(obs.Event{JobID: "j1", Kind: obs.EventLog, Message: strconv.Itoa(i)})
	}

	first := s.tail("j1", 0)
	if !first.Reset || len(first.Lines) != 5 || first.Next != 5 {
		t.Fatalf("the first read is wrong: %+v", first)
	}

	// Nothing said since: nothing sent, and the cursor stands still.
	quiet := s.tail("j1", first.Next)
	if len(quiet.Lines) != 0 || quiet.Reset || quiet.Next != 5 {
		t.Errorf("a quiet read is wrong: %+v", quiet)
	}

	s.record(obs.Event{JobID: "j1", Kind: obs.EventLog, Message: "5"})
	next := s.tail("j1", quiet.Next)
	if next.Reset {
		t.Error("a continuable read asked the caller to start again")
	}
	if len(next.Lines) != 1 || next.Lines[0].Text != "5" || next.Next != 6 {
		t.Errorf("the continued read is wrong: %+v", next)
	}
}

// A cursor pointing at a line the cap has discarded cannot be continued from.
// Returning the newest lines as if they followed it would splice a hole into
// the middle of the operator's log without saying so.
func TestLogStore_RestartsACallerThatFellBehindTheCap(t *testing.T) {
	s := newLogStore()
	for i := 0; i < maxLogLines+100; i++ {
		s.record(obs.Event{JobID: "j1", Kind: obs.EventLog, Message: strconv.Itoa(i)})
	}

	behind := s.tail("j1", 10)
	if !behind.Reset {
		t.Fatal("a caller behind the cap was told to continue from a line that is gone")
	}
	if len(behind.Lines) != maxLogLines || behind.Lines[0].Text != "100" {
		t.Errorf("the restart is not the whole held tail: %d lines from %q",
			len(behind.Lines), behind.Lines[0].Text)
	}
	if !behind.Truncated {
		t.Error("a tail that dropped lines did not say so")
	}
}

// The cap has to hold the memory down, not just the count. A tail resliced off
// an ever-growing array keeps every discarded line alive behind it.
func TestLogStore_DoesNotHoldDiscardedLines(t *testing.T) {
	s := newLogStore()
	for i := 0; i < maxLogLines*2; i++ {
		s.record(obs.Event{JobID: "j1", Kind: obs.EventLog, Message: strconv.Itoa(i)})
	}
	if got := cap(s.lines["j1"]); got > maxLogLines*2 {
		t.Errorf("the tail is backed by an array of %d for %d lines", got, maxLogLines)
	}
}
