// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	"portcloak/internal/engine/config"
)

// Two PortCloaks over one folder do not merely interleave: StartupSweep rewrites
// every running job to interrupted and deletes the working directories of
// snapshots it cannot see are open. So writing is exclusive — and reading is
// deliberately not, because watching a running capture from a terminal is the
// most useful thing this binary adds to a running desktop.

func TestCLI_ReadOnlyCommandsWorkWhileAWriterHoldsTheHome(t *testing.T) {
	home := scratchHome(t)
	h := config.Home{Root: home}
	if err := h.Bootstrap(); err != nil {
		t.Fatal(err)
	}

	// A window open all afternoon holds the folder *shared*, which is what lets
	// everything below run beside it.
	held, err := config.Acquire(h, config.LockShared, config.Holder{Program: "PortCloak"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Release() }()

	for _, args := range [][]string{
		{"snapshot", "list"},
		{"job", "list"},
		{"config", "show"},
		{"key", "list"},
		{"env", "list"},
	} {
		res := runCLI(t, home, args...)
		if res.Code != ExitOK {
			t.Errorf("`pcloak %s` was refused while the folder was held: exit %d\n%s",
				strings.Join(args, " "), res.Code, res.all())
		}
	}
}

// Capturing beside another PortCloak is fine and stays fine: each run writes its
// own job record and its own staging directory, and one snapshot holds one realm,
// so there is nothing shared to corrupt.
func TestCLI_CaptureRunsAlongsideAnotherPortCloak(t *testing.T) {
	home := scratchHome(t)
	h := config.Home{Root: home}
	if err := h.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	held, err := config.Acquire(h, config.LockShared, config.Holder{Program: "PortCloak"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Release() }()

	// It gets as far as a real refusal about the environment rather than a
	// refusal about the folder, which is the distinction under test.
	res := runCLI(t, home, "capture", "-e", "nope", "-r", "corp", "-s", "nowhere", "--key", "k")
	if res.Code == ExitBusy {
		t.Fatalf("a capture was refused for sharing the folder:\n%s", res.all())
	}
}

// A change to config.yaml is the one thing that genuinely cannot be concurrent:
// both writers read the file before either writes, so the second silently drops
// the first one's change.
func TestCLI_RefusesAConfigChangeWhileAnotherPortCloakHoldsTheHome(t *testing.T) {
	home := scratchHome(t)
	h := config.Home{Root: home}
	if err := h.Bootstrap(); err != nil {
		t.Fatal(err)
	}

	held, err := config.Acquire(h, config.LockExclusive,
		config.Holder{Program: "PortCloak", Command: "saving a storage"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Release() }()

	// A command that changes config.yaml and needs nothing else, so the folder
	// is the only thing that can refuse it.
	res := runCLI(t, home, "key", "rename", "a", "b")
	if res.Code != ExitBusy {
		t.Fatalf("a config change ran while the folder was held exclusively: exit %d\n%s", res.Code, res.all())
	}
	// Its own exit code, so a CI wrapper can retry on exactly this. And the
	// message has to name the holder — the commonest one is a window somebody
	// could simply quit.
	if !strings.Contains(res.all(), "PortCloak") {
		t.Errorf("the refusal does not name the holder:\n%s", res.all())
	}
	if !strings.Contains(res.all(), "Everything else works") {
		t.Errorf("the refusal does not say what still works:\n%s", res.all())
	}
}
