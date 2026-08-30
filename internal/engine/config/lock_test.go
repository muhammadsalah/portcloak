// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"portcloak/internal/engine/resil"
)

// The lock is what makes "one PortCloak per home" true once there is more than
// one binary. Everything it protects — the job records, the inspection indexes,
// the staging area — is shared mutable state that neither process would notice
// the other writing.

func lockHome(t *testing.T) Home {
	t.Helper()
	h := Home{Root: t.TempDir()}
	if err := h.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	return h
}

// Exclusive means "no other PortCloak is here". It is taken for a change to
// config.yaml and for the startup sweep, and released — never held for a session.
func TestHomeLock_RefusesASecondWriter(t *testing.T) {
	home := lockHome(t)

	first, err := Acquire(home, LockExclusive, Holder{Program: "PortCloak"})
	if err != nil {
		t.Fatalf("the first writer should have got the lock: %v", err)
	}
	defer func() { _ = first.Release() }()

	if _, err := Acquire(home, LockExclusive, Holder{Program: "pcloak"}); err == nil {
		t.Fatal("a second writer took a lock the first was holding")
	}
	// A reader must not slip past a writer either. Reading a job store being
	// rewritten is how a half-written record gets rendered as a real one.
	if _, err := Acquire(home, LockShared, Holder{Program: "pcloak"}); err == nil {
		t.Fatal("a reader took a lock while a writer held it")
	}

	// Releasing hands it on rather than leaving the folder claimed forever.
	if err := first.Release(); err != nil {
		t.Fatalf("releasing: %v", err)
	}
	second, err := Acquire(home, LockExclusive, Holder{Program: "pcloak"})
	if err != nil {
		t.Fatalf("the lock was not released: %v", err)
	}
	_ = second.Release()
}

// Shared means "a PortCloak is here". Every process takes it and keeps it, which
// is why several must coexist: a window open all afternoon holds one for the
// whole afternoon, and every terminal command has to run beside it.
func TestHomeLock_AllowsSeveralReaders(t *testing.T) {
	home := lockHome(t)

	var held []*Lock
	for i := range 3 {
		l, err := Acquire(home, LockShared, Holder{Program: "pcloak"})
		if err != nil {
			t.Fatalf("reader %d was refused: %v", i, err)
		}
		held = append(held, l)
	}
	// ...but a writer still cannot start underneath them.
	if _, err := Acquire(home, LockExclusive, Holder{Program: "pcloak"}); err == nil {
		t.Error("a writer started while readers held the folder")
	}
	for _, l := range held {
		_ = l.Release()
	}
	w, err := Acquire(home, LockExclusive, Holder{Program: "pcloak"})
	if err != nil {
		t.Fatalf("the writer should get the lock once the readers are gone: %v", err)
	}
	_ = w.Release()
}

// The refusal has to name who is in the way. "Another PortCloak is using this
// folder" with no further detail leaves an operator hunting for a process they
// cannot see, and the commonest holder is a window they could simply quit.
func TestHomeLock_NamesTheHolder(t *testing.T) {
	home := lockHome(t)

	first, err := Acquire(home, LockExclusive, Holder{Program: "PortCloak", Command: "capture"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Release() }()

	_, err = Acquire(home, LockExclusive, Holder{Program: "pcloak"})
	if err == nil {
		t.Fatal("expected a refusal")
	}
	msg := err.Error()
	for _, want := range []string{"PortCloak", "capture", home.Root} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not mention %q: %s", want, msg)
		}
	}
	if !strings.Contains(msg, "pid ") {
		t.Errorf("the refusal does not name a process id: %s", msg)
	}
	// It also has to say what still works, or the answer to "PortCloak is open"
	// looks like "nothing can be done from a terminal" — when in fact the
	// exclusive claim is only taken for a change to config.yaml.
	if advice := resil.Hint(err); !strings.Contains(advice, "Everything else works") {
		t.Errorf("the refusal does not say what still works: %s", advice)
	}
}

// The whole reason for an OS lock rather than a pidfile: a process that dies
// without releasing must not wedge the folder. A pidfile survives a crash, and
// the cure — telling an operator to delete a file by hand — teaches them to
// delete it whenever anything looks stuck, including when it is not.
func TestHomeLock_IsReleasedWhenTheProcessDies(t *testing.T) {
	if os.Getenv("PORTCLOAK_LOCK_CHILD") != "" {
		// Re-entered as a child: take the lock, say so, and block until killed.
		h := Home{Root: os.Getenv("PORTCLOAK_LOCK_CHILD")}
		if _, err := Acquire(h, LockExclusive, Holder{Program: "child"}); err != nil {
			os.Stderr.WriteString("child could not lock: " + err.Error())
			os.Exit(1)
		}
		os.Stdout.WriteString("locked\n")
		select {}
	}

	home := lockHome(t)

	child := exec.Command(os.Args[0], "-test.run=TestHomeLock_IsReleasedWhenTheProcessDies")
	child.Env = append(os.Environ(), "PORTCLOAK_LOCK_CHILD="+home.Root)
	out, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	}()

	// Wait for the child to report that it holds the lock, rather than sleeping
	// a guessed interval: a timing-based test here would be flaky on exactly
	// the loaded machines where it matters.
	buf := make([]byte, len("locked\n"))
	done := make(chan error, 1)
	go func() { _, err := out.Read(buf); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the child never reported holding the lock: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the child did not take the lock within 30s")
	}

	if _, err := Acquire(home, LockExclusive, Holder{Program: "parent"}); err == nil {
		t.Fatal("the parent took a lock the child was holding")
	}

	// Kill, do not signal: the point is that a process which never gets to run
	// cleanup code still gives the lock back.
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = child.Process.Wait()

	deadline := time.Now().Add(30 * time.Second)
	for {
		l, err := Acquire(home, LockExclusive, Holder{Program: "parent"})
		if err == nil {
			_ = l.Release()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the lock outlived the process that held it: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
